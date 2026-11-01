// Package app wires configuration, database, services and plugins together.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

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
	a.Auth.SetEnabledFunc(a.Kernel.Enabled)
	a.Content.SetEnabledFunc(a.Kernel.Enabled)
	if err := a.I18n.Register("core", i18n.CoreLocales()); err != nil {
		pool.Close()
		return nil, err
	}
	base := sdk.Host{Config: cfg, DB: pool, ACL: a.ACL, Content: a.Content, Auth: a.Auth, I18n: a.I18n, Log: log, Mux: a.Mux, IsEnabled: a.Kernel.Enabled}
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
