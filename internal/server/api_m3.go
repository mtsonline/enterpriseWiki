package server

import (
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/version"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

func (s *Server) routesM3() {
	// ---------------------------------------------------------------- macros
	// Lists the macros of active plugins (used by the editor's macro dialog).
	s.handle("GET /api/v1/macros", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Macros(), nil
	})

	// ---------------------------------------------------------------- updates
	manifests := func() []sdk.Manifest {
		ms := []sdk.Manifest{}
		for _, p := range s.app.Kernel.List() {
			ms = append(ms, p.Manifest)
		}
		return ms
	}
	// GET /api/v1/admin/updates?force=1 checks GitHub releases of the core
	// and all plugins with a repository (cached for an hour).
	s.handle("GET /api/v1/admin/updates", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		force := r.URL.Query().Get("force") == "1"
		if !force {
			if rep := s.plugins.LastUpdateReport(); rep != nil {
				return rep, nil
			}
			if c := s.app.Config.Get().Plugins.Updates.Check; c != nil && !*c {
				return map[string]any{"checked_at": nil, "items": []any{}}, nil
			}
		}
		return s.plugins.CheckUpdates(r.Context(), version.Version, manifests(), force)
	})
	// POST /api/v1/admin/updates/{id} installs the latest release of a
	// plugin and starts the rebuild (1-click update).
	s.handle("POST /api/v1/admin/updates/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id := r.PathValue("id")
		if id == "core" {
			return nil, apperr.BadRequest("the core is updated with scripts/update.sh (or the update timer) on the server")
		}
		res, err := s.plugins.UpdatePlugin(r.Context(), id)
		if err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.update", id)
		return res, nil
	})
}
