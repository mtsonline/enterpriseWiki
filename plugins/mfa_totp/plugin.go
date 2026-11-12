// Package mfa_totp is the factor "authenticator app" (TOTP, RFC 6238):
// 6 digits, 30 second steps, SHA-1, ±1 step tolerance, every code once.
// Setting (plugin_settings."mfa.totp"): factor (default 2).
package mfa_totp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- HMAC-SHA1 is the RFC 6238 default used by all authenticator apps (not a collision use)
	"embed"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// Plugin is the TOTP plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Setup registers the factor and the endpoints of the own authenticator.
func (p *Plugin) Setup(host *sdk.Host) error {
	m := &Method{pool: host.DB, cfg: host.Config, box: host.Secrets}
	host.RegisterMFAMethod(m)
	host.Handle("GET", "/me", func(w http.ResponseWriter, r *http.Request) (any, error) {
		pr := identity.From(r.Context())
		if pr == nil || pr.UserID == 0 {
			return nil, apperr.ErrUnauthorized
		}
		return map[string]bool{"enabled": m.Enrolled(r.Context(), pr.UserID)}, nil
	})
	// switching it off needs a recent sign-in (like setting it up)
	host.Handle("DELETE", "/me", func(w http.ResponseWriter, r *http.Request) (any, error) {
		uid, err := auth.RequireRecentSignIn(r.Context())
		if err != nil {
			return nil, err
		}
		if err := m.ResetMFA(r.Context(), uid); err != nil {
			return nil, err
		}
		return h.NoContent{}, audit.Log(r.Context(), host.DB, "user.totp_disable", "user", uid, nil, nil)
	})
	return nil
}

// Method implements auth.MFAMethod, MFAEnroller, MFAResetter and MFABulk.
type Method struct {
	pool *pgxpool.Pool
	cfg  *config.Store
	box  *secrets.Box
}

func init() {
	secrets.Register(secrets.Column{Table: "plugin_mfa_totp", Key: "user_id", Value: "secret", Text: true})
}

func aad(uid int64) string { return secrets.AAD("plugin_mfa_totp", strconv.FormatInt(uid, 10)) }

// ID implements auth.MFAMethod.
func (m *Method) ID() string { return "totp" }

// DefaultFactor implements auth.MFAMethod.
func (m *Method) DefaultFactor() int { return 2 }

// Enrolled implements auth.MFAMethod.
func (m *Method) Enrolled(ctx context.Context, uid int64) bool {
	var on bool
	_ = m.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM plugin_mfa_totp WHERE user_id=$1)`, uid).Scan(&on)
	return on
}

// EnrolledAmong implements auth.MFABulk.
func (m *Method) EnrolledAmong(ctx context.Context, ids []int64) map[int64]bool {
	out := map[int64]bool{}
	rows, err := m.pool.Query(ctx, `SELECT user_id FROM plugin_mfa_totp WHERE user_id = ANY($1)`, ids)
	if err != nil {
		return out
	}
	list, _ := pgx.CollectRows(rows, pgx.RowTo[int64])
	for _, id := range list {
		out[id] = true
	}
	return out
}

// Challenge implements auth.MFAMethod (nothing to prepare).
func (m *Method) Challenge(context.Context, int64) (any, string, error) { return nil, "", nil }

type codeAnswer struct {
	Code string `json:"code"`
}

// Verify implements auth.MFAMethod: the code of the current step (±1),
// newer than the last one used. Taking the step and checking it is one
// statement, so a code works once even with parallel requests.
func (m *Method) Verify(ctx context.Context, uid int64, _ string, answer json.RawMessage) error {
	var in codeAnswer
	_ = json.Unmarshal(answer, &in)
	var secret string
	var last int64
	if err := m.pool.QueryRow(ctx, `SELECT secret, last_step FROM plugin_mfa_totp WHERE user_id=$1`, uid).Scan(&secret, &last); err != nil {
		return auth.ErrMFAInvalid
	}
	secret, err := m.box.OpenString(secret, aad(uid))
	if err != nil {
		return err // the key file is missing: no sign-in with the app (fail closed)
	}
	step, ok := VerifyTOTP(secret, in.Code, time.Now(), last)
	if !ok {
		return auth.ErrMFAInvalid
	}
	tag, err := m.pool.Exec(ctx, `UPDATE plugin_mfa_totp SET last_step=$2 WHERE user_id=$1 AND last_step < $2`, uid, step)
	if err != nil || tag.RowsAffected() != 1 {
		return auth.ErrMFAInvalid
	}
	return nil
}

// Setup is returned when the authenticator is being set up.
type Setup struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// BeginEnroll implements auth.MFAEnroller: a new secret (kept in the
// state until the person proved it with a code).
func (m *Method) BeginEnroll(ctx context.Context, uid int64, _ json.RawMessage) (any, string, error) {
	if m.Enrolled(ctx, uid) {
		return nil, "", apperr.Conflict("the authenticator app is already set up")
	}
	var name string
	if err := m.pool.QueryRow(ctx, `SELECT COALESCE(alias, username) FROM users WHERE id=$1`, uid).Scan(&name); err != nil {
		return nil, "", err
	}
	issuer := m.cfg.Get().UI.SiteName
	if issuer == "" {
		issuer = "Enterprise Wiki"
	}
	secret := NewTOTPSecret()
	uri := fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		url.PathEscape(issuer+":"+name), secret, url.QueryEscape(issuer))
	return Setup{Secret: secret, URI: uri}, secret, nil
}

// FinishEnroll implements auth.MFAEnroller.
func (m *Method) FinishEnroll(ctx context.Context, uid int64, state string, answer json.RawMessage) error {
	var in codeAnswer
	_ = json.Unmarshal(answer, &in)
	if state == "" {
		return apperr.BadRequest("start the setup first")
	}
	step, ok := VerifyTOTP(state, in.Code, time.Now(), 0)
	if !ok {
		return auth.ErrMFAInvalid
	}
	sealed, err := m.box.SealString(state, aad(uid))
	if err != nil {
		return err
	}
	tag, err := m.pool.Exec(ctx, `INSERT INTO plugin_mfa_totp(user_id, secret, last_step) VALUES ($1,$2,$3) ON CONFLICT (user_id) DO NOTHING`, uid, sealed, step)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.Conflict("the authenticator app is already set up")
	}
	return nil
}

// ResetMFA implements auth.MFAResetter.
func (m *Method) ResetMFA(ctx context.Context, uid int64) error {
	_, err := m.pool.Exec(ctx, `DELETE FROM plugin_mfa_totp WHERE user_id=$1`, uid)
	return err
}

// ---------------------------------------------------------------- RFC 6238

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random base32 secret (160 bit).
func NewTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b32.EncodeToString(b)
}

// TOTPCode computes the code for a time step.
func TOTPCode(secret string, step int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) // #nosec G115 -- time step since 1970, never negative
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000), nil
}

// VerifyTOTP checks code against now ±1 step and returns the matched step.
// Steps <= lastStep are rejected (replay protection).
func VerifyTOTP(secret, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0, false
	}
	cur := now.Unix() / 30
	for _, s := range []int64{cur - 1, cur, cur + 1} {
		if s <= lastStep {
			continue
		}
		c, err := TOTPCode(secret, s)
		if err == nil && hmac.Equal([]byte(c), []byte(code)) {
			return s, true
		}
	}
	return 0, false
}
