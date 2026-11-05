// Package core_megamenu adds the menu style "mega" to core.menu: in
// horizontal positions each top entry opens a wide panel with its entries
// in columns. The plugin is frontend only; menus choose the style in the
// menu administration (Display).
package core_megamenu

import (
	"embed"
	"io/fs"

	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Plugin is the mega menu plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }
