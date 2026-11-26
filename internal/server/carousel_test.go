package server_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// TestCarousel covers carousels at positions, own slides, news published
// with the publish option, visibility of news and external feeds.
func TestCarousel(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<rss version="2.0"><channel><item><title>External item</title><link>https://example.org/x</link><guid>x1</guid><pubDate>Mon, 02 Nov 2026 10:00:00 +0100</pubDate></item></channel></rss>`)
	}))
	defer feed.Close()
	type car struct {
		ID int64 `json:"id"`
	}
	var c car
	// feeds on internal addresses are blocked until allowed
	admin.must("POST", "/api/v1/x/core.carousel/carousels", map[string]any{"name": "Home", "position": "content-top", "feeds": []string{feed.URL + "/rss"}, "enabled": true}, &c, 201)
	if code := admin.do("POST", fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d/refresh", c.ID), nil, nil); code < 400 {
		t.Fatalf("internal feed fetched without allow_private: %d", code)
	}
	// both stages: config.yaml and the carousel setting (see TestCarouselPrivateFeedsNeedBothStages)
	if err := e.app.Config.Update(func(x *config.Config) error { x.Feeds.AllowPrivate = true; return nil }); err != nil {
		t.Fatal(err)
	}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"core.carousel": map[string]any{"allow_private": true}}}, nil, 200)
	admin.must("POST", fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d/refresh", c.ID), nil, nil, 204)
	admin.must("POST", fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d/slides", c.ID), map[string]any{"title": "Welcome", "text": "Hello", "link": "/search"}, nil, 201)
	if code := admin.do("POST", fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d/slides", c.ID), map[string]any{"title": "Bad", "link": "javascript:alert(1)"}, nil); code != 400 {
		t.Fatalf("unsafe link accepted: %d", code)
	}
	if code := admin.do("POST", fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d/slides", c.ID), map[string]any{"title": "No alt", "image": "/api/v1/files/abc"}, nil); code != 400 {
		t.Fatalf("image without alternative text accepted: %d", code)
	}
	// news: a draft published with the option "as news"; in a category guests cannot see
	var secret cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Internal"}}, &secret, 201)
	var d struct{ ID int64 }
	admin.must("POST", "/api/v1/drafts", map[string]any{"category_id": secret.ID, "type": "core.html", "title": "Big news", "body": htmlBody("<p>The new building opens on Monday.</p>")}, &d, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/drafts/%d/publish", d.ID), map[string]any{"options": map[string]any{"core.carousel": map[string]any{"carousels": []int64{c.ID}}}}, nil, 200)
	type shown []struct {
		Slides []struct {
			Kind  string `json:"kind"`
			Title string `json:"title"`
			Text  string `json:"text"`
			Link  string `json:"link"`
		} `json:"slides"`
	}
	var s shown
	admin.must("GET", "/api/v1/x/core.carousel/shown?lang=en", nil, &s, 200)
	kinds := []string{}
	for _, x := range s[0].Slides {
		kinds = append(kinds, x.Kind+":"+x.Title)
	}
	if strings.Join(kinds, ",") != "news:Big news,manual:Welcome,feed:External item" {
		t.Fatalf("slides: %v", kinds)
	}
	if !strings.Contains(s[0].Slides[0].Text, "new building") || !strings.HasPrefix(s[0].Slides[0].Link, "/wiki/") {
		t.Fatalf("news slide: %+v", s[0].Slides[0])
	}
	guest := e.client()
	guest.must("GET", "/api/v1/x/core.carousel/shown?lang=en", nil, &s, 200)
	for _, x := range s[0].Slides {
		if x.Kind == "news" {
			t.Fatal("guest sees news of an internal page")
		}
	}
	// audience: only signed-in users
	admin.must("PUT", fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d", c.ID), map[string]any{"name": "Home", "position": "content-top", "audience": "users", "feeds": []string{feed.URL + "/rss"}, "enabled": true}, nil, 200)
	guest.must("GET", "/api/v1/x/core.carousel/shown", nil, &s, 200)
	if len(s) != 0 {
		t.Fatalf("guest sees a carousel for users: %+v", s)
	}
	// no fixed position: only where it is placed; an invalid position is refused
	var free struct {
		ID       int64  `json:"id"`
		Position string `json:"position"`
	}
	admin.must("POST", "/api/v1/x/core.carousel/carousels", map[string]any{"name": "Placed only", "position": "", "enabled": true}, &free, 201)
	if free.Position != "" {
		t.Fatalf("placed-only carousel got position %q", free.Position)
	}
	if code := admin.do("POST", "/api/v1/x/core.carousel/carousels", map[string]any{"name": "Bad", "position": "Top Bar"}, nil); code != 400 {
		t.Fatalf("invalid position: %d", code)
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d", free.ID), nil, nil, 204)
	// permissions
	pw := "Passw0rd!Passw0rd"
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "car", "password": pw, "display_name": "Car", "roles": []int64{}}, nil, 201)
	u := e.client()
	u.login("car", pw)
	if code := u.do("POST", "/api/v1/x/core.carousel/carousels", map[string]any{"name": "X", "position": "sidebar"}, nil); code != 403 {
		t.Fatalf("carousel created without permission: %d", code)
	}
	var targets []any
	u.must("GET", "/api/v1/x/core.carousel/targets", nil, &targets, 200)
	if len(targets) != 0 {
		t.Fatalf("targets without permission: %+v", targets)
	}
}
