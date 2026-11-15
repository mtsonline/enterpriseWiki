// Package office_collabora opens files of the media library in Collabora
// Online (or Collabora CODE) through WOPI.
//
// Configuration (config.yaml -> plugin_settings."office.collabora"):
//
//	url: https://collabora.example.com   # server; discovery at /hosting/discovery
//	verify_proof: true                   # check the proof key of every request (default)
//	readonly_without_download: true      # view-only users may view there (default)
package office_collabora

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	"github.com/mtsonline/enterprisewiki/internal/media"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/internal/wopi"

	_ "embed"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Plugin is the Collabora Online plugin (one instance per site).
type Plugin struct {
	host *coreplugin.Host
	op   *wopi.Opener
}

func init() { coreplugin.Register(&Plugin{}) }

// Manifest implements coreplugin.Plugin.
func (p *Plugin) Manifest() coreplugin.Manifest { return coreplugin.ParseManifest(manifest) }

// Locales implements coreplugin.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

func (p *Plugin) settings() wopi.Settings {
	m := p.host.Settings()
	return wopi.Settings{
		Discovery:               wopi.DiscoveryURL(wopi.Str(m, "url")),
		VerifyProof:             wopi.Bool(m, "verify_proof", true),
		ReadOnlyWithoutDownload: wopi.Bool(m, "readonly_without_download", true),
	}
}

// Setup registers the program and the connection test.
func (p *Plugin) Setup(host *coreplugin.Host) error {
	p.host = host
	p.op = &wopi.Opener{PluginID: host.Manifest.ID, Name: "Collabora Online", Restrict: true, Watermark: true, Settings: p.settings,
		Extend: func(info map[string]any, t *media.Token, m *media.Medium) {
			// Collabora specific: no "save as" to other places, no inline
			// "insert remote image" from the host
			info["EnableInsertRemoteImage"] = false
			info["EnableShare"] = false
			info["HideUserList"] = ""
		}}
	host.Media.RegisterOpener(p.op)
	p.op.Warm()
	// action: office.collabora.test
	host.Handle("POST", "/test", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := perm.Require(r.Context(), "core.settings.manage"); err != nil {
			return nil, err
		}
		return p.op.Test(r.Context()), nil
	})
	return nil
}
