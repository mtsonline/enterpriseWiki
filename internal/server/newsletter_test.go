package server_test

import (
	"strings"
	"testing"
)

// TestNewsletterFlow: an administrator allows a newsletter provider in the
// settings; then the block renders a form to it and every response opens
// the Content-Security-Policy (form-action) for exactly that provider.
// Without the setting the block is refused and form-action stays 'self';
// bad entries are refused; own HTML cannot build a form.
func TestNewsletterFlow(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	preview := func(format, src string) string {
		var res struct {
			HTML string `json:"html"`
		}
		admin.must("POST", "/api/v1/elements/preview", map[string]any{"type": "core.html", "lang": "de", "body": map[string]any{"format": format, "source": src}}, &res, 200)
		return res.HTML
	}
	block := `::newsletter{address="https://news.example.com/subscribe" hidden="list=7"}`
	formAction := func() string {
		resp, _ := admin.raw("/api/v1/auth/me", nil)
		c := resp.Header.Get("Content-Security-Policy")
		_, fa, _ := strings.Cut(c, "form-action ")
		fa, _, _ = strings.Cut(fa, ";")
		return fa
	}
	if h := preview("markdown", block); !strings.Contains(h, "ew-macro-error") || !strings.Contains(h, "nicht freigegeben") || strings.Contains(h, "<form") {
		t.Errorf("not allowed yet: %s", h)
	}
	if fa := formAction(); fa != "'self'" {
		t.Errorf("form-action before: %q", fa)
	}
	var cfg struct {
		Media map[string]any `json:"media"`
	}
	admin.must("GET", "/api/v1/admin/settings", nil, &cfg, 200)
	media := cfg.Media
	if media == nil {
		media = map[string]any{}
	}
	for _, bad := range []string{"https://*.example.com", "http://news.example.com", "https://news.example.com/path", "https://news.example.com; script-src *", "'self' https://x.example.com"} {
		media["form_allow"] = []string{bad}
		admin.must("PUT", "/api/v1/admin/settings", map[string]any{"media": media}, nil, 400)
	}
	media["form_allow"] = []string{"https://news.example.com"}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"media": media}, nil, 200)
	h := preview("markdown", block)
	for _, want := range []string{`<form class="ewl-newsletter-form" method="post" action="https://news.example.com/subscribe"`, `<input type="hidden" name="list" value="7"/>`,
		`type="email" autocomplete="email" required=""`, "(news.example.com)"} {
		if !strings.Contains(h, want) {
			t.Errorf("%q missing: %s", want, h)
		}
	}
	if fa := formAction(); fa != "'self' https://news.example.com" {
		t.Errorf("form-action: %q", fa)
	}
	// another provider stays refused
	if h := preview("markdown", `::newsletter{address="https://other.example.com/s"}`); !strings.Contains(h, "ew-macro-error") || strings.Contains(h, "<form") {
		t.Errorf("other provider: %s", h)
	}
	// own HTML cannot build a form to it (forms and their fields are removed)
	if h := preview("html", `<form action="https://news.example.com/subscribe" method="post"><input name="email"><button>Go</button></form>`); strings.Contains(h, "<form") || strings.Contains(h, "<input") {
		t.Errorf("own HTML: %s", h)
	}
	media["form_allow"] = []string{}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"media": media}, nil, 200)
	if fa := formAction(); fa != "'self'" {
		t.Errorf("form-action still open: %q", fa)
	}
}
