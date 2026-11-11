package server_test

import (
	"archive/zip"
	"bytes"
	"context"
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

// TestSpaceMoves: categories become spaces and spaces become subcategories.
func TestSpaceMoves(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var a, b, sub cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "A"}}, &a, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "B"}}, &b, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": a.ID, "titles": map[string]string{"en": "Sub"}}, &sub, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "content.blog", "title": "Blog", "body": map[string]any{"category_id": sub.ID}}, nil, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d/space", b.ID), map[string]any{"key": "BEE", "allowed_types": []string{"core.html"}, "own_tree": true}, nil, 200)
	// the blog is not allowed in B
	if code := admin.do("POST", fmt.Sprintf("/api/v1/categories/%d/move", sub.ID), map[string]any{"parent_id": b.ID}, nil); code != 400 {
		t.Fatalf("moved a blog into a space without blogs: %d", code)
	}
	if code := admin.do("POST", fmt.Sprintf("/api/v1/categories/%d/duplicate", sub.ID), map[string]any{"parent_id": b.ID}, nil); code != 400 {
		t.Fatalf("duplicated a blog into a space without blogs: %d", code)
	}
	// a subcategory becomes a space
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/move", sub.ID), map[string]any{"parent_id": 0}, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d/space", sub.ID), map[string]any{"key": "SUB"}, nil, 200)
	admin.must("GET", "/api/v1/spaces/SUB", nil, nil, 200)
	// a space becomes a subcategory: its key is free again, its settings rest
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/move", b.ID), map[string]any{"parent_id": a.ID}, nil, 200)
	if code := admin.do("GET", "/api/v1/spaces/BEE", nil, nil); code != 404 {
		t.Fatalf("key of a former space still works: %d", code)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d/space", a.ID), map[string]any{"key": "BEE"}, nil, 200)
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d/space", b.ID), map[string]any{"key": "X2"}, nil); code != 400 {
		t.Fatalf("space settings on a subcategory: %d", code)
	}
	var view struct {
		Space *struct {
			ID int64 `json:"id"`
		} `json:"space"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", b.ID), nil, &view, 200)
	if view.Space == nil || view.Space.ID != a.ID {
		t.Fatalf("space of the moved category: %+v", view.Space)
	}
	// back to the top: the settings apply again (without the old key)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/move", b.ID), map[string]any{"parent_id": 0}, nil, 200)
	var sp struct {
		Key          string   `json:"key"`
		OwnTree      bool     `json:"own_tree"`
		AllowedTypes []string `json:"allowed_types"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", b.ID), nil, &struct {
		Space *struct{} `json:"space"`
	}{}, 200)
	var dir []struct {
		ID           int64    `json:"id"`
		Key          string   `json:"key"`
		OwnTree      bool     `json:"own_tree"`
		AllowedTypes []string `json:"allowed_types"`
	}
	admin.must("GET", "/api/v1/spaces", nil, &dir, 200)
	for _, d := range dir {
		if d.ID == b.ID {
			sp.Key, sp.OwnTree, sp.AllowedTypes = d.Key, d.OwnTree, d.AllowedTypes
		}
	}
	if sp.Key != "" || !sp.OwnTree || len(sp.AllowedTypes) != 1 {
		t.Fatalf("settings back at the top: %+v", sp)
	}
}

