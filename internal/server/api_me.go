package server

import (
	"cmp"
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/readability"
)

// routesMe registers the routes of the signed-in person: own account, API keys, SSH keys, preferences.
func (s *Server) routesMe() {
	// action: core.me.update
	s.handle("PUT /api/v1/me", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			DisplayName *string `json:"display_name"`
			Email       *string `json:"email"`
			Language    *string `json:"language"`
			// Prefs changes personal settings ("" removes one).
			Prefs map[string]string `json:"prefs"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := s.app.Auth.UpdateProfile(r.Context(), in.DisplayName, in.Email, in.Language); err != nil {
			return nil, err
		}
		if in.Prefs != nil {
			if err := s.app.Auth.SetPrefs(r.Context(), in.Prefs); err != nil {
				return nil, err
			}
		}
		return h.NoContent{}, nil
	})
	// action: core.me.prefs
	s.handle("GET /api/v1/me/prefs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p := identity.From(r.Context())
		if p == nil || p.UserID == 0 {
			return nil, apperr.ErrUnauthorized
		}
		cur := p.Prefs
		if cur == nil {
			cur = map[string]string{}
		}
		// the hint on long sentences: what the site offers and whether it
		// is on for this person
		mode := cmp.Or(s.app.Config.Get().UI.Readability.Mode, readability.ModeOff)
		return map[string]any{"prefs": cur, "options": auth.PrefOptions(),
			"readability": map[string]any{"site": mode, "active": readability.Active(mode, cur["readability"])}}, nil
	})
	// action: core.me.password
	s.handle("PUT /api/v1/me/password", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Current string `json:"current"`
			New     string `json:"new"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.ChangeOwnPassword(r.Context(), in.Current, in.New, s.sessionToken(r))
	})
	// action: core.me.api_keys.list
	s.handle("GET /api/v1/me/api-keys", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Auth.ListAPIKeys(r.Context()) })
	// action: core.me.api_keys.create
	s.handle("POST /api/v1/me/api-keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in auth.NewAPIKey
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		k, err := s.app.Auth.CreateAPIKey(r.Context(), in)
		return h.Created{V: k}, err
	})
	// action: core.me.api_keys.delete
	s.handle("DELETE /api/v1/me/api-keys/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.RevokeAPIKey(r.Context(), id)
	})
	// action: core.me.ssh_keys.list
	s.handle("GET /api/v1/me/ssh-keys", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Auth.ListSSHKeys(r.Context()) })
	// action: core.me.ssh_keys.add
	s.handle("POST /api/v1/me/ssh-keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Name      string `json:"name"`
			PublicKey string `json:"public_key"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		k, err := s.app.Auth.AddSSHKey(r.Context(), in.Name, in.PublicKey)
		return h.Created{V: k}, err
	})
	// action: core.me.ssh_keys.delete
	s.handle("DELETE /api/v1/me/ssh-keys/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.DeleteSSHKey(r.Context(), id)
	})
}
