package core_macros

// Lists of spaces (plan of blocks, 3.2): the spaces the reader may see and
// the details of the space a page is in.

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

func spaceHref(id int64, key string) string {
	if key != "" {
		return "/s/" + key
	}
	return fmt.Sprintf("/c/%d", id)
}

// ---------------------------------------------------------------- space list

type spacelist struct{}

func (spacelist) Name() string              { return "spacelist" }
func (spacelist) Group() string             { return "lists" }
func (spacelist) Labels() map[string]string { return lm("List of spaces", "Bereichs-Liste") }
func (spacelist) Description() map[string]string {
	return lm("The spaces of the wiki the reader may see, as cards or a list, with their description.",
		"Die Bereiche des Wikis, die die lesende Person sehen darf, als Karten oder Liste, mit ihrer Beschreibung.")
}
func (spacelist) HasBody() bool { return false }
func (spacelist) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "layout", Labels: lm("Display", "Darstellung"), Type: "select", Options: []string{"cards", "list"}, Default: "cards",
			OptionLabels: map[string]map[string]string{"cards": lm("Cards", "Karten"), "list": lm("List", "Liste")}},
		{Name: "max", Labels: lm("At most (1–200)", "Höchstens (1–200)"), Type: "number", Default: "50"},
		{Name: "archived", Labels: lm("Also archived spaces", "Auch archivierte Bereiche"), Type: "bool"},
		{Name: "personal", Labels: lm("Also personal spaces", "Auch persönliche Bereiche"), Type: "bool"},
		{Name: "label", Labels: lm("Name for screen readers", "Name für Screenreader"), Type: "text"},
	}
}

func (spacelist) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	label := strings.TrimSpace(mc.Params["label"])
	if utf8.RuneCountInString(label) > 120 {
		return "", apperr.BadRequest("label: at most %d characters", 120)
	}
	if label == "" {
		label = tr(mc.Lang, "Spaces", "Bereiche")
	}
	list, err := mc.Service.SpaceList(ctx, mc.Lang, mc.Params["archived"] == "true", mc.Params["personal"] == "true", countParam(mc.Params["max"], 50, content.SpaceListMax))
	if err != nil {
		return "", err
	}
	layout := "cards"
	if mc.Params["layout"] == "list" {
		layout = "list"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<nav class="ew-spacelist ew-spacelist--%s" aria-label="%s">`, layout, esc(label))
	if len(list) == 0 {
		b.WriteString(`<p class="ew-muted">` + esc(tr(mc.Lang, "No spaces to show.", "Keine Bereiche zu zeigen.")) + `</p></nav>`)
		return b.String(), nil
	}
	b.WriteString(`<ul>`)
	for _, sp := range list {
		b.WriteString(`<li class="ew-spacelist-item">`)
		if sp.Logo != "" && layout == "cards" {
			// the name follows: the logo is decoration
			fmt.Fprintf(&b, `<img class="ew-spacelist-logo" src="%s" alt="" loading="lazy" width="48" height="48">`, esc(sp.Logo))
		}
		fmt.Fprintf(&b, `<a class="ew-spacelist-name" href="%s">%s</a>`, esc(spaceHref(sp.ID, sp.Key)), esc(sp.Title))
		if sp.Archived {
			b.WriteString(` <span class="ew-badge">` + esc(tr(mc.Lang, "archived", "archiviert")) + `</span>`)
		}
		if d := strings.TrimSpace(sp.Description); d != "" {
			b.WriteString(`<p class="ew-spacelist-desc">` + esc(d) + `</p>`)
		}
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul></nav>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- space details

type spacedetails struct{}

func (spacedetails) Name() string              { return "spacedetails" }
func (spacedetails) Group() string             { return "lists" }
func (spacedetails) Labels() map[string]string { return lm("Details of the space", "Bereichs-Details") }
func (spacedetails) Description() map[string]string {
	return lm("Name, key, description and address of the space a page is in (or of a chosen category).",
		"Name, Kürzel, Beschreibung und Adresse des Bereichs, in dem eine Seite liegt (oder einer gewählten Kategorie).")
}
func (spacedetails) HasBody() bool { return false }
func (spacedetails) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "category", Labels: lm("Category (empty: the one of this page)", "Kategorie (leer: die dieser Seite)"), Type: "custom:category"},
	}
}

func (spacedetails) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	cat := idParam(mc.Params["category"])
	if cat == 0 {
		cat = mc.CategoryID
	}
	if cat == 0 {
		return "", apperr.BadRequest("choose a category (here is no category of its own)")
	}
	sp, err := mc.Service.SpaceOf(ctx, cat)
	if err != nil || sp == nil {
		// not there or not visible: nothing about it
		return "", apperr.BadRequest("the category %d is not there or not visible to you", cat)
	}
	def := mc.Service.DefaultLanguage()
	title, desc := pickText(sp.Titles, mc.Lang, def), pickText(sp.Descriptions, mc.Lang, def)
	var b strings.Builder
	b.WriteString(`<section class="ew-spacedetails" aria-label="` + esc(tr(mc.Lang, "Space", "Bereich")+": "+title) + `"><dl>`)
	row := func(k, v string) { fmt.Fprintf(&b, `<dt>%s</dt><dd>%s</dd>`, esc(k), v) }
	row(tr(mc.Lang, "Space", "Bereich"), fmt.Sprintf(`<a href="%s">%s</a>`, esc(spaceHref(sp.ID, sp.Key)), esc(title)))
	if sp.Key != "" {
		row(tr(mc.Lang, "Key", "Kürzel"), `<code>`+esc(sp.Key)+`</code>`)
	}
	if strings.TrimSpace(desc) != "" {
		row(tr(mc.Lang, "Description", "Beschreibung"), esc(desc))
	}
	if sp.Archived {
		row(tr(mc.Lang, "State", "Zustand"), esc(tr(mc.Lang, "archived (read only)", "archiviert (nur lesen)")))
	}
	b.WriteString(`</dl></section>`)
	return b.String(), nil
}

// pickText: the text in lang, the default language or any language.
func pickText(m map[string]string, lang, def string) string {
	if v := m[lang]; v != "" {
		return v
	}
	if v := m[def]; v != "" {
		return v
	}
	for _, v := range m {
		if v != "" {
			return v
		}
	}
	return ""
}
