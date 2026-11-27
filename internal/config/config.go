// Package config implements the config-first settings model of Enterprise Wiki.
//
// The YAML file (default: config.yaml) is the single source of truth. The
// admin UI and the CLI never store settings anywhere else: they modify the
// file atomically (write temp file + rename) and every node reloads it.
//
// Values may reference environment variables with ${NAME} or ${NAME:-default}.
// References are kept verbatim when the file is written back, so secrets that
// live in .env or in the process environment are never copied into the file.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/mtsonline/enterprisewiki/internal/readability"
	"github.com/mtsonline/enterprisewiki/internal/tlsutil"
)

// Config is the complete configuration document.
type Config struct {
	Server    Server                    `yaml:"server" json:"server"`
	Mode      string                    `yaml:"mode" json:"mode"` // single | cluster
	Database  Database                  `yaml:"database" json:"database"`
	Storage   Storage                   `yaml:"storage" json:"storage"`
	Media     Media                     `yaml:"media" json:"media"`
	I18n      I18n                      `yaml:"i18n" json:"i18n"`
	UI        UI                        `yaml:"ui" json:"ui"`
	Auth      Auth                      `yaml:"auth" json:"auth"`
	Login     Login                     `yaml:"login,omitempty" json:"login"`
	Retention Retention                 `yaml:"retention" json:"retention"`
	Plugins   Plugins                   `yaml:"plugins" json:"plugins"`
	Mail      Mail                      `yaml:"mail" json:"mail"`
	Cache     Cache                     `yaml:"cache" json:"cache"`
	Webhooks  Webhooks                  `yaml:"webhooks" json:"webhooks"`
	Feeds     Feeds                     `yaml:"feeds" json:"feeds"`
	Lists     Lists                     `yaml:"lists" json:"lists"`
	Audit     Audit                     `yaml:"audit" json:"audit"`
	License   License                   `yaml:"license" json:"license"`
	Secrets   Secrets                   `yaml:"secrets,omitempty" json:"secrets"`
	Settings  map[string]map[string]any `yaml:"plugin_settings,omitempty" json:"plugin_settings,omitempty"`
	// TemplateSettings are the values each website template offers in its
	// settings (colours, fonts, logos …), per template id: only the values
	// changed from the defaults. Read and written only through
	// /api/v1/admin/templates/{id}/settings, which checks them against the
	// description of the template (never through the general settings).
	TemplateSettings map[string]map[string]any `yaml:"template_settings,omitempty" json:"-"`
	// Limits holds the changed technical limits (expert settings); what is
	// missing has the default declared by the core or the plugin (package
	// limits).
	Limits map[string]int64 `yaml:"limits,omitempty" json:"limits,omitempty"`
	Sites  Sites            `yaml:"sites" json:"sites"`
	// Maintenance: the wiki read-only or closed for everyone but
	// administrators (for example while the server moves).
	Maintenance Maintenance `yaml:"maintenance,omitempty" json:"maintenance"`
	// Consent: asking for consent to cookies and to the privacy policy.
	Consent Consent `yaml:"consent,omitempty" json:"consent"`
	// People: person pages and who sees e-mail addresses of people.
	People People `yaml:"people,omitempty" json:"people"`
}

// People configures what signed-in people see of other people (person
// page, menu on names). Who may open a person at all is a permission
// (core.people.view) together with the rules for finding people.
type People struct {
	// EmailVisibility: who sees the e-mail address of a person –
	// "users" (every signed-in person who may open the person; default),
	// "permission" (only with core.people.email) or "nobody".
	EmailVisibility string `yaml:"email_visibility,omitempty" json:"email_visibility"`
	// EmailHideAllowed: each person may hide their own address in their
	// profile (default true).
	EmailHideAllowed *bool `yaml:"email_hide_allowed,omitempty" json:"email_hide_allowed,omitempty"`
	// MentionDisplay: what readers who may not find a mentioned person see
	// in place of "@name" in pages and comments – "masked" (the first two
	// characters and ****, default) or "name" (the display name, without
	// the menu on names). Who may find the person sees the display name
	// with the menu (decision of the project owner, 26.11.2026).
	MentionDisplay string `yaml:"mention_display,omitempty" json:"mention_display"`
}

// MentionLevels are the values of people.mention_display.
var MentionLevels = []string{"masked", "name"}

// Mentions returns the configured display of mentions for readers who may
// not find the person ("masked" when unset or unknown).
func (p People) Mentions() string {
	if p.MentionDisplay == "name" {
		return "name"
	}
	return "masked"
}

// EmailLevels are the values of people.email_visibility.
var EmailLevels = []string{"users", "permission", "nobody"}

// Email returns the configured visibility of e-mail addresses ("users"
// when unset or unknown).
func (p People) Email() string {
	for _, v := range EmailLevels {
		if p.EmailVisibility == v {
			return v
		}
	}
	return "users"
}

// HideAllowed reports whether people may hide their own address.
func (p People) HideAllowed() bool { return p.EmailHideAllowed == nil || *p.EmailHideAllowed }

// Consent is the asking for consent (Administration → Consent).
type Consent struct {
	Cookies ConsentCookies `yaml:"cookies,omitempty" json:"cookies"`
	Privacy ConsentPrivacy `yaml:"privacy,omitempty" json:"privacy"`
}

// ConsentCookies: a banner asking which cookies and similar storage may be
// used beyond the necessary ones (the choice is kept in the browser).
type ConsentCookies struct {
	Enabled bool `yaml:"enabled,omitempty" json:"enabled"`
	// Required: the banner covers the page until a choice is made ("only
	// necessary" is a choice as well)
	Required bool `yaml:"required,omitempty" json:"required"`
	// Version: a higher version asks everybody again
	Version int `yaml:"version,omitempty" json:"version"`
	// Text per language (plain text); empty: a standard text
	Text map[string]string `yaml:"text,omitempty" json:"text"`
	// Page: the page with the cookie policy (0: none)
	Page int64 `yaml:"page,omitempty" json:"page"`
}

// Guest settings of the privacy consent.
const (
	ConsentGuestsNone     = ""         // nothing for guests
	ConsentGuestsNotice   = "notice"   // a notice with a link
	ConsentGuestsRequired = "required" // consent required (kept in the browser)
)

// ConsentPrivacy: people accept the privacy policy; signed-in people before
// they can use the wiki (enforced by the server for sessions in the
// browser; recorded per person and version).
type ConsentPrivacy struct {
	Enabled bool `yaml:"enabled,omitempty" json:"enabled"`
	// Version: a higher version asks everybody again
	Version int `yaml:"version,omitempty" json:"version"`
	// Page: the page with the privacy policy (required when enabled)
	Page int64 `yaml:"page,omitempty" json:"page"`
	// Guests: "", notice or required
	Guests string `yaml:"guests,omitempty" json:"guests"`
	// Text per language (plain text) shown with the policy; empty: a standard text
	Text map[string]string `yaml:"text,omitempty" json:"text"`
}

// ConsentTextMax is the most characters of a consent text per language.
const ConsentTextMax = 2000