// TestAccessRequests: asking for access never reveals whether the target
// exists; managers are notified and grant a normal permission rule.
func TestAccessRequests(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var space, sub cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Finance"}}, &space, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": space.ID, "titles": map[string]string{"en": "Budgets"}}, &sub, 201)
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "Budget 2027", "body": htmlBody("<p>x</p>")}, &page, 201)
	var mgr, req struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "fred", "password": "Passw0rd!Passw0rd", "display_name": "Fred", "email": "fred@example.com"}, &mgr, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rosa", "password": "Passw0rd!Passw0rd", "display_name": "Rosa", "email": "rosa@example.com"}, &req, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", space.ID), map[string]any{"subject_type": "user", "subject_id": mgr.ID, "preset": "manager", "inherit": true}, nil, 201)
	fred, rosa, guest := e.client(), e.client(), e.client()
	fred.login("fred", "Passw0rd!Passw0rd")
	rosa.login("rosa", "Passw0rd!Passw0rd")
	if code := guest.do("POST", "/api/v1/access-requests", map[string]any{"type": "element", "id": page.ID}, nil); code != 401 {
		t.Fatalf("guest asked: %d", code)
	}
	if code := rosa.do("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil); code != 404 {
		t.Fatalf("hidden page: %d", code)
	}
	// existing and missing targets answer the same
	var a, b map[string]any
	rosa.must("POST", "/api/v1/access-requests", map[string]any{"type": "element", "id": page.ID, "message": "I prepare the budget"}, &a, 200)
	rosa.must("POST", "/api/v1/access-requests", map[string]any{"type": "element", "id": 999999}, &b, 200)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("answers differ: %v %v", a, b)
	}
	rosa.must("POST", "/api/v1/access-requests", map[string]any{"type": "element", "id": page.ID}, nil, 200) // no second open request
	var open []struct {
		ID      int64  `json:"id"`
		User    string `json:"user"`
		Message string `json:"message"`
	}
	if code := rosa.do("GET", fmt.Sprintf("/api/v1/categories/%d/access-requests", sub.ID), nil, nil); code != 403 && code != 404 {
		t.Fatalf("requester listed requests: %d", code)
	}
	fred.must("GET", fmt.Sprintf("/api/v1/categories/%d/access-requests", sub.ID), nil, &open, 200)
	if len(open) != 1 || open[0].User != "Rosa" || open[0].Message != "I prepare the budget" {
		t.Fatalf("open requests: %+v", open)
	}
	var inbox struct {
		Items []struct {
			Kind string `json:"kind"`
		} `json:"items"`
	}
	fred.must("GET", "/api/v1/me/inbox", nil, &inbox, 200)
	found := false
	for _, n := range inbox.Items {
		found = found || n.Kind == "access_request"
	}
	if !found {
		t.Fatalf("manager not notified: %+v", inbox)
	}
	if code := rosa.do("POST", fmt.Sprintf("/api/v1/access-requests/%d", open[0].ID), map[string]any{"grant": true}, nil); code != 403 && code != 404 {
		t.Fatalf("requester granted herself: %d", code)
	}
	fred.must("POST", fmt.Sprintf("/api/v1/access-requests/%d", open[0].ID), map[string]any{"grant": true, "preset": "reader"}, nil, 204)
	rosa.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 200)
	if code := fred.do("POST", fmt.Sprintf("/api/v1/access-requests/%d", open[0].ID), map[string]any{"grant": false}, nil); code != 409 {
		t.Fatalf("answered twice: %d", code)
	}
	// the permission is a normal rule (also in the permission management)
	var acl struct {
		Own []struct {
			SubjectType string `json:"subject_type"`
			SubjectID   *int64 `json:"subject_id"`
			Permissions int    `json:"permissions"`
		} `json:"own"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d/acl", sub.ID), nil, &acl, 200)
	if len(acl.Own) != 1 || acl.Own[0].SubjectID == nil || *acl.Own[0].SubjectID != req.ID || acl.Own[0].Permissions&2 != 0 {
		t.Fatalf("rule: %+v", acl.Own)
	}
	// rate limit
	for i := 0; i < 12; i++ {
		rosa.do("POST", "/api/v1/access-requests", map[string]any{"type": "category", "id": 900000 + i}, nil)
	}
	if code := rosa.do("POST", "/api/v1/access-requests", map[string]any{"type": "category", "id": 1}, nil); code != 429 {
		t.Fatalf("no rate limit: %d", code)
	}
}

// TestPersonalSpaces and "my spaces".
func TestPersonalSpaces(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	pw := "Passw0rd!Passw0rd"
	var role struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Staff", "permissions": []string{"core.spaces.personal"}}, &role, 200)
	var lena, max struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "lena", "password": pw, "display_name": "Lena", "roles": []int64{role.ID}}, &lena, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "max", "password": pw, "display_name": "Max", "roles": []int64{}}, &max, 201)
	l, m := e.client(), e.client()
	l.login("lena", pw)
	m.login("max", pw)
	if code := m.do("POST", "/api/v1/spaces/personal", nil, nil); code != 403 {
		t.Fatalf("personal space without permission: %d", code)
	}
	type sp struct {
		ID        int64             `json:"id"`
		Titles    map[string]string `json:"titles"`
		Personal  int64             `json:"personal"`
		Member    bool              `json:"member"`
		CanManage bool              `json:"can_manage"`
		OwnTree   bool              `json:"own_tree"`
	}
	var a, b sp
	l.must("POST", "/api/v1/spaces/personal", nil, &a, 200)
	l.must("POST", "/api/v1/spaces/personal", nil, &b, 200)
	if a.ID != b.ID || a.Personal != lena.ID || a.Titles["en"] != "Lena" || !a.CanManage || !a.OwnTree || !a.Member {
		t.Fatalf("personal space: %+v %+v", a, b)
	}
	// only its owner sees it; the owner cannot hand it over by changing settings
	if code := m.do("GET", fmt.Sprintf("/api/v1/categories/%d", a.ID), nil, nil); code != 404 && code != 403 {
		t.Fatalf("other user sees the personal space: %d", code)
	}
	l.must("PUT", fmt.Sprintf("/api/v1/categories/%d/space", a.ID), map[string]any{"key": "LENA", "personal": max.ID}, &b, 200)
	if b.Personal != lena.ID {
		t.Fatalf("owner changed: %+v", b)
	}
	// the owner may create content in it but no further top-level categories
	l.must("POST", "/api/v1/categories", map[string]any{"parent_id": a.ID, "titles": map[string]string{"en": "Notes"}}, nil, 201)
	if code := l.do("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Another top"}}, nil); code != 403 {
		t.Fatalf("top-level category without permission: %d", code)
	}
	// my spaces: member only where own rules exist
	var pub cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Public"}}, &pub, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", pub.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	var dir []sp
	l.must("GET", "/api/v1/spaces", nil, &dir, 200)
	members := map[int64]bool{}
	for _, d := range dir {
		members[d.ID] = d.Member
	}
	if len(dir) != 2 || !members[a.ID] || members[pub.ID] {
		t.Fatalf("directory of lena: %+v", dir)
	}
}

// TestClassification: internal hides from guests, confidential keeps
// content out of feeds, strictly confidential out of search and exports.
func TestClassification(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()
	var top, sub cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Legal"}}, &top, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", top.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Contracts"}, "feed": "recursive"}, &sub, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"feed": "recursive"}, nil, 200)
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "Contract with platypus", "body": htmlBody("<p>platypus terms</p>")}, &page, 201)
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 200)
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d", top.ID), map[string]any{"classification": 7}, nil); code != 400 {
		t.Fatalf("invalid level: %d", code)
	}
	// internal on the space: guests are out despite the guest rule
	var c struct {
		Classification          *int `json:"classification"`
		EffectiveClassification int  `json:"effective_classification"`
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", top.ID), map[string]any{"classification": 1}, &c, 200)
	if c.Classification == nil || *c.Classification != 1 || c.EffectiveClassification != 1 {
		t.Fatalf("classification: %+v", c)
	}
	if code := guest.do("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil); code != 404 && code != 401 {
		t.Fatalf("guest reads internal content: %d", code)
	}
	// a lower level below has no effect (the strictest above applies)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"classification": 0}, &c, 200)
	if c.EffectiveClassification != 1 {
		t.Fatalf("lowered below: %+v", c)
	}
	// confidential: not in feeds
	_, body := admin.raw(fmt.Sprintf("/api/v1/feeds/categories/%d", sub.ID), nil)
	if !strings.Contains(body, "platypus") {
		t.Fatalf("feed before: %s", body)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"classification": 2}, nil, 200)
	_, body = admin.raw(fmt.Sprintf("/api/v1/feeds/categories/%d", sub.ID), nil)
	if strings.Contains(body, "platypus") {
		t.Fatalf("confidential content in a feed: %s", body)
	}
	var hits struct {
		Elements []struct {
			ID int64 `json:"element_id"`
		} `json:"elements"`
	}
	admin.must("GET", "/api/v1/search?q=platypus", nil, &hits, 200)
	if len(hits.Elements) != 1 {
		t.Fatalf("confidential not found: %+v", hits)
	}
	// strictly confidential: not in the general search, no export or copy
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"classification": 3}, nil, 200)
	admin.must("GET", "/api/v1/search?q=platypus", nil, &hits, 200)
	if len(hits.Elements) != 0 {
		t.Fatalf("strictly confidential in the search: %+v", hits)
	}
	if code, _ := admin.rawBody("GET", fmt.Sprintf("/api/v1/categories/%d/export", sub.ID), ""); code != 403 {
		t.Fatalf("export: %d", code)
	}
	if code := admin.do("POST", fmt.Sprintf("/api/v1/categories/%d/duplicate", sub.ID), map[string]any{"parent_id": top.ID}, nil); code != 403 {
		t.Fatalf("copy: %d", code)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 200) // still readable for the authorized
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"classification": -1}, &c, 200)
	if c.Classification != nil || c.EffectiveClassification != 1 {
		t.Fatalf("back to as above: %+v", c)
	}
}

// TestInsights: views per page and day, popular and outdated pages, contributors.
func TestInsights(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()
	var top cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Wiki help"}}, &top, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", top.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	var a, b, hidden elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": top.ID, "type": "core.html", "title": "Popular", "body": htmlBody("<p>x</p>")}, &a, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": top.ID, "type": "core.html", "title": "Quiet", "body": htmlBody("<p>x</p>")}, &b, 201)
	var secret cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Secret"}}, &secret, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "core.html", "title": "Hidden", "body": htmlBody("<p>x</p>")}, &hidden, 201)
	for i := 0; i < 3; i++ {
		guest.must("POST", fmt.Sprintf("/api/v1/elements/%d/view", a.ID), nil, nil, 204)
	}
	guest.must("POST", fmt.Sprintf("/api/v1/elements/%d/view", b.ID), nil, nil, 204)
	if code := guest.do("POST", fmt.Sprintf("/api/v1/elements/%d/view", hidden.ID), nil, nil); code < 400 {
		t.Fatalf("view of a hidden page counted: %d", code)
	}
	if err := e.app.Content.FlushViews(context.Background()); err != nil {
		t.Fatal(err)
	}
	if code := guest.do("GET", fmt.Sprintf("/api/v1/categories/%d/insights", top.ID), nil, nil); code != 401 && code != 403 {
		t.Fatalf("reader got insights: %d", code)
	}
	var in struct {
		Pages       int                   `json:"pages"`
		NewPages    int                   `json:"new_pages"`
		Views       int                   `json:"views"`
		ViewsPerDay []struct{ Views int } `json:"views_per_day"`
		Popular     []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Views int    `json:"views"`
		} `json:"popular"`
		Contributors []struct {
			Name     string `json:"name"`
			Versions int    `json:"versions"`
		} `json:"contributors"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d/insights?days=30&lang=en", top.ID), nil, &in, 200)
	if in.Pages != 2 || in.NewPages != 2 || in.Views != 4 || len(in.ViewsPerDay) != 1 || len(in.Popular) != 2 || in.Popular[0].Title != "Popular" || in.Popular[0].Views != 3 ||
		len(in.Contributors) != 1 || in.Contributors[0].Versions != 2 {
		t.Fatalf("insights: %+v", in)
	}
}

