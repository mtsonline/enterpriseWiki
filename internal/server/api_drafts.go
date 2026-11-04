package server

import (
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

func (s *Server) routesDrafts() {
	// ---------------------------------------------------------------- drafts
	// GET /api/v1/drafts?category=ID|element=ID – drafts the caller may see (without both: own drafts)
	s.handle("GET /api/v1/drafts", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Drafts(r.Context(), int64(h.QueryInt(r, "category", 0)), int64(h.QueryInt(r, "element", 0)))
	})
	s.handle("POST /api/v1/drafts", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in content.DraftInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		d, err := s.app.Content.SaveDraft(r.Context(), 0, in)
		return h.Created{V: d}, err
	})
	s.handle("GET /api/v1/drafts/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.GetDraft(r.Context(), id)
	})
	s.handle("PUT /api/v1/drafts/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.DraftInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.SaveDraft(r.Context(), id, in)
	})
	s.handle("DELETE /api/v1/drafts/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteDraft(r.Context(), id)
	})
	// POST /api/v1/drafts/{id}/publish {category_id, at, notify, options}
	s.handle("POST /api/v1/drafts/{id}/publish", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.PublishInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.PublishDraft(r.Context(), id, in)
	})
	s.handle("DELETE /api/v1/drafts/{id}/schedule", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.Unschedule(r.Context(), id)
	})

	// ---------------------------------------------------------------- templates
	s.handle("GET /api/v1/templates", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Templates(r.Context(), r.URL.Query().Get("type"), 0)
	})
	s.handle("GET /api/v1/templates/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		list, err := s.app.Content.Templates(r.Context(), "", id)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, apperr.NotFound("template")
		}
		return list[0], nil
	})
	s.handle("POST /api/v1/templates", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in content.TemplateInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		t, err := s.app.Content.SaveTemplate(r.Context(), 0, in)
		return h.Created{V: t}, err
	})
	s.handle("PUT /api/v1/templates/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.TemplateInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.SaveTemplate(r.Context(), id, in)
	})
	s.handle("DELETE /api/v1/templates/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteTemplate(r.Context(), id)
	})
}
