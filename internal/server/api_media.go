package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/media"
	"github.com/mtsonline/enterprisewiki/internal/storage"
)

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.BadRequest("invalid id")
	}
	return id, nil
}

func qInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return max(n, 0)
}

// mediaMeta is the JSON part of an upload.
type mediaMeta struct {
	CategoryID  int64               `json:"category_id"`
	Title       string              `json:"title"`
	Alt         string              `json:"alt"`
	Description string              `json:"description"`
	Download    *bool               `json:"download"`
	Watermark   media.WatermarkSpec `json:"watermark"`
	ACL         []media.Rule        `json:"acl"`
	Note        string              `json:"note"`
}

// readUpload reads a multipart upload: an optional field "meta" (JSON)
// before the field "file"; fn receives the file.
func (s *Server) readUpload(w http.ResponseWriter, r *http.Request, fn func(meta mediaMeta, name string, body io.Reader) (any, error)) (any, error) {
	p := identity.From(r.Context())
	if p == nil {
		return nil, apperr.ErrUnauthorized
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.app.Files.MaxBytes()+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, apperr.BadRequest("expected multipart/form-data with the fields \"meta\" (JSON) and \"file\"")
	}
	var meta mediaMeta
	for {
		part, err := mr.NextPart()
		if err != nil {
			return nil, apperr.BadRequest("no field \"file\" in the upload")
		}
		switch part.FormName() {
		case "meta":
			dec := json.NewDecoder(io.LimitReader(part, 1<<20))
			dec.DisallowUnknownFields()
			err := dec.Decode(&meta)
			_ = part.Close()
			if err != nil {
				return nil, apperr.BadRequest("meta: %v", err)
			}
		case "file":
			res, err := fn(meta, part.FileName(), part)
			_ = part.Close()
			return res, err
		default:
			_ = part.Close()
		}
	}
}

