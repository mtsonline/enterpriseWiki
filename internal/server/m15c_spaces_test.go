package server_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestCategoryTemplate covers the website template per category: only
// active templates, inherited by subcategories, offered in the site info.
func TestCategoryTemplate(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var site struct {
		Template  string `json:"template"`
		Templates []struct {
			ID string `json:"id"`
		} `json:"templates"`
	}
	admin.must("GET", "/api/v1/site", nil, &site, 200)
	if len(site.Templates) < 2 {
		t.Fatalf("templates: %+v", site)
	}
	other := ""
	for _, x := range site.Templates {
		if x.ID != site.Template {
			other = x.ID
		}
	}
	type tcat struct {
		ID                int64  `json:"id"`
		Template          string `json:"template"`
		EffectiveTemplate string `json:"effective_template"`
	}
	var top, sub, deeper tcat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Public"}, "template": other}, &top, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Sub"}}, &sub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": sub.ID, "titles": map[string]string{"en": "Deeper"}}, &deeper, 201)
	if top.Template != other || top.EffectiveTemplate != other || sub.Template != "" || deeper.EffectiveTemplate != other {
		t.Fatalf("inheritance: %+v %+v %+v", top, sub, deeper)
	}
	for _, bad := range []string{"core.macros", "nope", "<script>"} {
		if code := admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"template": bad}, nil); code != 400 {
			t.Fatalf("template %q accepted: %d", bad, code)
		}
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"template": site.Template}, &sub, 200)
	var view struct {
		Category tcat `json:"category"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", deeper.ID), nil, &view, 200)
	if view.Category.EffectiveTemplate != site.Template {
		t.Fatalf("nearest template wins: %+v", view.Category)
	}
	// managers of a category may set it, readers may not
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rita", "password": "Passw0rd!Passw0rd", "display_name": "Rita", "email": "rita@example.com"}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", top.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	rita := e.client()
	rita.login("rita", "Passw0rd!Passw0rd")
	if code := rita.do("PUT", fmt.Sprintf("/api/v1/categories/%d", top.ID), map[string]any{"template": ""}, nil); code != 403 {
		t.Fatalf("reader changed the template: %d", code)
	}
	// removing it falls back to the site template
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", top.ID), map[string]any{"template": ""}, &top, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"template": ""}, nil, 200)
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", deeper.ID), nil, &view, 200)
	if top.EffectiveTemplate != "" || view.Category.EffectiveTemplate != "" {
		t.Fatalf("fallback: %+v %+v", top, view.Category)
	}
}

// TestSpaces covers the settings of spaces (top-level categories): key and
// /s/KEY, shortcuts, allowed element types, language, directory and rights.
func TestSpaces(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()
	var hr, sub, secret cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Human resources"}}, &hr, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", hr.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": hr.ID, "titles": map[string]string{"en": "Forms"}}, &sub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Board"}}, &secret, 201)
	type space struct {
		ID           int64    `json:"id"`
		Key          string   `json:"key"`
		OwnTree      bool     `json:"own_tree"`
		AllowedTypes []string `json:"allowed_types"`
		Language     string   `json:"language"`
		Shortcuts    []struct {
			URL string `json:"url"`
		} `json:"shortcuts"`
	}
	put := func(c *client, id int64, body map[string]any) int {
		return c.do("PUT", fmt.Sprintf("/api/v1/categories/%d/space", id), body, nil)
	}
	for name, body := range map[string]map[string]any{
		"short key":         {"key": "H"},
		"key with symbols":  {"key": "H-R"},
		"javascript link":   {"shortcuts": []map[string]any{{"label": map[string]string{"en": "x"}, "url": "javascript:alert(1)"}}},
		"protocol-relative": {"shortcuts": []map[string]any{{"label": map[string]string{"en": "x"}, "url": "//evil.example"}}},
		"no label":          {"shortcuts": []map[string]any{{"label": map[string]string{}, "url": "/search"}}},
		"unknown type":      {"allowed_types": []string{"nope"}},
		"unknown language":  {"language": "xx"},
	} {
		if code := put(admin, hr.ID, body); code != 400 {
			t.Fatalf("%s accepted: %d", name, code)
		}
	}
	if code := put(admin, sub.ID, map[string]any{"key": "SUB"}); code != 400 {
		t.Fatalf("subcategory became a space: %d", code)
	}
	var sp space
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d/space", hr.ID), map[string]any{"key": "hr", "own_tree": true, "allowed_types": []string{"core.html"}, "language": "en",
		"shortcuts": []map[string]any{{"label": map[string]string{"en": "Search"}, "url": "/search"}, {"label": map[string]string{"en": "Intranet"}, "url": "https://example.org/"}}}, &sp, 200)
	if sp.Key != "HR" || !sp.OwnTree || len(sp.Shortcuts) != 2 || sp.Language != "en" {
		t.Fatalf("space: %+v", sp)
	}
	if code := put(admin, secret.ID, map[string]any{"key": "HR"}); code != 409 {
		t.Fatalf("duplicate key: %d", code)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d/space", secret.ID), map[string]any{"key": "BOARD"}, nil, 200)
	// address and directory: only what the reader may see
	guest.must("GET", "/api/v1/spaces/hr", nil, &sp, 200)
	if sp.ID != hr.ID {
		t.Fatalf("by key: %+v", sp)
	}
	if code := guest.do("GET", "/api/v1/spaces/BOARD", nil, nil); code != 404 {
		t.Fatalf("guest found a hidden space: %d", code)
	}
	var dir []space
	guest.must("GET", "/api/v1/spaces", nil, &dir, 200)
	if len(dir) != 1 || dir[0].Key != "HR" {
		t.Fatalf("directory: %+v", dir)
	}
	// the space comes with every category of it
	var view struct {
		Space *space `json:"space"`
	}
	guest.must("GET", fmt.Sprintf("/api/v1/categories/%d", sub.ID), nil, &view, 200)
	if view.Space == nil || view.Space.ID != hr.ID || view.Space.Key != "HR" {
		t.Fatalf("space of a subcategory: %+v", view.Space)
	}
	// allowed element types: also when moving elements in
	if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "content.blog", "title": "Blog", "body": map[string]any{"category_id": sub.ID}}, nil); code != 400 {
		t.Fatalf("type not allowed in the space: %d", code)
	}
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "Leave form", "body": htmlBody("<p>x</p>")}, &page, 201)
	var blog elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "content.blog", "title": "Board blog", "body": map[string]any{"category_id": secret.ID}}, &blog, 201)
	if code := admin.do("POST", fmt.Sprintf("/api/v1/elements/%d/move", blog.ID), map[string]any{"category_id": sub.ID}, nil); code != 400 {
		t.Fatalf("moved a type that is not allowed: %d", code)
	}
	// a manager of the space (no admin rights) may change it, a reader may not
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "hanna", "password": "Passw0rd!Passw0rd", "display_name": "Hanna", "email": "hanna@example.com"}, &u, 201)
	hanna := e.client()
	hanna.login("hanna", "Passw0rd!Passw0rd")
	if code := put(hanna, hr.ID, map[string]any{"key": "HR", "own_tree": false}); code != 403 {
		t.Fatalf("reader changed the space: %d", code)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", hr.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 31, "inherit": true}, nil, 201)
	if code := put(hanna, hr.ID, map[string]any{"key": "HR", "own_tree": false}); code != 200 {
		t.Fatalf("space manager: %d", code)
	}
	if code := put(hanna, secret.ID, map[string]any{"key": "X1"}); code != 403 && code != 404 {
		t.Fatalf("manager of another space: %d", code)
	}
}

// TestArchive covers archiving: read-only for everybody (admins too), out of
// navigation and search, comments refused, restorable by managers.
func TestArchive(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var top, sub cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Old project"}}, &top, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", top.ID), map[string]any{"subject_type": "authenticated", "permissions": 3, "inherit": true}, nil, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Notes"}}, &sub, 201)
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "Retrospective zeppelin", "body": htmlBody("<p>zeppelin</p>")}, &page, 201)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "otto", "password": "Passw0rd!Passw0rd", "display_name": "Otto", "email": "otto@example.com"}, &u, 201)
	otto := e.client()
	otto.login("otto", "Passw0rd!Passw0rd")
	if code := otto.do("POST", fmt.Sprintf("/api/v1/categories/%d/archive", top.ID), map[string]any{"archived": true}, nil); code != 403 {
		t.Fatalf("editor archived: %d", code)
	}
	var c struct {
		Archived   bool    `json:"archived"`
		ArchivedAt *string `json:"archived_at"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/archive", top.ID), map[string]any{"archived": true}, &c, 200)
	if !c.Archived || c.ArchivedAt == nil {
		t.Fatalf("archive: %+v", c)
	}
	var view struct {
		Category struct {
			Archived    bool     `json:"archived"`
			Permissions []string `json:"permissions"`
		} `json:"category"`
	}
	otto.must("GET", fmt.Sprintf("/api/v1/categories/%d", sub.ID), nil, &view, 200)
	if !view.Category.Archived || strings.Contains(strings.Join(view.Category.Permissions, ","), "edit") {
		t.Fatalf("subcategory: %+v", view.Category)
	}
	// read-only for everybody, administrators included
	for name, c := range map[string]*client{"editor": otto, "admin": admin} {
		if code := c.do("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "x", "body": htmlBody("<p>x</p>")}, nil); code != 403 {
			t.Fatalf("%s edited an archived page: %d", name, code)
		}
		if code := c.do("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "New", "body": htmlBody("<p>x</p>")}, nil); code != 403 {
			t.Fatalf("%s created in the archive: %d", name, code)
		}
		if code := c.do("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", page.ID), map[string]any{"body": "hi"}, nil); code < 400 {
			t.Fatalf("%s commented in the archive: %d", name, code)
		}
	}
	if code := admin.do("POST", "/api/v1/categories", map[string]any{"parent_id": sub.ID, "titles": map[string]string{"en": "More"}}, nil); code != 400 {
		t.Fatalf("subcategory in the archive: %d", code)
	}
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"titles": map[string]string{"en": "Renamed"}}, nil); code != 400 {
		t.Fatalf("archived category renamed: %d", code)
	}
	// still readable, but out of navigation and search
	otto.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 200)
	var tops []struct {
		ID int64 `json:"id"`
	}
	otto.must("GET", "/api/v1/categories?parent=0", nil, &tops, 200)
	for _, x := range tops {
		if x.ID == top.ID {
			t.Fatal("archived category in the navigation")
		}
	}
	var hits struct {
		Elements []struct {
			ID int64 `json:"element_id"`
		} `json:"elements"`
	}
	otto.must("GET", "/api/v1/search?q=zeppelin", nil, &hits, 200)
	if len(hits.Elements) != 0 {
		t.Fatalf("archived page found: %+v", hits)
	}
	var dir []struct {
		ID       int64 `json:"id"`
		Archived bool  `json:"archived"`
	}
	otto.must("GET", "/api/v1/spaces", nil, &dir, 200)
	if len(dir) != 1 || !dir[0].Archived {
		t.Fatalf("directory: %+v", dir)
	}
	// restore
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/archive", top.ID), map[string]any{"archived": false}, &c, 200)
	otto.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Retrospective zeppelin", "body": htmlBody("<p>zeppelin again</p>")}, nil, 200)
	otto.must("GET", "/api/v1/search?q=zeppelin", nil, &hits, 200)
	if len(hits.Elements) != 1 {
		t.Fatalf("restored page not found: %+v", hits)
	}
}

