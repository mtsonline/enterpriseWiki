package server

import (
	"net/http"
	"strconv"

	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

func (s *Server) routesM7() {
	// GET /api/v1/elements?q=&type=&category=&recursive=1&sort=title|updated|created|type&limit=&offset=&lang=
	// lists the elements the caller may view (all for administrators).
	s.handle("GET /api/v1/elements", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		cat, _ := strconv.ParseInt(q.Get("category"), 10, 64)
		list, total, err := s.app.Content.FindElements(r.Context(), content.FindOptions{Q: q.Get("q"), Type: q.Get("type"), Category: cat,
			Recursive: q.Get("recursive") != "0", Sort: q.Get("sort"), Lang: q.Get("lang"), Limit: h.QueryInt(r, "limit", 50), Offset: h.QueryInt(r, "offset", 0)})
		if err != nil {
			return nil, err
		}
		if list == nil {
			list = []content.FoundElement{}
		}
		return map[string]any{"items": list, "total": total}, nil
	})
	// GET /api/v1/resolve?path=kalender → {"type": "element"|"category", "id": 12}
	s.handle("GET /api/v1/resolve", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Resolve(r.Context(), r.URL.Query().Get("path"))
	})
}
