package server_test

import (
	"fmt"
	"testing"
)

// TestFavorites covers adding, ordering, removing and the visibility rule.
func TestFavorites(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var a, b cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Fav A"}}, &a, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Fav B"}}, &b, 201)
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": a.ID, "type": "core.html", "title": "Fav page", "body": htmlBody("<p>x</p>")}, &page, 201)
	type list struct {
		Items []struct {
			Type string `json:"type"`
			ID   int64  `json:"id"`
			URL  string `json:"url"`
		} `json:"items"`
	}
	var l list
	admin.must("POST", "/api/v1/x/core.favorites/", map[string]any{"type": "element", "id": page.ID}, &l, 200)
	admin.must("POST", "/api/v1/x/core.favorites/", map[string]any{"type": "category", "id": b.ID}, &l, 200)
	if len(l.Items) != 2 || l.Items[0].ID != page.ID || l.Items[0].URL != "/wiki/fav-a/fav-page" {
		t.Fatalf("favorites: %+v", l)
	}
	admin.must("PUT", "/api/v1/x/core.favorites/order", map[string]any{"items": []map[string]any{{"type": "category", "id": b.ID}, {"type": "element", "id": page.ID}}}, &l, 200)
	if l.Items[0].ID != b.ID {
		t.Fatalf("order: %+v", l)
	}
	// deleted pages disappear from the list
	admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 204)
	admin.must("GET", "/api/v1/x/core.favorites/", nil, &l, 200)
	if len(l.Items) != 1 {
		t.Fatalf("deleted favorite listed: %+v", l)
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/x/core.favorites/category/%d", b.ID), nil, &l, 200)
	if len(l.Items) != 0 {
		t.Fatalf("after removing: %+v", l)
	}
	// invisible targets cannot be added; guests have no favorites
	pw := "Passw0rd!Passw0rd"
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "fav", "password": pw, "display_name": "Fav", "roles": []int64{}}, nil, 201)
	u := e.client()
	u.login("fav", pw)
	if code := u.do("POST", "/api/v1/x/core.favorites/", map[string]any{"type": "category", "id": a.ID}, nil); code != 404 && code != 403 {
		t.Fatalf("invisible favorite: %d", code)
	}
	if code := e.client().do("GET", "/api/v1/x/core.favorites/", nil, nil); code != 401 {
		t.Fatalf("guest favorites: %d", code)
	}
}
