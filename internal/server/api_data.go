package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// routesData: the data of blocks and plugins for reports (content/data.go).
func (s *Server) routesData() {
	// the kinds of data with their schema and how far they are built
	// action: core.data.kinds
	s.handle("GET /api/v1/data", func(w http.ResponseWriter, r *http.Request) (any, error) {
		type kind struct {
			content.DataKind
			Complete bool `json:"complete"`
		}
		done, err := s.dataComplete(r)
		if err != nil {
			return nil, err
		}
		out := []kind{}
		for _, k := range s.app.Content.DataKinds() {
			out = append(out, kind{k, done[k.Kind] == k.Version})
		}
		return out, nil
	})
	// the entries of a kind the caller may see
	// action: core.data.query
	s.handle("GET /api/v1/data/{kind}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		id := func(name string) (int64, error) {
			v := strings.TrimSpace(q.Get(name))
			if v == "" {
				return 0, nil
			}
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 1 {
				return 0, apperr.BadRequest("%s: a number", name)
			}
			return n, nil
		}
		dq := content.DataQuery{Kind: r.PathValue("kind"), Lang: q.Get("lang"), From: q.Get("from"), To: q.Get("to"), Q: q.Get("q"), Sort: q.Get("sort"),
			Direct: q.Get("direct") == "true", Hidden: q.Get("hidden") == "true", Limit: h.QueryInt(r, "limit", 50), Offset: h.QueryInt(r, "offset", 0)}
		var err error
		if dq.Category, err = id("category"); err != nil {
			return nil, err
		}
		if dq.Element, err = id("element"); err != nil {
			return nil, err
		}
		if dq.Person, err = id("person"); err != nil {
			return nil, err
		}
		if dq.Ref, err = id("ref"); err != nil {
			return nil, err
		}
		for _, st := range strings.Split(q.Get("state"), ",") {
			if st = strings.TrimSpace(st); st != "" {
				dq.States = append(dq.States, st)
			}
		}
		return s.app.Content.QueryData(r.Context(), dq)
	})
	// build the entries of a kind (or of all kinds) again from the latest revisions
	// action: core.data.rebuild
	s.handle("POST /api/v1/admin/data/rebuild", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		var in struct {
			Kind string `json:"kind"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := s.app.Content.ResetData(r.Context(), in.Kind); err != nil {
			return nil, err
		}
		s.app.Workers.Trigger("data.rebuild")
		return map[string]any{"ok": true}, nil
	})
}

// dataComplete: the version each kind is completely built for.
func (s *Server) dataComplete(r *http.Request) (map[string]int, error) {
	rows, err := s.app.DB.Query(r.Context(), `SELECT kind, version FROM data_kinds WHERE done`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var v int
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
