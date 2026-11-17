package server_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// TestCarouselPrivateFeedsNeedBothStages: feeds on internal addresses need
// feeds.allow_private in config.yaml (first stage) and the setting of the
// carousel (second stage). The setting alone, which an administrator can
// switch in the interface, never reaches the internal network.
func TestCarouselPrivateFeedsNeedBothStages(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<rss version="2.0"><channel><item><title>Intranet news</title><link>https://example.org/n</link><guid>n1</guid></item></channel></rss>`)
	}))
	defer feed.Close()
	var c struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/x/core.carousel/carousels", map[string]any{"name": "Intranet", "position": "content-top", "enabled": true,
		"feed_items": 5, "feeds": []string{feed.URL + "/rss"}}, &c, 201)
	stage := func(cfg, setting bool) {
		t.Helper()
		if err := e.app.Config.Update(func(x *config.Config) error { x.Feeds.AllowPrivate = cfg; return nil }); err != nil {
			t.Fatal(err)
		}
		admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"core.carousel": map[string]any{"allow_private": setting}}}, nil, 200)
	}
	type state struct {
		Feeds []struct {
			Error string `json:"error"`
			Items int    `json:"items"`
		} `json:"feeds"`
	}
	refresh := func() (int, state) {
		t.Helper()
		code := admin.do("POST", fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d/refresh", c.ID), nil, nil)
		var s state
		admin.must("GET", fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d/slides", c.ID), nil, &s, 200)
		if len(s.Feeds) != 1 {
			t.Fatalf("feed state: %+v", s)
		}
		return code, s
	}
	for _, tc := range []struct {
		name         string
		cfg, setting bool
	}{{"setting only", false, true}, {"configuration only", true, false}, {"neither", false, false}} {
		stage(tc.cfg, tc.setting)
		code, s := refresh()
		if code != 400 || s.Feeds[0].Items != 0 {
			t.Fatalf("%s: internal feed fetched (status %d, %+v)", tc.name, code, s)
		}
		if !strings.Contains(s.Feeds[0].Error, "feeds.allow_private") || !strings.Contains(s.Feeds[0].Error, "carousel settings") {
			t.Fatalf("%s: the error does not name both switches: %q", tc.name, s.Feeds[0].Error)
		}
	}
	stage(true, true)
	if code, s := refresh(); code != 204 || s.Feeds[0].Items != 1 || s.Feeds[0].Error != "" {
		t.Fatalf("both stages: status %d, %+v", code, s)
	}
}
