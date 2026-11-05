// Package auth implements authentication: the pluggable provider chain
// (local first), sessions, API keys, SSH key login and the management of
// users, groups and roles.
package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/events"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// ErrUnknownUser is returned by a provider that does not know the user; the
// next provider in the chain is tried.
var ErrUnknownUser = errors.New("unknown user")

// ErrBadCredentials is returned by a provider that knows the user but the
// credentials are wrong.
var ErrBadCredentials = errors.New("bad credentials")

// ExternalIdentity is what a provider returns after successful authentication.
type ExternalIdentity struct {
	Provider    string
	Subject     string // stable id (e.g. LDAP DN / entryUUID, OIDC sub)
	Username    string
	DisplayName string
	Email       string
	Groups      []string // local group names (after Mapping.Apply)
	Roles       []string // local role names (after Mapping.Apply)
	Attributes  map[string]any
}

// PasswordProvider authenticates username/password credentials.
type PasswordProvider interface {
	ID() string   // short id used in name@id handles, e.g. "local", "ldap"
	Name() string // human readable
	Authenticate(ctx context.Context, username, password string) (*ExternalIdentity, error)
}

type registered struct {
	pluginID string
	p        PasswordProvider
}

// Service is the authentication service.
type Service struct {
	pool      *pgxpool.Pool
	cfg       *config.Store
	bus       *events.Bus
	log       *slog.Logger
	mu        sync.RWMutex
	providers []registered
	redirects []registeredRedirect
	enabled   func(pluginID string) bool

	cache   sync.Map // token hash (string) -> cachedPrincipal
	limiter *limiter
}

type cachedPrincipal struct {
	p       *identity.Principal
	expires time.Time
}

const principalCacheTTL = 30 * time.Second

// New creates the auth service.
func New(pool *pgxpool.Pool, cfg *config.Store, bus *events.Bus, log *slog.Logger) *Service {
	s := &Service{pool: pool, cfg: cfg, bus: bus, log: log, enabled: func(string) bool { return true }, limiter: newLimiter()}
	bus.Subscribe("auth", func(string) { s.flushCache() })
	bus.Subscribe("*", func(string) { s.flushCache() })
	return s
}

// SetEnabledFunc wires the plugin enabled check.
func (s *Service) SetEnabledFunc(f func(pluginID string) bool) { s.enabled = f }

// RegisterProvider adds a password provider owned by pluginID.
func (s *Service) RegisterProvider(pluginID string, p PasswordProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers = append(s.providers, registered{pluginID, p})
}

// ProviderInfo describes a provider for the login page.
type ProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"` // password | redirect
}

// Providers returns the active providers in evaluation order (local first).
func (s *Service) Providers() []PasswordProvider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	order := s.cfg.Get().Auth.Order
	rank := func(id string) int {
		if id == "local" {
			return -1
		}
		for i, o := range order {
			if o == id {
				return i
			}
		}
		return len(order)
	}
	var out []PasswordProvider
	for _, r := range s.providers {
		if r.p.ID() == "local" || s.enabled(r.pluginID) {
			out = append(out, r.p)
		}
	}
	// stable insertion sort by rank
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && rank(out[j].ID()) < rank(out[j-1].ID()); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// ProviderInfos lists active providers.
func (s *Service) ProviderInfos() []ProviderInfo {
	out := []ProviderInfo{}
	for _, p := range s.Providers() {
		if configured(p) {
			out = append(out, ProviderInfo{p.ID(), p.Name(), "password"})
		}
	}
	for _, p := range s.RedirectProviders() {
		if configured(p) {
			out = append(out, ProviderInfo{p.ID(), p.Name(), "redirect"})
		}
	}
	return out
}

// Configurable is implemented by providers that need settings before they
// can be offered on the login page.
type Configurable interface {
	Configured() bool
}

func configured(p any) bool {
	c, ok := p.(Configurable)
	return !ok || c.Configured()
}

// LoginResult is returned by Login.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	Principal *identity.Principal
}

