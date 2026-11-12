package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestSourceModesAndPages covers exclusions (-) and requirements (+) of
// sources, the paging of macro lists (page links, one macro again, deep
// links with ?mp=), the preview of the source builder and the bound of the
// setting lists.max_items.
func TestSourceModesAndPages(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()

	var site, board cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Site"}}, &site, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", site.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Board"}}, &board, 201)
	post := func(c int64, title string, tags ...string) {
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c, "type": "core.html", "title": title, "body": htmlBody("<p>About " + title + "</p>"), "tags": tags}, nil, 201)
	}
	for i := 1; i <= 7; i++ {
		post(site.ID, fmt.Sprintf("Item %02d", i), "list")
	}
	post(site.ID, "Internal note", "list", "intern")
	post(site.ID, "Featured item", "list", "top")
	post(board.ID, "Board item", "list", "top")

	// ------------------------------------------------------------ exclude and require
	src := fmt.Sprintf("category:%d+sub; -tag:intern", site.ID)
	xid := macroPage(t, admin, site.ID, "news", map[string]string{"source": src, "layout": "list", "limit": "50"})
	if html := viewHTML(t, admin, xid); !strings.Contains(html, "Item 01") || strings.Contains(html, "Internal note") {
		t.Fatalf("exclusion: %s", html)
	}
	rid := macroPage(t, admin, site.ID, "news", map[string]string{"source": "tag:list; +tag:top", "layout": "list", "limit": "50"})
	html := viewHTML(t, admin, rid)
	if !strings.Contains(html, "Featured item") || !strings.Contains(html, "Board item") || strings.Contains(html, "Item 01") {
		t.Fatalf("requirement: %s", html)
	}
	// a requirement never widens what the reader may see
	if html := viewHTML(t, guest, rid); strings.Contains(html, "Board item") || !strings.Contains(html, "Featured item") {
		t.Fatalf("guest requirement: %s", html)
	}
	// only exclusions: from this category
	oid := macroPage(t, admin, site.ID, "blog", map[string]string{"source": "-tag:intern", "limit": "50"})
	if html := viewHTML(t, admin, oid); !strings.Contains(html, "Item 03") || strings.Contains(html, "Internal note") || strings.Contains(html, "Board item") {
		t.Fatalf("only exclusions: %s", html)
	}

	// the same in property queries (text form round trip)
	var q struct {
		Rows []struct {
			Title string `json:"title"`
		} `json:"rows"`
	}
	guest.must("POST", "/api/v1/x/core.properties/query", map[string]any{"sources": []map[string]any{{"kind": "tag", "tag": "list"}, {"kind": "tag", "tag": "intern", "mode": "exclude"},
		{"kind": "tag", "tag": "top", "mode": "require"}}}, &q, 200)
	if len(q.Rows) != 1 || q.Rows[0].Title != "Featured item" {
		t.Fatalf("property query with modes: %+v", q.Rows)
	}

	// ------------------------------------------------------------ pages
	pid := macroPage(t, admin, site.ID, "news", map[string]string{"source": "tag:list; -tag:top; -tag:intern", "layout": "headlines",
		"order": "created", "limit": "6", "paginate": "yes", "per_page": "4"})
	html = viewHTML(t, guest, pid)
	if !strings.Contains(html, `class="ew-pager"`) || !strings.Contains(html, `aria-current="page"`) || strings.Count(html, `class="ew-news-item"`) != 4 {
		t.Fatalf("first page: %s", html)
	}
	var frag struct {
		HTML string `json:"html"`
	}
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d/macro/1?page=2&lang=en", pid), nil, &frag, 200)
	// 6 entries at most (Item 07 … Item 02, newest first), 4 per page: Item 03 and Item 02 on page 2
	if strings.Count(frag.HTML, `class="ew-news-item"`) != 2 || !strings.Contains(frag.HTML, "Item 02") || strings.Contains(frag.HTML, "Item 01") || strings.Contains(frag.HTML, "Item 04") ||
		!strings.HasPrefix(frag.HTML, `<div class="ew-macro ew-macro--news" data-macro-index="1">`) {
		t.Fatalf("page 2: %s", frag.HTML)
	}
	// a page past the end shows the last one
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d/macro/1?page=99&lang=en", pid), nil, &frag, 200)
	if !strings.Contains(frag.HTML, "Item 02") {
		t.Fatalf("page 99: %s", frag.HTML)
	}
	// deep link
	var v struct {
		HTML string `json:"html"`
	}
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en&mp=1-2", pid), nil, &v, 200)
	if !strings.Contains(v.HTML, "Item 02") || strings.Contains(v.HTML, "Item 07") {
		t.Fatalf("deep link: %s", v.HTML)
	}
	// no macro there / no right to the element
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d/macro/5?page=2", pid), nil, nil, 404)
	var hidden elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": board.ID, "type": "core.html", "title": "Hidden list",
		"body": htmlBody(`<div data-macro="news" data-params='{"source":"tag:list","paginate":"yes","per_page":"2"}'></div>`)}, &hidden, 201)
	if code := guest.do("GET", fmt.Sprintf("/api/v1/elements/%d/macro/1?page=2", hidden.ID), nil, nil); code != 401 && code != 404 {
		t.Fatalf("hidden element: %d", code)
	}
	if code := guest.do("GET", fmt.Sprintf("/api/v1/elements/%d/macro/abc", pid), nil, nil); code != 400 {
		t.Fatalf("bad index: %d", code)
	}

	// ------------------------------------------------------------ preview of the builder
	var pv struct {
		Total int `json:"total"`
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
	}
	admin.must("POST", "/api/v1/sources/preview", map[string]any{"sources": "tag:top", "lang": "en"}, &pv, 200)
	if pv.Total != 2 || len(pv.Items) != 2 {
		t.Fatalf("preview: %+v", pv)
	}
	admin.must("POST", "/api/v1/sources/preview", map[string]any{"sources": "-tag:intern", "category_id": site.ID}, &pv, 200)
	// 7 items, the featured one and 4 pages with macros (the internal note is left out)
	if pv.Total != 12 {
		t.Fatalf("preview with default category: %+v", pv)
	}
	if code := admin.do("POST", "/api/v1/sources/preview", map[string]any{"sources": "-tag:intern"}, nil); code != 400 {
		t.Fatalf("only exclusions without category: %d", code)
	}
	if code := guest.do("POST", "/api/v1/sources/preview", map[string]any{"sources": "tag:top"}, nil); code != 401 && code != 403 {
		t.Fatalf("guest preview: %d", code)
	}

	// ------------------------------------------------------------ the bound of lists.max_items
	for _, bad := range []int{-1, 10001} {
		if code := admin.do("PUT", "/api/v1/admin/settings", map[string]any{"lists": map[string]any{"max_items": bad}}, nil); code != 400 {
			t.Fatalf("max_items %d: %d", bad, code)
		}
	}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"lists": map[string]any{"max_items": 50}}, nil, 200)
	big := macroPage(t, admin, site.ID, "news", map[string]string{"source": "tag:list", "layout": "headlines", "limit": "5000"})
	if n := strings.Count(viewHTML(t, admin, big), `class="ew-news-item"`); n != 10 {
		t.Fatalf("all 10 within the bound: %d", n)
	}
}
