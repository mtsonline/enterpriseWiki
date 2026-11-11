// Package mfa_network is the factor "allowed networks": a person's own list
// of IP addresses and networks (CIDR). In the chain it passes without any
// action when the sign-in comes from one of them (e.g. "from the campus no
// code is needed": network and authenticator app as alternatives) and
// fails otherwise.
//
// Settings (plugin_settings."mfa.network"):
//
//	factor: 2            # place in the chain
//	enforce_always: true # also refuse sign-ins, sessions, API keys and feed
//	                     # links from elsewhere (false: only a factor)
package mfa_network

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// Plugin is the network plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Setup registers the factor, the guard and the endpoints.
func (p *Plugin) Setup(host *sdk.Host) error {
	m := &Method{host: host, cache: map[int64]cached{}}
	host.RegisterMFAMethod(m)
	host.RegisterAccessGuard(m)
	type netsIn struct {
		Networks []string `json:"networks"`
	}
	host.Handle("GET", "/me", func(w http.ResponseWriter, r *http.Request) (any, error) {
		pr := identity.From(r.Context())
		if pr == nil || pr.UserID == 0 {
			return nil, apperr.ErrUnauthorized
		}
		nets, err := m.networks(r.Context(), pr.UserID)
		return map[string]any{"networks": nets, "current_ip": audit.ClientIP(r.Context()), "enforce_always": m.enforce()}, err
	})
	// changing the own networks needs a recent sign-in; the current address
	// must be included (nobody locks themselves out by accident)
	host.Handle("PUT", "/me", func(w http.ResponseWriter, r *http.Request) (any, error) {
		uid, err := auth.RequireRecentSignIn(r.Context())
		if err != nil {
			return nil, err
		}
		var in netsIn
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		nets, err := ParseNetworks(in.Networks)
		if err != nil {
			return nil, err
		}
		if ip := audit.ClientIP(r.Context()); ip != "" && len(nets) > 0 && !IPAllowed(nets, ip) {
			return nil, apperr.BadRequest("your current address %s is not in these networks: you would lock yourself out", ip)
		}
		return map[string]any{"networks": nets}, m.set(r.Context(), uid, nets)
	})
	host.Handle("GET", "/users/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if err := host.Auth.RequireUserAdmin(r.Context(), id); err != nil {
			return nil, err
		}
		nets, err := m.networks(r.Context(), id)
		return map[string]any{"networks": nets}, err
	})
	host.Handle("PUT", "/users/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if err := host.Auth.RequireUserAdmin(r.Context(), id); err != nil {
			return nil, err
		}
		var in netsIn
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		nets, err := ParseNetworks(in.Networks)
		if err != nil {
			return nil, err
		}
		return map[string]any{"networks": nets}, m.set(r.Context(), id, nets)
	})
	return nil
}

type cached struct {
	nets []string
	at   time.Time
}

// Method implements auth.MFAMethod, MFASilent, MFABulk and auth.AccessGuard.
type Method struct {
	host  *sdk.Host
	mu    sync.Mutex
	cache map[int64]cached // networks per user for the request checks (30 s)
}

func (m *Method) enforce() bool { return auth.BoolSetting(m.host.Settings(), "enforce_always", true) }

func (m *Method) networks(ctx context.Context, uid int64) ([]string, error) {
	m.mu.Lock()
	if c, ok := m.cache[uid]; ok && time.Since(c.at) < 30*time.Second {
		m.mu.Unlock()
		return c.nets, nil
	}
	m.mu.Unlock()
	nets := []string{}
	err := m.host.DB.QueryRow(ctx, `SELECT networks FROM plugin_mfa_network WHERE user_id=$1`, uid).Scan(&nets)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	m.mu.Lock()
	if len(m.cache) > 100000 {
		m.cache = map[int64]cached{}
	}
	m.cache[uid] = cached{nets, time.Now()}
	m.mu.Unlock()
	return nets, nil
}

