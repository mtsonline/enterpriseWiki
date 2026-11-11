package content_faq

import (
	"context"
	"html"
	"slices"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

// faqMacro (::faq) shows the questions of any number of sources together,
// one expandable section per category, in the look of the FAQ element.
// Only questions the viewer may read are listed.
type faqMacro struct{}

func lm(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }

func (faqMacro) Name() string { return "faq" }
func (faqMacro) Labels() map[string]string {
	return lm("FAQ from several sources", "FAQ aus mehreren Quellen")
}
func (faqMacro) Description() map[string]string {
	return lm("Questions of several categories, tags or element types together, one section per category.",
		"Fragen mehrerer Kategorien, Schlagwörter oder Elementtypen gemeinsam, ein Abschnitt pro Kategorie.")
}
func (faqMacro) HasBody() bool   { return false }
func (faqMacro) Toolbar() string { return "❓" }

func (faqMacro) Params() []content.MacroParam {
	yesNo := map[string]map[string]string{"yes": lm("Yes", "Ja"), "no": lm("No", "Nein")}
	return []content.MacroParam{
		{Name: "source", Labels: lm("Sources (empty: this category with subcategories)", "Quellen (leer: diese Kategorie mit Unterkategorien)"), Type: "custom:sources"},
		{Name: "open", Labels: lm("Sections expanded", "Abschnitte aufgeklappt"), Type: "select", Options: []string{"yes", "no"}, Default: "yes", OptionLabels: yesNo},
		{Name: "numbering", Labels: lm("Numbering", "Nummerierung"), Type: "select", Options: []string{"yes", "no"}, Default: "no", OptionLabels: yesNo},
		{Name: "limit", Labels: lm("Maximum questions", "Höchstens Fragen"), Type: "number", Default: "100"},
		{Name: "title", Labels: lm("Heading", "Überschrift"), Type: "text"},
	}
}

func (faqMacro) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	esc := html.EscapeString
	srcs, err := content.ParseSources(mc.Params["source"])
	if err != nil {
		return `<p class="ew-notice ew-notice--warning" role="note">` + esc(err.Error()) + `</p>`, nil
	}
	if len(srcs) == 0 {
		if mc.CategoryID <= 0 {
			return "", nil
		}
		srcs = []content.Source{{Kind: "category", ID: mc.CategoryID, Sub: true}}
	}
	set, err := mc.Service.ResolveSources(ctx, srcs)
	if err != nil {
		return "", err
	}
	limit, _ := strconv.Atoi(mc.Params["limit"])
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var list []content.ListedElement
	if !set.Empty() {
		list, _, err = mc.Service.ListIn(ctx, content.ListOptions{Sources: set, Lang: mc.Lang, Order: "position", Limit: limit, WithContent: true, Exclude: mc.ElementID, SkipListings: true})
		if err != nil {
			return "", err
		}
	}
	// one section per category, in the order the categories first appear
	byCat := map[int64][]Question{}
	var order []int64
	for _, e := range list {
		if !slices.Contains(order, e.CategoryID) {
			order = append(order, e.CategoryID)
		}
		byCat[e.CategoryID] = append(byCat[e.CategoryID], Question{ID: e.ID, Title: e.Title, Lang: e.Lang, HTML: e.HTML, Tags: e.Tags, UpdatedAt: e.UpdatedAt})
	}
	root := int64(0)
	secs := []Section{{ID: 0, Depth: 0, Questions: []Question{}}}
	if len(order) > 0 {
		cats, err := mc.Service.CategoriesByID(ctx, order)
		if err != nil {
			return "", err
		}
		titles := map[int64]content.Category{}
		for _, c := range cats {
			titles[c.ID] = c
		}
		for i, id := range order {
			c := titles[id]
			secs = append(secs, Section{ID: id, ParentID: &root, Depth: 1, Title: pick(c.Titles, mc.Lang), Description: pick(c.Descriptions, mc.Lang),
				Questions: byCat[id], Number: strconv.Itoa(i + 1)})
		}
	}
	cfg := &Config{OpenSections: mc.Params["open"] != "no", Numbering: mc.Params["numbering"] == "yes"}
	var b strings.Builder
	title := strings.TrimSpace(mc.Params["title"])
	b.WriteString(`<section class="ewq ewq-macro" aria-label="` + esc(firstNonEmpty(title, "FAQ")) + `">`)
	if title != "" {
		b.WriteString(`<h2 class="ewq-heading">` + esc(title) + `</h2>`)
	}
	b.WriteString(`<div class="ewq-body">`)
	if len(list) == 0 {
		b.WriteString(`<p class="ewq-empty">` + esc(msg(mc.Lang, "empty")) + `</p>`)
	}
	var body strings.Builder
	RenderSections(&body, secs, cfg)
	out := body.String()
	if title != "" { // sections one level below the macro's heading
		out = strings.ReplaceAll(strings.ReplaceAll(out, `"ewq-section-title"><h2>`, `"ewq-section-title"><h3>`), `</h2></summary>`, `</h3></summary>`)
	}
	b.WriteString(out)
	b.WriteString(`</div></section>`)
	return b.String(), nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
