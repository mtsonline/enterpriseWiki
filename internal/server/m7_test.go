package server_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestShortURLsListingAndOverview(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var pub, sub, hidden cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Public"}}, &pub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": pub.ID, "titles": map[string]string{"en": "Events"}}, &sub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": pub.ID, "titles": map[string]string{"en": "Board"}}, &hidden, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", pub.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": false}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", sub.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	var page, secret elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "Calendar page", "body": htmlBody("<p>x</p>"), "short_url": "Mein Kalender"}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": hidden.ID, "type": "core.html", "title": "Secret minutes", "body": htmlBody("<p>x</p>")}, &secret, 201)

	// short URLs: normalized, unique across elements and categories, reserved words refused
	var v struct {
		ShortURL string `json:"short_url"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, &v, 200)
	if v.ShortURL != "mein-kalender" {
		t.Fatalf("short url: %q", v.ShortURL)
	}
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"short_url": "/mein-kalender/"}, nil); code != 409 {
		t.Fatalf("duplicate short url: %d", code)
	}
	if code := admin.do("PATCH", fmt.Sprintf("/api/v1/elements/%d", secret.ID), map[string]any{"short_url": "admin"}, nil); code != 400 {
		t.Fatalf("reserved short url: %d", code)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"short_url": "team/events"}, nil, 200)
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", secret.ID), map[string]any{"short_url": "protokoll"}, nil, 200)
	guest := e.client()
	var r struct {
		Type string `json:"type"`
		ID   int64  `json:"id"`
	}
	guest.must("GET", "/api/v1/resolve?path=mein-kalender", nil, &r, 200)
	if r.Type != "element" || r.ID != page.ID {
		t.Fatalf("resolve element: %+v", r)
	}
	guest.must("GET", "/api/v1/resolve?path=/team/events/", nil, &r, 200)
	if r.Type != "category" || r.ID != sub.ID {
		t.Fatalf("resolve category: %+v", r)
	}
	if code := guest.do("GET", "/api/v1/resolve?path=protokoll", nil, nil); code != 401 {
		t.Fatalf("hidden short url for guests: %d", code)
	}
	if code := guest.do("GET", "/api/v1/resolve?path=nothing-here", nil, nil); code != 404 {
		t.Fatalf("unknown short url: %d", code)
	}
	// removing
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", secret.ID), map[string]any{"short_url": ""}, nil, 200)
	if code := admin.do("GET", "/api/v1/resolve?path=protokoll", nil, nil); code != 404 {
		t.Fatalf("removed short url: %d", code)
	}
	// menus link to short URLs
	admin.must("POST", "/api/v1/x/core.menu/menus", map[string]any{"name": "Main", "location": "header-nav", "items": []map[string]any{
		{"type": "element", "element_id": page.ID}, {"type": "category", "category_id": sub.ID}}}, nil, 200)
	var ms []struct {
		Items []struct {
			Href string `json:"href"`
		} `json:"items"`
	}
	guest.must("GET", "/api/v1/x/core.menu/menus", nil, &ms, 200)
	if len(ms) != 1 || ms[0].Items[0].Href != "/mein-kalender" || ms[0].Items[1].Href != "/team/events" {
		t.Fatalf("menu hrefs: %+v", ms)
	}

	// element listing: search, filters, permissions, category path
	var list struct {
		Items []struct {
			ID           int64    `json:"id"`
			Title        string   `json:"title"`
			CategoryPath []string `json:"category_path"`
		} `json:"items"`
		Total int `json:"total"`
	}
	admin.must("GET", "/api/v1/elements?q=calendar", nil, &list, 200)
	if list.Total != 1 || list.Items[0].Title != "Calendar page" || strings.Join(list.Items[0].CategoryPath, "/") != "Public/Events" {
		t.Fatalf("search: %+v", list)
	}
	list.Items = nil
	admin.must("GET", "/api/v1/elements?q=kalender", nil, &list, 200) // short URL matches too
	if list.Total != 1 {
		t.Fatalf("search by short url: %+v", list)
	}
	list.Items = nil
	guest.must("GET", "/api/v1/elements?type=core.html", nil, &list, 200)
	for _, it := range list.Items {
		if it.ID == secret.ID {
			t.Fatal("guest lists a hidden element")
		}
	}
	list.Items = nil
	admin.must("GET", fmt.Sprintf("/api/v1/elements?category=%d&recursive=0", hidden.ID), nil, &list, 200)
	if list.Total != 1 || list.Items[0].ID != secret.ID {
		t.Fatalf("category filter: %+v", list)
	}

	// blog without a source: a category named like the blog is created
	var blog elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "content.blog", "title": "News"}, &blog, 201)
	var children []cat
	admin.must("GET", fmt.Sprintf("/api/v1/categories?parent=%d", pub.ID), nil, &children, 200)
	found := false
	for _, c := range children {
		found = found || c.Titles["en"] == "News"
	}
	if !found {
		t.Fatalf("no source category created: %+v", children)
	}

	// category overview, also as start page
	var ov elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "content.catlist", "title": "Overview",
		"body": map[string]any{"category_id": pub.ID, "depth": 2, "layout": "list", "elements": true}}, &ov, 201)
	var view struct {
		HTML string `json:"html"`
	}
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d", ov.ID), nil, &view, 200)
	if !strings.Contains(view.HTML, `href="/team/events"`) || !strings.Contains(view.HTML, `href="/mein-kalender"`) || strings.Contains(view.HTML, "Board") {
		t.Fatalf("overview: %s", view.HTML)
	}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": map[string]any{"site_name": "Wiki", "template": "theme.confluence", "home_element": ov.ID}}, nil, 200)
	var site struct {
		HomeElement int64 `json:"home_element"`
	}
	guest.must("GET", "/api/v1/site", nil, &site, 200)
	if site.HomeElement != ov.ID {
		t.Fatalf("home element: %+v", site)
	}
}
