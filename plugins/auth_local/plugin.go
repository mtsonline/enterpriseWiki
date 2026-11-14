// Package auth_local registers the local username/password provider.
package auth_local

import (
	_ "embed"

	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
)

//go:embed plugin.json
var manifest []byte

// Plugin is the local authentication plugin.
type Plugin struct{}

func init() { coreplugin.Register(&Plugin{}) }

// Manifest implements coreplugin.Plugin.
func (p *Plugin) Manifest() coreplugin.Manifest { return coreplugin.ParseManifest(manifest) }

// Setup registers the provider.
func (p *Plugin) Setup(h *coreplugin.Host) error {
	h.RegisterAuthProvider(h.Auth.NewLocalProvider(h.Settings))
	return nil
}
