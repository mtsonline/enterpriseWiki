package server_test

import (
	"fmt"
	"testing"
)

// TestPlacementsInstance: one page (level "instance") arranges the blocks of
// its own positions (element-…). A page needs the permission to edit it, an
// instance (a blog) the permission to manage it; the level applies only to
// people who may see the page; a copy keeps the arrangement and the labels;
// purging the page removes them.
func TestPlacementsInstance(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var cat, page, blog, ed, rd obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &cat, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "A page", "body": map[string]any{"format": "html", "source": "<p>x</p>"}}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "content.blog", "title": "Blog", "body": map[string]any{"category_id": cat.ID}}, &blog, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ed", "password": "Passw0rd!Passw0rd"}, &ed, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rd", "password": "Passw0rd!Passw0rd"}, &rd, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "user", "subject_id": ed.ID, "permissions": 1 | 2 | 4}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "user", "subject_id": rd.ID, "permissions": 1}, nil, 201)
	edc := e.client()
	edc.login("ed", "Passw0rd!Passw0rd")
	rdc := e.client()
	rdc.login("rd", "Passw0rd!Passw0rd")
	at := func(id int64) string { return fmt.Sprintf("/api/v1/placements/instance?target=%d", id) }
	arrangement := map[string]any{"placements": []map[string]any{
		{"block": "core.element.title", "position": ""},
		{"block": "core.share", "position": "element-footer", "sort": 5},
	}}

	// the editor arranges the page; the reader may not; guests neither
	edc.must("PUT", at(page.ID), arrangement, nil, 200)
	if code := rdc.do("PUT", at(page.ID), arrangement, nil); code != 403 {
		t.Fatalf("reader arranged the page: %d", code)
	}
	if code := rdc.do("GET", at(page.ID), nil, nil); code != 403 {
		t.Fatalf("reader reads the arrangement for editing: %d", code)
	}
	if code := e.client().do("GET", at(page.ID), nil, nil); code != 401 {
		t.Fatalf("guest: %d", code)
	}
	// a blog needs the permission to manage it; editing is not enough
	if code := edc.do("PUT", at(blog.ID), arrangement, nil); code != 403 {
		t.Fatalf("editor arranged a blog without managing it: %d", code)
	}
	admin.must("PUT", at(blog.ID), arrangement, nil, 200)

	// only the positions of a page; no required blocks; valid targets
	for name, body := range map[string]map[string]any{
		"site position": {"placements": []map[string]any{{"block": "core.search", "position": "sidebar"}}},
		"required":      {"placements": []map[string]any{{"block": "core.share", "position": "element-footer", "required": true}}},
	} {
		if code := edc.do("PUT", at(page.ID), body, nil); code != 400 {
			t.Fatalf("%s: %d", name, code)
		}
	}
	for _, target := range []string{"0", "x", "01", fmt.Sprint(page.ID + 100000)} {
		if code := admin.do("GET", "/api/v1/placements/instance?target="+target, nil, nil); code != 400 && code != 404 {
			t.Fatalf("target %q: %d", target, code)
		}
	}

	// resolving: the level of the page is there for those who see it
	type level struct {
		Level      string `json:"level"`
		Target     string `json:"target"`
		Placements []struct {
			Block    string `json:"block"`
			Position string `json:"position"`
		} `json:"placements"`
	}
	has := func(c *client, id int64) bool {
		var ls []level
		c.must("GET", fmt.Sprintf("/api/v1/placements?category=%d&element=%d", cat.ID, id), nil, &ls, 200)
		for _, l := range ls {
			if l.Level == "instance" && l.Target == fmt.Sprint(id) {
				return len(l.Placements) == 2
			}
		}
		return false
	}
	if !has(rdc, page.ID) || !has(admin, blog.ID) {
		t.Fatal("the arrangement of the page does not apply")
	}
	var other obj
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ot", "password": "Passw0rd!Passw0rd"}, &other, 201)
	otc := e.client()
	otc.login("ot", "Passw0rd!Passw0rd")
	if has(otc, page.ID) || has(e.client(), page.ID) {
		t.Fatal("the arrangement of a page is shown to people who may not see it")
	}

	// a copy keeps the arrangement and the labels of the instance
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d/labels", blog.ID), map[string]any{"values": map[string]any{"en": map[string]string{"content.blog.new_post": "Write"}}}, nil, 200)
	var dup obj
	admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/duplicate", blog.ID), map[string]any{"category_id": 0}, &dup, 201)
	if !has(admin, dup.ID) {
		t.Fatal("the copy lost the arrangement")
	}
	var lb struct {
		Labels []struct {
			Key   string            `json:"key"`
			Value map[string]string `json:"value"`
		} `json:"labels"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d/labels", dup.ID), nil, &lb, 200)
	copied := false
	for _, l := range lb.Labels {
		if l.Key == "content.blog.new_post" && l.Value["en"] == "Write" {
			copied = true
		}
	}
	if !copied {
		t.Fatalf("the copy lost the labels: %+v", lb)
	}

	// purging removes the arrangement
	admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 204)
	admin.must("DELETE", fmt.Sprintf("/api/v1/trash/element/%d", page.ID), nil, nil, 204)
	var n int
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM placements WHERE level='instance' AND target=$1`, fmt.Sprint(page.ID)).Scan(&n); err != nil || n != 0 {
		t.Fatalf("placements of the purged page: %d %v", n, err)
	}
}
