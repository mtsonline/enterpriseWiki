package server

import (
	"net/http"
)

// routesTemplates registers what the administration of the templates needs
// from the core beyond the templates themselves (Administration → Templates).
func (s *Server) routesTemplates() {
	// GET /admin/templates/usage → the template of the site and, per
	// template, how many categories choose it themselves. Counts only: no
	// names of categories, so nothing beyond the right of the page is shown.
	// action: core.templates.usage
	s.handle("GET /api/v1/admin/templates/usage", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		counts, err := s.app.Content.TemplateUsage(r.Context())
		if err != nil {
			return nil, err
		}
		return map[string]any{"site": s.app.Config.Get().UI.Template, "categories": counts}, nil
	})
}