// Maintenance modes.
const (
	MaintenanceReadOnly = "read_only" // everything can be read, nothing changed
	MaintenanceClosed   = "closed"    // only signing in; administrators work on
)

// Maintenance is the maintenance mode of the site. Administrators always
// keep working; signing in stays possible.
type Maintenance struct {
	// Mode: "" (off), read_only or closed
	Mode string `yaml:"mode,omitempty" json:"mode"`
	// Message: the text shown to everybody, per language (plain text)
	Message map[string]string `yaml:"message,omitempty" json:"message"`
	// Until: when the maintenance is expected to end (RFC 3339, shown only)
	Until string `yaml:"until,omitempty" json:"until"`
	// RunJobs: background jobs (e-mail, clean-up, retention …) go on during
	// maintenance; by default they wait, so nothing changes (a copy of the
	// database taken now does not send e-mails twice later)
	RunJobs bool `yaml:"run_jobs,omitempty" json:"run_jobs"`
}

// Active reports whether the maintenance mode is on.
func (m Maintenance) Active() bool { return m.Mode != "" }

// MaintenanceMessageMax is the most characters of the message per language.
const MaintenanceMessageMax = 2000

// Sites holds defaults for further sites of the instance.
type Sites struct {
	// Accounts of new sites: shared (the accounts of the instance) or
	// separate (own users, groups and roles per site). Each site keeps the
	// choice made when it was created.
	Accounts string `yaml:"accounts" json:"accounts"`
}

// Secrets configures how secrets in the database (signing keys of
// webhooks, authenticator apps, workflows and forms) are stored: plain
// (default), aes (AES-256-GCM with a key file) or pq (hybrid ML-KEM-1024 +
// X25519 with a key file). The key file can be protected by a password from
// the environment variable PasswordEnv. Instance-wide; see
// docs/en/admin/security.md.
type Secrets struct {
	Mode string `yaml:"mode,omitempty" json:"mode"`
	// KeyFile: default secrets.key next to config.yaml.
	KeyFile     string `yaml:"key_file,omitempty" json:"key_file"`
	PasswordEnv string `yaml:"password_env,omitempty" json:"password_env"`
}

// License holds the license key of the organisation (Administration →
// License, "ewiki license install"). Without a key the use is only allowed
// for private purposes of natural persons (see LICENSE).
type License struct {
	Key string `yaml:"key,omitempty" json:"key"`
}

// Webhooks configures HTTP notifications (the webhooks themselves are
// managed under Administration → Webhooks).
// Audit configures the audit log (Administration → Audit log → Settings).
// Security relevant areas (users, roles, permissions, sign-in, settings …)
// are always logged.
type Audit struct {
	// Disabled lists areas (the part of the action before the first dot,
	// e.g. "comment") that are not logged.
	Disabled []string `yaml:"disabled" json:"disabled"`
	// LogLogins records sign-ins, sign-outs and failed attempts (default true).
	LogLogins *bool `yaml:"log_logins,omitempty" json:"log_logins"`
	// LogDenied records requests refused for missing permissions (403).
	LogDenied bool `yaml:"log_denied" json:"log_denied"`
	// LogViews records who opened which page (off by default: much data,
	// personal data – check the data protection rules first).
	LogViews bool `yaml:"log_views" json:"log_views"`
	// Forward sends the entries to other systems.
	Forward []AuditForward `yaml:"forward" json:"forward"`
}

// Logins reports whether sign-ins are logged.
func (a Audit) Logins() bool { return a.LogLogins == nil || *a.LogLogins }

