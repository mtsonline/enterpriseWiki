// Package macro_shell provides the "shell" macro: the output of a shell
// command, inserted into a page.
//
// Security model:
//   - off unless plugins.macros.shell_enabled is set in config.yaml (the web
//     interface cannot change it);
//   - every command must be approved by a superadmin (a user listed in
//     plugins.macros.shell_approvers, or the local command line). The
//     approval is bound to the SHA-256 of the exact command text: any change
//     needs a new approval;
//   - commands run with a timeout, a minimal environment, in a temporary
//     directory, as an unprivileged user (plugins.macros.shell_user when the
//     wiki runs as root) and their output is size limited and escaped.
package macro_shell

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"html"
	"io/fs"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"

	_ "embed"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// MaxOutput is the maximum number of bytes kept from a command's output.
const MaxOutput = 64 << 10

// Plugin is the shell macro plugin.
type Plugin struct {
	host  *sdk.Host
	mu    sync.Mutex
	cache map[string]cached
	locks map[string]*sync.Mutex
}

type cached struct {
	out     string
	exit    int
	expires time.Time
}

func init() { sdk.Register(&Plugin{cache: map[string]cached{}, locks: map[string]*sync.Mutex{}}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Hash returns the approval hash of a command.
func Hash(command string) string {
	sum := sha256.Sum256([]byte(command))
	return hex.EncodeToString(sum[:])
}

// normalize removes \r and surrounding blank space so that copying a
// command between systems does not change its hash.
func normalize(cmd string) string {
	return strings.TrimSpace(strings.ReplaceAll(cmd, "\r\n", "\n"))
}

// CanApprove reports whether p is a superadmin for shell commands.
func CanApprove(p *identity.Principal, approvers []string) bool {
	if p == nil {
		return false
	}
	if p.Via == "cli" {
		return true
	}
	return perm.HasP(p, "macro.shell.manage") && p.Via != "apikey" && slices.Contains(approvers, p.Handle())
}

// Setup registers the macro and the approval API.
func (p *Plugin) Setup(host *sdk.Host) error {
	p.host = host
	host.RegisterMacro(&macro{p: p})

	approvers := func() []string { return host.Config.Get().Plugins.Macros.ShellApprovers }
	requireSuper := func(r *http.Request) error {
		if err := perm.Require(r.Context(), "macro.shell.manage"); err != nil {
			return err
		}
		if !CanApprove(identity.From(r.Context()), approvers()) {
			return apperr.Forbidden("only superadmins listed in plugins.macros.shell_approvers (config.yaml) or the local command line can approve shell commands")
		}
		return nil
	}

	// GET /commands lists all known commands (admins).
	host.Handle("GET", "/commands", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := perm.Require(r.Context(), "macro.shell.manage"); err != nil {
			return nil, err
		}
		rows, err := host.DB.Query(r.Context(), `SELECT hash, command, status, element_id, u.username || '@' || u.provider, requested_at,
			decided_by, decided_at, last_run_at, last_exit, last_ms
			FROM plugin_shell_commands c LEFT JOIN users u ON u.id=c.requested_by ORDER BY (status='pending') DESC, requested_at DESC`)
		if err != nil {
			return nil, err
		}
		list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Command])
		if err != nil {
			return nil, err
		}
		cfg := host.Config.Get().Plugins.Macros
		return map[string]any{"commands": list, "enabled": cfg.ShellEnabled,
			"can_approve": CanApprove(identity.From(r.Context()), cfg.ShellApprovers)}, nil
	})
	// POST /commands registers a command ({"command": "...", "approve": true}).
	host.Handle("POST", "/commands", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := perm.Require(r.Context(), "macro.shell.manage"); err != nil {
			return nil, err
		}
		var in struct {
			Command string `json:"command"`
			Approve bool   `json:"approve"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		cmd := normalize(in.Command)
		if cmd == "" || len(cmd) > 4000 {
			return nil, apperr.BadRequest("command must have 1 to 4000 characters")
		}
		if in.Approve {
			if err := requireSuper(r); err != nil {
				return nil, err
			}
		}
		hash := Hash(cmd)
		if err := p.request(r.Context(), hash, cmd, 0); err != nil {
			return nil, err
		}
		if in.Approve {
			if err := p.decide(r.Context(), hash, "approved"); err != nil {
				return nil, err
			}
		}
		return map[string]string{"hash": hash}, nil
	})
	for _, action := range []string{"approve", "revoke"} {
		status := map[string]string{"approve": "approved", "revoke": "revoked"}[action]
		host.Handle("POST", "/commands/{hash}/"+action, func(w http.ResponseWriter, r *http.Request) (any, error) {
			if err := requireSuper(r); err != nil {
				return nil, err
			}
			return h.NoContent{}, p.decide(r.Context(), r.PathValue("hash"), status)
		})
	}
	host.Handle("DELETE", "/commands/{hash}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireSuper(r); err != nil {
			return nil, err
		}
		hash := r.PathValue("hash")
		var cmd string
		if err := host.DB.QueryRow(r.Context(), `DELETE FROM plugin_shell_commands WHERE hash=$1 RETURNING command`, hash).Scan(&cmd); err != nil {
			return nil, apperr.NotFound("command")
		}
		p.forget(hash)
		_ = audit.Log(r.Context(), host.DB, "shell.delete", "shell_command", 0, map[string]string{"hash": hash, "command": cmd}, nil)
		return h.NoContent{}, nil
	})
	return nil
}

// Command is a known shell command.
type Command struct {
	Hash        string     `json:"hash"`
	Command     string     `json:"command"`
	Status      string     `json:"status"`
	ElementID   *int64     `json:"element_id"`
	RequestedBy *string    `json:"requested_by"`
	RequestedAt time.Time  `json:"requested_at"`
	DecidedBy   *string    `json:"decided_by"`
	DecidedAt   *time.Time `json:"decided_at"`
	LastRunAt   *time.Time `json:"last_run_at"`
	LastExit    *int       `json:"last_exit"`
	LastMS      *int       `json:"last_ms"`
}

func (p *Plugin) request(ctx context.Context, hash, cmd string, element int64) error {
	var el *int64
	if element != 0 {
		el = &element
	}
	var uid *int64
	if id := identity.From(ctx).ID(); id != 0 {
		uid = &id
	}
	_, err := p.host.DB.Exec(ctx, `INSERT INTO plugin_shell_commands(hash, command, element_id, requested_by) VALUES ($1,$2,$3,$4)
		ON CONFLICT (hash) DO NOTHING`, hash, cmd, el, uid)
	return err
}

func (p *Plugin) decide(ctx context.Context, hash, status string) error {
	actor := identity.From(ctx).Handle()
	var cmd string
	err := p.host.DB.QueryRow(ctx, `UPDATE plugin_shell_commands SET status=$2, decided_by=$3, decided_at=now() WHERE hash=$1 RETURNING command`,
		hash, status, actor).Scan(&cmd)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("command")
	}
	if err != nil {
		return err
	}
	p.forget(hash)
	return audit.Log(ctx, p.host.DB, "shell."+strings.TrimSuffix(status, "d"), "shell_command", 0, nil, map[string]string{"hash": hash, "command": cmd})
}

func (p *Plugin) forget(hash string) {
	p.mu.Lock()
	delete(p.cache, hash)
	p.mu.Unlock()
}

func (p *Plugin) lock(hash string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	l := p.locks[hash]
	if l == nil {
		l = &sync.Mutex{}
		p.locks[hash] = l
	}
	return l
}

// ---------------------------------------------------------------- macro

type macro struct{ p *Plugin }

func lm(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }

func (m *macro) Name() string              { return "shell" }
func (m *macro) Labels() map[string]string { return lm("Shell command", "Shell-Befehl") }
func (m *macro) Description() map[string]string {
	return lm("Inserts the output of a command. A superadmin must approve the exact command first.",
		"Fügt die Ausgabe eines Befehls ein. Ein Superadmin muss den exakten Befehl vorher freigeben.")
}
func (m *macro) HasBody() bool { return false }
func (m *macro) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "command", Labels: lm("Command", "Befehl"), Type: "textarea", Required: true},
		{Name: "cache", Labels: lm("Cache (seconds)", "Zwischenspeichern (Sekunden)"), Type: "number", Default: "60"},
		{Name: "title", Labels: lm("Title", "Titel"), Type: "text"},
	}
}

func note(lang, en, de string) string {
	msg := en
	if lang == "de" {
		msg = de
	}
	return `<p class="ew-macro-error" role="note">` + html.EscapeString(msg) + `</p>`
}

func (m *macro) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	p := m.p
	cfg := p.host.Config.Get().Plugins.Macros
	cmd := normalize(mc.Params["command"])
	if cmd == "" {
		return note(mc.Lang, "Shell macro: no command given.", "Shell-Makro: kein Befehl angegeben."), nil
	}
	if !cfg.ShellEnabled {
		return note(mc.Lang, "Shell macros are disabled on this server.", "Shell-Makros sind auf diesem Server deaktiviert."), nil
	}
	hash := Hash(cmd)
	var status string
	err := p.host.DB.QueryRow(ctx, `SELECT status FROM plugin_shell_commands WHERE hash=$1`, hash).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := p.request(ctx, hash, cmd, mc.ElementID); err != nil {
			return "", err
		}
		status = "pending"
	} else if err != nil {
		return "", err
	}
	if status != "approved" {
		if status == "revoked" {
			return note(mc.Lang, "This shell command has been revoked by a superadmin.", "Dieser Shell-Befehl wurde von einem Superadmin gesperrt."), nil
		}
		return note(mc.Lang, "This shell command is waiting for approval by a superadmin.", "Dieser Shell-Befehl wartet auf die Freigabe durch einen Superadmin."), nil
	}
	ttl, err := strconv.Atoi(mc.Params["cache"])
	if err != nil || ttl < 0 || ttl > 86400 {
		ttl = 60
	}
	out, exit, err := p.output(ctx, hash, cmd, time.Duration(ttl)*time.Second, cfg.ShellTimeout, cfg.ShellUser)
	if err != nil {
		return note(mc.Lang, "Shell command failed: "+err.Error(), "Shell-Befehl fehlgeschlagen: "+err.Error()), nil
	}
	var b strings.Builder
	b.WriteString(`<figure class="ew-shell">`)
	if t := strings.TrimSpace(mc.Params["title"]); t != "" {
		b.WriteString(`<figcaption>` + html.EscapeString(t) + `</figcaption>`)
	}
	b.WriteString(`<pre class="ew-shell-output" tabindex="0"><code>` + html.EscapeString(out) + `</code></pre>`)
	if exit != 0 {
		b.WriteString(`<p class="ew-shell-exit">exit ` + strconv.Itoa(exit) + `</p>`)
	}
	b.WriteString(`</figure>`)
	return b.String(), nil
}

// output returns the (cached) output of an approved command.
func (p *Plugin) output(ctx context.Context, hash, cmd string, ttl time.Duration, timeout, user string) (string, int, error) {
	l := p.lock(hash)
	l.Lock()
	defer l.Unlock()
	p.mu.Lock()
	c, ok := p.cache[hash]
	p.mu.Unlock()
	if ok && time.Now().Before(c.expires) {
		return c.out, c.exit, nil
	}
	d, err := time.ParseDuration(timeout)
	if err != nil || d <= 0 {
		d = 10 * time.Second
	}
	if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	start := time.Now()
	out, exit, err := run(ctx, cmd, d, user)
	ms := int(time.Since(start).Milliseconds())
	_, _ = p.host.DB.Exec(context.WithoutCancel(ctx), `UPDATE plugin_shell_commands SET last_run_at=now(), last_exit=$2, last_ms=$3 WHERE hash=$1`, hash, exit, ms)
	if err != nil {
		return "", exit, err
	}
	if ttl > 0 {
		p.mu.Lock()
		p.cache[hash] = cached{out: out, exit: exit, expires: time.Now().Add(ttl)}
		p.mu.Unlock()
	}
	return out, exit, nil
}
