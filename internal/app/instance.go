package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/db/migrations"
	"github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
)

// Instance holds the resources shared by all sites (instance settings,
// shared schema) and the sites themselves. Every site is a complete wiki
// with its own schema, settings overrides, services and plugin instances.
type Instance struct {
	// Config holds the instance settings; they are also the settings of
	// the site "main" and the defaults of all other sites.
	Config *config.Store
	// Shared is a pool for the shared schema (users, groups, roles, sites).
	Shared *pgxpool.Pool
	Log    *slog.Logger
	Root   string

	// Secrets seals and opens the secrets in the databases of all sites.
	Secrets *secrets.Box

	mu    sync.RWMutex
	sites map[string]*App
	// OnSite is called for every site opened after start (the server
	// attaches its handler and starts the site's jobs).
	OnSite func(*App)
}

// InstanceOptions configures OpenInstance.
type InstanceOptions struct {
	ConfigPath string
	Log        *slog.Logger
	Migrate    bool
	// Sites limits the opened sites (nil: all enabled sites).
	Sites []string
}

// SiteInfo describes a site.
type SiteInfo struct {
	Name      string   `json:"name"`
	Title     string   `json:"title"`
	Hostnames []string `json:"hostnames"`
	Disabled  bool     `json:"disabled"`
	// Accounts: shared (accounts of the instance) or separate (own users,
	// groups and roles of the site); fixed at creation.
	Accounts  string    `json:"accounts"`
	CreatedAt time.Time `json:"created_at"`
	// Running reports whether the site is open in this process.
	Running bool `json:"running"`
}

// OpenInstance loads the instance settings, connects to the database,
// applies the shared migrations and opens the sites.
func OpenInstance(ctx context.Context, o InstanceOptions) (*Instance, error) {
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	httpx.Logger = log
	root := filepath.Dir(o.ConfigPath)
	_ = config.LoadDotEnv(filepath.Join(root, ".env"))
	cfg, err := config.Open(o.ConfigPath)
	if err != nil {
		return nil, err
	}
	c := cfg.Get()
	box, err := openSecrets(c, root, log)
	if err != nil {
		return nil, err
	}
	shared, err := db.Open(ctx, c.Database.URL, 4)
	if err != nil {
		return nil, err
	}
	in := &Instance{Config: cfg, Shared: shared, Log: log, Root: root, Secrets: box, sites: map[string]*App{}}
	if o.Migrate {
		applied, err := db.Migrate(ctx, shared, []db.MigrationSource{{Name: "identity", Files: migrations.Identity}, {Name: "core", Files: migrations.Shared}})
		for _, m := range applied {
			log.Info("migration applied", "migration", "shared/"+m)
		}
		if err != nil {
			shared.Close()
			return nil, fmt.Errorf("migrate shared schema: %w", err)
		}
	}
	list, err := in.ListSites(ctx)
	if err != nil {
		shared.Close()
		return nil, err
	}
	for _, s := range list {
		if s.Disabled || (o.Sites != nil && !slices.Contains(o.Sites, s.Name)) {
			continue
		}
		a, err := newSite(ctx, in, s.Name, o.Migrate)
		if err != nil {
			in.Close()
			return nil, fmt.Errorf("site %s: %w", s.Name, err)
		}
		in.sites[s.Name] = a
	}
	if _, err := in.ListSites(ctx); err != nil { // host names of the open sites
		in.Close()
		return nil, err
	}
	return in, nil
}

// Close releases all sites and the shared pool.
func (in *Instance) Close() {
	in.mu.Lock()
	defer in.mu.Unlock()
	for _, a := range in.sites {
		a.close()
	}
	in.sites = map[string]*App{}
	in.Shared.Close()
}

// Site returns an open site (nil if unknown or not open).
func (in *Instance) Site(name string) *App {
	in.mu.RLock()
	defer in.mu.RUnlock()
	return in.sites[name]
}

// Sites returns the open sites ordered by name ("main" first).
func (in *Instance) Sites() []*App {
	in.mu.RLock()
	defer in.mu.RUnlock()
	out := make([]*App, 0, len(in.sites))
	for _, a := range in.sites {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Site == "main") != (out[j].Site == "main") {
			return out[i].Site == "main"
		}
		return out[i].Site < out[j].Site
	})
	return out
}

