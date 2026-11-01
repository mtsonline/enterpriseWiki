// Package cli implements the ewiki command line interface. Every API
// function is available through "ewiki api", plus convenience commands for
// installation, users, plugins and configuration.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
	"github.com/mtsonline/enterprisewiki/internal/server"
	"github.com/mtsonline/enterprisewiki/internal/version"
	"github.com/mtsonline/enterprisewiki/web"
)

type env struct {
	configPath string
	stdout     io.Writer
	stderr     io.Writer
	stdin      io.Reader
	log        *slog.Logger
}

const usage = `Enterprise Wiki %s

Usage: ewiki [--config FILE] <command> [arguments]

Server
  serve                         start the HTTP server (applies migrations)
  setup [--non-interactive]     install: write config, migrate DB, create admin
  migrate                       apply pending database migrations
  version                       print the version

Generic API access (every API function, as superadmin or --as USER)
  api METHOD PATH [BODY]        e.g. ewiki api GET /api/v1/categories
                                BODY: JSON string, @file.json or - (stdin)

Users
  user list [QUERY]
  user create NAME [--admin] [--display-name N] [--email E] [--password-stdin]
  user passwd NAME [--password-stdin]
  user disable NAME | user enable NAME | user unlock NAME

Plugins
  plugin list
  plugin enable ID | plugin disable ID
  plugin install SOURCE [--ref TAG] [--build]   SOURCE: catalog id, git URL,
                                                github.com/owner/repo or directory
  plugin remove ID [--build]
  plugin updates                check core and plugins for new GitHub releases
  plugin update ID [--build]    install the latest release of a plugin
  plugin generate               regenerate plugins/all/zz_generated.go
  plugin build                  rebuild the binary (scripts/build.sh)

Configuration (config.yaml is the single source of truth)
  config show | config path | config validate
  config get KEY                e.g. ewiki config get ui.site_name
  config set KEY VALUE          e.g. ewiki config set retention.trash_days 60

Maintenance
  retention run                 apply retention rules now

Environment: EWIKI_CONFIG (config path), EWIKI_LOG_LEVEL (debug|info|warn|error),
EWIKI_LOG_FORMAT (text|json). Variables from .env next to config.yaml are loaded.
`

// Main runs the CLI and returns the exit code.
func Main(args []string) int {
	e := &env{stdout: os.Stdout, stderr: os.Stderr, stdin: os.Stdin}
	e.configPath = os.Getenv("EWIKI_CONFIG")
	if e.configPath == "" {
		e.configPath = "config.yaml"
	}
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch {
		case args[0] == "--config" && len(args) > 1:
			e.configPath, args = args[1], args[2:]
		case strings.HasPrefix(args[0], "--config="):
			e.configPath, args = strings.TrimPrefix(args[0], "--config="), args[1:]
		case args[0] == "-h" || args[0] == "--help":
			args = []string{"help"}
		default:
			fmt.Fprintf(e.stderr, "unknown option %s\n", args[0])
			return 2
		}
	}
	if abs, err := filepath.Abs(e.configPath); err == nil {
		e.configPath = abs
	}
	e.log = newLogger()
	slog.SetDefault(e.log)
	if len(args) == 0 {
		args = []string{"help"}
	}
	var err error
	switch args[0] {
	case "help":
		fmt.Fprintf(e.stdout, usage, version.Version)
	case "version":
		fmt.Fprintf(e.stdout, "ewiki %s (%s)\n", version.Version, version.Commit)
	case "serve":
		err = e.serve(args[1:])
	case "setup":
		err = e.setup(args[1:])
	case "migrate":
		err = e.withApp(true, func(ctx context.Context, a *app.App) error { return nil })
	case "api":
		err = e.api(args[1:])
	case "user":
		err = e.user(args[1:])
	case "plugin":
		err = e.plugin(args[1:])
	case "config":
		err = e.config(args[1:])
	case "retention":
		err = e.withApp(false, func(ctx context.Context, a *app.App) error {
			res, err := a.Content.RunRetention(ctx)
			if err == nil {
				printJSON(e.stdout, res)
			}
			return err
		})
	default:
		fmt.Fprintf(e.stderr, "unknown command %q (see ewiki help)\n", args[0])
		return 2
	}
	if err != nil {
		_, _, msg := apperr.Status(err)
		var ae *apperr.Error
		if !errors.As(err, &ae) {
			msg = err.Error()
		}
		fmt.Fprintln(e.stderr, "error:", msg)
		return 1
	}
	return 0
}

func newLogger() *slog.Logger {
	lvl := slog.LevelInfo
	switch strings.ToLower(os.Getenv("EWIKI_LOG_LEVEL")) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: lvl}
	if os.Getenv("EWIKI_LOG_FORMAT") == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func (e *env) withApp(migrate bool, fn func(ctx context.Context, a *app.App) error) error {
	ctx := identity.With(context.Background(), identity.CLI())
	a, err := app.New(ctx, app.Options{ConfigPath: e.configPath, Log: e.log, Migrate: migrate})
	if err != nil {
		return err
	}
	defer a.Close()
	return fn(ctx, a)
}

// serve runs the server with background jobs, config reloading and restart support.
func (e *env) serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	noMigrate := fs.Bool("no-migrate", false, "do not apply migrations on start")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(e.configPath); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s not found - run \"ewiki setup\" first", e.configPath)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a, err := app.New(ctx, app.Options{ConfigPath: e.configPath, Log: e.log, Migrate: !*noMigrate})
	if err != nil {
		return err
	}
	defer a.Close()
	restart := make(chan struct{}, 1)
	pm := pluginmgr.New(sourceRoot(a.Root), a.Config, func() {
		select {
		case restart <- struct{}{}:
		default:
		}
		stop()
	})
	srv := server.New(a, web.FS(), pm)
	go a.Bus.Run(ctx)
	go e.jobs(ctx, a)
	a.Workers.Start(ctx)
	go e.watchConfig(ctx, a)
	if err := srv.Run(ctx); err != nil {
		return err
	}
	select {
	case <-restart:
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		a.Close()
		e.log.Info("restarting", "exe", exe)
		return syscall.Exec(exe, os.Args, os.Environ())
	default:
	}
	return nil
}

// watchConfig reloads config.yaml on SIGHUP and when the file changes.
func (e *env) watchConfig(ctx context.Context, a *app.App) {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-hup:
		case <-t.C:
			if !a.Config.Changed() {
				continue
			}
		}
		if err := a.Config.Reload(); err != nil {
			e.log.Error("config reload failed, keeping previous configuration", "err", err)
			continue
		}
		e.log.Info("configuration reloaded")
	}
}

// jobs runs periodic maintenance. In cluster mode only the node holding the
// advisory lock (leader) runs them.
func (e *env) jobs(ctx context.Context, a *app.App) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	run := func() {
		jctx := identity.With(ctx, identity.CLI())
		if a.Config.Get().Mode == "cluster" {
			conn, err := a.DB.Acquire(jctx)
			if err != nil {
				return
			}
			defer conn.Release()
			var ok bool
			if err := conn.QueryRow(jctx, `SELECT pg_try_advisory_lock(7300418)`).Scan(&ok); err != nil || !ok {
				return
			}
			defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(7300418)`) //nolint:errcheck
		}
		if err := a.Auth.Cleanup(jctx); err != nil {
			e.log.Error("session cleanup", "err", err)
		}
		if n, err := a.Files.GC(jctx); err != nil {
			e.log.Error("file cleanup", "err", err)
		} else if n > 0 {
			e.log.Info("unused files removed", "count", n)
		}
		if res, err := a.Content.RunRetention(jctx); err != nil {
			e.log.Error("retention", "err", err)
		} else if res.Revisions+res.Audit > 0 || res.Trash > 0 {
			e.log.Info("retention applied", "revisions", res.Revisions, "trash", res.Trash, "audit", res.Audit)
		}
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
		run()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

// ---------------------------------------------------------------- config

func (e *env) config(args []string) error {
	if len(args) == 0 {
		args = []string{"show"}
	}
	switch args[0] {
	case "path":
		fmt.Fprintln(e.stdout, e.configPath)
		return nil
	case "show":
		b, err := os.ReadFile(e.configPath)
		if err != nil {
			return err
		}
		_, err = e.stdout.Write(b)
		return err
	case "validate":
		if _, err := config.Open(e.configPath); err != nil {
			return err
		}
		fmt.Fprintln(e.stdout, "configuration is valid")
		return nil
	case "get":
		if len(args) != 2 {
			return errors.New("usage: ewiki config get KEY")
		}
		st, err := config.Open(e.configPath)
		if err != nil {
			return err
		}
		v, err := config.GetKey(st.Raw(), args[1])
		if err != nil {
			return err
		}
		fmt.Fprintln(e.stdout, v)
		return nil
	case "set":
		if len(args) != 3 {
			return errors.New("usage: ewiki config set KEY VALUE")
		}
		st, err := config.Open(e.configPath)
		if err != nil {
			return err
		}
		if err := st.Update(func(c *config.Config) error { return config.SetKey(c, args[1], args[2]) }); err != nil {
			return err
		}
		fmt.Fprintf(e.stdout, "%s updated (running servers reload it automatically)\n", args[1])
		return nil
	}
	return fmt.Errorf("unknown config command %q", args[0])
}
