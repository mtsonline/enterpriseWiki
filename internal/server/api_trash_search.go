package server

import (
	"net/http"
	"strconv"

	h "github.com/mtsonline/enterprisewiki/internal/httpx"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

// routesTrashSearch registers the routes of trash, full text search and tags.
func (s *Server) routesTrashSearch() {
	// action: core.trash.list
	s.handle("GET /api/v1/trash", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Content.Trash(r.Context()) })
	// action: core.trash.restore
	s.handle("POST /api/v1/trash/{type}/{id}/restore", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.Restore(r.Context(), r.PathValue("type"), id)
	})
	// action: core.trash.purge
	s.handle("DELETE /api/v1/trash/{type}/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.Purge(r.Context(), r.PathValue("type"), id)
	})
	// action: core.elements.recent
	s.handle("GET /api/v1/recent", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		limit := 10
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 50 {
				return nil, apperr.BadRequest("limit must be between 1 and 50")
			}
			limit = n
		}
		switch q.Get("by") {
		case "":
			var cat int64
			if v := q.Get("category"); v != "" {
				var err error
				if cat, err = strconv.ParseInt(v, 10, 64); err != nil || cat < 0 {
					return nil, apperr.BadRequest("category must be a category id")
				}
			}
			return s.app.Content.Recent(r.Context(), cat, limit)
		case "me":
			return s.app.Content.RecentlyEditedByMe(r.Context(), limit)
		}
		return nil, apperr.BadRequest(`by must be empty or "me"`)
	})
	// action: core.search
	s.handle("GET /api/v1/search", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		var scope []int64
		if c := q.Get("category"); c != "" {
			id, err := strconv.ParseInt(c, 10, 64)
			if err != nil {
				return nil, apperr.BadRequest("invalid category")
			}
			// category=ID searches in the category and its subcategories (recursive=0: only the category)
			if scope, err = s.app.Content.Scope(r.Context(), id, q.Get("recursive") != "0"); err != nil {
				return nil, err
			}
		}
		return s.app.Content.SearchIn(r.Context(), q.Get("q"), h.QueryList(r, "tags"), q.Get("lang"), h.QueryInt(r, "limit", 30), scope)
	})
	// action: core.tags
	s.handle("GET /api/v1/tags", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Tags(r.Context(), r.URL.Query().Get("prefix"), h.QueryInt(r, "limit", 50))
	})
}
