package server

import (
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// routesElementRights: own rights of an element (rules, "own rights only",
// classification); see content.GetElementACL.
func (s *Server) routesElementRights() {
	// action: core.elements.acl.get
	s.handle("GET /api/v1/elements/{id}/acl", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.GetElementACL(r.Context(), id)
	})
	// action: core.elements.acl.set
	s.handle("PUT /api/v1/elements/{id}/acl", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.ElementRightsInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.SetElementRights(r.Context(), id, in)
	})
	// action: core.elements.acl.add
	s.handle("POST /api/v1/elements/{id}/acl", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.ElementRuleInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		rule, err := s.app.Content.AddElementRule(r.Context(), id, in)
		return h.Created{V: rule}, err
	})
	// action: core.elements.acl.update
	s.handle("PUT /api/v1/element-acl/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.ElementRuleInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.UpdateElementRule(r.Context(), id, in)
	})
	// action: core.elements.acl.remove
	s.handle("DELETE /api/v1/element-acl/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteElementRule(r.Context(), id)
	})
}
