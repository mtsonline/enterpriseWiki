package server_test

import (
	"fmt"
	"testing"
)

// TestInstanceVia: content on behalf of an instance (a blog). Its
// permissions replace the page permissions of the category, but only for
// what was created through the instance, only inside its categories, only
// its element types, never in archived categories, and they end as soon
// as they are revoked. Changing the instance itself needs its manage
// permission, also for editors of the category.
func TestInstanceVia(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID            int64  `json:"id"`
		OwnerInstance *int64 `json:"owner_instance"`
	}
	var home, other, blog, page obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Blog home"}}, &home, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Other"}}, &other, 201)
	blogBody := map[string]any{"category_id": home.ID, "recursive": true}
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": home.ID, "type": "content.blog", "title": "News", "body": blogBody}, &blog, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": home.ID, "type": "core.html", "title": "A plain page", "body": map[string]any{"format": "html", "source": "<p>x</p>"}}, &page, 201)
	ids := map[string]int64{}
	for _, u := range []string{"ann", "bob", "cy", "dan", "eve"} {
		var x obj
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": u, "password": "Passw0rd!Passw0rd"}, &x, 201)
		ids[u] = x.ID
	}
	login := func(u string) *client {
		c := e.client()
		c.login(u, "Passw0rd!Passw0rd")
		return c
	}
	// everybody signed in reads both categories; Eve edits pages in the blog home
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", home.ID), map[string]any{"subject_type": "authenticated", "permissions": 1}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", other.ID), map[string]any{"subject_type": "authenticated", "permissions": 1}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", home.ID), map[string]any{"subject_type": "user", "subject_id": ids["eve"], "permissions": 1 | 2 | 4 | 8}, nil, 201)
	rules := func(r map[string]string) {
		t.Helper()
		var list []map[string]any
		for u, a := range r {
			list = append(list, map[string]any{"subject_type": "user", "subject_id": ids[u], "actions": []string{a}})
		}
		admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d/rights", blog.ID), map[string]any{"rules": list}, nil, 200)
	}
	rules(map[string]string{"ann": "content.blog.contribute", "bob": "content.blog.edit", "dan": "content.blog.manage"})
	ann, bob, cy, dan, eve := login("ann"), login("bob"), login("cy"), login("dan"), login("eve")
	post := func(c *client, cat int64, typ string, want int) obj {
		t.Helper()
		var x obj
		body := map[string]any{"via": blog.ID, "category_id": cat, "type": typ, "title": "Post", "body": map[string]any{"format": "html", "source": "<p>p</p>"}}
		if typ == "content.blog" {
			body["body"] = blogBody
		}
		if code := c.do("POST", "/api/v1/elements", body, &x); code != want {
			t.Fatalf("post into %d (%s): %d, want %d", cat, typ, code, want)
		}
		return x
	}
	// writing: only with a permission of the blog, only into its categories, only pages
	post(cy, home.ID, "core.html", 403)
	mine := post(ann, home.ID, "core.html", 201)
	if mine.OwnerInstance == nil || *mine.OwnerInstance != blog.ID {
		t.Fatalf("owner of the post: %+v", mine)
	}
	post(ann, other.ID, "core.html", 400)
	post(ann, home.ID, "content.blog", 400)
	if code := ann.do("POST", "/api/v1/elements", map[string]any{"via": page.ID, "category_id": home.ID, "type": "core.html", "title": "x"}, nil); code != 400 {
		t.Fatalf("via a page: %d", code)
	}
	if code := ann.do("POST", "/api/v1/elements", map[string]any{"category_id": home.ID, "type": "core.html", "title": "x"}, nil); code != 403 {
		t.Fatalf("without via, page permissions apply: %d", code)
	}
	bobs := post(bob, home.ID, "core.html", 201)
	save := func(c *client, id int64, want int) {
		t.Helper()
		if code := c.do("PUT", fmt.Sprintf("/api/v1/elements/%d", id), map[string]any{"title": "Changed", "body": map[string]any{"format": "html", "source": "<p>c</p>"}}, nil); code != want {
			t.Fatalf("save %d: %d, want %d", id, code, want)
		}
	}
	// changing: contributors their own, editors all – only what came through the blog
	save(ann, mine.ID, 200)
	save(ann, bobs.ID, 403)
	save(bob, mine.ID, 200)
	save(bob, page.ID, 403)
	save(cy, mine.ID, 403)
	var view struct {
		Permissions []string `json:"permissions"`
	}
	ann.must("GET", fmt.Sprintf("/api/v1/elements/%d", mine.ID), nil, &view, 200)
	if fmt.Sprint(view.Permissions) != "[view edit delete]" {
		t.Fatalf("permissions on the own post: %v", view.Permissions)
	}
	// sections: only with manage
	var sub obj
	if code := bob.do("POST", "/api/v1/categories", map[string]any{"via": blog.ID, "parent_id": home.ID, "titles": map[string]string{"en": "Topics"}}, nil); code != 403 {
		t.Fatalf("section without manage: %d", code)
	}
	if code := dan.do("POST", "/api/v1/categories", map[string]any{"via": blog.ID, "parent_id": other.ID, "titles": map[string]string{"en": "Topics"}}, nil); code != 400 {
		t.Fatalf("section outside the blog: %d", code)
	}
	dan.must("POST", "/api/v1/categories", map[string]any{"via": blog.ID, "parent_id": home.ID, "titles": map[string]string{"en": "Topics"}, "template": "theme.cms"}, &sub, 201)
	// moving: within the blog with its permissions, out of it only with page permissions
	bob.must("POST", fmt.Sprintf("/api/v1/elements/%d/move", mine.ID), map[string]any{"category_id": sub.ID}, nil, 200)
	if code := bob.do("POST", fmt.Sprintf("/api/v1/elements/%d/move", mine.ID), map[string]any{"category_id": other.ID}, nil); code != 403 {
		t.Fatalf("moved out of the blog: %d", code)
	}
	// the blog itself: its manage permission, editing pages is not enough
	cfg := map[string]any{"title": "News", "body": map[string]any{"category_id": home.ID, "recursive": false}}
	for name, c := range map[string]*client{"ann": ann, "bob": bob, "eve": eve} {
		if code := c.do("PUT", fmt.Sprintf("/api/v1/elements/%d", blog.ID), cfg, nil); code != 403 {
			t.Fatalf("%s changed the blog: %d", name, code)
		}
	}
	cfg["body"] = blogBody
	dan.must("PUT", fmt.Sprintf("/api/v1/elements/%d", blog.ID), cfg, nil, 200)
	// drafts on behalf of the blog
	var d struct {
		ID  int64  `json:"id"`
		Via *int64 `json:"via"`
	}
	ann.must("POST", "/api/v1/drafts", map[string]any{"via": blog.ID, "category_id": home.ID, "type": "core.html", "title": "Draft post", "body": map[string]any{"format": "html", "source": "<p>d</p>"}}, &d, 201)
	if d.Via == nil || *d.Via != blog.ID {
		t.Fatalf("draft: %+v", d)
	}
	if code := cy.do("POST", "/api/v1/drafts", map[string]any{"via": blog.ID, "category_id": home.ID, "type": "core.html", "title": "x", "body": map[string]any{"format": "html", "source": "<p>d</p>"}}, nil); code != 403 {
		t.Fatalf("draft without permission: %d", code)
	}
	var pub struct {
		Element obj `json:"element"`
	}
	ann.must("POST", fmt.Sprintf("/api/v1/drafts/%d/publish", d.ID), map[string]any{}, &pub, 200)
	if pub.Element.OwnerInstance == nil || *pub.Element.OwnerInstance != blog.ID {
		t.Fatalf("published draft: %+v", pub)
	}
	// revoked: no more changes
	rules(map[string]string{"ann": "content.blog.contribute"})
	save(bob, mine.ID, 403)
	// archived: read-only for everybody
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/archive", home.ID), map[string]any{"archived": true}, nil, 200)
	post(ann, home.ID, "core.html", 400)
	save(ann, pub.Element.ID, 403)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/archive", home.ID), map[string]any{"archived": false}, nil, 200)
	// deleting the own post
	ann.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", pub.Element.ID), nil, nil, 204)
}
