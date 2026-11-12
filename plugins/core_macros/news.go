package core_macros

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/feedfetch"
)

// The macro "news": the newest elements of several sources (categories,
// tags, element types, single elements) together with items of external
// RSS/Atom feeds, as cards, a list or headlines – with the rights of the
// reader (feeds are public).
//
//	::news{source="category:12+sub; tag:hr; feed:https://example.org/rss" limit=8 layout=cards}

type news struct{ feeds *feedfetch.Cache }

func (news) Name() string { return "news" }
func (news) Labels() map[string]string {
	return lm("News from several sources", "Neuigkeiten aus mehreren Quellen")
}
func (news) Description() map[string]string {
	return lm("The newest elements of categories, tags or element types and items of external RSS/Atom feeds, together and newest first.",
		"Die neuesten Elemente aus Kategorien, Schlagwörtern oder Elementtypen und Einträge externer RSS/Atom-Feeds, gemeinsam und neueste zuerst.")
}
func (news) HasBody() bool   { return false }
func (news) Toolbar() string { return "📰" }

// maxNewsFeeds limits the external feeds of one macro (each is fetched).
const maxNewsFeeds = 10

var newsLayouts = []string{"cards", "list", "headlines"}

func (news) Params() []content.MacroParam {
	return append([]content.MacroParam{
		{Name: "source", Labels: lm("Sources (empty: this category with subcategories)", "Quellen (leer: diese Kategorie mit Unterkategorien)"), Type: "custom:sources.feeds"},
		{Name: "layout", Labels: lm("Layout", "Darstellung"), Type: "select", Options: newsLayouts, Default: "cards",
			OptionLabels: map[string]map[string]string{"cards": lm("Cards", "Karten"), "list": lm("List with summary", "Liste mit Zusammenfassung"), "headlines": lm("Headlines", "Schlagzeilen")}},
		{Name: "order", Labels: lm("Newest by", "Neueste nach"), Type: "select", Options: []string{"created", "updated"}, Default: "created",
			OptionLabels: map[string]map[string]string{"created": lm("Created", "Erstellt"), "updated": lm("Changed", "Geändert")}},
		{Name: "title", Labels: lm("Heading", "Überschrift"), Type: "text"},
	}, content.ListParams(6)...)
}

type newsItem struct {
	title, link, summary, image, source string
	date                                time.Time
	external                            bool
	id                                  int64
}

