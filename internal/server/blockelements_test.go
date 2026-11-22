package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestBlockElements: a block is an element of the type core.block. It lives
// in a category (its permissions), is never listed (search, lists), is
// placed at positions like the other blocks and shows only what the viewer
// may see. Once it is placed for the whole site (template, site or element
// type), only the permission core.blocks.edit may change, move or delete it:
// permissions in one category must not change every page of the wiki.
func TestBlockElements(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var c, other cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &c, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Other"}}, &other, 201)
	var role idResp
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Site blocks", "permissions": []string{"core.blocks.edit"}}, &role, 201)
	user := func(name string, roles []int64, perms int) *client {
		var u idResp
		in := map[string]any{"username": name, "password": "Pw-" + name + "-Passw0rd!x", "display_name": name}
		if roles != nil {
			in["roles"] = roles
		}
		admin.must("POST", "/api/v1/admin/users", in, &u, 201)
		if perms != 0 {
			admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": perms, "inherit": true}, nil, 201)
			admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", other.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": perms, "inherit": true}, nil, 201)
		}
		cl := e.client()
		cl.login(name, "Pw-"+name+"-Passw0rd!x")
		return cl
	}
	ed := user("ed", nil, 1|2|4|8)                // view, edit, create, delete in the categories
	sam := user("sam", []int64{role.ID}, 1|2|4|8) // the same and core.blocks.edit
	vic := user("vic", nil, 1)                    // only view
	nob := user("nob", nil, 0)                    // no permissions in the categories
	body := func(s string) map[string]any { return map[string]any{"format": "html", "source": s} }

	// ed writes a block in the category: never listed
	var b elem
	ed.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.block", "title": "Contact", "body": body("<p>Phone 123 <script>x()</script></p>")}, &b, 201)
	var got struct {
		Hidden bool   `json:"hidden"`
		Type   string `json:"type"`
		HTML   string `json:"html"`
	}
	ed.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", b.ID), nil, &got, 200)
	if !got.Hidden || got.Type != "core.block" || strings.Contains(got.HTML, "<script") || !strings.Contains(got.HTML, "Phone 123") {
		t.Fatalf("block element: %+v", got)
	}
	if code := ed.do("PATCH", fmt.Sprintf("/api/v1/elements/%d", b.ID), map[string]any{"hidden": false}, nil); code != 400 {
		t.Fatalf("a block made listed: %d", code)
	}
	var found struct {
		Results []struct {
			ID int64 `json:"id"`
		} `json:"results"`
	}
	admin.must("GET", "/api/v1/search?q=Phone", nil, &found, 200)
	for _, r := range found.Results {
		if r.ID == b.ID {
			t.Fatalf("a block in the search: %+v", found)
		}
	}

	// the list for the placement: only the blocks the person may see
	type listed struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	var list []listed
	vic.must("GET", "/api/v1/x/core.blocks/blocks?lang=en", nil, &list, 200)
	if len(list) != 1 || list[0].ID != b.ID || list[0].Title != "Contact" {
		t.Fatalf("blocks for a viewer: %+v", list)
	}
	nob.must("GET", "/api/v1/x/core.blocks/blocks?lang=en", nil, &list, 200)
	if len(list) != 0 {
		t.Fatalf("blocks for someone who may not see them: %+v", list)
	}
	// the content at a position: only for those who may see it
	type batch struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	var bt batch
	nob.must("GET", fmt.Sprintf("/api/v1/elements/batch?ids=%d&lang=en", b.ID), nil, &bt, 200)
	if len(bt.Items) != 0 {
		t.Fatalf("block content for someone who may not see it: %+v", bt)
	}

	// placed for the category: the editors of the category change it
	block := fmt.Sprintf("core.blocks/%d", b.ID)
	admin.must("PUT", fmt.Sprintf("/api/v1/placements/category?target=%d", c.ID), map[string]any{"placements": []map[string]any{
		{"block": block, "position": "sidebar", "sort": 10, "no_subcategories": true}}}, nil, 200)
	ed.must("PUT", fmt.Sprintf("/api/v1/elements/%d", b.ID), map[string]any{"lang": "en", "title": "Contact", "body": body("<p>Phone 456</p>")}, nil, 200)
	type placement struct {
		Level           string `json:"level"`
		Target          string `json:"target"`
		Position        string `json:"position"`
		NoSubcategories bool   `json:"no_subcategories"`
	}
	var pl []placement
	ed.must("GET", fmt.Sprintf("/api/v1/x/core.blocks/blocks/%d/placements", b.ID), nil, &pl, 200)
	if len(pl) != 1 || pl[0].Level != "category" || pl[0].Target != fmt.Sprint(c.ID) || pl[0].Position != "sidebar" || !pl[0].NoSubcategories {
		t.Fatalf("placements of the block: %+v", pl)
	}
	// where a block is shown is for those who may edit it
	if code := vic.do("GET", fmt.Sprintf("/api/v1/x/core.blocks/blocks/%d/placements", b.ID), nil, nil); code != 403 {
		t.Fatalf("placements for a viewer: %d", code)
	}
	if code := nob.do("GET", fmt.Sprintf("/api/v1/x/core.blocks/blocks/%d/placements", b.ID), nil, nil); code != 404 && code != 403 {
		t.Fatalf("placements for someone who may not see the block: %d", code)
	}
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Page", "body": body("<p>x</p>")}, &page, 201)
	if code := admin.do("GET", fmt.Sprintf("/api/v1/x/core.blocks/blocks/%d/placements", page.ID), nil, nil); code != 400 && code != 404 {
		t.Fatalf("placements of a page that is no block: %d", code)
	}

	// adds the block to a level and gives back a function that restores the level
	place := func(level, target, position string) func() {
		var cur struct {
			Placements []map[string]any `json:"placements"`
		}
		q := ""
		if target != "" {
			q = "?target=" + target
		}
		admin.must("GET", "/api/v1/placements/"+level+q, nil, &cur, 200)
		before := append([]map[string]any{}, cur.Placements...)
		admin.must("PUT", "/api/v1/placements/"+level+q, map[string]any{"placements": append(cur.Placements, map[string]any{"block": block, "position": position, "sort": 99})}, nil, 200)
		return func() {
			admin.must("PUT", "/api/v1/placements/"+level+q, map[string]any{"placements": before}, nil, 200)
		}
	}
	// placed for the whole site: only core.blocks.edit changes, moves or deletes it
	undo := place("site", "", "footer")
	if code := ed.do("PUT", fmt.Sprintf("/api/v1/elements/%d", b.ID), map[string]any{"lang": "en", "title": "Contact", "body": body("<p>Pay here</p>")}, nil); code != 403 {
		t.Fatalf("site-wide block changed by a category editor: %d", code)
	}
	if code := ed.do("POST", fmt.Sprintf("/api/v1/elements/%d/move", b.ID), map[string]any{"category_id": other.ID}, nil); code != 403 {
		t.Fatalf("site-wide block moved by a category editor: %d", code)
	}
	if code := ed.do("DELETE", fmt.Sprintf("/api/v1/elements/%d", b.ID), nil, nil); code != 403 {
		t.Fatalf("site-wide block deleted by a category editor: %d", code)
	}
	if code := ed.do("POST", fmt.Sprintf("/api/v1/elements/%d/restore", b.ID), map[string]any{"lang": "en", "rev": 1}, nil); code != 403 {
		t.Fatalf("old revision of a site-wide block restored by a category editor: %d", code)
	}
	ed.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", b.ID), nil, &got, 200)
	if !strings.Contains(got.HTML, "Phone 456") {
		t.Fatalf("site-wide block after refused changes: %s", got.HTML)
	}
	sam.must("PUT", fmt.Sprintf("/api/v1/elements/%d", b.ID), map[string]any{"lang": "en", "title": "Contact", "body": body("<p>Phone 789</p>")}, nil, 200)
	// the same for the levels template and element type
	undo()
	ed.must("PUT", fmt.Sprintf("/api/v1/elements/%d", b.ID), map[string]any{"lang": "en", "title": "Contact", "body": body("<p>Phone 1</p>")}, nil, 200)
	undo = place("type", "core.html", "element-footer")
	if code := ed.do("PUT", fmt.Sprintf("/api/v1/elements/%d", b.ID), map[string]any{"lang": "en", "title": "Contact", "body": body("<p>Phone 2</p>")}, nil); code != 403 {
		t.Fatalf("block on all pages of a type changed by a category editor: %d", code)
	}
	undo()
	undo = place("template", "core.basic", "footer")
	if code := ed.do("PUT", fmt.Sprintf("/api/v1/elements/%d", b.ID), map[string]any{"lang": "en", "title": "Contact", "body": body("<p>Phone 3</p>")}, nil); code != 403 {
		t.Fatalf("block of the template changed by a category editor: %d", code)
	}
	undo()

	// deleted for good: its placements go too
	admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", b.ID), nil, nil, 204)
	admin.must("DELETE", fmt.Sprintf("/api/v1/trash/element/%d", b.ID), nil, nil, 204)
	var n int
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM placements WHERE block=$1`, block).Scan(&n); err != nil || n != 0 {
		t.Fatalf("placements of a deleted block: %d %v", n, err)
	}
}
