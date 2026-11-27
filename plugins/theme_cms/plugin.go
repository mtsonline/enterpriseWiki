// Package theme_cms registers the "theme.cms" website template. The layout is
// implemented in web/index.ts; its settings (logo, colours, start page hero
// …) are declared in plugin.json ("template_settings"), checked and stored
// by the core (Administration → Templates → Settings).
package theme_cms

import (
	"embed"
	"io/fs"

	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Plugin is the template plugin.
type Plugin struct{}

func init() { coreplugin.Register(&Plugin{}) }

// Manifest implements coreplugin.Plugin.
func (p *Plugin) Manifest() coreplugin.Manifest { return coreplugin.ParseManifest(manifest) }

// Locales implements coreplugin.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }
