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
		"sources": fmt.Sprintf("tag:hr; category:%d", news.ID)}, &c, 200)
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
		cl.must("GET", "/api/v1/x/core.carousel/position/content-top?lang=en", nil, &s, 200)
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
