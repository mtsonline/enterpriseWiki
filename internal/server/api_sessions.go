package server

import (
	"errors"
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// sessionToken returns the session token of the request (cookie), or "".
func (s *Server) sessionToken(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// routesSessions: own sessions, sessions of a user (administrators) and
// logout messages of single sign-on providers (M15b).
func (s *Server) routesSessions() {
	s.handle("GET /api/v1/me/sessions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Auth.MySessions(r.Context(), s.sessionToken(r))
	})
	// sign out everywhere else (the current session stays)
	s.handle("DELETE /api/v1/me/sessions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return h.NoContent{}, s.app.Auth.EndMyOtherSessions(r.Context(), s.sessionToken(r))
	})
	s.handle("DELETE /api/v1/me/sessions/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.EndMySession(r.Context(), id)
	})
	s.handle("GET /api/v1/admin/users/{id}/sessions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Auth.UserSessions(r.Context(), id)
	})
	s.handle("DELETE /api/v1/admin/users/{id}/sessions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.EndUserSession(r.Context(), id, 0)
	})
	s.handle("DELETE /api/v1/admin/users/{id}/sessions/{sid}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		sid, err := h.PathID(r, "sid")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.EndUserSession(r.Context(), id, sid)
	})

	// Called by the identity provider: OIDC back-channel logout (POST
	// logout_token), SAML LogoutRequest/LogoutResponse (redirect or POST
	// binding). The provider validates the message; no cookie is needed.
	logout := func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		w.Header().Set("Cache-Control", "no-store")
		respond, err := s.app.Auth.ProviderLogout(r.Context(), r.PathValue("provider"), r, s.clientIP(r))
		if err != nil {
			s.app.Log.Warn("provider logout", "provider", r.PathValue("provider"), "err", err)
			status := http.StatusBadRequest
			var ae *apperr.Error
			if errors.As(err, &ae) && (ae.Status == http.StatusNotFound || ae.Status == http.StatusTooManyRequests) {
				status = ae.Status
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		if respond != nil {
			respond(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
	h.RecordRoute("GET /api/v1/auth/sso/{provider}/logout")
	h.RecordRoute("POST /api/v1/auth/sso/{provider}/logout")
	s.app.Mux.HandleFunc("GET /api/v1/auth/sso/{provider}/logout", logout)
	s.app.Mux.HandleFunc("POST /api/v1/auth/sso/{provider}/logout", logout)
}
