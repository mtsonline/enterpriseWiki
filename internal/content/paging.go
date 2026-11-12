package content

import (
	"context"
	"fmt"
	"html"
	"strconv"
)

// Paging of lists from sources (macros, blog, FAQ …): the editor chooses
// the most entries in total ("limit", up to the setting lists.max_items)
// and whether the list is split into pages ("paginate") from how many
// entries on ("per_page"). Pages are changed without reloading: the
// browser asks GET /api/v1/elements/{id}/macro/{index}?page=N for the one
// macro again; without a script the links (?mp=INDEX-N) open the page.

// Paging is the resolved paging of one list.
type Paging struct {
	Limit    int  // most entries in total
	Paginate bool // split into pages
	PerPage  int  // entries per page (when paginating)
	Page     int  // current page (1 …)
}

// Offset is the first entry of the current page.
func (p Paging) Offset() int {
	if !p.Paginate {
		return 0
	}
	return (p.Page - 1) * p.PerPage
}

// Size is the number of entries to load for the current page.
func (p Paging) Size() int {
	if !p.Paginate {
		return p.Limit
	}
	return max(0, min(p.PerPage, p.Limit-p.Offset()))
}

// Pages is the number of pages for total entries.
func (p Paging) Pages(total int) int {
	total = min(total, p.Limit)
	if !p.Paginate || total <= p.PerPage {
		return 1
	}
	return (total + p.PerPage - 1) / p.PerPage
}

// ListParams are the macro parameters of paging (append to Params).
func ListParams(defLimit int) []MacroParam {
	lm := func(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }
	return []MacroParam{
		{Name: "limit", Labels: lm("Most entries", "Höchstzahl Einträge"), Type: "number", Default: strconv.Itoa(defLimit)},
		{Name: "paginate", Labels: lm("Split into pages", "Auf Seiten aufteilen"), Type: "select", Options: []string{"no", "yes"}, Default: "no",
			OptionLabels: map[string]map[string]string{"no": lm("No", "Nein"), "yes": lm("Yes", "Ja")}},
		{Name: "per_page", Labels: lm("Entries per page (pages from this number on)", "Einträge pro Seite (Seiten ab dieser Anzahl)"), Type: "number", Default: "10"},
	}
}

// Paging reads the paging parameters of a macro; limit is bounded by the
// setting lists.max_items.
func (mc MacroContext) Paging(defLimit int) Paging {
	most := 1000
	if mc.Service != nil {
		most = mc.Service.MaxListItems()
	}
	num := func(k string, def, lo, hi int) int {
		v, err := strconv.Atoi(mc.Params[k])
		if err != nil || v < lo {
			return def
		}
		return min(v, hi)
	}
	p := Paging{Limit: num("limit", min(defLimit, most), 1, most), Paginate: mc.Params["paginate"] == "yes", PerPage: num("per_page", 10, 1, 200), Page: 1}
	if p.Paginate && mc.Page > 1 {
		p.Page = min(mc.Page, p.Pages(p.Limit))
	}
	return p
}

// Pager returns the page navigation of a macro list ("" for one page).
func (mc MacroContext) Pager(p Paging, total int) string {
	pages := p.Pages(total)
	if pages <= 1 || mc.ElementID <= 0 {
		return ""
	}
	de := mc.Lang == "de"
	label, prev, next, cur := "Pages", "Previous page", "Next page", "Page %d of %d"
	if de {
		label, prev, next, cur = "Seiten", "Vorherige Seite", "Nächste Seite", "Seite %d von %d"
	}
	link := func(n int, text, aria string, rel string) string {
		attrs := fmt.Sprintf(`href="?mp=%d-%d" data-macro-page="%d" data-macro-index="%d" data-element="%d"`, mc.Index, n, n, mc.Index, mc.ElementID)
		if aria != "" {
			attrs += ` aria-label="` + html.EscapeString(aria) + `"`
		}
		if rel != "" {
			attrs += ` rel="` + rel + `"`
		}
		if n == p.Page {
			attrs += ` aria-current="page"`
		}
		return `<li><a ` + attrs + `>` + html.EscapeString(text) + `</a></li>`
	}
	var b []byte
	b = fmt.Appendf(b, `<nav class="ew-pager" aria-label="%s"><p class="sr-only">%s</p><ul>`, html.EscapeString(label), html.EscapeString(fmt.Sprintf(cur, p.Page, pages)))
	if p.Page > 1 {
		b = append(b, link(p.Page-1, "‹", prev, "prev")...)
	}
	for n := 1; n <= pages; n++ {
		// first, last and two around the current page
		if n == 1 || n == pages || n >= p.Page-2 && n <= p.Page+2 {
			b = append(b, link(n, strconv.Itoa(n), fmt.Sprintf(cur, n, pages), "")...)
		} else if n == p.Page-3 || n == p.Page+3 {
			b = append(b, `<li aria-hidden="true">…</li>`...)
		}
	}
	if p.Page < pages {
		b = append(b, link(p.Page+1, "›", next, "next")...)
	}
	b = append(b, `</ul></nav>`...)
	return string(b)
}

// macroPage asks RenderMacros of one element for a page of one macro
// (and, with only, to render just that macro into out).
type macroPage struct {
	element int64
	index   int
	page    int
	only    bool
	out     *string
}

type macroPageKey struct{}

// WithMacroPage returns a context in which element shows page of its
// macro number index.
func WithMacroPage(ctx context.Context, element int64, index, page int) context.Context {
	return context.WithValue(ctx, macroPageKey{}, &macroPage{element: element, index: index, page: page})
}

// RenderMacroPage renders only the macro number index of an element at a
// page (for the reader: GetElement checks the right to view).
func (s *Service) RenderMacroPage(ctx context.Context, id int64, index, page int, lang string) (string, error) {
	var out string
	ctx = context.WithValue(ctx, macroPageKey{}, &macroPage{element: id, index: index, page: page, only: true, out: &out})
	if _, err := s.GetElement(ctx, id, lang, false); err != nil {
		return "", err
	}
	return out, nil
}