// TestConfidentialMedia: pictures of confidential categories carry the
// viewer's name; strictly confidential ones cannot be downloaded.
func TestConfidentialMedia(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var folder cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "HR files"}}, &folder, 201)
	var bob struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd!x", "display_name": "Bob Baum"}, &bob, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", folder.ID), map[string]any{"subject_type": "user", "subject_id": bob.ID, "permissions": 1, "inherit": true}, nil, 201)
	bc := e.client()
	bc.login("bob", "Bob-Passw0rd!x")
	png := testPNG(200, 120)
	_, img := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": folder.ID, "title": "Org chart", "alt": "Org chart"}, "org.png", png)
	_, plain, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", img.ID))
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", folder.ID), map[string]any{"classification": 2}, nil, 200)
	c, marked, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", img.ID))
	if c != 200 || bytes.Equal(plain, marked) {
		t.Fatalf("confidential picture without watermark: %d", c)
	}
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", img.ID)); c != 200 {
		t.Fatalf("confidential download: %d", c)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", folder.ID), map[string]any{"classification": 3}, nil, 200)
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", img.ID)); c != 403 {
		t.Fatalf("strictly confidential download: %d", c)
	}
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", img.ID)); c != 200 {
		t.Fatalf("strictly confidential view: %d", c)
	}
	if c, _, _ := admin.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", img.ID)); c != 200 {
		t.Fatalf("manager download: %d", c)
	}
}

