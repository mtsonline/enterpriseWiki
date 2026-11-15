package server

import (
	"net/http"
	"strconv"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/watch"
)

func (s *Server) routesDrafts() {
	// ---------------------------------------------------------------- drafts
	// GET /api/v1/drafts?category=ID|element=ID – drafts the caller may see (without both: own drafts)
	// action: core.drafts.list
	s.handle("GET /api/v1/drafts", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Drafts(r.Context(), int64(h.QueryInt(r, "category", 0)), int64(h.QueryInt(r, "element", 0)))
	})
	// action: core.drafts.create
	s.handle("POST /api/v1/drafts", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in content.DraftInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		d, err := s.app.Content.SaveDraft(r.Context(), 0, in)
		return h.Created{V: d}, err
	})
	// action: core.drafts.get
	s.handle("GET /api/v1/drafts/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.GetDraft(r.Context(), id)
	})
	// action: core.drafts.update
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
	// action: core.drafts.delete
	s.handle("DELETE /api/v1/drafts/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteDraft(r.Context(), id)
	})
	// POST /api/v1/drafts/{id}/publish {category_id, at, notify, options}
	// action: core.drafts.publish
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
	// action: core.drafts.unschedule
	s.handle("DELETE /api/v1/drafts/{id}/schedule", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.Unschedule(r.Context(), id)
	})

	// ---------------------------------------------------------------- templates
	// action: core.templates.list
	s.handle("GET /api/v1/templates", func(w http.ResponseWriter, r *http.Request) (any, error) {
		cat, _ := strconv.ParseInt(r.URL.Query().Get("category"), 10, 64)
		return s.app.Content.Templates(r.Context(), r.URL.Query().Get("type"), 0, cat)
	})
	// action: core.templates.get
	s.handle("GET /api/v1/templates/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		list, err := s.app.Content.Templates(r.Context(), "", id, 0)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, apperr.NotFound("template")
		}
		return list[0], nil
	})
	// action: core.templates.create
	s.handle("POST /api/v1/templates", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in content.TemplateInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		t, err := s.app.Content.SaveTemplate(r.Context(), 0, in)
		return h.Created{V: t}, err
	})
	// action: core.templates.update
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
	// action: core.templates.delete
	s.handle("DELETE /api/v1/templates/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteTemplate(r.Context(), id)
	})
}

func (s *Server) routesNotifications() {
	// action: core.me.inbox.list
	s.handle("GET /api/v1/me/inbox", func(w http.ResponseWriter, r *http.Request) (any, error) {
		list, unread, err := s.app.Notify.List(r.Context(), h.QueryInt(r, "limit", 30))
		return map[string]any{"items": list, "unread": unread}, err
	})
	// action: core.me.inbox.read
	s.handle("POST /api/v1/me/inbox/read", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			IDs []int64 `json:"ids"` // empty: all
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if len(in.IDs) == 0 {
			in.IDs = nil
		}
		return h.NoContent{}, s.app.Notify.MarkRead(r.Context(), in.IDs)
	})
	// action: core.me.inbox.delete
	s.handle("DELETE /api/v1/me/inbox/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Notify.Delete(r.Context(), id)
	})
}

func (s *Server) routesWatchAssignments() {
	// action: core.watches.assign.list
	s.handle("GET /api/v1/admin/watch-assignments", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Watch.Assignments(r.Context())
	})
	// action: core.watches.assign.add
	s.handle("POST /api/v1/admin/watch-assignments", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in watch.AssignmentInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		a, err := s.app.Watch.SaveAssignment(r.Context(), 0, in)
		return h.Created{V: a}, err
	})
	// action: core.watches.assign.update
	s.handle("PUT /api/v1/admin/watch-assignments/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in watch.AssignmentInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Watch.SaveAssignment(r.Context(), id, in)
	})
	// action: core.watches.assign.remove
	s.handle("DELETE /api/v1/admin/watch-assignments/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Watch.DeleteAssignment(r.Context(), id)
	})
	// action: core.me.watch_assignments.list
	s.handle("GET /api/v1/me/watch-assignments", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Watch.MyAssignments(r.Context())
	})
	// action: core.me.watch_assignments.set
	s.handle("PUT /api/v1/me/watch-assignments/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Subscribed bool `json:"subscribed"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Watch.SetSubscribed(r.Context(), id, in.Subscribed)
	})
}