func (n news) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	pg := mc.Paging(6)
	layout := mc.Params["layout"]
	if layout != "list" && layout != "headlines" {
		layout = "cards"
	}
	srcs, err := content.ParseSourcesKinds(mc.Params["source"], []string{"feed"})
	if err != nil {
		return `<p class="ew-notice ew-notice--warning" role="note">` + esc(err.Error()) + `</p>`, nil
	}
	var elems []content.Source
	var feeds []string
	for _, s := range srcs {
		if s.Kind == "feed" {
			feeds = append(feeds, s.Value)
		} else {
			elems = append(elems, s)
		}
	}
	if len(feeds) > maxNewsFeeds {
		return `<p class="ew-notice ew-notice--warning" role="note">` + esc(fmt.Sprintf("at most %d feeds", maxNewsFeeds)) + `</p>`, nil
	}
	if len(feeds) == 0 || len(elems) > 0 {
		elems = content.WithDefault(elems, mc.CategoryID) // only exclusions: from this category
	}
	var items []newsItem
	total := 0
	if len(elems) > 0 {
		set, err := mc.Service.ResolveSources(ctx, elems)
		if err != nil {
			return "", err
		}
		if !set.Empty() {
			order := "created"
			if mc.Params["order"] == "updated" {
				order = "updated"
			}
			// without feeds only the page is loaded; with feeds all are merged by date
			lo := content.ListOptions{Sources: set, Lang: mc.Lang, Order: order, Limit: pg.Limit, SummaryLen: 200, Exclude: mc.ElementID, SkipListings: true}
			if len(feeds) == 0 {
				lo.Limit, lo.Offset = pg.Size(), pg.Offset()
			}
			list, n, err := mc.Service.ListIn(ctx, lo)
			if err != nil {
				return "", err
			}
			total = n
			for _, e := range list {
				d := e.CreatedAt
				if order == "updated" {
					d = e.UpdatedAt
				}
				link := mc.Service.Path(ctx, "element", e.ID, mc.Lang)
				if link == "" {
					link = fmt.Sprintf("/e/%d", e.ID)
				}
				items = append(items, newsItem{title: e.Title, link: link, summary: e.Summary, date: d, source: e.Author, id: e.ID})
			}
		}
	}
	if n.feeds != nil {
		for _, f := range feeds {
			list, err := n.feeds.Items(ctx, f, pg.Limit)
			if err != nil {
				continue
			}
			host := f
			if i := strings.Index(f, "://"); i >= 0 {
				host = strings.TrimPrefix(strings.SplitN(f[i+3:], "/", 2)[0], "www.")
			}
			for _, it := range list {
				ni := newsItem{title: it.Title, link: it.Link, summary: it.Summary, image: it.Image, source: host, external: true}
				if it.Published != nil {
					ni.date = *it.Published
				}
				items = append(items, ni)
			}
		}
	}
	if len(feeds) > 0 {
		sort.SliceStable(items, func(i, j int) bool { return items[i].date.After(items[j].date) })
		items = items[:min(len(items), pg.Limit)]
		total = len(items)
		items = items[min(len(items), pg.Offset()):min(len(items), pg.Offset()+pg.Size())]
	}
	if layout == "cards" {
		for i := range items {
			if items[i].id > 0 {
				items[i].image = elementImage(ctx, mc.Service, items[i].id, mc.Lang)
			}
		}
	}
	var b strings.Builder
	label := strings.TrimSpace(mc.Params["title"])
	if label == "" {
		label = pick(lm("News", "Neuigkeiten"), mc.Lang)
	} else {
		b.WriteString(`<h2 class="ew-news-title">` + esc(label) + `</h2>`)
	}
	if len(items) == 0 {
		b.WriteString(`<p class="ew-muted">` + esc(pick(lm("No news yet.", "Noch keine Neuigkeiten."), mc.Lang)) + `</p>`)
		return b.String(), nil
	}
	datef := "2006-01-02"
	if mc.Lang == "de" {
		datef = "02.01.2006"
	}
	fmt.Fprintf(&b, `<ul class="ew-news ew-news--%s" aria-label="%s">`, layout, esc(label))
	for _, it := range items {
		b.WriteString(`<li class="ew-news-item">`)
		if layout == "cards" && it.image != "" {
			// external pictures only as links would leak the reader: images of feeds are loaded without referrer
			fmt.Fprintf(&b, `<img class="ew-news-img" src="%s" alt="" loading="lazy" referrerpolicy="no-referrer">`, esc(it.image))
		}
		rel := ""
		if it.external {
			rel = ` rel="noopener noreferrer nofollow"`
		}
		if it.link != "" {
			fmt.Fprintf(&b, `<a class="ew-news-link" href="%s"%s>%s</a>`, esc(it.link), rel, esc(it.title))
		} else {
			b.WriteString(`<span class="ew-news-link">` + esc(it.title) + `</span>`)
		}
		meta := []string{}
		if !it.date.IsZero() {
			meta = append(meta, fmt.Sprintf(`<time datetime="%s">%s</time>`, it.date.UTC().Format(time.RFC3339), it.date.Format(datef)))
		}
		if it.source != "" {
			meta = append(meta, esc(it.source))
		}
		if len(meta) > 0 {
			b.WriteString(` <span class="ew-news-meta">` + strings.Join(meta, " · ") + `</span>`)
		}
		if layout != "headlines" && it.summary != "" {
			b.WriteString(`<p class="ew-news-summary">` + esc(it.summary) + `</p>`)
		}
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul>`)
	b.WriteString(mc.Pager(pg, total))
	return b.String(), nil
}

// elementImage is the preview image of an element (or its first picture).
func elementImage(ctx context.Context, svc *content.Service, id int64, lang string) string {
	v, err := svc.GetElement(ctx, id, lang, false)
	if err != nil {
		return ""
	}
	if img := v.PreviewImage; img != "" {
		if !strings.Contains(img, "/") {
			img = "/api/v1/files/" + img
		}
		return img
	}
	return feedfetch.FirstImage(v.HTML)
}