// TestSpaceLook: accent color (contrast checked), logo and allowed macros.
func TestSpaceLook(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var top cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Brand"}}, &top, 201)
	put := func(body map[string]any) int {
		return admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d/space", top.ID), body, nil)
	}
	for name, body := range map[string]map[string]any{
		"too light":     {"accent": "#ffcc00"},
		"no color":      {"accent": "red"},
		"external logo": {"logo": "https://tracker.example/logo.png"},
		"logo escape":   {"logo": "/api/v1/files/../../etc"},
		"unknown macro": {"allowed_macros": []string{"nope"}},
	} {
		if code := put(body); code != 400 {
			t.Fatalf("%s accepted: %d", name, code)
		}
	}
	var sp struct {
		Accent        string   `json:"accent"`
		Logo          string   `json:"logo"`
		AllowedMacros []string `json:"allowed_macros"`
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d/space", top.ID), map[string]any{"accent": "#0B5CAD", "logo": "/api/v1/files/abc123", "allowed_macros": []string{"toc"}}, &sp, 200)
	if sp.Accent != "#0b5cad" || sp.Logo != "/api/v1/files/abc123" || len(sp.AllowedMacros) != 1 {
		t.Fatalf("space look: %+v", sp)
	}
	// macros that are not allowed are not rendered in the space
	page := macroPage(t, admin, top.ID, "recent", map[string]string{})
	if html := viewHTML(t, admin, page); !strings.Contains(html, "not available in this space") {
		t.Fatalf("macro not allowed but rendered: %s", html)
	}
	toc := macroPage(t, admin, top.ID, "toc", map[string]string{})
	if html := viewHTML(t, admin, toc); strings.Contains(html, "not available") {
		t.Fatalf("allowed macro refused: %s", html)
	}
}

// TestConfidentialPictures: pictures embedded in pages of confidential
// categories carry the viewer's name; elsewhere they stay unchanged.
func TestConfidentialPictures(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var secret, open cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Board"}}, &secret, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Open"}}, &open, 201)
	var bob struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd!x", "display_name": "Bob Baum"}, &bob, 201)
	for _, c := range []int64{secret.ID, open.ID} {
		admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c), map[string]any{"subject_type": "user", "subject_id": bob.ID, "permissions": 1, "inherit": true}, nil, 201)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", secret.ID), map[string]any{"classification": 2}, nil, 200)
	bc := e.client()
	bc.login("bob", "Bob-Passw0rd!x")
	_, a := admin.upload(t, "a.png", testPNG(160, 100))
	_, b := admin.upload(t, "b.png", testPNG(161, 100))
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "core.html", "title": "Minutes", "body": htmlBody(`<p><img src="/api/v1/files/` + a.Hash + `" alt="chart"></p>`)}, nil, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": open.ID, "type": "core.html", "title": "News", "body": htmlBody(`<p><img src="/api/v1/files/` + b.Hash + `" alt="chart"></p>`)}, nil, 201)
	_, origA, _ := admin.fetch(t, "/api/v1/files/"+a.Hash) // own upload: unchanged
	c, markedA, hdr := bc.fetch(t, "/api/v1/files/"+a.Hash)
	if c != 200 || bytes.Equal(origA, markedA) || hdr.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("confidential picture: %d %v", c, hdr)
	}
	_, origB, _ := admin.fetch(t, "/api/v1/files/"+b.Hash)
	if c, plainB, _ := bc.fetch(t, "/api/v1/files/"+b.Hash); c != 200 || !bytes.Equal(origB, plainB) {
		t.Fatalf("open picture changed: %d", c)
	}
	// file elements of strictly confidential categories: no downloads for readers
	var att elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "core.attachment", "title": "Plan",
		"body": map[string]any{"file": map[string]string{"hash": a.Hash, "name": "plan.png"}}}, &att, 201)
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/x/core.attachment/elements/%d/file", att.ID)); c != 200 {
		t.Fatalf("confidential attachment: %d", c)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", secret.ID), map[string]any{"classification": 3}, nil, 200)
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/x/core.attachment/elements/%d/file", att.ID)); c != 403 {
		t.Fatalf("strictly confidential attachment: %d", c)
	}
	if c, _, _ := admin.fetch(t, fmt.Sprintf("/api/v1/x/core.attachment/elements/%d/file", att.ID)); c != 200 {
		t.Fatalf("manager download: %d", c)
	}
}

