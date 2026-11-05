package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Prerendering: guests (search engines, visitors without JavaScript, link
// previews) receive the content of a page as plain HTML inside #app plus
// title, description, canonical link and Open Graph tags. The frontend
// replaces #app when it starts. The HTML is always built as seen by a guest
// and cached in the guest cache (cleared on every change).

// Prerendered is a page as seen by guests.
type Prerendered struct {
	Path        string `json:"path"`
	Lang        string `json:"lang"`
	Title       string `json:"title"`       // page title without the site name ("" on the start page overview)
	Description string `json:"description"` // plain text, max. ~160 characters
	Canonical   string `json:"canonical"`   // absolute URL
	Type        string `json:"type"`        // element | category | home
	HTML        string `json:"html"`        // content of #app
	Image       string `json:"image"`       // absolute URL of the preview image ("" = none)
}

var imgSrcRe = regexp.MustCompile(`(?i)<img\b[^>]*?\ssrc="([^"]+)"`)

// firstImage returns the first image of a page that link previews can load
// (files of the wiki or http(s) images; no data: URLs).
func firstImage(htmlSrc string) string {
	for _, m := range imgSrcRe.FindAllStringSubmatch(htmlSrc, 8) {
		src := html.UnescapeString(m[1])
		if strings.HasPrefix(src, "/") && !strings.HasPrefix(src, "//") || strings.HasPrefix(src, "https://") || strings.HasPrefix(src, "http://") {
			return src
		}
	}
	return ""
}

// absURL makes a site path absolute.
func absURL(base, u string) string {
	if u == "" || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return strings.TrimRight(base, "/") + u
}

// paths of the single page app that are not content pages
var reservedPaths = map[string]bool{"search": true, "login": true, "profile": true, "trash": true, "admin": true, "assets": true, "api": true, "favicon.svg": true, "robots.txt": true}

// guestLang picks the language for a guest: ?lang=, Accept-Language, default.
func (s *Server) guestLang(r *http.Request) string {
	langs := s.app.Content.Languages()
	valid := func(l string) bool {
		for _, x := range langs {
			if x == l {
				return true
			}
		}
		return false
	}
	if l := r.URL.Query().Get("lang"); valid(l) {
		return l
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		base, _, _ := strings.Cut(strings.ToLower(tag), "-")
		if valid(base) {
			return base
		}
	}
	return s.app.Content.DefaultLanguage()
}

// prerenderCached returns the guest view of the requested page (cached), or nil.
func (s *Server) prerenderCached(r *http.Request) *Prerendered {
	lang := s.guestLang(r)
	b := s.app.Cache.Fragment("prerender|"+r.URL.Path+"|"+lang, func() ([]byte, bool) {
		p := s.Prerender(r.Context(), r.URL.Path, lang)
		if p == nil {
			return []byte("null"), true
		}
		out, err := json.Marshal(p)
		return out, err == nil
	})
	var p *Prerendered
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	return p
}

// Prerender builds the guest view of a page path (/, /e/ID, /c/ID or a
// short URL). It returns nil for other paths and pages guests may not see.
func (s *Server) Prerender(ctx context.Context, urlPath, lang string) *Prerendered {
	ctx = identity.With(ctx, nil) // always as a guest
	c := s.app.Config.Get()
	site := c.UI.SiteName
	p := &Prerendered{Path: urlPath, Lang: lang, Canonical: strings.TrimRight(c.Server.BaseURL, "/") + urlPath}
	var main string
	var err error
	segs := strings.Split(strings.Trim(urlPath, "/"), "/")
	switch {
	case urlPath == "/":
		switch {
		case c.UI.HomeElement != 0:
			main, err = s.prerenderElement(ctx, p, c.UI.HomeElement, lang)
		case c.UI.HomeCategory != 0:
			main, err = s.prerenderCategory(ctx, p, c.UI.HomeCategory, lang)
		default:
			main, err = s.prerenderOverview(ctx, p, lang)
		}
		p.Type = "home"
	case len(segs) == 2 && (segs[0] == "e" || segs[0] == "c"):
		id, perr := strconv.ParseInt(segs[1], 10, 64)
		if perr != nil || id <= 0 {
			return nil
		}
		if segs[0] == "e" {
			main, err = s.prerenderElement(ctx, p, id, lang)
			if u := s.app.Content.Path(ctx, "element", id, lang); u != "" {
				p.Canonical = strings.TrimRight(c.Server.BaseURL, "/") + u
			}
		} else {
			main, err = s.prerenderCategory(ctx, p, id, lang)
			if u := s.app.Content.Path(ctx, "category", id, lang); u != "" {
				p.Canonical = strings.TrimRight(c.Server.BaseURL, "/") + u
			}
		}
	case !reservedPaths[segs[0]] && !strings.Contains(urlPath, "."):
		res, rerr := s.app.Content.Resolve(ctx, strings.Trim(urlPath, "/"))
		if rerr != nil {
			return nil
		}
		if res.Type == "element" {
			main, err = s.prerenderElement(ctx, p, res.ID, lang)
		} else {
			main, err = s.prerenderCategory(ctx, p, res.ID, lang)
		}
	default:
		return nil
	}
	if err != nil {
		return nil
	}
	if p.Image == "" {
		p.Image = c.UI.PreviewImage
	}
	p.Image = absURL(c.Server.BaseURL, p.Image)
	nav := s.prerenderNav(ctx, lang)
	var b strings.Builder
	b.WriteString(`<div class="ew-prerender"><header class="ew-prerender-head"><a class="ew-prerender-site" href="/">`)
	b.WriteString(html.EscapeString(site))
	b.WriteString(`</a>`)
	b.WriteString(nav)
	b.WriteString(`</header><main id="main" class="ew-prerender-main">`)
	b.WriteString(main)
	b.WriteString(`</main></div>`)
	p.HTML = b.String()
	return p
}

func pickTitle(m map[string]string, lang, def string) string {
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

func elementHref(id int64, short string) string {
	if short != "" {
		return "/" + short
	}
	return fmt.Sprintf("/e/%d", id)
}

func categoryHref(id int64, short string) string {
	if short != "" {
		return "/" + short
	}
	return fmt.Sprintf("/c/%d", id)
}

func (s *Server) prerenderNav(ctx context.Context, lang string) string {
	cats, err := s.app.Content.Children(ctx, 0)
	if err != nil || len(cats) == 0 {
		return ""
	}
	def := s.app.Content.DefaultLanguage()
	var b strings.Builder
	b.WriteString(`<nav aria-label="Navigation"><ul>`)
	for _, c := range cats {
		fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`, html.EscapeString(categoryHref(c.ID, c.ShortURL)), html.EscapeString(pickTitle(c.Titles, lang, def)))
	}
	b.WriteString(`</ul></nav>`)
	return b.String()
}

func (s *Server) prerenderElement(ctx context.Context, p *Prerendered, id int64, lang string) (string, error) {
	v, err := s.app.Content.GetElement(ctx, id, lang, false)
	if err != nil {
		return "", err
	}
	p.Type, p.Title, p.Lang = "element", v.Title, v.ShownLang
	p.Description = plainSummary(v.HTML, 160)
	p.Image = firstNonEmpty(v.PreviewImage, firstImage(v.HTML))
	return `<article><h1>` + html.EscapeString(v.Title) + `</h1>` + v.HTML + `</article>`, nil
}

func (s *Server) prerenderCategory(ctx context.Context, p *Prerendered, id int64, lang string) (string, error) {
	c, err := s.app.Content.GetCategory(ctx, id)
	if err != nil {
		return "", err
	}
	def := s.app.Content.DefaultLanguage()
	p.Type, p.Title = "category", pickTitle(c.Titles, lang, def)
	desc := pickTitle(c.Descriptions, lang, def)
	var b strings.Builder
	b.WriteString(`<article><h1>` + html.EscapeString(p.Title) + `</h1>`)
	if desc != "" {
		b.WriteString(`<p>` + html.EscapeString(desc) + `</p>`)
	}
	var skip int64
	if c.DefaultElementID != nil {
		if v, err := s.app.Content.GetElement(ctx, *c.DefaultElementID, lang, false); err == nil {
			skip = v.ID
			b.WriteString(v.HTML)
			p.Image = firstNonEmpty(v.PreviewImage, firstImage(v.HTML))
			if desc == "" {
				desc = plainSummary(v.HTML, 160)
			}
		}
	}
	p.Description = plainSummary(html.EscapeString(desc), 160)
	if subs, err := s.app.Content.Children(ctx, id); err == nil && len(subs) > 0 {
		b.WriteString(`<ul class="ew-prerender-list">`)
		for _, sc := range subs {
			fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`, html.EscapeString(categoryHref(sc.ID, sc.ShortURL)), html.EscapeString(pickTitle(sc.Titles, lang, def)))
		}
		b.WriteString(`</ul>`)
	}
	if els, err := s.app.Content.ListElements(ctx, id); err == nil {
		var items []string
		for _, e := range els {
			if e.ID != skip {
				items = append(items, fmt.Sprintf(`<li><a href="%s">%s</a></li>`, html.EscapeString(elementHref(e.ID, e.ShortURL)), html.EscapeString(pickTitle(e.Titles, lang, e.OriginalLang))))
			}
		}
		if len(items) > 0 {
			b.WriteString(`<ul class="ew-prerender-list">` + strings.Join(items, "") + `</ul>`)
		}
	}
	b.WriteString(`</article>`)
	return b.String(), nil
}

