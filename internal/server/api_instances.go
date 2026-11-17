package server

import (
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// routesInstances: permissions of one instance of a plugin (a blog, a FAQ,
// a form …), see acl.Service.CanInstance.
func (s *Server) routesInstances() {
	// action: core.instances.rights.get
	s.handle("GET /api/v1/elements/{id}/rights", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.InstanceRights(r.Context(), id)
	})
	// action: core.instances.rights.set
	s.handle("PUT /api/v1/elements/{id}/rights", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Rules []content.InstanceRuleInput `json:"rules"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if in.Rules == nil {
			return nil, apperr.BadRequest("rules is required (an empty list removes all rules)")
		}
		return s.app.Content.SetInstanceRights(r.Context(), id, in.Rules)
	})
}
