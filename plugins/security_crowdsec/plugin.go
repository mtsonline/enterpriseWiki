// Package security_crowdsec shows and manages CrowdSec from the
// administration: whether it runs, its alerts, the addresses it blocks
// (with "lift the block") and the addresses it must never block.
//
// The wiki talks to the CrowdSec Local API as a machine with its own name
// and password; it never uses the Docker socket. Machines can read alerts
// and allowlists and delete decisions, but the Local API cannot write
// allowlists. So the wiki keeps the allowed addresses in its database and
// writes them to a file in an exchange folder (EWIKI_CROWDSEC_EXCHANGE,
// only from the environment). A script inside the CrowdSec container
// (docker/crowdsec/ewiki-sync.sh) checks every line and applies the list
// with cscli; the wiki reads the result back from the Local API.
//
// CrowdSec protects the whole instance, so everything here works on the
// main site only.
package security_crowdsec

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/httpx"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

//go:embed migrations/*.sql
var migrations embed.FS

const (
	id = "security.crowdsec"
	// EnvExchange is the exchange folder shared with the CrowdSec container.
	EnvExchange = "EWIKI_CROWDSEC_EXCHANGE"
	// AllowlistFile is the file in the exchange folder (one address or
	// network per line).
	AllowlistFile = "allowlist.txt"
	// AllowlistName is the name of the allowlist in CrowdSec.
	AllowlistName = "ewiki"
	noteMax       = 200
)

// Plugin is the CrowdSec page.
type Plugin struct {
	host *coreplugin.Host

	mu     sync.Mutex
	client *lapi
	sig    string // settings the client was made with
}

func init() { coreplugin.Register(&Plugin{}) }

// Manifest implements coreplugin.Plugin.
func (p *Plugin) Manifest() coreplugin.Manifest { return coreplugin.ParseManifest(manifest) }

// Locales implements coreplugin.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Migrations implements coreplugin.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// settings of the connection (plugin settings, preset by EWIKI_CROWDSEC_*).
func (p *Plugin) settings() (url, machine, password string) {
	m := p.host.Settings()
	str := func(k string) string { v, _ := m[k].(string); return strings.TrimSpace(v) }
	url, machine, password = str("url"), str("machine_id"), str("password")
	if machine == "" {
		machine = "ewiki"
	}
	return url, machine, password
}

// lapi returns the client for the current settings (nil: not set up).
func (p *Plugin) lapi() (*lapi, error) {
	url, machine, password := p.settings()
	if url == "" || password == "" {
		return nil, nil
	}
	sig := url + "\x00" + machine + "\x00" + password
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil && p.sig == sig {
		return p.client, nil
	}
	c, err := newLAPI(url, machine, password)
	if err != nil {
		return nil, apperr.BadRequest("CrowdSec: %s", err.Error())
	}
	p.client, p.sig = c, sig
	return c, nil
}

// connected returns the client or an error that says what is missing.
func (p *Plugin) connected() (*lapi, error) {
	c, err := p.lapi()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, apperr.Conflict("CrowdSec is not set up: enter the address of the Local API and the password of the machine")
	}
	return c, nil
}

// failed turns an error of the Local API into an answer of the wiki.
func failed(err error) error {
	var ae *apperr.Error
	if errors.As(err, &ae) || errors.Is(err, context.Canceled) {
		return err
	}
	return apperr.Busy("CrowdSec: %s", err.Error())
}

// exchangeDir is the exchange folder ("" if none is set).
func exchangeDir() string {
	d := strings.TrimSpace(os.Getenv(EnvExchange))
	if d == "" || !filepath.IsAbs(d) {
		return ""
	}
	return filepath.Clean(d)
}

// view allows reading for both permissions; manage allows changes only
// in a fresh browser session or with the command line on the server.
func (p *Plugin) view(r *http.Request) error {
	if err := p.host.RequirePerm(r, id+".view"); err != nil { // manage implies view
		return err
	}
	return p.mainSite()
}

func (p *Plugin) manage(r *http.Request) error {
	if err := p.host.RequirePerm(r, id+".manage"); err != nil {
		return err
	}
	if err := p.mainSite(); err != nil {
		return err
	}
	return auth.RequireFreshSession(r.Context())
}

func (p *Plugin) mainSite() error {
	if p.host.Site != "" && p.host.Site != "main" {
		return apperr.BadRequest("CrowdSec protects the whole instance: manage it on the main site")
	}
	return nil
}

// ---------------------------------------------------------------- answers

