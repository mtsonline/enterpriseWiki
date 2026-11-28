package server_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// pngOf returns a PNG image of w × h pixels.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 10, G: 55, B: 96, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type tplSettings struct {
	Settings []struct {
		Key  string `json:"key"`
		Kind string `json:"kind"`
	} `json:"settings"`
	Values  map[string]any `json:"values"`
	Changed []string       `json:"changed"`
}

// TestTemplateSettings: the settings a template declares are checked by
// the server, stored, given to every visitor, recorded in the audit log and
// set back; only for those who manage the settings.
func TestTemplateSettings(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	const url = "/api/v1/admin/templates/theme.cms/settings"
	var cur tplSettings
	admin.must("GET", url, nil, &cur, 200)
	if len(cur.Settings) == 0 || cur.Values["primary_color"] != "#0a3760" || len(cur.Changed) != 0 {
		t.Fatalf("defaults: %+v", cur)
	}
	// values of the wrong kind or beyond the limits are refused
	for _, bad := range []map[string]any{
		{"primary_color": "red;}body{display:none"},
		{"hero_button_link": "javascript:alert(1)"},
		{"hero_button_link": "//evil.example.com"},
		{"logo_height": 1000},
		{"header": "fixed"},
		{"not_declared": "x"},
		{"logo": "0000000000000000000000000000000000000000000000000000000000000000"},
	} {
		admin.must("PUT", url, map[string]any{"values": bad}, nil, 400)
	}
	// an SVG is not an image a template may use, even when uploaded
	code, svg := admin.upload(t, "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	if code == 201 || code == 200 {
		admin.must("PUT", url, map[string]any{"values": map[string]any{"logo": svg.Hash}}, nil, 400)
	}
	// a PNG with its size read from the file (a size sent along counts for nothing)
	code, logo := admin.upload(t, "logo.png", pngOf(t, 300, 120))
	if code != 201 && code != 200 {
		t.Fatalf("upload: %d", code)
	}
	admin.must("PUT", url, map[string]any{"values": map[string]any{
		"primary_color": "#7A1F1F", "logo": map[string]any{"hash": logo.Hash, "width": 1, "height": 1}, "copyright": "© {year} Example",
	}}, &cur, 200)
	if cur.Values["primary_color"] != "#7a1f1f" || len(cur.Changed) != 3 {
		t.Fatalf("stored: %+v", cur)
	}
	// every visitor gets the effective values; images as address and size
	var out struct {
		Values map[string]any `json:"values"`
	}
	e.client().must("GET", "/api/v1/templates/theme.cms/settings", nil, &out, 200)
	pub := out.Values
	img, _ := pub["logo"].(map[string]any)
	if img["src"] != "/api/v1/files/"+logo.Hash || img["width"] != 300.0 || img["height"] != 120.0 || pub["back_to_top"] != true {
		t.Fatalf("public: %v", pub)
	}
	e.client().must("GET", "/api/v1/templates/no.such.template/settings", nil, nil, 404)
	// the look: the CSS variables of the changed colours, on the element of the template
	var look struct {
		Look struct {
			Light map[string]string `json:"light"`
			Dark  map[string]string `json:"dark"`
			All   map[string]string `json:"all"`
		} `json:"look"`
		Root string `json:"root"`
	}
	e.client().must("GET", "/api/v1/templates/theme.cms/settings", nil, &look, 200)
	if look.Root != ".ewcms-app" || look.Look.Light["--ewcms-primary-l"] != "#7a1f1f" || len(look.Look.Dark) != 0 || look.Look.All["--ewcms-logo-h"] != "" {
		t.Fatalf("look: %+v", look)
	}
	// the preview shows a draft with the look it would give, without storing it
	look.Look.Light, look.Look.Dark, look.Look.All = nil, nil, nil
	admin.must("POST", url+"/preview", map[string]any{"values": map[string]any{"page_bg_dark": "#101010", "font": "inter", "primary_color": nil}}, &look, 200)
	if look.Look.Dark["--ewcms-page-bg"] != "#101010" || look.Look.All["--ewcms-font"] == "" || look.Look.Light["--ewcms-primary-l"] != "" {
		t.Fatalf("preview: %+v", look)
	}
	admin.must("POST", url+"/preview", map[string]any{"values": map[string]any{"font": "Comic Sans"}}, nil, 400)
	admin.must("GET", url, nil, &cur, 200)
	if len(cur.Changed) != 3 {
		t.Fatalf("the preview stored something: %v", cur.Changed)
	}
	// each template has its own settings: the modern variant still has the defaults
	e.client().must("GET", "/api/v1/templates/theme.cms.modern/settings", nil, &out, 200)
	if out.Values["primary_color"] != "#2450b8" {
		t.Fatalf("the variant shares the values: %v", pub)
	}
	// the template of the site: its settings come with /site
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": map[string]any{"template": "theme.cms", "site_name": "Test"}}, nil, 200)
	var site struct {
		TemplateSettings struct {
			Values map[string]any `json:"values"`
		} `json:"template_settings"`
	}
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if site.TemplateSettings.Values["primary_color"] != "#7a1f1f" {
		t.Fatalf("/site: %v", site.TemplateSettings)
	}
	// the general settings neither show nor overwrite them
	var all map[string]any
	admin.must("GET", "/api/v1/admin/settings", nil, &all, 200)
	if c, _ := all["config"].(map[string]any); c["template_settings"] != nil {
		t.Fatal("template settings in the general settings")
	}
	// the logo is kept by the storage clean-up while a setting refers to it
	// (both files old enough to be cleaned up; the other one is)
	_, other := admin.upload(t, "other.png", pngOf(t, 10, 10))
	ctx := context.Background()
	if _, err := e.app.DB.Exec(ctx, `UPDATE files SET created_at = now() - interval '2 days' WHERE hash = ANY($1)`, []string{logo.Hash, other.Hash}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.Files.GC(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.Files.Get(ctx, logo.Hash); err != nil {
		t.Fatalf("the logo of a template was cleaned up: %v", err)
	}
	if _, err := e.app.Files.Get(ctx, other.Hash); err == nil {
		t.Fatal("the clean-up kept a file nothing refers to (the check above proves nothing)")
	}
	// null: back to the default; then everything back
	admin.must("PUT", url, map[string]any{"values": map[string]any{"copyright": nil}}, &cur, 200)
	if len(cur.Changed) != 2 || cur.Values["copyright"] != "" {
		t.Fatalf("reset one: %+v", cur)
	}
	admin.must("DELETE", url, nil, nil, 204)
	admin.must("GET", url, nil, &cur, 200)
	if len(cur.Changed) != 0 || cur.Values["primary_color"] != "#0a3760" {
		t.Fatalf("reset all: %+v", cur)
	}
	var n int
	if err := e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action='settings.template'`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("audit: %d %v", n, err)
	}
	// only for those who manage the settings
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "tina", "password": "Tina-Passw0rd!", "display_name": "Tina"}, nil, 201)
	tina := e.client()
	tina.login("tina", "Tina-Passw0rd!")
	tina.must("GET", url, nil, nil, 403)
	tina.must("PUT", url, map[string]any{"values": map[string]any{"primary_color": "#000000"}}, nil, 403)
	tina.must("DELETE", url, nil, nil, 403)
	tina.must("POST", url+"/preview", map[string]any{"values": map[string]any{}}, nil, 403)
}
