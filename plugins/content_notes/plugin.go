// Package content_notes provides shared notes: a Markdown page (core.html)
// that editors write on directly in the page view, live together with the
// others (see internal/collab), saved automatically.
package content_notes

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"strconv"

	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
	core_html "github.com/mtsonline/enterprisewiki/plugins/core_html"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// TypeID is the element type.
const TypeID = "content.notes"

// Plugin is the shared notes plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { sub, _ := fs.Sub(locales, "locales"); return sub }

// Setup registers the element type.
func (p *Plugin) Setup(h *sdk.Host) error {
	h.RegisterElementType(&Type{})
	return nil
}

// Type inherits storing, sanitizing and searching from core.html.
type Type struct {
	core_html.Type
}

// TypeID implements content.ElementType.
func (t *Type) TypeID() string { return TypeID }

// Labels implements content.ElementType.
func (t *Type) Labels() map[string]string {
	return map[string]string{"en": "Shared notes", "de": "Gemeinsame Notizen"}
}

// EmptyBody implements content.ElementType.
func (t *Type) EmptyBody() json.RawMessage {
	return json.RawMessage(`{"format":"markdown","source":""}`)
}

// Render shows the notes; the widget "notes" turns them into the live
// editor for users who may edit (readers keep the rendered page).
func (t *Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	inner, err := t.Type.Render(ctx, rc, raw)
	if err != nil {
		return "", err
	}
	return `<div class="ewn" data-ew-widget="notes" data-element="` + strconv.FormatInt(rc.ElementID, 10) + `">` + inner + `</div>`, nil
}
