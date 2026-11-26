package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// routesCategories registers the routes of categories: tree, settings, permissions, translations, export and import.
func (s *Server) routesCategories() {
	// action: core.categories.list
	s.handle("GET /api/v1/categories", func(w http.ResponseWriter, r *http.Request) (any, error) {
		parent, _ := strconv.ParseInt(r.URL.Query().Get("parent"), 10, 64)
		return s.app.Content.ChildrenOpt(r.Context(), parent, r.URL.Query().Get("hidden") == "1")
	})
	// action: core.categories.tree
	s.handle("GET /api/v1/categories/tree", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Tree(r.Context(), h.QueryInt(r, "limit", 5000))
	})
	// action: core.categories.create
	s.handle("POST /api/v1/categories", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in content.CategoryInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		c, err := s.app.Content.CreateCategory(r.Context(), in)
		return h.Created{V: c}, err
	})
	// action: core.categories.get
	s.handle("GET /api/v1/categories/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		c, err := s.app.Content.GetCategory(r.Context(), id)
		if err != nil {
			return nil, err
		}
		bc, err := s.app.Content.Breadcrumbs(r.Context(), c)
		if err != nil {
			return nil, err
		}
		els := []content.Element{}
		if slices.Contains(c.Permissions, "view") { // category administrators: structure only
			if els, err = s.app.Content.ListElements(r.Context(), id); err != nil {
				return nil, err
			}
		}
		space, err := s.app.Content.SpaceOf(r.Context(), id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"category": c, "breadcrumbs": bc, "elements": els, "space": space}, nil
	})
	// GET /api/v1/categories/{id}/overview?lang=&page=&view= → one page of
	// the overview of the category with its display (only visible pages)
	// action: core.categories.overview
	s.handle("GET /api/v1/categories/{id}/overview", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		q := r.URL.Query()
		lang := q.Get("lang")
		if lang == "" {
			lang = s.app.Config.Get().I18n.DefaultLanguage
		}
		return s.app.Content.CategoryOverview(r.Context(), id, clip(lang, 20), h.QueryInt(r, "page", 1), clip(q.Get("view"), 20))
	})
	// spaces: the top-level categories (directory, address /s/KEY, settings)
	// action: core.spaces.list
	s.handle("GET /api/v1/spaces", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Spaces(r.Context())
	})
	// action: core.spaces.create
	s.handle("POST /api/v1/spaces", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in content.NewSpaceInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		sp, err := s.app.Content.CreateSpaceFrom(r.Context(), in)
		if err != nil {
			return nil, err
		}
		return h.Created{V: sp}, nil
	})
	// page views (counted per element and day only) and the evaluation of a category
	// action: core.elements.view
	s.handle("POST /api/v1/elements/{id}/view", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.CountView(r.Context(), id)
	})
	// action: core.categories.insights
	s.handle("GET /api/v1/categories/{id}/insights", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.Insights(r.Context(), id, h.QueryInt(r, "days", 30), r.URL.Query().Get("lang"))
	})
	// action: core.spaces.personal
	s.handle("POST /api/v1/spaces/personal", func(w http.ResponseWriter, r *http.Request) (any, error) {
		sp, created, err := s.app.Content.CreatePersonalSpace(r.Context())
		if created {
			return h.Created{V: sp}, err
		}
		return sp, err
	})
	// action: core.spaces.get
	s.handle("GET /api/v1/spaces/{key}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.SpaceByKey(r.Context(), r.PathValue("key"))
	})
	// action: core.categories.archive
	s.handle("POST /api/v1/categories/{id}/archive", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Archived bool `json:"archived"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.SetArchived(r.Context(), id, in.Archived)
	})
	// export a category tree as JSON Lines; import one below ?parent= (0 = new space)
	// action: core.categories.export
	h.RecordRoute("GET /api/v1/categories/{id}/export")
	s.app.Mux.HandleFunc("GET /api/v1/categories/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		id, err := h.PathID(r, "id")
		if err != nil {
			h.WriteError(w, r, err)
			return
		}
		ew := &lazyWriter{w: w, name: fmt.Sprintf("space-%d-%s.jsonl", id, time.Now().Format("20060102"))}
		if err := s.app.Content.ExportTree(r.Context(), id, ew); err != nil && !ew.started {
			h.WriteError(w, r, err)
		}
	})
	// action: core.categories.import
	s.handle("POST /api/v1/categories/import", func(w http.ResponseWriter, r *http.Request) (any, error) {
		parent, _ := strconv.ParseInt(r.URL.Query().Get("parent"), 10, 64)
		res, err := s.app.Content.ImportTree(r.Context(), parent, r.Body)
		if err != nil {
			return nil, err
		}
		return h.Created{V: res}, nil
	})
	// action: core.categories.space
	s.handle("PUT /api/v1/categories/{id}/space", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var raw json.RawMessage
		if err := h.Decode(r, &raw); err != nil {
			return nil, err
		}
		var in content.SpaceInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, apperr.BadRequest("invalid JSON: %v", err)
		}
		// which settings were sent (the others stay)
		var keys map[string]json.RawMessage
		_ = json.Unmarshal(raw, &keys)
		in.Present = map[string]bool{}
		for k := range keys {
			in.Present[k] = true
		}
		return s.app.Content.SetSpace(r.Context(), id, in)
	})
	// action: core.categories.settings.get
	s.handle("GET /api/v1/categories/{id}/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.GetCategorySettings(r.Context(), id)
	})
	// action: core.categories.settings.set
	s.handle("PUT /api/v1/categories/{id}/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.Space
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.SetCategorySettings(r.Context(), id, in)
	})
	// action: core.categories.update
	s.handle("PUT /api/v1/categories/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.CategoryInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.UpdateCategory(r.Context(), id, in)
	})
	// PUT {"title": "…", "description": "…"} translates the category into lang; {"confirm": true} marks an outdated translation as current
	// action: core.categories.translate
	s.handle("PUT /api/v1/categories/{id}/translations/{lang}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.CatTranslationInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.TranslateCategory(r.Context(), id, r.PathValue("lang"), in)
	})
	// action: core.categories.delete
	s.handle("DELETE /api/v1/categories/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteCategory(r.Context(), id)
	})
	// action: core.categories.move
	s.handle("POST /api/v1/categories/{id}/move", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			ParentID int64 `json:"parent_id"`
			Position *int  `json:"position"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.MoveCategory(r.Context(), id, in.ParentID, in.Position)
	})
	// action: core.categories.duplicate
	s.handle("POST /api/v1/categories/{id}/duplicate", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			ParentID int64 `json:"parent_id"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		c, err := s.app.Content.DuplicateCategory(r.Context(), id, in.ParentID)
		return h.Created{V: c}, err
	})
	// action: core.categories.reorder
	s.handle("POST /api/v1/categories/{id}/reorder", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Order []int64 `json:"order"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.Reorder(r.Context(), id, in.Order)
	})
	// action: core.categories.acl.list
	s.handle("GET /api/v1/categories/{id}/acl", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.GetACL(r.Context(), id)
	})
	// action: core.categories.acl.add
	s.handle("POST /api/v1/categories/{id}/acl", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.RuleInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		rule, err := s.app.Content.AddRule(r.Context(), id, in)
		return h.Created{V: rule}, err
	})
	// action: core.categories.acl.remove
	s.handle("DELETE /api/v1/acl/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Content.DeleteRule(r.Context(), id)
	})
	// action: core.categories.history
	s.handle("GET /api/v1/categories/{id}/history", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Content.CategoryHistory(r.Context(), id, h.QueryInt(r, "limit", 200))
	})
	// action: core.categories.rollback
	s.handle("POST /api/v1/categories/{id}/rollback", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.RollbackInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Content.RollbackCategory(r.Context(), id, in)
	})
}

// lazyWriter sets the download headers on the first write, so that errors
// before any output can still be answered as JSON.
type lazyWriter struct {
	w       http.ResponseWriter
	name    string
	started bool
}

func (l *lazyWriter) Write(p []byte) (int, error) {
	if !l.started {
		l.started = true
		l.w.Header().Set("Content-Type", "application/x-ndjson")
		l.w.Header().Set("Content-Disposition", `attachment; filename="`+l.name+`"`)
	}
	return l.w.Write(p)
}
