package server_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// TestFlexibleSources covers several sources in the news, blog and FAQ
// macros, further sources of blog/FAQ/calendar elements, external feeds
// and calendars, carousel slides from sources, and that nobody sees
// elements of sources they may not read.
func TestFlexibleSources(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()
	if err := e.app.Config.Update(func(c *config.Config) error { c.Feeds.AllowPrivate = true; return nil }); err != nil {
		t.Fatal(err)
	}
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rss":
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprint(w, `<rss version="2.0"><channel><item><title>Feed &lt;script&gt;item</title><link>https://example.org/x</link><guid>x1</guid>`+
				`<description>Outside &lt;img src=x onerror=alert(1)&gt; news</description><pubDate>Mon, 02 Nov 2026 10:00:00 +0100</pubDate></item>`+
				`<item><title>Bad link</title><link>javascript:alert(1)</link><guid>x2</guid></item></channel></rss>`)
		case "/cal.ics":
			w.Header().Set("Content-Type", "text/calendar")
			start := time.Now().Add(72 * time.Hour).UTC().Format("20060102T150000Z")
			end := time.Now().Add(74 * time.Hour).UTC().Format("20060102T150000Z")
			fmt.Fprint(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:ext-1\r\nDTSTART:"+start+"\r\nDTEND:"+end+
				"\r\nSUMMARY:Public holiday market\r\nDESCRIPTION:Bring ![x](https://tracker.example/p.png) <b>cash</b>\r\nURL:javascript:alert(1)\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer feed.Close()

	var site, news, hr, secret cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Site"}}, &site, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", site.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": site.ID, "titles": map[string]string{"en": "News"}}, &news, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": site.ID, "titles": map[string]string{"en": "HR"}}, &hr, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Board"}}, &secret, 201)
	post := func(c int64, title string, tags ...string) int64 {
		var el elem
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c, "type": "core.html", "title": title, "body": htmlBody("<p>About " + title + "</p>"), "tags": tags}, &el, 201)
		return el.ID
	}
	post(news.ID, "Canteen reopens")
	post(hr.ID, "New holiday rules", "hr")
	secretID := post(secret.ID, "Merger plans", "hr")

	// ------------------------------------------------------------ invalid sources are refused
	for _, bad := range []string{"category:abc", "nope:1", "feed:file:///etc/passwd", "elements:" + strings.Repeat("1,", 300)} {
		if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": news.ID, "type": "content.blog", "title": "B",
			"body": map[string]any{"category_id": news.ID, "more_sources": bad}}, nil); code != 400 {
			t.Fatalf("more_sources %q accepted: %d", bad, code)
		}
	}

	// ------------------------------------------------------------ news macro: categories, tags and a feed
	src := fmt.Sprintf("category:%d; tag:hr; feed:%s/rss", news.ID, feed.URL)
	nid := macroPage(t, admin, site.ID, "news", map[string]string{"source": src, "layout": "list", "limit": "10", "title": "Everything new"})
	if html := viewHTML(t, guest, nid); !strings.Contains(html, "Canteen reopens") || !strings.Contains(html, "New holiday rules") || strings.Contains(html, "Merger plans") {
		t.Fatalf("guest news: %s", html)
	}
	if html := viewHTML(t, admin, nid); !strings.Contains(html, "Merger plans") {
		t.Fatalf("admin misses the tagged board page: %s", html)
	}
	if err := e.app.Feeds.Refresh(context.Background(), "rss", feed.URL+"/rss"); err != nil {
		t.Fatal(err)
	}
	html := viewHTML(t, guest, nid)
	if !strings.Contains(html, ">Feed</a>") || !strings.Contains(html, "Outside news") || strings.Contains(html, "<script") || strings.Contains(html, "onerror") ||
		strings.Contains(html, "javascript:") || !strings.Contains(html, `rel="noopener noreferrer nofollow"`) {
		t.Fatalf("feed items: %s", html)
	}

	// ------------------------------------------------------------ blog macro across categories
	bid := macroPage(t, admin, site.ID, "blog", map[string]string{"source": fmt.Sprintf("category:%d; category:%d", hr.ID, secret.ID), "title": "Posts"})
	if html := viewHTML(t, guest, bid); !strings.Contains(html, "New holiday rules") || strings.Contains(html, "Merger plans") || !strings.Contains(html, `<h3 class="ewb-title">`) {
		t.Fatalf("guest blog macro: %s", html)
	}
	if html := viewHTML(t, admin, bid); !strings.Contains(html, "Merger plans") {
		t.Fatalf("admin blog macro: %s", html)
	}
	// a single element as source: only if the reader may see it
	eid := macroPage(t, admin, site.ID, "blog", map[string]string{"source": fmt.Sprintf("elements:%d", secretID)})
	if html := viewHTML(t, guest, eid); strings.Contains(html, "Merger plans") {
		t.Fatalf("guest sees a single foreign element: %s", html)
	}

	// ------------------------------------------------------------ FAQ macro: one section per category
	fid := macroPage(t, admin, site.ID, "faq", map[string]string{"source": fmt.Sprintf("category:%d; category:%d", news.ID, hr.ID), "numbering": "yes"})
	html = viewHTML(t, guest, fid)
	if strings.Count(html, `class="ewq-section ewq-depth-1"`) != 2 || !strings.Contains(html, "About Canteen reopens") || !strings.Contains(html, `<span class="ewq-num">2.1</span> New holiday rules`) {
		t.Fatalf("faq macro: %s", html)
	}

	// pages that show each other (FAQ answers are rendered): stops instead of looping
	var loopA, loopB cat
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": site.ID, "titles": map[string]string{"en": "Loop A"}}, &loopA, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": site.ID, "titles": map[string]string{"en": "Loop B"}}, &loopB, 201)
	la := macroPage(t, admin, loopA.ID, "faq", map[string]string{"source": fmt.Sprintf("category:%d", loopB.ID)})
	macroPage(t, admin, loopB.ID, "faq", map[string]string{"source": fmt.Sprintf("category:%d", loopA.ID)})
	if html := viewHTML(t, guest, la); !strings.Contains(html, "nested too deeply") {
		t.Fatalf("mutual macros: %.300s", html)
	}

	// ------------------------------------------------------------ blog element with further sources
	var blog elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": news.ID, "type": "content.blog", "title": "Blog",
		"body": map[string]any{"category_id": news.ID, "more_sources": fmt.Sprintf("category:%d; tag:hr", hr.ID)}}, &blog, 201)
	if html := viewHTML(t, guest, blog.ID); !strings.Contains(html, "Canteen reopens") || !strings.Contains(html, "New holiday rules") || strings.Contains(html, "Merger plans") {
		t.Fatalf("blog with more sources: %s", html)
	}

	// ------------------------------------------------------------ FAQ element: foreign sections below its own
	var faq elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": news.ID, "type": "content.faq", "title": "FAQ",
		"body": map[string]any{"category_id": news.ID, "recursive": true, "more_sources": fmt.Sprintf("category:%d", hr.ID)}}, &faq, 201)
	html = viewHTML(t, guest, faq.ID)
	if !strings.Contains(html, "Canteen reopens") || !strings.Contains(html, `<details class="ewq-section ewq-depth-1"`) || !strings.Contains(html, "New holiday rules") {
		t.Fatalf("faq with more sources: %s", html)
	}

	// ------------------------------------------------------------ calendars: other calendars and iCal subscriptions
	start := time.Now().Add(48 * time.Hour).Truncate(time.Hour)
	var boardCal, cal elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "content.events", "title": "Board calendar", "body": map[string]any{"view": "list"}}, &boardCal, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/x/content.events/calendars/%d/events", boardCal.ID), map[string]any{"title": "Board meeting", "starts_at": start, "ends_at": start.Add(time.Hour)}, nil, 201)
	if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": site.ID, "type": "content.events", "title": "Bad",
		"body": map[string]any{"view": "list", "more_sources": "ical:http://[::1"}}, nil); code != 400 {
		t.Fatalf("broken ical address accepted: %d", code)
	}
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": site.ID, "type": "content.events", "title": "All events",
		"body": map[string]any{"view": "list", "more_sources": fmt.Sprintf("calendar:%d; ical:%s/cal.ics", boardCal.ID, feed.URL)}}, &cal, 201)
	base := fmt.Sprintf("/api/v1/x/content.events/calendars/%d/events?from=%s&to=%s", cal.ID,
		time.Now().Format("2006-01-02"), time.Now().Add(10*24*time.Hour).Format("2006-01-02"))
	type evList struct {
		Events []struct {
			ID              int64  `json:"id"`
			Title           string `json:"title"`
			URL             string `json:"url"`
			DescriptionHTML string `json:"description_html"`
			External        bool   `json:"external"`
		} `json:"events"`
	}
	var l evList
	admin.must("GET", base, nil, &l, 200) // first use: the subscription is fetched in the background
	if err := e.app.Feeds.Refresh(context.Background(), "ical", feed.URL+"/cal.ics"); err != nil {
		t.Fatal(err)
	}
	titles := func(c *client) map[string]int {
		l.Events = nil
		c.must("GET", base, nil, &l, 200)
		out := map[string]int{}
		for i, x := range l.Events {
			out[x.Title] = i
		}
		return out
	}
	got := titles(admin)
	i, ok := got["Public holiday market"]
	if _, board := got["Board meeting"]; !board || !ok {
		t.Fatalf("admin calendar: %+v", l.Events)
	}
	x := l.Events[i]
	if !x.External || x.ID >= 0 || x.URL != "" || strings.Contains(x.DescriptionHTML, "<b>") || strings.Contains(x.DescriptionHTML, "<img") || !strings.Contains(x.DescriptionHTML, "&lt;b&gt;cash") {
		t.Fatalf("external event: %+v", x)
	}
	got = titles(guest)
	if _, board := got["Board meeting"]; board {
		t.Fatal("guest sees events of a calendar it may not view")
	}
	if _, ok := got["Public holiday market"]; !ok {
		t.Fatalf("guest misses the subscribed calendar: %+v", l.Events)
	}

	// ------------------------------------------------------------ carousel: elements of sources as slides
	var c struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/x/core.carousel/carousels", map[string]any{"name": "Home", "position": "content-top", "enabled": true, "feed_items": 10,
		"sources": fmt.Sprintf("tag:hr; category:%d", news.ID)}, &c, 201)
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d", c.ID), map[string]any{"name": "Home", "position": "content-top", "sources": "tag:"}, nil); code != 400 {
		t.Fatalf("invalid carousel source accepted: %d", code)
	}
	type shown []struct {
		Slides []struct {
			Kind  string `json:"kind"`
			Title string `json:"title"`
			Link  string `json:"link"`
		} `json:"slides"`
	}
	slides := func(cl *client) []string {
		var s shown
		cl.must("GET", "/api/v1/x/core.carousel/shown?lang=en", nil, &s, 200)
		out := []string{}
		if len(s) > 0 {
			for _, x := range s[0].Slides {
				out = append(out, x.Title)
			}
		}
		return out
	}
	if g := strings.Join(slides(guest), ","); !strings.Contains(g, "New holiday rules") || !strings.Contains(g, "Canteen reopens") || strings.Contains(g, "Merger plans") {
		t.Fatalf("guest slides: %s", g)
	}
	if a := strings.Join(slides(admin), ","); !strings.Contains(a, "Merger plans") {
		t.Fatalf("admin slides: %s", a)
	}
}

