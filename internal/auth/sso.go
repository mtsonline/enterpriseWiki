package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

// SSORequest carries the state of a single sign-on login between Start and
// Callback. Providers may store additional values in Data (e.g. the SAML
// request id); they are persisted server side, never in the browser.
type SSORequest struct {
	State       string
	Nonce       string
	Verifier    string // PKCE code verifier
	CallbackURL string // absolute URL of /api/v1/auth/sso/<id>/callback
	// TrustedProxy is true if the request came from a configured trusted
	// proxy (required for header based SSO such as Apache mod_shib).
	TrustedProxy bool
	Data         map[string]string
}

// RedirectProvider authenticates via the browser (OIDC, SAML, header SSO).
type RedirectProvider interface {
	ID() string
	Name() string
	// Start begins a login. It returns either a URL to redirect the browser
	// to, or (for header based SSO) the authenticated identity directly.
	Start(ctx context.Context, r *http.Request, req *SSORequest) (redirect string, id *ExternalIdentity, err error)
	// Callback completes the login after the identity provider redirected back.
	Callback(ctx context.Context, r *http.Request, req *SSORequest) (*ExternalIdentity, error)
}

type registeredRedirect struct {
	pluginID string
	p        RedirectProvider
	src      func() []RedirectProvider // provider instances (see instances.go)
}

// RegisterRedirectProvider adds a browser based provider owned by pluginID.
func (s *Service) RegisterRedirectProvider(pluginID string, p RedirectProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redirects = append(s.redirects, registeredRedirect{pluginID: pluginID, p: p})
}

// RegisterRedirectSource adds the browser based providers src returns at
// each call (one per configured instance) owned by pluginID.
func (s *Service) RegisterRedirectSource(pluginID string, src func() []RedirectProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redirects = append(s.redirects, registeredRedirect{pluginID: pluginID, src: src})
}

// RedirectProviders returns the active browser based providers.
func (s *Service) RedirectProviders() []RedirectProvider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []RedirectProvider
	for _, r := range s.redirects {
		switch {
		case !s.enabled(r.pluginID):
		case r.src != nil:
			out = append(out, r.src()...)
		default:
			out = append(out, r.p)
		}
	}
	return out
}

func (s *Service) redirectProvider(id string) RedirectProvider {
	for _, p := range s.RedirectProviders() {
		if p.ID() == id {
			return p
		}
	}
	return nil
}

// SafeNext returns next if it is a local path, otherwise "/". Browsers
// ignore tabs and line breaks in URLs and treat "\\" like "/", so
// "/\t/evil.example" or "/\\evil.example" would lead to another site: any
// control character, space or backslash makes next unsafe.
func SafeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || len(next) > 2048 {
		return "/"
	}
	for _, r := range next {
		if r < 0x21 || r == 0x7f || r == '\\' || (r >= 0x80 && r <= 0x9f) || r == '\u2028' || r == '\u2029' {
			return "/"
		}
	}
	return next
}

// SSOResult is returned when a single sign-on login completes: a session,
// or (MFAToken) a pending sign-in waiting for the second factor.
type SSOResult struct {
	*LoginResult
	Next     string
	MFAToken string
}

// SSOStart begins a browser login. It returns the URL to redirect to, or a
// completed login (header based providers).
func (s *Service) SSOStart(ctx context.Context, provider string, r *http.Request, next string, trusted bool, ip, ua string) (string, *SSOResult, error) {
	p := s.redirectProvider(provider)
	if p == nil {
		return "", nil, apperr.NotFound("sign-in method " + provider)
	}
	if !s.limiter.allow("sso:"+ip, 60, 5*time.Minute) {
		return "", nil, apperr.TooMany("too many login attempts, try again later")
	}
	req := &SSORequest{State: NewToken(""), Nonce: NewToken(""), Verifier: NewToken("") + NewToken(""), TrustedProxy: trusted, Data: map[string]string{},
		CallbackURL: strings.TrimRight(s.cfg.Get().Server.BaseURL, "/") + "/api/v1/auth/sso/" + provider + "/callback"}
	redirect, ext, err := p.Start(ctx, r, req)
	if err != nil {
		return "", nil, err
	}
	next = SafeNext(next)
	if ext != nil {
		res, err := s.completeSSO(ctx, ext, next, ip, ua)
		return "", res, err
	}
	data, _ := json.Marshal(req.Data)
	if _, err := s.pool.Exec(ctx, `INSERT INTO sso_states(state, provider, nonce, verifier, next, data, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6, now() + interval '10 minutes')`, req.State, provider, req.Nonce, req.Verifier, next, data); err != nil {
		return "", nil, err
	}
	return redirect, nil, nil
}

