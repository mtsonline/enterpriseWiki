package server

import (
	"net/http"
	"strconv"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/watch"
)

func (s *Server) routesM6() {
	// ---------------------------------------------------------------- watches
	s.handle("GET /api/v1/me/watches", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		if t := q.Get("target_type"); t != "" {
			id, err := strconv.ParseInt(q.Get("target_id"), 10, 64)
			if err != nil {
				return nil, apperr.BadRequest("target_id required")
			}
			wt, err := s.app.Watch.Get(r.Context(), t, id)
			if err != nil {
				return nil, err
			}
			return map[string]any{"watch": wt, "enabled": s.app.Mail.Enabled()}, nil
		}
		list, err := s.app.Watch.List(r.Context())
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": list, "enabled": s.app.Mail.Enabled()}, nil
	})
	s.handle("PUT /api/v1/me/watches", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in watch.Input
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Watch.Set(r.Context(), in)
	})
	s.handle("DELETE /api/v1/me/watches/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Watch.Delete(r.Context(), id)
	})
}
