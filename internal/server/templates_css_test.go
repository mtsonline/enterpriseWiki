package server_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestTemplateCSS: own CSS of a template only while ui.custom_css is on;
// checked on saving (nothing silently removed); not delivered while off.
func TestTemplateCSS(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	const base = "/api/v1/x/core.template"
	var tpl tplResp
	admin.must("POST", base+"/templates", map[string]any{"names": map[string]string{"en": "Styled"}, "copy_of": "core.template/example-wiki"}, &tpl, 201)
	withCSS := func(css string) map[string]any {
		var m map[string]any
		_ = json.Unmarshal(tpl.Design, &m)
		m["css"] = css
		return m
	}
	setCSS := func(on bool) {
		var cfg struct {
			Config map[string]any `json:"config"`
		}
		admin.must("GET", "/api/v1/admin/settings", nil, &cfg, 200)
		ui := cfg.Config["ui"].(map[string]any)
		ui["custom_css"] = on
		admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil, 200)
	}
	good := ".ew-titlebar { border-bottom: 3px solid #102040; }"
	// switched off (default): refused, and the check says why
	if code := admin.do("PUT", base+"/templates/"+tpl.Key, map[string]any{"version": tpl.Version, "design": withCSS(good)}, nil); code != 400 {
		t.Fatalf("own CSS while switched off: %d", code)
	}
	var chk struct {
		Problems []struct {
			Where string `json:"where"`
		} `json:"problems"`
	}
	admin.must("POST", base+"/check", map[string]any{"design": withCSS(good)}, &chk, 200)
	if len(chk.Problems) != 1 || chk.Problems[0].Where != "css" {
		t.Fatalf("check while switched off: %+v", chk)
	}
	// switched on: what loads from elsewhere is refused with its line
	setCSS(true)
	var body string
	for _, bad := range []string{"a{}\n@import 'x.css';", ".x{background:url(https://evil.example/t.png)}", ".x{background:\\75 rl(x)}"} {
		code := admin.do("PUT", base+"/templates/"+tpl.Key, map[string]any{"version": tpl.Version, "design": withCSS(bad)}, &body)
		if code != 400 {
			t.Errorf("bad CSS %q accepted: %d", bad, code)
		}
	}
	admin.must("PUT", base+"/templates/"+tpl.Key, map[string]any{"version": tpl.Version, "design": withCSS(good)}, &tpl, 200)
	css := func() string {
		var ds []tplResp
		e.client().must("GET", base+"/designs", nil, &ds, 200)
		for _, d := range ds {
			if d.ID == tpl.ID {
				var m map[string]any
				_ = json.Unmarshal(d.Design, &m)
				s, _ := m["css"].(string)
				return s
			}
		}
		t.Fatal("template not among the designs")
		return ""
	}
	if !strings.Contains(css(), "border-bottom") {
		t.Errorf("own CSS not delivered while switched on")
	}
	// switched off again: no longer used
	setCSS(false)
	if css() != "" {
		t.Errorf("own CSS delivered while switched off")
	}
}