// normHost lowercases a host and removes the port.
func normHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.TrimSuffix(h, ".")
}

// ForHost returns the site serving a host name; unknown hosts are served by
// the site "main".
func (in *Instance) ForHost(ctx context.Context, host string) *App {
	h := normHost(host)
	in.mu.RLock()
	defer in.mu.RUnlock()
	for _, a := range in.sites {
		for _, n := range a.hostnames {
			if n == h {
				return a
			}
		}
	}
	return in.sites["main"]
}

// ListSites returns all sites from the database.
func (in *Instance) ListSites(ctx context.Context) ([]SiteInfo, error) {
	rows, err := in.Shared.Query(ctx, `SELECT name, title, hostnames, disabled, accounts, created_at FROM sites ORDER BY name <> 'main', name`)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SiteInfo, error) {
		var s SiteInfo
		err := r.Scan(&s.Name, &s.Title, &s.Hostnames, &s.Disabled, &s.Accounts, &s.CreatedAt)
		return s, err
	})
	if err != nil {
		return nil, err
	}
	in.mu.Lock()
	for i := range list {
		if a := in.sites[list[i].Name]; a != nil {
			list[i].Running = true
			a.hostnames = normHosts(list[i].Hostnames)
		}
	}
	in.mu.Unlock()
	return list, nil
}

// GetSite returns one site.
func (in *Instance) GetSite(ctx context.Context, name string) (SiteInfo, error) {
	list, err := in.ListSites(ctx)
	if err != nil {
		return SiteInfo{}, err
	}
	for _, s := range list {
		if s.Name == name {
			return s, nil
		}
	}
	return SiteInfo{}, apperr.NotFound("site " + name)
}

