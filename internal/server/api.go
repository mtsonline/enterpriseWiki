package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

func (s *Server) handle(pattern string, fn h.Handler) {
	h.RecordRoute(pattern)
	s.app.Mux.HandleFunc(pattern, h.JSON(fn))
}

// adminPerms maps administrative routes to the global permission they need
// (longest prefix wins). Routes without an entry are for administrators only.
var adminPerms = []struct{ prefix, perm string }{
	{"/api/v1/admin/users", "core.users.manage"},
	{"/api/v1/admin/groups", "core.groups.manage"},
	{"/api/v1/admin/roles", "core.roles.manage"},
	{"/api/v1/admin/permissions", "core.roles.manage"},
	{"/api/v1/admin/settings", "core.settings.manage"},
	{"/api/v1/admin/auth/login", "core.settings.manage"},
	{"/api/v1/admin/limits", "core.settings.manage"},
	{"/api/v1/admin/data", "core.settings.manage"},
	{"/api/v1/admin/consent", "core.settings.manage"},
	{"/api/v1/admin/consents", "core.audit.view"},
	{"/api/v1/admin/languages", "core.i18n.manage"},
	{"/api/v1/admin/retention", "core.settings.manage"},
	{"/api/v1/admin/audit/settings", "core.audit.manage"},
	{"/api/v1/admin/audit/forward", "core.audit.manage"},
	{"/api/v1/admin/audit", "core.audit.view"},
	{"/api/v1/admin/plugins", "core.plugins.manage"},
	{"/api/v1/admin/restart", "core.plugins.manage"},
	{"/api/v1/admin/updates", "core.plugins.manage"},
	{"/api/v1/admin/jobs", "core.plugins.manage"},
	{"/api/v1/admin/mail", "core.mail.manage"},
	{"/api/v1/admin/webhooks", "core.webhooks.manage"},
	{"/api/v1/admin/watch-assignments", "core.watches.assign"},
	{"/api/v1/admin/license", "core.license.manage"},
	{"/api/v1/admin/cache", "core.system.manage"},
	{"/api/v1/admin/database", "core.system.manage"},
	// /api/v1/admin/i18n: checked per language by the i18n service (core.i18n.manage or core.i18n.translate.<lang>)
	{"/api/v1/prerender", "core.settings.manage"},
}

// routePerm returns the permission of an administrative route pattern.
func routePerm(pattern string) string {
	_, path, _ := strings.Cut(pattern, " ")
	best := ""
	key := ""
	for _, e := range adminPerms {
		if strings.HasPrefix(path, e.prefix) && len(e.prefix) > len(best) {
			best, key = e.prefix, e.perm
		}
	}
	return key
}

// requireAdmin checks the permission of the requested administrative route
// (see adminPerms); administrators may use all of them.
func requireAdmin(r *http.Request) error {
	p := identity.From(r.Context())
	if p == nil {
		return apperr.ErrUnauthorized
	}
	if key := routePerm(r.Pattern); key != "" {
		return perm.Require(r.Context(), key)
	}
	if !p.Admin() {
		return apperr.ErrForbidden
	}
	return nil
}

func requireUser(r *http.Request) (*identity.Principal, error) {
	p := identity.From(r.Context())
	if p == nil {
		return nil, apperr.ErrUnauthorized
	}
	return p, nil
}

func (s *Server) routes() {
	s.routesSSO()
	s.routesFiles()
	s.routesLogin()
	s.routesMedia()
	s.routesOffice()
	s.routesDemo()
	s.routesCollab()
	s.routesIdentity()
	s.routesAlias()
	s.routesSignatures()
	s.routesSessions()
	s.routesProviderAdmin()
	s.routesSCIM()
	s.routesMFA()
	s.routesPasswordless()
	s.routesScimAdmin()
	s.routesMacroList()
	s.routesOverview()
	s.routesMail()
	s.routesJobs()
	s.routesNotificationSettings()
	s.routesWatches()
	s.routesFeeds()
	s.routesProfile()
	s.routesCache()
	s.routesLookup()
	s.routesPrerender()
	s.routesWebhooks()
	s.routesSites()
	s.routesDatabaseState()
	s.routesLicense()
	s.routesPermissions()
	s.routesAccess()
	s.routesAudit()
	s.routesLimits()
	s.routesKeyLimits()
	s.routesSecrets()
	s.routesDrafts()
	s.routesInstances()
	s.routesElementRights()
	s.routesMaintenance()
	s.routesConsent()
	s.routesScheme()
	s.routesData()
	s.routesPlacements()
	s.routesNotifications()
	s.routesWatchAssignments()
	s.routesSystem()
	s.routesAuth()
	s.routesMe()
	s.routesUsers()
	s.routesCategories()
	s.routesElements()
	s.routesTrashSearch()
	s.routesI18n()
	s.routesSettings()
	s.routesPlugins()
}

func asAppErr(err error, target **apperr.Error) bool { return errors.As(err, target) }

// SettingsUpdate is the subset of the configuration editable in the UI.
// Server, database and mode settings require a restart and are changed in
// config.yaml or with "ewiki config set".
type SettingsUpdate struct {
	UI             *config.UI                `json:"ui"`
	I18n           *config.I18n              `json:"i18n"`
	Auth           *config.Auth              `json:"auth"`
	Retention      *config.Retention         `json:"retention"`
	Media          *config.Media             `json:"media"`
	Lists          *config.Lists             `json:"lists"`
	People         *config.People            `json:"people"`
	Macros         *config.MacroSecurity     `json:"macros"`
	Mail           *config.Mail              `json:"mail"`
	Catalog        []config.CatalogEntry     `json:"catalog"`
	PluginSettings map[string]map[string]any `json:"plugin_settings"`
}

// clip shortens a text to at most n bytes (whole runes).
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s[:n])
	return string(r[:len(r)-1]) + "…"
}
