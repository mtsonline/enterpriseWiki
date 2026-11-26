package server

import (
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/auth"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// routesProfile registers the profile (business card) of people: the own,
// the person page and the administration of a user's profile.
func (s *Server) routesProfile() {
	// ---------------------------------------------------------------- profile (business card)
	// action: core.me.profile.show
	s.handle("GET /api/v1/me/profile", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		prof, err := s.app.Auth.Profile(r.Context(), p.ID())
		return map[string]any{"profile": prof, "fields": auth.ProfileFields, "photo_policy": s.app.Auth.PhotoPolicy(), "email_policy": s.app.Auth.EmailPolicy()}, err
	})
	// GET /api/v1/people/{id} – what the caller may see of a person
	// (business card, e-mail address only if allowed)
	// action: core.people.get
	s.handle("GET /api/v1/people/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Auth.Person(r.Context(), id)
	})
	// action: core.me.profile.set
	s.handle("PUT /api/v1/me/profile", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		var in map[string]string
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Auth.SetProfile(r.Context(), p.ID(), in)
	})
	// action: core.users.profile.show
	s.handle("GET /api/v1/admin/users/{id}/profile", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		prof, err := s.app.Auth.Profile(r.Context(), id)
		return map[string]any{"profile": prof, "fields": auth.ProfileFields, "photo_policy": s.app.Auth.PhotoPolicy(), "email_policy": s.app.Auth.EmailPolicy()}, err
	})
	// action: core.users.profile.set
	s.handle("PUT /api/v1/admin/users/{id}/profile", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in map[string]string
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Auth.SetProfile(r.Context(), id, in)
	})
}
