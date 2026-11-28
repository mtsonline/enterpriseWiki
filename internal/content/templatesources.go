package content

import (
	"context"
	"slices"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/tplsettings"
)

// TemplateName is a website template a plugin offers besides the template
// plugins themselves (e.g. templates of data, core.template/12).
type TemplateName struct {
	ID    string            `json:"id"`
	Name  string            `json:"name"`
	Names map[string]string `json:"names,omitempty"`
	// ColorScheme: the default scheme when the template has dark colors too
	// (system, light, dark); empty: light colors only
	ColorScheme string `json:"color_scheme,omitempty"`
}

// TemplateSource offers website templates (the ids start with the id of
// the plugin and "/").
type TemplateSource interface {
	// HasTemplate reports whether id is a template that can be used now.
	HasTemplate(ctx context.Context, id string) bool
	// Templates lists the templates that can be used now.
	Templates(ctx context.Context) []TemplateName
	// ColorScheme is the default scheme of the template id ("" = light colors
	// only, or not a template of the source).
	ColorScheme(ctx context.Context, id string) string
}

// TemplateSettingsSource is a TemplateSource whose templates have settings
// (Administration → Templates → Settings), like the templates of code
// declare them in their manifest.
type TemplateSettingsSource interface {
	// TemplateSettings describes the settings of the template id. check,
	// when not nil, tests the effective values (defaults and stored values)
	// before a change is stored; its error says what is wrong. ok is false
	// when id is no template of the source.
	TemplateSettings(ctx context.Context, id string) (desc []tplsettings.Setting, check func(ctx context.Context, values map[string]any) error, ok bool)
}

type templateSource struct {
	plugin string
	src    TemplateSource
}

// AddTemplateSource adds the templates of a plugin (they count only while
// the plugin is switched on).
func (s *Service) AddTemplateSource(pluginID string, src TemplateSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tplSources = append(s.tplSources, templateSource{pluginID, src})
}

func (s *Service) templateSources() []templateSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.tplSources)
}

// TemplateOK reports whether id is a website template that can be used:
// an active template plugin or a template a switched on plugin offers.
func (s *Service) TemplateOK(ctx context.Context, id string) bool {
	if id == "" {
		return false
	}
	if s.templateOK != nil && s.templateOK(id) {
		return true
	}
	for _, t := range s.templateSources() {
		if strings.HasPrefix(id, t.plugin+"/") && s.enabled(t.plugin) && t.src.HasTemplate(ctx, id) {
			return true
		}
	}
	return false
}

// TemplateColorScheme is the default scheme of a template a switched on
// plugin offers ("" = light colors only or unknown).
func (s *Service) TemplateColorScheme(ctx context.Context, id string) string {
	for _, t := range s.templateSources() {
		if strings.HasPrefix(id, t.plugin+"/") && s.enabled(t.plugin) {
			return t.src.ColorScheme(ctx, id)
		}
	}
	return ""
}

// TemplateSettings describes the settings of a template a switched on
// plugin offers (see TemplateSettingsSource); ok is false for an unknown
// template or a source without settings.
func (s *Service) TemplateSettings(ctx context.Context, id string) ([]tplsettings.Setting, func(context.Context, map[string]any) error, bool) {
	for _, t := range s.templateSources() {
		if strings.HasPrefix(id, t.plugin+"/") && s.enabled(t.plugin) {
			if ss, ok := t.src.(TemplateSettingsSource); ok {
				return ss.TemplateSettings(ctx, id)
			}
		}
	}
	return nil, nil, false
}

// ExtraTemplates lists the templates offered by switched on plugins.
func (s *Service) ExtraTemplates(ctx context.Context) []TemplateName {
	out := []TemplateName{}
	for _, t := range s.templateSources() {
		if s.enabled(t.plugin) {
			out = append(out, t.src.Templates(ctx)...)
		}
	}
	return out
}

// maxTemplateUsage bounds the list of TemplateUsage: one row per template
// that categories choose (the templates of data are limited to far fewer).
const maxTemplateUsage = 2000

// TemplateUsage counts, per template, the categories that choose it
// themselves (not inherited). Only counts: the names of the categories are
// not given out here, so no right to see them is needed.
func (s *Service) TemplateUsage(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT template, count(*) FROM categories
		WHERE template <> '' AND deleted_at IS NULL GROUP BY template ORDER BY template LIMIT $1`, maxTemplateUsage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
