package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// routesAuth registers the routes of sign-in, sign-out and the sign-in methods.
func (s *Server) routesAuth() {
	// action: core.auth.providers
	s.handle("GET /api/v1/auth/providers", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Auth.ProviderInfos(), nil
	})
	// action: core.auth.login
	s.handle("POST /api/v1/auth/login", s.login)
	// action: core.auth.logout
	s.handle("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if p := identity.From(r.Context()); p != nil {
			_ = audit.Log(r.Context(), s.app.DB, "auth.logout", "user", p.ID(), nil, nil)
		}
		var redirect string
		if c, err := r.Cookie(sessionCookie); err == nil {
			redirect, _ = s.app.Auth.Logout(r.Context(), c.Value)
		}
		if a, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && !strings.HasPrefix(a, "ewk_") {
			_, _ = s.app.Auth.Logout(r.Context(), a)
		}
		s.clearCookie(w)
		if redirect != "" {
			// single logout: the browser continues at the identity provider
			return map[string]string{"redirect": redirect}, nil
		}
		return h.NoContent{}, nil
	})
	// action: core.auth.me
	s.handle("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p := identity.From(r.Context())
		if p == nil {
			return map[string]any{"user": nil}, nil
		}
		return map[string]any{"user": p, "handle": p.Handle(), "manages_categories": s.app.ACL.Any(r.Context(), p, acl.Manage)}, nil
	})
	// action: core.auth.ssh.challenge
	s.handle("POST /api/v1/auth/ssh/challenge", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Username string `json:"username"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		c, err := s.app.Auth.SSHChallenge(r.Context(), in.Username)
		if err != nil {
			return nil, err
		}
		return map[string]string{"challenge": c, "namespace": auth.SSHNamespace,
			"hint": "printf %s CHALLENGE | ssh-keygen -Y sign -f ~/.ssh/id_ed25519 -n " + auth.SSHNamespace}, nil
	})
	// action: core.auth.ssh.verify
	s.handle("POST /api/v1/auth/ssh/verify", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Challenge string `json:"challenge"`
			Signature string `json:"signature"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		res, err := s.app.Auth.SSHLogin(r.Context(), in.Challenge, in.Signature, s.clientIP(r), r.UserAgent())
		if err != nil {
			return nil, err
		}
		return map[string]any{"token": res.Token, "expires_at": res.ExpiresAt, "user": res.Principal}, nil
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := h.Decode(r, &in); err != nil {
		return nil, err
	}
	res, err := s.app.Auth.Login(r.Context(), in.Username, in.Password, s.clientIP(r), r.UserAgent())
	if err != nil {
		s.auditLogin(r, nil, "password", in.Username, err)
		return nil, err
	}
	s.auditLogin(r, res.Principal, "password", in.Username, nil)
	s.setCookie(w, res.Token, res.ExpiresAt)
	return map[string]any{"user": res.Principal, "expires_at": res.ExpiresAt}, nil
}

// auditLogin records a sign-in or a failed attempt (audit.log_logins).
func (s *Server) auditLogin(r *http.Request, p *identity.Principal, method, username string, err error) {
	ctx := audit.WithRequest(r.Context(), s.clientIP(r), r.UserAgent())
	if err != nil {
		reason := "error"
		var ae *apperr.Error
		if errors.As(err, &ae) {
			reason = ae.Code
		}
		_ = audit.Log(ctx, s.app.DB, "auth.login_failed", "user", 0, nil, map[string]string{"username": clip(username, 255), "method": method, "reason": reason})
		return
	}
	_ = audit.Log(identity.With(ctx, p), s.app.DB, "auth.login", "user", p.ID(), nil, map[string]string{"method": method})
}
