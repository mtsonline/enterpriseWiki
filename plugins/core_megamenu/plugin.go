// Package core_megamenu adds the menu style "mega" to core.menu: in
// horizontal positions each top entry opens a wide panel with its entries
// in columns. The plugin is frontend only; menus choose the style in the
// menu administration (Display).
package core_megamenu

import (
	"embed"
	"io/fs"

	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Plugin is the mega menu plugin.
type Plugin struct{}

func init() { coreplugin.Register(&Plugin{}) }

// Manifest implements coreplugin.Plugin.
func (p *Plugin) Manifest() coreplugin.Manifest { return coreplugin.ParseManifest(manifest) }

// Locales implements coreplugin.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }
