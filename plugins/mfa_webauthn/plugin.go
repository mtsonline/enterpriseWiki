// Package mfa_webauthn adds security keys and passkeys (WebAuthn/FIDO2):
// as second factor after every sign-in method (see auth.MFAMethod) and,
// for passkeys, as sign-in without password (auth.PasswordlessMethod).
//
// Configuration (config.yaml -> plugin_settings."mfa.webauthn"), usually
// not needed:
//
//	rp_id: wiki.example.com            # default: host of server.base_url
//	origins: [https://wiki.example.com] # default: origin of server.base_url
//	passkeys: true                     # sign-in without password with passkeys
//
// Security: user verification is required for passkey sign-ins; a
// credential that looks cloned (sign counter went backwards) is refused;
// adding or removing a key needs a sign-in in the last 15 minutes.
package mfa_webauthn

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// Plugin is the WebAuthn plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// recentSignIn: adding or removing a key needs a fresh sign-in.
const recentSignIn = 15 * time.Minute

// Setup registers the second factor, the passkey sign-in and the endpoints
// to manage the own keys.
func (p *Plugin) Setup(host *sdk.Host) error {
	wa := &WebAuthn{pool: host.DB, cfg: host.Config, settings: host.Settings, site: host.Site}
	host.RegisterMFAMethod(wa)
	host.RegisterPasswordless(passkeys{wa})

	host.Handle("GET", "/credentials", func(w http.ResponseWriter, r *http.Request) (any, error) {
		uid, err := signedIn(r, false)
		if err != nil {
			return nil, err
		}
		return wa.list(r.Context(), uid)
	})
	host.Handle("POST", "/register/begin", func(w http.ResponseWriter, r *http.Request) (any, error) {
		uid, err := signedIn(r, true)
		if err != nil {
			return nil, err
		}
		var in struct {
			Passkey bool `json:"passkey"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return wa.beginRegistration(r.Context(), uid, in.Passkey)
	})
	host.Handle("POST", "/register/finish", func(w http.ResponseWriter, r *http.Request) (any, error) {
		uid, err := signedIn(r, true)
		if err != nil {
			return nil, err
		}
		var in struct {
			Name       string          `json:"name"`
			Credential json.RawMessage `json:"credential"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		c, err := wa.finishRegistration(r.Context(), uid, in.Name, in.Credential)
		if err != nil {
			return nil, err
		}
		return h.Created{V: c}, nil
	})
	host.Handle("DELETE", "/credentials/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		uid, err := signedIn(r, true)
		if err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, wa.remove(r.Context(), uid, id)
	})
	return nil
}

// signedIn returns the user of a browser session; recent requires a
// sign-in in the last 15 minutes. API keys and SSH tokens cannot manage keys.
func signedIn(r *http.Request, recent bool) (int64, error) {
	p := identity.From(r.Context())
	if p == nil || p.UserID == 0 {
		return 0, apperr.ErrUnauthorized
	}
	if p.Via != "session" {
		return 0, apperr.Forbidden("security keys can only be managed in the browser")
	}
	if recent && time.Since(p.SignedIn) > recentSignIn {
		return 0, &apperr.Error{Status: 403, Code: "reauth_required", Msg: "please sign in again to change your security keys"}
	}
	return p.UserID, nil
}

// WebAuthn is the second factor and the store of the credentials.
type WebAuthn struct {
	pool     *pgxpool.Pool
	cfg      *config.Store
	settings func() map[string]any
	site     string
}

