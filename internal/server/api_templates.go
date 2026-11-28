package server

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/tplsettings"
)

// routesTemplates registers what the administration of the templates needs
// from the core beyond the templates themselves (Administration →
// Templates): which template is used where, and the settings of each
// template (colours, fonts, logos …), checked against the description the
// template declared.
func (s *Server) routesTemplates() {
	// GET /admin/templates/usage → the template of the site and, per
	// template, how many categories choose it themselves. Counts only: no
	// names of categories, so nothing beyond the right of the page is shown.
	// action: core.templates.usage
	s.handle("GET /api/v1/admin/templates/usage", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		counts, err := s.app.Content.TemplateUsage(r.Context())
		if err != nil {
			return nil, err
		}
		return map[string]any{"site": s.app.Config.Get().UI.Template, "categories": counts}, nil
	})

	// GET /admin/templates/{id}/settings → the description of the settings
	// of a template, the values stored (changed from the defaults) and the
	// effective values.
	// action: core.templates.settings.get
	s.handle("GET /api/v1/admin/templates/{id}/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		t, err := s.template(r.Context(), r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		stored := s.app.Config.Get().TemplateSettings[r.PathValue("id")]
		return map[string]any{"settings": t.settings, "values": tplsettings.Effective(t.settings, stored), "changed": changedKeys(t.settings, stored)}, nil
	})

	// PUT /admin/templates/{id}/settings {values: {key: value or null}} →
	// changes the values given (null: back to the default). Every value is
	// checked against the description; images must be uploaded PNG, WebP or
	// JPEG files (their size is read from the file).
	// action: core.templates.settings.update
	s.handle("PUT /api/v1/admin/templates/{id}/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id := r.PathValue("id")
		t, err := s.template(r.Context(), id)
		if err != nil {
			return nil, err
		}
		var in struct {
			Values map[string]any `json:"values"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		checked, err := tplsettings.Check(t.settings, in.Values, s.uploadedImage(r))
		if err != nil {
			return nil, apperr.BadRequest("%v", err)
		}
		return s.storeTemplateSettings(r, id, t, func(m map[string]any) { applyChanges(m, in.Values, checked) })
	})

	// DELETE /admin/templates/{id}/settings → all settings of the template
	// back to their defaults (204).
	// action: core.templates.settings.reset
	s.handle("DELETE /api/v1/admin/templates/{id}/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id := r.PathValue("id")
		t, err := s.template(r.Context(), id)
		if err != nil {
			return nil, err
		}
		_, err = s.storeTemplateSettings(r, id, t, func(m map[string]any) { clear(m) })
		return nil, err
	})

	// GET /templates/{id}/settings → the effective values of a template
	// (public: the look of the site is no secret, and every visitor's page
	// is built with them). Images as their address and size.
	// action: core.templates.settings.effective
	s.handle("GET /api/v1/templates/{id}/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id := r.PathValue("id")
		t, err := s.template(r.Context(), id)
		if err != nil {
			return nil, err
		}
		return publicTemplateSettings(t, s.app.Config.Get().TemplateSettings[id]), nil
	})

	// POST /admin/templates/{id}/settings/preview {values} → the effective
	// values and the look of a draft (the stored values with the changes
	// given), checked like a save but not stored: the live preview of the
	// form shows exactly what saving would give. A value of the wrong kind
	// is refused (400); a draft the template refuses as a whole comes back
	// with "problem", the text saving would answer.
	// action: core.templates.settings.preview
	s.handle("POST /api/v1/admin/templates/{id}/settings/preview", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id := r.PathValue("id")
		t, err := s.template(r.Context(), id)
		if err != nil {
			return nil, err
		}
		var in struct {
			Values map[string]any `json:"values"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		checked, err := tplsettings.Check(t.settings, in.Values, s.uploadedImage(r))
		if err != nil {
			return nil, apperr.BadRequest("%v", err)
		}
		draft := maps.Clone(s.app.Config.Get().TemplateSettings[id])
		if draft == nil {
			draft = map[string]any{}
		}
		applyChanges(draft, in.Values, checked)
		out := publicTemplateSettings(t, draft)
		// a draft whose values are each fine but together are not (a template
		// of data: too little contrast, no room) is the normal case while
		// someone tries colours: the preview says why saving would fail
		// (saving answers 400 with the same text)
		if err := t.checkValues(r.Context(), draft); err != nil {
			if status, _, _ := apperr.Status(err); status != http.StatusBadRequest {
				return nil, err
			}
			out["problem"] = apperr.Localized(err, apperr.RequestLanguage(r))
		}
		return out, nil
	})
}

// tplInfo is what the server needs to know of a template for its settings.
type tplInfo struct {
	settings []tplsettings.Setting
	// root of the CSS variables (Manifest.TemplateLookRoot)
	root string
	// check tests the effective values before they are stored (templates
	// of data: the design with these values must still pass all its checks,
	// such as the contrast of every pair of colours); nil: none
	check func(ctx context.Context, values map[string]any) error
}