// AuditForward is a destination for the audit log.
type AuditForward struct {
	ID      string `yaml:"id" json:"id"`
	Name    string `yaml:"name" json:"name"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
	// Type: syslog (RFC 5424 over udp, tcp or tls), webhook (JSON, HMAC
	// signed), otlp (OpenTelemetry logs over HTTP), file (JSON lines below
	// <data>/logs) or stdout (JSON lines in the server log, for log drivers).
	Type     string `yaml:"type" json:"type"`
	Address  string `yaml:"address" json:"address"`   // host:port, URL or file name
	Protocol string `yaml:"protocol" json:"protocol"` // syslog: udp | tcp | tls
	Secret   string `yaml:"secret,omitempty" json:"secret"`
	// Headers are added to webhook and OTLP requests (e.g. Authorization).
	Headers map[string]string `yaml:"headers,omitempty" json:"headers"`
	// Areas limits the forwarded entries (empty = all).
	Areas []string `yaml:"areas,omitempty" json:"areas"`
}

// Lists limits lists from sources (macros, blog, FAQ, reports …).
type Lists struct {
	// MaxItems is the technical upper bound of entries one list shows in
	// total and of elements a search or filter source contributes
	// (default 1000, at most 10000). Editors choose their limit below it.
	MaxItems int `yaml:"max_items,omitempty" json:"max_items"`
}

// ListMax returns the effective upper bound of list entries.
func (l Lists) ListMax() int {
	switch {
	case l.MaxItems <= 0:
		return 1000
	case l.MaxItems < 50:
		return 50
	case l.MaxItems > 10000:
		return 10000
	}
	return l.MaxItems
}

// Feeds configures reading external RSS/Atom feeds (news, lists, carousel).
type Feeds struct {
	// AllowPrivate allows feeds on internal addresses (intranet servers).
	AllowPrivate bool `yaml:"allow_private,omitempty" json:"allow_private"`
}

type Webhooks struct {
	// AllowPrivate allows webhook URLs on internal addresses (10.x, 192.168.x,
	// localhost …), e.g. an on-premises chat server. Default false.
	AllowPrivate bool `yaml:"allow_private,omitempty" json:"allow_private"`
	// KeepDays is how long sent and failed deliveries are kept (default 30).
	KeepDays int `yaml:"keep_days,omitempty" json:"keep_days"`
	// AccountsInAll also sends the events of accounts (user.*, group.* –
	// personal data) to webhooks without a choice of events ("all events").
	// Default false: only to webhooks that chose them explicitly.
	AccountsInAll bool `yaml:"accounts_in_all,omitempty" json:"accounts_in_all"`
}

// Cache configures the optional cache for guests (see package pagecache).
type Cache struct {
	Enabled    bool `yaml:"enabled" json:"enabled"`
	TTLSeconds int  `yaml:"ttl_seconds" json:"ttl_seconds"` // 0 = until the next change
	MaxEntries int  `yaml:"max_entries" json:"max_entries"`
	MaxMB      int  `yaml:"max_mb" json:"max_mb"`
}

// Server holds HTTP settings.
type Server struct {
	Listen         string   `yaml:"listen" json:"listen"`
	BaseURL        string   `yaml:"base_url" json:"base_url"`
	SecureCookies  bool     `yaml:"secure_cookies" json:"secure_cookies"`
	TrustedProxies []string `yaml:"trusted_proxies" json:"trusted_proxies"`
	// ClientIPHeader names the header of the reverse proxies with the
	// address of the client: "x-forwarded-for" (default), "forwarded"
	// (RFC 7239) or a header with one address (X-Real-IP,
	// CF-Connecting-IP ...). Only read from trusted_proxies.
	ClientIPHeader string `yaml:"client_ip_header,omitempty" json:"client_ip_header"`
	MaxBodyMB      int    `yaml:"max_body_mb" json:"max_body_mb"`
	// RateLimit limits API requests per account or address.
	RateLimit RateLimit `yaml:"rate_limit,omitempty" json:"rate_limit"`
}

// RateLimit switches the limits of API requests per account or address
// off (load tests). The rates and bursts are technical limits (expert
// settings, core.rate.*).
type RateLimit struct {
	Disabled bool `yaml:"disabled,omitempty" json:"disabled"`
}

// Database holds the PostgreSQL connection.
type Database struct {
	URL      string `yaml:"url" json:"url"`
	MaxConns int    `yaml:"max_conns" json:"max_conns"`
}

// Storage configures where binary data (attachments) is stored.
type Storage struct {
	Path        string `yaml:"path" json:"path"`
	MaxUploadMB int    `yaml:"max_upload_mb" json:"max_upload_mb"`
}

// Media configures how files of the media library are opened.
type Media struct {
	// Viewer is the default program for files: "internal" (the browser) or
	// the id of an active office plugin (e.g. office.collabora). Files the
	// program cannot open are shown internally.
	Viewer string `yaml:"viewer,omitempty" json:"viewer,omitempty"`
	// OpenExternal opens files the default program supports there right
	// away (instead of the preview with "Open in ..."). Only possible for
	// one program: the default viewer.
	OpenExternal bool `yaml:"open_external,omitempty" json:"open_external,omitempty"`
	// TokenTTL is the lifetime of the access token an office program gets
	// for one file (default 10h, at most 24h).
	TokenTTL string `yaml:"token_ttl,omitempty" json:"token_ttl,omitempty"`
	// EmbedAllow lists the services whose videos and pages may be embedded
	// in content (macro embed), as origins (https://player.example.com).
	// The same list opens the Content-Security-Policy (frame-src) for them.
	// Empty (default): nothing from outside the wiki is embedded.
	EmbedAllow []string `yaml:"embed_allow,omitempty" json:"embed_allow"`
	// FormAllow lists the services that sign-up forms in content (block
	// newsletter) may send to, as origins (https://news.example.com). The
	// same list opens the Content-Security-Policy (form-action) for them.
	// Empty (default): no form sends anything outside the wiki.
	FormAllow []string `yaml:"form_allow,omitempty" json:"form_allow"`
}

// I18n configures UI and content languages.
type I18n struct {
	DefaultLanguage string   `yaml:"default_language" json:"default_language"`
	Languages       []string `yaml:"languages" json:"languages"`
}

// UI configures the frontend.
type UI struct {
	SiteName string `yaml:"site_name" json:"site_name"`
	Template string `yaml:"template" json:"template"`
	// HomeCategory is shown on the start page (0 = first root category).
	HomeCategory int64 `yaml:"home_category" json:"home_category"`
	// HomeElement is shown as start page (wins over HomeCategory; 0 = none).
	HomeElement int64 `yaml:"home_element" json:"home_element"`
	// Prerender embeds the content of a page as plain HTML for guests
	// (search engines, visitors without JavaScript). Default true.
	Prerender bool `yaml:"prerender" json:"prerender"`
	// PreviewImage is the image for link previews of pages without an own
	// image (path like /api/v1/files/… or http(s) URL; "" = none).
	PreviewImage string `yaml:"preview_image" json:"preview_image"`
	// ReadableURLs shows readable paths (/wiki/<categories>/<page>, one per
	// language) instead of /e/12 and /c/7. Default true.
	ReadableURLs bool `yaml:"readable_urls" json:"readable_urls"`
	// Editor is the default editor of text pages: "visual" (ProseMirror,
	// default) or "simple" (plain contenteditable). Users may choose their
	// own in the profile; both save Markdown.
	Editor string `yaml:"editor" json:"editor"`
	// Collaboration lets several users edit a page at the same time in the
	// visual editor (live, with their cursors). Default true.
	Collaboration bool `yaml:"collaboration" json:"collaboration"`
	// PersonalPlacement lets signed-in people arrange the building blocks of
	// the interface for themselves (search, notifications …) at the
	// positions of the template. Default false; the own dashboard is always
	// arranged by its owner.
	PersonalPlacement bool `yaml:"personal_placement,omitempty" json:"personal_placement"`
	// CustomCSS allows own CSS in templates of data (checked on saving: no
	// @import, url() only to files of the wiki …). Default false; own CSS can
	// make the wiki less accessible.
	CustomCSS bool `yaml:"custom_css,omitempty" json:"custom_css"`
	// EditorTools are the buttons of the formatting toolbar of the text
	// editors (content.EditorTools; empty or ["*"] = all). Categories may
	// choose their own; hiding a button does not forbid the formatting.
	EditorTools []string `yaml:"editor_tools,omitempty" json:"editor_tools"`
	// DashboardTiles are the tiles everybody may put on the own dashboard
	// (block ids; empty = all). The default arrangement and required tiles
	// are placements of the site.
	DashboardTiles []string `yaml:"dashboard_tiles,omitempty" json:"dashboard_tiles"`
	// DashboardHome makes the own dashboard the start page of signed-in
	// people (also right after signing in). The start page of the site
	// stays at /start. Default false.
	DashboardHome bool `yaml:"dashboard_home,omitempty" json:"dashboard_home"`
	// NotFoundElement and ForbiddenElement are pages whose content is shown
	// on the page "not found" and "no permission" (0 = the standard text).
	// Readers see them only with their own rights: choose pages everybody
	// may see.
	NotFoundElement  int64 `yaml:"not_found_element,omitempty" json:"not_found_element"`
	ForbiddenElement int64 `yaml:"forbidden_element,omitempty" json:"forbidden_element"`
	// Readability is the hint on long sentences and long words while
	// writing in the editors (off by default).
	Readability Readability `yaml:"readability,omitempty" json:"readability"`
	// ChoiceCheckboxes: lists of up to this many entries (groups, roles …)
	// are offered as checkboxes, longer ones as a list with a filter and
	// several choices (0 = ChoiceCheckboxesDefault, at most ChoiceCheckboxesMax).
	ChoiceCheckboxes int `yaml:"choice_checkboxes,omitempty" json:"choice_checkboxes"`
}

// Lists offered as checkboxes (UI.ChoiceCheckboxes).
const (
	ChoiceCheckboxesDefault = 5
	ChoiceCheckboxesMax     = 50
)

// Readability configures the hint on long sentences and long words while
// writing (internal/readability). It never blocks saving and never changes
// the text.
type Readability struct {
	// Mode: "off" (or empty, the default), "offered" (each person may
	// switch it on in the profile) or "on" (on for everybody; each person
	// may switch it off).
	Mode string `yaml:"mode,omitempty" json:"mode"`
	// SentenceWords is the most words of a sentence (0 = 20).
	SentenceWords int `yaml:"sentence_words,omitempty" json:"sentence_words"`
	// WordLetters is the most letters of a word (0 = 22).
	WordLetters int `yaml:"word_letters,omitempty" json:"word_letters"`
}

// ValidateReadability checks the settings of the readability hint.
func ValidateReadability(r Readability) error {
	if r.Mode != "" && !slices.Contains(readability.Modes, r.Mode) {
		return fmt.Errorf("ui.readability.mode must be off, offered or on, got %q", r.Mode)
	}
	if n := r.SentenceWords; n != 0 && (n < readability.MinSentenceWords || n > readability.MaxSentenceWords) {
		return fmt.Errorf("ui.readability.sentence_words must be 0 (default %d) or %d to %d", readability.DefaultSentenceWords, readability.MinSentenceWords, readability.MaxSentenceWords)
	}
	if n := r.WordLetters; n != 0 && (n < readability.MinWordLetters || n > readability.MaxWordLetters) {
		return fmt.Errorf("ui.readability.word_letters must be 0 (default %d) or %d to %d", readability.DefaultWordLetters, readability.MinWordLetters, readability.MaxWordLetters)
	}
	return nil
}

// Auth configures authentication.
type Auth struct {
	// Order of password based providers. "local" is always tried first,
	// regardless of its position, so local (break-glass) accounts keep working
	// when an external identity provider is down.
	Order      []string `yaml:"order" json:"order"`
	SessionTTL string   `yaml:"session_ttl" json:"session_ttl"`
	// SessionIdle ends a browser session after this time without requests
	// ("0" = only the absolute session_ttl).
	SessionIdle     string `yaml:"session_idle" json:"session_idle"`
	APIKeyMaxTTL    string `yaml:"api_key_max_ttl" json:"api_key_max_ttl"`
	SSHTokenTTL     string `yaml:"ssh_token_ttl" json:"ssh_token_ttl"`
	MaxLoginFails   int    `yaml:"max_login_failures" json:"max_login_failures"`
	LockoutDuration string `yaml:"lockout_duration" json:"lockout_duration"`
	MinPasswordLen  int    `yaml:"min_password_length" json:"min_password_length"`
	// Profile photos: who sees them unless the person chooses (private:
	// the person and administrators; users: signed-in people; public:
	// everybody), the widest a person may choose, and whether the default
	// is fixed (no choice).
	PhotoVisibility      string `yaml:"photo_visibility" json:"photo_visibility"`
	PhotoVisibilityMax   string `yaml:"photo_visibility_max" json:"photo_visibility_max"`
	PhotoVisibilityFixed bool   `yaml:"photo_visibility_fixed" json:"photo_visibility_fixed"`
}

// Login configures the sign-in page.
type Login struct {
	// PasskeyAutofill offers passkeys in the suggestions of the user name
	// field (WebAuthn conditional mediation), next to the passkey button.
	// Default on; switch it off if it causes trouble with assistive
	// technology.
	PasskeyAutofill *bool `yaml:"passkey_autofill,omitempty" json:"passkey_autofill,omitempty"`
	// AutoRedirect sends the sign-in page straight to this redirect provider
	// (e.g. "oidc-entra"). The page itself stays reachable at
	// /login?local=1, after a sign-out and after errors.
	AutoRedirect string `yaml:"auto_redirect,omitempty" json:"auto_redirect"`
	// Buttons: how each redirect provider appears on the sign-in page.
	Buttons map[string]LoginButton `yaml:"buttons,omitempty" json:"buttons"`
	// Registration: people may create a local account themselves (off by
	// default). The account exists only after the confirmation link was used.
	Registration Registration `yaml:"registration,omitempty" json:"registration"`
	// PasswordReset offers "Forgot your password?" for local accounts (a
	// link by e-mail). Default on; switch it off to hide it.
	PasswordReset *bool `yaml:"password_reset,omitempty" json:"password_reset,omitempty"`
}

// ResetEnabled reports whether "Forgot your password?" is switched on.
func (l Login) ResetEnabled() bool { return l.PasswordReset == nil || *l.PasswordReset }

// Registration of local accounts by the people themselves.
type Registration struct {
	Enabled bool `yaml:"enabled,omitempty" json:"enabled"`
	// Domains: only addresses of these domains ("example.com", or
	// "*.example.com" for its subdomains); empty: any address.
	Domains []string `yaml:"domains,omitempty" json:"domains"`
	// Groups new accounts join (never groups with an administrator role).
	Groups []int64 `yaml:"groups,omitempty" json:"groups"`
}

// LoginButton is the sign-in button of one redirect provider.
type LoginButton struct {
	// Order on the page: lower first, then by name.
	Order int `yaml:"order,omitempty" json:"order"`
	// Hidden: no button; the provider stays usable through its direct link
	// /login?via=<id>.
	Hidden bool `yaml:"hidden,omitempty" json:"hidden"`
	// Label replaces the provider name on the button ("Sign in with ...").
	Label string `yaml:"label,omitempty" json:"label"`
}

// Autofill reports whether passkeys are offered in the user name field.
func (l Login) Autofill() bool { return l.PasskeyAutofill == nil || *l.PasskeyAutofill }

// Retention configures automatic cleanup. Nothing is ever deleted that is the
// current state of an object: only older revisions and trashed objects.
type Retention struct {
	RevisionsKeepDays int `yaml:"revisions_keep_days" json:"revisions_keep_days"` // 0 = keep forever
	RevisionsKeepMin  int `yaml:"revisions_keep_min" json:"revisions_keep_min"`
	TrashDays         int `yaml:"trash_days" json:"trash_days"` // 0 = never purge
	AuditDays         int `yaml:"audit_days" json:"audit_days"` // 0 = keep forever
	// MailDays removes sent e-mails from the outbox after this many days (0 = keep).
	MailDays int `yaml:"mail_days" json:"mail_days"`
}

// Plugins configures the plugin system.
type Plugins struct {
	Disabled []string `yaml:"disabled" json:"disabled"`
	// Enabled lists plugins that are off by default (manifest default_off)
	// and were switched on.
	Enabled []string       `yaml:"enabled,omitempty" json:"enabled"`
	Dir     string         `yaml:"dir" json:"dir"`
	Catalog []CatalogEntry `yaml:"catalog" json:"catalog"`
	Macros  MacroSecurity  `yaml:"macros" json:"macros"`
	// Updates configures the update check (GitHub releases).
	Updates Updates `yaml:"updates" json:"updates"`
	// AutoRestartOnDrop restarts automatically after plugins copied into the
	// plugin folder were built (default: the administration offers "Restart now").
	AutoRestartOnDrop bool `yaml:"auto_restart_on_drop" json:"auto_restart_on_drop"`
	// TrustedSources are the sources of the organisation's own core plugins
	// (address prefixes or local directories). Core plugins from any other
	// source are "external" and need an explicit confirmation.
	TrustedSources []TrustedSource `yaml:"trusted_sources,omitempty" json:"trusted_sources"`
	// AllowExternal permits core plugins from external sources at all
	// (default true; each installation still needs a confirmation).
	AllowExternal *bool `yaml:"allow_external,omitempty" json:"allow_external,omitempty"`
	// Credentials are access data for plugin sources in private
	// repositories (installing, updating, the update check). They are never
	// part of an answer of the API.
	Credentials []SourceCredential `yaml:"credentials,omitempty" json:"-"`
}

// SourceCredential is the access data for the sources below an https
// address prefix (https://github.com/acme or https://git.example.com).
type SourceCredential struct {
	Prefix string `yaml:"prefix" json:"prefix"`
	// Username for the address (default x-access-token, as GitHub expects
	// for tokens).
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	// Token (or password); ${ENV} references are expanded.
	Token string `yaml:"token" json:"-"`
	Note  string `yaml:"note,omitempty" json:"note,omitempty"`
}

// ExternalAllowed reports whether core plugins from external sources may be
// installed (with confirmation).
func (p Plugins) ExternalAllowed() bool { return p.AllowExternal == nil || *p.AllowExternal }

// TrustedSource is a source of the organisation's own core plugins.
type TrustedSource struct {
	// Prefix is an address prefix (https://git.example.com/wiki-plugins,
	// ssh://git@git.example.com/team) or an absolute local directory.
	Prefix string `yaml:"prefix" json:"prefix"`
	Note   string `yaml:"note,omitempty" json:"note,omitempty"`
}

// Updates configures the update check against GitHub releases.
type Updates struct {
	// Check enables the periodic check (default true; manual checks always work).
	Check *bool `yaml:"check,omitempty" json:"check,omitempty"`
	// CoreRepo is the repository of the core (default github.com/mtsonline/enterpriseWiki).
	CoreRepo string `yaml:"core_repo,omitempty" json:"core_repo,omitempty"`
	// APIURL is the GitHub API base URL (default https://api.github.com,
	// set it for GitHub Enterprise).
	APIURL string `yaml:"api_url,omitempty" json:"api_url,omitempty"`
	// Token is an optional GitHub token (use ${EWIKI_GITHUB_TOKEN}) for
	// private repositories and higher rate limits.
	Token string `yaml:"token,omitempty" json:"-"`
}

// CatalogEntry lists a plugin that can be installed with one click.
type CatalogEntry struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	Repo        string `yaml:"repo" json:"repo"`
	Ref         string `yaml:"ref,omitempty" json:"ref,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// MacroSecurity configures dangerous macros.
//
// These settings can only be changed in config.yaml (or with "ewiki config
// set"), never through the web interface or the API.
type MacroSecurity struct {
	// ShellEnabled switches the shell macro on (default off).
	ShellEnabled bool `yaml:"shell_enabled" json:"shell_enabled"`
	// ShellApprovers are the superadmins (name@provider) who may approve
	// shell commands. The local command line can always approve.
	ShellApprovers []string `yaml:"shell_approvers" json:"shell_approvers"`
	// ShellTimeout limits each command run (default 10s).
	ShellTimeout string `yaml:"shell_timeout" json:"shell_timeout"`
	// ShellUser is the unprivileged system user that runs the commands when
	// the wiki runs as root (default "nobody").
	ShellUser string `yaml:"shell_user" json:"shell_user"`
	// HTTPAllow lists URL prefixes the HTTP macro may fetch
	// (e.g. https://status.example.com/). Empty = HTTP macro disabled.
	HTTPAllow []string `yaml:"http_allow" json:"http_allow"`
	// HTTPAllowPrivate permits private/loopback addresses (intranet services).
	HTTPAllowPrivate bool `yaml:"http_allow_private" json:"http_allow_private"`
}

// Mail configures outgoing e-mail (notifications, workflow e-mails).
type Mail struct {
	// Enabled switches all e-mail off when false (nothing is queued or sent;
	// for test systems or installations without e-mail). Default true.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled"`
	// Transport: auto (SMTP if a host is set, else the local sendmail
	// binary if present, else keep messages in the outbox), smtp, sendmail,
	// log (only write to the server log, for tests) or none (keep in the outbox).
	Transport string `yaml:"transport" json:"transport"`
	// From is the sender address of all messages (e.g. wiki@example.com).
	From     string `yaml:"from" json:"from"`
	FromName string `yaml:"from_name" json:"from_name"`
	ReplyTo  string `yaml:"reply_to,omitempty" json:"reply_to"`
	// AllowedFrom lists further sender addresses workflows may use
	// (addresses or *@domain). Other requested senders become Reply-To.
	AllowedFrom   []string `yaml:"allowed_from,omitempty" json:"allowed_from"`
	SubjectPrefix string   `yaml:"subject_prefix,omitempty" json:"subject_prefix"`
	SMTP          SMTP     `yaml:"smtp" json:"smtp"`
	// Sendmail is the path of the sendmail binary (default /usr/sbin/sendmail).
	Sendmail string `yaml:"sendmail,omitempty" json:"sendmail"`
	// Concurrency is the number of parallel SMTP connections (default 4).
	Concurrency int `yaml:"concurrency,omitempty" json:"concurrency"`
	// RatePerMinute limits messages per minute (0 = unlimited), e.g. 30 for
	// Microsoft 365 mailboxes.
	RatePerMinute int `yaml:"rate_per_minute,omitempty" json:"rate_per_minute"`
	// Bounce processes delivery failures that arrive later as bounce mails.
	Bounce Bounce `yaml:"bounce" json:"bounce"`
	// Digests of watches: daily at DigestHour (0–23), weekly on
	// DigestWeekday (0 = Sunday … 6 = Saturday) at that hour, in
	// DigestTimezone (IANA name, default: time zone of the server).
	DigestHour     int    `yaml:"digest_hour" json:"digest_hour"`
	DigestWeekday  int    `yaml:"digest_weekday" json:"digest_weekday"`
	DigestTimezone string `yaml:"digest_timezone,omitempty" json:"digest_timezone"`
}

// DigestLocation returns the time zone of watch digests.
func (m Mail) DigestLocation() *time.Location {
	if m.DigestTimezone != "" {
		if l, err := time.LoadLocation(m.DigestTimezone); err == nil {
			return l
		}
	}
	return time.Local
}

// IsEnabled reports whether e-mail is switched on.
func (m Mail) IsEnabled() bool { return m.Enabled == nil || *m.Enabled }

// Bounce configures the bounce mailbox: the envelope sender (Return-Path)
// of all messages is Address (with +tokens when VERP is on); the wiki reads
// the mailbox via IMAP and reports failed recipients.
type Bounce struct {
	Address string `yaml:"address,omitempty" json:"address"` // e.g. bounces@example.com
	// VERP adds +m<message>r<recipient> to the address (the mailbox must
	// accept sub-addresses); each recipient then gets its own SMTP transaction.
	VERP     bool   `yaml:"verp,omitempty" json:"verp"`
	IMAPHost string `yaml:"imap_host,omitempty" json:"imap_host"`
	IMAPPort int    `yaml:"imap_port,omitempty" json:"imap_port"`         // default 993
	Security string `yaml:"imap_security,omitempty" json:"imap_security"` // tls (default) | starttls | none
	Username string `yaml:"imap_username,omitempty" json:"imap_username"`
	Password string `yaml:"imap_password,omitempty" json:"imap_password"` // ${EWIKI_BOUNCE_PASSWORD}
	Folder   string `yaml:"imap_folder,omitempty" json:"imap_folder"`     // default INBOX
	// Delete processed bounce mails (default: only mark them as read).
	Delete     bool `yaml:"delete_processed,omitempty" json:"delete_processed"`
	SkipVerify bool `yaml:"skip_verify,omitempty" json:"skip_verify"`
	// CA certificate (PEM or file path) trusted for the IMAP server.
	CA string `yaml:"ca_cert,omitempty" json:"ca_cert"`
}

// SMTP is an SMTP relay.
type SMTP struct {
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port" json:"port"`         // default 587 (starttls), 465 (tls), 25 (none)
	Security string `yaml:"security" json:"security"` // starttls | tls | none
	Username string `yaml:"username,omitempty" json:"username"`
	// Password; use ${EWIKI_SMTP_PASSWORD} with the value in .env.
	Password   string `yaml:"password,omitempty" json:"password"`
	SkipVerify bool   `yaml:"skip_verify,omitempty" json:"skip_verify"`
	HeloName   string `yaml:"helo_name,omitempty" json:"helo_name"`
	// CA certificate (PEM or file path) trusted for the relay, e.g. an
	// internal certificate authority (instead of skip_verify).
	CA string `yaml:"ca_cert,omitempty" json:"ca_cert"`
}

// Defaults returns a configuration with sane defaults.
func Defaults() *Config {
	return &Config{
		Server:   Server{Listen: ":8080", BaseURL: "http://localhost:8080", MaxBodyMB: 64},
		Mode:     "single",
		Database: Database{URL: "${EWIKI_DATABASE_URL}", MaxConns: 50},
		Storage:  Storage{Path: "./data", MaxUploadMB: 100},
		I18n:     I18n{DefaultLanguage: "en", Languages: []string{"en", "de"}},
		UI:       UI{SiteName: "Enterprise Wiki", Template: "theme.wiki", Prerender: true, ReadableURLs: true, Collaboration: true},
		Auth: Auth{
			Order: []string{"local"}, SessionTTL: "168h", SessionIdle: "12h", PhotoVisibility: "users", PhotoVisibilityMax: "public", APIKeyMaxTTL: "8760h", SSHTokenTTL: "1h",
			MaxLoginFails: 10, LockoutDuration: "15m", MinPasswordLen: 12,
		},
		Retention: Retention{RevisionsKeepDays: 0, RevisionsKeepMin: 50, TrashDays: 30, AuditDays: 0, MailDays: 90},
		Mail:      Mail{Transport: "auto", FromName: "Enterprise Wiki", SMTP: SMTP{Security: "starttls"}, DigestHour: 7, DigestWeekday: 1},
		Plugins:   Plugins{Dir: "./plugins"},
		Sites:     Sites{Accounts: "shared"},
		Cache:     Cache{Enabled: false, TTLSeconds: 300, MaxEntries: 5000, MaxMB: 128},
	}
}

// Duration parses a duration setting, falling back to def.
func Duration(s string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// Expand replaces ${VAR} and ${VAR:-default} references.
func Expand(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		p := envRef.FindStringSubmatch(m)
		if v, ok := os.LookupEnv(p[1]); ok && v != "" {
			return v
		}
		return p[3]
	})
}

