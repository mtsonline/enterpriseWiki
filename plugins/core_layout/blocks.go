package core_layout

// Layout blocks of priority A (plan of blocks and options, 3.1): grid with
// columns per view, heading, divider, buttons, call to action, tabs and
// accordion. Every block also has the options of blocks (parameter
// "options", added by the content service). Tabs and accordion work without
// script (sections, details); the browser makes tabs of the sections.

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/plugins/core_macros"
)

var (
	headingRe = regexp.MustCompile(`(?is)^\s*<h([1-6])[^>]*>(.*?)</h[1-6]>`)
	tagRe     = regexp.MustCompile(`<[^>]*>`)
)

// firstHeading splits the first heading off a part: its text and the rest.
func firstHeading(part string) (string, string) {
	m := headingRe.FindStringSubmatchIndex(part)
	if m == nil {
		return "", part
	}
	text := strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(part[m[4]:m[5]], "")))
	return text, part[m[1]:]
}

func intParam(v string, def, lo, hi int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return max(lo, min(hi, n))
}

// ---------------------------------------------------------------- grid

type grid struct{}

var gaps = []string{"medium", "small", "large", "none"}
var valigns = []string{"stretch", "start", "center", "end"}

func (grid) Name() string              { return "grid" }
func (grid) Labels() map[string]string { return lm("Grid", "Raster") }
func (grid) Description() map[string]string {
	return lm("Content in columns, with the number of columns per view. Separate the cells with a horizontal line.",
		"Inhalte in Spalten, mit der Zahl der Spalten je Ansicht. Zellen mit einer Trennlinie trennen.")
}
func (grid) HasBody() bool { return true }
func (grid) Params() []content.MacroParam {
	cols := []string{"1", "2", "3", "4", "5"}
	return []content.MacroParam{
		{Name: "desktop", Labels: lm("Columns on the desktop", "Spalten auf dem Desktop"), Type: "select", Options: cols, Default: "3"},
		{Name: "tablet", Labels: lm("Columns on the tablet", "Spalten auf dem Tablet"), Type: "select", Options: cols[:3], Default: "2"},
		{Name: "mobile", Labels: lm("Columns on the phone", "Spalten auf dem Handy"), Type: "select", Options: cols[:1], Default: "1"},
		{Name: "gap", Labels: lm("Space between", "Abstand dazwischen"), Type: "select", Options: gaps, Default: "medium"},
		{Name: "align", Labels: lm("Vertical alignment", "Senkrechte Ausrichtung"), Type: "select", Options: valigns, Default: "stretch"},
	}
}

func (grid) Render(_ context.Context, mc content.MacroContext) (string, error) {
	n := map[string]int{}
	for _, view := range []string{"desktop", "tablet", "mobile"} {
		def := map[string]int{"desktop": 3, "tablet": 2, "mobile": 1}[view]
		n[view] = intParam(mc.Params[view], def, 1, 5)
		// the same rule as for every block: what is side by side needs room
		if n[view] > content.MaxColumns(view) {
			return "", fmt.Errorf("on the %s at most %d columns have room side by side; set fewer", view, content.MaxColumns(view))
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewl-grid ewl-grid--d%d ewl-grid--t%d ewl-grid--m%d ewl-gap--%s ewl-valign--%s">`,
		n["desktop"], n["tablet"], n["mobile"], oneOf(mc.Params["gap"], gaps), oneOf(mc.Params["align"], valigns))
	for _, c := range core_macros.SplitOnHR(mc.Body) {
		b.WriteString(`<div class="ewl-cell">` + c + `</div>`)
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- heading

type heading struct{}

var headingSizes = []string{"auto", "xl", "l", "m", "s"}
var aligns3 = []string{"start", "center", "end"}

func (heading) Name() string              { return "heading" }
func (heading) Labels() map[string]string { return lm("Heading", "Überschrift") }
func (heading) Description() map[string]string {
	return lm("A heading with subtitle and line; its level (the structure of the page for screen readers) is chosen apart from its size.",
		"Eine Überschrift mit Untertitel und Linie; ihre Ebene (die Gliederung der Seite für Screenreader) wird getrennt von ihrer Größe gewählt.")
}
func (heading) HasBody() bool { return false }
func (heading) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "text", Labels: lm("Text", "Text"), Type: "text", Required: true},
		{Name: "level", Labels: lm("Level", "Ebene"), Type: "select", Options: []string{"2", "3", "4", "5", "6"}, Default: "2"},
		{Name: "size", Labels: lm("Size", "Größe"), Type: "select", Options: headingSizes, Default: "auto"},
		{Name: "subtitle", Labels: lm("Subtitle", "Untertitel"), Type: "text"},
		{Name: "line", Labels: lm("Line below", "Linie darunter"), Type: "bool"},
		{Name: "align", Labels: lm("Alignment", "Ausrichtung"), Type: "select", Options: aligns3, Default: "start"},
	}
}