// TestPropertyDownloads: CSV and Excel files of a query or report;
// strictly confidential rows are never exported.
func TestPropertyDownloads(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var c, strict cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Budgets"}}, &c, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": c.ID, "titles": map[string]string{"en": "Board"}}, &strict, 201)
	admin.must("POST", "/api/v1/x/core.properties/defs", map[string]any{"category_id": c.ID, "key": "budget", "type": "currency", "labels": map[string]string{"en": "Budget"}, "options": map[string]any{"currency": "EUR"}}, nil, 201)
	fields := func(v any) map[string]any {
		return map[string]any{"core.properties": map[string]any{"values": map[string]any{"budget": v}}}
	}
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "=cmd|' /C calc'!A0", "body": htmlBody("<p>a</p>"), "fields": fields(1250.5)}, nil, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": strict.ID, "type": "core.html", "title": "Merger", "body": htmlBody("<p>b</p>"), "fields": fields(9e6)}, nil, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", strict.ID), map[string]any{"classification": 3}, nil, 200)
	q := `{"sources":[{"kind":"category","id":` + fmt.Sprint(c.ID) + `,"sub":true}],"columns":["budget"]}`
	code, csvBody := admin.rawBody("POST", "/api/v1/x/core.properties/query.csv", q)
	if code != 200 || !strings.Contains(csvBody, "'=cmd") || strings.Contains(csvBody, "Merger") || !strings.Contains(csvBody, "1250") {
		t.Fatalf("csv %d: %s", code, csvBody)
	}
	code, x := admin.rawBody("POST", "/api/v1/x/core.properties/query.xlsx", q)
	if code != 200 || !strings.HasPrefix(x, "PK") {
		t.Fatalf("xlsx %d", code)
	}
	z, err := zip.NewReader(strings.NewReader(x), int64(len(x)))
	if err != nil {
		t.Fatal(err)
	}
	var sheet string
	for _, f := range z.File {
		if f.Name == "xl/worksheets/sheet1.xml" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			sheet = string(b)
		}
	}
	if !strings.Contains(sheet, "<v>1250.5</v>") || strings.Contains(sheet, "Merger") || strings.Contains(sheet, "<f>") {
		t.Fatalf("sheet: %s", sheet)
	}
	// a report body; reports without downloads refuse
	rep := `{"params":{"source":"category:` + fmt.Sprint(c.ID) + `+sub","columns":"budget"},"category_id":` + fmt.Sprint(c.ID) + `}`
	if code, _ := admin.rawBody("POST", "/api/v1/x/core.properties/query.xlsx", rep); code != 200 {
		t.Fatalf("report xlsx: %d", code)
	}
	no := `{"params":{"source":"category:` + fmt.Sprint(c.ID) + `","export":"false"}}`
	if code, _ := admin.rawBody("POST", "/api/v1/x/core.properties/query.csv", no); code != 403 {
		t.Fatalf("report without downloads: %d", code)
	}
}