// HasEnvRef reports whether s contains a ${VAR} reference.
func HasEnvRef(s string) bool { return envRef.MatchString(s) }

// EnvRefError refuses a ${VAR} reference written by the wiki itself.
type EnvRefError struct{ Path string }

func (e *EnvRefError) Error() string {
	return fmt.Sprintf("%s: references to environment variables (${…}) can only be written in config.yaml on the server", e.Path)
}

// ClientError marks the error as one of the request (400).
func (e *EnvRefError) ClientError() bool { return true }

// envRefs calls fn for every string with a ${VAR} reference, with its path;
// list positions are left out of the path, so that reordering a list keeps
// its references.
func envRefs(v reflect.Value, path string, fn func(path, value string)) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			envRefs(v.Elem(), path, fn)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).IsExported() {
				envRefs(v.Field(i), path+"."+t.Field(i).Name, fn)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			envRefs(v.Index(i), path+"[]", fn)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			envRefs(v.MapIndex(k), path+"."+fmt.Sprint(k.Interface()), fn)
		}
	case reflect.String:
		if HasEnvRef(v.String()) {
			fn(path, v.String())
		}
	}
}

// expandAll walks the struct and expands every string field in place.
func expandAll(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			expandAll(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() || v.Field(i).Kind() == reflect.Struct || v.Field(i).Kind() == reflect.Slice || v.Field(i).Kind() == reflect.Map {
				expandAll(v.Field(i))
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			expandAll(v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			e := v.MapIndex(k)
			if e.Kind() == reflect.Interface && !e.IsNil() {
				if s, ok := e.Interface().(string); ok {
					v.SetMapIndex(k, reflect.ValueOf(Expand(s)))
					continue
				}
				if m, ok := e.Interface().(map[string]any); ok {
					expandAll(reflect.ValueOf(m))
				}
			}
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString(Expand(v.String()))
		}
	}
}

// LoadDotEnv loads KEY=VALUE lines from path into the process environment.
// Existing environment variables win. A missing file is not an error.
func LoadDotEnv(path string) error {
	f, err := os.Open(path) // #nosec G304 -- .env next to the configuration
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		if _, exists := os.LookupEnv(k); !exists {
			if err := os.Setenv(k, v); err != nil {
				return fmt.Errorf("%s: %s: %w", path, k, err)
			}
		}
	}
	return sc.Err()
}

