// Package core_macros provides the standard macros (similar to Confluence).
package core_macros

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"

	_ "embed"
)

//go:embed plugin.json
var manifest []byte

// Plugin is the standard macro plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Setup registers the macros.
func (p *Plugin) Setup(h *sdk.Host) error {
	for _, m := range []content.Macro{toc{}, children{}, recent{}, panel{}, expand{}, status{}, columns{}} {
		h.RegisterMacro(m)
	}
	return nil
}

var esc = html.EscapeString

func lm(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }

func pick(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	for _, v := range m {
		return v
	}
	return ""
}

func intParam(p map[string]string, k string, def, min, max int) int {
	v, err := strconv.Atoi(p[k])
	if err != nil || v < min || v > max {
		return def
	}
	return v
}

// ---------------------------------------------------------------- toc

type toc struct{}

func (toc) Name() string              { return "toc" }
func (toc) Labels() map[string]string { return lm("Table of contents", "Inhaltsverzeichnis") }
func (toc) Description() map[string]string {
	return lm("Lists the headings of this page.", "Listet die Überschriften dieser Seite auf.")
}
func (toc) HasBody() bool { return false }
func (toc) Params() []content.MacroParam {
	return []content.MacroParam{{Name: "levels", Labels: lm("Heading levels", "Überschriftsebenen"), Type: "select", Options: []string{"2", "3", "4"}, Default: "3"}}
}
func (toc) Render(_ context.Context, mc content.MacroContext) (string, error) {
	max := intParam(mc.Params, "levels", 3, 1, 6)
	var b strings.Builder
	label := lm("Contents", "Inhalt")
	fmt.Fprintf(&b, `<nav class="ew-toc" aria-label="%s"><p class="ew-toc-title">%s</p><ul>`, esc(pick(label, mc.Lang)), esc(pick(label, mc.Lang)))
	n := 0
	for _, h := range mc.Headings {
		if h.Level < 2 || h.Level > max {
			continue
		}
		fmt.Fprintf(&b, `<li class="ew-toc-l%d"><a href="#%s">%s</a></li>`, h.Level, esc(h.ID), esc(h.Text))
		n++
	}
	b.WriteString(`</ul></nav>`)
	if n == 0 {
		return "", nil
	}
	return b.String(), nil
}

// ---------------------------------------------------------------- children

type children struct{}

func (children) Name() string              { return "children" }
func (children) Labels() map[string]string { return lm("Child pages", "Unterseiten") }
func (children) Description() map[string]string {
	return lm("Lists the subcategories and pages of this category.", "Listet Unterkategorien und Seiten dieser Kategorie auf.")
}
func (children) HasBody() bool { return false }
func (children) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "category", Labels: lm("Category id (empty = this category)", "Kategorie-ID (leer = diese Kategorie)"), Type: "number"},
		{Name: "show", Labels: lm("Show", "Anzeigen"), Type: "select", Options: []string{"all", "categories", "pages"}, Default: "all"},
	}
}
func (children) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	cat := mc.CategoryID
	if v, err := strconv.ParseInt(mc.Params["category"], 10, 64); err == nil && v > 0 {
		cat = v
	}
	if cat == 0 || mc.Service == nil {
		return "", nil
	}
	show := mc.Params["show"]
	var b strings.Builder
	b.WriteString(`<ul class="ew-children">`)
	if show != "pages" {
		cats, err := mc.Service.Children(ctx, cat)
		if err != nil {
			return "", nil // not visible: render nothing
		}
		for _, c := range cats {
			fmt.Fprintf(&b, `<li class="ew-children-cat"><a href="/c/%d">%s</a></li>`, c.ID, esc(pick(c.Titles, mc.Lang)))
		}
	}
	if show != "categories" {
		els, err := mc.Service.ListElements(ctx, cat)
		if err == nil {
			for _, e := range els {
				if e.ID == mc.ElementID {
					continue
				}
				fmt.Fprintf(&b, `<li><a href="/e/%d">%s</a></li>`, e.ID, esc(pick(e.Titles, mc.Lang)))
			}
		}
	}
	b.WriteString(`</ul>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- recent

type recent struct{}

func (recent) Name() string              { return "recent" }
func (recent) Labels() map[string]string { return lm("Recently changed", "Zuletzt geändert") }
func (recent) Description() map[string]string {
	return lm("Pages changed most recently.", "Die zuletzt geänderten Seiten.")
}
func (recent) HasBody() bool { return false }
func (recent) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "count", Labels: lm("Number of entries", "Anzahl Einträge"), Type: "number", Default: "10"},
		{Name: "scope", Labels: lm("Scope", "Bereich"), Type: "select", Options: []string{"category", "all"}, Default: "category"},
	}
}
func (recent) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	cat := mc.CategoryID
	if mc.Params["scope"] == "all" {
		cat = 0
	}
	items, err := mc.Service.Recent(ctx, cat, intParam(mc.Params, "count", 10, 1, 50))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`<ul class="ew-recent">`)
	for _, it := range items {
		fmt.Fprintf(&b, `<li><a href="/e/%d">%s</a> <time datetime="%s">%s</time></li>`, it.ID, esc(pick(it.Titles, mc.Lang)),
			it.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"), it.UpdatedAt.Format("2006-01-02"))
	}
	b.WriteString(`</ul>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- panel

type panel struct{}

var panelTypes = []string{"info", "note", "tip", "warning", "success"}

func (panel) Name() string { return "panel" }
func (panel) Labels() map[string]string {
	return lm("Panel (info, note, warning, ...)", "Hinweis-Box (Info, Notiz, Warnung, …)")
}
func (panel) Description() map[string]string {
	return lm("Highlights content in a colored box.", "Hebt Inhalt in einer farbigen Box hervor.")
}
func (panel) HasBody() bool { return true }
func (panel) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "type", Labels: lm("Type", "Art"), Type: "select", Options: panelTypes, Default: "info"},
		{Name: "title", Labels: lm("Title", "Titel"), Type: "text"},
	}
}
func (panel) Render(_ context.Context, mc content.MacroContext) (string, error) {
	t := mc.Params["type"]
	ok := false
	for _, x := range panelTypes {
		ok = ok || x == t
	}
	if !ok {
		t = "info"
	}
	title := ""
	if v := strings.TrimSpace(mc.Params["title"]); v != "" {
		title = `<p class="ew-panel-title"><strong>` + esc(v) + `</strong></p>`
	}
	return `<aside class="ew-macro-panel ew-macro-panel--` + t + `" role="note">` + title + mc.Body + `</aside>`, nil
}

