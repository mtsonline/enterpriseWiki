package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Second factors for every sign-in: after the sign-in method (password,
// LDAP, OpenID Connect, SAML) has confirmed the person, a person who set up
// a second factor gets a short-lived pending sign-in instead of a session
// ("mfa_required" with a token). One of their factors completes it.
// Factors are MFA methods: built in are the authenticator app (totp) and
// codes by e-mail (email); plugins add more (WebAuthn security keys and
// passkeys). For single sign-on providers the setting "mfa: false" leaves
// the second factor to the identity provider.

// MFAMethod is a second factor.
type MFAMethod interface {
	// ID is the short name ("totp", "email", "webauthn").
	ID() string
	// Enrolled reports whether user uid has set this factor up.
	Enrolled(ctx context.Context, uid int64) bool
	// Challenge prepares an answer: it may send a code or return options
	// for the browser (public) and keep state for Verify.
	Challenge(ctx context.Context, uid int64) (public any, state string, err error)
	// Verify checks the answer (JSON from the browser) with the state of
	// the last Challenge ("" without one).
	Verify(ctx context.Context, uid int64, state string, answer json.RawMessage) error
}

// MFAResetter is implemented by factors that administrators can remove
// (reset of all second factors of a user).
type MFAResetter interface {
	ResetMFA(ctx context.Context, uid int64) error
}

// PasswordlessMethod signs people in without a password (passkeys): Begin
// returns options for the browser and state; Finish checks the answer and
// returns the user. The method must verify the user itself (user
// verification), as no second factor is asked afterwards.
type PasswordlessMethod interface {
	ID() string
	Begin(ctx context.Context) (public any, state string, err error)
	Finish(ctx context.Context, state string, answer json.RawMessage) (uid int64, err error)
}

type registeredMFA struct {
	pluginID string
	m        MFAMethod
}

// RegisterMFAMethod adds a second factor owned by pluginID ("" = built in).
func (s *Service) RegisterMFAMethod(pluginID string, m MFAMethod) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mfa = append(s.mfa, registeredMFA{pluginID, m})
}

type registeredPasswordless struct {
	pluginID string
	m        PasswordlessMethod
}

// RegisterPasswordless adds a password-less sign-in owned by pluginID.
func (s *Service) RegisterPasswordless(pluginID string, m PasswordlessMethod) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.passwordless = append(s.passwordless, registeredPasswordless{pluginID, m})
}

func (s *Service) passwordlessMethod(id string) PasswordlessMethod {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.passwordless {
		if r.m.ID() == id && s.enabled(r.pluginID) {
			return r.m
		}
	}
	return nil
}

// PasswordlessMethods lists the active password-less sign-ins.
func (s *Service) PasswordlessMethods() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []string{}
	for _, r := range s.passwordless {
		if s.enabled(r.pluginID) {
			out = append(out, r.m.ID())
		}
	}
	return out
}

// PasswordlessBegin starts a password-less sign-in; the token identifies
// it at PasswordlessFinish (5 minutes, once).
func (s *Service) PasswordlessBegin(ctx context.Context, method, ip string) (string, any, error) {
	m := s.passwordlessMethod(method)
	if m == nil {
		return "", nil, apperr.NotFound("sign-in method " + method)
	}
	if !s.limiter.allow("pwless:"+ip, 60, 5*time.Minute) {
		return "", nil, apperr.TooMany("too many login attempts, try again later")
	}
	pub, state, err := m.Begin(ctx)
	if err != nil {
		return "", nil, err
	}
	tok := NewToken("")
	if _, err := s.pool.Exec(ctx, `INSERT INTO passwordless_pending(token_hash, method, state, expires_at) VALUES ($1,$2,$3, now() + interval '5 minutes')`,
		HashToken(tok), method, state); err != nil {
		return "", nil, err
	}
	return tok, pub, nil
}