// Login authenticates against the provider chain and creates a session.
// A handle "name@provider" restricts the attempt to that provider.
// totp is the two-factor code (only needed for local accounts with 2FA).
func (s *Service) Login(ctx context.Context, username, password, totp, ip, ua string) (*LoginResult, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, apperr.ErrInvalidLogin
	}
	if !s.limiter.allow("ip:"+ip, 30, 5*time.Minute) || !s.limiter.allow("user:"+strings.ToLower(username), 20, 5*time.Minute) {
		return nil, apperr.TooMany("too many login attempts, try again later")
	}
	providers := s.Providers()
	if i := strings.LastIndex(username, "@"); i > 0 {
		forced := username[i+1:]
		for _, p := range providers {
			if p.ID() == forced {
				providers = []PasswordProvider{p}
				username = username[:i]
				break
			}
		}
	}
	for _, p := range providers {
		ext, err := p.Authenticate(ctx, username, password)
		if errors.Is(err, ErrUnknownUser) || errors.Is(err, ErrBadCredentials) {
			continue
		}
		if err != nil {
			var ae *apperr.Error
			if errors.As(err, &ae) {
				return nil, err
			}
			s.log.Warn("auth provider error", "provider", p.ID(), "err", err)
			continue
		}
		uid, err := s.syncIdentity(ctx, ext)
		if err != nil {
			return nil, err
		}
		if ext.Provider == "local" {
			if err := s.checkTOTP(ctx, uid, totp); err != nil {
				return nil, err
			}
		}
		tok, exp, err := s.createSession(ctx, uid, "web", config.Duration(s.cfg.Get().Auth.SessionTTL, 7*24*time.Hour), ip, ua)
		if err != nil {
			return nil, err
		}
		pr, err := s.LoadPrincipal(ctx, uid)
		if err != nil {
			return nil, err
		}
		pr.Via = "session"
		s.log.Info("login", "user", pr.Handle(), "ip", ip)
		return &LoginResult{tok, exp, pr}, nil
	}
	return nil, apperr.ErrInvalidLogin
}

// syncIdentity creates or updates the local user record of an identity and
// returns its id. Local identities already exist.
func (s *Service) syncIdentity(ctx context.Context, ext *ExternalIdentity) (int64, error) {
	var id int64
	var disabled bool
	err := s.pool.QueryRow(ctx, `INSERT INTO users(username, provider, subject, display_name, email, attributes, last_login_at)
		VALUES ($1,$2,$3,$4,$5,COALESCE($6::jsonb,'{}'),now())
		ON CONFLICT (provider, subject) DO UPDATE SET
			username = EXCLUDED.username,
			display_name = CASE WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name ELSE users.display_name END,
			email = CASE WHEN EXCLUDED.email <> '' THEN EXCLUDED.email ELSE users.email END,
			attributes = CASE WHEN $6::jsonb IS NULL THEN users.attributes ELSE EXCLUDED.attributes END,
			last_login_at = now(), updated_at = now()
		RETURNING id, disabled`,
		ext.Username, ext.Provider, ext.Subject, ext.DisplayName, ext.Email, nullableJSON(ext.Attributes)).Scan(&id, &disabled)
	if err != nil {
		return 0, err
	}
	if disabled {
		return 0, apperr.ErrInvalidLogin
	}
	if ext.Provider != "local" && ext.Groups != nil {
		if err := s.syncProviderGroups(ctx, id, ext.Provider, ext.Groups); err != nil {
			return 0, err
		}
	}
	if ext.Provider != "local" && ext.Roles != nil {
		if err := s.syncProviderRoles(ctx, id, ext.Provider, ext.Roles); err != nil {
			return 0, err
		}
	}
	s.bump(ctx)
	return id, nil
}

