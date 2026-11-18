package server_test

import (
	"fmt"
	"testing"
)

type overviewResp struct {
	Display struct {
		View          string `json:"view"`
		Sort          string `json:"sort"`
		Subcategories bool   `json:"subcategories"`
	} `json:"display"`
	CategoryTitles map[string]string `json:"category_titles"`
	Items          []struct {
		ID      int64             `json:"id"`
		Titles  map[string]string `json:"titles"`
		Hidden  bool              `json:"hidden"`
		Excerpt string            `json:"excerpt"`
	} `json:"items"`
	Total int `json:"total"`
	Page  int `json:"page"`
	Pages int `json:"pages"`
}

func (o overviewResp) ids() []int64 {
	out := []int64{}
	for _, x := range o.Items {
		out = append(out, x.ID)
	}
	return out
}

// TestCategoryOverview covers the overview of a category page: the display
// (inherited until a subcategory chooses its own, checked), only pages the
// reader may view, hidden pages for editors only, subcategories without
// hidden, archived or invisible ones, the order, paging and excerpts.
func TestCategoryOverview(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var top, sub, hid, arch, secret cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Manual"}}, &top, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Chapter"}}, &sub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Hidden folder"}}, &hid, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Old"}}, &arch, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Board"}}, &secret, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", hid.ID), map[string]any{"hidden": true}, nil, 200)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ola", "password": "Ola-Passw0rd!x"}, &u, 201)
	// ola sees the manual and the chapter, not the board
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", top.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 1, "inherit": false}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", sub.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 1, "inherit": true}, nil, 201)
	ola := e.client()
	ola.login("ola", "Ola-Passw0rd!x")

	page := func(c int64, title, text string) int64 {
		t.Helper()
		var el elem
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c, "type": "core.html", "title": title, "body": map[string]any{"format": "markdown", "source": text}}, &el, 201)
		return el.ID
	}
	b := page(top.ID, "Beta", "Beta text "+longText())
	a := page(top.ID, "Alpha", "Alpha text")
	quiet := page(top.ID, "Quiet", "quiet")
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", quiet), map[string]any{"hidden": true}, nil, 200)
	inSub := page(sub.ID, "In chapter", "x")
	inHid := page(hid.ID, "In hidden", "x")
	inArch := page(arch.ID, "In old", "x")
	inSecret := page(secret.ID, "In board", "x")
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/archive", arch.ID), map[string]any{"archived": true}, nil, 200)

	get := func(c *client, id int64, q string) overviewResp {
		t.Helper()
		var o overviewResp
		c.must("GET", fmt.Sprintf("/api/v1/categories/%d/overview%s", id, q), nil, &o, 200)
		return o
	}
	// the default: the list in the order of the category, with excerpts
	o := get(ola, top.ID, "")
	if o.Display.View != "list" || o.Display.Sort != "manual" || fmt.Sprint(o.ids()) != fmt.Sprint([]int64{b, a}) {
		t.Fatalf("default: %+v %v", o.Display, o.ids())
	}
	if o.CategoryTitles["en"] != "Manual" || o.Items[1].Excerpt != "Alpha text" || len([]rune(o.Items[0].Excerpt)) > 241 || o.Items[0].Excerpt[len(o.Items[0].Excerpt)-3:] != "…" {
		t.Fatalf("titles and excerpts: %+v", o)
	}
	// hidden pages: only for editors, marked
	if ids := get(admin, top.ID, "").ids(); fmt.Sprint(ids) != fmt.Sprint([]int64{b, a, quiet}) {
		t.Fatalf("editor: %v", ids)
	}
	// an own display: by title, with subcategories (not hidden, archived or invisible ones)
	setDisplay := func(c *client, id int64, d map[string]any, want int) {
		t.Helper()
		var st struct {
			Own map[string]any `json:"own"`
		}
		admin.must("GET", fmt.Sprintf("/api/v1/categories/%d/settings", id), nil, &st, 200)
		body := map[string]any{}
		for k, v := range st.Own {
			body[k] = v
		}
		body["display"] = d
		if d == nil {
			delete(body, "display")
		}
		if code := c.do("PUT", fmt.Sprintf("/api/v1/categories/%d/settings", id), body, nil); code != want {
			t.Fatalf("display %v: %d, want %d", d, code, want)
		}
	}
	setDisplay(ola, top.ID, map[string]any{"view": "cards"}, 403) // needs Manage
	for _, bad := range []map[string]any{{"view": "bubbles"}, {"view": "list", "sort": "random"}, {"view": "list", "page_size": 5000}} {
		setDisplay(admin, top.ID, bad, 400)
	}
	setDisplay(admin, top.ID, map[string]any{"view": "cards", "sort": "title", "subcategories": true, "excerpt": false}, 200)
	o = get(ola, top.ID, "?lang=en")
	if o.Display.View != "cards" || fmt.Sprint(o.ids()) != fmt.Sprint([]int64{a, b, inSub}) || o.Items[0].Excerpt != "" {
		t.Fatalf("own display: %+v %v", o.Display, o.ids())
	}
	for _, id := range get(admin, top.ID, "").ids() {
		if id == inHid || id == inArch {
			t.Fatalf("hidden or archived subcategory listed: %v", id)
		}
	}
	if ids := get(admin, top.ID, "").ids(); !contains(ids, inSecret) || contains(get(ola, top.ID, "").ids(), inSecret) {
		t.Fatalf("invisible subcategory: admin %v", ids)
	}
	// inherited by the subcategory, until it chooses its own
	if d := get(ola, sub.ID, "").Display; d.View != "cards" || d.Sort != "title" {
		t.Fatalf("inherited: %+v", d)
	}
	setDisplay(admin, sub.ID, map[string]any{"view": "handbook"}, 200)
	if d := get(ola, sub.ID, "").Display; d.View != "handbook" || d.Sort != "manual" {
		t.Fatalf("own below: %+v", d)
	}
	// saving the other settings without the display: as above again
	setDisplay(admin, sub.ID, nil, 200)
	if d := get(ola, sub.ID, "").Display; d.View != "cards" {
		t.Fatalf("back to inherited: %+v", d)
	}
	// paging
	setDisplay(admin, top.ID, map[string]any{"view": "list", "sort": "title", "page_size": 1}, 200)
	o = get(ola, top.ID, "?page=2")
	if o.Total != 2 || o.Pages != 2 || o.Page != 2 || fmt.Sprint(o.ids()) != fmt.Sprint([]int64{b}) {
		t.Fatalf("paging: %+v", o)
	}
	// a view for one block (the tree has no pages of subcategories)
	if code := ola.do("GET", fmt.Sprintf("/api/v1/categories/%d/overview?view=bubbles", top.ID), nil, nil); code != 400 {
		t.Fatalf("bad view: %d", code)
	}
	// readers without the right
	if code := ola.do("GET", fmt.Sprintf("/api/v1/categories/%d/overview", secret.ID), nil, nil); code != 404 && code != 403 {
		t.Fatalf("invisible category: %d", code)
	}
	if code := e.client().do("GET", fmt.Sprintf("/api/v1/categories/%d/overview", top.ID), nil, nil); code != 401 {
		t.Fatalf("guest: %d", code)
	}
}

