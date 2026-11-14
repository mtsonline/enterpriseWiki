// Package auth implements authentication: the pluggable provider chain
// (local first), sessions, API keys, SSH key login and the management of
// users, groups and roles.
package auth

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/db"
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
	// EmailVerified: the provider confirms that the address belongs to the
	// person (OIDC email_verified, or configured for LDAP/SAML).
	EmailVerified bool
	// TrustEmail: the administrator trusts the provider's confirmation, so a
	// new sign-in with a confirmed address is linked to the account with that
	// address automatically (otherwise the user confirms with that account).
	TrustEmail bool
	// NoJIT: unknown people get no account at their first sign-in (only
	// accounts created or invited before, or linked by e-mail).
	NoJIT      bool
	Groups     []string // local group names (after Mapping.Apply)
	Roles      []string // local role names (after Mapping.Apply)
	Attributes map[string]any
	// Alias is the wiki user name the provider maps (see Mapping.AliasFrom).
	Alias string
	// SessionID and LogoutHint let the provider end the session later
	// (single logout): OIDC sid and id token, SAML SessionIndex and NameID.
	SessionID, LogoutHint string
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
	src      func() []PasswordProvider // provider instances (see instances.go)
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
	mfa       []registeredMFA
	guards    []registeredGuard
	enabled   func(pluginID string) bool

	cache   sync.Map // token hash (string) -> cachedPrincipal
	limiter *limiter

	// SeparateAccounts: the site has its own accounts (users, groups,
	// roles) - its administrators manage them, and nobody there is an
	// instance administrator (user IDs belong to the site, not the instance).
	SeparateAccounts bool

	// SendConfirmation e-mails the confirmation link of an address;
	// MailReady reports whether e-mails can be sent at all.
	SendConfirmation func(ctx context.Context, uid int64, addr, link string) error
	MailReady        func() bool
}

type cachedPrincipal struct {
	p       *identity.Principal
	expires time.Time
}

const principalCacheTTL = 30 * time.Second

// New creates the auth service.
func New(pool *pgxpool.Pool, cfg *config.Store, bus *events.Bus, log *slog.Logger) *Service {
	s := &Service{pool: pool, cfg: cfg, bus: bus, log: log, enabled: func(string) bool { return true }, limiter: newLimiter()}
	bus.Subscribe("auth", func(msg string) {
		// "session:HASH" ends one cached session (sign-out); anything else
		// (user, group, role changes) flushes all
		if x, ok := strings.CutPrefix(msg, "session:"); ok {
			if h, err := hex.DecodeString(x); err == nil {
				s.cache.Delete(string(h))
				return
			}
		}
		s.flushCache()
	})
	bus.Subscribe("*", func(string) { s.flushCache() })
	return s
}

// SetEnabledFunc wires the plugin enabled check.
func (s *Service) SetEnabledFunc(f func(pluginID string) bool) { s.enabled = f }

// RegisterProvider adds a password provider owned by pluginID.
func (s *Service) RegisterProvider(pluginID string, p PasswordProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers = append(s.providers, registered{pluginID: pluginID, p: p})
}

// RegisterProviderSource adds the password providers src returns at each
// call (one per configured instance) owned by pluginID.
func (s *Service) RegisterProviderSource(pluginID string, src func() []PasswordProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers = append(s.providers, registered{pluginID: pluginID, src: src})
}

// ProviderInfo describes a provider for the login page.
type ProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"` // password | redirect | passwordless
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
		switch {
		case r.src != nil:
			if s.enabled(r.pluginID) {
				out = append(out, r.src()...)
			}
		case r.p.ID() == "local" || s.enabled(r.pluginID):
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
	for _, id := range s.PasswordlessMethods() {
		out = append(out, ProviderInfo{id, id, "passwordless"})
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

// Limits of the sign-in: failed attempts per address and per user name in
// 5 minutes, longest user name and password.
const (
	loginFailsIP   = 30
	loginFailsUser = 20
	maxUsername    = 255
	maxPassword    = 1024
)

// Login checks factor 1 with a password against the provider chain. A
// handle "name@provider" restricts the attempt to that provider; the wiki
// user name (alias) signs in with the account's own method. If further
// factors are needed, the error is "mfa_required" with the token of the
// pending sign-in (see mfaflow.go).
func (s *Service) Login(ctx context.Context, username, password, ip, ua string) (*LoginResult, error) {
	username = strings.TrimSpace(username)
	// only failed attempts count (S-05: offices behind one address)
	failed := func() (*LoginResult, error) {
		s.limiter.allow("ip:"+ip, loginFailsIP, 5*time.Minute)
		if len(username) <= maxUsername {
			s.limiter.allow("user:"+strings.ToLower(username), loginFailsUser, 5*time.Minute)
		}
		return nil, apperr.ErrInvalidLogin
	}
	if s.limiter.exceeded("ip:"+ip, loginFailsIP, 5*time.Minute) ||
		(len(username) <= maxUsername && s.limiter.exceeded("user:"+strings.ToLower(username), loginFailsUser, 5*time.Minute)) {
		return nil, apperr.TooMany("too many login attempts, try again later")
	}
	// S-03: never process (or log) oversized names and passwords
	if username == "" || password == "" || len(username) > maxUsername || len(password) > maxPassword {
		return failed()
	}
	providers := s.Providers()
	// the wiki user name (alias): signs in with the account's own method;
	// a local account with that user name always wins
	if !strings.Contains(username, "@") {
		var local bool
		_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE provider='local' AND lower(username)=lower($1))`, username).Scan(&local)
		if _, prov, uname, ok := s.userByAlias(ctx, username); ok && !local {
			providers = slices.DeleteFunc(slices.Clone(providers), func(p PasswordProvider) bool { return p.ID() != prov })
			username = uname
		}
	}
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
		if err := s.checkSignIn(ctx, uid, ip); err != nil {
			return nil, err
		}
		res, err := s.afterFirstFactor(ctx, uid, ext.Provider, ext, "", ip, ua, false)
		if err != nil {
			return nil, err
		}
		if res.MFAToken != "" {
			return nil, errMFARequired(res.MFAToken)
		}
		s.log.Info("login", "user", res.Principal.Handle(), "ip", ip)
		return res.LoginResult, nil
	}
	return failed()
}

// syncIdentity creates or updates the local user record of an identity and
// returns its id. Local identities already exist.
func (s *Service) syncIdentity(ctx context.Context, ext *ExternalIdentity) (int64, error) {
	if ext.Provider != "local" {
		// a further sign-in linked to an account, or a new one that is
		// linked now (same confirmed e-mail address) - see link.go
		uid, linked, err := s.linkedIdentity(ctx, ext)
		if err != nil {
			return 0, err
		}
		if linked {
			s.syncAlias(ctx, uid, ext)
			return uid, s.syncMemberships(ctx, uid, ext)
		}
	}
	var id int64
	var disabled bool
	err := s.pool.QueryRow(ctx, `INSERT INTO users(username, provider, subject, display_name, email, attributes, last_login_at)
		VALUES ($1,$2,$3,$4,$5,COALESCE($6::jsonb,'{}'),now())
		ON CONFLICT (provider, subject) DO UPDATE SET
			-- accounts provisioned by SCIM keep the directory's name and address
			username = CASE WHEN users.scim_client IS NULL THEN EXCLUDED.username ELSE users.username END,
			display_name = CASE WHEN EXCLUDED.display_name <> '' AND users.scim_client IS NULL THEN EXCLUDED.display_name ELSE users.display_name END,
			email = CASE WHEN EXCLUDED.email <> '' AND users.scim_client IS NULL THEN EXCLUDED.email ELSE users.email END,
			attributes = CASE WHEN $6::jsonb IS NULL THEN users.attributes ELSE EXCLUDED.attributes END,
			last_login_at = now(), updated_at = now()
		RETURNING id, disabled`,
		ext.Username, ext.Provider, ext.Subject, ext.DisplayName, ext.Email, nullableJSON(ext.Attributes)).Scan(&id, &disabled)
	if db.IsUniqueViolation(err) {
		// another account of this provider has the name, for example a person
		// deleted and created again in the directory (new id), or a name given
		// to a new person; merging them could hand one person's pages and
		// rights to another, so an administrator decides
		s.log.Warn("sign-in refused: user name belongs to another account of the provider", "provider", ext.Provider, "user", ext.Username)
		return 0, apperr.Conflict("the user name %q already belongs to another account of this sign-in; please ask an administrator", ext.Username)
	}
	if err != nil {
		return 0, err
	}
	if disabled {
		return 0, apperr.ErrInvalidLogin
	}
	if ext.Provider != "local" {
		// the address of the provider; confirmed only if the provider is trusted
		if _, err := adoptEmail(ctx, s.pool, id, ext.Email, ext.TrustEmail && ext.EmailVerified); err != nil {
			s.log.Warn("e-mail of identity provider not taken over", "provider", ext.Provider, "err", err)
		}
	}
	if ext.Provider != "local" {
		s.syncAlias(ctx, id, ext)
	}
	return id, s.syncMemberships(ctx, id, ext)
}

// syncMemberships replaces the groups and roles a provider assigns.
func (s *Service) syncMemberships(ctx context.Context, id int64, ext *ExternalIdentity) error {
	if ext.Provider != "local" && ext.Groups != nil {
		if err := s.syncProviderGroups(ctx, id, ext.Provider, ext.Groups); err != nil {
			return err
		}
	}
	if ext.Provider != "local" && ext.Roles != nil {
		if err := s.syncProviderRoles(ctx, id, ext.Provider, ext.Roles); err != nil {
			return err
		}
	}
	if ext.Provider != "local" {
		s.bump(ctx) // P-01: a local sign-in changes no memberships
	}
	return nil
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

func (s *Service) createSession(ctx context.Context, uid int64, kind string, ttl time.Duration, ip, ua string, sso *ExternalIdentity) (string, time.Time, error) {
	tok := NewToken("")
	exp := time.Now().Add(ttl)
	if len(ua) > 300 {
		ua = ua[:300]
	}
	var prov, subj, sid, hint string
	if sso != nil && sso.Provider != "local" {
		prov, subj, sid, hint = sso.Provider, sso.Subject, sso.SessionID, sso.LogoutHint
		if len(hint) > 16<<10 {
			hint = ""
		}
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions(token_hash, user_id, kind, expires_at, ip, user_agent, sso_provider, sso_subject, sso_session, sso_hint)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, HashToken(tok), uid, kind, exp, ip, ua, prov, subj, sid, hint)
	return tok, exp, err
}

func (s *Service) flushCache() {
	s.cache.Range(func(k, _ any) bool { s.cache.Delete(k); return true })
}

type backgroundKey struct{}

// WithBackground marks a request made automatically by the browser
// (polling): it does not keep an idle session alive.
func WithBackground(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundKey{}, true)
}

// Background reports whether ctx belongs to an automatic request.
func Background(ctx context.Context) bool { b, _ := ctx.Value(backgroundKey{}).(bool); return b }

// Authenticate resolves a session token, SSH token or API key ("ewk_...").
func (s *Service) Authenticate(ctx context.Context, token string) (*identity.Principal, error) {
	if token == "" || len(token) > 200 {
		return nil, apperr.ErrUnauthorized
	}
	h := HashToken(token)
	if c, ok := s.cache.Load(string(h)); ok {
		cp := c.(cachedPrincipal)
		if time.Now().Before(cp.expires) {
			if err := s.checkRequest(ctx, cp.p); err != nil {
				return nil, err
			}
			return cp.p, nil
		}
		s.cache.Delete(string(h))
	}
	// P-01: one statement per sign-in that is not cached - the session or
	// key, the account, its groups, roles and the default permissions
	var p *identity.Principal
	var err error
	if strings.HasPrefix(token, "ewk_") {
		var catPtr *int64
		var rate *int
		key := &identity.Principal{}
		p, err = s.scanPrincipal(s.pool.QueryRow(ctx, `WITH k AS (SELECT id, user_id, permissions, category_id, allow_admin, rate_per_second, rate_own FROM api_keys
				WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at > now()),
			touch AS (UPDATE api_keys SET last_used_at=now() WHERE id=(SELECT id FROM k) AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes'))
			SELECT `+s.principalCols()+`, k.id, k.permissions, k.category_id, k.allow_admin, k.rate_per_second, k.rate_own
			FROM k JOIN users u ON u.id=k.user_id`, h), &key.KeyID, &key.KeyPerms, &catPtr, &key.KeyAllowAdmin, &rate, &key.KeyRateOwn)
		if err != nil {
			return nil, apperr.ErrUnauthorized
		}
		p.Via, p.KeyID, p.KeyPerms, p.KeyAllowAdmin, p.KeyRateOwn = "apikey", key.KeyID, key.KeyPerms, key.KeyAllowAdmin, key.KeyRateOwn
		if catPtr != nil {
			p.KeyCategory = *catPtr
		}
		if rate != nil {
			p.KeyRate = *rate
		}
	} else {
		var kind string
		var created time.Time
		// idle sessions end (browser sessions only; SSH tokens are short-lived);
		// automatic requests (Background) do not count as activity. The
		// last activity is written at most once per minute (no write per
		// request): a session can end up to a minute before the idle time.
		idle := config.Duration(s.cfg.Get().Auth.SessionIdle, 12*time.Hour) // "0" switches it off
		p, err = s.scanPrincipal(s.pool.QueryRow(ctx, `WITH s AS (SELECT user_id, kind, created_at FROM sessions
				WHERE token_hash=$1 AND expires_at > now() AND (kind <> 'web' OR $2::float8 <= 0 OR last_seen_at > now() - make_interval(secs => $2))),
			touch AS (UPDATE sessions SET last_seen_at=now() WHERE token_hash=$1 AND NOT $3 AND last_seen_at < now() - interval '1 minute' AND EXISTS(SELECT 1 FROM s))
			SELECT `+s.principalCols()+`, s.kind, s.created_at FROM s JOIN users u ON u.id=s.user_id`, h, idle.Seconds(), Background(ctx)), &kind, &created)
		if err != nil {
			return nil, apperr.ErrUnauthorized
		}
		p.Via, p.SignedIn = "session", created
		if kind == "ssh" {
			p.Via = "ssh"
		}
	}
	s.cache.Store(string(h), cachedPrincipal{p, time.Now().Add(principalCacheTTL)})
	if err := s.checkRequest(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// LoadPrincipal loads a user with its groups and roles.
func (s *Service) LoadPrincipal(ctx context.Context, uid int64) (*identity.Principal, error) {
	return s.LoadPrincipalQ(ctx, s.pool, uid)
}

// LoadPrincipalQ loads a user with q: inside a transaction pass the
// transaction, never take a second connection of the pool while one is
// held (all connections could wait on each other).
func (s *Service) LoadPrincipalQ(ctx context.Context, q db.Querier, uid int64) (*identity.Principal, error) {
	p, err := s.scanPrincipal(q.QueryRow(ctx, `SELECT `+s.principalCols()+` FROM users u WHERE u.id=$1`, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("user")
	}
	return p, err
}

// roleSet are the roles of the account u (own ones and those of its groups).
const roleSet = `(SELECT role_id FROM user_roles WHERE user_id=u.id
	UNION SELECT gr.role_id FROM group_roles gr JOIN user_groups ug ON ug.group_id=gr.group_id WHERE ug.user_id=u.id)`

// principalCols are the columns of an account u with its groups, roles and
// permissions (see scanPrincipal).
func (s *Service) principalCols() string {
	instance := `false`
	if !s.SeparateAccounts { // IDs of own accounts must never be looked up in the instance tables
		instance = `(EXISTS(SELECT 1 FROM public.instance_admins WHERE user_id=u.id)
		OR EXISTS(SELECT 1 FROM public.roles r WHERE r.is_admin AND (
			r.id IN (SELECT role_id FROM site_main.user_roles WHERE user_id=u.id) OR
			r.id IN (SELECT gr.role_id FROM site_main.group_roles gr JOIN public.user_groups ug ON ug.group_id=gr.group_id WHERE ug.user_id=u.id))))`
	}
	return `u.id, u.username, COALESCE(u.alias, ''), u.alias_source, u.provider, u.display_name, u.email, u.language, u.disabled, u.prefs,
		ARRAY(SELECT group_id FROM user_groups WHERE user_id=u.id),
		ARRAY(SELECT r.id FROM roles r WHERE r.id IN ` + roleSet + `),
		COALESCE((SELECT bool_or(r.is_admin) FROM roles r WHERE r.id IN ` + roleSet + `), false),
		ARRAY(SELECT DISTINCT x FROM roles r, unnest(r.permissions) x WHERE r.id IN ` + roleSet + `),
		` + instance + `,
		EXISTS(SELECT 1 FROM permission_defaults WHERE subject='authenticated'),
		COALESCE((SELECT permissions FROM permission_defaults WHERE subject='authenticated'), '{}')`
}

// scanPrincipal reads a row of principalCols followed by more columns.
// Disabled accounts are refused.
func (s *Service) scanPrincipal(row pgx.Row, more ...any) (*identity.Principal, error) {
	p := &identity.Principal{}
	var disabled, hasDefaults bool
	var perms, defaults []string
	dst := append([]any{&p.UserID, &p.Username, &p.Alias, &p.AliasSource, &p.Provider, &p.DisplayName, &p.Email, &p.Language, &disabled, &p.Prefs,
		&p.Groups, &p.Roles, &p.IsAdmin, &perms, &p.InstanceAdmin, &hasDefaults, &defaults}, more...)
	if err := row.Scan(dst...); err != nil {
		return nil, err
	}
	if disabled {
		return nil, apperr.ErrUnauthorized
	}
	p.IsAdmin = p.IsAdmin || p.InstanceAdmin
	if !hasDefaults {
		defaults = perm.DefaultAuthenticated()
	}
	set := map[string]bool{}
	for _, k := range append(perms, defaults...) {
		if !set[k] {
			set[k] = true
			p.Perms = append(p.Perms, k)
		}
	}
	p.Perms = perm.Expand(p.Perms)
	return p, nil
}

// DefaultPermissions returns the global permissions of every signed-in user
// (Administration → Roles; without a setting the declared defaults).
func (s *Service) DefaultPermissions(ctx context.Context) ([]string, error) {
	return defaultPermissions(ctx, s.pool)
}

func defaultPermissions(ctx context.Context, q db.Querier) ([]string, error) {
	var out []string
	err := q.QueryRow(ctx, `SELECT permissions FROM permission_defaults WHERE subject='authenticated'`).Scan(&out)
	if errors.Is(err, pgx.ErrNoRows) {
		return perm.DefaultAuthenticated(), nil
	}
	return out, err
}

// ---------------------------------------------------------------- local provider

// LocalProvider authenticates users stored in the wiki database. It is always
// evaluated first so local (break-glass) admins can log in when external
// identity providers fail.
type LocalProvider struct {
	s        *Service
	settings func() map[string]any
}

// NewLocalProvider returns the local provider; settings are those of the
// plugin auth.local (factor chain: mfa_methods, mfa_required).
func (s *Service) NewLocalProvider(settings func() map[string]any) *LocalProvider {
	if settings == nil {
		settings = func() map[string]any { return nil }
	}
	return &LocalProvider{s, settings}
}

// Settings implements SettingsProvider.
func (l *LocalProvider) Settings() map[string]any { return l.settings() }

func (l *LocalProvider) ID() string   { return "local" }
func (l *LocalProvider) Name() string { return "Local account" }

// Authenticate verifies a local password with lockout protection.
func (l *LocalProvider) Authenticate(ctx context.Context, username, password string) (*ExternalIdentity, error) {
	var id int64
	var hash *string
	var locked *time.Time
	var disabled bool
	var ext ExternalIdentity
	// the user name, or a confirmed e-mail address of a local account
	err := l.s.pool.QueryRow(ctx, `SELECT id, password_hash, locked_until, disabled, subject, display_name, email FROM users
		WHERE provider='local' AND (lower(username)=lower($1)
			OR id = (SELECT user_id FROM user_emails WHERE email=lower($1) AND verified))
		ORDER BY lower(username)=lower($1) DESC LIMIT 1`, username).Scan(&id, &hash, &locked, &disabled, &ext.Subject, &ext.DisplayName, &ext.Email)
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

// exceeded reports whether key has max hits in window, without counting.
func (l *limiter) exceeded(key string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, t := range l.hits[key] {
		if time.Since(t) <= window {
			n++
		}
	}
	return n >= max
}

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
	if _, err := s.pool.Exec(ctx, `DELETE FROM mfa_pending WHERE expires_at < now()`); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM passwordless_pending WHERE expires_at < now()`); err != nil {
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
			if err := s.checkSignIn(ctx, uid, ip); err != nil {
				return nil, err
			}
			tok, exp, err := s.createSession(ctx, uid, "ssh", config.Duration(s.cfg.Get().Auth.SSHTokenTTL, time.Hour), ip, ua, nil)
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