// Store owns the config file. Raw() returns the document as written
// (with ${VAR} references), Get() the expanded, effective configuration.
type Store struct {
	path      string
	mu        sync.RWMutex
	raw       *Config
	effective *Config
	modTime   time.Time
	listeners []func(*Config)
	// base and site are set for the settings of a further site (see OpenSite).
	base *Store
	site string
}

// Open loads the config file. If it does not exist, defaults are used and
// written on the first Update.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// Path returns the config file path.
func (s *Store) Path() string { return s.path }

// Reload re-reads the file from disk.
func (s *Store) Reload() error {
	if s.base != nil {
		return s.reloadSite()
	}
	raw := Defaults()
	var mt time.Time
	b, err := os.ReadFile(s.path) // #nosec G304 -- the configuration file
	switch {
	case err == nil:
		if err := yaml.Unmarshal(b, raw); err != nil {
			return fmt.Errorf("config %s: %w", s.path, err)
		}
		if st, err := os.Stat(s.path); err == nil {
			mt = st.ModTime()
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return err
	}
	if err := Validate(raw); err != nil {
		return fmt.Errorf("config %s: %w", s.path, err)
	}
	eff, err := clone(raw)
	if err != nil {
		return err
	}
	expandAll(reflect.ValueOf(eff))
	applyEnv(eff)
	s.resolvePaths(eff)
	s.mu.Lock()
	s.raw, s.effective, s.modTime = raw, eff, mt
	ls := append([]func(*Config){}, s.listeners...)
	s.mu.Unlock()
	for _, l := range ls {
		l(eff)
	}
	return nil
}

// Get returns the effective configuration. Callers must not modify it.
func (s *Store) Get() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.effective
}

