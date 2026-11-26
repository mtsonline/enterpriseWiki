package server

import (
	"cmp"
	"net/http"
	"slices"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"github.com/mtsonline/enterprisewiki/internal/readability"
	"github.com/mtsonline/enterprisewiki/internal/version"
)

// routesSystem registers the routes of health, readiness, TLS check of the proxy and the site information.
func (s *Server) routesSystem() {
	// action: core.health
	h.RecordRoute("GET /healthz")
	s.app.Mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { h.Write(w, 200, map[string]string{"status": "ok"}) })
	// on-demand TLS of the bundled reverse proxy (Caddy "ask"): 200 only for
	// the wiki's own host names, so no certificates are requested for others
	// action: core.tls_allowed
	h.RecordRoute("GET /tls-allowed")
	s.app.Mux.HandleFunc("GET /tls-allowed", func(w http.ResponseWriter, r *http.Request) {
		ok, err := s.app.Instance.AllowedHost(r.Context(), r.URL.Query().Get("domain"))
		switch {
		case err != nil:
			http.Error(w, "error", http.StatusInternalServerError)
		case ok:
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unknown host", http.StatusNotFound)
		}
	})
	// action: core.ready
	s.handle("GET /readyz", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := s.app.DB.Ping(r.Context()); err != nil {
			return nil, &apperr.Error{Status: 503, Code: "db_unavailable", Msg: "database unavailable"}
		}
		return map[string]string{"status": "ready"}, nil
	})
	// action: core.site
	s.handle("GET /api/v1/site", s.site)
}

// site returns public information needed by the frontend before login.
func (s *Server) site(w http.ResponseWriter, r *http.Request) (any, error) {
	c := s.app.Config.Get()
	var frontend []string
	templates := []map[string]any{}
	for _, p := range s.app.Kernel.List() {
		if p.Active && p.Frontend {
			frontend = append(frontend, p.ID)
		}
		if p.Active && slices.Contains(p.Kinds, coreplugin.KindTemplate) {
			templates = append(templates, map[string]any{"id": p.ID, "name": p.Name, "color_scheme": p.ColorScheme})
		}
	}
	// templates offered by plugins (templates of data, core.template/12)
	for _, t := range s.app.Content.ExtraTemplates(r.Context()) {
		templates = append(templates, map[string]any{"id": t.ID, "name": t.Name, "names": t.Names, "color_scheme": t.ColorScheme})
	}
	slices.SortFunc(templates, func(a, b map[string]any) int { return strings.Compare(a["name"].(string), b["name"].(string)) })
	home := c.UI.HomeCategory
	return map[string]any{
		"name": c.UI.SiteName, "template": c.UI.Template, "templates": templates, "home_category": home, "home_element": c.UI.HomeElement, "editor": cmp.Or(c.UI.Editor, "visual"), "collaboration": c.UI.Collaboration, "personal_placement": c.UI.PersonalPlacement, "custom_css": c.UI.CustomCSS, "editor_tools": siteEditorTools(c.UI.EditorTools), "dashboard_tiles": nonNil(c.UI.DashboardTiles), "dashboard_home": c.UI.DashboardHome, "maintenance": siteMaintenance(c.Maintenance), "consent": siteConsent(c.Consent), "not_found_element": c.UI.NotFoundElement, "forbidden_element": c.UI.ForbiddenElement,
		"wait_notice_s": limits.Value(c, limits.WaitNoticeSeconds),
		"readability":   siteReadability(c),
		"languages":     c.I18n.Languages, "default_language": c.I18n.DefaultLanguage,
		"plugins": frontend, "element_types": s.app.Content.Types(), "auth_providers": s.app.Auth.ProviderInfos(),
		"version": version.Version, "licensed": s.licensed(), "permissions": acl.Names,
		// site: the site of this host; instance_only: settings only
		// instance administrators change (in config.yaml of the instance)
		"site": s.app.Site, "instance_only": config.InstanceOnly, "sites_accounts": s.app.Config.Get().Sites.Accounts,
		"separate_accounts": s.app.SeparateAccounts,
		"login":             s.loginPage(r.Context()),
	}, nil
}

// siteReadability is the hint on long sentences and words for the editors:
// the mode of the site, the limits (defaults filled in) and how much of a
// text the browser checks.
func siteReadability(c *config.Config) map[string]any {
	r := c.UI.Readability
	return map[string]any{
		"mode":           cmp.Or(r.Mode, readability.ModeOff),
		"sentence_words": cmp.Or(r.SentenceWords, readability.DefaultSentenceWords),
		"word_letters":   cmp.Or(r.WordLetters, readability.DefaultWordLetters),
		"max_kb":         limits.Value(c, limits.ReadabilityKB),
	}
}

// siteEditorTools is the site's choice of toolbar buttons (["*"] = all).
func siteEditorTools(list []string) []string {
	if len(list) == 0 {
		return []string{content.AllEditorTools}
	}
	return list
}

// nonNil returns an empty list for nil (JSON [] instead of null).
func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
