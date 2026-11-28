// Package coreplugin is the interface of core plugins: plugins that are
// compiled into the binary and run in the wiki process (plugins/<dir>).
//
// It hands out internal services (database pool, content, auth …) and makes
// no stability promise: it changes together with the core. The stable
// interface for plugins that run as their own process is the separate SDK
// module (sdk/, Apache-2.0); see docs/en/developer/plugins.md.
//
// A core plugin registers itself in init():
//
//	func init() { coreplugin.Register(&Plugin{}) }
//
// and implements Plugin plus any of the optional interfaces (Setup,
// Migrations, Locales). Enabling and disabling takes effect immediately
// without a restart. A plugin's frontend part lives in plugins/<dir>/web.
package coreplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mtsonline/enterprisewiki/internal/tplsettings"
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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/feedfetch"
	"github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/i18n"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"github.com/mtsonline/enterprisewiki/internal/mail"
	"github.com/mtsonline/enterprisewiki/internal/media"
	"github.com/mtsonline/enterprisewiki/internal/notify"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
	"github.com/mtsonline/enterprisewiki/internal/signature"
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
	KindSignature    = "signature"    // ways to sign (passkey, S/MIME, qualified …)
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
	// DefaultOff plugins are shipped but inactive until an administrator
	// switches them on (they are then listed in plugins.enabled of
	// config.yaml), e.g. the web application firewall.
	DefaultOff bool `json:"default_off,omitempty"`
	// MinCore is the minimum core version.
	MinCore string `json:"min_core"`
	// Frontend is true if plugins/<dir>/web/index.ts exists.
	Frontend bool `json:"frontend"`
	// Permissions the plugin checks (see docs/en/developer/plugins.md#permissions-required).
	// Every permission a plugin uses must be declared here; keys start with
	// the plugin id. Labels are English (translations: UI key perm.<key>).
	Permissions []perm.Def `json:"permissions,omitempty"`
	// Limits are the technical limits of the plugin (sizes, counts, waiting
	// times). Keys start with the plugin id. Administrators see and change
	// them under Administration → Expert settings; read them with
	// Host.Limit (see docs/en/developer/plugins.md#limits).
	Limits []limits.Def `json:"limits,omitempty"`
	// InstanceLabels are UI texts of the plugin (keys of its en.json) that
	// each instance may change, per language: buttons and headings of a
	// blog, a FAQ … (see docs/en/developer/plugins.md#instance-labels). The
	// texts are stored as UI translations "<key>@<plugin>/<instance>".
	InstanceLabels []string `json:"instance_labels,omitempty"`
	// ColorScheme is for templates whose colours come from the colour
	// variables of the core (light and dark): the scheme shown when the
	// person has not chosen one: "system" (as the device is set), "light"
	// or "dark". Empty: the template has light colours only, and the choice
	// of a scheme does not apply to it.
	ColorScheme string `json:"color_scheme,omitempty"`
	// TemplateSettings are the settings the template of this plugin offers
	// (colours, fonts, logos …; Administration → Templates → Settings). The
	// server checks every value against them before it is stored; the
	// template gets the effective values (see docs/en/developer/plugins.md#template-settings).
	TemplateSettings []tplsettings.Setting `json:"template_settings,omitempty"`
	// TemplateLookRoot is the element whose CSS variables the settings with
	// css_vars change: "" for <body> (the variables of the core), or one
	// class of the element where the template defines its own variables
	// (".ewcms-app").
	TemplateLookRoot string `json:"template_look_root,omitempty"`
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
		panic("coreplugin.Register: " + err.Error())
	}
	id := m.ID
	if _, dup := registry[id]; dup {
		panic("coreplugin.Register: duplicate plugin id " + id)
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
	// Feeds reads external RSS/Atom feeds (cached, SSRF-safe).
	Feeds   *feedfetch.Cache
	Workers *worker.Registry
	// Webhooks queues HTTP notifications (Emit) for events of the plugin.
	Webhooks *webhook.Service
	// Secrets seals secrets stored in the database (secrets.mode); register
	// the columns with secrets.Register so that "rewrap" finds them.
	Secrets *secrets.Box
	// Notify sends notifications in the wiki (bell) and by e-mail; declare
	// the kinds with notify.RegisterKind so users can configure the e-mails.
	Notify *notify.Service
	// Signatures signs records with the methods of signature plugins and
	// keeps sealed, verifiable evidence (see internal/signature).
	Signatures *signature.Service
	Log        *slog.Logger

	Mux       *http.ServeMux
	IsEnabled func(pluginID string) bool
	// Filters are the request filters of the app (RegisterRequestFilter).
	Filters *RequestFilters
	// Site is the name of the site this host belongs to ("main" for the
	// first site). Each site gets its own plugin instance and host.
	Site string
	// Scope identifies the app of this host in the process (see
	// identity.Scope); OnClose registers work for when the app closes.
	Scope   identity.Scope
	OnClose func(func())
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

// Limit returns the current value of a limit (of this plugin or the core).
func (h *Host) Limit(key string) int64 { return limits.Value(h.Config.Get(), key) }

// BodyLimit declares that a route of this plugin (method and path as given
// to Handle) accepts request bodies up to the limit key - a limit of the
// plugin's manifest or one of the core (limits.RequestContentKB for page
// content). Without it the general limit of requests applies.
func (h *Host) BodyLimit(method, path, key string) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	limits.RouteBody(method+" /api/v1/x/"+h.Manifest.ID+path, key)
}

