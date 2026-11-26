package server

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
)

// routesPlugins registers the routes of plugins: list, switch on and off, install, update, remove.
func (s *Server) routesPlugins() {
	// action: core.plugins.list
	s.handle("GET /api/v1/admin/plugins", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return map[string]any{"installed": s.app.Kernel.List(), "origins": s.plugins.Origins(), "catalog": s.app.Config.Get().Plugins.Catalog,
			"pending": s.plugins.Pending(), "build": s.plugins.BuildStatus(), "can_build": s.plugins.CanBuild(),
			"build_unavailable": s.plugins.BuildUnavailable(), "mode": s.plugins.Mode(), "plugins_dir": s.plugins.PluginsDir(),
			"restart_pending": s.plugins.RestartPending(), "auto_restart_on_drop": s.app.Config.Get().Plugins.AutoRestartOnDrop,
			"cluster": s.app.Config.Get().Mode == "cluster", "removable": s.removablePlugins()}, nil
	})
	// action: core.plugins.sources.check (origin level of a source before installing)
	s.handle("POST /api/v1/admin/plugins/origin", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		var in struct {
			Source string `json:"source"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.plugins.CheckSource(in.Source)
	})
	// action: core.plugins.sources.list
	s.handle("GET /api/v1/admin/plugins/sources", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		c := s.app.Config.Get().Plugins
		list := c.TrustedSources
		if list == nil {
			list = []config.TrustedSource{}
		}
		return map[string]any{"sources": list, "allow_external": c.ExternalAllowed()}, nil
	})
	// action: core.plugins.sources.add (critical: needs a recent sign-in)
	s.handle("POST /api/v1/admin/plugins/sources", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		if err := requireRecentSignIn(r); err != nil {
			return nil, err
		}
		var in config.TrustedSource
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		norm, err := pluginmgr.NormalizeTrusted(in.Prefix)
		if err != nil {
			return nil, apperr.BadRequest("%s", err.Error())
		}
		if err := s.app.Config.Base().Update(func(c *config.Config) error {
			return pluginmgr.SetTrustedSources(c, append(slices.Clone(c.Plugins.TrustedSources), config.TrustedSource{Prefix: norm, Note: in.Note}))
		}); err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.sources.add", norm)
		return h.Created{V: config.TrustedSource{Prefix: norm, Note: strings.TrimSpace(in.Note)}}, nil
	})
	// action: core.plugins.sources.remove
	s.handle("DELETE /api/v1/admin/plugins/sources", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		prefix := r.URL.Query().Get("prefix")
		found := false
		if err := s.app.Config.Base().Update(func(c *config.Config) error {
			c.Plugins.TrustedSources = slices.DeleteFunc(slices.Clone(c.Plugins.TrustedSources), func(t config.TrustedSource) bool {
				if t.Prefix == prefix {
					found = true
				}
				return t.Prefix == prefix
			})
			return nil
		}); err != nil {
			return nil, err
		}
		if !found {
			return nil, apperr.NotFound("trusted source")
		}
		s.auditSettings(r, "plugin.sources.remove", prefix)
		return h.NoContent{}, nil
	})
	// Access data of plugin sources (private repositories). The token is
	// written once and never shown again (neither API nor audit log).
	// action: core.plugins.credentials.list
	s.handle("GET /api/v1/admin/plugins/credentials", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		out := []pluginmgr.CredentialView{}
		for _, c := range s.app.Config.Base().Raw().Plugins.Credentials {
			out = append(out, pluginmgr.ViewCredential(c))
		}
		return map[string]any{"credentials": out}, nil
	})
	// action: core.plugins.credentials.set (critical: needs a recent sign-in)
	s.handle("PUT /api/v1/admin/plugins/credentials", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		if err := requireRecentSignIn(r); err != nil {
			return nil, err
		}
		var in struct {
			Prefix   string `json:"prefix"`
			Username string `json:"username"`
			Token    string `json:"token"`
			Note     string `json:"note"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		var saved config.SourceCredential
		if err := s.app.Config.Base().Update(func(c *config.Config) error {
			var err error
			saved, err = pluginmgr.SetCredential(c, config.SourceCredential{Prefix: in.Prefix, Username: in.Username, Token: in.Token, Note: in.Note})
			return err
		}); err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.credentials.set", saved.Prefix)
		return pluginmgr.ViewCredential(saved), nil
	})
	// action: core.plugins.credentials.remove
	s.handle("DELETE /api/v1/admin/plugins/credentials", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		prefix := r.URL.Query().Get("prefix")
		found := false
		if err := s.app.Config.Base().Update(func(c *config.Config) error {
			c.Plugins.Credentials = slices.DeleteFunc(slices.Clone(c.Plugins.Credentials), func(x config.SourceCredential) bool {
				if x.Prefix == prefix {
					found = true
				}
				return x.Prefix == prefix
			})
			return nil
		}); err != nil {
			return nil, err
		}
		if !found {
			return nil, apperr.NotFound("access data")
		}
		s.auditSettings(r, "plugin.credentials.remove", prefix)
		return h.NoContent{}, nil
	})
	// action: core.plugins.sources.external (allow or forbid external core
	// plugins; allowing needs a recent sign-in)
	s.handle("PUT /api/v1/admin/plugins/sources/external", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil {
			return nil, err
		}
		var in struct {
			Allow bool `json:"allow"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if in.Allow {
			if err := requireRecentSignIn(r); err != nil {
				return nil, err
			}
		}
		if err := s.app.Config.Base().Update(func(c *config.Config) error { c.Plugins.AllowExternal = &in.Allow; return nil }); err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.sources.external", fmt.Sprintf("allow=%v", in.Allow))
		return in, nil
	})
	// action: core.plugins.options
	s.handle("PUT /api/v1/admin/plugins/options", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // installed plugins and the process are shared by all sites
			return nil, err
		}
		var in struct {
			AutoRestartOnDrop bool `json:"auto_restart_on_drop"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := s.app.Config.Base().Update(func(c *config.Config) error { c.Plugins.AutoRestartOnDrop = in.AutoRestartOnDrop; return nil }); err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.options", fmt.Sprintf("auto_restart_on_drop=%v", in.AutoRestartOnDrop))
		return in, nil
	})
	// action: core.plugins.enable
	s.handle("POST /api/v1/admin/plugins/{id}/enable", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		err := s.app.Kernel.SetEnabled(r.Context(), r.PathValue("id"), true)
		s.auditSettings(r, "plugin.enable", r.PathValue("id"))
		return h.NoContent{}, err
	})
	// action: core.plugins.disable
	s.handle("POST /api/v1/admin/plugins/{id}/disable", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		err := s.app.Kernel.SetEnabled(r.Context(), r.PathValue("id"), false)
		s.auditSettings(r, "plugin.disable", r.PathValue("id"))
		return h.NoContent{}, err
	})
	// action: core.plugins.install
	s.handle("POST /api/v1/admin/plugins/install", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // installed plugins and the process are shared by all sites
			return nil, err
		}
		var in struct {
			Source string `json:"source"` // git URL, github.com/owner/repo, catalog id or local directory
			Ref    string `json:"ref"`
			// AcceptExternal confirms the warning for a source that is
			// neither official nor trusted (needed for every installation).
			AcceptExternal bool `json:"accept_external"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		res, err := s.plugins.Install(r.Context(), in.Source, in.Ref, pluginmgr.InstallOptions{AcceptExternal: in.AcceptExternal, By: handleOf(r)})
		if err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.install", fmt.Sprintf("%s level=%s source=%s commit=%s accepted=%v",
			res.ID, res.Origin.Level, res.Origin.Source, res.Origin.Commit, res.Origin.Accepted))
		return s.buildAfterChange(r.Context(), res, "install"), nil
	})
	// action: core.plugins.remove
	s.handle("DELETE /api/v1/admin/plugins/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // installed plugins and the process are shared by all sites
			return nil, err
		}
		if err := s.plugins.Remove(r.Context(), r.PathValue("id")); err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.remove", r.PathValue("id"))
		return s.buildAfterChange(r.Context(), nil, "remove"), nil
	})
	// action: core.plugins.build
	s.handle("POST /api/v1/admin/plugins/build", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // installed plugins and the process are shared by all sites
			return nil, err
		}
		st, err := s.plugins.StartBuild(r.Context(), "manual")
		return h.Accepted{V: st}, err
	})
	// action: core.plugins.restart
	s.handle("POST /api/v1/admin/restart", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // installed plugins and the process are shared by all sites
			return nil, err
		}
		if s.app.Config.Get().Mode != "cluster" {
			if err := s.plugins.ScheduleRestart(); err != nil {
				return nil, err
			}
		} else {
			s.plugins.RestartAll(r.Context()) // one node after the other
		}
		s.auditSettings(r, "system.restart", "")
		return h.Accepted{V: map[string]string{"status": "restarting"}}, nil
	})
}

// buildAfterChange starts the build after an installation or removal; the
// server restarts automatically when it succeeded.
func (s *Server) buildAfterChange(ctx context.Context, res *pluginmgr.InstallResult, trigger string) map[string]any {
	out := map[string]any{"building": false}
	if res != nil {
		out["id"], out["name"], out["version"], out["dir"], out["upgrade"] = res.ID, res.Name, res.Version, res.Dir, res.Upgrade
		out["origin"] = res.Origin
	}
	if !s.plugins.CanBuild() {
		out["rebuild_required"] = true
		return out
	}
	if _, err := s.plugins.StartBuild(ctx, trigger); err != nil {
		out["rebuild_required"], out["error"] = true, err.Error()
		return out
	}
	out["building"] = true
	return out
}

// removablePlugins lists plugins whose source can be removed (Docker images:
// only the additional ones in the data directory).
func (s *Server) removablePlugins() []string {
	out := []string{}
	disk, dirs, err := s.plugins.OnDisk()
	if err != nil {
		return out
	}
	for id := range dirs {
		if mf := disk[id]; mf != nil && !mf.Required {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
