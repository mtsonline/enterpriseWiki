package server_test

import (
	"strings"
	"testing"
)

// TestEmbedFlow: an administrator allows a video service in the settings;
// then the embed renders the box (no frame) and every response opens the
// Content-Security-Policy for exactly that service. Without the setting
// the embed is refused and the CSP stays closed; bad entries are refused.
func TestEmbedFlow(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	preview := func() string {
		var res struct {
			HTML string `json:"html"`
		}
		admin.must("POST", "/api/v1/elements/preview", map[string]any{"type": "core.html", "lang": "de", "body": map[string]any{"format": "markdown",
			"source": `::embed{url="https://youtu.be/dQw4w9WgXcQ" title="Einführung"}`}}, &res, 200)
		return res.HTML
	}
	csp := func() string {
		resp, _ := admin.raw("/api/v1/me", nil)
		return resp.Header.Get("Content-Security-Policy")
	}
	if h := preview(); !strings.Contains(h, "ew-macro-error") || !strings.Contains(h, "nicht freigegeben") || strings.Contains(h, "data-src") {
		t.Errorf("not allowed yet: %s", h)
	}
	if c := csp(); strings.Contains(c, "youtube") {
		t.Errorf("CSP open before: %s", c)
	}
	var cfg struct {
		Media map[string]any `json:"media"`
	}
	admin.must("GET", "/api/v1/admin/settings", nil, &cfg, 200)
	media := cfg.Media
	if media == nil {
		media = map[string]any{}
	}
	for _, bad := range []string{"https://*.example.com", "http://video.example.com", "https://video.example.com/path", "https://video.example.com; script-src *"} {
		media["embed_allow"] = []string{bad}
		admin.must("PUT", "/api/v1/admin/settings", map[string]any{"media": media}, nil, 400)
	}
	media["embed_allow"] = []string{"https://www.youtube-nocookie.com"}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"media": media}, nil, 200)
	h := preview()
	for _, want := range []string{`data-ew-widget="ewl.embed"`, `data-src="https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"`, "Einführung"} {
		if !strings.Contains(h, want) {
			t.Errorf("%q missing: %s", want, h)
		}
	}
	if strings.Contains(h, "<iframe") {
		t.Errorf("frame before consent: %s", h)
	}
	if c := csp(); !strings.Contains(c, "frame-src 'self' https://www.youtube-nocookie.com") || strings.Contains(c, "form-action 'self' https://www.youtube-nocookie.com") {
		t.Errorf("CSP: %s", c)
	}
	// own HTML cannot build the widget (the attributes are removed)
	var res struct {
		HTML string `json:"html"`
	}
	admin.must("POST", "/api/v1/elements/preview", map[string]any{"type": "core.html", "body": map[string]any{"format": "html",
		"source": `<figure data-ew-widget="ewl.embed" data-src="https://www.youtube-nocookie.com/embed/x"><iframe src="https://www.youtube-nocookie.com/embed/x"></iframe></figure>`}}, &res, 200)
	if strings.Contains(res.HTML, "data-ew-widget") || strings.Contains(res.HTML, "data-src") || strings.Contains(res.HTML, "<iframe") {
		t.Errorf("own HTML: %s", res.HTML)
	}
	media["embed_allow"] = []string{}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"media": media}, nil, 200)
	if c := csp(); strings.Contains(c, "youtube") {
		t.Errorf("CSP still open: %s", c)
	}
}
