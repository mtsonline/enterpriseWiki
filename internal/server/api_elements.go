package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/limits"
)

// routesElements registers the routes of elements (pages and other types): content, versions, translations, moving.
func (s *Server) routesElements() {
	// action: core.elements.types
	s.handle("GET /api/v1/element-types", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.Content.Types(), nil })
	// action: core.elements.create
	s.handle("POST /api/v1/elements", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in content.ElementInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		e, err := s.app.Content.CreateElement(r.Context(), in)
		return h.Created{V: e}, err
	})
	// action: core.elements.preview
	s.handle("POST /api/v1/elements/preview", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Type string          `json:"type"`
			Lang string          `json:"lang"`
			Body json.RawMessage `json:"body"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		html, err := s.app.Content.Preview(r.Context(), in.Type, in.Lang, in.Body)
		return map[string]string{"html": html}, err
	})
	// action: core.elements.get
	s.handle("GET /api/v1/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		if idx, page, ok := macroPageParam(r.URL.Query().Get("mp")); ok {
			ctx = content.WithMacroPage(ctx, id, idx, page) // a page of a list (link ?mp=INDEX-PAGE)
		}
		v, err := s.app.Content.GetElement(ctx, id, r.URL.Query().Get("lang"), r.URL.Query().Get("edit") == "1")
		if err == nil && s.app.Config.Get().Audit.LogViews {
			_ = audit.Log(r.Context(), s.app.DB, "element.view", "element", id, nil, map[string]string{"lang": v.ShownLang})
		}
		return v, err
	})
	// GET /api/v1/elements/batch?ids=1,2,3&lang= → the pages (as GET
	// /elements/{id}) the caller may view, in the order asked for; others
	// are left out without saying why (featured pages of a category page).
	// At most core.categories.featured ids.
	// action: core.elements.batch
	s.handle("GET /api/v1/elements/batch", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		// enough for the featured pages and for the whole pages of an overview
		max := max(limits.Value(s.app.Config.Get(), limits.CategoryFeatured), limits.Value(s.app.Config.Get(), limits.CategoryFull))
		ids := []int64{}
		for _, part := range strings.Split(q.Get("ids"), ",") {
			if part = strings.TrimSpace(part); part == "" {
				continue
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || id <= 0 {
				return nil, apperr.BadRequest("ids: element ids separated by commas")
			}
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		if int64(len(ids)) > max {
			return nil, apperr.BadRequest("at most %d ids (expert settings %s and %s)", max, limits.CategoryFeatured, limits.CategoryFull)
		}
		ctx := r.Context()
		items := []*content.ElementView{}
		// bounded by the limit above; each page is rendered with its own checks
		for _, id := range ids {
			v, err := s.app.Content.GetElement(ctx, id, q.Get("lang"), false)
			if err != nil {
				if code, _, _ := apperr.Status(err); code >= 500 {
					return nil, err
				}
				continue // not visible or gone: left out
			}
			if s.app.Config.Get().Audit.LogViews {
				_ = audit.Log(ctx, s.app.DB, "element.view", "element", id, nil, map[string]string{"lang": v.ShownLang})
			}
			items = append(items, v)
		}
		return map[string]any{"items": items}, nil
	})
	// preview of sources for their editor: count and first titles with the caller's rights
	// action: core.sources.preview
	s.handle("POST /api/v1/sources/preview", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if _, err := requireUser(r); err != nil {
			return nil, err
		}
		var in struct {
			Sources    string `json:"sources"`
			CategoryID int64  `json:"category_id"`
			Lang       string `json:"lang"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		list, err := content.ParseSources(in.Sources)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"total": 0, "items": []map[string]any{}}
		if list = content.WithDefault(list, in.CategoryID); len(list) == 0 {
			return out, nil
		}
		set, err := s.app.Content.ResolveSources(r.Context(), list)
		if err != nil || set.Empty() {
			return out, err
		}
		items, total, err := s.app.Content.ListIn(r.Context(), content.ListOptions{Sources: set, Lang: in.Lang, Order: "updated", Limit: 5, SummaryLen: 1})
		if err != nil {
			return nil, err
		}
		shown := []map[string]any{}
		for _, e := range items {
			shown = append(shown, map[string]any{"id": e.ID, "title": e.Title})
		}
		out["total"], out["items"] = total, shown
		return out, nil
	})
	// one macro of an element again at another page of its list
	// action: core.elements.macro
	s.handle("GET /api/v1/elements/{id}/macro/{index}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		idx, err := h.PathID(r, "index")
		if err != nil || idx > 200 {
			return nil, apperr.BadRequest("index")
		}
		page := h.QueryInt(r, "page", 1)
		if page < 1 || page > 100000 {
			page = 1
		}
		out, err := s.app.Content.RenderMacroPage(r.Context(), id, int(idx), page, r.URL.Query().Get("lang"))
		if err != nil {
			return nil, err
		}
		if out == "" {
			return nil, apperr.NotFound("macro")
		}
		return map[string]string{"html": out}, nil
	})
	// action: core.elements.update
	s.handle("PUT /api/v1/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.SaveInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.SaveElement(r.Context(), id, in)
	})
	// action: core.elements.patch
	s.handle("PATCH /api/v1/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.MetaInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.UpdateElementMeta(r.Context(), id, in)
	})
	// action: core.elements.delete
	s.handle("DELETE /api/v1/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteElement(r.Context(), id)
	})
	// action: core.elements.move
	s.handle("POST /api/v1/elements/{id}/move", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			CategoryID int64 `json:"category_id"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.MoveElement(r.Context(), id, in.CategoryID)
	})
	// action: core.elements.duplicate
	s.handle("POST /api/v1/elements/{id}/duplicate", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			CategoryID int64 `json:"category_id"`
			// WithRights copies the element's own rules and "own rights only"
			WithRights bool `json:"with_rights"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		e, err := s.app.Content.DuplicateElement(r.Context(), id, in.CategoryID, in.WithRights)
		return h.Created{V: e}, err
	})
	// action: core.elements.revisions
	s.handle("GET /api/v1/elements/{id}/revisions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.Revisions(r.Context(), id, r.URL.Query().Get("lang"))
	})
	// action: core.elements.revision
	s.handle("GET /api/v1/elements/{id}/revisions/{lang}/{rev}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		rev, err := h.PathID(r, "rev")
		if err != nil {
			return nil, err
		}
		return s.app.Content.GetRevision(r.Context(), id, r.PathValue("lang"), int(rev))
	})
	// action: core.elements.restore
	s.handle("POST /api/v1/elements/{id}/restore", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Lang string `json:"lang"`
			Rev  int    `json:"rev"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.RestoreRevision(r.Context(), id, in.Lang, in.Rev)
	})
}

// macroPageParam reads "INDEX-PAGE" (a page of a macro list).
func macroPageParam(v string) (index, page int, ok bool) {
	a, b, found := strings.Cut(v, "-")
	if !found {
		return 0, 0, false
	}
	i, err1 := strconv.Atoi(a)
	p, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || i < 1 || i > 200 || p < 1 || p > 100000 {
		return 0, 0, false
	}
	return i, p, true
}

// routesMacroList registers the list of blocks (macros) the editors offer.
func (s *Server) routesMacroList() {
	// ---------------------------------------------------------------- macros
	// Lists the macros of active plugins (used by the editor's macro dialog).
	// action: core.macros
	s.handle("GET /api/v1/macros", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Macros(), nil
	})
}