// PasswordlessFinish completes a password-less sign-in with a session.
func (s *Service) PasswordlessFinish(ctx context.Context, token string, answer json.RawMessage, ip, ua string) (*LoginResult, error) {
	var method, state string
	err := s.pool.QueryRow(ctx, `DELETE FROM passwordless_pending WHERE token_hash=$1 AND expires_at > now() RETURNING method, state`,
		HashToken(token)).Scan(&method, &state)
	if err != nil {
		return nil, ErrMFAExpired
	}
	m := s.passwordlessMethod(method)
	if m == nil {
		return nil, ErrMFAExpired
	}
	uid, err := m.Finish(ctx, state, answer)
	if err != nil {
		s.limiter.allow("pwless:"+ip, 60, 5*time.Minute)
		return nil, apperr.ErrInvalidLogin
	}
	return s.SignInVerified(ctx, uid, method, ip, ua)
}

// MFAMethods returns the active second factors, strongest first.
func (s *Service) MFAMethods() []MFAMethod {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []MFAMethod
	for _, r := range s.mfa {
		if r.pluginID == "" || s.enabled(r.pluginID) {
			out = append(out, r.m)
		}
	}
	return out
}

func (s *Service) mfaMethod(id string) MFAMethod {
	for _, m := range s.MFAMethods() {
		if m.ID() == id {
			return m
		}
	}
	return nil
}

// MFABulk is implemented by factors that can tell for many users at once
// who has set them up (user lists).
type MFABulk interface {
	EnrolledAmong(ctx context.Context, ids []int64) map[int64]bool
}

// mfaAmong reports for each of ids whether the user has any factor.
func (s *Service) mfaAmong(ctx context.Context, ids []int64) map[int64]bool {
	out := map[int64]bool{}
	for _, m := range s.MFAMethods() {
		if b, ok := m.(MFABulk); ok {
			for id, on := range b.EnrolledAmong(ctx, ids) {
				out[id] = out[id] || on
			}
			continue
		}
		for _, id := range ids {
			if !out[id] && m.Enrolled(ctx, id) {
				out[id] = true
			}
		}
	}
	return out
}

// EnrolledMFA lists the second factors user uid has set up.
func (s *Service) EnrolledMFA(ctx context.Context, uid int64) []string {
	out := []string{}
	for _, m := range s.MFAMethods() {
		if m.Enrolled(ctx, uid) {
			out = append(out, m.ID())
		}
	}
	return out
}

// mfaApplies reports whether the wiki asks for the second factor after a
// sign-in with provider: always for local accounts and LDAP style
// providers, unless the provider's setting "mfa" is false (the identity
// provider does it).
func (s *Service) mfaApplies(provider string) bool {
	if provider == "local" {
		return true
	}
	var settings map[string]any
	for _, p := range s.Providers() {
		if sp, ok := p.(SettingsProvider); ok && p.ID() == provider {
			settings = sp.Settings()
		}
	}
	for _, p := range s.RedirectProviders() {
		if sp, ok := p.(SettingsProvider); ok && p.ID() == provider {
			settings = sp.Settings()
		}
	}
	return BoolSetting(settings, "mfa", true)
}

const (
	mfaPendingTTL      = 10 * time.Minute
	mfaPendingAttempts = 5
)

// errMFARequired is the answer to a sign-in that needs a second factor;
// Detail carries the token of the pending sign-in (its factors: see
// MFAPendingInfo).
func errMFARequired(token string) error {
	return &apperr.Error{Status: 401, Code: "mfa_required", Msg: "a second factor is required", Detail: token}
}

// ErrMFAExpired is returned for an unknown, used or expired pending sign-in.
var ErrMFAExpired = &apperr.Error{Status: 401, Code: "mfa_expired", Msg: "the sign-in has expired: please sign in again"}

type pendingSSO struct {
	Provider  string `json:"provider"`
	Subject   string `json:"subject"`
	SessionID string `json:"session_id"`
	Hint      string `json:"hint"`
}

// pendingMFA stores a sign-in waiting for its second factor and returns its
// token (only the hash is stored).
func (s *Service) pendingMFA(ctx context.Context, uid int64, ext *ExternalIdentity, next string) (string, error) {
	sso := pendingSSO{}
	if ext != nil {
		sso = pendingSSO{ext.Provider, ext.Subject, ext.SessionID, ext.LogoutHint}
		if len(sso.Hint) > 16<<10 {
			sso.Hint = ""
		}
	}
	tok := NewToken("")
	_, err := s.pool.Exec(ctx, `INSERT INTO mfa_pending(token_hash, user_id, sso, next, expires_at) VALUES ($1,$2,$3,$4,$5)`,
		HashToken(tok), uid, sso, SafeNext(next), time.Now().Add(mfaPendingTTL))
	return tok, err
}

