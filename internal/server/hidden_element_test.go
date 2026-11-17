package server_test

import (
	"fmt"
	"testing"
)

// TestHiddenElement: a hidden element is not in listings (category for
// readers, blog, overview), not in the search or the tags, but reachable
// by its link; people who may edit it see it in the category, marked.
func TestHiddenElement(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID     int64 `json:"id"`
		Hidden bool  `json:"hidden"`
	}
	var cat, blog, page, other obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "News"}}, &cat, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "authenticated", "permissions": 1}, nil, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "content.blog", "title": "Blog", "body": map[string]any{"category_id": cat.ID}}, &blog, 201)
	body := func(s string) map[string]any { return map[string]any{"format": "html", "source": "<p>" + s + "</p>"} }
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Quiet narwhal", "tags": []string{"quiettag"}, "body": body("narwhal")}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Loud narwhal", "body": body("narwhal")}, &other, 201)
	var after obj
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"hidden": true}, &after, 200)
	if !after.Hidden {
		t.Fatalf("not hidden: %+v", after)
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rea", "password": "Passw0rd!Passw0rd"}, nil, 201)
	rea := e.client()
	rea.login("rea", "Passw0rd!Passw0rd")
	// reachable by its link
	rea.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 200)
	// not in the search, the tags, the blog
	var sr struct {
		Elements []struct {
			ElementID int64 `json:"element_id"`
		} `json:"elements"`
	}
	rea.must("GET", "/api/v1/search?q=narwhal", nil, &sr, 200)
	if len(sr.Elements) != 1 || sr.Elements[0].ElementID != other.ID {
		t.Fatalf("search: %+v", sr)
	}
	rea.must("GET", "/api/v1/search?tags=quiettag", nil, &sr, 200)
	if len(sr.Elements) != 0 {
		t.Fatalf("tag search: %+v", sr)
	}
	var list struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	rea.must("GET", fmt.Sprintf("/api/v1/x/content.blog/elements/%d/posts?lang=en", blog.ID), nil, &list, 200)
	for _, it := range list.Items {
		if it.ID == page.ID {
			t.Fatalf("hidden element in the blog: %+v", list)
		}
	}
	// the category: readers do not see it, editors see it marked
	var c struct {
		Elements []obj `json:"elements"`
	}
	rea.must("GET", fmt.Sprintf("/api/v1/categories/%d", cat.ID), nil, &c, 200)
	for _, x := range c.Elements {
		if x.ID == page.ID {
			t.Fatalf("hidden element listed for a reader")
		}
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", cat.ID), nil, &c, 200)
	found := false
	for _, x := range c.Elements {
		found = found || (x.ID == page.ID && x.Hidden)
	}
	if !found {
		t.Fatalf("editors do not see the hidden element marked: %+v", c)
	}
	// readers cannot hide or show it
	if code := rea.do("PATCH", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"hidden": false}, nil); code != 403 {
		t.Fatalf("reader changed hidden: %d", code)
	}
	// shown again
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"hidden": false}, nil, 200)
	rea.must("GET", "/api/v1/search?q=narwhal", nil, &sr, 200)
	if len(sr.Elements) != 2 {
		t.Fatalf("search after showing it again: %+v", sr)
	}
}
