package content

import (
	"context"
	"slices"
	"strings"
)

// TemplateName is a website template a plugin offers besides the template
// plugins themselves (e.g. templates of data, core.template/12).
type TemplateName struct {
	ID    string            `json:"id"`
	Name  string            `json:"name"`
	Names map[string]string `json:"names,omitempty"`
}

// TemplateSource offers website templates (the ids start with the id of
// the plugin and "/").
type TemplateSource interface {
	// HasTemplate reports whether id is a template that can be used now.
	HasTemplate(ctx context.Context, id string) bool
	// Templates lists the templates that can be used now.
	Templates(ctx context.Context) []TemplateName
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
