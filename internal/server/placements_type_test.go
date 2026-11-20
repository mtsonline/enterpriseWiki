package server_test

import (
	"fmt"
	"testing"
)

// TestPlacementTypeLevel: the layout of an element type applies to the
// pages of that type only, between the site and the categories; only the
// administration changes it, only at the positions of a page.
func TestPlacementTypeLevel(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var cat obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Types"}}, &cat, 201)
	var page, cal obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "A page", "body": htmlBody("<p>x</p>")}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "content.faq", "title": "Questions", "body": map[string]any{}}, &cal, 201)

	admin.must("PUT", "/api/v1/placements/type?target=core.html", map[string]any{"placements": []map[string]any{
		{"block": "core.element.titlebar", "position": "element-header", "sort": 5},
		{"block": "core.element.image", "position": "element-header", "sort": 6},
		{"block": "core.element.info", "position": ""},
	}}, nil, 200)
	// checks: unknown type, a position of the site, the wrong level for a person
	if c := admin.do("PUT", "/api/v1/placements/type?target=nope.type", map[string]any{"placements": []any{}}, nil); c != 400 {
		t.Errorf("unknown type: %d", c)
	}
	if c := admin.do("PUT", "/api/v1/placements/type?target=core.html", map[string]any{"placements": []map[string]any{{"block": "core.search", "position": "header-tools"}}}, nil); c != 400 {
		t.Errorf("a position of the site at the type level: %d", c)
	}
	var u obj
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rea", "password": "Passw0rd!Passw0rd", "email": "rea@example.com"}, &u, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 1 | 2 | 4 | 8 | 16}, nil, 201)
	rea := e.client()
	rea.login("rea", "Passw0rd!Passw0rd")
	if c := rea.do("PUT", "/api/v1/placements/type?target=core.html", map[string]any{"placements": []any{}}, nil); c != 403 {
		t.Errorf("a manager of a category changes the layout of a type: %d", c)
	}

	type level struct {
		Level  string `json:"level"`
		Target string `json:"target"`
	}
	levels := func(el int64) []level {
		t.Helper()
		var ls []level
		rea.must("GET", fmt.Sprintf("/api/v1/placements?category=%d&element=%d", cat.ID, el), nil, &ls, 200)
		return ls
	}
	// the type level between site and category, for pages of that type only
	ls := levels(page.ID)
	at := -1
	for i, l := range ls {
		if l.Level == "type" {
			at = i
			if l.Target != "core.html" {
				t.Errorf("type level of another type: %+v", l)
			}
		}
		if l.Level == "category" && at < 0 {
			t.Errorf("the category comes before the type: %+v", ls)
		}
	}
	if at < 0 {
		t.Fatalf("no type level for a page: %+v", ls)
	}
	for _, l := range levels(cal.ID) {
		if l.Level == "type" {
			t.Errorf("the layout of core.html applies to a FAQ: %+v", l)
		}
	}
	// a page one may not see brings no layout of its type
	var hidden obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Closed"}}, &hidden, 201)
	var hp obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": hidden.ID, "type": "core.html", "title": "Hidden", "body": htmlBody("<p>x</p>")}, &hp, 201)
	var hl []level
	rea.must("GET", fmt.Sprintf("/api/v1/placements?element=%d", hp.ID), nil, &hl, 200)
	for _, l := range hl {
		if l.Level == "type" || l.Level == "instance" {
			t.Errorf("levels of a page the reader may not see: %+v", hl)
		}
	}
}
