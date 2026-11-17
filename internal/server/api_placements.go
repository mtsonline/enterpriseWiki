package server

import (
	"net/http"
	"strconv"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// routesPlacements: where the building blocks of the interface appear.
func (s *Server) routesPlacements() {
	// GET /api/v1/placements?template=&category= – the levels that apply to
	// a page (template, site, category and its ancestors, own if allowed)
	// action: core.placements.resolve
	s.handle("GET /api/v1/placements", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		var cat int64
		if v := q.Get("category"); v != "" {
			var err error
			if cat, err = strconv.ParseInt(v, 10, 64); err != nil || cat < 0 {
				return nil, apperr.BadRequest("category must be a category id")
			}
		}
		return s.app.Content.ResolvedPlacements(r.Context(), q.Get("template"), cat)
	})
	// action: core.placements.get
	s.handle("GET /api/v1/placements/{level}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.GetPlacements(r.Context(), r.PathValue("level"), r.URL.Query().Get("target"))
	})
	// action: core.placements.set
	s.handle("PUT /api/v1/placements/{level}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Placements []content.Placement `json:"placements"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if in.Placements == nil {
			return nil, apperr.BadRequest("placements is required (an empty list removes all placements of the level)")
		}
		return s.app.Content.SetPlacements(r.Context(), r.PathValue("level"), r.URL.Query().Get("target"), in.Placements)
	})
}
