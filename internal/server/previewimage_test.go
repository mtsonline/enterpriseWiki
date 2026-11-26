package server_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

func TestPreviewImage(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var pub cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Public"}}, &pub, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", pub.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": false}, nil, 201)
	img := "/api/v1/files/" + strings.Repeat("ab", 32)
	var withImg, plain elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "core.html", "title": "Team",
		"body": htmlBody(`<p>Hello</p><p><img src="data:image/png;base64,AAAA" alt=""><img src="` + img + `" alt="Team photo"></p>`)}, &withImg, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "core.html", "title": "Plain", "body": htmlBody("<p>Text only</p>")}, &plain, 201)
	ogImage := func(path string) string {
		t.Helper()
		body, _ := e.guestPage(path, "")
		m := regexp.MustCompile(`<meta property="og:image" content="([^"]*)">`).FindStringSubmatch(body)
		if m == nil {
			return ""
		}
		if !strings.Contains(body, `<meta name="twitter:card" content="summary_large_image">`) {
			t.Errorf("no large twitter card on %s", path)
		}
		return m[1]
	}
	// automatic: first loadable image of the page (absolute URL)
	if got := ogImage(fmt.Sprintf("/e/%d", withImg.ID)); !strings.HasPrefix(got, "http") || !strings.HasSuffix(got, img) {
		t.Fatalf("automatic preview image: %q", got)
	}
	if got := ogImage(fmt.Sprintf("/e/%d", plain.ID)); got != "" {
		t.Fatalf("page without image has preview %q", got)
	}
	// own image per page, validated
	if code := admin.do("PATCH", fmt.Sprintf("/api/v1/elements/%d", plain.ID), map[string]any{"preview_image": "javascript:alert(1)"}, nil); code != 400 {
		t.Fatalf("invalid preview image accepted: %d", code)
	}
	var got elem
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", plain.ID), map[string]any{"preview_image": "https://cdn.example.com/cover.jpg"}, nil, 200)
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", plain.ID), nil, &got, 200)
	if got.PreviewImage != "https://cdn.example.com/cover.jpg" {
		t.Fatalf("preview image not stored: %+v", got)
	}
	if g := ogImage(fmt.Sprintf("/e/%d", plain.ID)); g != "https://cdn.example.com/cover.jpg" {
		t.Fatalf("own preview image: %q", g)
	}
	// site default for pages without an image
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", plain.ID), map[string]any{"preview_image": ""}, nil, 200)
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.UI.PreviewImage = "/api/v1/files/" + strings.Repeat("cd", 32)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e.app.Cache.Invalidate(context.Background(), "test")
	if g := ogImage(fmt.Sprintf("/e/%d", plain.ID)); !strings.HasSuffix(g, strings.Repeat("cd", 32)) {
		t.Fatalf("site preview image: %q", g)
	}
}
