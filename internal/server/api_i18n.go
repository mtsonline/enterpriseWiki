package server

import (
	"io"
	"maps"
	"mime"
	"net/http"
	"slices"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/i18n"
)

// routesI18n registers the routes of texts of the interface and their translations.
func (s *Server) routesI18n() {
	// action: core.i18n.status
	s.handle("GET /api/v1/i18n/status", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.app.I18n.Status(r.Context()) })
	// action: core.i18n.texts
	s.handle("GET /api/v1/i18n/{lang}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		lang := r.PathValue("lang")
		if !config.ValidLanguage(lang) {
			return nil, apperr.BadRequest("invalid language")
		}
		return s.app.I18n.Messages(r.Context(), lang)
	})
	// action: core.i18n.strings
	s.handle("GET /api/v1/admin/i18n/{lang}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.I18n.Strings(r.Context(), r.PathValue("lang"))
	})
	// PUT {"key": "text", "plural.key_few": "…"}: translations; "" removes a custom text
	// action: core.i18n.set
	s.handle("PUT /api/v1/admin/i18n/{lang}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in map[string]string
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		lang := r.PathValue("lang")
		res, err := s.app.I18n.SetStrings(r.Context(), lang, in)
		if err == nil && res.Written+res.Removed > 0 {
			_ = audit.Log(r.Context(), s.app.DB, "i18n.translate", "language", 0, nil, map[string]any{"lang": lang, "keys": slices.Sorted(maps.Keys(in)), "written": res.Written, "removed": res.Removed})
		}
		return res, err
	})
	// GET …/export?format=json|xliff|po&source=core&filter=all|missing|custom
	// action: core.i18n.export
	h.RecordRoute("GET /api/v1/admin/i18n/{lang}/export")
	s.app.Mux.HandleFunc("GET /api/v1/admin/i18n/{lang}/export", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		data, ctype, name, err := s.app.I18n.Export(r.Context(), r.PathValue("lang"), i18n.ExportOptions{Format: q.Get("format"), Source: q.Get("source"), Filter: q.Get("filter")})
		if err != nil {
			h.WriteError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": safeFileName(name)}))
		_, _ = w.Write(data) // #nosec G705 -- download (attachment, nosniff), not rendered
	})
	// POST …/import?format=json|xliff|po (body: the file; format detected when omitted)
	// action: core.i18n.import
	s.handle("POST /api/v1/admin/i18n/{lang}/import", func(w http.ResponseWriter, r *http.Request) (any, error) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 20<<20))
		if err != nil {
			return nil, apperr.BadRequest("file too large (max. 20 MB)")
		}
		lang := r.PathValue("lang")
		res, err := s.app.I18n.Import(r.Context(), lang, r.URL.Query().Get("format"), data)
		if err == nil {
			_ = audit.Log(r.Context(), s.app.DB, "i18n.import", "language", 0, nil, map[string]any{"lang": lang, "written": res.Written, "unchanged": res.Unchanged, "unknown": len(res.Unknown)})
		}
		return res, err
	})
	// action: core.languages.add
	s.handle("POST /api/v1/admin/languages", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		var in struct {
			Code string `json:"code"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if !config.ValidLanguage(in.Code) {
			return nil, apperr.BadRequest("invalid language code (use e.g. de, fr, pt-BR)")
		}
		err := s.app.Config.Update(func(c *config.Config) error {
			if slices.Contains(c.I18n.Languages, in.Code) {
				return apperr.Conflict("language already exists")
			}
			c.I18n.Languages = append(c.I18n.Languages, in.Code)
			return nil
		})
		s.auditSettings(r, "settings.language_add", in.Code)
		return h.NoContent{}, err
	})
	// action: core.languages.remove
	s.handle("DELETE /api/v1/admin/languages/{code}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		code := r.PathValue("code")
		err := s.app.Config.Update(func(c *config.Config) error {
			if code == c.I18n.DefaultLanguage {
				return apperr.BadRequest("the default language cannot be removed")
			}
			if !slices.Contains(c.I18n.Languages, code) {
				return apperr.NotFound("language")
			}
			c.I18n.Languages = slices.DeleteFunc(c.I18n.Languages, func(x string) bool { return x == code })
			return nil
		})
		s.auditSettings(r, "settings.language_remove", code)
		return h.NoContent{}, err
	})
}

// safeFileName keeps letters, digits, dot, dash and underscore of a
// download name (no quotes, separators or control characters).
func safeFileName(name string) string {
	b := []rune{}
	for _, r := range name {
		if r < 128 && (r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			b = append(b, r)
		}
	}
	if len(b) == 0 || len(b) > 120 {
		return "download"
	}
	return string(b)
}
