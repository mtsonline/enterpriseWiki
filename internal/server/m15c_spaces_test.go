package server_test

import (
	"fmt"
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