// relyingParty builds the WebAuthn configuration from the settings.
func (wa *WebAuthn) relyingParty() (*webauthn.WebAuthn, error) {
	s := wa.settings()
	base, err := url.Parse(strings.TrimRight(wa.cfg.Get().Server.BaseURL, "/"))
	if err != nil || base.Host == "" {
		return nil, apperr.BadRequest("security keys need server.base_url")
	}
	origins := auth.StringList(s["origins"])
	if len(origins) == 0 {
		origins = []string{base.Scheme + "://" + base.Host}
	}
	name := wa.cfg.Get().UI.SiteName
	if name == "" {
		name = "Enterprise Wiki"
	}
	rp, err := webauthn.New(&webauthn.Config{
		RPID: auth.Setting(s, "rp_id", base.Hostname()), RPDisplayName: name, RPOrigins: origins,
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: 3 * time.Minute, TimeoutUVD: 3 * time.Minute},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute, TimeoutUVD: 5 * time.Minute},
		},
	})
	if err != nil {
		// e.g. an IP address as host: WebAuthn needs a domain name
		return nil, apperr.BadRequest("security keys are not available here: %v", err)
	}
	return rp, nil
}

// user implements webauthn.User.
type user struct {
	id          int64
	handle      []byte
	name, label string
	creds       []webauthn.Credential
}

