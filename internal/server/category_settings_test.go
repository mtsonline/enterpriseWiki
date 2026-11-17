package server_test

import (
	"fmt"
	"slices"
	"testing"
)

// TestCategorySettings: settings of every category are inherited; the
// nearest language, accent and own navigation apply; allowed types can only
// be narrowed below (an empty intersection allows nothing); the parts of a
// space stay with the space; only managers change them.
func TestCategorySettings(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var top, sub, deep obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Top"}}, &top, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Sub"}}, &sub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": sub.ID, "titles": map[string]string{"en": "Deep"}}, &deep, 201)
	set := func(id int64, body map[string]any, want int) {
		t.Helper()
		if code := admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d/settings", id), body, nil); code != want {
			t.Fatalf("settings of %d %v: %d, want %d", id, body, code, want)
		}
	}
	set(top.ID, map[string]any{"language": "de", "accent": "#0b5cad", "allowed_types": []string{"core.html", "content.blog"}}, 200)
	set(sub.ID, map[string]any{"accent": "#7a1f1f", "own_tree": true, "allowed_types": []string{"content.blog", "content.faq"}}, 200)
	type settings struct {
		Effective struct {
			Language     string   `json:"language"`
			Accent       string   `json:"accent"`
			AllowedTypes []string `json:"allowed_types"`
			OwnTree      bool     `json:"own_tree"`
		} `json:"effective"`
		From     map[string]int64 `json:"from"`
		TreeRoot int64            `json:"tree_root"`
	}
	var st settings
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d/settings", deep.ID), nil, &st, 200)
	if st.Effective.Language != "de" || st.Effective.Accent != "#7a1f1f" || !slices.Equal(st.Effective.AllowedTypes, []string{"content.blog"}) ||
		st.TreeRoot != sub.ID || st.From["language"] != top.ID || st.From["accent"] != sub.ID {
		t.Fatalf("effective settings: %+v", st)
	}
	el := func(cat int64, typ string, want int) {
		t.Helper()
		body := map[string]any{"category_id": cat, "type": typ, "title": "x"}
		if typ == "content.blog" {
			body["body"] = map[string]any{"category_id": cat}
		}
		if code := admin.do("POST", "/api/v1/elements", body, nil); code != want {
			t.Fatalf("%s in %d: %d, want %d", typ, cat, code, want)
		}
	}
	// narrowed: only the blog is left below Sub (core.html is not allowed there, the FAQ not above)
	el(deep.ID, "core.html", 400)
	el(deep.ID, "content.faq", 400)
	el(deep.ID, "content.blog", 201)
	el(top.ID, "core.html", 201)
	// an empty intersection allows nothing (it never means "all")
	set(deep.ID, map[string]any{"allowed_types": []string{"core.html"}}, 200)
	el(deep.ID, "content.blog", 400)
	el(deep.ID, "core.html", 400)
	// the language of new elements comes from above
	var page struct {
		OriginalLang string `json:"original_lang"`
	}
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "content.blog", "title": "Blog", "body": map[string]any{"category_id": sub.ID}}, &page, 201)
	if page.OriginalLang != "de" {
		t.Fatalf("language of a new element: %q", page.OriginalLang)
	}
	// the space of a page: the own navigation starts at Sub
	var c struct {
		Space struct {
			TreeRoot int64  `json:"tree_root"`
			Accent   string `json:"accent"`
		} `json:"space"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", deep.ID), nil, &c, 200)
	if c.Space.TreeRoot != sub.ID || c.Space.Accent != "#7a1f1f" {
		t.Fatalf("space of a page: %+v", c)
	}
	// page templates of a category: offered there and below, not above
	var tpl obj
	admin.must("POST", "/api/v1/templates", map[string]any{"name": "Sub template", "type": "content.blog", "title": "t", "body": map[string]any{"category_id": sub.ID}, "space_id": sub.ID}, &tpl, 201)
	var tpls []obj
	has := func(cat int64) bool {
		admin.must("GET", fmt.Sprintf("/api/v1/templates?category=%d", cat), nil, &tpls, 200)
		return slices.ContainsFunc(tpls, func(x obj) bool { return x.ID == tpl.ID })
	}
	if !has(deep.ID) || !has(sub.ID) || has(top.ID) {
		t.Fatalf("templates of a category offered in the wrong places")
	}
	// the parts of a space: changing only the key keeps the settings
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d/space", top.ID), map[string]any{"key": "TOPS"}, nil, 200)
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d/settings", top.ID), nil, &st, 200)
	if st.Effective.Language != "de" || len(st.Effective.AllowedTypes) != 2 {
		t.Fatalf("settings lost by the key of the space: %+v", st)
	}
	// only managers; validated
	var u obj
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "kai", "password": "Passw0rd!Passw0rd"}, &u, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", top.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 1 | 2 | 4 | 8}, nil, 201)
	kai := e.client()
	kai.login("kai", "Passw0rd!Passw0rd")
	kai.must("GET", fmt.Sprintf("/api/v1/categories/%d/settings", sub.ID), nil, nil, 200)
	if code := kai.do("PUT", fmt.Sprintf("/api/v1/categories/%d/settings", sub.ID), map[string]any{"allowed_types": []string{}}, nil); code != 403 {
		t.Fatalf("editor changed the settings: %d", code)
	}
	set(sub.ID, map[string]any{"accent": "#ffffff"}, 400)
	set(sub.ID, map[string]any{"allowed_types": []string{"nope.type"}}, 400)
	set(sub.ID, map[string]any{"shortcuts": []map[string]any{{"label": map[string]string{"en": "x"}, "url": "javascript:alert(1)"}}}, 400)
	// moved to the top level, Sub keeps its settings and becomes a space
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/move", sub.ID), map[string]any{"parent_id": 0}, nil, 200)
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d/settings", sub.ID), nil, &st, 200)
	if st.Effective.Accent != "#7a1f1f" || st.Effective.Language != "" || !slices.Equal(st.Effective.AllowedTypes, []string{"content.blog", "content.faq"}) {
		t.Fatalf("settings after moving to the top level: %+v", st)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d/space", sub.ID), map[string]any{"key": "SUBS"}, nil, 200)
}