func (s *Service) syncProviderGroups(ctx context.Context, uid int64, provider string, names []string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM user_groups WHERE user_id=$1 AND source=$2`, uid, provider); err != nil {
			return err
		}
		for _, n := range names {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			var gid int64
			if err := tx.QueryRow(ctx, `INSERT INTO groups(name, source) VALUES ($1,$2)
				ON CONFLICT (name) DO UPDATE SET name=EXCLUDED.name RETURNING id`, n, provider).Scan(&gid); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO user_groups(user_id, group_id, source) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, uid, gid, provider); err != nil {
				return err
			}
		}
		return nil
	})
}

// syncProviderRoles replaces the provider-assigned roles of a user. Roles
// must exist locally; unknown role names are logged and skipped.
func (s *Service) syncProviderRoles(ctx context.Context, uid int64, provider string, names []string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1 AND source=$2`, uid, provider); err != nil {
			return err
		}
		for _, n := range names {
			var rid int64
			if err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE lower(name)=lower($1)`, n).Scan(&rid); err != nil {
				s.log.Warn("role mapping: unknown local role", "role", n, "provider", provider)
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id, role_id, source) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, uid, rid, provider); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) createSession(ctx context.Context, uid int64, kind string, ttl time.Duration, ip, ua string) (string, time.Time, error) {
	tok := NewToken("")
	exp := time.Now().Add(ttl)
	if len(ua) > 300 {
		ua = ua[:300]
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions(token_hash, user_id, kind, expires_at, ip, user_agent) VALUES ($1,$2,$3,$4,$5,$6)`,
		HashToken(tok), uid, kind, exp, ip, ua)
	return tok, exp, err
}

// Logout deletes the session of token.
func (s *Service) Logout(ctx context.Context, token string) error {
	h := HashToken(token)
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, h)
	s.cache.Delete(string(h))
	s.bus.Publish(ctx, "auth", "")
	return err
}

func (s *Service) flushCache() {
	s.cache.Range(func(k, _ any) bool { s.cache.Delete(k); return true })
}

// Authenticate resolves a session token, SSH token or API key ("ewk_...").
func (s *Service) Authenticate(ctx context.Context, token string) (*identity.Principal, error) {
	if token == "" || len(token) > 200 {
		return nil, apperr.ErrUnauthorized
	}
	h := HashToken(token)
	if c, ok := s.cache.Load(string(h)); ok {
		cp := c.(cachedPrincipal)
		if time.Now().Before(cp.expires) {
			return cp.p, nil
		}
		s.cache.Delete(string(h))
	}
	var p *identity.Principal
	if strings.HasPrefix(token, "ewk_") {
		var uid, cat int64
		var perms int
		var allowAdmin bool
		var catPtr *int64
		var keyID int64
		err := s.pool.QueryRow(ctx, `SELECT id, user_id, permissions, category_id, allow_admin FROM api_keys
			WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at > now()`, h).Scan(&keyID, &uid, &perms, &catPtr, &allowAdmin)
		if err != nil {
			return nil, apperr.ErrUnauthorized
		}
		if catPtr != nil {
			cat = *catPtr
		}
		p, err = s.LoadPrincipal(ctx, uid)
		if err != nil {
			return nil, apperr.ErrUnauthorized
		}
		p.Via, p.KeyPerms, p.KeyCategory, p.KeyAllowAdmin = "apikey", perms, cat, allowAdmin
		_, _ = s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=now() WHERE id=$1 AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes')`, keyID)
	} else {
		var uid int64
		var kind string
		err := s.pool.QueryRow(ctx, `UPDATE sessions SET last_seen_at = now()
			WHERE token_hash=$1 AND expires_at > now() RETURNING user_id, kind`, h).Scan(&uid, &kind)
		if err != nil {
			return nil, apperr.ErrUnauthorized
		}
		p, err = s.LoadPrincipal(ctx, uid)
		if err != nil {
			return nil, apperr.ErrUnauthorized
		}
		p.Via = "session"
		if kind == "ssh" {
			p.Via = "ssh"
		}
	}
	s.cache.Store(string(h), cachedPrincipal{p, time.Now().Add(principalCacheTTL)})
	return p, nil
}

// LoadPrincipal loads a user with its groups and roles.
func (s *Service) LoadPrincipal(ctx context.Context, uid int64) (*identity.Principal, error) {
	p := &identity.Principal{UserID: uid}
	var disabled bool
	err := s.pool.QueryRow(ctx, `SELECT username, provider, display_name, email, language, disabled FROM users WHERE id=$1`, uid).
		Scan(&p.Username, &p.Provider, &p.DisplayName, &p.Email, &p.Language, &disabled)
	if err != nil {
		return nil, apperr.NotFound("user")
	}
	if disabled {
		return nil, apperr.ErrUnauthorized
	}
	rows, err := s.pool.Query(ctx, `SELECT group_id FROM user_groups WHERE user_id=$1`, uid)
	if err != nil {
		return nil, err
	}
	p.Groups, err = pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT r.id, r.is_admin, r.permissions FROM roles r WHERE r.id IN (
			SELECT role_id FROM user_roles WHERE user_id=$1
			UNION SELECT gr.role_id FROM group_roles gr JOIN user_groups ug ON ug.group_id=gr.group_id WHERE ug.user_id=$1)`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var id int64
		var admin bool
		var perms []string
		if err := rows.Scan(&id, &admin, &perms); err != nil {
			return nil, err
		}
		p.Roles = append(p.Roles, id)
		p.IsAdmin = p.IsAdmin || admin
		for _, k := range perms {
			set[k] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.instance_admins WHERE user_id=$1)
		OR EXISTS(SELECT 1 FROM public.roles r WHERE r.is_admin AND (
			r.id IN (SELECT role_id FROM site_main.user_roles WHERE user_id=$1) OR
			r.id IN (SELECT gr.role_id FROM site_main.group_roles gr JOIN public.user_groups ug ON ug.group_id=gr.group_id WHERE ug.user_id=$1)))`, uid).
		Scan(&p.InstanceAdmin); err != nil {
		return nil, err
	}
	p.IsAdmin = p.IsAdmin || p.InstanceAdmin
	defaults, err := s.DefaultPermissions(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range defaults {
		set[k] = true
	}
	for k := range set {
		p.Perms = append(p.Perms, k)
	}
	p.Perms = perm.Expand(p.Perms)
	return p, nil
}

// DefaultPermissions returns the global permissions of every signed-in user
// (Administration → Roles; without a setting the declared defaults).
func (s *Service) DefaultPermissions(ctx context.Context) ([]string, error) {
	var out []string
	err := s.pool.QueryRow(ctx, `SELECT permissions FROM permission_defaults WHERE subject='authenticated'`).Scan(&out)
	if errors.Is(err, pgx.ErrNoRows) {
		return perm.DefaultAuthenticated(), nil
	}
	return out, err
}

// ---------------------------------------------------------------- local provider

// LocalProvider authenticates users stored in the wiki database. It is always
// evaluated first so local (break-glass) admins can log in when external
// identity providers fail.
type LocalProvider struct{ s *Service }

// NewLocalProvider returns the local provider.
func (s *Service) NewLocalProvider() *LocalProvider { return &LocalProvider{s} }

func (l *LocalProvider) ID() string   { return "local" }
func (l *LocalProvider) Name() string { return "Local account" }

// Authenticate verifies a local password with lockout protection.
func (l *LocalProvider) Authenticate(ctx context.Context, username, password string) (*ExternalIdentity, error) {
	var id int64
	var hash *string
	var locked *time.Time
	var disabled bool
	var ext ExternalIdentity
	err := l.s.pool.QueryRow(ctx, `SELECT id, password_hash, locked_until, disabled, subject, display_name, email FROM users
		WHERE provider='local' AND lower(username)=lower($1)`, username).Scan(&id, &hash, &locked, &disabled, &ext.Subject, &ext.DisplayName, &ext.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		VerifyPassword(dummyHash, password)
		return nil, ErrUnknownUser
	}
	if err != nil {
		return nil, err
	}
	if disabled || hash == nil {
		VerifyPassword(dummyHash, password)
		return nil, ErrBadCredentials
	}
	if locked != nil && locked.After(time.Now()) {
		return nil, apperr.TooMany("account temporarily locked after repeated failed logins")
	}
	if !VerifyPassword(*hash, password) {
		cfg := l.s.cfg.Get().Auth
		_, _ = l.s.pool.Exec(ctx, `UPDATE users SET failed_logins = failed_logins + 1,
			locked_until = CASE WHEN failed_logins + 1 >= $2 THEN now() + make_interval(secs => $3) ELSE locked_until END
			WHERE id=$1`, id, cfg.MaxLoginFails, config.Duration(cfg.LockoutDuration, 15*time.Minute).Seconds())
		return nil, ErrBadCredentials
	}
	_, _ = l.s.pool.Exec(ctx, `UPDATE users SET failed_logins=0, locked_until=NULL WHERE id=$1`, id)
	ext.Provider = "local"
	var uname string
	_ = l.s.pool.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, id).Scan(&uname)
	ext.Username = uname
	return &ext, nil
}