// Status is the state of the connection.
type Status struct {
	MainSite   bool   `json:"main_site"`
	Configured bool   `json:"configured"`
	Connected  bool   `json:"connected"`
	Error      string `json:"error,omitempty"`
	Machine    string `json:"machine"`
	URL        string `json:"url"`
	// Exchange: the allowed addresses can be changed (exchange folder set)
	Exchange bool `json:"exchange"`
	Blocked  int  `json:"blocked"`
}

// Alert is an alert of CrowdSec as the wiki shows it.
type Alert struct {
	ID        int64  `json:"id"`
	Scenario  string `json:"scenario"`
	Message   string `json:"message"`
	Source    string `json:"source"`
	Country   string `json:"country,omitempty"`
	Network   string `json:"network,omitempty"`
	Events    int    `json:"events"`
	Decisions int    `json:"decisions"`
	Machine   string `json:"machine"`
	Simulated bool   `json:"simulated"`
	CreatedAt string `json:"created_at"`
}

// Decision is a decision in force (usually a ban of an address).
type Decision struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Scope     string `json:"scope"`
	Value     string `json:"value"`
	Origin    string `json:"origin"`
	Scenario  string `json:"scenario"`
	Remaining string `json:"remaining"`
	Simulated bool   `json:"simulated"`
	AlertID   int64  `json:"alert_id"`
}

