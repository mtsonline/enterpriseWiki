// Package theme_cms_modern registers the "theme.cms.modern" template (neutral, modern website look). It is
// implemented in web/index.ts (frontend).
package theme_cms_modern

import (
	_ "embed"

	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
)

//go:embed plugin.json
var manifest []byte

// Plugin is the template plugin.
type Plugin struct{}

func init() { coreplugin.Register(&Plugin{}) }

// Manifest implements coreplugin.Plugin.
func (p *Plugin) Manifest() coreplugin.Manifest { return coreplugin.ParseManifest(manifest) }
