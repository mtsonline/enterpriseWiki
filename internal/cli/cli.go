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
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
	"github.com/mtsonline/enterprisewiki/internal/server"
	"github.com/mtsonline/enterprisewiki/internal/version"
	"github.com/mtsonline/enterprisewiki/web"
)

type env struct {
	configPath string
	site       string // --site / EWIKI_SITE (default main)
	outErr     error  // first failed write to stdout (e.g. a closed pipe)
	stdout     io.Writer
	stderr     io.Writer
	stdin      io.Reader
	log        *slog.Logger
}

const usage = `Enterprise Wiki %s

Usage: ewiki [--config FILE] [--site NAME] <command> [arguments]

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
  user invite ADDRESS [--display-name N] [--groups A,B]   account before the first single sign-on
  user emails NAME | user email-add NAME ADDRESS [--unconfirmed]
  user email-primary NAME ADDRESS | user email-remove NAME ADDRESS
  user identities NAME | user unlink NAME PROVIDER SUBJECT
  user alias NAME [ALIAS]       wiki user name (sign-in with it); without ALIAS: remove
  user sessions NAME | user logout NAME [SESSION-ID]   sessions; sign out one or all
  scim list | scim create NAME --provider P [--subject-from userName|externalId] [--on-delete disable|delete]
  scim token ID | scim delete ID   SCIM 2.0 provisioning clients (Entra ID, Okta, ...)
  user signin-preview PROVIDER USERNAME [--email E] [--verified] [--groups A,B] [--subject S]
                                what a sign-in would lead to (mapping, JIT, linking)
  user passwd NAME [--password-stdin]
  user disable NAME | user enable NAME | user unlock NAME
  user mfa-reset NAME           remove the second factors (app and e-mail)
  user networks NAME [NET...]   allowed IP addresses / CIDR networks (none: everywhere)

Guest cache
  cache status | cache clear

Media library
  media list|upload|info|update|replace|edit|versions|restore|usage|acl|download|delete   (ewiki media for details)
  demo status|create|remove|reset-site   demo content of all features; reset the site to its first start

License (Business Source License 1.1, free for private use of natural persons)
  site list | create NAME --host H[,H] [--title T] | update NAME [...] | delete NAME --yes
  site admins | site admins add|remove USER_ID     instance administrators
  license show | install KEY|@file|- | remove
  license notices               licenses of the third-party components

Watches (e-mail on changes)
  watch list --as USER
  watch add category|element ID [--mode link|content] [--frequency instant|daily|weekly]
            [--no-recursive] --as USER
  watch remove WATCH_ID --as USER
  watch assign list | remove ID
  watch assign add user|group|role SUBJECT_ID category|element TARGET_ID
            [--mode link|content] [--frequency instant|daily|weekly] [--no-recursive]
            [--mandatory] [--note TEXT]       centrally assigned watches

Permissions, audit log, drafts, notifications
  perm list                     all permissions (core and plugins)
  perm overview user|group|role|authenticated|guest [ID]
                                direct and effective permissions per category
  perm defaults set KEY...      global permissions of every signed-in user
  audit search [--q T] [--area A] [--action A] [--actor N] [--ip IP] [--from T] [--to T]
               [--entity-type T --entity-id ID] [--limit N]
  audit export [--format csv|jsonl] [filters]
  audit settings | audit settings set @file|-
  audit forward-test @file|-    send a test entry to a forwarding target (JSON as in settings)
  draft list | show ID | delete ID --as USER
  draft publish ID [--at RFC3339] [--category ID] [--silent] --as USER
  inbox [list] [--limit N] | inbox read [ID...] --as USER

Personal settings
  pref list --as USER           settings of the user and their allowed values
  pref set KEY VALUE --as USER  e.g. pref set editor simple ("" removes it)

Translations of the user interface
  i18n status                   progress of every language
  i18n strings LANG [--filter todo|missing|outdated|custom|all] [--source ID]
  i18n set LANG KEY TEXT        e.g. i18n set de inbox.new_one "{n} neue Nachricht"
  i18n export LANG [--format json|xliff|po] [--source ID] [--filter all|missing|custom] > FILE
  i18n import LANG FILE|- [--format json|xliff|po]
  i18n add LANG | i18n remove LANG
  category translations ID      translation state of a category per language
  category translate ID LANG [--title T] [--description D] [--confirm] [--as USER]

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

E-mail queue
  mail status                   transport, queue counts, oldest undelivered message
  mail list [--status S] [--q TEXT] [--limit N]
  mail show ID                  message with the delivery state of every recipient
  mail retry ID | mail delete ID
  mail test ADDRESS             send a test message now
  mail send                     deliver queued messages now
  mail bounces                  read the bounce mailbox now

Signatures
  signature verify FILE [--offline]  check an exported signature (record, method, seal of this wiki)
  signature keys                public seal keys of the wiki (fingerprints)
  smime list|add|set|remove|settings  S/MIME certificates and settings (plugin signature.smime, see ewiki smime)

Maintenance
  retention run                 apply retention rules now
  jobs list | jobs run NAME     background jobs (mail, workflow timers, ...)

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
		case args[0] == "--site" && len(args) > 1:
			e.site, args = args[1], args[2:]
		case strings.HasPrefix(args[0], "--site="):
			e.site, args = strings.TrimPrefix(args[0], "--site="), args[1:]
		case args[0] == "-h" || args[0] == "--help":
			args = []string{"help"}
		default:
			e.eprintf("unknown option %s\n", args[0])
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
	// Docker images: hand over to the binary built with additional plugins
	if exe := pluginmgr.Launch(context.Background(), sourceRoot(filepath.Dir(e.configPath)), e.configPath, args[0] == "serve", e.log); exe != "" {
		self, _ := os.Executable()
		env := append(withoutEnv(os.Environ(), "EWIKI_CHILD", "EWIKI_LAUNCHER"), "EWIKI_CHILD=1", "EWIKI_LAUNCHER="+self)
		if err := syscall.Exec(exe, append([]string{exe}, args...), env); err != nil { // #nosec G204 G702 -- the binary built for this installation
			e.log.Error("starting the binary with the additional plugins failed; continuing without them", "exe", exe, "err", err)
		}
	}
	var err error
	switch args[0] {
	case "help":
		e.printf(usage, version.Version)
	case "version":
		e.printf("ewiki %s (%s)\n", version.Version, version.Commit)
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
	case "scim":
		err = e.scim(args[1:])
	case "plugin":
		err = e.plugin(args[1:])
	case "config":
		err = e.config(args[1:])
	case "mail":
		err = e.mailCmd(args[1:])
	case "jobs":
		err = e.jobsCmd(args[1:])
	case "watch":
		err = e.watchCmd(args[1:])
	case "audit":
		err = e.auditCmd(args[1:])
	case "perm":
		err = e.permCmd(args[1:])
	case "draft":
		err = e.draftCmd(args[1:])
	case "inbox":
		err = e.inboxCmd(args[1:])
	case "i18n":
		err = e.i18nCmd(args[1:])
	case "pref":
		err = e.prefCmd(args[1:])
	case "category":
		err = e.categoryCmd(args[1:])
	case "cache":
		err = e.cacheCmd(args[1:])
	case "media":
		err = e.media(args[1:])
	case "demo":
		err = e.demoCmd(args[1:])
	case "site":
		err = e.siteCmd(args[1:])
	case "license":
		err = e.licenseCmd(args[1:])
	case "signature":
		err = e.signatureCmd(args[1:])
	case "smime":
		err = e.smimeCmd(args[1:])
	case "retention":
		err = e.withApp(false, func(ctx context.Context, a *app.App) error {
			res, err := a.Content.RunRetention(ctx)
			if err == nil {
				printJSON(e.stdout, res)
			}
			return err
		})
	default:
		e.eprintf("unknown command %q (see ewiki help)\n", args[0])
		return 2
	}
	if err == nil && e.outErr != nil {
		err = fmt.Errorf("writing the output: %w", e.outErr)
	}
	if err != nil {
		_, _, msg := apperr.Status(err)
		var ae *apperr.Error
		if !errors.As(err, &ae) {
			msg = err.Error()
		}
		e.eprintln("error:", msg)
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
	ctx := identity.WithSite(identity.With(context.Background(), identity.CLI()), e.siteName())
	a, err := app.New(ctx, app.Options{ConfigPath: e.configPath, Log: e.log, Migrate: migrate, Site: e.siteName()})
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
	in, err := app.OpenInstance(ctx, app.InstanceOptions{ConfigPath: e.configPath, Log: e.log, Migrate: !*noMigrate})
	if err != nil {
		return err
	}
	defer in.Close()
	a := in.Site("main")
	if a == nil {
		return errors.New("site main is not available")
	}
	restart := make(chan struct{}, 1)
	pm := pluginmgr.New(sourceRoot(a.Root), a.Config, func() {
		select {
		case restart <- struct{}{}:
		default:
		}
		stop()
	})
	pm.Attach(pluginmgr.Cluster{Enabled: func() bool { return a.Config.Get().Mode == "cluster" }, DB: a.DB,
		Publish: a.Bus.Publish, Subscribe: a.Bus.Subscribe}, e.log)
	srv := server.NewInstance(in, web.FS(), pm)
	srv.Background = func(ctx context.Context, a *app.App) {
		go a.Bus.Run(ctx)
		go e.jobs(ctx, a)
		a.Workers.Start(ctx)
		if a.Site != "main" {
			go e.watchConfig(ctx, a)
		}
	}
	a.PGVersion.LogStatus(ctx)
	go pluginmgr.ReleaseRestartLease(ctx, a.DB, a.Config.Get().Server.Listen)
	go pm.Watch(ctx, 10*time.Second)
	go e.watchConfig(ctx, a)
	go in.RunSync(ctx, 15*time.Second)
	if err := srv.Run(ctx); err != nil {
		return err
	}
	select {
	case <-restart:
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		env := os.Environ()
		if l := os.Getenv("EWIKI_LAUNCHER"); l != "" {
			// Docker image: the image binary decides again which binary to run
			exe, env = l, withoutEnv(env, "EWIKI_CHILD", "EWIKI_LAUNCHER")
		}
		in.Close()
		e.log.Info("restarting", "exe", exe)
		return syscall.Exec(exe, append([]string{exe}, os.Args[1:]...), env) // #nosec G204 G702 -- restart of the own binary
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
		jctx := identity.WithSite(identity.With(ctx, identity.CLI()), a.Site)
		if a.Config.Get().Mode == "cluster" {
			conn, err := a.DB.Acquire(jctx)
			if err != nil {
				return
			}
			defer conn.Release()
			var ok bool
			lock := "ewiki.maintenance." + a.Site
			if err := conn.QueryRow(jctx, `SELECT pg_try_advisory_lock(hashtext($1))`, lock).Scan(&ok); err != nil || !ok {
				return
			}
			defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext($1))`, lock) }() // released with the connection anyway
		}
		if err := a.Auth.Cleanup(jctx); err != nil {
			e.log.Error("session cleanup", "err", err)
		}
		if n, err := a.Files.GC(jctx); err != nil {
			e.log.Error("file cleanup", "err", err)
		} else if n > 0 {
			e.log.Info("unused files removed", "count", n)
		}
		if n := a.Files.PruneCache(7 * 24 * time.Hour); n > 0 {
			e.log.Info("cached derived files removed", "count", n)
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
		st, err := e.configStore()
		if err != nil {
			return err
		}
		e.println(st.Path())
		return nil
	case "show":
		st, err := e.configStore()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(st.Path()) // #nosec G304 -- configuration file of the installation
		if errors.Is(err, os.ErrNotExist) && st.Site() != "" {
			b, err = []byte("# no differences to the instance settings\n"), nil
		}
		if err != nil {
			return err
		}
		_, err = e.stdout.Write(b)
		return err
	case "validate":
		if _, err := e.configStore(); err != nil {
			return err
		}
		e.println("configuration is valid")
		return nil
	case "get":
		if len(args) != 2 {
			return errors.New("usage: ewiki config get KEY")
		}
		st, err := e.configStore()
		if err != nil {
			return err
		}
		v, err := config.GetKey(st.Raw(), args[1])
		if err != nil {
			return err
		}
		e.println(v)
		return nil
	case "set":
		if len(args) != 3 {
			return errors.New("usage: ewiki config set KEY VALUE")
		}
		st, err := e.configStore()
		if err != nil {
			return err
		}
		if st.Site() != "" && config.IsInstanceOnly(args[1]) {
			return fmt.Errorf("%s applies to the whole instance: set it without --site", args[1])
		}
		if err := st.Update(func(c *config.Config) error { return config.SetKey(c, args[1], args[2]) }); err != nil {
			return err
		}
		e.printf("%s updated (running servers reload it automatically)\n", args[1])
		return nil
	}
	return fmt.Errorf("unknown config command %q", args[0])
}

// configStore returns the settings of the selected site (--site): the
// instance settings for "main", otherwise the site's own file on top.
func (e *env) configStore() (*config.Store, error) {
	base, err := config.Open(e.configPath)
	if err != nil || e.siteName() == "main" {
		return base, err
	}
	if !db.ValidSiteName(e.siteName()) {
		return nil, fmt.Errorf("invalid site name %q", e.siteName())
	}
	return config.OpenSite(base, e.siteName())
}

// withoutEnv removes variables from an environment list.
func withoutEnv(env []string, keys ...string) []string {
	out := env[:0:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(keys, k) {
			out = append(out, kv)
		}
	}
	return out
}

// siteName returns the site local commands work on (--site, EWIKI_SITE).
func (e *env) siteName() string {
	if e.site != "" {
		return e.site
	}
	if s := os.Getenv("EWIKI_SITE"); s != "" {
		return s
	}
	return "main"
}

// printf, println and print write command output to stdout; the first
// failed write makes the command fail at the end.
func (e *env) printf(format string, a ...any) { e.keep(fmt.Fprintf(e.stdout, format, a...)) }
func (e *env) println(a ...any)               { e.keep(fmt.Fprintln(e.stdout, a...)) }
func (e *env) print(a ...any)                 { e.keep(fmt.Fprint(e.stdout, a...)) }

func (e *env) keep(_ int, err error) {
	if err != nil && e.outErr == nil {
		e.outErr = err
	}
}

// eprintf, eprintln, eprint and ewrite write messages to stderr; there is
// nowhere left to report a failure of these writes.
func (e *env) eprintf(format string, a ...any) { _, _ = fmt.Fprintf(e.stderr, format, a...) }
func (e *env) eprintln(a ...any)               { _, _ = fmt.Fprintln(e.stderr, a...) }
func (e *env) eprint(a ...any)                 { _, _ = fmt.Fprint(e.stderr, a...) }
func (e *env) ewrite(b []byte)                 { _, _ = e.stderr.Write(b) }

// row and rowln write a line of a table; write errors of a tabwriter are
// reported by its Flush, which every caller checks.
func row(tw *tabwriter.Writer, format string, a ...any) { _, _ = fmt.Fprintf(tw, format, a...) }
func rowln(tw *tabwriter.Writer, a ...any)              { _, _ = fmt.Fprintln(tw, a...) }