// requireMFA turns a confirmed sign-in into a pending one if the person
// has second factors and the provider leaves them to the wiki.
func (s *Service) requireMFA(ctx context.Context, uid int64, ext *ExternalIdentity, next string) (string, []string, error) {
	if !s.mfaApplies(ext.Provider) {
		return "", nil, nil
	}
	methods := s.EnrolledMFA(ctx, uid)
	if len(methods) == 0 {
		return "", nil, nil
	}
	tok, err := s.pendingMFA(ctx, uid, ext, next)
	return tok, methods, err
}

type pending struct {
	uid      int64
	sso      pendingSSO
	next     string
	data     map[string]string
	attempts int
}

func (s *Service) loadPending(ctx context.Context, token string) (*pending, error) {
	if token == "" || len(token) > 100 {
		return nil, ErrMFAExpired
	}
	p := &pending{}
	err := s.pool.QueryRow(ctx, `SELECT user_id, sso, next, data, attempts FROM mfa_pending WHERE token_hash=$1 AND expires_at > now()`,
		HashToken(token)).Scan(&p.uid, &p.sso, &p.next, &p.data, &p.attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMFAExpired
	}
	if p.data == nil {
		p.data = map[string]string{}
	}
	return p, err
}

// PendingMFA describes a pending sign-in for the second-factor step.
type PendingMFA struct {
	Methods []string `json:"methods"`
}

// MFAPendingInfo returns the factors of a pending sign-in.
func (s *Service) MFAPendingInfo(ctx context.Context, token string) (*PendingMFA, error) {
	p, err := s.loadPending(ctx, token)
	if err != nil {
		return nil, err
	}
	return &PendingMFA{Methods: s.EnrolledMFA(ctx, p.uid)}, nil
}