// TestResolvedSources covers the source kinds resolved to elements: a
// search ("search:…") and a properties filter ("props:…") in lists of
// several sources, that readers only get what they may see and that bad
// values are refused.
func TestResolvedSources(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()

	var site, board cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Site"}}, &site, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", site.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Board"}}, &board, 201)
	admin.must("POST", "/api/v1/x/core.properties/defs", map[string]any{"key": "stage", "type": "text", "labels": map[string]string{"en": "Stage"}}, nil, 201)
	post := func(c int64, title, stage string) {
		body := map[string]any{"category_id": c, "type": "core.html", "title": title, "body": htmlBody("<p>The quokkafest " + title + "</p>")}
		if stage != "" {
			body["fields"] = map[string]any{"core.properties": map[string]any{"values": map[string]any{"stage": stage}}}
		}
		admin.must("POST", "/api/v1/elements", body, nil, 201)
	}
	post(site.ID, "Public plan", "live")
	post(site.ID, "Draft idea", "draft")
	post(board.ID, "Board secret", "live")

	// ------------------------------------------------------------ search
	sid := macroPage(t, admin, site.ID, "news", map[string]string{"source": "search:quokkafest", "layout": "list", "limit": "20"})
	if html := viewHTML(t, guest, sid); !strings.Contains(html, "Public plan") || !strings.Contains(html, "Draft idea") || strings.Contains(html, "Board secret") {
		t.Fatalf("guest search source: %s", html)
	}
	if html := viewHTML(t, admin, sid); !strings.Contains(html, "Board secret") {
		t.Fatalf("admin search source: %s", html)
	}
	// a search without hits shows nothing (never everything)
	nid := macroPage(t, admin, site.ID, "news", map[string]string{"source": "search:zzxqnohit", "layout": "list"})
	if html := viewHTML(t, admin, nid); strings.Contains(html, "Public plan") {
		t.Fatalf("empty search shows elements: %s", html)
	}

	// ------------------------------------------------------------ properties filter ("&&" between the filters)
	pid := macroPage(t, admin, site.ID, "news", map[string]string{"source": "props:stage = live && stage notempty", "layout": "list", "limit": "20"})
	if html := viewHTML(t, guest, pid); !strings.Contains(html, "Public plan") || strings.Contains(html, "Draft idea") || strings.Contains(html, "Board secret") {
		t.Fatalf("guest props source: %s", html)
	}
	if html := viewHTML(t, admin, pid); !strings.Contains(html, "Board secret") || strings.Contains(html, "Draft idea") {
		t.Fatalf("admin props source: %s", html)
	}
	// combined with a category: either matches
	cid := macroPage(t, admin, site.ID, "news", map[string]string{"source": fmt.Sprintf("props:stage = draft; category:%d", board.ID), "layout": "list", "limit": "20"})
	if html := viewHTML(t, admin, cid); !strings.Contains(html, "Draft idea") || !strings.Contains(html, "Board secret") || strings.Contains(html, "Public plan") {
		t.Fatalf("combined sources: %s", html)
	}

	// ------------------------------------------------------------ in the properties query itself
	var q struct {
		Rows []struct {
			Title string `json:"title"`
		} `json:"rows"`
	}
	guest.must("POST", "/api/v1/x/core.properties/query", map[string]any{"sources": []map[string]any{{"kind": "search", "value": "quokkafest"}}, "columns": []string{"stage"}}, &q, 200)
	for _, r := range q.Rows {
		if r.Title == "Board secret" {
			t.Fatal("guest query sees the board via a search source")
		}
	}
	if len(q.Rows) != 2 {
		t.Fatalf("guest query rows: %+v", q.Rows)
	}

	// ------------------------------------------------------------ bad values are refused
	for _, bad := range []string{"search:", "props:", "props:" + strings.Repeat("a", 2100)} {
		if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": site.ID, "type": "content.blog", "title": "B",
			"body": map[string]any{"category_id": site.ID, "more_sources": bad}}, nil); code != 400 {
			t.Fatalf("more_sources %q accepted: %d", bad, code)
		}
	}
}

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

// macroPage creates a page whose HTML holds one macro.
func macroPage(t *testing.T, c *client, catID int64, name string, params map[string]string) int64 {
	t.Helper()
	ps := []string{}
	for k, v := range params {
		ps = append(ps, fmt.Sprintf("%q:%q", k, v))
	}
	src := fmt.Sprintf(`<div data-macro="%s" data-params='{%s}'></div>`, name, strings.Join(ps, ","))
	var el elem
	c.must("POST", "/api/v1/elements", map[string]any{"category_id": catID, "type": "core.html", "title": "Macro " + name, "body": htmlBody(src)}, &el, 201)
	return el.ID
}

func viewHTML(t *testing.T, c *client, id int64) string {
	t.Helper()
	var v struct {
		HTML string `json:"html"`
	}
	c.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", id), nil, &v, 200)
	return v.HTML
}
