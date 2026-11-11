// Package mfa_email is the factor "code by e-mail": a 6-digit code to the
// primary address, valid 10 minutes, 5 attempts, used once. Switching it
// on is confirmed with a code as well. Setting
// (plugin_settings."mfa.email"): factor (default 2).
package mfa_email

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/mail"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// Plugin is the e-mail code plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

const (
	codeTTL      = 10 * time.Minute
	codeAttempts = 5
)

// Setup registers the factor and the endpoints of the own setting.
func (p *Plugin) Setup(host *sdk.Host) error {
	m := &Method{host: host, sent: map[int64][]time.Time{}}
	host.RegisterMFAMethod(m)
	host.Handle("GET", "/me", func(w http.ResponseWriter, r *http.Request) (any, error) {
		pr := identity.From(r.Context())
		if pr == nil || pr.UserID == 0 {
			return nil, apperr.ErrUnauthorized
		}
		return map[string]any{"enabled": m.Enrolled(r.Context(), pr.UserID), "address": pr.Email, "mail": host.Mail.Enabled()}, nil
	})
	host.Handle("DELETE", "/me", func(w http.ResponseWriter, r *http.Request) (any, error) {
		uid, err := auth.RequireRecentSignIn(r.Context())
		if err != nil {
			return nil, err
		}
		if err := m.ResetMFA(r.Context(), uid); err != nil {
			return nil, err
		}
		return h.NoContent{}, audit.Log(r.Context(), host.DB, "user.mfa_email_disable", "user", uid, nil, nil)
	})
	return nil
}

// Method implements auth.MFAMethod, MFAEnroller, MFAResetter and MFABulk.
type Method struct {
	host *sdk.Host
	mu   sync.Mutex
	sent map[int64][]time.Time // codes sent per user (5 per 15 minutes)
}

// ID implements auth.MFAMethod.
func (m *Method) ID() string { return "email" }

// DefaultFactor implements auth.MFAMethod.
func (m *Method) DefaultFactor() int { return 2 }