func (u *user) WebAuthnID() []byte                         { return u.handle }
func (u *user) WebAuthnName() string                       { return u.name }
func (u *user) WebAuthnDisplayName() string                { return u.label }
func (u *user) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// loadUser reads a user with its credentials; create makes the user
// handle if there is none yet.
func (wa *WebAuthn) loadUser(ctx context.Context, uid int64, create bool) (*user, error) {
	u := &user{id: uid}
	if err := wa.pool.QueryRow(ctx, `SELECT username, display_name FROM users WHERE id=$1`, uid).Scan(&u.name, &u.label); err != nil {
		return nil, err
	}
	err := wa.pool.QueryRow(ctx, `SELECT handle FROM plugin_webauthn_users WHERE user_id=$1`, uid).Scan(&u.handle)
	if errors.Is(err, pgx.ErrNoRows) && create {
		u.handle = make([]byte, 32)
		if _, err := rand.Read(u.handle); err != nil {
			return nil, err
		}
		if _, err := wa.pool.Exec(ctx, `INSERT INTO plugin_webauthn_users(user_id, handle) VALUES ($1,$2) ON CONFLICT (user_id) DO NOTHING`, uid, u.handle); err != nil {
			return nil, err
		}
		err = wa.pool.QueryRow(ctx, `SELECT handle FROM plugin_webauthn_users WHERE user_id=$1`, uid).Scan(&u.handle)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	rows, err := wa.pool.Query(ctx, `SELECT credential FROM plugin_webauthn_credentials WHERE user_id=$1 ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	u.creds, err = pgx.CollectRows(rows, pgx.RowTo[webauthn.Credential])
	return u, err
}

// ---------------------------------------------------------------- second factor

// ID implements auth.MFAMethod.
func (wa *WebAuthn) ID() string { return "webauthn" }

// Enrolled implements auth.MFAMethod.
func (wa *WebAuthn) Enrolled(ctx context.Context, uid int64) bool {
	var n int
	_ = wa.pool.QueryRow(ctx, `SELECT count(*) FROM plugin_webauthn_credentials WHERE user_id=$1`, uid).Scan(&n)
	return n > 0
}

// Challenge implements auth.MFAMethod: the options for
// navigator.credentials.get with the user's keys.
func (wa *WebAuthn) Challenge(ctx context.Context, uid int64) (any, string, error) {
	rp, err := wa.relyingParty()
	if err != nil {
		return nil, "", err
	}
	u, err := wa.loadUser(ctx, uid, false)
	if err != nil {
		return nil, "", err
	}
	opts, sess, err := rp.BeginLogin(u, webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		return nil, "", err
	}
	state, err := json.Marshal(sess)
	return opts, string(state), err
}

// Verify implements auth.MFAMethod.
func (wa *WebAuthn) Verify(ctx context.Context, uid int64, state string, answer json.RawMessage) error {
	if state == "" {
		return apperr.BadRequest("request the security key first")
	}
	var in struct {
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.Unmarshal(answer, &in); err != nil || len(in.Credential) == 0 {
		return apperr.BadRequest("credential missing")
	}
	var sess webauthn.SessionData
	if err := json.Unmarshal([]byte(state), &sess); err != nil {
		return err
	}
	rp, err := wa.relyingParty()
	if err != nil {
		return err
	}
	u, err := wa.loadUser(ctx, uid, false)
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(in.Credential)
	if err != nil {
		return auth.ErrTOTPInvalid
	}
	cred, err := rp.ValidateLogin(u, sess, parsed)
	if err != nil {
		return auth.ErrTOTPInvalid
	}
	return wa.used(ctx, uid, cred)
}

// used stores the new state of a credential after a sign-in; a credential
// that looks cloned is refused.
func (wa *WebAuthn) used(ctx context.Context, uid int64, cred *webauthn.Credential) error {
	if cred.Authenticator.CloneWarning {
		_ = audit.Log(ctx, wa.pool, "webauthn.clone_warning", "user", uid, nil, nil)
		return &apperr.Error{Status: 401, Code: "totp_invalid", Msg: "this security key may have been copied: it was refused"}
	}
	_, err := wa.pool.Exec(ctx, `UPDATE plugin_webauthn_credentials SET credential=$3, last_used_at=now() WHERE user_id=$1 AND cred_id=$2`,
		uid, cred.ID, cred)
	return err
}

// ResetMFA implements auth.MFAResetter (administrators remove all factors).
func (wa *WebAuthn) ResetMFA(ctx context.Context, uid int64) error {
	_, err := wa.pool.Exec(ctx, `DELETE FROM plugin_webauthn_credentials WHERE user_id=$1`, uid)
	return err
}

// ---------------------------------------------------------------- passkeys

type passkeys struct{ wa *WebAuthn }

// ID implements auth.PasswordlessMethod.
func (p passkeys) ID() string { return "passkey" }

// Begin implements auth.PasswordlessMethod: options for a discoverable
// credential, user verification required.
func (p passkeys) Begin(context.Context) (any, string, error) {
	if !auth.BoolSetting(p.wa.settings(), "passkeys", true) {
		return nil, "", apperr.NotFound("passkey sign-in")
	}
	rp, err := p.wa.relyingParty()
	if err != nil {
		return nil, "", err
	}
	opts, sess, err := rp.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}
	state, err := json.Marshal(sess)
	return opts, string(state), err
}

// Finish implements auth.PasswordlessMethod.
func (p passkeys) Finish(ctx context.Context, state string, answer json.RawMessage) (int64, error) {
	var sess webauthn.SessionData
	if err := json.Unmarshal([]byte(state), &sess); err != nil {
		return 0, err
	}
	rp, err := p.wa.relyingParty()
	if err != nil {
		return 0, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(answer)
	if err != nil {
		return 0, err
	}
	var found *user
	u, cred, err := rp.ValidatePasskeyLogin(func(rawID, handle []byte) (webauthn.User, error) {
		var uid int64
		if err := p.wa.pool.QueryRow(ctx, `SELECT w.user_id FROM plugin_webauthn_users w JOIN plugin_webauthn_credentials c ON c.user_id=w.user_id
			WHERE w.handle=$1 AND c.cred_id=$2`, handle, rawID).Scan(&uid); err != nil {
			return nil, errors.New("unknown passkey")
		}
		x, err := p.wa.loadUser(ctx, uid, false)
		found = x
		return x, err
	}, sess, parsed)
	if err != nil {
		return 0, err
	}
	if found == nil || u.(*user) != found || !cred.Flags.UserVerified {
		return 0, errors.New("passkey not verified")
	}
	if err := p.wa.used(ctx, found.id, cred); err != nil {
		return 0, err
	}
	return found.id, nil
}

// ---------------------------------------------------------------- managing the own keys

// Credential is a registered key as shown to its user.
type Credential struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Passkey    bool       `json:"passkey"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func (wa *WebAuthn) list(ctx context.Context, uid int64) ([]Credential, error) {
	rows, err := wa.pool.Query(ctx, `SELECT id, name, passkey, created_at, last_used_at FROM plugin_webauthn_credentials WHERE user_id=$1 ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Credential])
}

func (wa *WebAuthn) beginRegistration(ctx context.Context, uid int64, passkey bool) (any, error) {
	rp, err := wa.relyingParty()
	if err != nil {
		return nil, err
	}
	u, err := wa.loadUser(ctx, uid, true)
	if err != nil {
		return nil, err
	}
	if len(u.creds) >= 20 {
		return nil, apperr.BadRequest("at most 20 security keys")
	}
	exclude := make([]protocol.CredentialDescriptor, len(u.creds))
	for i := range u.creds {
		exclude[i] = u.creds[i].Descriptor()
	}
	sel := protocol.AuthenticatorSelection{UserVerification: protocol.VerificationPreferred, ResidentKey: protocol.ResidentKeyRequirementDiscouraged}
	if passkey {
		sel = protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired, ResidentKey: protocol.ResidentKeyRequirementRequired}
	}
	opts, sess, err := rp.BeginRegistration(u, webauthn.WithExclusions(exclude), webauthn.WithAuthenticatorSelection(sel))
	if err != nil {
		return nil, err
	}
	if _, err := wa.pool.Exec(ctx, `INSERT INTO plugin_webauthn_registrations(user_id, passkey, session, expires_at) VALUES ($1,$2,$3, now() + interval '5 minutes')
		ON CONFLICT (user_id) DO UPDATE SET passkey=EXCLUDED.passkey, session=EXCLUDED.session, expires_at=EXCLUDED.expires_at`, uid, passkey, sess); err != nil {
		return nil, err
	}
	return opts, nil
}

func (wa *WebAuthn) finishRegistration(ctx context.Context, uid int64, name string, raw json.RawMessage) (*Credential, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return nil, apperr.BadRequest("a name (at most 100 characters) is required")
	}
	var passkey bool
	var sess webauthn.SessionData
	err := wa.pool.QueryRow(ctx, `DELETE FROM plugin_webauthn_registrations WHERE user_id=$1 AND expires_at > now() RETURNING passkey, session`, uid).
		Scan(&passkey, &sess)
	if err != nil {
		return nil, apperr.BadRequest("the registration has expired: please start again")
	}
	rp, err := wa.relyingParty()
	if err != nil {
		return nil, err
	}
	u, err := wa.loadUser(ctx, uid, false)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(raw)
	if err != nil {
		return nil, apperr.BadRequest("invalid security key response")
	}
	cred, err := rp.CreateCredential(u, sess, parsed)
	if err != nil {
		return nil, apperr.BadRequest("the security key could not be registered: %v", err)
	}
	c := &Credential{Name: name, Passkey: passkey}
	err = wa.pool.QueryRow(ctx, `INSERT INTO plugin_webauthn_credentials(user_id, cred_id, name, passkey, credential) VALUES ($1,$2,$3,$4,$5)
		RETURNING id, created_at`, uid, cred.ID, name, passkey, cred).Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		var unique interface{ SQLState() string }
		if errors.As(err, &unique) && unique.SQLState() == "23505" {
			return nil, apperr.Conflict("this security key is already registered")
		}
		return nil, err
	}
	_ = audit.Log(ctx, wa.pool, "webauthn.add", "user", uid, nil, map[string]any{"name": name, "passkey": passkey})
	return c, nil
}

func (wa *WebAuthn) remove(ctx context.Context, uid, id int64) error {
	tag, err := wa.pool.Exec(ctx, `DELETE FROM plugin_webauthn_credentials WHERE id=$1 AND user_id=$2`, id, uid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("security key")
	}
	return audit.Log(ctx, wa.pool, "webauthn.remove", "user", uid, nil, map[string]any{"id": id})
}
