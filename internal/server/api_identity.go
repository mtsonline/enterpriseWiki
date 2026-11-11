package server

import (
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/auth"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// routesIdentity: e-mail addresses, linked sign-ins and invitations (M15b).
func (s *Server) routesIdentity() {
	// ---------------------------------------------------------------- own addresses
	s.handle("GET /api/v1/me/emails", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Auth.MyEmails(r.Context())
	})
	s.handle("POST /api/v1/me/emails", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Email string `json:"email"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		e, err := s.app.Auth.AddMyEmail(r.Context(), in.Email)
		if err != nil {
			return nil, err
		}
		return h.Created{V: e}, nil
	})
	s.handle("POST /api/v1/me/emails/{id}/resend", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.ResendConfirmation(r.Context(), id)
	})
	s.handle("POST /api/v1/me/emails/{id}/primary", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.SetMyPrimaryEmail(r.Context(), id)
	})
	s.handle("DELETE /api/v1/me/emails/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.RemoveMyEmail(r.Context(), id)
	})
	// the confirmation link (anyone holding it; the token is the secret)
	s.handle("POST /api/v1/auth/verify-email", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Token string `json:"token"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		addr, err := s.app.Auth.VerifyEmail(r.Context(), in.Token)
		if err != nil {
			return nil, err
		}
		return map[string]string{"email": addr}, nil
	})

	// ---------------------------------------------------------------- linked sign-ins
	s.handle("GET /api/v1/me/identities", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Auth.MyIdentities(r.Context())
	})
	// ?provider=…&subject=… (subjects may contain any characters, e.g. LDAP DNs)
	s.handle("DELETE /api/v1/me/identities", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		return h.NoContent{}, s.app.Auth.UnlinkMyIdentity(r.Context(), q.Get("provider"), q.Get("subject"))
	})
	// a sign-in waiting for confirmation: what it is (before signing in)
	s.handle("POST /api/v1/auth/link/info", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Token string `json:"token"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Auth.PendingLink(r.Context(), in.Token)
	})
	s.handle("POST /api/v1/me/identities/link", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Token string `json:"token"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Auth.ConfirmLink(r.Context(), in.Token)
	})

	// ---------------------------------------------------------------- administration
	s.handle("GET /api/v1/admin/users/{id}/emails", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Auth.UserEmails(r.Context(), id)
	})
	s.handle("POST /api/v1/admin/users/{id}/emails", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Email    string `json:"email"`
			Verified bool   `json:"verified"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		e, err := s.app.Auth.AddUserEmail(r.Context(), id, in.Email, in.Verified)
		if err != nil {
			return nil, err
		}
		return h.Created{V: e}, nil
	})
	s.handle("POST /api/v1/admin/users/{id}/emails/{eid}/primary", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		eid, err := h.PathID(r, "eid")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.SetUserPrimaryEmail(r.Context(), id, eid)
	})
	s.handle("DELETE /api/v1/admin/users/{id}/emails/{eid}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		eid, err := h.PathID(r, "eid")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.RemoveUserEmail(r.Context(), id, eid)
	})
	s.handle("GET /api/v1/admin/users/{id}/identities", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if _, err := s.app.Auth.UserEmails(r.Context(), id); err != nil { // permission check
			return nil, err
		}
		return s.app.Auth.Identities(r.Context(), id)
	})
	s.handle("DELETE /api/v1/admin/users/{id}/identities", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		q := r.URL.Query()
		return h.NoContent{}, s.app.Auth.UnlinkUserIdentity(r.Context(), id, q.Get("provider"), q.Get("subject"))
	})
	s.handle("POST /api/v1/admin/users/invite", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in auth.Invite
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		u, err := s.app.Auth.InviteUser(r.Context(), in)
		if err != nil {
			return nil, err
		}
		return h.Created{V: u}, nil
	})
}