// Enrolled implements auth.MFAMethod: switched on and an address present.
func (m *Method) Enrolled(ctx context.Context, uid int64) bool {
	var on bool
	_ = m.host.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM plugin_mfa_email e JOIN users u ON u.id=e.user_id WHERE e.user_id=$1 AND u.email <> '')`, uid).Scan(&on)
	return on
}

// EnrolledAmong implements auth.MFABulk.
func (m *Method) EnrolledAmong(ctx context.Context, ids []int64) map[int64]bool {
	out := map[int64]bool{}
	rows, err := m.host.DB.Query(ctx, `SELECT e.user_id FROM plugin_mfa_email e JOIN users u ON u.id=e.user_id WHERE e.user_id = ANY($1) AND u.email <> ''`, ids)
	if err != nil {
		return out
	}
	list, _ := pgx.CollectRows(rows, pgx.RowTo[int64])
	for _, id := range list {
		out[id] = true
	}
	return out
}

func (m *Method) address(ctx context.Context, uid int64) (string, string, error) {
	var addr, lang string
	if err := m.host.DB.QueryRow(ctx, `SELECT email, language FROM users WHERE id=$1`, uid).Scan(&addr, &lang); err != nil {
		return "", "", err
	}
	if addr == "" {
		return "", "", apperr.BadRequest("add an e-mail address to your profile first")
	}
	if lang == "" {
		lang = m.host.Config.Get().I18n.DefaultLanguage
	}
	return addr, lang, nil
}

// send creates a code for purpose and e-mails it (directly, not kept in
// the outbox).
func (m *Method) send(ctx context.Context, uid int64, purpose string) (string, error) {
	if !m.host.Mail.Enabled() {
		return "", apperr.BadRequest("e-mail codes are not available (no e-mail delivery)")
	}
	addr, lang, err := m.address(ctx, uid)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	recent := []time.Time{}
	for _, t := range m.sent[uid] {
		if time.Since(t) < 15*time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= 5 {
		m.mu.Unlock()
		return "", apperr.TooMany("too many codes requested, try again later")
	}
	m.sent[uid] = append(recent, time.Now())
	m.mu.Unlock()
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	code := fmt.Sprintf("%06d", n.Int64())
	if _, err := m.host.DB.Exec(ctx, `INSERT INTO plugin_mfa_email_codes(user_id, purpose, code_hash, expires_at) VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id, purpose) DO UPDATE SET code_hash=EXCLUDED.code_hash, attempts=0, expires_at=EXCLUDED.expires_at`,
		uid, purpose, auth.HashToken(code), time.Now().Add(codeTTL)); err != nil {
		return "", err
	}
	p := map[string]string{"code": code, "site": m.host.Config.Get().UI.SiteName, "minutes": "10"}
	if err := m.host.Mail.SendNow(ctx, mail.Message{To: []string{addr}, Subject: m.host.T(ctx, lang, "mfa.mail_subject_"+purpose, p),
		Text: m.host.T(ctx, lang, "mfa.mail_text_"+purpose, p), Source: "auth", NoReport: true}); err != nil {
		m.host.Log.Error("sending the e-mail code failed", "user", uid, "err", err)
		return "", &apperr.Error{Status: 503, Code: "mail_failed", Msg: "the code could not be sent by e-mail"}
	}
	return auth.MaskEmail(addr), nil
}

// check verifies and consumes a code of purpose.
func (m *Method) check(ctx context.Context, uid int64, purpose string, answer json.RawMessage) error {
	var in struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(answer, &in)
	code := strings.TrimSpace(in.Code)
	var hash []byte
	var attempts int
	var expires time.Time
	err := m.host.DB.QueryRow(ctx, `UPDATE plugin_mfa_email_codes SET attempts = attempts + 1 WHERE user_id=$1 AND purpose=$2
		RETURNING code_hash, attempts, expires_at`, uid, purpose).Scan(&hash, &attempts, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrMFAInvalid
	}
	if err != nil {
		return err
	}
	if attempts > codeAttempts || time.Now().After(expires) || code == "" || subtle.ConstantTimeCompare(hash, auth.HashToken(code)) != 1 {
		if attempts >= codeAttempts {
			_, _ = m.host.DB.Exec(ctx, `DELETE FROM plugin_mfa_email_codes WHERE user_id=$1 AND purpose=$2`, uid, purpose)
		}
		return auth.ErrMFAInvalid
	}
	tag, err := m.host.DB.Exec(ctx, `DELETE FROM plugin_mfa_email_codes WHERE user_id=$1 AND purpose=$2 AND code_hash=$3`, uid, purpose, hash)
	if err != nil || tag.RowsAffected() != 1 {
		return auth.ErrMFAInvalid
	}
	return nil
}

// Challenge implements auth.MFAMethod: sends the sign-in code.
func (m *Method) Challenge(ctx context.Context, uid int64) (any, string, error) {
	addr, err := m.send(ctx, uid, "login")
	if err != nil {
		return nil, "", err
	}
	return map[string]string{"address": addr}, "", nil
}

// Verify implements auth.MFAMethod.
func (m *Method) Verify(ctx context.Context, uid int64, _ string, answer json.RawMessage) error {
	return m.check(ctx, uid, "login", answer)
}

// BeginEnroll implements auth.MFAEnroller: sends a code to confirm.
func (m *Method) BeginEnroll(ctx context.Context, uid int64, _ json.RawMessage) (any, string, error) {
	if m.Enrolled(ctx, uid) {
		return nil, "", apperr.Conflict("codes by e-mail are already on")
	}
	addr, err := m.send(ctx, uid, "setup")
	if err != nil {
		return nil, "", err
	}
	return map[string]string{"address": addr}, "", nil
}

// FinishEnroll implements auth.MFAEnroller.
func (m *Method) FinishEnroll(ctx context.Context, uid int64, _ string, answer json.RawMessage) error {
	if err := m.check(ctx, uid, "setup", answer); err != nil {
		return err
	}
	_, err := m.host.DB.Exec(ctx, `INSERT INTO plugin_mfa_email(user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`, uid)
	return err
}

// ResetMFA implements auth.MFAResetter.
func (m *Method) ResetMFA(ctx context.Context, uid int64) error {
	_, err := m.host.DB.Exec(ctx, `DELETE FROM plugin_mfa_email WHERE user_id=$1`, uid)
	return err
}
