package server_test

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestTemplateRights: rights of one template narrow down who of the
// designers may change it (edit) and manage it (rights, trash, own CSS).
// They never give anything without core.template.design; administrators
// always may; nobody locks themselves out; the rights are checked on every
// way that changes the template.
func TestTemplateRights(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	const base = "/api/v1/x/core.template"

	type idResp struct {
		ID int64 `json:"id"`
	}
	var role, grp idResp
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Designers", "permissions": []string{"core.template.design"}}, &role, 201)
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Design team"}, &grp, 201)
	users := map[string]int64{}
	clients := map[string]*client{}
	for _, n := range []string{"dana", "erik", "fritz", "olaf"} {
		var u idResp
		in := map[string]any{"username": n, "password": "Pw-" + n + "-Passw0rd!x"}
		if n != "olaf" { // olaf is no designer
			in["roles"] = []int64{role.ID}
		}
		admin.must("POST", "/api/v1/admin/users", in, &u, 201)
		users[n] = u.ID
		clients[n] = e.client()
		clients[n].login(n, "Pw-"+n+"-Passw0rd!x")
	}
	dana, erik, fritz, olaf := clients["dana"], clients["erik"], clients["fritz"], clients["olaf"]
	user := func(n string) map[string]any { return map[string]any{"type": "user", "id": users[n]} }

	type tplMay struct {
		tplResp
		May struct {
			Edit   bool `json:"edit"`
			Manage bool `json:"manage"`
		} `json:"may"`
		EditSubjects   []map[string]any `json:"edit_subjects"`
		ManageSubjects []map[string]any `json:"manage_subjects"`
	}
	var tpl tplMay
	dana.must("POST", base+"/templates", map[string]any{"names": map[string]string{"en": "Ours"}, "copy_of": "core.template/example-wiki"}, &tpl, 201)
	path := base + "/templates/" + tpl.Key
	load := func(c *client) tplMay {
		var r struct {
			Template tplMay `json:"template"`
		}
		c.must("GET", path, nil, &r, 200)
		return r.Template
	}
	rename := func(c *client, name string) int {
		cur := load(admin)
		return c.do("PUT", path, map[string]any{"names": map[string]string{"en": name}, "version": cur.Version}, nil)
	}

	// without rights of its own: every designer may change and manage it
	if m := load(fritz).May; !m.Edit || !m.Manage {
		t.Fatalf("open template: %+v", m)
	}
	if code := rename(fritz, "Fritz was here"); code != 200 {
		t.Fatalf("a designer changes an open template: %d", code)
	}

	// the shipped templates have no rights of their own
	if code := dana.do("PUT", base+"/templates/example-wiki/rights", map[string]any{"edit_subjects": []any{}}, nil); code != 403 {
		t.Fatalf("rights of a shipped template: %d", code)
	}
	// subjects are checked; guests make no sense
	for _, bad := range []any{
		[]any{map[string]any{"type": "guest"}},
		[]any{map[string]any{"type": "user"}},
		[]any{map[string]any{"type": "user", "id": 0}},
		[]any{map[string]any{"type": "nobody", "id": 1}},
	} {
		if code := dana.do("PUT", path+"/rights", map[string]any{"edit_subjects": bad}, nil); code != 400 {
			t.Errorf("subjects %v: %d", bad, code)
		}
	}
	// nobody locks themselves out
	if code := dana.do("PUT", path+"/rights", map[string]any{"manage_subjects": []any{user("erik")}}, nil); code != 400 {
		t.Fatalf("dana gives away managing: %d", code)
	}

	// erik may change it (and the group of the team), dana manages it
	dana.must("PUT", path+"/rights", map[string]any{
		"edit_subjects":   []any{user("erik"), user("olaf"), map[string]any{"type": "group", "id": grp.ID}},
		"manage_subjects": []any{user("dana")},
	}, &tpl, 200)
	if len(tpl.EditSubjects) != 3 || len(tpl.ManageSubjects) != 1 {
		t.Fatalf("rights stored: %+v %+v", tpl.EditSubjects, tpl.ManageSubjects)
	}
	if m := load(erik).May; !m.Edit || m.Manage {
		t.Fatalf("erik: %+v", m)
	}
	if m := load(fritz).May; m.Edit || m.Manage {
		t.Fatalf("fritz: %+v", m)
	}
	if code := rename(erik, "Erik's"); code != 200 {
		t.Fatalf("erik changes: %d", code)
	}
	if code := rename(fritz, "Fritz again"); code != 403 {
		t.Fatalf("fritz changes a template limited to others: %d", code)
	}
	// a template limited to others can still be read and copied (the designs are public anyway)
	if code := fritz.do("POST", base+"/templates", map[string]any{"names": map[string]string{"en": "Fritz's copy"}, "copy_of": tpl.ID}, nil); code != 201 {
		t.Fatalf("fritz copies: %d", code)
	}
	// rights of a template never give anything without core.template.design
	if code := rename(olaf, "Olaf"); code != 403 {
		t.Fatalf("olaf (no designer, but named) changes: %d", code)
	}
	if code := olaf.do("GET", path, nil, nil); code != 403 {
		t.Fatalf("olaf reads: %d", code)
	}
	// only managers: the rights, the trash, own CSS
	if code := erik.do("PUT", path+"/rights", map[string]any{"edit_subjects": []any{}}, nil); code != 403 {
		t.Fatalf("erik changes the rights: %d", code)
	}
	if code := erik.do("DELETE", path, nil, nil); code != 403 {
		t.Fatalf("erik moves it to the trash: %d", code)
	}
	var cfg struct {
		Config map[string]any `json:"config"`
	}
	admin.must("GET", "/api/v1/admin/settings", nil, &cfg, 200)
	ui := cfg.Config["ui"].(map[string]any)
	ui["custom_css"] = true
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil, 200)
	withCSS := func(css string) map[string]any {
		var m map[string]any
		_ = json.Unmarshal(load(admin).Design, &m)
		m["css"] = css
		return m
	}
	if code := erik.do("PUT", path, map[string]any{"design": withCSS(".x { color: #102040; }"), "version": load(admin).Version}, nil); code != 403 {
		t.Fatalf("erik writes own CSS: %d", code)
	}
	dana.must("PUT", path, map[string]any{"design": withCSS(".x { color: #102040; }"), "version": load(admin).Version}, nil, 200)
	// with the CSS unchanged, erik may still change the rest
	d := withCSS(".x { color: #102040; }")
	d["options"].(map[string]any)["back_to_top"] = true
	if code := erik.do("PUT", path, map[string]any{"design": d, "version": load(admin).Version}, nil); code != 200 {
		t.Fatalf("erik changes the design, own CSS unchanged: %d", code)
	}

	// a group: fritz joins the team and may change it
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", users["fritz"]), map[string]any{"groups": []int64{grp.ID}}, nil, 200)
	fritz.login("fritz", "Pw-fritz-Passw0rd!x")
	if code := rename(fritz, "Team"); code != 200 {
		t.Fatalf("fritz in the team: %d", code)
	}
	// administrators always may
	if code := rename(admin, "Admin"); code != 200 {
		t.Fatalf("administrator: %d", code)
	}
	admin.must("PUT", path+"/rights", map[string]any{"edit_subjects": []any{}, "manage_subjects": []any{}}, nil, 200)
	if m := load(erik).May; !m.Edit || !m.Manage {
		t.Fatalf("open again: %+v", m)
	}
	// the trash: manage needed (dana manages again after the reset, as everybody)
	dana.must("PUT", path+"/rights", map[string]any{"manage_subjects": []any{user("dana")}}, nil, 200)
	if code := erik.do("DELETE", path, nil, nil); code != 403 {
		t.Fatalf("erik trashes (only managers are named: only they may change it): %d", code)
	}
	dana.must("DELETE", path, nil, nil, 204)
	if code := erik.do("POST", path+"/restore", nil, nil); code != 403 {
		t.Fatalf("erik restores: %d", code)
	}
	dana.must("POST", path+"/restore", nil, nil, 200)

	var log struct {
		Entries []struct {
			Action string `json:"action"`
		} `json:"entries"`
	}
	admin.must("GET", "/api/v1/admin/audit/search?action=core.template.rights", nil, &log, 200)
	if len(log.Entries) < 3 {
		t.Fatalf("audit entries of rights: %+v", log)
	}
}