// Entry is an allowed address or network.
type Entry struct {
	Value     string     `json:"value"`
	Note      string     `json:"note"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

// Allowlist is the list of the wiki and what CrowdSec applies.
type Allowlist struct {
	Entries  []Entry  `json:"entries"`
	Applied  []string `json:"applied"`
	Exchange bool     `json:"exchange"`
	InSync   bool     `json:"in_sync"`
	// Error: CrowdSec could not be asked (the list of the wiki is shown anyway)
	Error string `json:"error,omitempty"`
}

// ---------------------------------------------------------------- setup

// Setup implements coreplugin.SetupPlugin.
func (p *Plugin) Setup(h *coreplugin.Host) error {
	p.host = h
	// GET /api/v1/x/security.crowdsec/status – connection and counts.
	// action: security.crowdsec.status
	h.Handle("GET", "/status", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := p.view(r); err != nil {
			var ae *apperr.Error
			if errors.As(err, &ae) && ae.Code == "bad_request" {
				return Status{}, nil // another site: nothing to show
			}
			return nil, err
		}
		url, machine, _ := p.settings()
		st := Status{MainSite: true, Machine: machine, URL: url, Exchange: exchangeDir() != ""}
		c, err := p.lapi()
		if err != nil {
			st.Error = err.Error()
			return st, nil
		}
		if c == nil {
			return st, nil
		}
		st.Configured = true
		list, err := c.alerts(r.Context(), int(h.Limit(id+".list_max")), true)
		if err != nil {
			st.Error = err.Error()
			return st, nil
		}
		st.Connected = true
		st.Blocked = len(decisionsOf(list))
		return st, nil
	})

	// GET /api/v1/x/security.crowdsec/alerts – the latest alerts.
	// action: security.crowdsec.alerts
	h.Handle("GET", "/alerts", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := p.view(r); err != nil {
			return nil, err
		}
		c, err := p.connected()
		if err != nil {
			return nil, err
		}
		max := int(h.Limit(id + ".list_max"))
		limit := httpx.QueryInt(r, "limit", 50)
		if limit < 1 || limit > max {
			limit = max
		}
		list, err := c.alerts(r.Context(), limit, false)
		if err != nil {
			return nil, failed(err)
		}
		out := make([]Alert, 0, len(list))
		for _, a := range list {
			src := a.Source.Value
			if src == "" {
				src = a.Source.IP
			}
			out = append(out, Alert{ID: a.ID, Scenario: short(a.Scenario, 200), Message: short(a.Message, 500), Source: short(src, 100),
				Country: short(a.Source.CN, 8), Network: short(a.Source.ASName, 200), Events: a.EventsCount, Decisions: len(a.Decisions),
				Machine: short(a.MachineID, 100), Simulated: a.Simulated, CreatedAt: a.CreatedAt})
		}
		return out, nil
	})

	// GET /api/v1/x/security.crowdsec/decisions – addresses blocked now.
	// action: security.crowdsec.decisions
	h.Handle("GET", "/decisions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := p.view(r); err != nil {
			return nil, err
		}
		c, err := p.connected()
		if err != nil {
			return nil, err
		}
		list, err := c.alerts(r.Context(), int(h.Limit(id+".list_max")), true)
		if err != nil {
			return nil, failed(err)
		}
		return decisionsOf(list), nil
	})

	// DELETE /api/v1/x/security.crowdsec/decisions/{id} – lift a block.
	// action: security.crowdsec.unblock
	h.Handle("DELETE", "/decisions/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := p.manage(r); err != nil {
			return nil, err
		}
		did, err := httpx.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		c, err := p.connected()
		if err != nil {
			return nil, err
		}
		// the decision must be one in force (and its address goes into the audit)
		list, err := c.alerts(r.Context(), int(h.Limit(id+".list_max")), true)
		if err != nil {
			return nil, failed(err)
		}
		var d *Decision
		for _, x := range decisionsOf(list) {
			if x.ID == did {
				d = &x
				break
			}
		}
		if d == nil {
			return nil, apperr.NotFound("block")
		}
		if _, err := c.deleteDecision(r.Context(), did); err != nil {
			return nil, failed(err)
		}
		if err := audit.Log(r.Context(), h.DB, id+".unblock", "crowdsec_decision", did, d, nil); err != nil {
			return nil, err
		}
		return httpx.NoContent{}, nil
	})

	// GET /api/v1/x/security.crowdsec/allowlist – addresses never blocked.
	// action: security.crowdsec.allowlist
	h.Handle("GET", "/allowlist", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := p.view(r); err != nil {
			return nil, err
		}
		return p.allowlist(r.Context())
	})

	// PUT /api/v1/x/security.crowdsec/allowlist – replace the allowed addresses.
	// action: security.crowdsec.allowlist.set
	h.Handle("PUT", "/allowlist", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := p.manage(r); err != nil {
			return nil, err
		}
		dir := exchangeDir()
		if dir == "" {
			return nil, apperr.Conflict("the allowed addresses cannot be changed: no exchange folder with CrowdSec is set up (%s)", EnvExchange)
		}
		var in struct {
			Entries []Entry `json:"entries"`
		}
		if err := httpx.Decode(r, &in); err != nil {
			return nil, err
		}
		entries, err := cleanEntries(in.Entries, int(h.Limit(id+".allowlist_max")))
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		before, err := p.entries(ctx)
		if err != nil {
			return nil, err
		}
		err = db.Tx(ctx, h.DB, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `DELETE FROM plugin_crowdsec_allowlist WHERE NOT (value = ANY($1))`, values(entries)); err != nil {
				return err
			}
			for _, e := range entries {
				if _, err := tx.Exec(ctx, `INSERT INTO plugin_crowdsec_allowlist(value, note) VALUES ($1,$2)
					ON CONFLICT (value) DO UPDATE SET note=EXCLUDED.note`, e.Value, e.Note); err != nil {
					return err
				}
			}
			return audit.Log(ctx, tx, id+".allowlist", "crowdsec_allowlist", 0, before, entries)
		})
		if err != nil {
			return nil, err
		}
		// the file follows the database; if writing fails now, the worker
		// writes it again (the list in the database is the reference)
		if err := writeAllowlist(dir, values(entries)); err != nil {
			h.Log.Warn("crowdsec: exchange file not written", "err", err)
			return nil, apperr.Busy("the allowed addresses are saved, but the exchange file could not be written; the wiki tries again: %s", err.Error())
		}
		return p.allowlist(ctx)
	})

	// the exchange file follows the database (after a restart, a failed write
	// or a file removed by hand)
	h.RegisterWorker("allowlist", time.Minute, func(ctx context.Context) error {
		dir := exchangeDir()
		if dir == "" || p.mainSite() != nil {
			return nil
		}
		list, err := p.entries(ctx)
		if err != nil {
			return err
		}
		return writeAllowlist(dir, values(list))
	})
	return nil
}

// decisionsOf flattens the decisions of alerts (each once).
func decisionsOf(list []lapiAlert) []Decision {
	seen := map[int64]bool{}
	out := []Decision{}
	for _, a := range list {
		for _, d := range a.Decisions {
			if seen[d.ID] {
				continue
			}
			seen[d.ID] = true
			out = append(out, Decision{ID: d.ID, Type: short(d.Type, 50), Scope: short(d.Scope, 50), Value: short(d.Value, 100),
				Origin: short(d.Origin, 50), Scenario: short(d.Scenario, 200), Remaining: short(d.Duration, 50), Simulated: d.Simulated, AlertID: a.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// entries reads the allowed addresses of the wiki.
func (p *Plugin) entries(ctx context.Context) ([]Entry, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT value, note, created_at FROM plugin_crowdsec_allowlist ORDER BY value`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Entry, error) {
		var e Entry
		var at time.Time
		err := row.Scan(&e.Value, &e.Note, &at)
		e.CreatedAt = &at
		return e, err
	})
}