// MFAChallenge prepares the factor method of a pending sign-in (sends the
// e-mail code, returns the WebAuthn options).
func (s *Service) MFAChallenge(ctx context.Context, token, method string) (any, error) {
	p, err := s.loadPending(ctx, token)
	if err != nil {
		return nil, err
	}
	m := s.mfaMethod(method)
	if m == nil || !m.Enrolled(ctx, p.uid) {
		return nil, apperr.BadRequest("this second factor is not available")
	}
	pub, state, err := m.Challenge(ctx, p.uid)
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE mfa_pending SET data = data || jsonb_build_object($2::text, $3::text) WHERE token_hash=$1`,
		HashToken(token), method, state); err != nil {
		return nil, err
	}
	return pub, nil
}

// MFAResult is a completed sign-in.
type MFAResult struct {
	*LoginResult
	Next string
}

// CompleteMFA checks the answer of a pending sign-in and creates the
// session. A pending sign-in allows 5 attempts and is used once.
func (s *Service) CompleteMFA(ctx context.Context, token, method string, answer json.RawMessage, ip, ua string) (*MFAResult, error) {
	p, err := s.loadPending(ctx, token)
	if err != nil {
		return nil, err
	}
	m := s.mfaMethod(method)
	if m == nil || !m.Enrolled(ctx, p.uid) {
		return nil, apperr.BadRequest("this second factor is not available")
	}
	// count the attempt first (concurrent guesses count too)
	var attempts int
	if err := s.pool.QueryRow(ctx, `UPDATE mfa_pending SET attempts = attempts + 1 WHERE token_hash=$1 RETURNING attempts`,
		HashToken(token)).Scan(&attempts); err != nil {
		return nil, ErrMFAExpired
	}
	if attempts > mfaPendingAttempts {
		_, _ = s.pool.Exec(ctx, `DELETE FROM mfa_pending WHERE token_hash=$1`, HashToken(token))
		return nil, ErrMFAExpired
	}
	if err := m.Verify(ctx, p.uid, p.data[method], answer); err != nil {
		return nil, err
	}
	// single use: whoever deletes the row creates the session
	tag, err := s.pool.Exec(ctx, `DELETE FROM mfa_pending WHERE token_hash=$1`, HashToken(token))
	if err != nil || tag.RowsAffected() != 1 {
		return nil, ErrMFAExpired
	}
	if err := s.checkNetwork(ctx, p.uid, ip); err != nil {
		return nil, err
	}
	ext := &ExternalIdentity{Provider: p.sso.Provider, Subject: p.sso.Subject, SessionID: p.sso.SessionID, LogoutHint: p.sso.Hint}
	res, err := s.newSession(ctx, p.uid, ext, ip, ua)
	if err != nil {
		return nil, err
	}
	return &MFAResult{LoginResult: res, Next: p.next}, nil
}

// newSession creates a browser session for uid after a complete sign-in.
func (s *Service) newSession(ctx context.Context, uid int64, ext *ExternalIdentity, ip, ua string) (*LoginResult, error) {
	tok, exp, err := s.createSession(ctx, uid, "web", config.Duration(s.cfg.Get().Auth.SessionTTL, 7*24*time.Hour), ip, ua, ext)
	if err != nil {
		return nil, err
	}
	pr, err := s.LoadPrincipal(ctx, uid)
	if err != nil {
		return nil, err
	}
	pr.Via = "session"
	return &LoginResult{tok, exp, pr}, nil
}

// SignInVerified creates a session for uid after a sign-in method that is
// multi-factor itself (a passkey with user verification). The account must
// be enabled and the network allowed.
func (s *Service) SignInVerified(ctx context.Context, uid int64, provider, ip, ua string) (*LoginResult, error) {
	var disabled bool
	if err := s.pool.QueryRow(ctx, `SELECT disabled FROM users WHERE id=$1`, uid).Scan(&disabled); err != nil || disabled {
		return nil, apperr.ErrInvalidLogin
	}
	if err := s.checkNetwork(ctx, uid, ip); err != nil {
		return nil, err
	}
	_, _ = s.pool.Exec(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, uid)
	return s.newSession(ctx, uid, &ExternalIdentity{Provider: provider}, ip, ua)
}

// ---------------------------------------------------------------- built-in factors

type totpMethod struct{ s *Service }

func (m totpMethod) ID() string { return "totp" }
func (m totpMethod) Enrolled(ctx context.Context, uid int64) bool {
	var on bool
	_ = m.s.pool.QueryRow(ctx, `SELECT totp_enabled AND totp_secret IS NOT NULL FROM users WHERE id=$1`, uid).Scan(&on)
	return on
}
func (m totpMethod) Challenge(context.Context, int64) (any, string, error) { return nil, "", nil }
func (m totpMethod) Verify(ctx context.Context, uid int64, _ string, answer json.RawMessage) error {
	var in struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(answer, &in)
	if strings.TrimSpace(in.Code) == "" {
		return ErrTOTPInvalid
	}
	return m.s.checkTOTP(ctx, uid, in.Code)
}

type emailMethod struct{ s *Service }

func (m emailMethod) ID() string { return "email" }
func (m emailMethod) Enrolled(ctx context.Context, uid int64) bool {
	var on bool
	_ = m.s.pool.QueryRow(ctx, `SELECT mfa_email AND email <> '' FROM users WHERE id=$1`, uid).Scan(&on)
	return on
}
func (m emailMethod) Challenge(ctx context.Context, uid int64) (any, string, error) {
	if err := m.s.sendCode(ctx, uid, "login"); err != nil {
		return nil, "", err
	}
	var addr string
	_ = m.s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, uid).Scan(&addr)
	return map[string]string{"address": maskEmail(addr)}, "", nil
}
func (m emailMethod) Verify(ctx context.Context, uid int64, _ string, answer json.RawMessage) error {
	var in struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(answer, &in)
	ok, err := m.s.verifyCode(ctx, uid, "login", in.Code)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTOTPInvalid
	}
	return nil
}

// maskEmail shows enough of an address to recognise it (a***@example.org).
func maskEmail(a string) string {
	local, domain, ok := strings.Cut(a, "@")
	if !ok || local == "" {
		return ""
	}
	return local[:1] + "***@" + domain
}

// CurrentUserID returns the signed-in user of ctx (0 if none).
func CurrentUserID(ctx context.Context) int64 {
	if p := identity.From(ctx); p != nil {
		return p.UserID
	}
	return 0
}
