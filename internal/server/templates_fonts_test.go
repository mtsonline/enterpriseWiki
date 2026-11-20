package server_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
)

// TestTemplateFonts: uploaded fonts (WOFF2) for templates of data: only
// designers upload, with a license and the confirmation; the file is
// checked; a design may only use fonts that exist; the file is public only
// while a template in use has it; a font in use cannot be removed.
func TestTemplateFonts(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	const base = "/api/v1/x/core.template"
	raw, err := os.ReadFile("../../plugins/core_template/testdata/inter-sample.woff2") // Inter, SIL OFL 1.1
	if err != nil {
		t.Fatal(err)
	}
	data := base64.StdEncoding.EncodeToString(raw)
	in := func(change func(m map[string]any)) map[string]any {
		m := map[string]any{"name": "Inter sample", "license": "OFL-1.1", "license_note": "rsms/inter", "confirm": true, "data": data}
		if change != nil {
			change(m)
		}
		return m
	}

	// designers only
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "olaf", "password": "Olaf-Passw0rd!x"}, nil, 201)
	olaf, guest := e.client(), e.client()
	olaf.login("olaf", "Olaf-Passw0rd!x")
	if code := olaf.do("POST", base+"/fonts", in(nil), nil); code != 403 {
		t.Fatalf("upload without the permission: %d", code)
	}
	if code := olaf.do("GET", base+"/fonts", nil, nil); code != 403 {
		t.Fatalf("list without the permission: %d", code)
	}
	// what is refused
	for name, change := range map[string]func(m map[string]any){
		"not confirmed":        func(m map[string]any) { m["confirm"] = false },
		"unknown license":      func(m map[string]any) { m["license"] = "proprietary" },
		"other without a note": func(m map[string]any) { m["license"] = "other"; m["license_note"] = "" },
		"markup in the note":   func(m map[string]any) { m["license_note"] = "<b>x</b>" },
		"no name":              func(m map[string]any) { m["name"] = " " },
		"markup in the name":   func(m map[string]any) { m["name"] = "<script>" },
		"not base64":           func(m map[string]any) { m["data"] = "!!!" },
		"no WOFF2 (a TrueType)": func(m map[string]any) {
			m["data"] = base64.StdEncoding.EncodeToString(append([]byte("\x00\x01\x00\x00"), raw[4:]...))
		},
		"no WOFF2 (cut off)": func(m map[string]any) { m["data"] = base64.StdEncoding.EncodeToString(raw[:len(raw)-100]) },
		"no WOFF2 (an SVG script)": func(m map[string]any) {
			m["data"] = base64.StdEncoding.EncodeToString([]byte(`<svg onload="alert(1)"></svg>`))
		},
	} {
		if code := admin.do("POST", base+"/fonts", in(change), nil); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	var font struct {
		ID     int64    `json:"id"`
		Token  string   `json:"token"`
		Bytes  int      `json:"bytes"`
		UsedBy []string `json:"used_by"`
	}
	admin.must("POST", base+"/fonts", in(nil), &font, 201)
	if font.Token != fmt.Sprintf("own-%d", font.ID) || font.Bytes != len(raw) || len(font.UsedBy) != 0 {
		t.Fatalf("font: %+v", font)
	}
	file := fmt.Sprintf("%s/fonts/%d/file", base, font.ID)
	fetch := func(c *client) (int, []byte, http.Header) {
		t.Helper()
		res, err := c.http.Get(e.srv.URL + file)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, b, res.Header
	}
	// not used by any template yet: only designers get the file
	if code, _, _ := fetch(guest); code != 404 {
		t.Fatalf("file of an unused font for guests: %d", code)
	}
	if code, b, hd := fetch(admin); code != 200 || string(b) != string(raw) || hd.Get("Content-Type") != "font/woff2" || hd.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("file for a designer: %d %d bytes %v", code, len(b), hd)
	}

	// a design may only use fonts that exist
	var tpl tplResp
	admin.must("POST", base+"/templates", map[string]any{"names": map[string]string{"en": "With a font"}, "copy_of": "core.template/example-wiki"}, &tpl, 201)
	withFont := func(f string) map[string]any {
		var m map[string]any
		_ = json.Unmarshal(tpl.Design, &m)
		m["tokens"].(map[string]any)["font"] = f
		return m
	}
	if code := admin.do("PUT", base+"/templates/"+tpl.Key, map[string]any{"version": tpl.Version, "design": withFont(fmt.Sprintf("own-%d", font.ID+1000))}, nil); code != 400 {
		t.Fatalf("a font that does not exist: %d", code)
	}
	var chk struct {
		Problems []struct {
			Where string `json:"where"`
		} `json:"problems"`
	}
	admin.must("POST", base+"/check", map[string]any{"design": withFont(fmt.Sprintf("own-%d", font.ID+1000))}, &chk, 200)
	if len(chk.Problems) != 1 || chk.Problems[0].Where != "tokens.font" {
		t.Fatalf("check of a font that does not exist: %+v", chk)
	}
	admin.must("PUT", base+"/templates/"+tpl.Key, map[string]any{"version": tpl.Version, "design": withFont(font.Token)}, &tpl, 200)
	// used by a template that can be used: public, like the design
	if code, b, _ := fetch(guest); code != 200 || len(b) != len(raw) {
		t.Fatalf("file of a font in use for guests: %d", code)
	}
	var fonts []struct {
		ID     int64    `json:"id"`
		UsedBy []string `json:"used_by"`
	}
	admin.must("GET", base+"/fonts", nil, &fonts, 200)
	if len(fonts) != 1 || len(fonts[0].UsedBy) != 1 || fonts[0].UsedBy[0] != tpl.ID {
		t.Fatalf("fonts with use: %+v", fonts)
	}
	// in use (also in the trash): not removed
	if code := admin.do("DELETE", fmt.Sprintf("%s/fonts/%d", base, font.ID), nil, nil); code != 409 {
		t.Fatalf("remove a font in use: %d", code)
	}
	admin.must("DELETE", base+"/templates/"+tpl.Key, nil, nil, 204)
	if code, _, _ := fetch(guest); code != 404 {
		t.Fatalf("file of a font only used in the trash, for guests: %d", code)
	}
	if code := admin.do("DELETE", fmt.Sprintf("%s/fonts/%d", base, font.ID), nil, nil); code != 409 {
		t.Fatalf("remove a font used in the trash: %d", code)
	}
	admin.must("DELETE", base+"/templates/"+tpl.Key+"?purge=1", nil, nil, 204)
	admin.must("DELETE", fmt.Sprintf("%s/fonts/%d", base, font.ID), nil, nil, 204)
	if code, _, _ := fetch(admin); code != 404 {
		t.Fatalf("file of a removed font: %d", code)
	}
	var log struct {
		Entries []struct {
			Action string `json:"action"`
		} `json:"entries"`
	}
	admin.must("GET", "/api/v1/admin/audit/search?action=core.template.font.*", nil, &log, 200)
	if len(log.Entries) < 2 {
		t.Fatalf("audit of fonts: %+v", log)
	}
}
