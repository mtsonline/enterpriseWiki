package auth

import (
	"context"
	"slices"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Administration of the factor chain: the factor number of each MFA plugin
// and, per sign-in provider instance, its factors and how many a sign-in
// needs. Stored in plugin_settings: "factor" of the MFA plugin,
// "mfa_methods" and "mfa_required" of the provider (instance).

// MFAProviderConfig is the chain of one sign-in provider instance.
type MFAProviderConfig struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`    // password | redirect
	Methods  []string `json:"methods"` // nil: all factors
	Required int      `json:"required"`
}

// MFAConfigView is the factor chain of the site.
type MFAConfigView struct {
	Factors   []MFAFactor         `json:"factors"`
	Providers []MFAProviderConfig `json:"providers"`
}

// MFAProviderUpdate changes the chain of a provider instance.
type MFAProviderUpdate struct {
	All      bool     `json:"all"`     // all factors (also future ones)
	Methods  []string `json:"methods"` // otherwise exactly these
	Required int      `json:"required"`
}

// MFAConfigUpdate changes factor numbers (by plugin id) and provider chains.
type MFAConfigUpdate struct {
	Factors map[string]int `json:"factors"`
	// CountsTwo: a password-less factor (passkey) counts as two factors
	CountsTwo map[string]bool              `json:"counts_two"`
	Providers map[string]MFAProviderUpdate `json:"providers"`
}

// MFAConfig returns the factor chain (administrators).
func (s *Service) MFAConfig(ctx context.Context) (*MFAConfigView, error) {
	if err := perm.Require(ctx, "core.settings.manage"); err != nil {
		return nil, err
	}
	v := &MFAConfigView{Factors: s.MFAFactors(), Providers: []MFAProviderConfig{}}
	add := func(id, name, kind string) {
		set := s.providerSettings(id)
		pc := MFAProviderConfig{ID: id, Name: name, Kind: kind, Required: 1}
		if m, ok := set["mfa_methods"]; ok && m != nil {
			pc.Methods = StringList(m)
			if pc.Methods == nil {
				pc.Methods = []string{}
			}
		}
		_, pc.Required = s.chainFor(id)
		v.Providers = append(v.Providers, pc)
	}
	for _, p := range s.Providers() {
		add(p.ID(), p.Name(), "password")
	}
	for _, p := range s.RedirectProviders() {
		add(p.ID(), p.Name(), "redirect")
	}
	return v, nil
}

// providerOwner returns the plugin of a provider instance and its base id.
func (s *Service) providerOwner(id string) (pluginID, base string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.providers {
		if r.src == nil && r.p.ID() == id {
			return r.pluginID, id, true
		}
		if r.src != nil {
			list := r.src()
			for _, p := range list {
				if p.ID() == id {
					return r.pluginID, list[0].ID(), true
				}
			}
		}
	}
	for _, r := range s.redirects {
		if r.src == nil && r.p.ID() == id {
			return r.pluginID, id, true
		}
		if r.src != nil {
			list := r.src()
			for _, p := range list {
				if p.ID() == id {
					return r.pluginID, list[0].ID(), true
				}
			}
		}
	}
	return "", "", false
}

// SetMFAConfig changes the factor chain (administrators).
func (s *Service) SetMFAConfig(ctx context.Context, in MFAConfigUpdate) error {
	if err := perm.Require(ctx, "core.settings.manage"); err != nil {
		return err
	}
	factors := s.MFAFactors()
	byPlugin := map[string]MFAFactor{}
	ids := map[string]bool{}
	for _, f := range factors {
		byPlugin[f.Plugin] = f
		ids[f.ID] = true
	}
	numbers := map[string]int{} // factor id -> number after the change
	for _, f := range factors {
		numbers[f.ID] = f.Factor
	}
	for plugin, n := range in.Factors {
		f, ok := byPlugin[plugin]
		if !ok {
			return apperr.BadRequest("%s is not a factor plugin", plugin)
		}
		if n < 1 || n > 9 {
			return apperr.BadRequest("the factor number must be 1 to 9")
		}
		if n == 1 && !f.Passwordless {
			return apperr.BadRequest("%s cannot be factor 1: only methods that sign in by themselves (passkeys) replace the password", f.ID)
		}
		numbers[f.ID] = n
	}
	two := map[string]bool{} // factor id -> counts two after the change
	for _, f := range factors {
		two[f.ID] = f.CountsTwo
	}
	for plugin, v := range in.CountsTwo {
		f, ok := byPlugin[plugin]
		if !ok {
			return apperr.BadRequest("%s is not a factor plugin", plugin)
		}
		if v && !f.Passwordless {
			return apperr.BadRequest("%s cannot count as two factors: only methods that check the person themselves (passkeys)", f.ID)
		}
		two[f.ID] = v
	}
	type target struct {
		id, plugin, base string
		upd              MFAProviderUpdate
	}
	var targets []target
	for id, upd := range in.Providers {
		plugin, base, ok := s.providerOwner(id)
		if !ok {
			return apperr.NotFound("sign-in provider " + id)
		}
		for _, m := range upd.Methods {
			if !ids[m] {
				return apperr.BadRequest("unknown factor %q", m)
			}
		}
		if upd.Required < 1 || upd.Required > 9 {
			return apperr.BadRequest("the number of required factors must be 1 to 9")
		}
		// enough factor numbers for the requirement
		levels := map[int]bool{1: true}
		extra := 0
		for _, f := range factors {
			if f.Enabled && (upd.All || slices.Contains(upd.Methods, f.ID)) {
				levels[numbers[f.ID]] = true
				if two[f.ID] {
					extra = 1
				}
			}
		}
		if upd.Required > len(levels)+extra {
			return apperr.BadRequest("%s: %d factors required, but its chain has only %d", id, upd.Required, len(levels))
		}
		targets = append(targets, target{id, plugin, base, upd})
	}
	err := s.cfg.Update(func(c *config.Config) error {
		if c.Settings == nil {
			c.Settings = map[string]map[string]any{}
		}
		for plugin, n := range in.Factors {
			if c.Settings[plugin] == nil {
				c.Settings[plugin] = map[string]any{}
			}
			c.Settings[plugin]["factor"] = n
		}
		for plugin, v := range in.CountsTwo {
			if c.Settings[plugin] == nil {
				c.Settings[plugin] = map[string]any{}
			}
			c.Settings[plugin]["counts_two"] = v
		}
		for _, t := range targets {
			if c.Settings[t.plugin] == nil {
				c.Settings[t.plugin] = map[string]any{}
			}
			set := c.Settings[t.plugin]
			if t.id != t.base {
				// the entry of the instance (a map inside the list)
				set = InstanceSettings(t.base, t.id, set)
				if set == nil {
					return apperr.NotFound("instance " + t.id)
				}
			}
			if t.upd.All {
				delete(set, "mfa_methods")
			} else {
				list := make([]any, len(t.upd.Methods))
				for i, m := range t.upd.Methods {
					list[i] = m
				}
				set["mfa_methods"] = list
			}
			set["mfa_required"] = t.upd.Required
		}
		return nil
	})
	if err != nil {
		return err
	}
	return audit.Log(ctx, s.pool, "mfa.config", "settings", 0, nil, in)
}