func (heading) Render(_ context.Context, mc content.MacroContext) (string, error) {
	text := strings.TrimSpace(mc.Params["text"])
	if text == "" {
		return "", fmt.Errorf("the heading needs a text")
	}
	level := intParam(mc.Params["level"], 2, 2, 6)
	cls := "ewl-heading ewl-heading--" + oneOf(mc.Params["size"], headingSizes) + " ewl-align--" + oneOf(mc.Params["align"], aligns3)
	if mc.Params["line"] == "true" {
		cls += " ewl-heading--line"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="%s"><h%d>%s</h%d>`, cls, level, esc(text), level)
	if s := strings.TrimSpace(mc.Params["subtitle"]); s != "" {
		b.WriteString(`<p class="ewl-heading-sub">` + esc(s) + `</p>`)
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- divider

type divider struct{}

var dividerStyles = []string{"line", "dashed", "dotted", "double", "fade", "space"}

func (divider) Name() string              { return "divider" }
func (divider) Labels() map[string]string { return lm("Divider", "Trenner") }
func (divider) Description() map[string]string {
	return lm("A line or space between parts of a page.", "Eine Linie oder ein Abstand zwischen Teilen einer Seite.")
}
func (divider) HasBody() bool { return false }
func (divider) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "style", Labels: lm("Style", "Stil"), Type: "select", Options: dividerStyles, Default: "line"},
		{Name: "size", Labels: lm("Space around it", "Abstand darum"), Type: "select", Options: sizes, Default: "medium"},
		{Name: "width", Labels: lm("Width", "Breite"), Type: "select", Options: []string{"full", "half", "short"}, Default: "full"},
	}
}

func (divider) Render(_ context.Context, mc content.MacroContext) (string, error) {
	// a divider is decoration: screen readers skip it (the page structure is in the headings)
	return `<div class="ewl-divider ewl-divider--` + oneOf(mc.Params["style"], dividerStyles) + ` ewl-spacer--` + oneOf(mc.Params["size"], sizes) +
		` ewl-divider-w--` + oneOf(mc.Params["width"], []string{"full", "half", "short"}) + `" aria-hidden="true"><span></span></div>`, nil
}

// ---------------------------------------------------------------- buttons

type buttons struct{}

var buttonLayouts = []string{"row", "full", "stack"}

func (buttons) Name() string              { return "buttons" }
func (buttons) Labels() map[string]string { return lm("Buttons", "Knopfgruppe") }
func (buttons) Description() map[string]string {
	return lm("Up to four links shown as buttons, side by side or over the full width.",
		"Bis zu vier Links als Schaltflächen, nebeneinander oder über die volle Breite.")
}
func (buttons) HasBody() bool { return false }
func (buttons) Params() []content.MacroParam {
	p := []content.MacroParam{}
	for i := 1; i <= 4; i++ {
		n := strconv.Itoa(i)
		p = append(p,
			content.MacroParam{Name: "text" + n, Labels: lm("Text of button "+n, "Text der Schaltfläche "+n), Type: "text", Required: i == 1},
			content.MacroParam{Name: "link" + n, Labels: lm("Link of button "+n, "Link der Schaltfläche "+n), Type: "url", Required: i == 1},
			content.MacroParam{Name: "style" + n, Labels: lm("Style of button "+n, "Stil der Schaltfläche "+n), Type: "select", Options: buttonStyles, Default: map[bool]string{true: "primary", false: "secondary"}[i == 1]},
		)
	}
	return append(p,
		content.MacroParam{Name: "layout", Labels: lm("Arrangement", "Anordnung"), Type: "select", Options: buttonLayouts, Default: "row"},
		content.MacroParam{Name: "align", Labels: lm("Alignment", "Ausrichtung"), Type: "select", Options: aligns3, Default: "start"},
	)
}

func (buttons) Render(_ context.Context, mc content.MacroContext) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewl-buttons ewl-buttons--%s ewl-align--%s">`, oneOf(mc.Params["layout"], buttonLayouts), oneOf(mc.Params["align"], aligns3))
	shown := 0
	for i := 1; i <= 4; i++ {
		n := strconv.Itoa(i)
		text := strings.TrimSpace(mc.Params["text"+n])
		if text == "" {
			continue
		}
		shown++
		style := mc.Params["style"+n]
		if style == "" && i > 1 {
			style = "secondary"
		}
		b.WriteString(`<a class="ewl-cta ewl-cta--` + oneOf(style, buttonStyles) + `" href="` + esc(SafeURL(mc.Params["link"+n])) + `">` + esc(text) + `</a>`)
	}
	if shown == 0 {
		return "", fmt.Errorf("at least one button needs a text")
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- call to action

type cta struct{}

func (cta) Name() string              { return "cta" }
func (cta) Labels() map[string]string { return lm("Call to action", "Handlungsaufruf") }
func (cta) Description() map[string]string {
	return lm("A box with a title, text and a button that asks to act.", "Ein Kasten mit Titel, Text und einer Schaltfläche, die zum Handeln auffordert.")
}
func (cta) HasBody() bool { return true }
func (cta) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "title", Labels: lm("Title", "Titel"), Type: "text", Required: true},
		{Name: "button", Labels: lm("Button text", "Schaltflächentext"), Type: "text", Required: true},
		{Name: "link", Labels: lm("Button link", "Schaltflächen-Link"), Type: "url", Required: true},
		{Name: "layout", Labels: lm("Arrangement", "Anordnung"), Type: "select", Options: []string{"side", "stack"}, Default: "side"},
	}
}

