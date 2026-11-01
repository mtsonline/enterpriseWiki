// Package auth_local registers the local username/password provider.
package auth_local

import (
	_ "embed"

	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

// Plugin is the local authentication plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Setup registers the provider.
func (p *Plugin) Setup(h *sdk.Host) error {
	h.RegisterAuthProvider(h.Auth.NewLocalProvider())
	return nil
}