// allowlist combines the list of the wiki with what CrowdSec applies.
func (p *Plugin) allowlist(ctx context.Context) (*Allowlist, error) {
	list, err := p.entries(ctx)
	if err != nil {
		return nil, err
	}
	out := &Allowlist{Entries: list, Applied: []string{}, Exchange: exchangeDir() != ""}
	c, err := p.lapi()
	if err != nil || c == nil {
		if err != nil {
			out.Error = err.Error()
		}
		return out, nil
	}
	als, err := c.allowlists(ctx)
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	for _, a := range als {
		if a.Name != AllowlistName {
			continue
		}
		for _, it := range a.Items {
			if v, err := canonical(it.Value); err == nil {
				out.Applied = append(out.Applied, v)
			}
		}
	}
	sort.Strings(out.Applied)
	out.InSync = slices.Equal(out.Applied, values(list))
	return out, nil
}

func values(list []Entry) []string {
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = e.Value
	}
	sort.Strings(out)
	return out
}

// canonical checks an address or network and returns its usual form.
// Networks broader than /8 (IPv4) or /24 (IPv6) are refused: they would
// switch CrowdSec off for a large part of the internet.
func canonical(v string) (string, error) {
	v = strings.TrimSpace(v)
	if strings.Contains(v, "/") {
		pfx, err := netip.ParsePrefix(v)
		if err != nil {
			return "", apperr.BadRequest("%q is not an IP address or network", v)
		}
		pfx = pfx.Masked()
		min := 8
		if pfx.Addr().Is6() && !pfx.Addr().Is4In6() {
			min = 24
		}
		if pfx.Bits() < min {
			return "", apperr.BadRequest("the network %s is too large (at least /%d)", pfx.String(), min)
		}
		if pfx.IsSingleIP() {
			return pfx.Addr().String(), nil
		}
		return pfx.String(), nil
	}
	a, err := netip.ParseAddr(v)
	if err != nil || a.Zone() != "" {
		return "", apperr.BadRequest("%q is not an IP address or network", v)
	}
	return a.Unmap().String(), nil
}

// cleanEntries checks and normalizes the entries (no duplicates).
func cleanEntries(in []Entry, max int) ([]Entry, error) {
	if len(in) > max {
		return nil, apperr.BadRequest("at most %d allowed addresses", max)
	}
	seen := map[string]bool{}
	out := make([]Entry, 0, len(in))
	for _, e := range in {
		v, err := canonical(e.Value)
		if err != nil {
			return nil, err
		}
		if seen[v] {
			return nil, apperr.BadRequest("%s is listed twice", v)
		}
		seen[v] = true
		note := strings.TrimSpace(e.Note)
		if !utf8.ValidString(note) || utf8.RuneCountInString(note) > noteMax || strings.ContainsAny(note, "\r\n") {
			return nil, apperr.BadRequest("a note has at most %d characters on one line", noteMax)
		}
		out = append(out, Entry{Value: v, Note: note})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out, nil
}

// fileHeader starts the exchange file; the script in the CrowdSec container
// ignores lines starting with #.
const fileHeader = "# Allowed addresses of Enterprise Wiki for CrowdSec (written by the wiki; changes here are overwritten)\n"

// writeAllowlist writes the exchange file atomically, only when it changed.
func writeAllowlist(dir string, vals []string) error {
	var b strings.Builder
	b.WriteString(fileHeader)
	for _, v := range vals {
		// values come from canonical(): addresses and networks only
		b.WriteString(v)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { // #nosec G301 -- read by the CrowdSec container, holds no secret
		return err
	}
	path := filepath.Join(dir, AllowlistFile)
	if cur, err := os.ReadFile(path); err == nil && string(cur) == b.String() { // #nosec G304 -- fixed name in the folder of EWIKI_CROWDSEC_EXCHANGE
		return nil
	}
	tmp, err := os.CreateTemp(dir, ".allowlist-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(b.String()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil { // #nosec G302 -- read by the CrowdSec container, holds no secret
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("exchange file: %w", err)
	}
	return nil
}
