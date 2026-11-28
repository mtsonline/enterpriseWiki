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
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/i18n"
	"github.com/mtsonline/enterprisewiki/internal/limits"
)

// Kernel owns the plugins.
type Kernel struct {
	cfg     *config.Store
	log     *slog.Logger
	plugins map[string]coreplugin.Plugin
	// manifests are those of the plugins, read once: a manifest never
	// changes, and reading it (JSON) for every request made /site slow
	manifests map[string]coreplugin.Manifest
	active    atomic.Pointer[map[string]bool]
	site      string
}

// sorted returns the plugins of this kernel ordered by id.
func (k *Kernel) sorted() []coreplugin.Plugin {
	ids := make([]string, 0, len(k.plugins))
	for id := range k.plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]coreplugin.Plugin, len(ids))
	for i, id := range ids {
		out[i] = k.plugins[id]
	}
	return out
}

// New creates a kernel for all plugins registered via coreplugin.Register
// (site "main").
func New(cfg *config.Store, log *slog.Logger) *Kernel {
	return NewSite(cfg, log, "main")
}

// NewSite creates the kernel of a site with fresh plugin instances
// (coreplugin.NewInstance): every app has its own, the registered ones are
// only templates. So two apps never share a plugin instance – not two
// sites, and not two apps of the same site in one process (tests, a
// command line inside a server process).
func NewSite(cfg *config.Store, log *slog.Logger, site string) *Kernel {
	k := &Kernel{cfg: cfg, log: log, plugins: map[string]coreplugin.Plugin{}, manifests: map[string]coreplugin.Manifest{}, site: site}
	for _, p := range coreplugin.Registered() {
		p = coreplugin.NewInstance(p)
		m := p.Manifest()
		k.plugins[m.ID] = p
		k.manifests[m.ID] = m
	}
	k.recompute(cfg.Get())
	cfg.OnChange(k.recompute)
	return k
}

func (k *Kernel) recompute(c *config.Config) {
	active := map[string]bool{}
	var visit func(id string, seen map[string]bool) bool
	visit = func(id string, seen map[string]bool) bool {
		if v, ok := active[id]; ok {
			return v
		}
		_, ok := k.plugins[id]
		if !ok || seen[id] {
			return false
		}
		seen[id] = true
		m := k.manifests[id]
		ok = switchedOn(c, m)
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
	_, ok := k.plugins[id]
	return ok && k.Enabled(id) && slices.Contains(k.manifests[id].Kinds, coreplugin.KindTemplate)
}

// Get returns a compiled plugin.
func (k *Kernel) Get(id string) (coreplugin.Plugin, bool) {
	p, ok := k.plugins[id]
	return p, ok
}

// Manifest is the manifest of plugin id (read once when the kernel was made).
func (k *Kernel) Manifest(id string) (coreplugin.Manifest, bool) {
	m, ok := k.manifests[id]
	return m, ok
}

// Info is the admin view of a plugin.
type Info struct {
	coreplugin.Manifest
	Enabled    bool     `json:"enabled"`    // switched on in the configuration
	Active     bool     `json:"active"`     // enabled and all dependencies active
	Missing    []string `json:"missing"`    // extended plugins that are not installed
	Dependents []string `json:"dependents"` // plugins extending this one
}

// switchedOn reports whether the configuration switches plugin m on:
// required plugins always, plugins that are off by default only when listed
// in plugins.enabled, all others unless listed in plugins.disabled.
func switchedOn(c *config.Config, m coreplugin.Manifest) bool {
	switch {
	case m.Required:
		return true
	case m.DefaultOff:
		return slices.Contains(c.Plugins.Enabled, m.ID) && !slices.Contains(c.Plugins.Disabled, m.ID)
	}
	return !slices.Contains(c.Plugins.Disabled, m.ID)
}

// List returns all compiled plugins.
func (k *Kernel) List() []Info {
	c := k.cfg.Get()
	out := []Info{}
	for id := range k.plugins {
		m := k.manifests[id]
		inf := Info{Manifest: m, Enabled: switchedOn(c, m), Active: k.Enabled(id), Missing: []string{}, Dependents: []string{}}
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
		for oid, o := range k.manifests {
			if slices.Contains(o.Extends, id) {
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
	_, ok := k.plugins[id]
	if !ok {
		return apperr.NotFound("plugin " + id)
	}
	if !on && k.manifests[id].Required {
		return apperr.BadRequest("plugin %s is required and cannot be disabled", id)
	}
	if !on {
		var deps []string
		for oid, o := range k.manifests {
			if k.Enabled(oid) && slices.Contains(o.Extends, id) {
				deps = append(deps, oid)
			}
		}
		if len(deps) > 0 {
			sort.Strings(deps)
			return apperr.Conflict("disable the plugins that extend %s first: %v", id, deps)
		}
	}
	defaultOff := k.manifests[id].DefaultOff
	return k.cfg.Update(func(c *config.Config) error {
		c.Plugins.Disabled = slices.DeleteFunc(c.Plugins.Disabled, func(s string) bool { return s == id })
		c.Plugins.Enabled = slices.DeleteFunc(c.Plugins.Enabled, func(s string) bool { return s == id })
		switch {
		case defaultOff && on:
			c.Plugins.Enabled = append(c.Plugins.Enabled, id)
			sort.Strings(c.Plugins.Enabled)
		case !defaultOff && !on:
			c.Plugins.Disabled = append(c.Plugins.Disabled, id)
			sort.Strings(c.Plugins.Disabled)
		}
		if c.Plugins.Disabled == nil {
			c.Plugins.Disabled = []string{}
		}
		if len(c.Plugins.Enabled) == 0 {
			c.Plugins.Enabled = nil
		}
		return nil
	})
}

// Migrations returns the migration sources of all compiled plugins.
func (k *Kernel) Migrations() []db.MigrationSource {
	var out []db.MigrationSource
	for _, p := range k.sorted() {
		if m, ok := p.(coreplugin.MigrationsPlugin); ok {
			out = append(out, db.MigrationSource{Name: "plugin:" + p.Manifest().ID, Files: m.Migrations()})
		}
	}
	return out
}

// Setup calls Setup on every compiled plugin and loads their locales.
func (k *Kernel) Setup(base coreplugin.Host, tr *i18n.Service) error {
	base.Site = k.site
	for _, p := range k.sorted() {
		m := p.Manifest()
		if err := perm.Register(m.ID, m.Permissions...); err != nil {
			return fmt.Errorf("plugin %s: %w", m.ID, err)
		}
		for _, d := range m.Limits {
			if err := limits.Register(m.ID, d); err != nil {
				return fmt.Errorf("plugin %s: %w", m.ID, err)
			}
		}
		if l, ok := p.(coreplugin.LocalesPlugin); ok {
			if err := tr.Register(m.ID, l.Locales()); err != nil {
				return fmt.Errorf("plugin %s locales: %w", m.ID, err)
			}
		}
		if err := tr.RegisterInstanceLabels(m.ID, m.InstanceLabels); err != nil {
			return fmt.Errorf("plugin %s: %w", m.ID, err)
		}
		if s, ok := p.(coreplugin.SetupPlugin); ok {
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
