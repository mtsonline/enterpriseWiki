package content_blog

import (
	"context"
	"html"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

// postsMacro (::blog) shows the posts of any number of sources together –
// categories, tags, element types or single elements – in the look of the
// blog element. Only elements the viewer may read are listed.
type postsMacro struct{}

func lm(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }

func (postsMacro) Name() string { return "blog" }
func (postsMacro) Labels() map[string]string {
	return lm("Blog posts from several sources", "Blogbeiträge aus mehreren Quellen")
}
func (postsMacro) Description() map[string]string {
	return lm("Posts of several categories, tags or element types together, newest first, in the look of the blog.",
		"Beiträge mehrerer Kategorien, Schlagwörter oder Elementtypen gemeinsam, neueste zuerst, im Aussehen des Blogs.")
}
func (postsMacro) HasBody() bool   { return false }
func (postsMacro) Toolbar() string { return "✍️" }

func (postsMacro) Params() []content.MacroParam {
	yesNo := map[string]map[string]string{"yes": lm("Yes", "Ja"), "no": lm("No", "Nein")}
	return append([]content.MacroParam{
		{Name: "source", Labels: lm("Sources (empty: this category with subcategories)", "Quellen (leer: diese Kategorie mit Unterkategorien)"), Type: "custom:sources"},
		{Name: "layout", Labels: lm("Layout", "Darstellung"), Type: "select", Options: []string{"list", "cards", "compact"}, Default: "list",
			OptionLabels: map[string]map[string]string{"list": lm("List", "Liste"), "cards": lm("Cards", "Karten"), "compact": lm("Compact", "Kompakt")}},
		{Name: "order", Labels: lm("Newest by", "Neueste nach"), Type: "select", Options: []string{"created", "updated"}, Default: "created",
			OptionLabels: map[string]map[string]string{"created": lm("Created", "Erstellt"), "updated": lm("Changed", "Geändert")}},
		{Name: "date", Labels: lm("Show date", "Datum zeigen"), Type: "select", Options: []string{"yes", "no"}, Default: "yes", OptionLabels: yesNo},
		{Name: "author", Labels: lm("Show author", "Autor/in zeigen"), Type: "select", Options: []string{"yes", "no"}, Default: "yes", OptionLabels: yesNo},
		{Name: "title", Labels: lm("Heading", "Überschrift"), Type: "text"},
	}, content.ListParams(5)...)
}

func (postsMacro) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	esc := html.EscapeString
	srcs, err := content.ParseSources(mc.Params["source"])
	if err != nil {
		return `<p class="ew-notice ew-notice--warning" role="note">` + esc(err.Error()) + `</p>`, nil
	}
	if srcs = content.WithDefault(srcs, mc.CategoryID); len(srcs) == 0 {
		return "", nil
	}
	set, err := mc.Service.ResolveSources(ctx, srcs)
	if err != nil {
		return "", err
	}
	pg := mc.Paging(5)
	c := &Config{Layout: mc.Params["layout"], Order: mc.Params["order"], ShowDate: mc.Params["date"] != "no", ShowAuthor: mc.Params["author"] != "no", SummaryLength: 280}
	if c.Layout != "cards" && c.Layout != "compact" {
		c.Layout = "list"
	}
	if c.Order != "updated" {
		c.Order = "created"
	}
	var list []content.ListedElement
	total := 0
	if !set.Empty() {
		list, total, err = mc.Service.ListIn(ctx, content.ListOptions{Sources: set, Lang: mc.Lang, Order: c.Order, Limit: pg.Size(), Offset: pg.Offset(), SummaryLen: c.SummaryLength, Exclude: mc.ElementID, SkipListings: true})
		if err != nil {
			return "", err
		}
	}
	var b strings.Builder
	b.WriteString(`<section class="ewb ewb--` + esc(c.Layout) + ` ewb-macro"`)
	title := strings.TrimSpace(mc.Params["title"])
	if title != "" {
		b.WriteString(` aria-label="` + esc(title) + `"><h2 class="ewb-heading">` + esc(title) + `</h2>`)
	} else {
		b.WriteString(` aria-label="` + esc(msg(mc.Lang, "posts")) + `">`)
	}
	b.WriteString(`<div class="ewb-posts">`)
	if len(list) == 0 {
		b.WriteString(`<p class="ewb-empty">` + esc(msg(mc.Lang, "empty")) + `</p>`)
	}
	for i := range list {
		// headings inside the macro are one level below its own heading
		post := RenderPost(&list[i], c, mc.Lang)
		if title != "" {
			post = strings.Replace(strings.Replace(post, `<h2 class="ewb-title">`, `<h3 class="ewb-title">`, 1), `</a></h2>`, `</a></h3>`, 1)
		}
		b.WriteString(post)
	}
	b.WriteString(`</div>` + mc.Pager(pg, total) + `</section>`)
	return b.String(), nil
}
