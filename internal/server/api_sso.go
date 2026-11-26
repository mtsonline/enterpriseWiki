package server

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// routesSSO registers single sign-on: the start at the identity provider and
// its answer (callback, as redirect or HTTP-POST binding).
func (s *Server) routesSSO() {
	// action: core.auth.sso.start
	h.RecordRoute("GET /api/v1/auth/sso/{provider}/start")
	s.app.Mux.HandleFunc("GET /api/v1/auth/sso/{provider}/start", func(w http.ResponseWriter, r *http.Request) {
		next := r.URL.Query().Get("next")
		redirect, res, err := s.app.Auth.SSOStart(r.Context(), r.PathValue("provider"), r, next, s.isTrustedProxy(r), s.clientIP(r), r.UserAgent())
		if err != nil {
			s.ssoError(w, r, err)
			return
		}
		if res != nil {
			s.finishSSO(w, r, res)
			return
		}
		http.Redirect(w, r, redirect, http.StatusFound) // #nosec G710 -- URL of the configured identity provider, not from the request
	})
	callback := func(w http.ResponseWriter, r *http.Request) {
		res, err := s.app.Auth.SSOCallback(r.Context(), r.PathValue("provider"), r, s.isTrustedProxy(r), s.clientIP(r), r.UserAgent())
		if err != nil {
			s.auditLogin(r, nil, "sso:"+r.PathValue("provider"), "", err)
			s.ssoError(w, r, err)
			return
		}
		s.finishSSO(w, r, res)
	}
	// Called by the identity provider (redirect or HTTP-POST binding).
	// action: core.auth.sso.callback
	h.RecordRoute("GET /api/v1/auth/sso/{provider}/callback")
	// action: core.auth.sso.callback.post
	h.RecordRoute("POST /api/v1/auth/sso/{provider}/callback")
	s.app.Mux.HandleFunc("GET /api/v1/auth/sso/{provider}/callback", callback)
	s.app.Mux.HandleFunc("POST /api/v1/auth/sso/{provider}/callback", callback)
}

// ssoError sends the browser back to the login page with a message.
func (s *Server) ssoError(w http.ResponseWriter, r *http.Request, err error) {
	_, code, _ := apperr.Status(err)
	if code == "internal" {
		s.log.Error("sso failed", "err", err)
	}
	var ae *apperr.Error
	if code == "link_required" && errors.As(err, &ae) {
		http.Redirect(w, r, "/login?link="+url.QueryEscape(ae.Detail), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login?error="+url.QueryEscape(apperr.Localized(err, apperr.RequestLanguage(r))), http.StatusSeeOther)
}

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
