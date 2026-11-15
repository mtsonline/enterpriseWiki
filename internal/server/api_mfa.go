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

// Further factors of a sign-in (see internal/auth/mfaflow.go). The pending
// sign-in is identified by its token, given in the request body (the token
// came with the "mfa_required" error) or in a short-lived HttpOnly cookie
// (single sign-on: the callback set it and sent the browser to /login?mfa=1).
const mfaCookie = "ewiki_mfa"

// finishSSO ends a single sign-on: with a session, or with the pending
// sign-in for further factors.
func (s *Server) finishSSO(w http.ResponseWriter, r *http.Request, res *auth.SSOResult) {
	if res.MFAToken != "" {
		s.setMFACookie(w, res.MFAToken)
		http.Redirect(w, r, "/login?mfa=1", http.StatusSeeOther)
		return
	}
	s.auditLogin(r, res.Principal, "sso:"+r.PathValue("provider"), "", nil)
	s.setCookie(w, res.Token, res.ExpiresAt)
	http.Redirect(w, r, auth.SafeNext(res.Next), http.StatusSeeOther) // #nosec G710 -- SafeNext allows local paths only
}

func (s *Server) setMFACookie(w http.ResponseWriter, token string) {
	// #nosec G124 -- HttpOnly, SameSite=Lax, only sent to the MFA endpoints; Secure as configured
	http.SetCookie(w, &http.Cookie{Name: mfaCookie, Value: token, Path: "/api/v1/auth/mfa", MaxAge: 600, HttpOnly: true,
		Secure: s.app.Config.Get().Server.SecureCookies, SameSite: http.SameSiteLaxMode})
}

func (s *Server) clearMFACookie(w http.ResponseWriter) {
	// #nosec G124 -- deletes the cookie
	http.SetCookie(w, &http.Cookie{Name: mfaCookie, Value: "", Path: "/api/v1/auth/mfa", MaxAge: -1, HttpOnly: true,
		Secure: s.app.Config.Get().Server.SecureCookies, SameSite: http.SameSiteLaxMode})
}

// mfaRequest reads the body (at most 64 KB), the pending sign-in token and
// the method.
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
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	return in.Token, in.Method, raw, nil
}

// mfaAnswer writes the result of a step: the session, or the next step.
func (s *Server) mfaAnswer(w http.ResponseWriter, r *http.Request, method string, res *auth.MFAResult) any {
	if res.Step != nil {
		return map[string]any{"done": false, "step": res.Step}
	}
	s.clearMFACookie(w)
	s.auditLogin(r, res.Principal, "mfa:"+method, "", nil)
	s.setCookie(w, res.Token, res.ExpiresAt)
	return map[string]any{"done": true, "user": res.Principal, "expires_at": res.ExpiresAt.Format(time.RFC3339), "next": auth.SafeNext(res.Next)}
}

func (s *Server) routesMFA() {
	// ---------------------------------------------------------------- during the sign-in
	// action: core.auth.mfa.info
	s.handle("POST /api/v1/auth/mfa/info", func(w http.ResponseWriter, r *http.Request) (any, error) {
		tok, _, _, err := s.mfaRequest(r)
		if err != nil {
			return nil, err
		}
		return s.app.Auth.MFAPendingInfo(r.Context(), tok, s.clientIP(r))
	})
	// action: core.auth.mfa.challenge
	s.handle("POST /api/v1/auth/mfa/challenge", func(w http.ResponseWriter, r *http.Request) (any, error) {
		tok, method, _, err := s.mfaRequest(r)
		if err != nil {
			return nil, err
		}
		pub, err := s.app.Auth.MFAChallenge(r.Context(), tok, method, s.clientIP(r))
		if err != nil {
			return nil, err
		}
		if pub == nil {
			return h.NoContent{}, nil
		}
		return pub, nil
	})
	// action: core.auth.mfa.verify
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
		return s.mfaAnswer(w, r, method, res), nil
	})
	// action: core.auth.mfa.enroll.begin
	s.handle("POST /api/v1/auth/mfa/enroll/begin", func(w http.ResponseWriter, r *http.Request) (any, error) {
		tok, method, body, err := s.mfaRequest(r)
		if err != nil {
			return nil, err
		}
		return s.app.Auth.MFAEnrollBegin(r.Context(), tok, method, body, s.clientIP(r))
	})
	// action: core.auth.mfa.enroll.finish
	s.handle("POST /api/v1/auth/mfa/enroll/finish", func(w http.ResponseWriter, r *http.Request) (any, error) {
		tok, method, body, err := s.mfaRequest(r)
		if err != nil {
			return nil, err
		}
		res, err := s.app.Auth.MFAEnrollFinish(r.Context(), tok, method, body, s.clientIP(r), r.UserAgent())
		if err != nil {
			s.auditLogin(r, nil, "mfa-enroll:"+method, "", err)
			return nil, err
		}
		return s.mfaAnswer(w, r, method, res), nil
	})

	// ---------------------------------------------------------------- own factors
	// action: core.me.mfa
	s.handle("GET /api/v1/me/mfa", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Auth.MFA(r.Context())
	})
	// action: core.me.mfa.setup.begin
	s.handle("POST /api/v1/me/mfa/{method}/begin", func(w http.ResponseWriter, r *http.Request) (any, error) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil {
			return nil, err
		}
		return s.app.Auth.MyEnrollBegin(r.Context(), r.PathValue("method"), raw)
	})
	// action: core.me.mfa.setup.finish
	s.handle("POST /api/v1/me/mfa/{method}/finish", func(w http.ResponseWriter, r *http.Request) (any, error) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.MyEnrollFinish(r.Context(), r.PathValue("method"), raw)
	})

	// ---------------------------------------------------------------- administration
	// action: core.mfa.settings.show
	s.handle("GET /api/v1/admin/mfa", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Auth.MFAConfig(r.Context())
	})
	// action: core.mfa.settings.set
	s.handle("PUT /api/v1/admin/mfa", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in auth.MFAConfigUpdate
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := s.app.Auth.SetMFAConfig(r.Context(), in); err != nil {
			return nil, err
		}
		return s.app.Auth.MFAConfig(r.Context())
	})
}

// routesPasswordless: sign-in without password (passkeys as factor 1).
func (s *Server) routesPasswordless() {
	// action: core.auth.passwordless.begin
	s.handle("POST /api/v1/auth/passwordless/{method}/begin", func(w http.ResponseWriter, r *http.Request) (any, error) {
		// {"autofill": true}: the request of the user name field (own limit)
		var in struct {
			Autofill bool `json:"autofill"`
		}
		if r.ContentLength != 0 {
			if err := h.Decode(r, &in); err != nil {
				return nil, err
			}
		}
		tok, pub, err := s.app.Auth.PasswordlessBegin(r.Context(), r.PathValue("method"), s.clientIP(r), in.Autofill)
		if err != nil {
			return nil, err
		}
		return map[string]any{"token": tok, "options": pub}, nil
	})
	// action: core.auth.passwordless.finish
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
		if res.MFAToken != "" {
			return nil, &apperr.Error{Status: 401, Code: "mfa_required", Msg: "a further factor is required", Detail: res.MFAToken}
		}
		s.auditLogin(r, res.Principal, r.PathValue("method"), "", nil)
		s.setCookie(w, res.Token, res.ExpiresAt)
		return map[string]any{"user": res.Principal, "expires_at": res.ExpiresAt.Format(time.RFC3339)}, nil
	})
}