// TestCategoryPagePlacement covers the arrangement of one category page:
// who may change it, only its positions, featured pages (only there, only
// visible pages, at most the limit), the batch of featured pages and the
// cleanup when a page is deleted for good.
func TestCategoryPagePlacement(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var c, other cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &c, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Board"}}, &other, 201)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "kai", "password": "Kai-Passw0rd!xy"}, &u, 201)
	// kai edits the team pages but does not manage the category
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 1 | 2 | 4, "inherit": true}, nil, 201)
	kai := e.client()
	kai.login("kai", "Kai-Passw0rd!xy")
	mk := func(cid int64, title string) int64 {
		var el elem
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cid, "type": "core.html", "title": title, "body": map[string]any{"format": "markdown", "source": title}}, &el, 201)
		return el.ID
	}
	welcome, contact, board := mk(c.ID, "Welcome"), mk(c.ID, "Contact"), mk(other.ID, "Board minutes")
	path := fmt.Sprintf("/api/v1/placements/category_page?target=%d", c.ID)
	pl := func(block, pos string) map[string]any { return map[string]any{"block": block, "position": pos, "sort": 10} }
	put := func(cl *client, pls []map[string]any, want int) {
		t.Helper()
		if code := cl.do("PUT", path, map[string]any{"placements": pls}, nil); code != want {
			t.Fatalf("set %v: %d, want %d", pls, code, want)
		}
	}
	put(kai, []map[string]any{pl("core.category.title", "category-aside")}, 403)
	put(admin, []map[string]any{pl("core.category.title", "sidebar")}, 400) // only positions of the category page
	put(admin, []map[string]any{pl(fmt.Sprintf("core.category.element/%d", welcome), "category-content"), pl(fmt.Sprintf("core.category.element/%d", contact), "category-aside")}, 200)
	// featured pages: not on other levels, only pages of the wiki that exist
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/placements/category?target=%d", c.ID), map[string]any{"placements": []map[string]any{pl(fmt.Sprintf("core.category.element/%d", welcome), "category-content")}}, nil); code != 400 {
		t.Fatalf("featured page on the inherited level: %d", code)
	}
	put(admin, []map[string]any{pl("core.category.element/999999", "category-content")}, 400)
	// the manager of the team may not feature pages he cannot see
	var mgr struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "mo", "password": "Mo-Passw0rd!xyz"}, &mgr, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": mgr.ID, "permissions": 31, "inherit": true}, nil, 201)
	mo := e.client()
	mo.login("mo", "Mo-Passw0rd!xyz")
	put(mo, []map[string]any{pl(fmt.Sprintf("core.category.element/%d", board), "category-content")}, 400)
	put(mo, []map[string]any{pl(fmt.Sprintf("core.category.element/%d", welcome), "category-content")}, 200)

	// the level applies on the page of this category only
	var levels []struct {
		Level  string `json:"level"`
		Target string `json:"target"`
	}
	kai.must("GET", fmt.Sprintf("/api/v1/placements?category=%d", c.ID), nil, &levels, 200)
	found := false
	for _, l := range levels {
		found = found || (l.Level == "category_page" && l.Target == fmt.Sprint(c.ID))
	}
	if !found {
		t.Fatalf("category page level not resolved: %+v", levels)
	}

	// the batch: in the order asked for, invisible ones left out, at most the limit
	var batch struct {
		Items []elem `json:"items"`
	}
	kai.must("GET", fmt.Sprintf("/api/v1/elements/batch?ids=%d,%d,%d", contact, board, welcome), nil, &batch, 200)
	if len(batch.Items) != 2 || batch.Items[0].ID != contact || batch.Items[1].ID != welcome || batch.Items[1].HTML == "" {
		t.Fatalf("batch: %+v", batch.Items)
	}
	if code := kai.do("GET", "/api/v1/elements/batch?ids=1,x", nil, nil); code != 400 {
		t.Fatalf("bad ids: %d", code)
	}
	many := "1"
	for i := 2; i <= 21; i++ {
		many += fmt.Sprintf(",%d", i)
	}
	if code := kai.do("GET", "/api/v1/elements/batch?ids="+many, nil, nil); code != 400 {
		t.Fatalf("too many ids: %d", code)
	}

	// deleted for good: its placements as featured page go too
	admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", welcome), nil, nil, 204)
	admin.must("DELETE", fmt.Sprintf("/api/v1/trash/element/%d", welcome), nil, nil, 204)
	var lv struct {
		Placements []struct {
			Block string `json:"block"`
		} `json:"placements"`
	}
	admin.must("GET", path, nil, &lv, 200)
	for _, p := range lv.Placements {
		if p.Block == fmt.Sprintf("core.category.element/%d", welcome) {
			t.Fatalf("placement of a page deleted for good remains: %+v", lv)
		}
	}
}

func contains(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func longText() string {
	s := ""
	for i := 0; i < 60; i++ {
		s += "word "
	}
	return s
}
