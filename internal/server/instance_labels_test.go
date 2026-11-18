package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestInstanceLabels: an instance (a blog) changes the texts its plugin
// declared per active language; they are stored as UI texts
// "<key>@<plugin>/<id>", only those who manage the instance change them,
// readers get them with the page, and they are not part of the global
// texts. Purging the instance removes them.
func TestInstanceLabels(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var cat, blog, u obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "News"}}, &cat, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "authenticated", "permissions": 1}, nil, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "content.blog", "title": "Blog", "body": map[string]any{"category_id": cat.ID}}, &blog, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "lea", "password": "Passw0rd!Passw0rd"}, &u, 201)
	lea := e.client()
	lea.login("lea", "Passw0rd!Passw0rd")
	path := fmt.Sprintf("/api/v1/elements/%d/labels", blog.ID)
	type labels struct {
		Labels []struct {
			Key     string            `json:"key"`
			Default map[string]string `json:"default"`
			Value   map[string]string `json:"value"`
		} `json:"labels"`
		CanEdit bool `json:"can_edit"`
	}
	var l labels
	lea.must("GET", path, nil, &l, 200)
	if l.CanEdit || len(l.Labels) == 0 || l.Labels[0].Default["en"] == "" {
		t.Fatalf("reader: %+v", l)
	}
	if code := lea.do("PUT", path, map[string]any{"values": map[string]any{"en": map[string]string{"content.blog.new_post": "x"}}}, nil); code != 403 {
		t.Fatalf("reader changed labels: %d", code)
	}
	if code := e.client().do("GET", path, nil, nil); code != 401 && code != 404 {
		t.Fatalf("guest reads labels of an internal blog: %d", code)
	}
	// validation
	for _, bad := range []map[string]any{
		{"en": map[string]string{"content.blog.by": "x"}},       // not declared
		{"xx": map[string]string{"content.blog.new_post": "x"}}, // not an active language
		{"en": map[string]string{"content.blog.new_post": strings.Repeat("a", 201)}},
		{"en": map[string]string{"content.blog.new_post": "a\nb"}},
	} {
		if code := admin.do("PUT", path, map[string]any{"values": bad}, nil); code != 400 {
			t.Fatalf("%v: %d", bad, code)
		}
	}
	admin.must("PUT", path, map[string]any{"values": map[string]any{"en": map[string]string{"content.blog.new_post": "Write an article"}, "de": map[string]string{"content.blog.new_post": "Artikel schreiben"}}}, &l, 200)
	lea.must("GET", path, nil, &l, 200)
	found := false
	for _, x := range l.Labels {
		if x.Key == "content.blog.new_post" && x.Value["en"] == "Write an article" && x.Value["de"] == "Artikel schreiben" {
			found = true
		}
	}
	if !found {
		t.Fatalf("labels: %+v", l)
	}
	// not part of the texts everybody loads
	var msgs map[string]string
	lea.must("GET", "/api/v1/i18n/en", nil, &msgs, 200)
	for k := range msgs {
		if strings.Contains(k, "@") {
			t.Fatalf("instance label in the global texts: %s", k)
		}
	}
	// administrators see them in the translation list and can import them
	key := fmt.Sprintf("content.blog.new_post@content.blog/%d", blog.ID)
	var list []struct {
		Key   string            `json:"key"`
		Value map[string]string `json:"value"`
	}
	admin.must("GET", "/api/v1/admin/i18n/de", nil, &list, 200)
	found = false
	for _, x := range list {
		if x.Key == key && x.Value[""] == "Artikel schreiben" {
			found = true
		}
	}
	if !found {
		t.Fatal("instance label missing in the translation list")
	}
	admin.must("PUT", "/api/v1/admin/i18n/de", map[string]string{key: "Beitrag verfassen"}, nil, 200)
	lea.must("GET", path, nil, &l, 200)
	for _, x := range l.Labels {
		if x.Key == "content.blog.new_post" && x.Value["de"] != "Beitrag verfassen" {
			t.Fatalf("set through the translations: %+v", x)
		}
	}
	// the type tells the interface which texts can be changed
	var site struct {
		ElementTypes []struct {
			ID             string   `json:"id"`
			InstanceLabels []string `json:"instance_labels"`
		} `json:"element_types"`
	}
	lea.must("GET", "/api/v1/site", nil, &site, 200)
	for _, ty := range site.ElementTypes {
		if ty.ID == "content.blog" && len(ty.InstanceLabels) == 0 {
			t.Fatal("content.blog without instance labels")
		}
	}
	// purging the blog removes its labels
	admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", blog.ID), nil, nil, 204)
	admin.must("DELETE", fmt.Sprintf("/api/v1/trash/element/%d", blog.ID), nil, nil, 204)
	var n int
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM ui_translations WHERE key LIKE $1`, "%@content.blog/"+fmt.Sprint(blog.ID)).Scan(&n); err != nil || n != 0 {
		t.Fatalf("labels left: %d %v", n, err)
	}
}
