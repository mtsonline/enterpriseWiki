// Package sdk is the plugin API of Enterprise Wiki.
//
// A plugin is a Go package below plugins/ that registers itself in init():
//
//	func init() { sdk.Register(&Plugin{}) }
//
// and implements Plugin plus any of the optional interfaces (Setup,
// Migrations, Locales). Plugins are compiled into the binary (see
// docs/developer/plugins.md); enabling and disabling takes effect immediately
// without a restart. A plugin's frontend part lives in plugins/<dir>/web.
package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/i18n"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/mail"
	"github.com/mtsonline/enterprisewiki/internal/media"
	"github.com/mtsonline/enterprisewiki/internal/notify"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/internal/storage"
	"github.com/mtsonline/enterprisewiki/internal/webhook"
	"github.com/mtsonline/enterprisewiki/internal/worker"
)

// Plugin kinds (Manifest.Kinds, see PluginKinds).
const (
	KindElement      = "element"      // element types
	KindMacro        = "macro"        // macros
	KindAuth         = "auth"         // sign-in providers
	KindMFA          = "mfa"          // second factors
	KindStorage      = "storage"      // storage and cloud drives
	KindOffice       = "office"       // office programs
	KindSearch       = "search"       // search engines
	KindNotification = "notification" // notification channels
	KindWorkflow     = "workflow"     // workflows
	KindMenu         = "menu"         // menus
	KindTemplate     = "template"     // frontend templates (themes)
	KindIntegration  = "integration"  // external systems
	KindGeneral      = "general"      // anything else
)

// Manifest describes a plugin. It must match plugins/<dir>/plugin.json.
type Manifest struct {
	ID          string `json:"id"`      // unique, e.g. "core.html"
	Name        string `json:"name"`    // human readable
	Version     string `json:"version"` // semantic version
	Description string `json:"description"`
	Author      string `json:"author"`
	Homepage    string `json:"homepage"`
	Repo        string `json:"repo"` // e.g. github.com/owner/repo (for updates)
	// Kinds is the type of the plugin (required, one or more of
	// PluginKinds); the administration groups plugins by the first one.
	Kinds []string `json:"kinds"`
	// Extends lists plugins this plugin builds on. It is only active while
	// all of them are active.
	Extends []string `json:"extends"`
	// Required plugins cannot be disabled (e.g. local authentication).
	Required bool `json:"required"`
	// MinCore is the minimum core version.
	MinCore string `json:"min_core"`
	// Frontend is true if plugins/<dir>/web/index.ts exists.
	Frontend bool `json:"frontend"`
	// Permissions the plugin checks (see docs/developer/plugins.md#permissions).
	// Every permission a plugin uses must be declared here; keys start with
	// the plugin id. Labels are English (translations: UI key perm.<key>).
	Permissions []perm.Def `json:"permissions,omitempty"`
}

// Plugin is implemented by every plugin.
type Plugin interface {
	Manifest() Manifest
}

// SetupPlugin registers routes, element types, auth providers etc.
// Setup is called once at startup for every compiled plugin, enabled or not;
// everything registered through the Host is automatically inactive while
// the plugin is disabled.
type SetupPlugin interface {
	Setup(h *Host) error
}

// MigrationsPlugin provides SQL migrations (NNNN_name.sql at the FS root).
type MigrationsPlugin interface {
	Migrations() fs.FS
}

// LocalesPlugin provides UI strings (<lang>.json at the FS root).
type LocalesPlugin interface {
	Locales() fs.FS
}

var (
	regMu    sync.Mutex
	registry = map[string]Plugin{}
)

// Register adds a plugin to the registry. Call it from init().
func Register(p Plugin) {
	regMu.Lock()
	defer regMu.Unlock()
	m := p.Manifest()
	if err := m.Validate(); err != nil {
		panic("sdk.Register: " + err.Error())
	}
	id := m.ID
	if _, dup := registry[id]; dup {
		panic("sdk.Register: duplicate plugin id " + id)
	}
	registry[id] = p
}

// Registered returns all compiled plugins sorted by id.
func Registered() []Plugin {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]Plugin, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest().ID < out[j].Manifest().ID })
	return out
}

// Host is the plugin's handle to the system.
type Host struct {
	Manifest Manifest
	Config   *config.Store
	DB       *pgxpool.Pool
	ACL      *acl.Service
	Content  *content.Service
	Auth     *auth.Service
	I18n     *i18n.Service
	Files    *storage.Store
	Media    *media.Service
	Mail     *mail.Service
	Workers  *worker.Registry
	// Webhooks queues HTTP notifications (Emit) for events of the plugin.
	Webhooks *webhook.Service
	// Notify sends notifications in the wiki (bell) and by e-mail; declare
	// the kinds with notify.RegisterKind so users can configure the e-mails.
	Notify *notify.Service
	Log    *slog.Logger

	Mux       *http.ServeMux
	IsEnabled func(pluginID string) bool
	// Site is the name of the site this host belongs to ("main" for the
	// first site). Each site gets its own plugin instance and host.
	Site string
}