func (cta) Render(_ context.Context, mc content.MacroContext) (string, error) {
	title := strings.TrimSpace(mc.Params["title"])
	text := strings.TrimSpace(mc.Params["button"])
	if title == "" || text == "" {
		return "", fmt.Errorf("a call to action needs a title and a button text")
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewl-ctabox ewl-ctabox--%s"><div class="ewl-ctabox-text"><p class="ewl-ctabox-title">%s</p>`, oneOf(mc.Params["layout"], []string{"side", "stack"}), esc(title))
	if strings.TrimSpace(mc.Body) != "" {
		b.WriteString(`<div class="ewl-ctabox-body">` + mc.Body + `</div>`)
	}
	b.WriteString(`</div><p class="ewl-ctabox-action"><a class="ewl-cta ewl-cta--primary" href="` + esc(SafeURL(mc.Params["link"])) + `">` + esc(text) + `</a></p></div>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- tabs and accordion

type tabs struct{}

func (tabs) Name() string              { return "tabs" }
func (tabs) Labels() map[string]string { return lm("Tabs", "Reiter") }
func (tabs) Description() map[string]string {
	return lm("Parts shown as tabs. Separate the parts with a horizontal line; the first heading of each part is the name of its tab.",
		"Teile als Reiter. Teile mit einer Trennlinie trennen; die erste Überschrift jedes Teils ist der Name seines Reiters.")
}
func (tabs) HasBody() bool { return true }
func (tabs) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "label", Labels: lm("Name of the tabs for screen readers", "Name der Reiter für Screenreader"), Type: "text"},
	}
}

// panels splits a body into named parts (at least one).
func panels(body string, what string) ([][2]string, error) {
	var out [][2]string
	for i, part := range core_macros.SplitOnHR(body) {
		name, rest := firstHeading(part)
		if name == "" {
			name = fmt.Sprintf("%s %d", what, i+1)
		}
		out = append(out, [2]string{name, rest})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("separate the parts with a horizontal line")
	}
	if len(out) > 20 {
		return nil, fmt.Errorf("at most 20 parts")
	}
	return out, nil
}

func (tabs) Render(_ context.Context, mc content.MacroContext) (string, error) {
	ps, err := panels(mc.Body, "Tab")
	if err != nil {
		return "", err
	}
	// without script: one section per part with its heading; the browser
	// turns them into tabs (tablist, arrow keys)
	var b strings.Builder
	label := strings.TrimSpace(mc.Params["label"])
	fmt.Fprintf(&b, `<div class="ewl-tabs" data-ew-widget="ewl.tabs" data-label="%s">`, esc(label))
	for i, p := range ps {
		fmt.Fprintf(&b, `<section class="ewl-tab" id="ewl-tab-%d-%d"><h3 class="ewl-tab-name">%s</h3><div class="ewl-tab-body">%s</div></section>`, mc.Index, i+1, esc(p[0]), p[1])
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}

type accordion struct{}

func (accordion) Name() string              { return "accordion" }
func (accordion) Labels() map[string]string { return lm("Accordion", "Akkordeon") }
func (accordion) Description() map[string]string {
	return lm("Parts to open and close. Separate the parts with a horizontal line; the first heading of each part is its title.",
		"Teile zum Auf- und Zuklappen. Teile mit einer Trennlinie trennen; die erste Überschrift jedes Teils ist sein Titel.")
}
func (accordion) HasBody() bool { return true }
func (accordion) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "one", Labels: lm("Only one open at a time", "Immer nur einer offen"), Type: "bool"},
		{Name: "first", Labels: lm("First one open", "Erster offen"), Type: "bool"},
	}
}

func (accordion) Render(_ context.Context, mc content.MacroContext) (string, error) {
	ps, err := panels(mc.Body, "Part")
	if err != nil {
		return "", err
	}
	name := ""
	if mc.Params["one"] == "true" {
		// details with the same name: the browser keeps only one open
		name = fmt.Sprintf(` name="ewl-acc-%d"`, mc.Index)
	}
	var b strings.Builder
	b.WriteString(`<div class="ewl-accordion">`)
	for i, p := range ps {
		open := ""
		if i == 0 && mc.Params["first"] == "true" {
			open = " open"
		}
		fmt.Fprintf(&b, `<details class="ewl-acc"%s%s><summary>%s</summary><div class="ewl-acc-body">%s</div></details>`, name, open, esc(p[0]), p[1])
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}
