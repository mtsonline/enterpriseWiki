package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
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
	for _, v := range []string{"night", "x><script>alert(1)</script>"} {
		res, body := e.client().raw("/", map[string]string{"Cookie": "ewiki_scheme=" + v})
		if res.StatusCode != 200 || strings.Contains(body, "<script>alert") || strings.Contains(body, "data-scheme-choice") || strings.Contains(body, "night") {
			t.Errorf("forged cookie %q in the page: %d", v, res.StatusCode)
		}
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
	// the settings of a template choose its default scheme (a setting the
	// core adds to every template with dark colours)
	setTemplate("theme.wiki")
	type setting struct {
		Key     string   `json:"key"`
		Default any      `json:"default"`
		Options []string `json:"options"`
	}
	var desc struct {
		Settings []setting `json:"settings"`
	}
	admin.must("GET", "/api/v1/admin/templates/theme.wiki/settings", nil, &desc, 200)
	if i := slices.IndexFunc(desc.Settings, func(x setting) bool { return x.Key == "color_scheme" }); i < 0 || desc.Settings[i].Default != "system" || len(desc.Settings[i].Options) != 3 {
		t.Fatalf("scheme setting of theme.wiki: %+v", desc.Settings)
	}
	admin.must("PUT", "/api/v1/admin/templates/theme.wiki/settings", map[string]any{"values": map[string]any{"color_scheme": "dark"}}, nil, 200)
	if tag, _ = page(e.client()); !strings.Contains(tag, `data-scheme="dark"`) {
		t.Fatalf("theme.wiki set to dark: %s", tag)
	}
	admin.must("PUT", "/api/v1/admin/templates/theme.wiki/settings", map[string]any{"values": map[string]any{"color_scheme": "purple"}}, nil, 400)
	admin.must("DELETE", "/api/v1/admin/templates/theme.wiki/settings", nil, nil, 204)
	if tag, _ = page(e.client()); !strings.Contains(tag, `data-scheme="system"`) {
		t.Fatalf("theme.wiki back to its default: %s", tag)
	}
	// a template with light colours only has no such setting
	admin.must("GET", "/api/v1/admin/templates/core.template%2Fexample-wiki/settings", nil, &desc, 200)
	if slices.ContainsFunc(desc.Settings, func(x setting) bool { return x.Key == "color_scheme" }) {
		t.Fatal("a template with light colours only offers a scheme")
	}
	admin.must("PUT", "/api/v1/admin/templates/core.template%2Fexample-wiki/settings", map[string]any{"values": map[string]any{"color_scheme": "dark"}}, nil, 400)
	setTemplate("theme.cms")
	if tag, _ = page(e.client()); !strings.Contains(tag, `data-scheme="system"`) {
		t.Fatalf("theme.cms follows the device: %s", tag)
	}
	// a template of the designer has light colours only (no dark palette yet)
	setTemplate("core.template/example-wiki")
	if tag, _ = page(e.client()); strings.Contains(tag, "data-scheme=") {
		t.Fatalf("a designed template has light colours only: %s", tag)
	}
	if tag, _ = page(admin); strings.Contains(tag, "data-scheme=") || !strings.Contains(tag, `data-scheme-choice="dark"`) {
		t.Fatalf("a light template with a choice: %s", tag)
	}
	// a designed template with dark colors follows the choice; its default is in the design
	const base = "/api/v1/x/core.template"
	var tpl tplResp
	admin.must("POST", base+"/templates", map[string]any{"names": map[string]string{"en": "Dark too"}, "copy_of": "core.template/example-wiki"}, &tpl, 201)
	var d map[string]any
	if err := json.Unmarshal(tpl.Design, &d); err != nil {
		t.Fatal(err)
	}
	dark := map[string]string{"bg": "#1d2125", "bg_subtle": "#22272b", "text": "#dee4ea", "text_muted": "#b6c2cf", "border": "#8c9bab", "primary": "#85b8ff",
		"on_primary": "#10151a", "link": "#85b8ff", "focus": "#85b8ff", "header_bg": "#161a1d", "header_text": "#dee4ea", "footer_bg": "#161a1d", "footer_text": "#dee4ea"}
	d["tokens"].(map[string]any)["dark"] = dark
	// too little contrast in the dark is refused like in the light
	dark["text"] = "#333333"
	if code := admin.do("PUT", base+"/templates/"+tpl.Key, map[string]any{"version": tpl.Version, "design": d}, nil); code != 400 {
		t.Fatalf("dark colors with too little contrast: %d", code)
	}
	dark["text"] = "#dee4ea"
	admin.must("PUT", base+"/templates/"+tpl.Key, map[string]any{"version": tpl.Version, "design": d}, &tpl, 200)
	setTemplate(tpl.ID)
	if tag, _ = page(e.client()); !strings.Contains(tag, `data-scheme="system"`) {
		t.Fatalf("designed template with dark colors: %s", tag)
	}
	d["options"].(map[string]any)["color_scheme"] = "dark"
	admin.must("PUT", base+"/templates/"+tpl.Key, map[string]any{"version": tpl.Version, "design": d}, &tpl, 200)
	if tag, _ = page(e.client()); !strings.Contains(tag, `data-scheme="dark"`) {
		t.Fatalf("designed template, dark by default: %s", tag)
	}
	// its settings choose another; /site names the effective one
	sp := "/api/v1/admin/templates/" + url.PathEscape(tpl.ID) + "/settings"
	admin.must("PUT", sp, map[string]any{"values": map[string]any{"color_scheme": "light"}}, nil, 200)
	if tag, _ = page(e.client()); !strings.Contains(tag, `data-scheme="light"`) {
		t.Fatalf("designed template set to light in its settings: %s", tag)
	}
	admin.must("PUT", "/api/v1/admin/templates/theme.wiki/settings", map[string]any{"values": map[string]any{"color_scheme": "dark"}}, nil, 200)
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
	if got["theme.wiki"] != "dark" || got["theme.cms"] != "system" || got["theme.cms.modern"] != "system" ||
		got["core.template/example-wiki"] != "" || got[tpl.ID] != "light" {
		t.Errorf("color schemes of the templates: %v", got)
	}
}