// TestSpaceTemplates covers page templates of a space: saved by its
// managers, offered in the space only, hidden from other readers.
func TestSpaceTemplates(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var hr, sub, other cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "HR"}}, &hr, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": hr.ID, "titles": map[string]string{"en": "Forms"}}, &sub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Sales"}}, &other, 201)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "mia", "password": "Passw0rd!Passw0rd", "display_name": "Mia", "email": "mia@example.com"}, &u, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", hr.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 7, "inherit": true}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", other.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 7, "inherit": true}, nil, 201)
	mia := e.client()
	mia.login("mia", "Passw0rd!Passw0rd")
	tpl := map[string]any{"name": "Leave request", "type": "core.html", "lang": "en", "title": "Leave", "body": htmlBody("<p>From … to …</p>"), "space_id": hr.ID}
	if code := mia.do("POST", "/api/v1/templates", tpl, nil); code != 403 {
		t.Fatalf("editor saved a space template: %d", code)
	}
	tpl["space_id"] = sub.ID
	if code := admin.do("POST", "/api/v1/templates", tpl, nil); code != 400 {
		t.Fatalf("space template on a subcategory: %d", code)
	}
	tpl["space_id"] = hr.ID
	var saved struct {
		ID      int64 `json:"id"`
		SpaceID int64 `json:"space_id"`
	}
	admin.must("POST", "/api/v1/templates", tpl, &saved, 201)
	type tl []struct {
		ID int64 `json:"id"`
	}
	var list tl
	mia.must("GET", fmt.Sprintf("/api/v1/templates?type=core.html&category=%d", sub.ID), nil, &list, 200)
	if len(list) != 1 || list[0].ID != saved.ID {
		t.Fatalf("in the space: %+v", list)
	}
	mia.must("GET", fmt.Sprintf("/api/v1/templates?type=core.html&category=%d", other.ID), nil, &list, 200)
	if len(list) != 0 {
		t.Fatalf("in another space: %+v", list)
	}
	mia.must("GET", fmt.Sprintf("/api/v1/templates/%d", saved.ID), nil, nil, 200)
	if code := mia.do("DELETE", fmt.Sprintf("/api/v1/templates/%d", saved.ID), nil, nil); code != 403 {
		t.Fatalf("editor deleted a space template: %d", code)
	}
	// readers without access to the space do not see it
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "nils", "password": "Passw0rd!Passw0rd", "display_name": "Nils", "email": "nils@example.com"}, nil, 201)
	nils := e.client()
	nils.login("nils", "Passw0rd!Passw0rd")
	if code := nils.do("GET", fmt.Sprintf("/api/v1/templates/%d", saved.ID), nil, nil); code != 404 {
		t.Fatalf("outsider read a space template: %d", code)
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/templates/%d", saved.ID), nil, nil, 204)
}