func (m *Method) set(ctx context.Context, uid int64, nets []string) error {
	before, _ := m.networks(ctx, uid)
	var err error
	if len(nets) == 0 {
		_, err = m.host.DB.Exec(ctx, `DELETE FROM plugin_mfa_network WHERE user_id=$1`, uid)
	} else {
		_, err = m.host.DB.Exec(ctx, `INSERT INTO plugin_mfa_network(user_id, networks) VALUES ($1,$2)
			ON CONFLICT (user_id) DO UPDATE SET networks=EXCLUDED.networks`, uid, nets)
	}
	if err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.cache, uid)
	m.mu.Unlock()
	return audit.Log(ctx, m.host.DB, "user.networks", "user", uid, map[string]any{"networks": before}, map[string]any{"networks": nets})
}

// ID implements auth.MFAMethod.
func (m *Method) ID() string { return "network" }

// DefaultFactor implements auth.MFAMethod.
func (m *Method) DefaultFactor() int { return 2 }

// Enrolled implements auth.MFAMethod: the person has networks.
func (m *Method) Enrolled(ctx context.Context, uid int64) bool {
	nets, err := m.networks(ctx, uid)
	return err == nil && len(nets) > 0
}

// EnrolledAmong implements auth.MFABulk.
func (m *Method) EnrolledAmong(ctx context.Context, ids []int64) map[int64]bool {
	out := map[int64]bool{}
	rows, err := m.host.DB.Query(ctx, `SELECT user_id FROM plugin_mfa_network WHERE user_id = ANY($1)`, ids)
	if err != nil {
		return out
	}
	list, _ := pgx.CollectRows(rows, pgx.RowTo[int64])
	for _, id := range list {
		out[id] = true
	}
	return out
}

// Passes implements auth.MFASilent.
func (m *Method) Passes(ctx context.Context, uid int64, ip string) bool {
	nets, err := m.networks(ctx, uid)
	return err == nil && len(nets) > 0 && IPAllowed(nets, ip)
}

// Challenge implements auth.MFAMethod (never asked: silent factor).
func (m *Method) Challenge(context.Context, int64) (any, string, error) {
	return nil, "", apperr.BadRequest("this factor needs no answer")
}

// Verify implements auth.MFAMethod (never asked: silent factor).
func (m *Method) Verify(context.Context, int64, string, json.RawMessage) error {
	return auth.ErrMFAInvalid
}

// CheckSignIn implements auth.AccessGuard (with enforce_always).
func (m *Method) CheckSignIn(ctx context.Context, uid int64, ip string) error {
	if !m.enforce() {
		return nil
	}
	nets, err := m.networks(ctx, uid)
	if err != nil {
		return err
	}
	if !IPAllowed(nets, ip) {
		return auth.ErrNetworkDenied
	}
	return nil
}

// CheckRequest implements auth.AccessGuard (with enforce_always):
// sessions, API keys, SSH tokens and feed links of the account.
func (m *Method) CheckRequest(ctx context.Context, p *identity.Principal) error {
	if p.UserID == 0 || !m.enforce() {
		return nil
	}
	nets, err := m.networks(ctx, p.UserID)
	if err != nil {
		return err
	}
	if !IPAllowed(nets, audit.ClientIP(ctx)) {
		return auth.ErrNetworkDenied
	}
	return nil
}

// ParseNetworks validates a list of IP addresses and CIDR networks and
// returns them normalized (10.0.0.0/8, 192.0.2.7/32, 2001:db8::/32).
func ParseNetworks(list []string) ([]string, error) {
	out := []string{}
	for _, n := range list {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		var p netip.Prefix
		if strings.Contains(n, "/") {
			var err error
			if p, err = netip.ParsePrefix(n); err != nil {
				return nil, apperr.BadRequest("invalid network %q (use e.g. 10.0.0.0/8 or 2001:db8::/32)", n)
			}
		} else {
			a, err := netip.ParseAddr(n)
			if err != nil {
				return nil, apperr.BadRequest("invalid IP address %q", n)
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		if s := p.Masked().String(); !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	if len(out) > 50 {
		return nil, apperr.BadRequest("at most 50 networks")
	}
	return out, nil
}

// IPAllowed reports whether ip lies in one of the networks (no networks:
// everywhere). Unknown or invalid addresses are not allowed when networks
// are set.
func IPAllowed(networks []string, ip string) bool {
	if len(networks) == 0 {
		return true
	}
	a, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, n := range networks {
		if p, err := netip.ParsePrefix(n); err == nil && p.Contains(a) {
			return true
		}
	}
	return false
}