// TestSavedViews: personal and shared views of a property database.
func TestSavedViews(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Projects"}}, &c, 201)
	var db elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.propdb", "title": "All projects", "body": map[string]any{"source": fmt.Sprintf("category:%d", c.ID)}}, &db, 201)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "vera", "password": "Passw0rd!Passw0rd", "display_name": "Vera"}, &u, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 1, "inherit": true}, nil, 201)
	vera := e.client()
	vera.login("vera", "Passw0rd!Passw0rd")
	base := fmt.Sprintf("/api/v1/x/core.properties/databases/%d/views", db.ID)
	state := map[string]any{"search": "alpha", "view": "board", "group": "status", "filters": []map[string]any{{"key": "status", "op": "eq", "value": "Open"}}}
	if code := vera.do("POST", base, map[string]any{"name": "For all", "shared": true, "state": state}, nil); code != 403 {
		t.Fatalf("reader shared a view: %d", code)
	}
	var mine struct {
		ID int64 `json:"id"`
	}
	vera.must("POST", base, map[string]any{"name": "My board", "state": state}, &mine, 201)
	admin.must("POST", base, map[string]any{"name": "Team view", "shared": true, "state": state}, nil, 201)
	type sv struct {
		Name   string `json:"name"`
		Shared bool   `json:"shared"`
		Mine   bool   `json:"mine"`
		State  struct {
			View    string `json:"view"`
			Filters []any  `json:"filters"`
		} `json:"state"`
	}
	var list []sv
	vera.must("GET", base, nil, &list, 200)
	if len(list) != 2 || list[0].Name != "Team view" || !list[0].Shared || !list[1].Mine || list[1].State.View != "board" || len(list[1].State.Filters) != 1 {
		t.Fatalf("views of vera: %+v", list)
	}
	admin.must("GET", base, nil, &list, 200)
	if len(list) != 1 {
		t.Fatalf("admin sees vera's personal view: %+v", list)
	}
	if code := admin.do("DELETE", fmt.Sprintf("/api/v1/x/core.properties/views/%d", mine.ID), nil, nil); code != 404 {
		t.Fatalf("other person deleted a personal view: %d", code)
	}
	vera.must("DELETE", fmt.Sprintf("/api/v1/x/core.properties/views/%d", mine.ID), nil, nil, 204)
	guest := e.client()
	if code := guest.do("GET", base, nil, nil); code != 404 && code != 401 {
		t.Fatalf("guest: %d", code)
	}
}