// SSOCallback completes a browser login. The state is read from the "state"
// (OIDC) or "RelayState" (SAML) parameter and can be used only once.
func (s *Service) SSOCallback(ctx context.Context, provider string, r *http.Request, trusted bool, ip, ua string) (*SSOResult, error) {
	p := s.redirectProvider(provider)
	if p == nil {
		return nil, apperr.NotFound("sign-in method " + provider)
	}
	state := r.FormValue("state")
	if state == "" {
		state = r.FormValue("RelayState")
	}
	if state == "" {
		return nil, apperr.BadRequest("missing state")
	}
	req := &SSORequest{State: state, TrustedProxy: trusted, Data: map[string]string{},
		CallbackURL: strings.TrimRight(s.cfg.Get().Server.BaseURL, "/") + "/api/v1/auth/sso/" + provider + "/callback"}
	var next string
	var data []byte
	err := s.pool.QueryRow(ctx, `DELETE FROM sso_states WHERE state=$1 AND provider=$2 AND expires_at > now()
		RETURNING nonce, verifier, next, data`, state, provider).Scan(&req.Nonce, &req.Verifier, &next, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.BadRequest("the login request expired or was already used, please try again")
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(data, &req.Data)
	ext, err := p.Callback(ctx, r, req)
	if err != nil {
		s.log.Warn("sso callback failed", "provider", provider, "err", err)
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return nil, err
		}
		return nil, apperr.ErrInvalidLogin
	}
	return s.completeSSO(ctx, ext, next, ip, ua)
}

func (s *Service) completeSSO(ctx context.Context, ext *ExternalIdentity, next, ip, ua string) (*SSOResult, error) {
	if ext.Subject == "" || ext.Username == "" {
		return nil, apperr.BadRequest("the identity provider did not return a user name")
	}
	uid, err := s.syncIdentity(ctx, ext)
	if err != nil {
		return nil, err
	}
	if err := s.checkSignIn(ctx, uid, ip); err != nil {
		return nil, err
	}
	// further factors of this provider's chain
	res, err := s.afterFirstFactor(ctx, uid, ext.Provider, ext, next, ip, ua)
	if err != nil {
		return nil, err
	}
	if res.MFAToken != "" {
		return &SSOResult{Next: next, MFAToken: res.MFAToken}, nil
	}
	s.log.Info("sso login", "user", res.Principal.Handle(), "ip", ip)
	return &SSOResult{LoginResult: res.LoginResult, Next: next}, nil
}

// CleanupSSO removes expired SSO states.
func (s *Service) CleanupSSO(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sso_replay WHERE expires_at < now()`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM sso_states WHERE expires_at < now()`)
	return err
}

// ErrReplayed is returned for a message ID that was already accepted.
var ErrReplayed = errors.New("message has already been used")

// ReplayCache records IDs of single-use messages of identity providers in
// the database, so each is accepted once on all instances. It implements
// the AssertionReplayCache interface of github.com/crewjam/saml.
type ReplayCache struct {
	s      *Service
	prefix string
}

// ReplayCache returns the cache of a provider (prefix keeps providers apart).
func (s *Service) ReplayCache(prefix string) *ReplayCache { return &ReplayCache{s, prefix} }

// Consume records id until expiresAt (at most one day); it returns
// ErrReplayed if id was recorded already.
func (c *ReplayCache) Consume(id string, expiresAt time.Time) error {
	if id == "" || len(id) > 512 {
		return errors.New("invalid message id")
	}
	if max := time.Now().Add(24 * time.Hour); expiresAt.After(max) {
		expiresAt = max
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, err := c.s.pool.Exec(ctx, `INSERT INTO sso_replay(id, expires_at) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`, c.prefix+":"+id, expiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrReplayed
	}
	return nil
}