// ---------------------------------------------------------------- helpers

func nullableJSON(m map[string]any) any {
	if m == nil {
		return nil
	}
	return m
}

type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLimiter() *limiter { return &limiter{hits: map[string][]time.Time{}} }

func (l *limiter) allow(key string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	h := l.hits[key]
	i := 0
	for ; i < len(h) && now.Sub(h[i]) > window; i++ {
	}
	h = h[i:]
	if len(l.hits) > 100000 { // memory guard
		l.hits = map[string][]time.Time{}
	}
	if len(h) >= max {
		l.hits[key] = h
		return false
	}
	l.hits[key] = append(h, now)
	return true
}

// Cleanup removes expired sessions and challenges.
func (s *Service) Cleanup(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM auth_challenges WHERE expires_at < now()`); err != nil {
		return err
	}
	return s.CleanupSSO(ctx)
}

// ---------------------------------------------------------------- SSH key login

// SSHChallenge creates a single-use challenge for username.
func (s *Service) SSHChallenge(ctx context.Context, username string) (string, error) {
	if !s.limiter.allow("sshc:"+strings.ToLower(username), 30, 5*time.Minute) {
		return "", apperr.TooMany("too many requests")
	}
	nonce := "ewiki-ssh-" + base64.RawURLEncoding.EncodeToString(HashToken(NewToken("")))[:32]
	var uid int64
	err := s.pool.QueryRow(ctx, `SELECT u.id FROM users u WHERE lower(u.username)=lower($1) AND NOT u.disabled
		AND EXISTS (SELECT 1 FROM ssh_keys k WHERE k.user_id=u.id) ORDER BY (u.provider='local') DESC LIMIT 1`, username).Scan(&uid)
	if err != nil {
		// Do not reveal whether the user exists: return a challenge that can never verify.
		return nonce, nil
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO auth_challenges(nonce, user_id, expires_at) VALUES ($1,$2, now() + interval '2 minutes')`, nonce, uid)
	return nonce, err
}

// SSHLogin verifies a signed challenge and returns a short-lived bearer token.
func (s *Service) SSHLogin(ctx context.Context, challenge, signature, ip, ua string) (*LoginResult, error) {
	var uid int64
	err := s.pool.QueryRow(ctx, `DELETE FROM auth_challenges WHERE nonce=$1 AND expires_at > now() RETURNING user_id`, challenge).Scan(&uid)
	if err != nil {
		return nil, apperr.ErrInvalidLogin
	}
	rows, err := s.pool.Query(ctx, `SELECT id, public_key FROM ssh_keys WHERE user_id=$1`, uid)
	if err != nil {
		return nil, err
	}
	type k struct {
		id  int64
		key string
	}
	keys, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (k, error) {
		var x k
		return x, r.Scan(&x.id, &x.key)
	})
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		pk, _, err := ParseAuthorizedKey(key.key)
		if err != nil {
			continue
		}
		if VerifySSHSignature(pk, signature, SSHNamespace, []byte(challenge)) == nil {
			_, _ = s.pool.Exec(ctx, `UPDATE ssh_keys SET last_used_at=now() WHERE id=$1`, key.id)
			tok, exp, err := s.createSession(ctx, uid, "ssh", config.Duration(s.cfg.Get().Auth.SSHTokenTTL, time.Hour), ip, ua)
			if err != nil {
				return nil, err
			}
			p, err := s.LoadPrincipal(ctx, uid)
			if err != nil {
				return nil, err
			}
			p.Via = "ssh"
			return &LoginResult{tok, exp, p}, nil
		}
	}
	return nil, apperr.ErrInvalidLogin
}

// ensure fmt is used
var _ = fmt.Sprintf
