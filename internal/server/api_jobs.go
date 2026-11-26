package server

import (
	"net/http"

	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// routesJobs registers the list of background jobs and starting one now.
func (s *Server) routesJobs() {
	// POST /api/v1/admin/jobs/{name}/run runs a background job now (e.g. "mail").
	// action: core.jobs.list
	s.handle("GET /api/v1/admin/jobs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.app.Workers.Names(), nil
	})
	// action: core.jobs.run
	s.handle("POST /api/v1/admin/jobs/{name}/run", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		if err := s.app.Workers.RunOnce(r.Context(), r.PathValue("name")); err != nil {
			return nil, err
		}
		return h.NoContent{}, nil
	})
}
