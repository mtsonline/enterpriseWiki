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
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the complete configuration document.
type Config struct {
	Server    Server                    `yaml:"server" json:"server"`
	Mode      string                    `yaml:"mode" json:"mode"` // single | cluster
	Database  Database                  `yaml:"database" json:"database"`
	Storage   Storage                   `yaml:"storage" json:"storage"`
	I18n      I18n                      `yaml:"i18n" json:"i18n"`
	UI        UI                        `yaml:"ui" json:"ui"`
	Auth      Auth                      `yaml:"auth" json:"auth"`
	Retention Retention                 `yaml:"retention" json:"retention"`
	Plugins   Plugins                   `yaml:"plugins" json:"plugins"`
	Mail      Mail                      `yaml:"mail" json:"mail"`
	Cache     Cache                     `yaml:"cache" json:"cache"`
	Webhooks  Webhooks                  `yaml:"webhooks" json:"webhooks"`
	Audit     Audit                     `yaml:"audit" json:"audit"`
	License   License                   `yaml:"license" json:"license"`
	Settings  map[string]map[string]any `yaml:"plugin_settings,omitempty" json:"plugin_settings,omitempty"`
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

type Webhooks struct {
	// AllowPrivate allows webhook URLs on internal addresses (10.x, 192.168.x,
	// localhost …), e.g. an on-premises chat server. Default false.
	AllowPrivate bool `yaml:"allow_private,omitempty" json:"allow_private"`
	// KeepDays is how long sent and failed deliveries are kept (default 30).
	KeepDays int `yaml:"keep_days,omitempty" json:"keep_days"`
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
	MaxBodyMB      int      `yaml:"max_body_mb" json:"max_body_mb"`
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
}

// Auth configures authentication.
type Auth struct {
	// Order of password based providers. "local" is always tried first,
	// regardless of its position, so local (break-glass) accounts keep working
	// when an external identity provider is down.
	Order           []string `yaml:"order" json:"order"`
	SessionTTL      string   `yaml:"session_ttl" json:"session_ttl"`
	APIKeyMaxTTL    string   `yaml:"api_key_max_ttl" json:"api_key_max_ttl"`
	SSHTokenTTL     string   `yaml:"ssh_token_ttl" json:"ssh_token_ttl"`
	MaxLoginFails   int      `yaml:"max_login_failures" json:"max_login_failures"`
	LockoutDuration string   `yaml:"lockout_duration" json:"lockout_duration"`
	MinPasswordLen  int      `yaml:"min_password_length" json:"min_password_length"`
}

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
	Disabled []string       `yaml:"disabled" json:"disabled"`
	Dir      string         `yaml:"dir" json:"dir"`
	Catalog  []CatalogEntry `yaml:"catalog" json:"catalog"`
	Macros   MacroSecurity  `yaml:"macros" json:"macros"`
	// Updates configures the update check (GitHub releases).
	Updates Updates `yaml:"updates" json:"updates"`
	// AutoRestartOnDrop restarts automatically after plugins copied into the
	// plugin folder were built (default: the administration offers "Restart now").
	AutoRestartOnDrop bool `yaml:"auto_restart_on_drop" json:"auto_restart_on_drop"`
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
}

// Defaults returns a configuration with sane defaults.
func Defaults() *Config {
	return &Config{
		Server:   Server{Listen: ":8080", BaseURL: "http://localhost:8080", MaxBodyMB: 64},
		Mode:     "single",
		Database: Database{URL: "${EWIKI_DATABASE_URL}", MaxConns: 50},
		Storage:  Storage{Path: "./data", MaxUploadMB: 100},
		I18n:     I18n{DefaultLanguage: "en", Languages: []string{"en", "de"}},
		UI:       UI{SiteName: "Enterprise Wiki", Template: "theme.wiki", Prerender: true, ReadableURLs: true},
		Auth: Auth{
			Order: []string{"local"}, SessionTTL: "168h", APIKeyMaxTTL: "8760h", SSHTokenTTL: "1h",
			MaxLoginFails: 10, LockoutDuration: "15m", MinPasswordLen: 12,
		},
		Retention: Retention{RevisionsKeepDays: 0, RevisionsKeepMin: 50, TrashDays: 30, AuditDays: 0, MailDays: 90},
		Mail:      Mail{Transport: "auto", FromName: "Enterprise Wiki", SMTP: SMTP{Security: "starttls"}, DigestHour: 7, DigestWeekday: 1},
		Plugins:   Plugins{Dir: "./plugins"},
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
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
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
			os.Setenv(k, v)
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
	b, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(b, raw); err != nil {
			return fmt.Errorf("config %s: %w", s.path, err)
		}
		migrateLegacy(raw)
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
func (s *Store) Update(fn func(c *Config) error) error {
	c := s.Raw()
	if err := fn(c); err != nil {
		return err
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
# Reference: docs/admin/configuration.md
`

// WriteFile writes c to path atomically.
func WriteFile(path string, c *Config) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(header + string(b)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if st, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmp.Name(), st.Mode().Perm())
	} else {
		_ = os.Chmod(tmp.Name(), 0o640)
	}
	return os.Rename(tmp.Name(), path)
}

var langRe = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)

// ValidLanguage reports whether code is an accepted language tag (en, de, de-AT).
func ValidLanguage(code string) bool { return langRe.MatchString(code) }

// Validate checks a configuration document.
func Validate(c *Config) error {
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

// renamedPlugins maps former plugin ids to their current ids.
var renamedPlugins = map[string]string{"theme.confluence": "theme.wiki", "theme.confluence.dark": "theme.wiki.dark"}

// migrateLegacy updates settings written by older versions (renamed plugins).
// The file is rewritten with the new ids on the next save.
func migrateLegacy(c *Config) {
	if n, ok := renamedPlugins[c.UI.Template]; ok {
		c.UI.Template = n
	}
	for i, id := range c.Plugins.Disabled {
		if n, ok := renamedPlugins[id]; ok {
			c.Plugins.Disabled[i] = n
		}
	}
	for old, n := range renamedPlugins {
		if v, ok := c.Settings[old]; ok {
			if _, exists := c.Settings[n]; !exists {
				c.Settings[n] = v
			}
			delete(c.Settings, old)
		}
	}
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
