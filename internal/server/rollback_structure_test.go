package server_test

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// TestRollbackStructure: a rollback with "structure" undoes moves of
// categories and pages (also into and out of the subtree) and orders since
// the point in time and moves categories created since then to the trash; a
// former place that no longer exists or a missing permission changes nothing.
func TestRollbackStructure(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	newCat := func(title string, parent int64) int64 {
		var c cat
		body := map[string]any{"titles": map[string]string{"en": title}}
		if parent != 0 {
			body["parent_id"] = parent
		}
		admin.must("POST", "/api/v1/categories", body, &c, 201)
		return c.ID
	}
	newPage := func(title string, category int64) int64 {
		var el elem
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": category, "type": "core.html", "title": title, "body": htmlBody("x")}, &el, 201)
		return el.ID
	}
	parentOf := func(id int64) int64 {
		var c struct {
			Category cat `json:"category"`
		}
		admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", id), nil, &c, 200)
		if c.Category.ParentID == nil {
			return 0
		}
		return *c.Category.ParentID
	}
	categoryOf := func(id int64) int64 {
		var el struct {
			CategoryID int64 `json:"category_id"`
		}
		admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", id), nil, &el, 200)
		return el.CategoryID
	}
	children := func(id int64) []int64 {
		var cs []cat
		admin.must("GET", fmt.Sprintf("/api/v1/categories?parent=%d", id), nil, &cs, 200)
		var ids []int64
		for _, c := range cs {
			ids = append(ids, c.ID)
		}
		return ids
	}
	move := func(id, parent int64) {
		admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/move", id), map[string]any{"parent_id": parent}, nil, 200)
	}
	movePage := func(id, category int64) {
		admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/move", id), map[string]any{"category_id": category}, nil, 200)
	}
	type result struct {
		Moved             int `json:"moved"`
		CategoriesTrashed int `json:"categories_trashed"`
		Trashed           int `json:"trashed"`
	}
	pointInTime := func() time.Time {
		time.Sleep(20 * time.Millisecond)
		at := time.Now()
		time.Sleep(20 * time.Millisecond)
		return at
	}

	root := newCat("Root", 0)
	a, b := newCat("A", root), newCat("B", root)
	c := newCat("C", a)
	outside := newCat("Outside", 0)
	p := newPage("P", a)
	q := newPage("Q", outside)
	at := pointInTime()

	// changes since then
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/reorder", root), map[string]any{"order": []int64{b, a}}, nil, 204)
	move(c, b)       // within the subtree
	move(c, outside) // and out of it
	movePage(q, a)   // a page into the subtree
	movePage(p, outside)
	n := newCat("New", root)
	n2 := newCat("New below new", n) // nested: goes with its parent
	newPage("Page in new", n2)
	d := newCat("D", a)

	// without the structure nothing of this changes
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/rollback", root), map[string]any{"at": at, "recursive": true}, nil, 200)
	if parentOf(c) != outside || categoryOf(p) != outside {
		t.Fatal("a rollback without the structure moved something")
	}
	// (it moved the page in New below new to the trash: created later)
	n3 := newCat("Newer", root)
	newPage("Page in newer", n3)

	// a person who may edit and delete here but not manage: refused, nothing changed
	pw := "Passw0rd!Passw0rd"
	var ed struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "editor", "password": pw, "display_name": "Ed"}, &ed, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root), map[string]any{"subject_type": "user", "subject_id": ed.ID, "preset": "editor", "inherit": true}, nil, 201)
	edc := e.client()
	edc.login("editor", pw)
	if code := edc.do("POST", fmt.Sprintf("/api/v1/categories/%d/rollback", root), map[string]any{"at": at, "recursive": true, "structure": true}, nil); code != 403 {
		t.Fatalf("structure without the permission to manage: %d", code)
	}
	if parentOf(c) != outside || !slices.Equal(children(root), []int64{b, a, n, n3}) {
		t.Fatal("a refused rollback changed the structure")
	}

	var res result
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/rollback", root), map[string]any{"at": at, "recursive": true, "structure": true}, &res, 200)
	if res.Moved != 5 || res.CategoriesTrashed != 3 || res.Trashed != 1 {
		t.Fatalf("result %+v, want 5 moves undone, 3 categories and 1 page to the trash", res)
	}
	if parentOf(c) != a {
		t.Fatalf("C is below %d, want %d", parentOf(c), a)
	}
	if got := children(root); !slices.Equal(got, []int64{a, b}) {
		t.Fatalf("order %v, want %v", got, []int64{a, b})
	}
	if categoryOf(p) != a || categoryOf(q) != outside {
		t.Fatalf("pages: P in %d (want %d), Q in %d (want %d)", categoryOf(p), a, categoryOf(q), outside)
	}
	for _, id := range []int64{n, n2, n3, d} {
		if code := admin.do("GET", fmt.Sprintf("/api/v1/categories/%d", id), nil, nil); code != 404 {
			t.Fatalf("category %d created later: %d, want 404 (trash)", id, code)
		}
	}
	// the category itself kept its place
	if parentOf(root) != 0 {
		t.Fatal("the category itself was moved")
	}

	// a former place that no longer exists: 409, nothing changed (all or nothing)
	at2 := pointInTime()
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/reorder", root), map[string]any{"order": []int64{b, a}}, nil, 204)
	move(c, b)
	admin.must("DELETE", fmt.Sprintf("/api/v1/categories/%d", a), nil, nil, 204)
	if code := admin.do("POST", fmt.Sprintf("/api/v1/categories/%d/rollback", root), map[string]any{"at": at2, "recursive": true, "structure": true}, nil); code != 409 {
		t.Fatalf("former place in the trash: %d, want 409", code)
	}
	if parentOf(c) != b {
		t.Fatal("a refused rollback moved a category")
	}
	// restored from the trash, the rollback works
	admin.must("POST", fmt.Sprintf("/api/v1/trash/category/%d/restore", a), nil, nil, 204)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/rollback", root), map[string]any{"at": at2, "recursive": true, "structure": true}, &res, 200)
	if parentOf(c) != a || !slices.Equal(children(root), []int64{a, b}) {
		t.Fatalf("after restoring: C below %d, order %v", parentOf(c), children(root))
	}

	// structure needs recursive
	if code := admin.do("POST", fmt.Sprintf("/api/v1/categories/%d/rollback", root), map[string]any{"at": at2, "structure": true}, nil); code != 400 {
		t.Fatalf("structure without recursive: %d", code)
	}
}
