package server_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type tplResp struct {
	ID       string            `json:"id"`
	Key      string            `json:"key"`
	Names    map[string]string `json:"names"`
	Design   json.RawMessage   `json:"design"`
	Version  int               `json:"version"`
	ReadOnly bool              `json:"read_only"`
	BasedOn  string            `json:"based_on"`
}

// TestDataTemplates covers website templates of data (core.template): the
// own permission core.template.design on every way, the shipped templates
// (read only), copies, checks of the design, versions with conflicts, the
// use as template of the site and of categories, the trash and import.
func TestDataTemplates(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)

	// a designer: only the permission core.template.design, no administrator
	var role, u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Designers", "permissions": []string{"core.template.design"}}, &role, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "dana", "password": "Dana-Passw0rd!x", "roles": []int64{role.ID}}, &u, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "olaf", "password": "Olaf-Passw0rd!x"}, nil, 201)
	dana, olaf, guest := e.client(), e.client(), e.client()
	dana.login("dana", "Dana-Passw0rd!x")
	olaf.login("olaf", "Olaf-Passw0rd!x")

	const base = "/api/v1/x/core.template"
	// the designs are public (the pages are built from them); the rest needs the permission
	var designs []tplResp
	guest.must("GET", base+"/designs", nil, &designs, 200)
	if len(designs) != 2 || designs[0].ID != "core.template/example-wiki" || len(designs[0].Design) == 0 {
		t.Fatalf("designs: %+v", designs)
	}
	for _, path := range []string{"/templates", "/templates/example-wiki", "/templates/example-wiki/versions", "/templates/example-wiki/export"} {
		if code := olaf.do("GET", base+path, nil, nil); code != 403 {
			t.Fatalf("GET %s without the permission: %d", path, code)
		}
	}
	if code := olaf.do("POST", base+"/templates", map[string]any{"names": map[string]string{"en": "X"}, "copy_of": "core.template/example-wiki"}, nil); code != 403 {
		t.Fatalf("create without the permission: %d", code)
	}
	if code := guest.do("POST", base+"/check", map[string]any{"design": map[string]any{}}, nil); code != 401 {
		t.Fatalf("check as guest: %d", code)
	}

	// the shipped templates are read only
	if code := dana.do("PUT", base+"/templates/example-wiki", map[string]any{"names": map[string]string{"en": "X"}, "version": 1}, nil); code != 403 {
		t.Fatalf("change a shipped template: %d", code)
	}
	if code := dana.do("DELETE", base+"/templates/example-wiki", nil, nil); code != 403 {
		t.Fatalf("delete a shipped template: %d", code)
	}

	// a copy
	var tpl tplResp
	dana.must("POST", base+"/templates", map[string]any{"names": map[string]string{"en": "Our wiki", "de": "Unser Wiki"}, "copy_of": "core.template/example-wiki"}, &tpl, 201)
	if !strings.HasPrefix(tpl.ID, "core.template/") || tpl.Version != 1 || tpl.BasedOn != "core.template/example-wiki" || tpl.ReadOnly {
		t.Fatalf("copy: %+v", tpl)
	}
	if code := dana.do("POST", base+"/templates", map[string]any{"names": map[string]string{"en": "<b>X</b>"}, "copy_of": "core.template/example-wiki"}, nil); code != 400 {
		t.Fatalf("name with markup: %d", code)
	}

	// the design is checked: contrast, room, unknown fields
	var d map[string]any
	_ = json.Unmarshal(tpl.Design, &d)
	bad := func(change func(m map[string]any)) map[string]any {
		var m map[string]any
		_ = json.Unmarshal(tpl.Design, &m)
		change(m)
		return m
	}
	for name, design := range map[string]map[string]any{
		"contrast":      bad(func(m map[string]any) { m["tokens"].(map[string]any)["text"] = "#bbbbbb" }),
		"unknown field": bad(func(m map[string]any) { m["onload"] = "x" }),
		"no content":    bad(func(m map[string]any) { m["rows"] = m["rows"].([]any)[:2] }),
	} {
		if code, body := dana.rawJSON("PUT", base+"/templates/"+tpl.Key, map[string]any{"design": design, "version": 1}); code != 400 {
			t.Fatalf("%s: %d %v", name, code, body)
		}
	}
	var check struct {
		Problems []struct{ Where, Text string } `json:"problems"`
	}
	dana.must("POST", base+"/check", map[string]any{"design": bad(func(m map[string]any) { m["tokens"].(map[string]any)["text"] = "#bbbbbb" })}, &check, 200)
	if len(check.Problems) == 0 || !strings.Contains(check.Problems[0].Text, "contrast") {
		t.Fatalf("check: %+v", check)
	}
	// a change makes a new version; an outdated version is refused
	d["tokens"].(map[string]any)["primary"] = "#1d4ed8"
	dana.must("PUT", base+"/templates/"+tpl.Key, map[string]any{"design": d, "version": 1}, &tpl, 200)
	if tpl.Version != 2 {
		t.Fatalf("version: %d", tpl.Version)
	}
	if code := dana.do("PUT", base+"/templates/"+tpl.Key, map[string]any{"names": map[string]string{"en": "Late"}, "version": 1}, nil); code != 409 {
		t.Fatalf("outdated version: %d", code)
	}
	var versions []struct {
		Version int `json:"version"`
	}
	dana.must("GET", base+"/templates/"+tpl.Key+"/versions", nil, &versions, 200)
	if len(versions) != 2 || versions[0].Version != 2 {
		t.Fatalf("versions: %+v", versions)
	}

	// it can be used: in the list of the site, for the site and a category
	var site struct {
		Templates []struct{ ID string } `json:"templates"`
	}
	guest.must("GET", "/api/v1/site", nil, &site, 200)
	found := false
	for _, x := range site.Templates {
		found = found || x.ID == tpl.ID
	}
	if !found {
		t.Fatalf("not offered: %+v", site.Templates)
	}
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Designed"}, "template": tpl.ID}, &c, 201)
	if code := admin.do("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Unknown"}, "template": "core.template/999999"}, nil); code != 400 {
		t.Fatalf("unknown template for a category: %d", code)
	}
	var s struct {
		Config struct {
			UI map[string]any `json:"ui"`
		} `json:"config"`
	}
	admin.must("GET", "/api/v1/admin/settings", nil, &s, 200)
	s.Config.UI["template"] = "core.template/999999"
	if code := admin.do("PUT", "/api/v1/admin/settings", map[string]any{"ui": s.Config.UI}, nil); code != 400 {
		t.Fatalf("unknown template for the site: %d", code)
	}
	// the default placements of the template belong to its designers
	dana.must("PUT", "/api/v1/placements/template?target="+tpl.ID, map[string]any{"placements": []map[string]any{{"block": "core.search", "position": "sidebar", "sort": 1}}}, nil, 200)
	if code := olaf.do("PUT", "/api/v1/placements/template?target="+tpl.ID, map[string]any{"placements": []map[string]any{}}, nil); code != 403 {
		t.Fatalf("placements of the template without the permission: %d", code)
	}

	// in use: not to the trash
	if code := dana.do("DELETE", base+"/templates/"+tpl.Key, nil, nil); code != 409 {
		t.Fatalf("delete while in use: %d", code)
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/categories/%d", c.ID), nil, nil, 204)
	dana.must("DELETE", base+"/templates/"+tpl.Key, nil, nil, 204)
	guest.must("GET", base+"/designs", nil, &designs, 200)
	if len(designs) != 2 {
		t.Fatalf("a template in the trash is still used: %+v", designs)
	}
	if code := admin.do("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Trash"}, "template": tpl.ID}, nil); code != 400 {
		t.Fatalf("a template in the trash for a category: %d", code)
	}
	var trash []tplResp
	dana.must("GET", base+"/templates?deleted=1", nil, &trash, 200)
	if len(trash) != 1 || trash[0].ID != tpl.ID {
		t.Fatalf("trash: %+v", trash)
	}
	dana.must("POST", base+"/templates/"+tpl.Key+"/restore", nil, nil, 200)
	if code := dana.do("DELETE", base+"/templates/"+tpl.Key+"?purge=1", nil, nil); code != 400 {
		t.Fatalf("purge outside the trash: %d", code)
	}
	dana.must("DELETE", base+"/templates/"+tpl.Key, nil, nil, 204)
	dana.must("DELETE", base+"/templates/"+tpl.Key+"?purge=1", nil, nil, 204)
	var lv struct {
		Placements []any `json:"placements"`
	}
	if code := admin.do("GET", "/api/v1/placements/template?target="+tpl.ID, nil, &lv); code == 200 && len(lv.Placements) > 0 {
		t.Fatalf("placements of a removed template remain: %+v", lv)
	}

	// export and import
	var file map[string]any
	dana.must("GET", base+"/templates/example-cms/export", nil, &file, 200)
	if file["format"] != "ewiki-template" {
		t.Fatalf("export: %+v", file)
	}
	file["names"] = map[string]string{"en": "Imported"}
	var imp tplResp
	dana.must("POST", base+"/templates/import", file, &imp, 201)
	if imp.Names["en"] != "Imported" || imp.Version != 1 {
		t.Fatalf("import: %+v", imp)
	}
	file["design"].(map[string]any)["tokens"].(map[string]any)["font"] = "Comic Sans"
	if code := dana.do("POST", base+"/templates/import", file, nil); code != 400 {
		t.Fatalf("import of a bad design: %d", code)
	}
}