func (s *Server) prerenderOverview(ctx context.Context, p *Prerendered, lang string) (string, error) {
	cats, err := s.app.Content.Children(ctx, 0)
	if err != nil {
		return "", err
	}
	def := s.app.Content.DefaultLanguage()
	var b strings.Builder
	b.WriteString(`<h1>` + html.EscapeString(s.app.Config.Get().UI.SiteName) + `</h1><ul class="ew-prerender-list">`)
	for _, c := range cats {
		fmt.Fprintf(&b, `<li><a href="%s">%s</a>`, html.EscapeString(categoryHref(c.ID, c.ShortURL)), html.EscapeString(pickTitle(c.Titles, lang, def)))
		if d := pickTitle(c.Descriptions, lang, def); d != "" {
			b.WriteString(` – ` + html.EscapeString(d))
		}
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul>`)
	return b.String(), nil
}

var (
	htmlTagRe = regexp.MustCompile(`(?i)<html[^>]*>`)
	tagRe     = regexp.MustCompile(`(?s)<(script|style)\b.*?</(script|style)>|<[^>]*>`)
	spaceRe   = regexp.MustCompile(`\s+`)
)

// plainSummary turns HTML into plain text of at most max characters, cut at a word.
func plainSummary(h string, max int) string {
	t := strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(tagRe.ReplaceAllString(h, " ")), " "))
	if utf8.RuneCountInString(t) <= max {
		return t
	}
	r := []rune(t)[:max]
	if i := strings.LastIndexByte(string(r), ' '); i > max/2 {
		return strings.TrimRight(string(r)[:i], " ,.;:–-") + " …"
	}
	return string(r) + "…"
}

// injectPrerender puts the guest view into the page skeleton.
func injectPrerender(page []byte, p *Prerendered, site string) []byte {
	esc := html.EscapeString
	title := site
	if p.Title != "" && p.Title != site {
		title = p.Title + " – " + site
	}
	var head strings.Builder
	head.WriteString(`<title>` + esc(title) + `</title>`)
	if p.Description != "" {
		head.WriteString(`<meta name="description" content="` + esc(p.Description) + `">`)
		head.WriteString(`<meta property="og:description" content="` + esc(p.Description) + `">`)
	}
	head.WriteString(`<link rel="canonical" href="` + esc(p.Canonical) + `">`)
	head.WriteString(`<meta property="og:title" content="` + esc(firstNonEmpty(p.Title, site)) + `">`)
	head.WriteString(`<meta property="og:site_name" content="` + esc(site) + `">`)
	head.WriteString(`<meta property="og:url" content="` + esc(p.Canonical) + `">`)
	ogType := "website"
	if p.Type == "element" {
		ogType = "article"
	}
	head.WriteString(`<meta property="og:type" content="` + ogType + `">`)
	if p.Image != "" {
		head.WriteString(`<meta property="og:image" content="` + esc(p.Image) + `">`)
		head.WriteString(`<meta name="twitter:card" content="summary_large_image">`)
		head.WriteString(`<meta name="twitter:image" content="` + esc(p.Image) + `">`)
	} else {
		head.WriteString(`<meta name="twitter:card" content="summary">`)
	}
	page = replaceTag(page, "<title>", "</title>", head.String())
	page = htmlTagRe.ReplaceAll(page, []byte(`<html lang="`+esc(p.Lang)+`">`))
	if i := bytes.Index(page, []byte(`id="app"`)); i >= 0 {
		if j := bytes.IndexByte(page[i:], '>'); j >= 0 {
			at := i + j + 1
			page = append(page[:at:at], append([]byte(p.HTML), page[at:]...)...)
		}
	}
	return page
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// replaceTag replaces <open>…</close> (first occurrence) with repl.
func replaceTag(b []byte, open, close, repl string) []byte {
	i := bytes.Index(b, []byte(open))
	if i < 0 {
		return b
	}
	j := bytes.Index(b[i:], []byte(close))
	if j < 0 {
		return b
	}
	out := append([]byte{}, b[:i]...)
	out = append(out, repl...)
	return append(out, b[i+j+len(close):]...)
}

// prerenderAPI is GET /api/v1/prerender?path=/e/1&lang=de: the guest view of
// a page (for checking what search engines see).
func (s *Server) prerenderAPI(w http.ResponseWriter, r *http.Request) (any, error) {
	path := r.URL.Query().Get("path")
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		path = "/"
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	p := s.Prerender(r.Context(), path, s.guestLang(r))
	if p == nil {
		return map[string]any{"path": path, "available": false}, nil
	}
	return map[string]any{"path": path, "available": true, "page": p}, nil
}
