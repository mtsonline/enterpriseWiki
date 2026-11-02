package server_test

import (
	"fmt"
	"testing"
)

type menuItem struct {
	Label    map[string]string `json:"label"`
	Type     string            `json:"type"`
	Href     string            `json:"href"`
	Submenu  bool              `json:"submenu"`
	Children []menuItem        `json:"children"`
}

func TestMultilevelMenus(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()
	var pub, intranet, hidden cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Public", "de": "Öffentlich"}}, &pub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Intranet"}}, &intranet, 201)
	// top-level categories are visible to everybody, their children are not
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": intranet.ID, "titles": map[string]string{"en": "Internal"}}, &hidden, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", pub.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	var page, secret elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "core.html", "title": "Contact", "body": htmlBody("<p>x</p>")}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": hidden.ID, "type": "core.html", "title": "Salaries"}, &secret, 201)

	var sub, main struct {
		ID int64 `json:"id"`
	}
	// a submenu without a position
	admin.must("POST", "/api/v1/x/core.menu/menus", map[string]any{"name": "Service", "location": "", "items": []map[string]any{
		{"type": "element", "element_id": page.ID},
		{"type": "element", "element_id": secret.ID},
		{"type": "url", "label": map[string]string{"en": "Status"}, "url": "https://status.example.com", "new_tab": true},
	}}, &sub, 200)
	admin.must("POST", "/api/v1/x/core.menu/menus", map[string]any{"name": "Main", "location": "header-nav", "items": []map[string]any{
		{"type": "category", "category_id": pub.ID, "children": []map[string]any{{"type": "element", "element_id": page.ID, "label": map[string]string{"en": "Write to us"}}}},
		{"type": "menu", "menu_id": sub.ID, "label": map[string]string{"en": "Service"}},
		{"type": "heading", "label": map[string]string{"en": "Internal"}, "children": []map[string]any{{"type": "category", "category_id": hidden.ID}}},
		{"label": map[string]string{"en": "Legacy"}, "url": "/search"}, // item without type (older menus)
	}}, &main, 200)

	var ms []struct {
		Name  string     `json:"name"`
		Items []menuItem `json:"items"`
	}
	guest.must("GET", "/api/v1/x/core.menu/menus", nil, &ms, 200)
	if len(ms) != 1 || ms[0].Name != "Main" {
		t.Fatalf("submenu-only menus must not be placed: %+v", ms)
	}
	items := ms[0].Items
	// category with title fallback + own children; submenu expanded and filtered; empty heading dropped
	if len(items) != 3 || items[0].Href != fmt.Sprintf("/c/%d", pub.ID) || items[0].Label["en"] != "Public" || items[0].Label["de"] != "Öffentlich" ||
		len(items[0].Children) != 1 || items[0].Children[0].Href != fmt.Sprintf("/e/%d", page.ID) {
		t.Fatalf("resolved items: %+v", items)
	}
	if !items[1].Submenu || len(items[1].Children) != 2 || items[1].Children[0].Label["en"] != "Contact" || items[1].Children[1].Href != "https://status.example.com" {
		t.Fatalf("submenu: %+v", items[1])
	}
	if items[2].Type != "url" || items[2].Href != "/search" {
		t.Fatalf("legacy item: %+v", items[2])
	}
	// admins see the heading with the hidden category
	ms = nil
	admin.must("GET", "/api/v1/x/core.menu/menus?location=header-nav", nil, &ms, 200)
	if len(ms[0].Items) != 4 || len(ms[0].Items[2].Children) != 1 {
		t.Fatalf("admin view: %+v", ms[0].Items)
	}
	// cycles, unknown menus and missing targets are refused
	if got := admin.do("PUT", fmt.Sprintf("/api/v1/x/core.menu/menus/%d", sub.ID), map[string]any{"name": "Service", "location": "", "items": []map[string]any{{"type": "menu", "menu_id": main.ID}}}, nil); got != 400 {
		t.Fatalf("cycle accepted: %d", got)
	}
	if got := admin.do("POST", "/api/v1/x/core.menu/menus", map[string]any{"name": "X", "location": "sidebar", "items": []map[string]any{{"type": "menu", "menu_id": 9999}}}, nil); got != 400 {
		t.Fatalf("unknown submenu accepted: %d", got)
	}
	if got := admin.do("POST", "/api/v1/x/core.menu/menus", map[string]any{"name": "X", "location": "sidebar", "items": []map[string]any{{"type": "url"}}}, nil); got != 400 {
		t.Fatalf("url item without url accepted: %d", got)
	}
	// a submenu in use cannot be deleted
	if got := admin.do("DELETE", fmt.Sprintf("/api/v1/x/core.menu/menus/%d", sub.ID), nil, nil); got != 409 {
		t.Fatalf("delete used submenu: %d", got)
	}
	var one struct {
		Items []menuItem `json:"items"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/core.menu/menus/%d", main.ID), nil, &one, 200)
	if one.Items[3].Type != "url" {
		t.Fatalf("stored types: %+v", one.Items)
	}
	// deleted targets disappear
	admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 204)
	ms = nil
	guest.must("GET", "/api/v1/x/core.menu/menus", nil, &ms, 200)
	if len(ms[0].Items[0].Children) != 0 || len(ms[0].Items[1].Children) != 1 {
		t.Fatalf("deleted element still in menu: %+v", ms[0].Items)
	}
}