// ---------------------------------------------------------------- expand

type expand struct{}

func (expand) Name() string              { return "expand" }
func (expand) Labels() map[string]string { return lm("Expandable section", "Aufklappbereich") }
func (expand) Description() map[string]string {
	return lm("Content that is hidden until the reader opens it.", "Inhalt, der erst nach dem Aufklappen sichtbar ist.")
}
func (expand) HasBody() bool { return true }
func (expand) Params() []content.MacroParam {
	return []content.MacroParam{{Name: "title", Labels: lm("Title", "Titel"), Type: "text", Required: true, Default: "Details"}}
}
func (expand) Render(_ context.Context, mc content.MacroContext) (string, error) {
	t := strings.TrimSpace(mc.Params["title"])
	if t == "" {
		t = "Details"
	}
	return `<details class="ew-expand"><summary>` + esc(t) + `</summary><div class="ew-expand-body">` + mc.Body + `</div></details>`, nil
}

// ---------------------------------------------------------------- status

type status struct{}

var colors = []string{"grey", "blue", "green", "yellow", "red"}

func (status) Name() string              { return "status" }
func (status) Labels() map[string]string { return lm("Status label", "Status-Label") }
func (status) Description() map[string]string {
	return lm("A colored label such as IN PROGRESS or DONE.", "Ein farbiges Label wie IN ARBEIT oder ERLEDIGT.")
}
func (status) HasBody() bool { return false }
func (status) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "text", Labels: lm("Text", "Text"), Type: "text", Required: true},
		{Name: "color", Labels: lm("Color", "Farbe"), Type: "select", Options: colors, Default: "grey"},
	}
}
func (status) Render(_ context.Context, mc content.MacroContext) (string, error) {
	c := mc.Params["color"]
	ok := false
	for _, x := range colors {
		ok = ok || x == c
	}
	if !ok {
		c = "grey"
	}
	return `<span class="ew-status ew-status--` + c + `">` + esc(strings.ToUpper(strings.TrimSpace(mc.Params["text"]))) + `</span>`, nil
}

// ---------------------------------------------------------------- columns

type columns struct{}

// SplitOnHR splits sanitized HTML at horizontal rules (used for columns,
// cards and other layout macros).
func SplitOnHR(body string) []string {
	parts := regexp.MustCompile(`(?i)<hr\s*/?>`).Split(body, -1)
	out := []string{}
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

func (columns) Name() string              { return "columns" }
func (columns) Labels() map[string]string { return lm("Columns", "Spalten") }
func (columns) Description() map[string]string {
	return lm("Shows content side by side. Separate the columns with a horizontal line.", "Zeigt Inhalte nebeneinander. Spalten mit einer Trennlinie trennen.")
}
func (columns) HasBody() bool                { return true }
func (columns) Params() []content.MacroParam { return nil }
func (columns) Render(_ context.Context, mc content.MacroContext) (string, error) {
	cols := SplitOnHR(mc.Body)
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ew-columns ew-columns--%d">`, min(len(cols), 4))
	for _, c := range cols {
		b.WriteString(`<div class="ew-column">` + c + `</div>`)
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}