// Active reports whether this plugin is currently enabled.
func (h *Host) Active() bool { return h.IsEnabled(h.Manifest.ID) }

// Settings returns the plugin's section of plugin_settings in config.yaml.
func (h *Host) Settings() map[string]any {
	s := h.Config.Get().Settings[h.Manifest.ID]
	if s == nil {
		return map[string]any{}
	}
	return s
}

// Handle registers an API route below /api/v1/x/<plugin-id>. The route
// responds 404 while the plugin is disabled. Use identity.From(r.Context())
// for the caller and the services for permission checks.
func (h *Host) Handle(method, path string, fn httpx.Handler) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	id := h.Manifest.ID
	pattern := method + " /api/v1/x/" + id + path
	httpx.RecordRoute(pattern)
	h.Mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if !h.IsEnabled(id) {
			httpx.WriteError(w, r, apperr.NotFound("plugin "+id))
			return
		}
		httpx.JSON(fn)(w, r)
	})
}

// RegisterElementType adds an element type owned by this plugin.
func (h *Host) RegisterElementType(t content.ElementType) {
	h.Content.RegisterType(h.Manifest.ID, t)
}

// RegisterMacro adds a macro for rich text (see content.Macro).
func (h *Host) RegisterMacro(m content.Macro) {
	h.Content.RegisterMacro(h.Manifest.ID, m)
}

// RegisterWorker adds a periodic background job named "<plugin-id>.<name>".
// It only runs while the plugin is enabled. In cluster mode one node runs it
// at a time; jobs must still tolerate concurrent runs (FOR UPDATE SKIP LOCKED).
func (h *Host) RegisterWorker(name string, interval time.Duration, run func(ctx context.Context) error) {
	id := h.Manifest.ID
	h.Workers.Register(worker.Job{Name: id + "." + name, Interval: interval, Active: func() bool { return h.IsEnabled(id) }, Run: run})
}

// T translates a UI string (backend messages such as e-mails) into lang with
// {name} placeholders. Missing keys return the key.
func (h *Host) T(ctx context.Context, lang, key string, params map[string]string) string {
	return h.I18n.T(ctx, lang, key, params)
}

// RegisterSearchBackend sets an external full text engine (see content.SearchBackend).
func (h *Host) RegisterSearchBackend(b content.SearchBackend) {
	h.Content.RegisterSearchBackend(h.Manifest.ID, b)
}

// RegisterAuthProvider adds a password authentication provider.
func (h *Host) RegisterAuthProvider(p auth.PasswordProvider) {
	h.Auth.RegisterProvider(h.Manifest.ID, p)
}

// RegisterRedirectProvider adds a browser based sign-in method (OIDC, SAML, ...).
// Its endpoints are /api/v1/auth/sso/<id>/start and /api/v1/auth/sso/<id>/callback.
func (h *Host) RegisterRedirectProvider(p auth.RedirectProvider) {
	h.Auth.RegisterRedirectProvider(h.Manifest.ID, p)
}

// RegisterAuthInstances adds one password provider per configured instance
// of the plugin (see auth.InstanceSet): the top-level settings are the
// provider base, each entry of settings "instances" a provider base-<id>.
func RegisterAuthInstances[T auth.PasswordProvider](h *Host, set *auth.InstanceSet[T]) {
	h.Auth.RegisterProviderSource(h.Manifest.ID, func() []auth.PasswordProvider {
		all := set.All()
		out := make([]auth.PasswordProvider, len(all))
		for i, p := range all {
			out[i] = p
		}
		return out
	})
}

// RegisterRedirectInstances adds one browser based provider per configured
// instance of the plugin (see RegisterAuthInstances); the endpoints of an
// instance are /api/v1/auth/sso/<base>-<id>/...
func RegisterRedirectInstances[T auth.RedirectProvider](h *Host, set *auth.InstanceSet[T]) {
	h.Auth.RegisterRedirectSource(h.Manifest.ID, func() []auth.RedirectProvider {
		all := set.All()
		out := make([]auth.RedirectProvider, len(all))
		for i, p := range all {
			out[i] = p
		}
		return out
	})
}

// RequirePerm returns 401/403 unless the caller of r has the global
// permission key (declared in plugin.json; administrators have all).
func (h *Host) RequirePerm(r *http.Request, key string) error {
	return perm.Require(r.Context(), key)
}

