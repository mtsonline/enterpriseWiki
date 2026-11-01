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
	Settings  map[string]map[string]any `yaml:"plugin_settings,omitempty" json:"plugin_settings,omitempty"`
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
	Path string `yaml:"path" json:"path"`
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
}

// Plugins configures the plugin system.
type Plugins struct {
	Disabled []string       `yaml:"disabled" json:"disabled"`
	Dir      string         `yaml:"dir" json:"dir"`
	Catalog  []CatalogEntry `yaml:"catalog" json:"catalog"`
	Macros   MacroSecurity  `yaml:"macros" json:"macros"`
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
type MacroSecurity struct {
	ShellEnabled bool     `yaml:"shell_enabled" json:"shell_enabled"`
	HTTPAllow    []string `yaml:"http_allow" json:"http_allow"`
}

// Defaults returns a configuration with sane defaults.
func Defaults() *Config {
	return &Config{
		Server:   Server{Listen: ":8080", BaseURL: "http://localhost:8080", MaxBodyMB: 64},
		Mode:     "single",
		Database: Database{URL: "${EWIKI_DATABASE_URL}", MaxConns: 50},
		Storage:  Storage{Path: "./data"},
		I18n:     I18n{DefaultLanguage: "en", Languages: []string{"en", "de"}},
		UI:       UI{SiteName: "Enterprise Wiki", Template: "theme.confluence"},
		Auth: Auth{
			Order: []string{"local"}, SessionTTL: "168h", APIKeyMaxTTL: "8760h", SSHTokenTTL: "1h",
			MaxLoginFails: 10, LockoutDuration: "15m", MinPasswordLen: 12,
		},
		Retention: Retention{RevisionsKeepDays: 0, RevisionsKeepMin: 50, TrashDays: 30, AuditDays: 0},
		Plugins:   Plugins{Dir: "./plugins"},
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
	raw := Defaults()
	var mt time.Time
	b, err := os.ReadFile(s.path)
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
	if err := WriteFile(s.path, c); err != nil {
		return err
	}
	return s.Reload()
}

// Changed reports whether the file on disk is newer than the loaded one.
func (s *Store) Changed() bool {
	st, err := os.Stat(s.path)
	if err != nil {
		return false
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
	if !seen[c.I18n.DefaultLanguage] {
		return fmt.Errorf("i18n.default_language %q must be listed in i18n.languages", c.I18n.DefaultLanguage)
	}
	if c.Retention.RevisionsKeepMin < 1 {
		return errors.New("retention.revisions_keep_min must be >= 1")
	}
	if c.Auth.MinPasswordLen < 8 {
		return errors.New("auth.min_password_length must be >= 8")
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