// Raw returns a deep copy of the raw document (env references intact).
func (s *Store) Raw() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, _ := clone(s.raw)
	return c
}

// OnChange registers a callback invoked after every successful (re)load.
func (s *Store) OnChange(f func(*Config)) {
	s.mu.Lock()
	s.listeners = append(s.listeners, f)
	s.mu.Unlock()
}

// Update applies fn to a copy of the raw document, validates it, writes the
// file atomically and reloads.
//
// It is the way of the wiki itself (API, interface, plugins, site
// settings): a value it writes must not bring a new ${VAR} reference, which
// would read the environment of the server (database password, key
// passwords, …) and show it or send it elsewhere. References already in the
// file stay. Only the operator writes new ones (UpdateTrusted).
func (s *Store) Update(fn func(c *Config) error) error { return s.update(fn, false) }

// UpdateTrusted is Update for the operator of the server (the command line
// on the server, "ewiki config set"): values may reference the environment.
func (s *Store) UpdateTrusted(fn func(c *Config) error) error { return s.update(fn, true) }

func (s *Store) update(fn func(c *Config) error, trusted bool) error {
	c := s.Raw()
	before := s.Raw()
	if err := fn(c); err != nil {
		return err
	}
	if !trusted {
		// every reference was already there, at the same place, at most as
		// often (a copy into a new list entry is a new one)
		known := map[string]int{}
		envRefs(reflect.ValueOf(before), "", func(path, v string) { known[path+"\x00"+v]++ })
		var bad string
		envRefs(reflect.ValueOf(c), "", func(path, v string) {
			if known[path+"\x00"+v]--; known[path+"\x00"+v] < 0 && bad == "" {
				bad = path
			}
		})
		if bad != "" {
			return &EnvRefError{Path: strings.TrimPrefix(bad, ".")}
		}
	}
	if err := Validate(c); err != nil {
		return err
	}
	if s.base != nil {
		if err := s.writeSite(c); err != nil {
			return err
		}
		return s.Reload()
	}
	if err := WriteFile(s.path, c); err != nil {
		return err
	}
	return s.Reload()
}

