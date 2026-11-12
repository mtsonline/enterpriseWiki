// Package app wires configuration, database, services and plugins together.
package app

import (
	"context"
	"fmt"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/collab"
	"github.com/mtsonline/enterprisewiki/internal/feedfetch"
	"github.com/mtsonline/enterprisewiki/internal/notify"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/db/migrations"
	"github.com/mtsonline/enterprisewiki/internal/events"
	"github.com/mtsonline/enterprisewiki/internal/i18n"
	"github.com/mtsonline/enterprisewiki/internal/kernel"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"github.com/mtsonline/enterprisewiki/internal/mail"
	"github.com/mtsonline/enterprisewiki/internal/media"
	"github.com/mtsonline/enterprisewiki/internal/pagecache"
	"github.com/mtsonline/enterprisewiki/internal/pgversion"
	"github.com/mtsonline/enterprisewiki/internal/storage"
	"github.com/mtsonline/enterprisewiki/internal/watch"
	"github.com/mtsonline/enterprisewiki/internal/webhook"
	"github.com/mtsonline/enterprisewiki/internal/worker"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

// App holds all services.
type App struct {
	Config  *config.Store
	DB      *pgxpool.Pool
	Bus     *events.Bus
	Auth    *auth.Service
	ACL     *acl.Service
	Content *content.Service
	I18n    *i18n.Service
	Files   *storage.Store
	Media   *media.Service
	Mail    *mail.Service
	Feeds   *feedfetch.Cache
	Watch   *watch.Service
	// Webhooks sends HTTP notifications about events.
	// PGVersion checks the PostgreSQL version (log, e-mails).
	PGVersion *pgversion.Notifier
	Webhooks  *webhook.Service
	// Notify stores notifications (bell) and e-mails them.
	Notify *notify.Service
	// AuditForward sends the audit log to other systems.
	AuditForward *audit.Forwarder
	Cache        *pagecache.Cache
	Workers      *worker.Registry
	Kernel       *kernel.Kernel
	Log          *slog.Logger
	// Mux receives core and plugin routes.
	Mux *http.ServeMux
	// Root is the source directory (for plugin installation / rebuilds).
	Root string
	// Site is the name of this site ("main" for the first one); Instance
	// holds the shared resources and the other sites.
	Site     string
	Instance *Instance
	// SeparateAccounts: the site has its own users, groups and roles in its
	// schema instead of the shared accounts of the instance.
	SeparateAccounts bool
	owner            bool // Close also closes the instance (opened by New)
	// hostnames served by this site (normalized)
	hostnames []string
	// stop cancels the site's background work (set by the server)
	stop func()
}

// SetStop registers the function that stops the site's background work
// (called when the site is disabled or deleted).
func (a *App) SetStop(f func()) { a.stop = f }

// Options configures New.
type Options struct {
	ConfigPath string
	Log        *slog.Logger
	// Migrate applies pending migrations on start.
	Migrate bool
	// Site selects the site (default "main").
	Site string
}

// New loads the configuration, connects to the database and initializes all
// services and plugins of one site (default "main").
func New(ctx context.Context, o Options) (*App, error) {
	site := o.Site
	if site == "" {
		site = "main"
	}
	in, err := OpenInstance(ctx, InstanceOptions{ConfigPath: o.ConfigPath, Log: o.Log, Migrate: o.Migrate, Sites: []string{site}})
	if err != nil {
		return nil, err
	}
	a := in.Site(site)
	if a == nil {
		in.Close()
		return nil, fmt.Errorf("site %q not found or disabled", site)
	}
	a.owner = true
	return a, nil
}

// newSite initializes the services and plugins of a site.
func newSite(ctx context.Context, in *Instance, site string, migrate bool) (*App, error) {
	log := in.Log.With("site", site)
	if site == "main" {
		log = in.Log
	}
	cfg := in.Config
	if site != "main" {
		var err error
		if cfg, err = config.OpenSite(in.Config, site); err != nil {
			return nil, err
		}
	}
	c := cfg.Get()
	pool, err := db.OpenSite(ctx, c.Database.URL, c.Database.MaxConns, site)
	if err != nil {
		return nil, err
	}
	a := &App{Config: cfg, DB: pool, Log: log, Mux: http.NewServeMux(), Root: in.Root, Site: site, Instance: in}
	if site != "main" {
		if err := in.Shared.QueryRow(ctx, `SELECT accounts = 'separate' FROM sites WHERE name=$1`, site).Scan(&a.SeparateAccounts); err != nil {
			pool.Close()
			return nil, fmt.Errorf("site %s: %w", site, err)
		}
	}
	a.Kernel = kernel.NewSite(cfg, log, site)
	if migrate {
		if err := a.MigrateDB(ctx); err != nil {
			pool.Close()
			return nil, err
		}
	}
	// the accounts must be where the site expects them; otherwise queries
	// would silently use the accounts of another schema
	if err := a.checkAccounts(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	auth.KeyRateMax = func() (int, int) {
		d, _ := limits.Get(limits.RateUser)
		return int(limits.Value(in.Config.Get(), limits.RateUser)), int(d.Max)
	}
	audit.SnapshotSearchDays = func() int { return int(limits.Value(in.Config.Get(), limits.AuditSearchDays)) }
	collab.Limits = func() (int, int) {
		c := in.Config.Get()
		return int(limits.Value(c, limits.LivePersons)), int(limits.Value(c, limits.LivePerPerson))
	}
	audit.ValueLimit = func(guest bool) int {
		if guest {
			return int(limits.Value(in.Config.Get(), limits.AuditValueGuest))
		}
		return int(limits.Value(in.Config.Get(), limits.AuditValue))
	}
	a.Bus = events.NewSite(pool, c.Mode == "cluster", log, site)
	a.ACL = acl.New(pool, a.Bus, log)
	a.Auth = auth.New(pool, cfg, a.Bus, log)
	a.Auth.SeparateAccounts = a.SeparateAccounts
	a.Content = content.New(pool, a.ACL, a.Bus, cfg, log)
	a.I18n = i18n.New(pool, cfg, a.Bus)
	a.Files = storage.New(pool, cfg)
	a.Files.AddReference("users", "profile") // profile photos
	a.Mail = mail.New(pool, cfg, log)
	a.Feeds = feedfetch.NewCache(pool, log, func() bool { return cfg.Get().Feeds.AllowPrivate })
	a.Workers = worker.New(pool, log, func() bool { return cfg.Get().Mode == "cluster" })
	a.Workers.Site = site
	a.Workers.Register(worker.Job{Name: "mail", Interval: 10 * time.Second, Run: func(ctx context.Context) error {
		n, err := a.Mail.Process(ctx)
		if n > 0 {
			log.Info("e-mails delivered", "recipients", n)
		}
		return err
	}})
	a.Mail.Kick = func() { time.AfterFunc(300*time.Millisecond, func() { a.Workers.Trigger("mail") }) } // after the commit
	a.Workers.Register(worker.Job{Name: "mail.bounces", Interval: 5 * time.Minute, Run: func(ctx context.Context) error {
		n, err := a.Mail.ProcessBounces(ctx)
		if n > 0 {
			log.Info("bounces processed", "failed_recipients", n)
		}
		return err
	}})
	a.Workers.Register(worker.Job{Name: "feeds", Interval: 10 * time.Minute, Run: func(ctx context.Context) error {
		return a.Feeds.RefreshAll(ctx)
	}})
	a.Workers.Register(worker.Job{Name: "search.changes.cleanup", Interval: 24 * time.Hour, Run: func(ctx context.Context) error {
		// the change log only serves external search engines; they rebuild
		// their index when they missed changes
		_, err := pool.Exec(ctx, `DELETE FROM search_changes WHERE at < now() - interval '30 days'`)
		return err
	}})
	a.Workers.Register(worker.Job{Name: "mail.cleanup", Interval: 6 * time.Hour, Run: func(ctx context.Context) error {
		_, err := a.Mail.Cleanup(ctx, cfg.Get().Retention.MailDays)
		return err
	}})
	a.Mail.T = a.I18n.T
	// confirmation links of e-mail addresses (sent directly, the link is secret)
	a.Auth.SendConfirmation = func(ctx context.Context, uid int64, addr, link string) error {
		var lang, name string
		if err := pool.QueryRow(ctx, `SELECT language, coalesce(nullif(display_name,''), username) FROM users WHERE id=$1`, uid).Scan(&lang, &name); err != nil {
			return err
		}
		if lang == "" {
			lang = cfg.Get().I18n.DefaultLanguage
		}
		p := map[string]string{"link": link, "site": cfg.Get().UI.SiteName, "name": name, "email": addr, "hours": "48"}
		return a.Mail.SendNow(ctx, mail.Message{To: []string{addr}, Subject: a.I18n.T(ctx, lang, "email.confirm_subject", p),
			Text: a.I18n.T(ctx, lang, "email.confirm_text", p), Source: "auth", NoReport: true})
	}
	a.Auth.MailReady = func() bool { return a.Mail.Enabled() && a.Mail.Transport() != nil }
	a.Workers.Register(worker.Job{Name: "auth.emails", Interval: 24 * time.Hour, Run: func(ctx context.Context) error {
		if err := a.Auth.BackfillEmails(ctx); err != nil {
			return err
		}
		_, err := pool.Exec(ctx, `DELETE FROM email_tokens WHERE expires_at < now() - interval '1 day';
			DELETE FROM identity_links WHERE expires_at < now()`)
		return err
	}})
	a.Watch = watch.New(pool, cfg, a.ACL, a.Auth, a.Content, a.Mail, a.I18n, log)
	// guest cache: every change invalidates it (here, on other nodes and from CLI processes)
	a.Cache = pagecache.New(pool, cfg, log)
	a.Content.OnChange(func(ctx context.Context, event, etype string, id int64) { a.Cache.Invalidate(ctx, etype+"."+event) })
	a.Media = media.New(pool, a.ACL, a.Files, log)
	a.Media.UserName = func(ctx context.Context, id int64) string {
		var n string
		_ = pool.QueryRow(ctx, `SELECT coalesce(nullif(display_name, ''), username) FROM users WHERE id=$1`, id).Scan(&n)
		return n
	}
	a.Content.OnChange(func(ctx context.Context, event, etype string, id int64) {
		if etype == "element" && event != "delete" {
			if err := a.Media.RefreshUsage(ctx, id); err != nil {
				log.Warn("media usage", "element", id, "err", err)
			}
		}
	})
	for _, topic := range []string{"auth", "acl", "i18n"} {
		a.Bus.Subscribe(topic, func(string) { a.Cache.Invalidate(context.Background(), topic) })
	}
	a.Workers.Register(worker.Job{Name: "watch", Interval: 30 * time.Second, Run: func(ctx context.Context) error {
		n, err := a.Watch.Send(ctx)
		if n > 0 {
			log.Info("watch notifications queued", "users", n)
			a.Mail.Kick()
		}
		return err
	}})
	// PostgreSQL version: log at start, e-mails to administrators (recommended version, end of support)
	a.PGVersion = &pgversion.Notifier{Pool: pool, Cfg: cfg, Mail: a.Mail, Log: log, T: a.I18n.T}
	a.Workers.Register(worker.Job{Name: "pgversion", Interval: 24 * time.Hour, Run: a.PGVersion.Run})
	// webhooks: content changes here, other events from plugins (Host.Webhooks)
	a.Webhooks = webhook.New(pool, cfg, log)
	a.Webhooks.T = a.I18n.T
	a.Webhooks.Kick = func() { time.AfterFunc(300*time.Millisecond, func() { a.Workers.Trigger("webhooks") }) }
	a.Content.OnChange(func(ctx context.Context, event, etype string, id int64) { a.emitContent(ctx, event, etype, id) })
	a.Workers.Register(worker.Job{Name: "webhooks", Interval: 30 * time.Second, Run: func(ctx context.Context) error {
		_, err := a.Webhooks.Deliver(ctx)
		return err
	}})
	a.Workers.Register(worker.Job{Name: "webhooks.cleanup", Interval: 24 * time.Hour, Run: func(ctx context.Context) error {
		return a.Webhooks.Cleanup(ctx, cfg.Get().Webhooks.KeepDays)
	}})
	// audit log: areas switched off in the settings, forwarding to other systems
	audit.SetSiteFilter(site, func(action string) bool {
		c := cfg.Get().Audit
		switch {
		case strings.HasPrefix(action, "auth.login") || action == "auth.logout":
			return c.Logins()
		case action == "element.view":
			return c.LogViews
		}
		return !slices.Contains(c.Disabled, audit.Area(action))
	})
	a.AuditForward = &audit.Forwarder{Pool: pool, Cfg: cfg, DataDir: filepath.Dir(cfg.Path()), Site: func() string { return cfg.Get().UI.SiteName }}
	a.Workers.Register(worker.Job{Name: "audit.forward", Interval: 15 * time.Second, Active: func() bool { return len(cfg.Get().Audit.Forward) > 0 }, Run: a.AuditForward.Run})
	a.Notify = notify.New(pool, cfg, a.Mail)
	a.Workers.Register(worker.Job{Name: "views.flush", Interval: time.Minute, Run: a.Content.FlushViews})
	a.Workers.Register(worker.Job{Name: "notifications.cleanup", Interval: 24 * time.Hour, Run: func(ctx context.Context) error { return a.Notify.Cleanup(ctx, 90) }})
	// scheduled publication of drafts (with the permissions of who scheduled it)
	a.Workers.Register(worker.Job{Name: "drafts.publish", Interval: time.Minute, Run: func(ctx context.Context) error {
		_, err := a.Content.PublishDue(ctx, a.Auth.LoadPrincipal)
		return err
	}})
	a.Auth.SetEnabledFunc(a.Kernel.Enabled)
	a.Content.SetEnabledFunc(a.Kernel.Enabled)
	a.Content.SetTemplateFunc(a.Kernel.IsTemplate)
	a.Media.Enabled = a.Kernel.Enabled
	a.Media.LoadPrincipal = a.Auth.LoadPrincipal
	if err := a.I18n.Register("core", i18n.CoreLocales()); err != nil {
		a.close()
		return nil, err
	}
	base := sdk.Host{Notify: a.Notify, Webhooks: a.Webhooks, Config: cfg, DB: pool, ACL: a.ACL, Content: a.Content, Auth: a.Auth, I18n: a.I18n, Files: a.Files, Media: a.Media, Mail: a.Mail, Feeds: a.Feeds, Workers: a.Workers,
		Log: log, Mux: a.Mux, IsEnabled: a.Kernel.Enabled}
	if err := a.Kernel.Setup(base, a.I18n); err != nil {
		a.close()
		return nil, err
	}
	return a, nil
}

// MigrateDB applies core and plugin migrations.
func (a *App) MigrateDB(ctx context.Context) error {
	sources := []db.MigrationSource{{Name: "core", Files: migrations.FS}}
	if a.SeparateAccounts { // own accounts first: the site tables refer to them
		sources = append([]db.MigrationSource{{Name: "identity", Files: migrations.Identity}}, sources...)
	}
	sources = append(sources, a.Kernel.Migrations()...)
	applied, err := db.Migrate(ctx, a.DB, sources)
	for _, m := range applied {
		a.Log.Info("migration applied", "migration", m)
	}
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// checkAccounts verifies that the account tables are in the site schema for
// separate accounts and only in the shared schema otherwise.
func (a *App) checkAccounts(ctx context.Context) error {
	var own bool
	if err := a.DB.QueryRow(ctx, `SELECT to_regclass(quote_ident(current_schema()) || '.users') IS NOT NULL`).Scan(&own); err != nil {
		return err
	}
	if own != a.SeparateAccounts {
		return fmt.Errorf("site %s: accounts are configured as separate=%v, but the schema says %v (refusing to start)", a.Site, a.SeparateAccounts, own)
	}
	return nil
}

// Close releases resources (with New: also the instance).
func (a *App) Close() {
	if a.owner {
		a.Instance.Close()
		return
	}
	a.close()
}

func (a *App) close() {
	if a.Bus != nil {
		a.Bus.Close()
	}
	a.DB.Close()
}

// emitContent turns a content change into a webhook event.
func (a *App) emitContent(ctx context.Context, event, etype string, id int64) {
	ev := webhook.Event{Name: etype + "." + event, Object: webhook.Object{Type: etype, ID: id}}
	def := a.Config.Get().I18n.DefaultLanguage
	var titles map[string]string
	switch etype {
	case "element":
		var short string
		if err := a.DB.QueryRow(context.WithoutCancel(ctx), `SELECT titles, category_id, COALESCE(short_url, '') FROM elements WHERE id=$1`, id).Scan(&titles, &ev.CategoryID, &short); err != nil {
			return
		}
		ev.Object.URL = fmt.Sprintf("/e/%d", id)
		if short != "" {
			ev.Object.URL = "/" + short
		}
	case "category":
		if err := a.DB.QueryRow(context.WithoutCancel(ctx), `SELECT titles FROM categories WHERE id=$1`, id).Scan(&titles); err != nil {
			return
		}
		ev.CategoryID = id
		ev.Object.URL = fmt.Sprintf("/c/%d", id)
	default:
		return
	}
	ev.Object.Title = titles[def]
	if ev.Object.Title == "" {
		for _, t := range titles {
			ev.Object.Title = t
			break
		}
	}
	if event == "delete" {
		ev.Object.URL = ""
	}
	a.Webhooks.Emit(ctx, ev)
}
