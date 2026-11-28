package server_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestTemplateSettingsOfDesigns: a template of data offers the values of
// its design as settings, with the design as the defaults. The server
// checks the design with the values like a saved design (contrast, room,
// fonts); a copy takes the values as its defaults; logos are PNG, WebP or
// JPEG only; a font chosen in the settings cannot be deleted; deleting a
// template for good removes its settings.
func TestTemplateSettingsOfDesigns(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	const base = "/api/v1/x/core.template"
	type tpl struct {
		ID     string          `json:"id"`
		Key    string          `json:"key"`
		Design json.RawMessage `json:"design"`
	}
	tokens := func(x tpl) map[string]any {
		var d struct {
			Tokens map[string]any `json:"tokens"`
		}
		if err := json.Unmarshal(x.Design, &d); err != nil {
			t.Fatal(err)
		}
		return d.Tokens
	}
	var a tpl
	admin.must("POST", base+"/templates", map[string]any{"names": map[string]string{"en": "A"}, "copy_of": "core.template/example-wiki"}, &a, 201)
	surl := "/api/v1/admin/templates/" + url.PathEscape(a.ID) + "/settings"

	// the defaults are the values of the design
	var cur tplSettings
	admin.must("GET", surl, nil, &cur, 200)
	if len(cur.Settings) < 20 || cur.Values["primary"] != tokens(a)["primary"] || cur.Values["font"] != tokens(a)["font"] || len(cur.Changed) != 0 {
		t.Fatalf("defaults: %+v", cur)
	}
	// too little contrast, no room in the rows, an unknown font: refused,
	// in the preview too, and nothing is stored
	for _, bad := range []map[string]any{
		{"text": "#ffffff"},
		{"on_primary": tokens(a)["primary"]},
		{"font": "own-999999"},
	} {
		admin.must("PUT", surl, map[string]any{"values": bad}, nil, 400)
		// the preview shows the draft and says why saving would fail
		var pv struct {
			Problem string `json:"problem"`
		}
		admin.must("POST", surl+"/preview", map[string]any{"values": bad}, &pv, 200)
		if pv.Problem == "" {
			t.Fatalf("preview of %v: no problem named", bad)
		}
	}
	// a value of the wrong kind is refused at once
	admin.must("PUT", surl, map[string]any{"values": map[string]any{"primary": "red"}}, nil, 400)
	admin.must("POST", surl+"/preview", map[string]any{"values": map[string]any{"primary": "red"}}, nil, 400)
	admin.must("GET", surl, nil, &cur, 200)
	if len(cur.Changed) != 0 {
		t.Fatalf("a refused value was stored: %v", cur.Changed)
	}
	// good values are stored and given to every visitor
	_, mark := admin.upload(t, "mark.png", pngOf(t, 64, 64))
	admin.must("PUT", surl, map[string]any{"values": map[string]any{"primary": "#1A3A6B", "radius": 2, "mark": map[string]any{"hash": mark.Hash}}}, &cur, 200)
	if cur.Values["primary"] != "#1a3a6b" || len(cur.Changed) != 3 {
		t.Fatalf("stored: %+v", cur)
	}
	var pub struct {
		Values map[string]any `json:"values"`
	}
	e.client().must("GET", "/api/v1/templates/"+url.PathEscape(a.ID)+"/settings", nil, &pub, 200)
	if m, _ := pub.Values["mark"].(map[string]any); pub.Values["primary"] != "#1a3a6b" || pub.Values["radius"] != 2.0 || m["width"] != 64.0 {
		t.Fatalf("public: %v", pub.Values)
	}

	// a copy has the effective values as its design and keeps the mark
	var b tpl
	admin.must("POST", base+"/templates", map[string]any{"names": map[string]string{"en": "B"}, "copy_of": a.ID}, &b, 201)
	if tokens(b)["primary"] != "#1a3a6b" || tokens(b)["radius"] != 2.0 {
		t.Fatalf("copy: %v", tokens(b))
	}
	admin.must("GET", "/api/v1/admin/templates/"+url.PathEscape(b.ID)+"/settings", nil, &cur, 200)
	if !slices.Equal(cur.Changed, []string{"mark"}) || cur.Values["primary"] != "#1a3a6b" {
		t.Fatalf("settings of the copy: %+v", cur)
	}

	// the logo of a design: PNG, WebP or JPEG; then the default of its setting
	var full struct {
		Template struct {
			Version int             `json:"version"`
			Design  json.RawMessage `json:"design"`
		} `json:"template"`
	}
	admin.must("GET", base+"/templates/"+b.Key, nil, &full, 200)
	withLogo := func(logo string) map[string]any {
		var d map[string]any
		if err := json.Unmarshal(full.Template.Design, &d); err != nil {
			t.Fatal(err)
		}
		d["tokens"].(map[string]any)["logo"] = logo
		return map[string]any{"names": map[string]string{"en": "B"}, "design": d, "version": full.Template.Version}
	}
	code, svg := admin.upload(t, "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	if code == 201 || code == 200 {
		admin.must("PUT", base+"/templates/"+b.Key, withLogo("/api/v1/files/"+svg.Hash), nil, 400)
	}
	admin.must("PUT", base+"/templates/"+b.Key, withLogo("/api/v1/files/"+strings.Repeat("0", 64)), nil, 400)
	_, logo := admin.upload(t, "logo.png", pngOf(t, 200, 50))
	admin.must("PUT", base+"/templates/"+b.Key, withLogo("/api/v1/files/"+logo.Hash), nil, 200)
	// the storage clean-up keeps the logo of a design (old enough to be
	// cleaned up; a file nothing refers to is)
	_, other := admin.upload(t, "other.png", pngOf(t, 9, 9))
	ctx := context.Background()
	if _, err := e.app.DB.Exec(ctx, `UPDATE files SET created_at = now() - interval '2 days' WHERE hash = ANY($1)`, []string{logo.Hash, other.Hash}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.Files.GC(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.Files.Get(ctx, logo.Hash); err != nil {
		t.Fatalf("the logo of a design was cleaned up: %v", err)
	}
	if _, err := e.app.Files.Get(ctx, other.Hash); err == nil {
		t.Fatal("the clean-up kept a file nothing refers to")
	}
	var desc struct {
		Settings []struct {
			Key        string `json:"key"`
			DefaultSrc string `json:"default_src"`
		} `json:"settings"`
	}
	admin.must("GET", "/api/v1/admin/templates/"+url.PathEscape(b.ID)+"/settings", nil, &desc, 200)
	if i := slices.IndexFunc(desc.Settings, func(s struct {
		Key        string `json:"key"`
		DefaultSrc string `json:"default_src"`
	}) bool {
		return s.Key == "logo"
	}); i < 0 || desc.Settings[i].DefaultSrc != "/api/v1/files/"+logo.Hash {
		t.Fatalf("logo default: %+v", desc.Settings)
	}

	// a font chosen in the settings is in use: it cannot be deleted
	raw, err := os.ReadFile("../../plugins/core_template/testdata/inter-sample.woff2") // Inter, SIL OFL 1.1
	if err != nil {
		t.Fatal(err)
	}
	var font struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", base+"/fonts", map[string]any{"name": "Inter sample", "license": "OFL-1.1", "confirm": true,
		"files": []any{map[string]any{"data": base64.StdEncoding.EncodeToString(raw), "weight": "100 900"}}}, &font, 201)
	own := "own-" + strconv.FormatInt(font.ID, 10)
	admin.must("PUT", surl, map[string]any{"values": map[string]any{"heading_font": own}}, nil, 200)
	var fonts []struct {
		ID     int64    `json:"id"`
		UsedBy []string `json:"used_by"`
	}
	admin.must("GET", base+"/fonts", nil, &fonts, 200)
	if i := slices.IndexFunc(fonts, func(f struct {
		ID     int64    `json:"id"`
		UsedBy []string `json:"used_by"`
	}) bool {
		return f.ID == font.ID
	}); i < 0 || !slices.Contains(fonts[i].UsedBy, a.ID) {
		t.Fatalf("used by: %+v", fonts)
	}
	admin.must("DELETE", base+"/fonts/"+strconv.FormatInt(font.ID, 10), nil, nil, 409)

	// deleted for good: its settings go with it
	admin.must("DELETE", base+"/templates/"+a.Key, nil, nil, 204)
	admin.must("DELETE", base+"/templates/"+a.Key+"?purge=1", nil, nil, 204)
	if _, ok := e.app.Config.Get().TemplateSettings[a.ID]; ok {
		t.Fatal("the settings of a template deleted for good are still stored")
	}
	admin.must("DELETE", base+"/fonts/"+strconv.FormatInt(font.ID, 10), nil, nil, 204)
}