func (s *Server) routesMedia() {
	m := func() *media.Service { return s.app.Media }

	// action: core.media.list
	s.handle("GET /api/v1/media", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		cat, _ := strconv.ParseInt(q.Get("category"), 10, 64)
		var usedBy int64
		if v := q.Get("used_by"); v != "" {
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil || id <= 0 {
				return nil, apperr.BadRequest("invalid used_by")
			}
			// only for a page the caller may see (it says which files the page shows)
			if _, err := s.app.Content.ElementCategory(r.Context(), id, acl.View); err != nil {
				return nil, err
			}
			usedBy = id
		}
		return m().List(r.Context(), media.ListInput{CategoryID: cat, Recursive: q.Get("recursive") == "1", Query: q.Get("q"),
			Type: q.Get("type"), UsedBy: usedBy, Sort: q.Get("sort"), Limit: qInt(r, "limit"), Offset: qInt(r, "offset")})
	})
	// action: core.media.upload
	s.handle("POST /api/v1/media", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.readUpload(w, r, func(meta mediaMeta, name string, body io.Reader) (any, error) {
			if meta.CategoryID <= 0 {
				return nil, apperr.BadRequest("meta.category_id is required (send the field \"meta\" before \"file\")")
			}
			md, err := m().Upload(r.Context(), media.UploadInput{CategoryID: meta.CategoryID, Name: name, Title: meta.Title, Alt: meta.Alt,
				Description: meta.Description, Download: meta.Download, Watermark: meta.Watermark, ACL: meta.ACL}, body)
			if err != nil {
				return nil, err
			}
			return h.Created{V: md}, nil
		})
	})
	// action: core.media.get
	s.handle("GET /api/v1/media/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return m().Get(r.Context(), id)
	})
	// action: core.media.update
	s.handle("PATCH /api/v1/media/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		var in media.UpdateInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return m().Update(r.Context(), id, in)
	})
	// action: core.media.delete
	s.handle("DELETE /api/v1/media/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, m().Delete(r.Context(), id)
	})
	// action: core.media.replace
	s.handle("POST /api/v1/media/{id}/file", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.readUpload(w, r, func(meta mediaMeta, _ string, body io.Reader) (any, error) {
			return m().Replace(r.Context(), id, body, meta.Note)
		})
	})
	// action: core.media.edit
	s.handle("POST /api/v1/media/{id}/edit", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		var in struct {
			Ops  []media.Op `json:"ops"`
			Note string     `json:"note"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return m().EditImage(r.Context(), id, in.Ops, in.Note)
	})
	// action: core.media.versions
	s.handle("GET /api/v1/media/{id}/versions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return m().Versions(r.Context(), id)
	})
	// action: core.media.restore
	s.handle("POST /api/v1/media/{id}/versions/{v}/restore", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		v, err := strconv.Atoi(r.PathValue("v"))
		if err != nil || v <= 0 {
			return nil, apperr.BadRequest("invalid version")
		}
		return m().Restore(r.Context(), id, v)
	})
	// action: core.media.usage
	s.handle("GET /api/v1/media/{id}/usage", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return m().Usage(r.Context(), id)
	})
	// action: core.media.acl
	s.handle("GET /api/v1/media/{id}/acl", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return m().Rules(r.Context(), id)
	})

	deliver := func(download bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id, err := pathID(r)
			if err != nil {
				h.WriteError(w, r, err)
				return
			}
			d, err := m().Deliver(r.Context(), id, media.DeliverInput{Download: download, Version: qInt(r, "v"), Width: qInt(r, "w"), Page: qInt(r, "page")})
			if err != nil {
				h.WriteError(w, r, err)
				return
			}
			if d.Data == nil && d.Pages > 0 { // PDF shown as page images
				w.Header().Set("Cache-Control", "private, no-store")
				h.Write(w, http.StatusOK, map[string]any{"pages": d.Pages, "view_only": d.ViewOnly})
				return
			}
			// restricted or personalised content is never cached
			cacheable := !d.ViewOnly && r.URL.Query().Get("v") == ""
			storage.ServeBytes(w, r, d.Data, d.Mime, d.Name, d.Inline, cacheable)
		}
	}
	// action: core.media.view
	h.RecordRoute("GET /api/v1/media/{id}/view")
	s.app.Mux.HandleFunc("GET /api/v1/media/{id}/view", deliver(false))
	// action: core.media.download
	h.RecordRoute("GET /api/v1/media/{id}/download")
	s.app.Mux.HandleFunc("GET /api/v1/media/{id}/download", deliver(true))
}

// legacyRefs caches which categories reference a file hash in element
// content (images inserted before the media library).
type legacyRefs struct {
	mu sync.Mutex
	m  map[string]legacyEntry
}

type legacyEntry struct {
	cats []int64
	at   time.Time
}

func (l *legacyRefs) get(key string) ([]int64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.m[key]
	if !ok || time.Since(e.at) > time.Minute {
		return nil, false
	}
	return e.cats, true
}

// clear forgets everything: a page that embeds a file was created,
// changed, moved or deleted.
func (l *legacyRefs) clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m = nil
}

func (l *legacyRefs) put(key string, cats []int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil || len(l.m) > 5000 {
		l.m = map[string]legacyEntry{}
	}
	l.m[key] = legacyEntry{cats, time.Now()}
}

// fileVisible decides whether GET /api/v1/files/{hash} may deliver a file:
// settings assets and profile photos are public; otherwise the caller must
// be allowed to see a medium with this file or an element that embeds it.
// The level is the lowest classification of the places the caller sees the
// file from (confidential pictures are delivered with a watermark).
func (s *Server) fileVisible(r *http.Request, hash string) (bool, int) {
	ctx := r.Context()
	if s.app.Files.IsConfigAsset(hash) || s.profilePhoto(r, hash) {
		return true, 0
	}
	if s.app.Media.HashVisible(ctx, hash) {
		return true, 0
	}
	// settings of a medium itself (own permissions, view only, watermark)
	// come first: such files are only delivered through the media library,
	// never because a page embeds them or a category allows more
	if s.app.Media.HashRestricted(ctx, hash) {
		return false, 0
	}
	p := identity.From(ctx)
	if p != nil && p.UserID > 0 { // own uploads (e.g. the editor preview before saving)
		var mine bool
		if s.app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM files WHERE hash=$1 AND created_by=$2)`, hash, p.UserID).Scan(&mine) == nil && mine {
			return true, 0
		}
	}
	// pairs of element and category
	refs, ok := s.legacy.get(s.app.Site + "|" + hash)
	if !ok {
		// acl: checked per element below (CanElement)
		rows, err := s.app.DB.Query(ctx, `SELECT DISTINCT e.id, e.category_id FROM element_revisions v JOIN elements e ON e.id = v.element_id
			WHERE e.deleted_at IS NULL AND strpos(v.body::text, $1) > 0 LIMIT 100`, hash)
		if err != nil {
			return false, 0
		}
		for rows.Next() {
			var el, c int64
			if rows.Scan(&el, &c) == nil {
				refs = append(refs, el, c)
			}
		}
		rows.Close()
		s.legacy.put(s.app.Site+"|"+hash, refs)
	}
	// watermarked only when every place the caller sees it from asks for it
	visible, marked := false, true
	for i := 0; i+1 < len(refs); i += 2 {
		el, c := refs[i], refs[i+1]
		if s.app.ACL.CanElement(ctx, p, el, c, acl.View) {
			visible = true
			if wm, _ := s.app.ACL.ElementFileProtection(ctx, el, c); !wm {
				marked = false
			}
		}
	}
	if !visible {
		return false, 0
	}
	if marked {
		return true, acl.Confidential
	}
	return true, 0
}

// profilePhoto reports whether hash is a profile photo the caller may see
// (visibility chosen by the person within the administrators' rule).
func (s *Server) profilePhoto(r *http.Request, hash string) bool {
	if s.app.Auth.PhotoHashVisible(r.Context(), s.app.DB, hash) {
		return true
	}
	return !s.app.SeparateAccounts && s.app.Instance != nil && storage.ValidHash(hash) &&
		s.app.Auth.PhotoHashVisible(r.Context(), s.app.Instance.Shared, hash)
}