// Signed declares that a route of this plugin (method and path as given to
// Handle) is called by other systems that authenticate with a signature the
// handler checks itself (incoming webhooks): the caller is always anonymous
// there, a session or API key never counts and no CSRF header is needed.
func (h *Host) Signed(method, path string) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	httpx.RegisterSigned(method + " /api/v1/x/" + h.Manifest.ID + path)
}

// RegisterElementType adds an element type owned by this plugin.
func (h *Host) RegisterElementType(t content.ElementType) {
	h.Content.RegisterType(h.Manifest.ID, t)
}

// RegisterDataKind registers a kind of data of this plugin for reports
// (content.DataKind; the kind starts with the plugin id). Blocks with data
// implement content.DataMacro; the plugin itself writes with PutData.
func (h *Host) RegisterDataKind(k content.DataKind) {
	h.Content.RegisterDataKind(h.Manifest.ID, k)
}

// PutData writes an entry of a kind of this plugin for an element (tx: the
// transaction of the change it follows from, or nil).
func (h *Host) PutData(ctx context.Context, tx pgx.Tx, elementID int64, lang, kind string, idx int, data map[string]any) error {
	return h.Content.PutData(ctx, tx, h.Manifest.ID, elementID, lang, kind, idx, data)
}

// DeleteData removes entries of a kind of this plugin (idx 0: all of the
// element and language; lang "": all languages).
func (h *Host) DeleteData(ctx context.Context, tx pgx.Tx, elementID int64, lang, kind string, idx int) error {
	return h.Content.DeleteData(ctx, tx, h.Manifest.ID, elementID, lang, kind, idx)
}

// RegisterMacro adds a macro for rich text (see content.Macro).
func (h *Host) RegisterMacro(m content.Macro) {
	h.Content.RegisterMacro(h.Manifest.ID, m)
}

// RegisterPictureMacro adds a macro whose result the visual editor may show
// while editing (it renders it again whenever its parameters change). Only
// for macros that are cheap and have no side effects: no requests to other
// servers, no commands, nothing written.
func (h *Host) RegisterPictureMacro(m content.Macro) {
	h.Content.RegisterMacroWith(h.Manifest.ID, m, content.MacroOptions{Picture: true})
}