// checkValues tests the effective values of stored (the defaults and the
// values changed) with the check of the template, if it has one.
func (t tplInfo) checkValues(ctx context.Context, stored map[string]any) error {
	if t.check == nil {
		return nil
	}
	if err := t.check(ctx, tplsettings.Effective(t.settings, stored)); err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return err
		}
		return apperr.BadRequest("%v", err)
	}
	return nil
}

// template finds a template that can be used now: a switched on template
// plugin or a template a switched on plugin offers (templates of data,
// whose settings come from their design). A variant that declares no settings uses the description of the
// template it extends; its values stay its own (each template has its own
// settings). Only the manifests needed are read: this runs for every /site.
func (s *Server) template(ctx context.Context, id string) (tplInfo, error) {
	find := func(id string) (coreplugin.Manifest, bool) {
		p, ok := s.app.Kernel.Get(id)
		if !ok || !s.app.Kernel.Enabled(id) {
			return coreplugin.Manifest{}, false
		}
		m := p.Manifest()
		return m, slices.Contains(m.Kinds, coreplugin.KindTemplate)
	}
	m, ok := find(id)
	if !ok {
		if desc, check, ok := s.app.Content.TemplateSettings(ctx, id); ok {
			return tplInfo{settings: desc, check: check}, nil
		}
		if s.app.Content.TemplateOK(ctx, id) {
			return tplInfo{}, nil
		}
		return tplInfo{}, apperr.NotFound("template")
	}
	t := tplInfo{settings: m.TemplateSettings, root: m.TemplateLookRoot}
	if len(t.settings) == 0 {
		for _, base := range m.Extends {
			if b, ok := find(base); ok && len(b.TemplateSettings) > 0 {
				t.settings = b.TemplateSettings
				if t.root == "" {
					t.root = b.TemplateLookRoot
				}
				break
			}
		}
	}
	return t, nil
}

// siteTemplateSettings are the effective settings of the template of the
// site (sent with /site); empty for a template without settings.
func (s *Server) siteTemplateSettings(ctx context.Context, c *config.Config) map[string]any {
	t, err := s.template(ctx, c.UI.Template)
	if err != nil {
		return map[string]any{"values": map[string]any{}}
	}
	return publicTemplateSettings(t, c.TemplateSettings[c.UI.Template])
}

// storeTemplateSettings changes the stored values of a template with
// change, records the change in the audit log and returns the new state.
func (s *Server) storeTemplateSettings(r *http.Request, id string, t tplInfo, change func(map[string]any)) (any, error) {
	var before, after map[string]any
	err := s.app.Config.Update(func(c *config.Config) error {
		m := maps.Clone(c.TemplateSettings[id])
		if m == nil {
			m = map[string]any{}
		}
		before = maps.Clone(m)
		change(m)
		if err := t.checkValues(r.Context(), m); err != nil {
			return err
		}
		after = maps.Clone(m)
		if c.TemplateSettings == nil {
			c.TemplateSettings = map[string]map[string]any{}
		}
		if len(m) == 0 {
			delete(c.TemplateSettings, id)
		} else {
			c.TemplateSettings[id] = m
		}
		if len(c.TemplateSettings) == 0 {
			c.TemplateSettings = nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	_ = audit.Log(r.Context(), s.app.DB, "settings.template", "template:"+id, 0, before, after)
	return map[string]any{"settings": t.settings, "values": tplsettings.Effective(t.settings, after), "changed": changedKeys(t.settings, after)}, nil
}

// uploadedImage reads the format and size of uploaded files for the checks
// of the settings of a request.
func (s *Server) uploadedImage(r *http.Request) tplsettings.ImageInfo {
	return func(hash string) (string, int, int, error) { return s.app.Files.ImageInfo(r.Context(), hash) }
}

// applyChanges puts the checked values into the stored ones; a value sent
// as null removes the stored one (back to the default).
func applyChanges(stored, sent, checked map[string]any) {
	for k, v := range sent {
		if c, ok := checked[k]; ok && v != nil {
			stored[k] = c
		} else {
			delete(stored, k)
		}
	}
}

// changedKeys lists the settings that have a stored value (shown as changed,
// with "reset" next to them).
func changedKeys(desc []tplsettings.Setting, stored map[string]any) []string {
	out := []string{}
	for _, d := range desc {
		if _, ok := stored[d.Key]; ok {
			out = append(out, d.Key)
		}
	}
	return out
}

// publicTemplateSettings are the effective values as a page needs them
// (images as their address and size) and the look: the CSS variables of
// the values changed from their defaults, with the element they belong to.
func publicTemplateSettings(t tplInfo, stored map[string]any) map[string]any {
	values := tplsettings.Effective(t.settings, stored)
	for k, v := range values {
		if img, ok := v.(tplsettings.Image); ok {
			values[k] = map[string]any{"src": "/api/v1/files/" + img.Hash, "width": img.Width, "height": img.Height}
		}
	}
	return map[string]any{"values": values, "look": tplsettings.LookOf(t.settings, stored), "root": t.root}
}
