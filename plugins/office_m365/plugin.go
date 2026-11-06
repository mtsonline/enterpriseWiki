// Package office_m365 opens files of the media library in Microsoft 365
// for the web (Word, Excel, PowerPoint) through WOPI.
//
// Microsoft only serves WOPI hosts of its Cloud Storage Partner Program
// (CSPP): Microsoft allows the domain of the wiki.
//
// Configuration (config.yaml -> plugin_settings."office.m365"):
//
//	url: https://onenote.officeapps.live.com/hosting/discovery  # default (production)
//	verify_proof: true  # check the proof key of every request (default)
//
// Microsoft cannot reliably block downloads, so users without the right
// "download" always see files in the browser.
package office_m365

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
	"github.com/mtsonline/enterprisewiki/pkg/wopi"

	_ "embed"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Production is the discovery of Microsoft 365 for the web.
const Production = "https://onenote.officeapps.live.com/hosting/discovery"

// Plugin is the Microsoft 365 plugin (one instance per site).
type Plugin struct {
	host *sdk.Host
	op   *wopi.Opener
}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

func (p *Plugin) settings() wopi.Settings {
	m := p.host.Settings()
	d := wopi.Str(m, "url")
	if d == "" {
		d = Production
	}
	if !wopi.Bool(m, "enabled", false) {
		d = "" // nothing is sent to Microsoft before the administrator switches it on
	}
	return wopi.Settings{Discovery: d, Zone: "external-https", VerifyProof: wopi.Bool(m, "verify_proof", true)}
}

// Setup registers the program and the connection test.
func (p *Plugin) Setup(host *sdk.Host) error {
	p.host = host
	p.op = &wopi.Opener{PluginID: host.Manifest.ID, Name: "Microsoft 365", Settings: p.settings}
	host.Media.RegisterOpener(p.op)
	p.op.Warm()
	host.Handle("POST", "/test", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := perm.Require(r.Context(), "core.settings.manage"); err != nil {
			return nil, err
		}
		return p.op.Test(r.Context()), nil
	})
	return nil
}
