package server_test

import (
	"fmt"
	"testing"
)

// TestHiddenCategoryAsSource: a hidden category (not in navigation, menus
// and the general search) is often the source of a blog or a FAQ. Hidden
// says nothing about permissions: its entries are listed, and people with
// the permissions of the instance write into it. Only the permissions of
// the category of an entry decide whether it is shown.
func TestHiddenCategoryAsSource(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var site, posts, private, blog, faq obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Site"}}, &site, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", site.ID), map[string]any{"subject_type": "authenticated", "permissions": 1}, nil, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": site.ID, "titles": map[string]string{"en": "Posts"}, "hidden": true}, &posts, 201)
	// a hidden subcategory nobody but administrators may read
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Private"}, "hidden": true}, &private, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": site.ID, "type": "content.blog", "title": "News", "body": map[string]any{"category_id": posts.ID, "recursive": true}}, &blog, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": site.ID, "type": "content.faq", "title": "Help", "body": map[string]any{"category_id": posts.ID, "recursive": true}}, &faq, 201)
	page := func(cat int64, title string) {
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat, "type": "core.html", "title": title, "body": map[string]any{"format": "html", "source": "<p>x</p>"}}, nil, 201)
	}
	page(posts.ID, "From the hidden category")
	page(private.ID, "Secret")
	var u obj
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rita", "password": "Passw0rd!Passw0rd"}, &u, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d/rights", blog.ID), map[string]any{"rules": []map[string]any{{"subject_type": "user", "subject_id": u.ID, "actions": []string{"content.blog.contribute"}}}}, nil, 200)
	rita := e.client()
	rita.login("rita", "Passw0rd!Passw0rd")
	var list struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
		Info struct {
			CanCreate bool `json:"can_create"`
		} `json:"info"`
	}
	rita.must("GET", fmt.Sprintf("/api/v1/x/content.blog/elements/%d/posts?lang=en", blog.ID), nil, &list, 200)
	if len(list.Items) != 1 || list.Items[0].Title != "From the hidden category" || !list.Info.CanCreate {
		t.Fatalf("blog from a hidden category: %+v", list)
	}
	var q struct {
		Sections []struct {
			Questions []struct {
				Title string `json:"title"`
			} `json:"questions"`
		} `json:"sections"`
	}
	rita.must("GET", fmt.Sprintf("/api/v1/x/content.faq/elements/%d/questions?lang=en", faq.ID), nil, &q, 200)
	if len(q.Sections) == 0 || len(q.Sections[0].Questions) != 1 {
		t.Fatalf("FAQ from a hidden category: %+v", q)
	}
	// writing on behalf of the blog into the hidden category
	rita.must("POST", "/api/v1/elements", map[string]any{"via": blog.ID, "category_id": posts.ID, "type": "core.html", "title": "Written", "body": map[string]any{"format": "html", "source": "<p>w</p>"}}, nil, 201)
	// a blog on a hidden category the reader may not see shows nothing of it
	var secret obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": site.ID, "type": "content.blog", "title": "Secret blog", "body": map[string]any{"category_id": private.ID}}, &secret, 201)
	rita.must("GET", fmt.Sprintf("/api/v1/x/content.blog/elements/%d/posts?lang=en", secret.ID), nil, &list, 200)
	if len(list.Items) != 0 {
		t.Fatalf("posts of a category without permission: %+v", list)
	}
}
