package server

import (
	"context"
	"net/http"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/license"
	"github.com/mtsonline/enterprisewiki/internal/pgversion"
)

// LicenseStatus evaluates the configured license key.
func (s *Server) LicenseStatus(ctx context.Context) license.Status {
	var users int
	_ = s.app.DB.QueryRow(ctx, `SELECT count(*) FROM users WHERE NOT disabled`).Scan(&users)
	return license.Evaluate(s.app.Config.Get().License.Key, users, time.Now())
}

func (s *Server) routesM9() {
	// GET /api/v1/admin/database – PostgreSQL version, recommended version, end of support
	s.handle("GET /api/v1/admin/database", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return pgversion.Check(r.Context(), s.app.DB, time.Now())
	})
	// GET /api/v1/admin/license – license state (offline check of the key)
	s.handle("GET /api/v1/admin/license", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.LicenseStatus(r.Context()), nil
	})
	// PUT /api/v1/admin/license {"key": "EWL1…"} installs a key (checked first)
	s.handle("PUT /api/v1/admin/license", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		var in struct {
			Key string `json:"key"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		l, err := license.Parse(in.Key)
		if err != nil {
			return nil, apperr.BadRequest("%s", err.Error())
		}
		if err := s.app.Config.Base().Update(func(c *config.Config) error { c.License.Key = in.Key; return nil }); err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), s.app.DB, "license.install", "license", 0, nil, l)
		return s.LicenseStatus(r.Context()), nil
	})
	s.handle("DELETE /api/v1/admin/license", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		if err := s.app.Config.Base().Update(func(c *config.Config) error { c.License.Key = ""; return nil }); err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), s.app.DB, "license.remove", "license", 0, nil, nil)
		return h.NoContent{}, nil
	})
}
