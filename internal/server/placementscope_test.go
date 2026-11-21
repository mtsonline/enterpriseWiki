package server_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// TestPlacementScope: a placement for a category applies by default on its
// page, the pages in it and the subcategories; only_category_page leaves
// out the pages in it, no_subcategories the subcategories. Only the level
// category takes a scope; the scope is kept and returned for editing.
func TestPlacementScope(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var cat, sub obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Scope"}}, &cat, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Scope sub"}, "parent_id": cat.ID}, &sub, 201)
	var pg, subPg obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "In it", "body": htmlBody("<p>x</p>")}, &pg, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "In the sub", "body": htmlBody("<p>x</p>")}, &subPg, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/placements/category?target=%d", cat.ID), map[string]any{"placements": []map[string]any{
		{"block": "core.search", "position": "content-top", "sort": 1, "only_category_page": true},
		{"block": "core.favorites", "position": "content-top", "sort": 2, "no_subcategories": true},
		{"block": "core.tree", "position": "content-bottom", "sort": 3},
		{"block": "core.lang", "position": "", "only_category_page": true, "no_subcategories": true},
	}}, nil, 200)
	// kept for editing
	var got struct {
		Placements []map[string]any `json:"placements"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/placements/category?target=%d", cat.ID), nil, &got, 200)
	flags := map[string]string{}
	for _, p := range got.Placements {
		flags[p["block"].(string)] = fmt.Sprint(p["only_category_page"] == true, p["no_subcategories"] == true)
	}
	if flags["core.search"] != "true false" || flags["core.favorites"] != "false true" || flags["core.tree"] != "false false" || flags["core.lang"] != "true true" {
		t.Errorf("stored: %v", flags)
	}
	// what applies where (blocks of the category level of the shown category or its parent)
	applies := func(category, element int64) string {
		t.Helper()
		var ls []struct {
			Level      string `json:"level"`
			Placements []struct {
				Block string `json:"block"`
			} `json:"placements"`
		}
		q := fmt.Sprintf("/api/v1/placements?category=%d", category)
		if element > 0 {
			q += fmt.Sprintf("&element=%d", element)
		}
		admin.must("GET", q, nil, &ls, 200)
		var out []string
		for _, l := range ls {
			if l.Level == "category" {
				for _, p := range l.Placements {
					out = append(out, p.Block)
				}
			}
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	for name, c := range map[string]struct {
		cat, el int64
		want    string
	}{
		"page of the category":      {cat.ID, 0, "core.favorites,core.lang,core.search,core.tree"},
		"a page in it":              {cat.ID, pg.ID, "core.favorites,core.tree"},
		"page of the subcategory":   {sub.ID, 0, "core.search,core.tree"},
		"a page in the subcategory": {sub.ID, subPg.ID, "core.tree"},
	} {
		if got := applies(c.cat, c.el); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
	// only the level category has a scope
	for lv, pos := range map[string]string{"site": "header-tools", fmt.Sprintf("category_page?target=%d", cat.ID): "category-content"} {
		pl := map[string]any{"block": "core.search", "position": pos}
		admin.must("PUT", "/api/v1/placements/"+lv, map[string]any{"placements": []map[string]any{pl}}, nil, 200)
		pl["no_subcategories"] = true
		if code := admin.do("PUT", "/api/v1/placements/"+lv, map[string]any{"placements": []map[string]any{pl}}, nil); code != 400 {
			t.Errorf("scope at %s: %d", lv, code)
		}
	}
}
