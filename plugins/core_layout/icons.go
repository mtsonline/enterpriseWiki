package core_layout

// Icons of a free set (MIT, shipped by the frontend as one file per icon):
// a single icon (also inside a line), an icon box (icon, title, text, the
// whole box as one link) and an icon list (a list with an icon per entry).
// The HTML carries only the name of the icon; the browser shows its file.
// Screen readers read an icon: its label, or else its name in words in the
// language of the page (web.IconLabel; an icon without a description is not
// left out). Only an icon marked as decoration is
// skipped; the bullets of an icon list are decoration (the list is a list).

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
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
		return apperr.BadRequest("choose an icon (a name of lowercase letters, digits and hyphens)")
	}
	if has, built := web.HasIcon(style, name); built && !has {
		return apperr.BadRequest("there is no icon %q in the style %s", name, style)
	}
	return nil
}

// iconHTML is one icon: an image named by its label or, without one, by the
// name of the icon in words in the language of the page ("Pfeil links");
// decoration (hidden from screen readers) only when asked.
func iconHTML(lang, name, style, size, label string, decorative bool) string {
	a11y := ` aria-hidden="true"`
	if !decorative {
		l := strings.TrimSpace(label)
		if l == "" {
			l = web.IconLabel(lang, name)
		}
		a11y = ` role="img" aria-label="` + esc(l) + `"`
	}
	return `<span class="ew-icon ew-icon--` + size + `" data-icon="` + esc(name) + `" data-icon-style="` + style + `"` + a11y + `></span>`
}

func iconParams() []content.MacroParam {
	return []content.MacroParam{
		{Name: "name", Labels: lm("Icon", "Symbol"), Type: "custom:icon", Required: true},
		{Name: "look", Labels: lm("Style", "Stil"), Type: "select", Options: iconStyles, Default: "outline"},
		{Name: "size", Labels: lm("Size", "Größe"), Type: "select", Options: iconSizes, Default: "m"},
	}
}

// labelParams: what screen readers read for the icon.
func labelParams() []content.MacroParam {
	return []content.MacroParam{
		{Name: "label", Labels: lm("Label for screen readers (empty: the name of the icon)", "Beschriftung für Screenreader (leer: der Name des Symbols)"), Type: "text"},
		{Name: "decorative", Labels: lm("Decoration only (screen readers skip it)", "Nur Schmuck (Screenreader überspringen es)"), Type: "bool"},
	}
}

// ---------------------------------------------------------------- icon

type icon struct{}

func (icon) Name() string              { return "icon" }
func (icon) Group() string             { return "text" }
func (icon) Labels() map[string]string { return lm("Icon", "Symbol") }
func (icon) Description() map[string]string {
	return lm("An icon, also inside a line of text. Screen readers read its label, or else its name.", "Ein Symbol, auch in einer Textzeile. Screenreader lesen seine Beschriftung, sonst seinen Namen.")
}
func (icon) HasBody() bool { return false }
func (icon) Inline() bool  { return true }
func (icon) Params() []content.MacroParam {
	return append(iconParams(), labelParams()...)
}
func (icon) Render(_ context.Context, mc content.MacroContext) (string, error) {
	name, style := strings.TrimSpace(mc.Params["name"]), oneOf(mc.Params["look"], iconStyles)
	if err := checkIcon(name, style); err != nil {
		return "", err
	}
	label := mc.Params["label"]
	if strings.TrimSpace(label) == "" {
		label = mc.Params["text"] // :icon[Home]{name=home}: the text in brackets names it
	}
	return iconHTML(mc.Lang, name, style, oneOf(mc.Params["size"], iconSizes), label, mc.Params["decorative"] == "true"), nil
}

// ---------------------------------------------------------------- icon box

type iconbox struct{}

func (iconbox) Name() string              { return "iconbox" }
func (iconbox) Group() string             { return "layout" }
func (iconbox) Labels() map[string]string { return lm("Icon box", "Symbolbox") }
func (iconbox) Description() map[string]string {
	return lm("An icon with title and text; with a link the whole box is one link.", "Ein Symbol mit Titel und Text; mit Link ist der ganze Kasten ein Link.")
}
func (iconbox) HasBody() bool { return true }
func (iconbox) Params() []content.MacroParam {
	return append(append(iconParams(), labelParams()...),
		content.MacroParam{Name: "title", Labels: lm("Title", "Titel"), Type: "text", Required: true},
		content.MacroParam{Name: "link", Labels: lm("Link (the whole box)", "Link (der ganze Kasten)"), Type: "url"},
		content.MacroParam{Name: "position", Labels: lm("Icon", "Symbol"), Type: "select", Options: []string{"top", "left"}, Default: "top"},
		content.MacroParam{Name: "align", Labels: lm("Alignment", "Ausrichtung"), Type: "select", Options: aligns3, Default: "start"},
	)
}
func (iconbox) Render(_ context.Context, mc content.MacroContext) (string, error) {
	name, style := strings.TrimSpace(mc.Params["name"]), oneOf(mc.Params["look"], iconStyles)
	if err := checkIcon(name, style); err != nil {
		return "", err
	}
	title := strings.TrimSpace(mc.Params["title"])
	if title == "" {
		return "", apperr.BadRequest("an icon box needs a title")
	}
	pos := oneOf(mc.Params["position"], []string{"top", "left"})
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewl-iconbox ewl-iconbox--%s ewl-align--%s">%s<div class="ewl-iconbox-text">`, pos, oneOf(mc.Params["align"], aligns3), iconHTML(mc.Lang, name, style, oneOf(mc.Params["size"], iconSizes), mc.Params["label"], mc.Params["decorative"] == "true"))
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
func (iconlist) Group() string             { return "text" }
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
	name, style := strings.TrimSpace(mc.Params["name"]), oneOf(mc.Params["look"], iconStyles)
	if name == "" {
		name = "check"
	}
	if err := checkIcon(name, style); err != nil {
		return "", err
	}
	// the icon is the bullet of the list: decoration, the list stays a list
	return `<div class="ewl-iconlist ewl-iconlist--` + oneOf(mc.Params["size"], iconSizes) + `" data-icon="` + esc(name) + `" data-icon-style="` + style + `">` + mc.Body + `</div>`, nil
}
