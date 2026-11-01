// Package example_notice is an example plugin that builds on core.html.
//
// It demonstrates:
//   - inheriting an element type through struct embedding (Type embeds
//     core_html.Type and only overrides what differs),
//   - an own API route (/api/v1/x/example.notice/tones),
//   - UI translations (locales/*.json),
//   - a frontend part (web/index.ts) that reuses the core.html editor.
package example_notice

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
	core_html "github.com/mtsonline/enterprisewiki/plugins/core_html"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Plugin is the example plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { sub, _ := fs.Sub(locales, "locales"); return sub }

// Tones are the available box styles.
var Tones = []string{"info", "success", "warning", "danger"}

// Setup registers the element type and an API route.
func (p *Plugin) Setup(h *sdk.Host) error {
	h.RegisterElementType(&Type{})
	h.Handle("GET", "/tones", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return Tones, nil
	})
	return nil
}

// Type inherits everything from core.html and adds a tone.
type Type struct {
	core_html.Type
}

type body struct {
	core_html.Body
	Tone string `json:"tone"`
}

func decode(raw json.RawMessage) (*body, error) {
	var b body
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&b); err != nil {
		return nil, errors.New("invalid body")
	}
	valid := false
	for _, t := range Tones {
		valid = valid || t == b.Tone
	}
	if !valid {
		b.Tone = "info"
	}
	return &b, nil
}

// TypeID implements content.ElementType.
func (t *Type) TypeID() string { return "example.notice" }

// Labels implements content.ElementType.
func (t *Type) Labels() map[string]string {
	return map[string]string{"en": "Notice box", "de": "Hinweisbox"}
}

// EmptyBody implements content.ElementType.
func (t *Type) EmptyBody() json.RawMessage {
	return json.RawMessage(`{"format":"html","source":"","tone":"info"}`)
}

// Normalize reuses the core.html sanitizer and validates the tone.
func (t *Type) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	b, err := decode(raw)
	if err != nil {
		return nil, err
	}
	inner, _ := json.Marshal(b.Body)
	norm, err := t.Type.Normalize(inner)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(norm, &b.Body); err != nil {
		return nil, err
	}
	return json.Marshal(b)
}

// Render wraps the core.html rendering in a note box.
func (t *Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	b, err := decode(raw)
	if err != nil {
		return "", err
	}
	inner := core_html.ToHTML(&b.Body)
	if rc.Service != nil {
		inner = rc.Service.RenderMacros(ctx, rc, inner)
	}
	return `<aside class="ew-notice ew-notice--` + b.Tone + `" role="note">` + inner + `</aside>`, nil
}

// Text reuses core.html (the inherited method works on the same fields).
func (t *Type) Text(raw json.RawMessage) string { return t.Type.Text(raw) }