// Changed reports whether the file on disk is newer than the loaded one.
func (s *Store) Changed() bool {
	st, err := os.Stat(s.path)
	if err != nil {
		return s.base != nil && !s.modTime.IsZero()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !st.ModTime().Equal(s.modTime)
}

const header = `# Enterprise Wiki configuration - this file is the single source of truth.
# Settings changed in the admin UI or via "ewiki config set" are written here.
# Values can reference environment variables (e.g. from .env): ${NAME} or ${NAME:-default}.
# Reference: docs/en/admin/configuration.md
`

// WriteFile writes c to path atomically.
func WriteFile(path string, c *Config) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(header + string(b)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if st, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmp.Name(), st.Mode().Perm())
	} else {
		_ = os.Chmod(tmp.Name(), 0o600)
	}
	return os.Rename(tmp.Name(), path)
}

var envNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)

var pluginIDRe = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

var langRe = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)

// ValidLanguage reports whether code is an accepted language tag (en, de, de-AT).
func ValidLanguage(code string) bool { return langRe.MatchString(code) }

// Validate checks a configuration document.
func Validate(c *Config) error {
	if m := c.Secrets.Mode; m != "" && m != "plain" && m != "aes" && m != "pq" {
		return fmt.Errorf("secrets.mode must be plain, aes or pq, got %q", m)
	}
	if e := c.Secrets.PasswordEnv; e != "" && !envNameRe.MatchString(e) {
		return fmt.Errorf("secrets.password_env must be the name of an environment variable")
	}
	if c.Sites.Accounts != "" && c.Sites.Accounts != "shared" && c.Sites.Accounts != "separate" {
		return fmt.Errorf("sites.accounts must be 'shared' or 'separate', got %q", c.Sites.Accounts)
	}
	if err := tlsutil.Validate(c.Mail.SMTP.CA); err != nil && !strings.Contains(c.Mail.SMTP.CA, "${") {
		return fmt.Errorf("mail.smtp.ca_cert: %w", err)
	}
	if v := c.Media.Viewer; v != "" && v != "internal" && !pluginIDRe.MatchString(v) {
		return fmt.Errorf("media.viewer must be 'internal' or a plugin id, got %q", v)
	}
	if c.Media.OpenExternal && (c.Media.Viewer == "" || c.Media.Viewer == "internal") {
		return errors.New("media.open_external needs an office plugin as media.viewer")
	}
	if t := c.Media.TokenTTL; t != "" {
		if d, err := time.ParseDuration(t); err != nil || d < time.Minute || d > 24*time.Hour {
			return fmt.Errorf("media.token_ttl must be a duration between 1m and 24h, got %q", t)
		}
	}
	if len(c.Media.EmbedAllow) > MaxEmbedOrigins {
		return fmt.Errorf("media.embed_allow: at most %d services", MaxEmbedOrigins)
	}
	for _, o := range c.Media.EmbedAllow {
		if !EmbedOrigin(o) {
			return fmt.Errorf("media.embed_allow: %q is not an origin like https://player.example.com (https, lower case, no path)", o)
		}
	}
	if len(c.Media.FormAllow) > MaxEmbedOrigins {
		return fmt.Errorf("media.form_allow: at most %d services", MaxEmbedOrigins)
	}
	for _, o := range c.Media.FormAllow {
		if !EmbedOrigin(o) {
			return fmt.Errorf("media.form_allow: %q is not an origin like https://news.example.com (https, lower case, no path)", o)
		}
	}
	if err := ValidateConsent(c.Consent); err != nil {
		return err
	}
	if err := ValidateMaintenance(c.Maintenance); err != nil {
		return err
	}
	if n := c.UI.ChoiceCheckboxes; n < 0 || n > ChoiceCheckboxesMax {
		return fmt.Errorf("ui.choice_checkboxes must be 0 (default %d) to %d, got %d", ChoiceCheckboxesDefault, ChoiceCheckboxesMax, n)
	}
	if err := ValidateReadability(c.UI.Readability); err != nil {
		return err
	}
	if c.UI.NotFoundElement < 0 || c.UI.ForbiddenElement < 0 {
		return errors.New("ui.not_found_element and ui.forbidden_element must be the id of a page or 0")
	}
	if c.Mode != "single" && c.Mode != "cluster" {
		return fmt.Errorf("mode must be 'single' or 'cluster', got %q", c.Mode)
	}
	if len(c.I18n.Languages) == 0 {
		return errors.New("i18n.languages must not be empty")
	}
	seen := map[string]bool{}
	for _, l := range c.I18n.Languages {
		if !ValidLanguage(l) {
			return fmt.Errorf("invalid language code %q", l)
		}
		if seen[l] {
			return fmt.Errorf("duplicate language %q", l)
		}
		seen[l] = true
	}
	if e := c.UI.Editor; e != "" && e != "visual" && e != "simple" {
		return fmt.Errorf("ui.editor must be 'visual' or 'simple', got %q", e)
	}
	if p := c.UI.PreviewImage; p != "" && (strings.ContainsAny(p, " \t\r\n\"'<>\\") ||
		!(strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") || strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "http://"))) {
		return fmt.Errorf("ui.preview_image must be a path like /api/v1/files/… or an http(s) URL")
	}
	if !seen[c.I18n.DefaultLanguage] {
		return fmt.Errorf("i18n.default_language %q must be listed in i18n.languages", c.I18n.DefaultLanguage)
	}
	if c.Retention.RevisionsKeepMin < 1 {
		return errors.New("retention.revisions_keep_min must be >= 1")
	}
	if c.Auth.MinPasswordLen < 8 {
		return errors.New("auth.min_password_length must be >= 8")
	}
	switch c.Mail.Transport {
	case "", "auto", "smtp", "sendmail", "log", "none":
	default:
		return fmt.Errorf("mail.transport must be auto, smtp, sendmail, log or none, got %q", c.Mail.Transport)
	}
	switch c.Mail.SMTP.Security {
	case "", "starttls", "tls", "none":
	default:
		return fmt.Errorf("mail.smtp.security must be starttls, tls or none, got %q", c.Mail.SMTP.Security)
	}
	if c.Mail.DigestHour < 0 || c.Mail.DigestHour > 23 || c.Mail.DigestWeekday < 0 || c.Mail.DigestWeekday > 6 {
		return errors.New("mail.digest_hour must be 0–23 and mail.digest_weekday 0–6 (0 = Sunday)")
	}
	if c.Mail.DigestTimezone != "" {
		if _, err := time.LoadLocation(c.Mail.DigestTimezone); err != nil {
			return fmt.Errorf("mail.digest_timezone: unknown time zone %q", c.Mail.DigestTimezone)
		}
	}
	if c.Cache.TTLSeconds < 0 || c.Cache.MaxEntries < 0 || c.Cache.MaxMB < 0 || c.Cache.MaxMB > 16384 {
		return fmt.Errorf("cache: ttl_seconds, max_entries and max_mb must be 0 or positive (max_mb at most 16384)")
	}
	if c.Mail.Concurrency < 0 || c.Mail.Concurrency > 64 || c.Mail.RatePerMinute < 0 {
		return errors.New("mail.concurrency must be 0..64 and mail.rate_per_minute >= 0")
	}
	switch c.Mail.Bounce.Security {
	case "", "tls", "starttls", "none":
	default:
		return fmt.Errorf("mail.bounce.imap_security must be tls, starttls or none, got %q", c.Mail.Bounce.Security)
	}
	if a := c.Mail.Bounce.Address; a != "" && !HasEnvRef(a) {
		if x, err := mail.ParseAddress(a); err != nil || x.Name != "" {
			return fmt.Errorf("mail.bounce.address must be a plain address, got %q", a)
		}
	}
	if c.Mail.SMTP.Port < 0 || c.Mail.SMTP.Port > 65535 {
		return errors.New("mail.smtp.port is invalid")
	}
	if c.Mail.From != "" && !HasEnvRef(c.Mail.From) {
		if a, err := mail.ParseAddress(c.Mail.From); err != nil || a.Name != "" {
			return fmt.Errorf("mail.from must be a plain address like wiki@example.com, got %q", c.Mail.From)
		}
	}
	return nil
}

func clone(c *Config) (*Config, error) {
	b, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}
	out := &Config{}
	if err := yaml.Unmarshal(b, out); err != nil {
		return nil, err
	}
	return out, nil
}

