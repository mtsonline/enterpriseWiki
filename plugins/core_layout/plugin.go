// Package core_layout provides layout builder macros for website-like pages:
// section, hero, cards, button and spacer. They work in every template; the
// CMS template (theme.cms) styles them as full-width page sections.
package core_layout

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
	"github.com/mtsonline/enterprisewiki/plugins/core_macros"

	_ "embed"
)

//go:embed plugin.json
var manifest []byte

// Plugin is the layout plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Setup registers the macros.
func (p *Plugin) Setup(h *sdk.Host) error {
	for _, m := range []content.Macro{section{}, hero{}, cards{}, button{}, spacer{}} {
		h.RegisterMacro(m)
	}
	return nil
}

var esc = html.EscapeString

func lm(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }

func oneOf(v string, opts []string) string {
	for _, o := range opts {
		if o == v {
			return v
		}
	}
	return opts[0]
}

// SafeURL accepts relative URLs, http(s) and mailto; everything else becomes "#".
func SafeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "#"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "#"
	}
	switch strings.ToLower(u.Scheme) {
	case "", "http", "https", "mailto":
		if u.Scheme == "" && strings.HasPrefix(raw, "//") {
			return "#"
		}
		return u.String()
	}
	return "#"
}

var backgrounds = []string{"none", "light", "dark", "accent"}
var widths = []string{"normal", "wide", "full"}

// ---------------------------------------------------------------- section

type section struct{}

func (section) Name() string              { return "section" }
func (section) Labels() map[string]string { return lm("Section", "Abschnitt") }
func (section) Description() map[string]string {
	return lm("A page section with background and width.", "Ein Seitenabschnitt mit Hintergrund und Breite.")
}
func (section) HasBody() bool { return true }
func (section) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "background", Labels: lm("Background", "Hintergrund"), Type: "select", Options: backgrounds, Default: "none"},
		{Name: "width", Labels: lm("Width", "Breite"), Type: "select", Options: widths, Default: "normal"},
		{Name: "align", Labels: lm("Alignment", "Ausrichtung"), Type: "select", Options: []string{"left", "center"}, Default: "left"},
	}
}
func (section) Render(_ context.Context, mc content.MacroContext) (string, error) {
	return fmt.Sprintf(`<section class="ewl-section ewl-section--bg-%s ewl-section--w-%s ewl-section--%s"><div class="ewl-section-inner">%s</div></section>`,
		oneOf(mc.Params["background"], backgrounds), oneOf(mc.Params["width"], widths), oneOf(mc.Params["align"], []string{"left", "center"}), mc.Body), nil
}

// ---------------------------------------------------------------- hero

type hero struct{}

func (hero) Name() string              { return "hero" }
func (hero) Labels() map[string]string { return lm("Hero banner", "Hero-Banner") }
func (hero) Description() map[string]string {
	return lm("A large introduction with title, text and button.", "Eine große Einleitung mit Titel, Text und Schaltfläche.")
}
func (hero) HasBody() bool { return true }
func (hero) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "title", Labels: lm("Title", "Titel"), Type: "text", Required: true},
		{Name: "subtitle", Labels: lm("Subtitle", "Untertitel"), Type: "text"},
		{Name: "button", Labels: lm("Button text", "Schaltflächentext"), Type: "text"},
		{Name: "link", Labels: lm("Button link", "Schaltflächen-Link"), Type: "url"},
		{Name: "background", Labels: lm("Background", "Hintergrund"), Type: "select", Options: []string{"accent", "dark", "light"}, Default: "accent"},
	}
}
func (hero) Render(_ context.Context, mc content.MacroContext) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, `<section class="ewl-hero ewl-hero--%s"><div class="ewl-hero-inner">`, oneOf(mc.Params["background"], []string{"accent", "dark", "light"}))
	if t := strings.TrimSpace(mc.Params["title"]); t != "" {
		b.WriteString(`<p class="ewl-hero-title">` + esc(t) + `</p>`)
	}
	if t := strings.TrimSpace(mc.Params["subtitle"]); t != "" {
		b.WriteString(`<p class="ewl-hero-subtitle">` + esc(t) + `</p>`)
	}
	if strings.TrimSpace(mc.Body) != "" {
		b.WriteString(`<div class="ewl-hero-body">` + mc.Body + `</div>`)
	}
	if t := strings.TrimSpace(mc.Params["button"]); t != "" {
		b.WriteString(`<p><a class="ewl-cta ewl-cta--primary" href="` + esc(SafeURL(mc.Params["link"])) + `">` + esc(t) + `</a></p>`)
	}
	b.WriteString(`</div></section>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- cards

type cards struct{}

func (cards) Name() string              { return "cards" }
func (cards) Labels() map[string]string { return lm("Cards / features", "Karten / Features") }
func (cards) Description() map[string]string {
	return lm("A grid of cards. Separate the cards with a horizontal line.", "Ein Raster aus Karten. Karten mit einer Trennlinie trennen.")
}
func (cards) HasBody() bool { return true }
func (cards) Params() []content.MacroParam {
	return []content.MacroParam{{Name: "columns", Labels: lm("Columns", "Spalten"), Type: "select", Options: []string{"2", "3", "4"}, Default: "3"}}
}
func (cards) Render(_ context.Context, mc content.MacroContext) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewl-cards ewl-cards--%s">`, oneOf(mc.Params["columns"], []string{"3", "2", "4"}))
	for _, c := range core_macros.SplitOnHR(mc.Body) {
		b.WriteString(`<div class="ewl-card">` + c + `</div>`)
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- button

type button struct{}

var buttonStyles = []string{"primary", "secondary"}

func (button) Name() string              { return "button" }
func (button) Labels() map[string]string { return lm("Button link", "Schaltflächen-Link") }
func (button) Description() map[string]string {
	return lm("A link shown as a prominent button.", "Ein Link, der als auffällige Schaltfläche erscheint.")
}
func (button) HasBody() bool { return false }
func (button) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "text", Labels: lm("Text", "Text"), Type: "text", Required: true},
		{Name: "link", Labels: lm("Link", "Link"), Type: "url", Required: true},
		{Name: "style", Labels: lm("Style", "Stil"), Type: "select", Options: buttonStyles, Default: "primary"},
	}
}
func (button) Render(_ context.Context, mc content.MacroContext) (string, error) {
	return `<p class="ewl-cta-wrap"><a class="ewl-cta ewl-cta--` + oneOf(mc.Params["style"], buttonStyles) + `" href="` + esc(SafeURL(mc.Params["link"])) + `">` +
		esc(strings.TrimSpace(mc.Params["text"])) + `</a></p>`, nil
}

// ---------------------------------------------------------------- spacer

type spacer struct{}

var sizes = []string{"medium", "small", "large"}

func (spacer) Name() string                   { return "spacer" }
func (spacer) Labels() map[string]string      { return lm("Spacer", "Abstand") }
func (spacer) Description() map[string]string { return lm("Vertical space.", "Vertikaler Abstand.") }
func (spacer) HasBody() bool                  { return false }
func (spacer) Params() []content.MacroParam {
	return []content.MacroParam{{Name: "size", Labels: lm("Size", "Größe"), Type: "select", Options: sizes, Default: "medium"}}
}
func (spacer) Render(_ context.Context, mc content.MacroContext) (string, error) {
	return `<div class="ewl-spacer ewl-spacer--` + oneOf(mc.Params["size"], sizes) + `" aria-hidden="true"></div>`, nil
}