// TestPropertyTrend: development of a status and a number per week.
func TestPropertyTrend(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Tasks"}}, &c, 201)
	choices := []map[string]any{{"value": "Open"}, {"value": "Done"}}
	admin.must("POST", "/api/v1/x/core.properties/defs", map[string]any{"category_id": c.ID, "key": "state", "type": "status", "options": map[string]any{"choices": choices}}, nil, 201)
	admin.must("POST", "/api/v1/x/core.properties/defs", map[string]any{"category_id": c.ID, "key": "hours", "type": "integer"}, nil, 201)
	var role struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Payroll"}, &role, 200)
	admin.must("POST", "/api/v1/x/core.properties/defs", map[string]any{"category_id": c.ID, "key": "salary", "type": "integer", "view_subjects": []map[string]any{{"type": "role", "id": role.ID}}}, nil, 201)
	fields := func(v map[string]any) map[string]any {
		return map[string]any{"core.properties": map[string]any{"values": v}}
	}
	var a, b elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "A", "body": htmlBody("<p>a</p>"), "fields": fields(map[string]any{"state": "Open", "hours": 3, "salary": 100})}, &a, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "B", "body": htmlBody("<p>b</p>"), "fields": fields(map[string]any{"state": "Open", "hours": 5})}, &b, 201)
	// A was done two weeks ago (history moved back), B still open
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE plugin_prop_history SET changed_at = now() - interval '21 days' WHERE element_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", a.ID), map[string]any{"lang": "en", "title": "A", "body": htmlBody("<p>a</p>"), "fields": fields(map[string]any{"state": "Done", "hours": 3})}, nil, 200)
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE plugin_prop_history SET changed_at = now() - interval '14 days' WHERE element_id=$1 AND changed_at > now() - interval '1 hour' AND key='state'`, a.ID); err != nil {
		t.Fatal(err)
	}
	rep := map[string]any{"params": map[string]any{"source": fmt.Sprintf("category:%d", c.ID)}, "category_id": c.ID}
	var tr struct {
		Weeks  []string `json:"weeks"`
		Series []struct {
			Value  string    `json:"value"`
			Values []float64 `json:"values"`
		} `json:"series"`
	}
	admin.must("POST", "/api/v1/x/core.properties/trend", map[string]any{"report": rep, "key": "state", "weeks": 4}, &tr, 200)
	got := map[string][]float64{}
	for _, s := range tr.Series {
		got[s.Value] = s.Values
	}
	// weeks: -3w (A open), -2w (A done), -1w, now; B only exists now
	if len(tr.Weeks) != 4 || got["Open"][0] != 1 || got["Done"][1] != 1 || got["Done"][3] != 1 || got["Open"][3] != 1 {
		t.Fatalf("trend: %+v", tr)
	}
	admin.must("POST", "/api/v1/x/core.properties/trend", map[string]any{"report": rep, "key": "hours", "weeks": 2}, &tr, 200)
	if len(tr.Series) != 1 || tr.Series[0].Values[1] != 8 {
		t.Fatalf("sum trend: %+v", tr)
	}
	// readers without the role do not get the history of a hidden property
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "tom", "password": "Passw0rd!Passw0rd", "display_name": "Tom"}, &u, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 1, "inherit": true}, nil, 201)
	tom := e.client()
	tom.login("tom", "Passw0rd!Passw0rd")
	if code := tom.do("POST", "/api/v1/x/core.properties/trend", map[string]any{"report": rep, "key": "salary"}, nil); code != 404 {
		t.Fatalf("hidden property trend: %d", code)
	}
	tom.must("POST", "/api/v1/x/core.properties/trend", map[string]any{"report": rep, "key": "state"}, nil, 200)
	if code := admin.do("POST", "/api/v1/x/core.properties/trend", map[string]any{"report": rep, "key": "title"}, nil); code != 400 {
		t.Fatalf("builtin key: %d", code)
	}
}

// TestReportSubscriptions: the report by e-mail with the subscriber's
// rights; strictly confidential rows are left out.
func TestReportSubscriptions(t *testing.T) {
	e := setup(t)
	c := &capture{}
	e.app.Mail.SetTransport(c)
	admin := e.client()
	admin.login("admin", adminPW)
	var cat1, strict cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Projects"}}, &cat1, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": cat1.ID, "titles": map[string]string{"en": "Board"}}, &strict, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat1.ID, "type": "core.html", "title": "Visible project", "body": htmlBody("<p>a</p>")}, nil, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": strict.ID, "type": "core.html", "title": "Secret merger", "body": htmlBody("<p>b</p>")}, nil, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", strict.ID), map[string]any{"classification": 3}, nil, 200)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "sue", "password": "Passw0rd!Passw0rd", "display_name": "Sue", "email": "sue@example.com"}, &u, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat1.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 1, "inherit": true}, nil, 201)
	page := macroPage(t, admin, cat1.ID, "properties", map[string]string{"source": fmt.Sprintf("category:%d+sub", cat1.ID), "title": "Portfolio"})
	sue := e.client()
	sue.login("sue", "Passw0rd!Passw0rd")
	body := map[string]any{"element_id": page, "params": map[string]string{"source": fmt.Sprintf("category:%d+sub", cat1.ID), "title": "Portfolio"}, "title": "Portfolio", "frequency": "hourly"}
	if code := sue.do("POST", "/api/v1/x/core.properties/subscriptions", body, nil); code != 400 {
		t.Fatalf("bad frequency: %d", code)
	}
	body["frequency"] = "weekly"
	var sub struct {
		ID int64 `json:"id"`
	}
	sue.must("POST", "/api/v1/x/core.properties/subscriptions", body, &sub, 201)
	var list []struct {
		ID int64 `json:"id"`
	}
	sue.must("GET", fmt.Sprintf("/api/v1/x/core.properties/subscriptions?element=%d", page), nil, &list, 200)
	if len(list) != 1 {
		t.Fatalf("subscriptions: %+v", list)
	}
	if code := admin.do("DELETE", fmt.Sprintf("/api/v1/x/core.properties/subscriptions/%d", sub.ID), nil, nil); code != 404 {
		t.Fatalf("other person ended the subscription: %d", code)
	}
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE plugin_prop_subs SET next_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Workers.RunOnce(context.Background(), "core.properties.subscriptions"); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	if !c.find("sue@example.com", "Visible project") || c.find("sue@example.com", "Secret merger") {
		t.Fatalf("report mail: %d messages", c.count())
	}
	sue.must("DELETE", fmt.Sprintf("/api/v1/x/core.properties/subscriptions/%d", sub.ID), nil, nil, 204)
}

// TestProtectionOptions: the original instead of watermarks and blocked
// downloads, and formulas in downloads, when chosen.
func TestProtectionOptions(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var folder, sub cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Design"}}, &folder, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": folder.ID, "titles": map[string]string{"en": "Drafts"}}, &sub, 201)
	var bob struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd!x", "display_name": "Bob"}, &bob, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", folder.ID), map[string]any{"subject_type": "user", "subject_id": bob.ID, "permissions": 1, "inherit": true}, nil, 201)
	bc := e.client()
	bc.login("bob", "Bob-Passw0rd!x")
	_, img := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": sub.ID, "title": "Logo", "alt": "Logo"}, "logo.png", testPNG(120, 80))
	_, orig, _ := admin.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", img.ID))
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", folder.ID), map[string]any{"classification": 3}, nil, 200)
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d", folder.ID), map[string]any{"watermark": "maybe"}, nil); code != 400 {
		t.Fatalf("invalid watermark setting: %d", code)
	}
	if _, marked, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", img.ID)); bytes.Equal(marked, orig) {
		t.Fatal("no watermark by default")
	}
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", img.ID)); c != 403 {
		t.Fatalf("download by default: %d", c)
	}
	// the original: set on the space, applies below
	var cv struct {
		Watermark *string `json:"watermark"`
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", folder.ID), map[string]any{"watermark": "off", "downloads": "allow"}, &cv, 200)
	if cv.Watermark == nil || *cv.Watermark != "off" {
		t.Fatalf("setting: %+v", cv)
	}
	if c, plain, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", img.ID)); c != 200 || !bytes.Equal(plain, orig) {
		t.Fatalf("original view: %d", c)
	}
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", img.ID)); c != 200 {
		t.Fatalf("original download: %d", c)
	}
	// a subcategory can switch protection on again
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"watermark": "auto", "downloads": "auto"}, nil, 200)
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", img.ID)); c != 403 {
		t.Fatalf("protection below: %d", c)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"watermark": "", "downloads": ""}, nil, 200)
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", img.ID)); c != 200 {
		t.Fatalf("as above again: %d", c)
	}

	// downloads of reports: safe by default, formulas only when chosen
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "=1+1", "body": htmlBody("<p>a</p>")}, nil, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", folder.ID), map[string]any{"classification": 0}, nil, 200)
	q := func(mode string) string {
		return `{"sources":[{"kind":"category","id":` + fmt.Sprint(sub.ID) + `}],"export_mode":"` + mode + `"}`
	}
	sheet := func(x string) string {
		z, err := zip.NewReader(strings.NewReader(x), int64(len(x)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range z.File {
			if f.Name == "xl/worksheets/sheet1.xml" {
				r, _ := f.Open()
				b, _ := io.ReadAll(r)
				return string(b)
			}
		}
		return ""
	}
	_, x := admin.rawBody("POST", "/api/v1/x/core.properties/query.xlsx", q("safe"))
	if s := sheet(x); strings.Contains(s, "<f>") || !strings.Contains(s, "=1+1") {
		t.Fatalf("safe sheet: %s", s)
	}
	_, x = admin.rawBody("POST", "/api/v1/x/core.properties/query.xlsx", q("original"))
	if s := sheet(x); !strings.Contains(s, "<f>1+1</f>") {
		t.Fatalf("original sheet: %s", s)
	}
	_, c1 := admin.rawBody("POST", "/api/v1/x/core.properties/query.csv", q("safe"))
	_, c2 := admin.rawBody("POST", "/api/v1/x/core.properties/query.csv", q("original"))
	if !strings.Contains(c1, "'=1+1") || strings.Contains(c2, "'=1+1") || !strings.Contains(c2, "=1+1") {
		t.Fatalf("csv: %q / %q", c1, c2)
	}
}

// TestMediumSettingsFirst: restrictions set on a medium itself win over
// more open settings of its category or space and over embedding pages.
func TestMediumSettingsFirst(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var space cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Open space"}}, &space, 201)
	var bob struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd!x", "display_name": "Bob"}, &bob, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", space.ID), map[string]any{"subject_type": "user", "subject_id": bob.ID, "permissions": 1, "inherit": true}, nil, 201)
	// the space is strictly confidential but chooses the original everywhere
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", space.ID), map[string]any{"classification": 3, "watermark": "off", "downloads": "allow"}, nil, 200)
	bc := e.client()
	bc.login("bob", "Bob-Passw0rd!x")
	_, marked := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": space.ID, "watermark": map[string]any{"mode": "viewer_view", "user": true, "date": true}}, "plan.png", testPNG(300, 200))
	_, viewOnly := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": space.ID, "title": "Org", "alt": "Org"}, "org.png", testPNG(200, 100))
	admin.must("PATCH", fmt.Sprintf("/api/v1/media/%d", viewOnly.ID), map[string]any{"download": false}, nil, 200)
	_, open := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": space.ID, "title": "Free", "alt": "Free"}, "free.png", testPNG(201, 100))
	// a page embeds all three files directly
	var hashes []string
	for _, id := range []int64{marked.ID, viewOnly.ID, open.ID} {
		var hsh string
		if err := e.app.DB.QueryRow(context.Background(), `SELECT hash FROM media_versions WHERE media_id=$1 ORDER BY version DESC LIMIT 1`, id).Scan(&hsh); err != nil || hsh == "" {
			t.Fatalf("hash: %v", err)
		}
		hashes = append(hashes, hsh)
	}
	src := ""
	for _, hsh := range hashes {
		src += `<p><img src="/api/v1/files/` + hsh + `" alt="x"></p>`
	}
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": space.ID, "type": "core.html", "title": "Plans", "body": htmlBody(src)}, nil, 201)

	// medium with its own watermark: always marked, never raw
	_, orig, _ := admin.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", marked.ID))
	if _, v, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", marked.ID)); bytes.Equal(v, orig) {
		t.Fatal("own watermark overridden by the space")
	}
	if c, _, _ := bc.fetch(t, "/api/v1/files/"+hashes[0]); c != 404 {
		t.Fatalf("raw file of a watermarked medium through the page: %d", c)
	}
	// view only: no download although the space allows downloads
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", viewOnly.ID)); c != 403 {
		t.Fatalf("download of a view-only medium: %d", c)
	}
	if c, _, _ := bc.fetch(t, "/api/v1/files/"+hashes[1]); c != 404 {
		t.Fatalf("raw file of a view-only medium through the page: %d", c)
	}
	// without own settings the choice of the space counts (original)
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", open.ID)); c != 200 {
		t.Fatalf("open medium: %d", c)
	}
	if c, _, _ := bc.fetch(t, "/api/v1/files/"+hashes[2]); c != 200 {
		t.Fatalf("open file: %d", c)
	}
}