// MaxEmbedOrigins bounds media.embed_allow (each entry goes into every
// Content-Security-Policy header).
const MaxEmbedOrigins = 30

// EmbedOrigin reports whether o is an origin media.embed_allow accepts:
// https://, a host name of lower-case letters, digits, dots and dashes
// with at least one dot, an optional port, nothing else. No wildcards, no
// IP addresses in brackets, no path: the value goes into the CSP header.
func EmbedOrigin(o string) bool {
	rest, ok := strings.CutPrefix(o, "https://")
	if !ok || rest == "" || len(rest) > 253 {
		return false
	}
	host, port, hasPort := strings.Cut(rest, ":")
	if hasPort {
		if port == "" || len(port) > 5 || port[0] == '0' {
			return false
		}
		for _, c := range port {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	if !strings.Contains(host, ".") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// ValidateMaintenance checks the maintenance mode.
func ValidateMaintenance(m Maintenance) error {
	if m.Mode != "" && m.Mode != MaintenanceReadOnly && m.Mode != MaintenanceClosed {
		return fmt.Errorf("maintenance.mode must be empty, read_only or closed, got %q", m.Mode)
	}
	if len(m.Message) > 50 {
		return errors.New("maintenance.message: at most 50 languages")
	}
	for l, t := range m.Message {
		if !ValidLanguage(l) {
			return fmt.Errorf("maintenance.message: invalid language code %q", l)
		}
		if !utf8.ValidString(t) || utf8.RuneCountInString(t) > MaintenanceMessageMax {
			return fmt.Errorf("maintenance.message: at most %d characters", MaintenanceMessageMax)
		}
	}
	if m.Until != "" {
		if _, err := time.Parse(time.RFC3339, m.Until); err != nil {
			return fmt.Errorf("maintenance.until must be a time like 2026-11-23T18:00:00+01:00, got %q", m.Until)
		}
	}
	return nil
}

func validTexts(name string, m map[string]string, max int) error {
	if len(m) > 50 {
		return fmt.Errorf("%s: at most 50 languages", name)
	}
	for l, t := range m {
		if !ValidLanguage(l) {
			return fmt.Errorf("%s: invalid language code %q", name, l)
		}
		if !utf8.ValidString(t) || utf8.RuneCountInString(t) > max {
			return fmt.Errorf("%s: at most %d characters", name, max)
		}
	}
	return nil
}

// ValidateConsent checks the asking for consent.
func ValidateConsent(c Consent) error {
	if c.Cookies.Version < 0 || c.Privacy.Version < 0 || c.Cookies.Page < 0 || c.Privacy.Page < 0 {
		return errors.New("consent: versions and pages must not be negative")
	}
	if c.Privacy.Enabled && c.Privacy.Page == 0 {
		return errors.New("consent.privacy: choose the page with the privacy policy")
	}
	switch c.Privacy.Guests {
	case ConsentGuestsNone, ConsentGuestsNotice, ConsentGuestsRequired:
	default:
		return fmt.Errorf("consent.privacy.guests must be empty, notice or required, got %q", c.Privacy.Guests)
	}
	if err := validTexts("consent.cookies.text", c.Cookies.Text, ConsentTextMax); err != nil {
		return err
	}
	return validTexts("consent.privacy.text", c.Privacy.Text, ConsentTextMax)
}
