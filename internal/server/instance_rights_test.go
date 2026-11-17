package server_test

import (
	"fmt"
	"slices"
	"testing"
)

// TestInstanceRights: permissions on one instance (here a blog) come from
// rules of the instance, from named permissions on its category, or from
// managing the category by a rule; never without seeing the instance, never
// in an archived category, and nobody without the manage permission of the
// instance changes its rules.
func TestInstanceRights(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type idOnly struct {
		ID int64 `json:"id"`
	}
	var cat, other, blog, page idOnly
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Blog home"}}, &cat, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Elsewhere"}}, &other, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "content.blog", "title": "News", "body": map[string]any{"category_id": cat.ID}}, &blog, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "A page", "body": map[string]any{"format": "html", "source": "<p>x</p>"}}, &page, 201)
	var role idOnly
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Structure", "permissions": []string{"core.categories.admin"}}, &role, 201)
	users := map[string]int64{}
	for _, u := range []string{"ann", "bob", "cat", "dan"} {
		var x idOnly
		body := map[string]any{"username": u, "password": "Passw0rd!Passw0rd"}
		if u == "dan" {
			body["roles"] = []int64{role.ID}
		}
		admin.must("POST", "/api/v1/admin/users", body, &x, 201)
		users[u] = x.ID
	}
	login := func(u string) *client {
		c := e.client()
		c.login(u, "Passw0rd!Passw0rd")
		return c
	}
	// everybody signed in may read the blog home
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "authenticated", "permissions": 1}, nil, 201)
	ann, bob, cl, dan := login("ann"), login("bob"), login("cat"), login("dan")
	rightsPath := fmt.Sprintf("/api/v1/elements/%d/rights", blog.ID)
	type rights struct {
		Type   string   `json:"type"`
		Mine   []string `json:"mine"`
		Manage bool     `json:"manage"`
		Rules  []struct {
			SubjectType string   `json:"subject_type"`
			SubjectID   *int64   `json:"subject_id"`
			Actions     []string `json:"actions"`
			Name        string   `json:"name"`
		} `json:"rules"`
	}
	mine := func(c *client) rights {
		t.Helper()
		var r rights
		c.must("GET", rightsPath, nil, &r, 200)
		return r
	}
	set := func(c *client, rules []map[string]any, want int) {
		t.Helper()
		if code := c.do("PUT", rightsPath, map[string]any{"rules": rules}, nil); code != want {
			t.Fatalf("set rules %v: %d, want %d", rules, code, want)
		}
	}
	// a reader has nothing and sees no rules
	if r := mine(ann); len(r.Mine) != 0 || r.Manage || r.Rules != nil || r.Type != "content.blog" {
		t.Fatalf("reader: %+v", r)
	}
	set(ann, []map[string]any{}, 403)
	// the administrator grants Ann "edit all posts" (includes contribute)
	set(admin, []map[string]any{{"subject_type": "user", "subject_id": users["ann"], "actions": []string{"content.blog.edit"}}}, 200)
	if r := mine(ann); !slices.Equal(r.Mine, []string{"content.blog.contribute", "content.blog.edit"}) || r.Manage {
		t.Fatalf("editor: %+v", r)
	}
	set(ann, []map[string]any{}, 403) // editing is not managing
	// the rules name the person
	if r := mine(admin); !r.Manage || len(r.Rules) != 1 || r.Rules[0].Name != "ann" {
		t.Fatalf("rules for the administrator: %+v", r)
	}
	// Bob manages the blog: he may change its rules, but only with
	// permissions of this type and for existing subjects
	set(admin, []map[string]any{
		{"subject_type": "user", "subject_id": users["ann"], "actions": []string{"content.blog.edit"}},
		{"subject_type": "user", "subject_id": users["bob"], "actions": []string{"content.blog.manage"}},
	}, 200)
	if r := mine(bob); !r.Manage || len(r.Mine) != 3 {
		t.Fatalf("manager: %+v", r)
	}
	set(bob, []map[string]any{{"subject_type": "authenticated", "actions": []string{"content.faq.edit"}}}, 400)
	set(bob, []map[string]any{{"subject_type": "user", "subject_id": 999999, "actions": []string{"content.blog.contribute"}}}, 400)
	set(bob, []map[string]any{{"subject_type": "guest", "actions": []string{"content.blog.contribute"}}}, 400)
	set(bob, []map[string]any{{"subject_type": "user", "subject_id": users["bob"], "actions": []string{"content.blog.manage"}},
		{"subject_type": "user", "subject_id": users["bob"], "actions": []string{"content.blog.edit"}}}, 400) // one rule per subject
	set(bob, []map[string]any{{"subject_type": "user", "subject_id": users["bob"], "actions": []string{"content.blog.manage"}},
		{"subject_type": "authenticated", "actions": []string{"content.blog.contribute"}}}, 200)
	if r := mine(cl); !slices.Equal(r.Mine, []string{"content.blog.contribute"}) {
		t.Fatalf("contribute for everybody signed in: %+v", r)
	}
	if r := mine(ann); !slices.Equal(r.Mine, []string{"content.blog.contribute"}) {
		t.Fatalf("Ann's rule was replaced: %+v", r)
	}
	// a permission granted on the category applies to the instances in it
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "user", "subject_id": users["cat"], "permissions": 1, "actions": []string{"content.blog.edit"}}, nil, 201)
	if r := mine(cl); !slices.Equal(r.Mine, []string{"content.blog.contribute", "content.blog.edit"}) {
		t.Fatalf("permission from the category: %+v", r)
	}
	// managing the category by a rule includes all instance permissions;
	// the global category administration (structure only) does not
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "user", "subject_id": users["ann"], "permissions": 1 | 16}, nil, 201)
	if r := mine(ann); !r.Manage || len(r.Mine) != 3 {
		t.Fatalf("category manager: %+v", r)
	}
	if r := mine(dan); r.Manage || len(r.Mine) != 1 {
		t.Fatalf("global category administration gave instance permissions: %+v", r)
	}
	// without seeing the instance: not found, no matter what rules say
	var hidden idOnly
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": other.ID, "type": "content.blog", "title": "Hidden", "body": map[string]any{"category_id": other.ID}}, &hidden, 201)
	hiddenPath := fmt.Sprintf("/api/v1/elements/%d/rights", hidden.ID)
	admin.must("PUT", hiddenPath, map[string]any{"rules": []map[string]any{{"subject_type": "user", "subject_id": users["bob"], "actions": []string{"content.blog.manage"}}}}, nil, 200)
	if code := bob.do("GET", hiddenPath, nil, nil); code != 404 {
		t.Fatalf("instance in a hidden category: %d", code)
	}
	if code := bob.do("PUT", hiddenPath, map[string]any{"rules": []map[string]any{}}, nil); code != 404 {
		t.Fatalf("rules of an instance in a hidden category changed: %d", code)
	}
	// elements without instance permissions
	if code := admin.do("GET", fmt.Sprintf("/api/v1/elements/%d/rights", page.ID), nil, nil); code != 400 {
		t.Fatalf("rights of a page: %d", code)
	}
	// archived: read-only, no instance permissions
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/archive", cat.ID), map[string]any{"archived": true}, nil, 200)
	if r := mine(bob); len(r.Mine) != 0 || r.Manage {
		t.Fatalf("archived: %+v", r)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/archive", cat.ID), map[string]any{"archived": false}, nil, 200)
	// a deleted person loses the rules
	admin.must("DELETE", fmt.Sprintf("/api/v1/admin/users/%d", users["bob"]), nil, nil, 204)
	if r := mine(admin); len(r.Rules) != 1 || r.Rules[0].SubjectType != "authenticated" {
		t.Fatalf("rules after deleting Bob: %+v", r)
	}
	// every change is in the audit log
	var log struct {
		Entries []struct {
			Action string `json:"action"`
		} `json:"entries"`
	}
	admin.must("GET", "/api/v1/admin/audit/search?action=instance.rights", nil, &log, 200)
	if len(log.Entries) < 4 {
		t.Fatalf("audit entries: %+v", log)
	}
}
