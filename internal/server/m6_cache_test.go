package server_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

func TestGuestCache(t *testing.T) {
	e := setup(t)
	if err := e.app.Config.Update(func(c *config.Config) error { c.Cache.Enabled = true; c.Cache.TTLSeconds = 0; return nil }); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"guest_visible": true, "titles": map[string]string{"en": "Public"}}, &c, 201)
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Hello", "body": htmlBody("<p>Version 1</p>")}, &page, 201)
	guest := e.client()
	path := fmt.Sprintf("/api/v1/elements/%d", page.ID)
	ref := map[string]string{"Referer": e.srv.URL + fmt.Sprintf("/e/%d", page.ID), "Accept-Language": "en"}
	r1, b1 := guest.raw(path, ref)
	r2, b2 := guest.raw(path, ref)
	if r1.Header.Get("X-Cache") != "MISS" || r2.Header.Get("X-Cache") != "HIT" || b1 != b2 || !strings.Contains(b2, "Version 1") {
		t.Fatalf("cache: %s/%s", r1.Header.Get("X-Cache"), r2.Header.Get("X-Cache"))
	}
	// signed-in users never get cached answers
	if r, _ := admin.raw(path, ref); r.Header.Get("X-Cache") != "" {
		t.Fatalf("admin got %q", r.Header.Get("X-Cache"))
	}
	// the page HTML embeds what the page needed
	hr, html := guest.raw(fmt.Sprintf("/e/%d", page.ID), map[string]string{"Accept-Language": "en"})
	if hr.Header.Get("X-Cache") != "PRELOAD" || !strings.Contains(html, `<script type="application/json" id="ew-preload">`) || !strings.Contains(html, `Version 1`) ||
		strings.Contains(strings.SplitN(strings.SplitN(html, `id="ew-preload">`, 2)[1], "</script>", 2)[0], "</p>") {
		t.Fatalf("preload: %s", html)
	}
	// a change through the API clears the cache
	admin.must("PUT", path, map[string]any{"lang": "en", "title": "Hello", "body": htmlBody("<p>Version 2</p>"), "base_rev": 1}, nil, 200)
	r3, b3 := guest.raw(path, ref)
	if r3.Header.Get("X-Cache") != "MISS" || !strings.Contains(b3, "Version 2") {
		t.Fatalf("after change: %s %s", r3.Header.Get("X-Cache"), b3)
	}
	// a change outside a request (worker/CLI) through the content hook and the DB generation
	before := e.app.Cache.GetStatus().Generation
	e.app.Content.Touch(context.Background(), page.ID)
	if e.app.Cache.GetStatus().Generation <= before {
		t.Fatal("generation not incremented")
	}
	if r, _ := guest.raw(path, ref); r.Header.Get("X-Cache") != "MISS" {
		t.Fatal("hook did not clear the cache")
	}
	// requests that are never cached
	if r, _ := guest.raw("/api/v1/me", nil); r.Header.Get("X-Cache") != "" {
		t.Fatal("/me cached")
	}
	var st struct {
		Enabled bool  `json:"enabled"`
		Hits    int64 `json:"hits"`
		Entries int   `json:"entries"`
	}
	admin.must("GET", "/api/v1/admin/cache", nil, &st, 200)
	if !st.Enabled || st.Hits < 1 {
		t.Fatalf("status: %+v", st)
	}
	admin.must("DELETE", "/api/v1/admin/cache", nil, nil, 204)
	admin.must("GET", "/api/v1/admin/cache", nil, &st, 200)
	if st.Entries != 0 {
		t.Fatalf("not cleared: %+v", st)
	}
	if code := guest.do("GET", "/api/v1/admin/cache", nil, nil); code != 401 {
		t.Fatalf("guest cache status: %d", code)
	}
}