// HasPerm reports whether the caller has the global permission key.
func (h *Host) HasPerm(ctx context.Context, key string) bool { return perm.Has(ctx, key) }

// Can reports whether the caller has the category permission action (a key
// declared in plugin.json with scope "category") on a category.
func (h *Host) Can(ctx context.Context, categoryID int64, action string) bool {
	return h.ACL.CanAction(ctx, identity.From(ctx), categoryID, action)
}

// Members returns the (active) users among users and in groups and roles.
func (h *Host) Members(ctx context.Context, users, groups, roles []int64) ([]int64, error) {
	rows, err := h.DB.Query(ctx, `SELECT DISTINCT u.id FROM users u WHERE NOT u.disabled AND (u.id = ANY($1)
		OR u.id IN (SELECT user_id FROM user_groups WHERE group_id = ANY($2))
		OR u.id IN (SELECT user_id FROM user_roles WHERE role_id = ANY($3))
		OR u.id IN (SELECT ug.user_id FROM group_roles gr JOIN user_groups ug ON ug.group_id = gr.group_id WHERE gr.role_id = ANY($3)))`, users, groups, roles)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// UserCan reports whether user uid has want on a category (e.g. to notify
// only people who may see a page).
func (h *Host) UserCan(ctx context.Context, uid, categoryID int64, want acl.Perm) bool {
	p, err := h.Auth.LoadPrincipal(ctx, uid)
	if err != nil {
		return false
	}
	return h.ACL.Can(ctx, p, categoryID, want)
}

// RequireAdmin returns an error unless the caller of r is an administrator.
// Prefer RequirePerm with a declared permission.
func RequireAdmin(r *http.Request) error {
	p := identity.From(r.Context())
	if p == nil {
		return apperr.ErrUnauthorized
	}
	if !p.Admin() {
		return apperr.ErrForbidden
	}
	return nil
}

// PluginKinds are the types of plugins, in the order of the administration.
var PluginKinds = []string{KindElement, KindMacro, KindAuth, KindMFA, KindStorage, KindOffice, KindSearch, KindNotification,
	KindWorkflow, KindMenu, KindTemplate, KindIntegration, KindGeneral}

var pluginID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(\.[a-z0-9][a-z0-9_-]*)+$`)

// Validate checks the required fields of a manifest.
func (m Manifest) Validate() error {
	if len(m.ID) > 64 || !pluginID.MatchString(m.ID) {
		return fmt.Errorf("plugin id %q: must look like vendor.name (lower case)", m.ID)
	}
	if m.Name == "" || m.Version == "" {
		return fmt.Errorf("plugin %s: name and version are required", m.ID)
	}
	if len(m.Kinds) == 0 {
		return fmt.Errorf("plugin %s: kinds is required (one or more of %s)", m.ID, strings.Join(PluginKinds, ", "))
	}
	for _, k := range m.Kinds {
		if !slices.Contains(PluginKinds, k) {
			return fmt.Errorf("plugin %s: unknown kind %q (allowed: %s)", m.ID, k, strings.Join(PluginKinds, ", "))
		}
	}
	return nil
}

// ParseManifest parses an embedded plugin.json and panics on error (use it
// in Manifest() with //go:embed plugin.json).
func ParseManifest(b []byte) Manifest {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		panic("invalid plugin.json: " + err.Error())
	}
	return m
}

// Factory is implemented by plugins whose zero value is not ready to use:
// NewInstance returns a fresh instance for a further site. Other plugins
// get a new zero value of their type.
type Factory interface {
	NewInstance() Plugin
}

// NewInstance returns a fresh instance of p for a further site.
func NewInstance(p Plugin) Plugin {
	if f, ok := p.(Factory); ok {
		return f.NewInstance()
	}
	t := reflect.TypeOf(p)
	if t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct {
		return reflect.New(t.Elem()).Interface().(Plugin)
	}
	return p
}

// SiteOf returns the site of a request or job context ("main" if unset).
func SiteOf(ctx context.Context) string { return identity.Site(ctx) }

// PerSite keeps one value per site, for package-level state that code
// without access to the plugin instance needs (renderers, hooks).
type PerSite[T any] struct {
	m sync.Map
}

// Set stores the value of a site.
func (p *PerSite[T]) Set(site string, v T) { p.m.Store(site, v) }

// Get returns the value of the site of ctx (zero value if none).
func (p *PerSite[T]) Get(ctx context.Context) T {
	if v, ok := p.m.Load(SiteOf(ctx)); ok {
		return v.(T)
	}
	var zero T
	return zero
}
