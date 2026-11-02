// Package content_blog provides the "Blog" element type: the elements of a
// category (optionally with its subcategories) shown as blog posts, newest
// first. Posts are normal elements; "new post" creates a page in the blog's
// category. The blog offers search within the blog and an Atom/RSS feed.
package content_blog

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/pkg/catlist"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// TypeID is the element type.
const TypeID = "content.blog"

// Plugin is the blog plugin.
type Plugin struct{ host *sdk.Host }

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Config is the body of a blog element.
type Config struct {
	catlist.Source
	PerPage       int    `json:"per_page"`       // posts per page (1–50)
	Layout        string `json:"layout"`         // list | cards | compact
	ShowAuthor    bool   `json:"show_author"`    //
	ShowDate      bool   `json:"show_date"`      //
	FullContent   bool   `json:"full_content"`   // whole posts instead of teasers
	SummaryLength int    `json:"summary_length"` // characters of the teaser
	Order         string `json:"order"`          // created (default) | updated
}

func decode(raw []byte) (*Config, error) {
	c := &Config{}
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, apperr.BadRequest("invalid blog settings: %v", err)
	}
	if err := c.Check(); err != nil {
		return nil, err
	}
	if c.PerPage <= 0 || c.PerPage > 50 {
		c.PerPage = 10
	}
	if c.Layout != "cards" && c.Layout != "compact" {
		c.Layout = "list"
	}
	if c.SummaryLength <= 0 || c.SummaryLength > 2000 {
		c.SummaryLength = 280
	}
	if c.Order != "updated" {
		c.Order = "created"
	}
	return c, nil
}

// Type implements content.ElementType.
type Type struct{}

// TypeID implements content.ElementType.
func (Type) TypeID() string { return TypeID }

// Labels implements content.ElementType.
func (Type) Labels() map[string]string { return map[string]string{"en": "Blog", "de": "Blog"} }

// EmptyBody implements content.ElementType. category_id 0 is completed by
// the editor (an existing or a new category).
func (Type) EmptyBody() json.RawMessage {
	return json.RawMessage(`{"category_id":0,"recursive":true,"intro":{"format":"markdown","source":""},"feed":true,"per_page":10,"layout":"list","show_author":true,"show_date":true,"full_content":false,"summary_length":280,"order":"created"}`)
}

