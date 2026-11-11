package server

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// The second step of a sign-in: the pending sign-in is identified by its
// token, given in the request body (password sign-in: the token came with
// the "mfa_required" error) or in a short-lived HttpOnly cookie (single
// sign-on: the callback set it and sent the browser to /login?mfa=1).
const mfaCookie = "ewiki_mfa"

// finishSSO ends a single sign-on: with a session, or with the pending
// sign-in for the second factor.
func (s *Server) finishSSO(w http.ResponseWriter, r *http.Request, res *auth.SSOResult) {
	if res.MFAToken != "" {
		// #nosec G124 -- HttpOnly, SameSite=Lax, only sent to the MFA endpoints; Secure as configured
		http.SetCookie(w, &http.Cookie{Name: mfaCookie, Value: res.MFAToken, Path: "/api/v1/auth/mfa", MaxAge: 600, HttpOnly: true,
			Secure: s.app.Config.Get().Server.SecureCookies, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/login?mfa=1", http.StatusSeeOther)
		return
	}
	s.auditLogin(r, res.Principal, "sso:"+r.PathValue("provider"), "", nil)
	s.setCookie(w, res.Token, res.ExpiresAt)
	http.Redirect(w, r, auth.SafeNext(res.Next), http.StatusSeeOther) // #nosec G710 -- SafeNext allows local paths only
}

func (s *Server) clearMFACookie(w http.ResponseWriter) {
	// #nosec G124 -- deletes the cookie
	http.SetCookie(w, &http.Cookie{Name: mfaCookie, Value: "", Path: "/api/v1/auth/mfa", MaxAge: -1, HttpOnly: true,
		Secure: s.app.Config.Get().Server.SecureCookies, SameSite: http.SameSiteLaxMode})
}

// mfaRequest reads the body (at most 64 KB) and the pending sign-in token.
func (s *Server) mfaRequest(r *http.Request) (token, method string, body json.RawMessage, err error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return "", "", nil, err
	}
	var in struct {
		Token  string `json:"token"`
		Method string `json:"method"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", "", nil, apperr.BadRequest("invalid JSON: %v", err)
		}
	}
	if in.Token == "" {
		if c, err := r.Cookie(mfaCookie); err == nil {
			in.Token = c.Value
		}
	}
	return in.Token, in.Method, raw, nil
}

func (s *Server) routesMFA() {
	s.handle("POST /api/v1/auth/mfa/info", func(w http.ResponseWriter, r *http.Request) (any, error) {
		tok, _, _, err := s.mfaRequest(r)
		if err != nil {
			return nil, err
		}
		return s.app.Auth.MFAPendingInfo(r.Context(), tok)
	})
	s.handle("POST /api/v1/auth/mfa/challenge", func(w http.ResponseWriter, r *http.Request) (any, error) {
		tok, method, _, err := s.mfaRequest(r)
		if err != nil {
			return nil, err
		}
		pub, err := s.app.Auth.MFAChallenge(r.Context(), tok, method)
		if err != nil {
			return nil, err
		}
		if pub == nil {
			return h.NoContent{}, nil
		}
		return pub, nil
	})
	s.handle("POST /api/v1/auth/mfa/verify", func(w http.ResponseWriter, r *http.Request) (any, error) {
		tok, method, body, err := s.mfaRequest(r)
		if err != nil {
			return nil, err
		}
		res, err := s.app.Auth.CompleteMFA(r.Context(), tok, method, body, s.clientIP(r), r.UserAgent())
		if err != nil {
			s.auditLogin(r, nil, "mfa:"+method, "", err)
			return nil, err
		}
		s.clearMFACookie(w)
		s.auditLogin(r, res.Principal, "mfa:"+method, "", nil)
		s.setCookie(w, res.Token, res.ExpiresAt)
		return map[string]any{"user": res.Principal, "expires_at": res.ExpiresAt.Format(time.RFC3339), "next": auth.SafeNext(res.Next)}, nil
	})
}

// routesPasswordless: sign-in without password (passkeys).
func (s *Server) routesPasswordless() {
	s.handle("POST /api/v1/auth/passwordless/{method}/begin", func(w http.ResponseWriter, r *http.Request) (any, error) {
		tok, pub, err := s.app.Auth.PasswordlessBegin(r.Context(), r.PathValue("method"), s.clientIP(r))
		if err != nil {
			return nil, err
		}
		return map[string]any{"token": tok, "options": pub}, nil
	})
	s.handle("POST /api/v1/auth/passwordless/{method}/finish", func(w http.ResponseWriter, r *http.Request) (any, error) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil {
			return nil, err
		}
		var in struct {
			Token      string          `json:"token"`
			Credential json.RawMessage `json:"credential"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, apperr.BadRequest("invalid JSON: %v", err)
		}
		res, err := s.app.Auth.PasswordlessFinish(r.Context(), in.Token, in.Credential, s.clientIP(r), r.UserAgent())
		if err != nil {
			s.auditLogin(r, nil, r.PathValue("method"), "", err)
			return nil, err
		}
		s.auditLogin(r, res.Principal, r.PathValue("method"), "", nil)
		s.setCookie(w, res.Token, res.ExpiresAt)
		return map[string]any{"user": res.Principal, "expires_at": res.ExpiresAt.Format(time.RFC3339)}, nil
	})
}
