// Package core_template provides website templates of data and their
// designer (see design.go and web/).
package core_template

import (
	_ "embed"

	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
)

//go:embed plugin.json
var manifest []byte

// Plugin is the template designer.
type Plugin struct{ host *coreplugin.Host }

func init() { coreplugin.Register(&Plugin{}) }

// Manifest implements coreplugin.Plugin.
func (p *Plugin) Manifest() coreplugin.Manifest { return coreplugin.ParseManifest(manifest) }
