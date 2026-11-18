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
	// GET /api/v1/elements/{id}/labels – the texts the instance may change
	// (per active language: the plugin's text and the instance's own)
	// action: core.instances.labels.get
	s.handle("GET /api/v1/elements/{id}/labels", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		plugin, manage, err := s.app.Content.InstanceLabelsOf(r.Context(), id)
		if err != nil {
			return nil, err
		}
		labels, err := s.app.I18n.InstanceLabels(r.Context(), plugin, id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"labels": labels, "languages": s.app.Config.Get().I18n.Languages, "can_edit": manage}, nil
	})
	// PUT /api/v1/elements/{id}/labels {"values": {"de": {"content.blog.new_post": "Neuer Artikel"}}}
	// – "" removes a text (the plugin's text applies); manage of the instance
	// action: core.instances.labels.set
	s.handle("PUT /api/v1/elements/{id}/labels", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Values map[string]map[string]string `json:"values"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		plugin, manage, err := s.app.Content.InstanceLabelsOf(r.Context(), id)
		if err != nil {
			return nil, err
		}
		if !manage {
			return nil, apperr.Forbidden("changing the labels needs the permission to manage this instance")
		}
		if err := s.app.I18n.SetInstanceLabels(r.Context(), plugin, id, in.Values); err != nil {
			return nil, err
		}
		labels, err := s.app.I18n.InstanceLabels(r.Context(), plugin, id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"labels": labels, "languages": s.app.Config.Get().I18n.Languages, "can_edit": true}, nil
	})
}
