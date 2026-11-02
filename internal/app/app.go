// Package app wires configuration, database, services and plugins together.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/db/migrations"
	"github.com/mtsonline/enterprisewiki/internal/events"
	"github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/i18n"
	"github.com/mtsonline/enterprisewiki/internal/kernel"
	"github.com/mtsonline/enterprisewiki/internal/mail"
	"github.com/mtsonline/enterprisewiki/internal/storage"
	"github.com/mtsonline/enterprisewiki/internal/watch"
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
	Mail    *mail.Service
	Watch   *watch.Service
	Workers *worker.Registry
	Kernel  *kernel.Kernel
	Log     *slog.Logger
	// Mux receives core and plugin routes.
	Mux *http.ServeMux
	// Root is the source directory (for plugin installation / rebuilds).
	Root string
}

// Options configures New.
type Options struct {
	ConfigPath string
	Log        *slog.Logger
	// Migrate applies pending migrations on start.
	Migrate bool
}

// New loads the configuration, connects to the database and initializes all
// services and plugins.
func New(ctx context.Context, o Options) (*App, error) {
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
	pool, err := db.Open(ctx, c.Database.URL, c.Database.MaxConns)
	if err != nil {
		return nil, err
	}
	a := &App{Config: cfg, DB: pool, Log: log, Mux: http.NewServeMux(), Root: root}
	a.Kernel = kernel.New(cfg, log)
	if o.Migrate {
		if err := a.MigrateDB(ctx); err != nil {
			pool.Close()
			return nil, err
		}
	}
	a.Bus = events.New(pool, c.Mode == "cluster", log)
	a.ACL = acl.New(pool, a.Bus, log)
	a.Auth = auth.New(pool, cfg, a.Bus, log)
	a.Content = content.New(pool, a.ACL, a.Bus, cfg, log)
	a.I18n = i18n.New(pool, cfg, a.Bus)
	a.Files = storage.New(pool, cfg)
	a.Files.AddReference("users", "profile") // profile photos
	a.Mail = mail.New(pool, cfg, log)
	a.Workers = worker.New(pool, log, func() bool { return cfg.Get().Mode == "cluster" })
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
	a.Watch = watch.New(pool, cfg, a.ACL, a.Auth, a.Content, a.Mail, a.I18n, log)
	a.Workers.Register(worker.Job{Name: "watch", Interval: 30 * time.Second, Run: func(ctx context.Context) error {
		n, err := a.Watch.Send(ctx)
		if n > 0 {
			log.Info("watch notifications queued", "users", n)
			a.Mail.Kick()
		}
		return err
	}})
	a.Auth.SetEnabledFunc(a.Kernel.Enabled)
	a.Content.SetEnabledFunc(a.Kernel.Enabled)
	if err := a.I18n.Register("core", i18n.CoreLocales()); err != nil {
		pool.Close()
		return nil, err
	}
	base := sdk.Host{Config: cfg, DB: pool, ACL: a.ACL, Content: a.Content, Auth: a.Auth, I18n: a.I18n, Files: a.Files, Mail: a.Mail, Workers: a.Workers,
		Log: log, Mux: a.Mux, IsEnabled: a.Kernel.Enabled}
	if err := a.Kernel.Setup(base, a.I18n); err != nil {
		pool.Close()
		return nil, err
	}
	return a, nil
}

// MigrateDB applies core and plugin migrations.
func (a *App) MigrateDB(ctx context.Context) error {
	sources := append([]db.MigrationSource{{Name: "core", Files: migrations.FS}}, a.Kernel.Migrations()...)
	applied, err := db.Migrate(ctx, a.DB, sources)
	for _, m := range applied {
		a.Log.Info("migration applied", "migration", m)
	}
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// Close releases resources.
func (a *App) Close() { a.DB.Close() }
