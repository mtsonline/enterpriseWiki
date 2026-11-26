package server

import (
	"cmp"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/media"
	"github.com/mtsonline/enterprisewiki/internal/storage"
)

// routesFiles registers uploading files and delivering them (profile
// photos, pictures of pages; with a watermark where the settings ask for it).
func (s *Server) routesFiles() {
	// action: core.files.upload
	s.handle("POST /api/v1/files", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p := identity.From(r.Context())
		if p == nil {
			return nil, apperr.ErrUnauthorized
		}
		if p.Via == "apikey" && p.KeyPerms&(2|4) == 0 {
			return nil, apperr.ErrForbidden
		}
		r.Body = http.MaxBytesReader(w, r.Body, s.app.Files.MaxBytes()+1<<20)
		mr, err := r.MultipartReader()
		if err != nil {
			return nil, apperr.BadRequest("expected multipart/form-data with a field \"file\"")
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				return nil, apperr.BadRequest("no field \"file\" in the upload")
			}
			if part.FormName() != "file" {
				_ = part.Close()
				continue
			}
			f, err := s.app.Files.Put(r.Context(), part, part.FileName())
			_ = part.Close()
			if err != nil {
				return nil, err
			}
			return h.Created{V: f}, nil
		}
	})
	// Images by hash: only settings assets, profile photos, files of media
	// the caller may see and images embedded in elements the caller may see
	// (content from before the media library). Other file types are only
	// delivered through their medium or element (permission checked).
	// action: core.files.get
	h.RecordRoute("GET /api/v1/files/{hash}")
	s.app.Mux.HandleFunc("GET /api/v1/files/{hash}", func(w http.ResponseWriter, r *http.Request) {
		store := s.app.Files
		f, err := store.Get(r.Context(), r.PathValue("hash"))
		if err != nil && s.app.Instance != nil && !s.app.SeparateAccounts && s.sharedProfilePhoto(r.Context(), r.PathValue("hash")) {
			// profile photos belong to the shared user account: they may
			// have been uploaded on another site with shared accounts. Only
			// these images are looked up elsewhere - never other files of
			// another site.
			for _, o := range s.app.Instance.Sites() {
				if o == s.app || o.SeparateAccounts {
					continue
				}
				if of, oerr := o.Files.Get(r.Context(), r.PathValue("hash")); oerr == nil {
					store, f, err = o.Files, of, nil
					break
				}
			}
		}
		if err != nil || !strings.HasPrefix(f.Mime, "image/") || strings.Contains(f.Mime, "svg") {
			h.WriteError(w, r, apperr.NotFound("image"))
			return
		}
		ok, level := s.fileVisible(r, f.Hash)
		if !ok {
			h.WriteError(w, r, apperr.NotFound("image"))
			return
		}
		if level >= acl.Confidential {
			// only seen in confidential content: with the viewer's name and the time
			s.serveMarked(w, r, store, f)
			return
		}
		store.Serve(w, r, f, "", true)
	})
}

// sharedProfilePhoto reports whether hash is the profile photo of a shared
// (instance) account.
func (s *Server) sharedProfilePhoto(ctx context.Context, hash string) bool {
	if !storage.ValidHash(hash) {
		return false
	}
	var ok bool
	err := s.app.Instance.Shared.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE profile->>'photo' = $1)`, hash).Scan(&ok)
	return err == nil && ok
}

// serveMarked delivers a picture with the name of the viewer and the time.
func (s *Server) serveMarked(w http.ResponseWriter, r *http.Request, store *storage.Store, f *storage.File) {
	rd, err := store.Open(f.Hash)
	if err != nil {
		h.WriteError(w, r, apperr.NotFound("image"))
		return
	}
	data, err := io.ReadAll(io.LimitReader(rd, 64<<20))
	_ = rd.Close()
	if err != nil {
		h.WriteError(w, r, err)
		return
	}
	img, format, err := media.Decode(data)
	if err != nil {
		h.WriteError(w, r, apperr.NotFound("image"))
		return
	}
	name := "?"
	if p := identity.From(r.Context()); p != nil {
		name = cmp.Or(p.DisplayName, p.Username, name)
	}
	if img, err = media.Watermark(img, []string{name, time.Now().Format("2006-01-02 15:04")}); err != nil {
		h.WriteError(w, r, err)
		return
	}
	out, mime, err := media.Encode(img, format)
	if err != nil {
		h.WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(out)
}