// RegisterElementField adds data edited and saved together with elements
// of every type (see content.ElementField).
func (h *Host) RegisterElementField(f content.ElementField) {
	h.Content.RegisterElementField(h.Manifest.ID, f)
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

// RegisterMFAMethod adds a factor (see auth.MFAMethod and the optional
// interfaces MFAEnroller, MFASilent, MFAResetter, PasswordlessMethod). Its
// number in the chain is the plugin setting "factor" (default
// m.DefaultFactor()).
func (h *Host) RegisterMFAMethod(m auth.MFAMethod) {
	h.Auth.RegisterMFAMethod(h.Manifest.ID, m)
}

// RegisterSignatureMethod adds a way to sign (plugins of the kind
// "signature"); it is inactive while the plugin is disabled.
func (h *Host) RegisterSignatureMethod(m signature.Method) {
	h.Signatures.Register(m)
}

// RegisterAccessGuard adds checks of sign-ins and requests (see
// auth.AccessGuard).
func (h *Host) RegisterAccessGuard(g auth.AccessGuard) {
	h.Auth.RegisterAccessGuard(h.Manifest.ID, g)
}

// RegisterRequestFilter adds a filter that looks at every request before
// it is handled (see RequestFilter); it is inactive while the plugin is
// disabled.
func (h *Host) RegisterRequestFilter(f RequestFilter) {
	if h.Filters != nil {
		h.Filters.add(h.Manifest.ID, f)
	}
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

// CanElement reports whether the caller has the category permission action
// on an element in categoryID: the element's own rights apply (see
// acl.Service.ElementActions). Use it for everything about one element
// (comments, reactions …) instead of Can.
func (h *Host) CanElement(ctx context.Context, elementID, categoryID int64, action string) bool {
	return h.ACL.CanElementAction(ctx, identity.From(ctx), elementID, categoryID, action)
}

// CanInstance reports whether the caller has the instance permission action
// (declared with "instance" in plugin.json) on the instance element id that
// the caller may view (see acl.Service.CanInstance).
func (h *Host) CanInstance(ctx context.Context, elementID int64, action string) bool {
	inst, err := h.Content.Instance(ctx, elementID)
	if err != nil {
		return false
	}
	return h.ACL.CanInstance(ctx, identity.From(ctx), inst, action)
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

// UserCanElement reports whether user uid has want on an element in
// categoryID (its own rights apply), e.g. to notify only people who may see
// the page.
func (h *Host) UserCanElement(ctx context.Context, uid, elementID, categoryID int64, want acl.Perm) bool {
	p, err := h.Auth.LoadPrincipal(ctx, uid)
	if err != nil {
		return false
	}
	return h.ACL.CanElement(ctx, p, elementID, categoryID, want)
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
var PluginKinds = []string{KindElement, KindMacro, KindAuth, KindMFA, KindSignature, KindStorage, KindOffice, KindSearch, KindNotification,
	KindWorkflow, KindMenu, KindTemplate, KindIntegration, KindGeneral}

// ColorSchemes are the colour schemes a person or a template may choose
// ("system": as the device is set).
var ColorSchemes = []string{"system", "light", "dark"}

var lookRoot = regexp.MustCompile(`^\.[a-z][a-z0-9-]{0,40}$`)

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
	if m.ColorScheme != "" && (!slices.Contains(ColorSchemes, m.ColorScheme) || !slices.Contains(m.Kinds, KindTemplate)) {
		return fmt.Errorf("plugin %s: color_scheme %q: only for templates, one of %s", m.ID, m.ColorScheme, strings.Join(ColorSchemes, ", "))
	}
	if len(m.TemplateSettings) > 0 && !slices.Contains(m.Kinds, KindTemplate) {
		return fmt.Errorf("plugin %s: template_settings are only for templates", m.ID)
	}
	if m.TemplateLookRoot != "" && !lookRoot.MatchString(m.TemplateLookRoot) {
		return fmt.Errorf("plugin %s: template_look_root must be one class such as .name", m.ID)
	}
	if err := tplsettings.CheckDescription(m.TemplateSettings); err != nil {
		return fmt.Errorf("plugin %s: template_settings: %w", m.ID, err)
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
// NewInstance returns a fresh instance for an app. Other plugins get a new
// zero value of their type.
type Factory interface {
	NewInstance() Plugin
}

// NewInstance returns a fresh instance of p for an app (every app gets its
// own; the registered instance is only a template).
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

// PerSite keeps one value per app (site in a process), for package-level
// state that code without access to the plugin instance needs (renderers,
// hooks). Get finds the value of the app of the context (its scope); a
// context without one gets the newest open app of its site. The value of
// an app is removed when the app closes, so a closed app never shadows an
// open one (two apps of the same site in one process: tests, a command
// line inside a server process).
type PerSite[T any] struct {
	m identity.PerScope[T]
}

// Set stores the value of the app of host h.
func (p *PerSite[T]) Set(h *Host, v T) {
	p.m.Set(h.Scope, h.Site, v)
	if h.OnClose != nil {
		s := h.Scope
		h.OnClose(func() { p.m.Remove(s) })
	}
}

// Get returns the value of the app of ctx (zero value if none).
func (p *PerSite[T]) Get(ctx context.Context) T {
	v, _ := p.m.Get(ctx)
	return v
}

// RequestFilter looks at a request before the wiki handles it, for example
// a web application firewall. It runs after authentication, the request
// limits and the body size limit, for every request except calls of the
// CLI in the same process. It may read the body but must leave r.Body so
// that the handler reads the same bytes. A non-nil error refuses the request
// (written as the error answer, so use apperr errors).
type RequestFilter interface {
	FilterRequest(r *http.Request, clientIP string) error
}

// RequestFilters holds the request filters of the plugins of an app.
type RequestFilters struct {
	mu   sync.RWMutex
	list []registeredFilter
}

type registeredFilter struct {
	plugin string
	f      RequestFilter
}

func (r *RequestFilters) add(plugin string, f RequestFilter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.list = append(r.list, registeredFilter{plugin, f})
}

// Active returns the filters of the enabled plugins, in the order they were
// registered.
func (r *RequestFilters) Active(enabled func(string) bool) []RequestFilter {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []RequestFilter
	for _, x := range r.list {
		if enabled(x.plugin) {
			out = append(out, x.f)
		}
	}
	return out
}