// TestSpaceFromTemplate creates a space from a space template.
func TestSpaceFromTemplate(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var tpl, sub cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Project template"}}, &tpl, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": tpl.ID, "titles": map[string]string{"en": "Meetings"}}, &sub, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "Kick-off", "body": htmlBody("<p>Agenda</p>")}, nil, 201)
	admin.must("POST", "/api/v1/templates", map[string]any{"name": "Minutes", "type": "core.html", "lang": "en", "title": "Minutes", "body": htmlBody("<p>…</p>"), "space_id": tpl.ID}, nil, 201)
	newSpace := map[string]any{"template_id": tpl.ID, "titles": map[string]string{"en": "Project Apollo"}, "key": "APOLLO"}
	if code := admin.do("POST", "/api/v1/spaces", newSpace, nil); code != 404 {
		t.Fatalf("space that is no template used: %d", code)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d/space", tpl.ID), map[string]any{"key": "PTPL", "is_template": true, "own_tree": true, "allowed_types": []string{"core.html"}}, nil, 200)
	var sp struct {
		ID           int64             `json:"id"`
		Key          string            `json:"key"`
		Titles       map[string]string `json:"titles"`
		OwnTree      bool              `json:"own_tree"`
		IsTemplate   bool              `json:"is_template"`
		AllowedTypes []string          `json:"allowed_types"`
	}
	admin.must("POST", "/api/v1/spaces", newSpace, &sp, 201)
	if sp.Key != "APOLLO" || sp.Titles["en"] != "Project Apollo" || !sp.OwnTree || sp.IsTemplate || len(sp.AllowedTypes) != 1 {
		t.Fatalf("new space: %+v", sp)
	}
	var subs []struct {
		ID           int64 `json:"id"`
		ElementCount int   `json:"element_count"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories?parent=%d", sp.ID), nil, &subs, 200)
	if len(subs) != 1 || subs[0].ElementCount != 0 {
		t.Fatalf("structure without pages: %+v", subs)
	}
	var tl []struct {
		Name string `json:"name"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/templates?category=%d", subs[0].ID), nil, &tl, 200)
	if len(tl) != 1 || tl[0].Name != "Minutes" {
		t.Fatalf("page templates of the new space: %+v", tl)
	}
	newSpace["key"], newSpace["with_pages"] = "APOLLO2", true
	admin.must("POST", "/api/v1/spaces", newSpace, &sp, 201)
	admin.must("GET", fmt.Sprintf("/api/v1/categories?parent=%d", sp.ID), nil, &subs, 200)
	if len(subs) != 1 || subs[0].ElementCount != 1 {
		t.Fatalf("with pages: %+v", subs)
	}
	newSpace["key"] = "APOLLO"
	if code := admin.do("POST", "/api/v1/spaces", newSpace, nil); code != 409 {
		t.Fatalf("duplicate key: %d", code)
	}
}

// rawBody sends a body as is (e.g. JSON Lines).
func (c *client) rawBody(method, path, body string) (int, string) {
	c.e.t.Helper()
	req, _ := http.NewRequest(method, c.e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-ndjson")
	req.Header.Set("X-Requested-With", "ewiki")
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(data)
}

// TestSpaceExportImport exports a space as JSON Lines and imports it as a
// new space and below a category; bodies are cleaned like in the editor.
func TestSpaceExportImport(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var top, sub cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Handbook", "de": "Handbuch"}}, &top, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Travel"}}, &sub, 201)
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "lang": "en", "title": "Expenses", "body": htmlBody("<p>Receipts please</p>"), "tags": []string{"finance"}}, &page, 201)
	zero := 0
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "de", "title": "Spesen", "body": htmlBody("<p>Belege bitte</p>"), "base_rev": zero}, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d/space", top.ID), map[string]any{"key": "HB", "own_tree": true}, nil, 200)
	admin.must("POST", "/api/v1/templates", map[string]any{"name": "Trip report", "type": "core.html", "lang": "en", "title": "Trip", "body": htmlBody("<p>Where</p>"), "space_id": top.ID}, nil, 201)

	// readers may not export
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "eva", "password": "Passw0rd!Passw0rd", "display_name": "Eva", "email": "eva@example.com"}, &u, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", top.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 7, "inherit": true}, nil, 201)
	eva := e.client()
	eva.login("eva", "Passw0rd!Passw0rd")
	if code, _ := eva.rawBody("GET", fmt.Sprintf("/api/v1/categories/%d/export", top.ID), ""); code != 403 {
		t.Fatalf("editor exported: %d", code)
	}
	code, pkg := admin.rawBody("GET", fmt.Sprintf("/api/v1/categories/%d/export", top.ID), "")
	if code != 200 || strings.Count(pkg, "\n") != 5 || !strings.Contains(pkg, `"key":"HB"`) || !strings.Contains(pkg, "Belege bitte") || !strings.Contains(pkg, "Trip report") {
		t.Fatalf("export %d: %s", code, pkg)
	}
	// import as a new space: the key is taken, so it comes without key
	var res struct {
		CategoryID int64    `json:"category_id"`
		Categories int      `json:"categories"`
		Elements   int      `json:"elements"`
		Templates  int      `json:"templates"`
		Skipped    []string `json:"skipped"`
	}
	if code, body := eva.rawBody("POST", "/api/v1/categories/import", pkg); code != 403 {
		t.Fatalf("editor imported a space: %d %s", code, body)
	}
	code, body := admin.rawBody("POST", "/api/v1/categories/import", pkg)
	if code != 201 || json.Unmarshal([]byte(body), &res) != nil || res.Categories != 2 || res.Elements != 1 || res.Templates != 1 || len(res.Skipped) != 1 {
		t.Fatalf("import %d: %s", code, body)
	}
	var subs []struct {
		ID int64 `json:"id"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories?parent=%d", res.CategoryID), nil, &subs, 200)
	var cv struct {
		Elements []struct {
			ID int64 `json:"id"`
		} `json:"elements"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", subs[0].ID), nil, &cv, 200)
	els := cv.Elements
	var v struct {
		Title string `json:"title"`
		HTML  string `json:"html"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=de", els[0].ID), nil, &v, 200)
	if v.Title != "Spesen" || !strings.Contains(v.HTML, "Belege") {
		t.Fatalf("translation imported: %+v", v)
	}
	// an editor may import below a category where she may manage; the body is cleaned
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", sub.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 31, "inherit": true}, nil, 201)
	evil := `{"format":"ewiki-space","version":1}
{"kind":"category","ref":1,"titles":{"en":"Imported"}}
{"kind":"element","category":1,"type":"core.html","revisions":[{"lang":"en","title":"X","body":{"format":"html","source":"<p>ok</p><script>alert(1)</script><img src=x onerror=alert(2)>"}}]}
{"kind":"element","category":1,"type":"nope","revisions":[{"lang":"en","title":"Y","body":{}}]}
{"kind":"element","category":99,"type":"core.html","revisions":[{"lang":"en","title":"Z","body":{}}]}
not json
`
	code, body = eva.rawBody("POST", fmt.Sprintf("/api/v1/categories/import?parent=%d", sub.ID), evil)
	if code != 201 || json.Unmarshal([]byte(body), &res) != nil || res.Elements != 1 || len(res.Skipped) != 3 {
		t.Fatalf("import below: %d %s", code, body)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", res.CategoryID), nil, &cv, 200)
	els = cv.Elements
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", els[0].ID), nil, &v, 200)
	if strings.Contains(v.HTML, "<script") || strings.Contains(v.HTML, "onerror") {
		t.Fatalf("imported body not cleaned: %s", v.HTML)
	}
	if code, _ := admin.rawBody("POST", "/api/v1/categories/import", `{"format":"other"}`); code != 400 {
		t.Fatalf("foreign format: %d", code)
	}
}
