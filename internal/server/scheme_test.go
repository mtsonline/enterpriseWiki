package server_test

import (
	"net/http"
	"strings"
	"testing"
)

// Light and dark: the choice of a guest (cookie) and of a person (personal
// settings), the default of the template, and the mark in the page the server
// sends (no light flash).
func TestColorScheme(t *testing.T) {
	e := setup(t)
	page := func(c *client) (string, http.Header) {
		t.Helper()
		res, body := c.raw("/", nil)
		i := strings.Index(body, "<html")
		if i < 0 {
			t.Fatalf("no <html> in the page:\n%s", body)
		}
		return body[i : i+strings.Index(body[i:], ">")+1], res.Header
	}

	// a guest without a choice: the default of the template (theme.wiki: as the device is set)
	guest := e.client()
	tag, hdr := page(guest)
	if !strings.Contains(tag, `data-scheme="system"`) || strings.Contains(tag, "data-scheme-choice") || !strings.Contains(tag, `lang="`) {
		t.Fatalf("guest without a choice: %s", tag)
	}
	if !strings.Contains(strings.Join(hdr.Values("Vary"), ","), "Cookie") {
		t.Errorf("the page depends on the cookie but does not say so: Vary %q", hdr.Values("Vary"))
	}

	// the guest chooses dark: a cookie, and the page says so
	guest.must("PUT", "/api/v1/scheme", map[string]string{"scheme": "dark"}, nil, 200)
	if tag, _ = page(guest); !strings.Contains(tag, `data-scheme="dark"`) || !strings.Contains(tag, `data-scheme-choice="dark"`) {
		t.Fatalf("guest chose dark: %s", tag)
	}
	// only known values
	if code := guest.do("PUT", "/api/v1/scheme", map[string]string{"scheme": "night"}, nil); code != 400 {
		t.Errorf("unknown scheme accepted: %d", code)
	}
	// a forged cookie never reaches the page
	evil := e.client()
	res, body := evil.raw("/", map[string]string{"Cookie": `ewiki_scheme="><script>alert(1)</script>`})
	if res.StatusCode != 200 || strings.Contains(body, "<script>alert") || strings.Contains(body, "data-scheme-choice") {
		t.Errorf("forged cookie in the page: %d", res.StatusCode)
	}
	// back to the default: the cookie is removed
	guest.must("PUT", "/api/v1/scheme", map[string]string{"scheme": ""}, nil, 200)
	if tag, _ = page(guest); strings.Contains(tag, "data-scheme-choice") || !strings.Contains(tag, `data-scheme="system"`) {
		t.Fatalf("guest back to the default: %s", tag)
	}

	// a person: the choice is kept in the personal settings (also on another device)
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("PUT", "/api/v1/scheme", map[string]string{"scheme": "light"}, nil, 200)
	var prefs struct {
		Prefs   map[string]string   `json:"prefs"`
		Options map[string][]string `json:"options"`
	}
	admin.must("GET", "/api/v1/me/prefs", nil, &prefs, 200)
	if prefs.Prefs["scheme"] != "light" || len(prefs.Options["scheme"]) != 3 {
		t.Fatalf("prefs: %+v", prefs)
	}
	other := e.client()
	other.login("admin", adminPW)
	if tag, _ = page(other); !strings.Contains(tag, `data-scheme="light"`) || !strings.Contains(tag, `data-scheme-choice="light"`) {
		t.Fatalf("person on another device: %s", tag)
	}
	// the personal setting wins over the cookie of the browser
	other.must("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"scheme": "dark"}}, nil, 204)
	if tag, _ = page(admin); !strings.Contains(tag, `data-scheme="dark"`) {
		t.Fatalf("personal setting does not win over the cookie: %s", tag)
	}

	// the template decides without a choice; a template with light colours only gets no scheme
	var s struct {
		Config struct {
			UI map[string]any `json:"ui"`
		} `json:"config"`
	}
	setTemplate := func(id string) {
		t.Helper()
		admin.must("GET", "/api/v1/admin/settings", nil, &s, 200)
		s.Config.UI["template"] = id
		admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": s.Config.UI}, nil, 200)
	}
	setTemplate("theme.wiki.dark")
	if tag, _ = page(e.client()); !strings.Contains(tag, `data-scheme="dark"`) {
		t.Fatalf("theme.wiki.dark: %s", tag)
	}
	setTemplate("theme.cms")
	if tag, _ = page(e.client()); strings.Contains(tag, "data-scheme=") {
		t.Fatalf("theme.cms has light colours only: %s", tag)
	}
	if tag, _ = page(admin); strings.Contains(tag, "data-scheme=") || !strings.Contains(tag, `data-scheme-choice="dark"`) {
		t.Fatalf("theme.cms with a choice: %s", tag)
	}
	var site struct {
		Templates []struct {
			ID          string `json:"id"`
			ColorScheme string `json:"color_scheme"`
		} `json:"templates"`
	}
	admin.must("GET", "/api/v1/site", nil, &site, 200)
	got := map[string]string{}
	for _, x := range site.Templates {
		got[x.ID] = x.ColorScheme
	}
	if got["theme.wiki"] != "system" || got["theme.wiki.dark"] != "dark" || got["theme.cms"] != "" {
		t.Errorf("color schemes of the templates: %v", got)
	}
}
