// Package theme_cms registers the "theme.cms" website template. The layout is
// implemented in web/index.ts; the Go part publishes the template settings
// (logo, colours, start page hero) that visitors need.
package theme_cms

import (
	"embed"
	"io/fs"
	"net/http"
	"regexp"

	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Plugin is the template plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Public lists the settings visitors may read (everything else stays admin-only).
var Public = []string{"logo", "logo_light", "logo_height", "primary_color", "accent_color", "header", "hero_image", "hero_title", "hero_subtitle",
	"hero_button_label", "hero_button_link", "hero_size", "banner", "banner_image", "copyright", "back_to_top"}

var (
	colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	hashRe  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Setup registers GET /config (public template settings).
func (p *Plugin) Setup(hs *sdk.Host) error {
	hs.Handle("GET", "/config", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s := hs.Settings()
		out := map[string]any{}
		for _, k := range Public {
			v, ok := s[k]
			if !ok {
				continue
			}
			str, isStr := v.(string)
			switch k {
			case "primary_color", "accent_color":
				if !isStr || !colorRe.MatchString(str) {
					continue
				}
			case "logo", "logo_light", "hero_image", "banner_image":
				if !isStr || !hashRe.MatchString(str) {
					continue
				}
			}
			out[k] = v
		}
		return out, nil
	})
	return nil
}
