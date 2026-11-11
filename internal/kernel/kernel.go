// Package kernel manages the lifecycle and activation state of the compiled
// plugins.
package kernel

import (
	"context"
	"fmt"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"log/slog"
	"slices"
	"sort"
	"sync/atomic"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/i18n"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

// Kernel owns the plugins.
type Kernel struct {
	cfg     *config.Store
	log     *slog.Logger
	plugins map[string]sdk.Plugin
	active  atomic.Pointer[map[string]bool]
	site    string
}

// sorted returns the plugins of this kernel ordered by id.
func (k *Kernel) sorted() []sdk.Plugin {
	out := make([]sdk.Plugin, 0, len(k.plugins))
	for _, p := range k.plugins {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest().ID < out[j].Manifest().ID })
	return out
}

// New creates a kernel for all plugins registered via sdk.Register (site
// "main": the registered instances).
func New(cfg *config.Store, log *slog.Logger) *Kernel {
	return NewSite(cfg, log, "main")
}

// NewSite creates the kernel of a site; further sites get fresh plugin
// instances (sdk.NewInstance).
func NewSite(cfg *config.Store, log *slog.Logger, site string) *Kernel {
	k := &Kernel{cfg: cfg, log: log, plugins: map[string]sdk.Plugin{}, site: site}
	for _, p := range sdk.Registered() {
		if site != "main" {
			p = sdk.NewInstance(p)
		}
		k.plugins[p.Manifest().ID] = p
	}
	k.recompute(cfg.Get())
	cfg.OnChange(k.recompute)
	return k
}

func (k *Kernel) recompute(c *config.Config) {
	disabled := map[string]bool{}
	for _, id := range c.Plugins.Disabled {
		disabled[id] = true
	}
	active := map[string]bool{}
	var visit func(id string, seen map[string]bool) bool
	visit = func(id string, seen map[string]bool) bool {
		if v, ok := active[id]; ok {
			return v
		}
		p, ok := k.plugins[id]
		if !ok || seen[id] {
			return false
		}
		seen[id] = true
		m := p.Manifest()
		ok = m.Required || !disabled[id]
		for _, dep := range m.Extends {
			if !visit(dep, seen) {
				ok = false
			}
		}
		active[id] = ok
		return ok
	}
	for id := range k.plugins {
		visit(id, map[string]bool{})
	}
	k.active.Store(&active)
}

// Enabled reports whether plugin id is compiled in, enabled and all plugins
// it extends are enabled.
func (k *Kernel) Enabled(id string) bool {
	return (*k.active.Load())[id]
}

// IsTemplate reports whether id is an active website template plugin.
func (k *Kernel) IsTemplate(id string) bool {
	p, ok := k.plugins[id]
	return ok && k.Enabled(id) && slices.Contains(p.Manifest().Kinds, sdk.KindTemplate)
}

// Get returns a compiled plugin.
func (k *Kernel) Get(id string) (sdk.Plugin, bool) {
	p, ok := k.plugins[id]
	return p, ok
}

// Info is the admin view of a plugin.
type Info struct {
	sdk.Manifest
	Enabled    bool     `json:"enabled"`    // switched on in the configuration
	Active     bool     `json:"active"`     // enabled and all dependencies active
	Missing    []string `json:"missing"`    // extended plugins that are not installed
	Dependents []string `json:"dependents"` // plugins extending this one
}

// List returns all compiled plugins.
func (k *Kernel) List() []Info {
	disabled := k.cfg.Get().Plugins.Disabled
	out := []Info{}
	for id, p := range k.plugins {
		m := p.Manifest()
		inf := Info{Manifest: m, Enabled: m.Required || !slices.Contains(disabled, id), Active: k.Enabled(id), Missing: []string{}, Dependents: []string{}}
		if inf.Kinds == nil {
			inf.Kinds = []string{}
		}
		if inf.Extends == nil {
			inf.Extends = []string{}
		}
		for _, d := range m.Extends {
			if _, ok := k.plugins[d]; !ok {
				inf.Missing = append(inf.Missing, d)
			}
		}
		for oid, o := range k.plugins {
			if slices.Contains(o.Manifest().Extends, id) {
				inf.Dependents = append(inf.Dependents, oid)
			}
		}
		sort.Strings(inf.Dependents)
		out = append(out, inf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SetEnabled switches a plugin on or off by writing config.yaml.
func (k *Kernel) SetEnabled(_ context.Context, id string, on bool) error {
	p, ok := k.plugins[id]
	if !ok {
		return apperr.NotFound("plugin " + id)
	}
	if !on && p.Manifest().Required {
		return apperr.BadRequest("plugin %s is required and cannot be disabled", id)
	}
	if !on {
		var deps []string
		for oid, o := range k.plugins {
			if k.Enabled(oid) && slices.Contains(o.Manifest().Extends, id) {
				deps = append(deps, oid)
			}
		}
		if len(deps) > 0 {
			sort.Strings(deps)
			return apperr.Conflict("disable the plugins that extend %s first: %v", id, deps)
		}
	}
	return k.cfg.Update(func(c *config.Config) error {
		c.Plugins.Disabled = slices.DeleteFunc(c.Plugins.Disabled, func(s string) bool { return s == id })
		if !on {
			c.Plugins.Disabled = append(c.Plugins.Disabled, id)
			sort.Strings(c.Plugins.Disabled)
		}
		if c.Plugins.Disabled == nil {
			c.Plugins.Disabled = []string{}
		}
		return nil
	})
}

// Migrations returns the migration sources of all compiled plugins.
func (k *Kernel) Migrations() []db.MigrationSource {
	var out []db.MigrationSource
	for _, p := range k.sorted() {
		if m, ok := p.(sdk.MigrationsPlugin); ok {
			out = append(out, db.MigrationSource{Name: "plugin:" + p.Manifest().ID, Files: m.Migrations()})
		}
	}
	return out
}

// Setup calls Setup on every compiled plugin and loads their locales.
func (k *Kernel) Setup(base sdk.Host, tr *i18n.Service) error {
	base.Site = k.site
	for _, p := range k.sorted() {
		m := p.Manifest()
		if err := perm.Register(m.ID, m.Permissions...); err != nil {
			return fmt.Errorf("plugin %s: %w", m.ID, err)
		}
		if l, ok := p.(sdk.LocalesPlugin); ok {
			if err := tr.Register(m.ID, l.Locales()); err != nil {
				return fmt.Errorf("plugin %s locales: %w", m.ID, err)
			}
		}
		if s, ok := p.(sdk.SetupPlugin); ok {
			h := base
			h.Manifest = m
			h.Log = k.log.With("plugin", m.ID)
			if err := s.Setup(&h); err != nil {
				return fmt.Errorf("plugin %s setup: %w", m.ID, err)
			}
		}
	}
	return nil
}