// Normalize implements content.ElementType.
func (Type) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	c, err := decode(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

// Text implements content.ElementType (the intro is searchable).
func (Type) Text(raw json.RawMessage) string {
	var c Config
	_ = json.Unmarshal(raw, &c)
	return c.Intro.Source
}

var msgs = map[string]map[string]string{
	"en": {"by": "by", "more": "Read more", "empty": "No posts yet.", "missing": "The category of this blog no longer exists or you may not see it.", "older": "More posts"},
	"de": {"by": "von", "more": "Weiterlesen", "empty": "Noch keine Beiträge.", "missing": "Die Kategorie dieses Blogs existiert nicht mehr oder ist für Sie nicht sichtbar.", "older": "Weitere Beiträge"},
}

func msg(lang, key string) string {
	if m, ok := msgs[lang]; ok {
		return m[key]
	}
	return msgs["en"][key]
}

func fmtDate(t time.Time, lang string) string {
	if lang == "de" {
		return t.Format("02.01.2006")
	}
	return t.Format("Jan 2, 2006")
}

// Render implements content.ElementType: the intro and the first page of
// posts are rendered on the server (works without JavaScript, can be
// cached); the "blog" widget adds search, paging and buttons.
func (Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	c, err := decode(raw)
	if err != nil {
		return "", err
	}
	esc := html.EscapeString
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewb ewb--%s" data-ew-widget="blog" data-element="%d">`, esc(c.Layout), rc.ElementID)
	if intro := c.IntroHTML(); intro != "" {
		b.WriteString(`<div class="ewb-intro">` + intro + `</div>`)
	}
	scope, err := rc.Service.Scope(ctx, c.CategoryID, c.Recursive)
	if err != nil {
		b.WriteString(`<p class="ewb-missing" role="note">` + esc(msg(rc.Lang, "missing")) + `</p></div>`)
		return b.String(), nil
	}
	list, total, err := rc.Service.ListIn(ctx, content.ListOptions{Categories: scope, Lang: rc.Lang, Order: c.Order, Limit: c.PerPage,
		SummaryLen: c.SummaryLength, WithContent: c.FullContent, Exclude: rc.ElementID})
	if err != nil {
		return "", err
	}
	b.WriteString(`<div class="ewb-posts">`)
	if len(list) == 0 {
		b.WriteString(`<p class="ewb-empty">` + esc(msg(rc.Lang, "empty")) + `</p>`)
	}
	for _, p := range list {
		b.WriteString(RenderPost(&p, c, rc.Lang))
	}
	b.WriteString(`</div>`)
	if total > len(list) {
		fmt.Fprintf(&b, `<p class="ewb-more-static"><a href="/c/%d">%s</a></p>`, c.CategoryID, esc(msg(rc.Lang, "older")))
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}

// RenderPost renders one post as an article (also used by the widget's
// server-side counterpart for consistency).
func RenderPost(p *content.ListedElement, c *Config, lang string) string {
	esc := html.EscapeString
	var b strings.Builder
	fmt.Fprintf(&b, `<article class="ewb-post" lang="%s"><h2 class="ewb-title"><a href="/e/%d">%s</a></h2>`, esc(p.Lang), p.ID, esc(p.Title))
	if c.ShowDate || c.ShowAuthor {
		b.WriteString(`<p class="ewb-meta">`)
		if c.ShowDate {
			d := p.CreatedAt
			if c.Order == "updated" {
				d = p.UpdatedAt
			}
			fmt.Fprintf(&b, `<time datetime="%s">%s</time>`, d.UTC().Format(time.RFC3339), esc(fmtDate(d, lang)))
		}
		if c.ShowAuthor && p.Author != "" {
			if c.ShowDate {
				b.WriteString(" · ")
			}
			b.WriteString(esc(msg(lang, "by")) + " " + esc(p.Author))
		}
		b.WriteString(`</p>`)
	}
	if c.FullContent && p.HTML != "" {
		b.WriteString(`<div class="ewb-content">` + p.HTML + `</div>`)
	} else if p.Summary != "" {
		fmt.Fprintf(&b, `<p class="ewb-summary">%s</p><p class="ewb-readmore"><a href="/e/%d">%s<span class="sr-only">: %s</span></a></p>`,
			esc(p.Summary), p.ID, esc(msg(lang, "more")), esc(p.Title))
	}
	if len(p.Tags) > 0 {
		b.WriteString(`<ul class="ewb-tags">`)
		for _, t := range p.Tags {
			b.WriteString(`<li class="ew-tag">` + esc(t) + `</li>`)
		}
		b.WriteString(`</ul>`)
	}
	b.WriteString(`</article>`)
	return b.String()
}

func (p *Plugin) load(ctx context.Context, id int64, lang string) (*Config, string, error) {
	var raw json.RawMessage
	shown, err := catlist.Load(ctx, p.host, id, TypeID, lang, &raw)
	if err != nil {
		return nil, "", err
	}
	c, err := decode(raw)
	return c, shown, err
}

// Setup registers the element type and the API.
func (p *Plugin) Setup(hs *sdk.Host) error {
	p.host = hs
	hs.RegisterElementType(Type{})
	catlist.Routes(hs, "blog", "posts", func(ctx context.Context, id int64, lang string) (*catlist.Source, string, error) {
		c, shown, err := p.load(ctx, id, lang)
		if err != nil {
			return nil, "", err
		}
		return &c.Source, shown, nil
	})

	// GET /elements/{id}/posts?page=1&q=&tag=&lang= lists posts (search within the blog with q).
	hs.Handle("GET", "/elements/{id}/posts", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		q := r.URL.Query()
		c, shown, err := p.load(ctx, id, q.Get("lang"))
		if err != nil {
			return nil, err
		}
		lang := q.Get("lang")
		if lang == "" {
			lang = shown
		}
		info, err := catlist.Describe(ctx, hs, &c.Source)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"config": c, "info": info, "items": []content.ListedElement{}, "total": 0, "page": 1, "pages": 0, "per_page": c.PerPage}
		if info.Missing {
			return out, nil
		}
		page := h.QueryInt(r, "page", 1)
		if page < 1 {
			page = 1
		}
		if term := strings.TrimSpace(q.Get("q")); term != "" {
			res, err := hs.Content.SearchIn(ctx, term, h.QueryList(r, "tags"), lang, 50, info.Scope)
			if err != nil {
				return nil, err
			}
			hits := res.Elements[:0]
			for _, x := range res.Elements {
				if x.ElementID != id {
					hits = append(hits, x)
				}
			}
			out["hits"] = hits
			out["total"] = len(hits)
			return out, nil
		}
		list, total, err := hs.Content.ListIn(ctx, content.ListOptions{Categories: info.Scope, Lang: lang, Order: c.Order, Tag: q.Get("tag"),
			Limit: c.PerPage, Offset: (page - 1) * c.PerPage, SummaryLen: c.SummaryLength, WithContent: c.FullContent, Exclude: id})
		if err != nil {
			return nil, err
		}
		out["items"], out["total"], out["page"], out["pages"] = list, total, page, (total+c.PerPage-1)/c.PerPage
		return out, nil
	})

	// GET /elements/{id}/feed?format=atom|rss&lang=&token= (personal link with token).
	hs.Handle("GET", "/elements/{id}/feed", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if r, err = catlist.FeedContext(hs, r); err != nil {
			return nil, err
		}
		c, _, err := p.load(r.Context(), id, r.URL.Query().Get("lang"))
		if err != nil {
			return nil, err
		}
		v, err := hs.Content.GetElement(r.Context(), id, r.URL.Query().Get("lang"), false)
		if err != nil {
			return nil, err
		}
		return catlist.WriteFeed(w, r, hs, id, &c.Source, catlist.FeedOptions{Title: v.Title, Order: c.Order, WithContent: true, Limit: 30, Exclude: id})
	})
	return nil
}

// Prepare implements content.Preparer: without a chosen category a new one
// (named like the element) is created for the entries.
func (Type) Prepare(ctx context.Context, svc *content.Service, categoryID int64, lang, title string, body json.RawMessage) (json.RawMessage, error) {
	return catlist.PrepareSource(ctx, svc, categoryID, lang, title, body)
}
