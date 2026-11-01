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
	"github.com/mtsonline/enterprisewiki/internal/config"
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
}

// RegisterRedirectProvider adds a browser based provider owned by pluginID.
func (s *Service) RegisterRedirectProvider(pluginID string, p RedirectProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.redirects = append(s.redirects, registeredRedirect{pluginID, p})
}

// RedirectProviders returns the active browser based providers.
func (s *Service) RedirectProviders() []RedirectProvider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []RedirectProvider
	for _, r := range s.redirects {
		if s.enabled(r.pluginID) {
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

// SafeNext returns next if it is a local path, otherwise "/".
func SafeNext(next string) string {
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") && !strings.HasPrefix(next, "/\\") {
		return next
	}
	return "/"
}

// SSOResult is returned when a single sign-on login completes.
type SSOResult struct {
	*LoginResult
	Next string
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
		res, err := s.completeSSO(ctx, ext, ip, ua)
		if err != nil {
			return "", nil, err
		}
		return "", &SSOResult{res, next}, nil
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
	res, err := s.completeSSO(ctx, ext, ip, ua)
	if err != nil {
		return nil, err
	}
	return &SSOResult{res, next}, nil
}

func (s *Service) completeSSO(ctx context.Context, ext *ExternalIdentity, ip, ua string) (*LoginResult, error) {
	if ext.Subject == "" || ext.Username == "" {
		return nil, apperr.BadRequest("the identity provider did not return a user name")
	}
	uid, err := s.syncIdentity(ctx, ext)
	if err != nil {
		return nil, err
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
	s.log.Info("sso login", "user", pr.Handle(), "ip", ip)
	return &LoginResult{tok, exp, pr}, nil
}

// CleanupSSO removes expired SSO states.
func (s *Service) CleanupSSO(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sso_states WHERE expires_at < now()`)
	return err
}
