// Package theme_cms_modern registers the "theme.cms.modern" template (neutral, modern website look). It is
// implemented in web/index.ts (frontend).
package theme_cms_modern

import (
	_ "embed"

	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

// Plugin is the template plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }
