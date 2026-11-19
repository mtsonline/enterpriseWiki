package core_layout

// Icons of a free set (MIT, shipped by the frontend as one file per icon):
// a single icon (also inside a line), an icon box (icon, title, text, the
// whole box as one link) and an icon list (a list with an icon per entry).
// The HTML carries only the name of the icon; the browser shows its file.
// An icon is decoration unless it has a label (then it is an image with
// that name for screen readers).

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/web"
)

var (
	iconNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	iconStyles = []string{"outline", "filled"}
	iconSizes  = []string{"m", "s", "l", "xl"}
)

// checkIcon checks the name of an icon: its form always, and that it exists
// when the icons are part of the program.
func checkIcon(name, style string) error {
	if !iconNameRe.MatchString(name) {
		return fmt.Errorf("choose an icon (a name of lowercase letters, digits and hyphens)")
	}
	if has, built := web.HasIcon(style, name); built && !has {
		return fmt.Errorf("there is no icon %q in the style %s", name, style)
	}
	return nil
}

// iconHTML is one icon: decoration (hidden from screen readers) or, with a
// label, an image with that name.
func iconHTML(name, style, size, label string) string {
	a11y := ` aria-hidden="true"`
	if l := strings.TrimSpace(label); l != "" {
		a11y = ` role="img" aria-label="` + esc(l) + `"`
	}
	return `<span class="ew-icon ew-icon--` + size + `" data-icon="` + esc(name) + `" data-icon-style="` + style + `"` + a11y + `></span>`
}

func iconParams() []content.MacroParam {
	return []content.MacroParam{
		{Name: "name", Labels: lm("Icon", "Symbol"), Type: "custom:icon", Required: true},
		{Name: "style", Labels: lm("Style", "Stil"), Type: "select", Options: iconStyles, Default: "outline"},
		{Name: "size", Labels: lm("Size", "Größe"), Type: "select", Options: iconSizes, Default: "m"},
	}
}

// ---------------------------------------------------------------- icon

type icon struct{}

func (icon) Name() string              { return "icon" }
func (icon) Labels() map[string]string { return lm("Icon", "Symbol") }
func (icon) Description() map[string]string {
	return lm("An icon, also inside a line of text. Without a label it is decoration.", "Ein Symbol, auch in einer Textzeile. Ohne Beschriftung ist es Schmuck.")
}
func (icon) HasBody() bool { return false }
func (icon) Inline() bool  { return true }
func (icon) Params() []content.MacroParam {
	return append(iconParams(), content.MacroParam{Name: "label", Labels: lm("Label for screen readers (empty: decoration)", "Beschriftung für Screenreader (leer: Schmuck)"), Type: "text"})
}
func (icon) Render(_ context.Context, mc content.MacroContext) (string, error) {
	name, style := strings.TrimSpace(mc.Params["name"]), oneOf(mc.Params["style"], iconStyles)
	if err := checkIcon(name, style); err != nil {
		return "", err
	}
	label := mc.Params["label"]
	if strings.TrimSpace(label) == "" {
		label = mc.Params["text"] // :icon[Home]{name=home}: the text in brackets names it
	}
	return iconHTML(name, style, oneOf(mc.Params["size"], iconSizes), label), nil
}

// ---------------------------------------------------------------- icon box

type iconbox struct{}

func (iconbox) Name() string              { return "iconbox" }
func (iconbox) Labels() map[string]string { return lm("Icon box", "Symbolbox") }
func (iconbox) Description() map[string]string {
	return lm("An icon with title and text; with a link the whole box is one link.", "Ein Symbol mit Titel und Text; mit Link ist der ganze Kasten ein Link.")
}
func (iconbox) HasBody() bool { return true }
func (iconbox) Params() []content.MacroParam {
	return append(iconParams(),
		content.MacroParam{Name: "title", Labels: lm("Title", "Titel"), Type: "text", Required: true},
		content.MacroParam{Name: "link", Labels: lm("Link (the whole box)", "Link (der ganze Kasten)"), Type: "url"},
		content.MacroParam{Name: "position", Labels: lm("Icon", "Symbol"), Type: "select", Options: []string{"top", "left"}, Default: "top"},
		content.MacroParam{Name: "align", Labels: lm("Alignment", "Ausrichtung"), Type: "select", Options: aligns3, Default: "start"},
	)
}
func (iconbox) Render(_ context.Context, mc content.MacroContext) (string, error) {
	name, style := strings.TrimSpace(mc.Params["name"]), oneOf(mc.Params["style"], iconStyles)
	if err := checkIcon(name, style); err != nil {
		return "", err
	}
	title := strings.TrimSpace(mc.Params["title"])
	if title == "" {
		return "", fmt.Errorf("an icon box needs a title")
	}
	pos := oneOf(mc.Params["position"], []string{"top", "left"})
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewl-iconbox ewl-iconbox--%s ewl-align--%s">%s<div class="ewl-iconbox-text">`, pos, oneOf(mc.Params["align"], aligns3), iconHTML(name, style, oneOf(mc.Params["size"], iconSizes), ""))
	// the whole box as one link: the title is the link, its area covers the box
	// (one link, one name, one focus – not the text inside as a link)
	if link := strings.TrimSpace(mc.Params["link"]); link != "" {
		b.WriteString(`<p class="ewl-iconbox-title"><a class="ewl-boxlink" href="` + esc(SafeURL(link)) + `">` + esc(title) + `</a></p>`)
	} else {
		b.WriteString(`<p class="ewl-iconbox-title">` + esc(title) + `</p>`)
	}
	if strings.TrimSpace(mc.Body) != "" {
		b.WriteString(`<div class="ewl-iconbox-body">` + mc.Body + `</div>`)
	}
	b.WriteString(`</div></div>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- icon list

type iconlist struct{}

func (iconlist) Name() string              { return "iconlist" }
func (iconlist) Labels() map[string]string { return lm("Icon list", "Symbolliste") }
func (iconlist) Description() map[string]string {
	return lm("A list with an icon in front of every entry (for example a check mark).", "Eine Liste mit einem Symbol vor jedem Eintrag (zum Beispiel ein Häkchen).")
}
func (iconlist) HasBody() bool { return true }
func (iconlist) Params() []content.MacroParam {
	p := iconParams()
	p[0].Default = "check"
	return p
}
func (iconlist) Render(_ context.Context, mc content.MacroContext) (string, error) {
	name, style := strings.TrimSpace(mc.Params["name"]), oneOf(mc.Params["style"], iconStyles)
	if name == "" {
		name = "check"
	}
	if err := checkIcon(name, style); err != nil {
		return "", err
	}
	// the icon is the bullet of the list: decoration, the list stays a list
	return `<div class="ewl-iconlist ewl-iconlist--` + oneOf(mc.Params["size"], iconSizes) + `" data-icon="` + esc(name) + `" data-icon-style="` + style + `">` + mc.Body + `</div>`, nil
}
