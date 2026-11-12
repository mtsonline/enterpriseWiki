package server

import (
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
)

// LimitInfo is a technical limit with its current value (Administration →
// Expert settings).
type LimitInfo struct {
	limits.Def
	Value   int64 `json:"value"`
	Changed bool  `json:"changed"` // differs from the default
	// Active is false for limits of plugins that are switched off.
	Active bool `json:"active"`
}

// routesLimits registers the expert settings: all technical limits of the
// core and the plugins in one place.
func (s *Server) routesLimits() {
	list := func() []LimitInfo {
		c := s.app.Instance.Config.Get()
		out := []LimitInfo{}
		for _, d := range limits.List() {
			v := limits.Value(c, d.Key)
			out = append(out, LimitInfo{Def: d, Value: v, Changed: v != d.Default, Active: d.Owner == "core" || s.app.Kernel.Enabled(d.Owner)})
		}
		return out
	}
	// GET /admin/limits lists the limits with default, range and value.
	s.handle("GET /api/v1/admin/limits", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return map[string]any{"limits": list(), "editable": s.limitsEditable(r)}, nil
	})
	// PUT /admin/limits {"values": {"core.request.guest_kb": 128, "core.request.body_kb": null}}
	// changes limits (null: back to the default). They apply to the whole
	// instance, so only its administrators change them.
	s.handle("PUT /api/v1/admin/limits", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		if !s.limitsEditable(r) {
			return nil, apperr.Forbidden("the limits apply to the whole instance: only its administrators change them")
		}
		var in struct {
			Values map[string]*int64 `json:"values"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		for k, v := range in.Values {
			if _, ok := limits.Get(k); !ok {
				return nil, apperr.BadRequest("unknown limit %q", k)
			}
			if v != nil {
				if err := limits.Check(k, *v); err != nil {
					return nil, apperr.BadRequest("%v", err)
				}
			}
		}
		before, after := map[string]int64{}, map[string]int64{}
		err := s.app.Instance.Config.Update(func(c *config.Config) error {
			for k, v := range in.Values {
				d, _ := limits.Get(k)
				before[k] = limits.Value(c, k)
				if v == nil || *v == d.Default {
					delete(c.Limits, k)
					after[k] = d.Default
					continue
				}
				if c.Limits == nil {
					c.Limits = map[string]int64{}
				}
				c.Limits[k], after[k] = *v, *v
			}
			if len(c.Limits) == 0 {
				c.Limits = nil
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		for k := range before {
			if before[k] == after[k] {
				delete(before, k)
				delete(after, k)
			}
		}
		if len(after) > 0 {
			_ = audit.Log(r.Context(), s.app.DB, "settings.limits", "settings", 0, before, after)
		}
		return map[string]any{"limits": list(), "editable": true}, nil
	})
}

// limitsEditable reports whether the caller may change the limits: an
// administrator of the instance (on the first site: everybody with the
// right to change settings).
func (s *Server) limitsEditable(r *http.Request) bool {
	p := identity.From(r.Context())
	return p != nil && (s.app.Site == "main" || p.InstanceAdmin)
}
