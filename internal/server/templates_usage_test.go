package server_test

import (
	"testing"
)

// TestTemplateUsage: the template of the site and how many categories
// choose each template; only for those who manage the settings.
func TestTemplateUsage(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var cat struct {
		ID int64 `json:"id"`
	}
	for range 2 {
		admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Uses CMS"}}, &cat, 201)
		admin.must("PUT", "/api/v1/categories/"+itoa(cat.ID), map[string]any{"template": "theme.cms"}, nil, 200)
	}
	// a deleted category does not count
	admin.must("DELETE", "/api/v1/categories/"+itoa(cat.ID), nil, nil, 204)
	var u struct {
		Site       string         `json:"site"`
		Categories map[string]int `json:"categories"`
	}
	admin.must("GET", "/api/v1/admin/templates/usage", nil, &u, 200)
	if u.Site == "" || u.Categories["theme.cms"] != 1 || len(u.Categories) != 1 {
		t.Fatalf("usage: %+v", u)
	}
	// someone without the right to manage the settings
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "tina", "password": "Tina-Passw0rd!", "display_name": "Tina"}, nil, 201)
	tina := e.client()
	tina.login("tina", "Tina-Passw0rd!")
	tina.must("GET", "/api/v1/admin/templates/usage", nil, nil, 403)
	e.client().must("GET", "/api/v1/admin/templates/usage", nil, nil, 401)
}