func normHosts(list []string) []string {
	out := []string{}
	for _, h := range list {
		if h = normHost(h); h != "" && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

func (in *Instance) checkHosts(ctx context.Context, name string, hosts []string) ([]string, error) {
	hosts = normHosts(hosts)
	for _, h := range hosts {
		if strings.ContainsAny(h, "/ :@") || len(h) > 253 {
			return nil, apperr.BadRequest("invalid host name %q", h)
		}
	}
	var other string
	err := in.Shared.QueryRow(ctx, `SELECT name FROM sites WHERE name <> $1 AND hostnames && $2 LIMIT 1`, name, hosts).Scan(&other)
	if err == nil {
		return nil, apperr.Conflict("a host name is already used by the site %s", other)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return hosts, nil
}

// SiteInput creates or changes a site.
type SiteInput struct {
	Name      string    `json:"name"`
	Title     *string   `json:"title"`
	Hostnames *[]string `json:"hostnames"`
	Disabled  *bool     `json:"disabled"`
	// Accounts (only when creating): shared or separate; default
	// sites.accounts of the instance settings.
	Accounts *string `json:"accounts"`
	// Admin is the first administrator of a site with separate accounts.
	Admin *SiteAdmin `json:"admin,omitempty"`
}

// SiteAdmin is the first local administrator of a site with own accounts.
type SiteAdmin struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// CreateSite adds a site: its schema with all tables and its settings file
// (base URL from the first host name). The site is opened right away.
func (in *Instance) CreateSite(ctx context.Context, s SiteInput) (SiteInfo, error) {
	if !db.ValidSiteName(s.Name) {
		return SiteInfo{}, apperr.BadRequest("invalid site name %q (lowercase letters, digits and _, starting with a letter, at most 31)", s.Name)
	}
	hosts := []string{}
	if s.Hostnames != nil {
		var err error
		if hosts, err = in.checkHosts(ctx, s.Name, *s.Hostnames); err != nil {
			return SiteInfo{}, err
		}
	}
	if len(hosts) == 0 {
		return SiteInfo{}, apperr.BadRequest("a further site needs at least one host name")
	}
	title := ""
	if s.Title != nil {
		title = strings.TrimSpace(*s.Title)
	}
	accounts := in.Config.Get().Sites.Accounts
	if s.Accounts != nil {
		accounts = *s.Accounts
	}
	if accounts == "" {
		accounts = "shared"
	}
	if accounts != "shared" && accounts != "separate" {
		return SiteInfo{}, apperr.BadRequest("accounts must be shared or separate")
	}
	if accounts == "separate" && (s.Admin == nil || strings.TrimSpace(s.Admin.Username) == "" || s.Admin.Password == "") {
		return SiteInfo{}, apperr.BadRequest("a site with separate accounts needs a first administrator (admin.username, admin.password)")
	}
	if _, err := in.Shared.Exec(ctx, `INSERT INTO sites(name, title, hostnames, accounts) VALUES ($1,$2,$3,$4)`, s.Name, title, hosts, accounts); err != nil {
		if db.IsUniqueViolation(err) {
			return SiteInfo{}, apperr.Conflict("the site %s already exists", s.Name)
		}
		return SiteInfo{}, err
	}
	st, err := config.OpenSite(in.Config, s.Name)
	if err == nil {
		err = st.Update(func(c *config.Config) error {
			scheme := "https"
			if strings.HasPrefix(in.Config.Get().Server.BaseURL, "http://") {
				scheme = "http"
			}
			c.Server.BaseURL = scheme + "://" + hosts[0]
			if title != "" {
				c.UI.SiteName = title
			}
			return nil
		})
	}
	if err != nil {
		_, _ = in.Shared.Exec(ctx, `DELETE FROM sites WHERE name=$1`, s.Name)
		return SiteInfo{}, err
	}
	if err := in.open(ctx, s.Name); err != nil {
		return SiteInfo{}, err
	}
	if accounts == "separate" {
		if err := in.createSiteAdmin(ctx, s.Name, *s.Admin); err != nil {
			_ = in.DeleteSite(ctx, s.Name) // no site without an administrator
			return SiteInfo{}, err
		}
	}
	return in.GetSite(ctx, s.Name)
}

// createSiteAdmin creates the first administrator of a site with separate
// accounts (password rules of the site apply).
func (in *Instance) createSiteAdmin(ctx context.Context, site string, adm SiteAdmin) error {
	a := in.Site(site)
	if a == nil {
		return fmt.Errorf("site %s is not open", site)
	}
	cctx := identity.WithSite(identity.With(ctx, identity.CLI()), site)
	rid, err := a.Auth.EnsureRole(cctx, "Administrators", "", true)
	if err != nil {
		return err
	}
	name := adm.DisplayName
	if name == "" {
		name = adm.Username
	}
	_, err = a.Auth.CreateLocalUserUnchecked(cctx, auth.NewUser{Username: strings.TrimSpace(adm.Username), Password: adm.Password,
		DisplayName: name, Email: strings.TrimSpace(adm.Email), Roles: []int64{rid}})
	return err
}

func (in *Instance) open(ctx context.Context, name string) error {
	if in.Site(name) != nil {
		return nil
	}
	a, err := newSite(ctx, in, name, true)
	if err != nil {
		return err
	}
	in.mu.Lock()
	in.sites[name] = a
	in.mu.Unlock()
	if _, err := in.ListSites(ctx); err != nil { // host names
		return err
	}
	if in.OnSite != nil {
		in.OnSite(a)
	}
	return nil
}

func (in *Instance) closeSite(name string) {
	in.mu.Lock()
	a := in.sites[name]
	delete(in.sites, name)
	in.mu.Unlock()
	if a != nil {
		if a.stop != nil {
			a.stop()
		}
		a.close()
	}
}

// UpdateSite changes title, host names or the disabled flag. A disabled
// site is closed; its data stays.
func (in *Instance) UpdateSite(ctx context.Context, s SiteInput) (SiteInfo, error) {
	cur, err := in.GetSite(ctx, s.Name)
	if err != nil {
		return SiteInfo{}, err
	}
	if s.Title != nil {
		cur.Title = strings.TrimSpace(*s.Title)
	}
	if s.Hostnames != nil {
		if cur.Hostnames, err = in.checkHosts(ctx, s.Name, *s.Hostnames); err != nil {
			return SiteInfo{}, err
		}
		if s.Name != "main" && len(cur.Hostnames) == 0 {
			return SiteInfo{}, apperr.BadRequest("a further site needs at least one host name")
		}
	}
	if s.Accounts != nil && *s.Accounts != cur.Accounts {
		return SiteInfo{}, apperr.BadRequest("the accounts of a site (shared or separate) cannot be changed after it was created")
	}
	if s.Disabled != nil {
		if *s.Disabled && s.Name == "main" {
			return SiteInfo{}, apperr.BadRequest("the site main cannot be disabled")
		}
		cur.Disabled = *s.Disabled
	}
	if _, err := in.Shared.Exec(ctx, `UPDATE sites SET title=$2, hostnames=$3, disabled=$4 WHERE name=$1`, s.Name, cur.Title, cur.Hostnames, cur.Disabled); err != nil {
		return SiteInfo{}, err
	}
	if cur.Disabled {
		in.closeSite(s.Name)
	} else if err := in.open(ctx, s.Name); err != nil {
		return SiteInfo{}, err
	}
	return in.GetSite(ctx, s.Name)
}

// DeleteSite removes a site with all its data (schema and settings file;
// files in the storage directory are left for the administrator).
func (in *Instance) DeleteSite(ctx context.Context, name string) error {
	if name == "main" {
		return apperr.BadRequest("the site main cannot be deleted")
	}
	if _, err := in.GetSite(ctx, name); err != nil {
		return err
	}
	in.closeSite(name)
	if err := db.DropSite(ctx, in.Shared, name); err != nil {
		return err
	}
	if _, err := in.Shared.Exec(ctx, `DELETE FROM sites WHERE name=$1`, name); err != nil {
		return err
	}
	return config.RemoveSite(in.Config, name)
}

// Sync opens sites that were created or enabled elsewhere (CLI, other
// nodes) and closes deleted or disabled ones.
func (in *Instance) Sync(ctx context.Context) error {
	list, err := in.ListSites(ctx)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, s := range list {
		if !s.Disabled {
			want[s.Name] = true
			if !s.Running {
				if err := in.open(ctx, s.Name); err != nil {
					in.Log.Error("open site", "site", s.Name, "err", err)
				} else {
					in.Log.Info("site opened", "site", s.Name)
				}
			}
		}
	}
	for _, a := range in.Sites() {
		if !want[a.Site] {
			in.closeSite(a.Site)
			in.Log.Info("site closed", "site", a.Site)
		}
	}
	return nil
}

// RunSync calls Sync periodically until ctx is done.
func (in *Instance) RunSync(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := in.Sync(ctx); err != nil && ctx.Err() == nil {
				in.Log.Error("site sync", "err", err)
			}
		}
	}
}

// AllowedHost reports whether a certificate may be requested for host: a
// host name of an enabled site or the host of a site's base URL. Used by the
// bundled reverse proxy (on-demand TLS) so it only fetches certificates for
// the wiki's own addresses.
func (in *Instance) AllowedHost(ctx context.Context, host string) (bool, error) {
	h := normHost(host)
	if h == "" {
		return false, nil
	}
	if slices.Contains(config.ExtraTLSHosts(), h) {
		return true, nil // bundled office servers
	}
	list, err := in.ListSites(ctx)
	if err != nil {
		return false, err
	}
	for _, s := range list {
		if s.Disabled {
			continue
		}
		if slices.Contains(normHosts(s.Hostnames), h) {
			return true, nil
		}
	}
	for _, a := range in.Sites() {
		if u, err := url.Parse(a.Config.Get().Server.BaseURL); err == nil && normHost(u.Host) == h {
			return true, nil
		}
	}
	return false, nil
}

// openSecrets loads the key file of the secrets (created at the first
// start of a mode that needs one).
func openSecrets(c *config.Config, root string, log *slog.Logger) (*secrets.Box, error) {
	path := SecretsKeyFile(c, root)
	envName := c.Secrets.PasswordEnv
	if envName == "" {
		envName = "EWIKI_SECRETS_PASSWORD"
	}
	pw := os.Getenv(envName)
	if c.Secrets.Mode == secrets.ModeAES || c.Secrets.Mode == secrets.ModePQ {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			log.Warn("creating the key file of the secrets; keep a copy of it apart from the database backups", "file", path, "password", pw != "")
		}
	}
	box, err := secrets.Load(c.Secrets.Mode, path, pw, true)
	if err != nil {
		return nil, fmt.Errorf("secrets: %w", err)
	}
	return box, nil
}

// SecretsKeyFile returns the key file of the secrets (default: secrets.key
// next to config.yaml).
func SecretsKeyFile(c *config.Config, root string) string {
	if c.Secrets.KeyFile != "" {
		if filepath.IsAbs(c.Secrets.KeyFile) {
			return c.Secrets.KeyFile
		}
		return filepath.Join(root, c.Secrets.KeyFile)
	}
	return filepath.Join(root, "secrets.key")
}
