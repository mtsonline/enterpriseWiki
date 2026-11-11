package server_test

import (
	"fmt"
	"testing"
)

// TestCategoryTemplate covers the website template per category: only
// active templates, inherited by subcategories, offered in the site info.
func TestCategoryTemplate(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var site struct {
		Template  string `json:"template"`
		Templates []struct {
			ID string `json:"id"`
		} `json:"templates"`
	}
	admin.must("GET", "/api/v1/site", nil, &site, 200)
	if len(site.Templates) < 2 {
		t.Fatalf("templates: %+v", site)
	}
	other := ""
	for _, x := range site.Templates {
		if x.ID != site.Template {
			other = x.ID
		}
	}
	type tcat struct {
		ID                int64  `json:"id"`
		Template          string `json:"template"`
		EffectiveTemplate string `json:"effective_template"`
	}
	var top, sub, deeper tcat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Public"}, "template": other}, &top, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Sub"}}, &sub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": sub.ID, "titles": map[string]string{"en": "Deeper"}}, &deeper, 201)
	if top.Template != other || top.EffectiveTemplate != other || sub.Template != "" || deeper.EffectiveTemplate != other {
		t.Fatalf("inheritance: %+v %+v %+v", top, sub, deeper)
	}
	for _, bad := range []string{"core.macros", "nope", "<script>"} {
		if code := admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"template": bad}, nil); code != 400 {
			t.Fatalf("template %q accepted: %d", bad, code)
		}
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"template": site.Template}, &sub, 200)
	var view struct {
		Category tcat `json:"category"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", deeper.ID), nil, &view, 200)
	if view.Category.EffectiveTemplate != site.Template {
		t.Fatalf("nearest template wins: %+v", view.Category)
	}
	// managers of a category may set it, readers may not
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rita", "password": "Passw0rd!Passw0rd", "display_name": "Rita", "email": "rita@example.com"}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", top.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	rita := e.client()
	rita.login("rita", "Passw0rd!Passw0rd")
	if code := rita.do("PUT", fmt.Sprintf("/api/v1/categories/%d", top.ID), map[string]any{"template": ""}, nil); code != 403 {
		t.Fatalf("reader changed the template: %d", code)
	}
	// removing it falls back to the site template
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", top.ID), map[string]any{"template": ""}, &top, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"template": ""}, nil, 200)
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", deeper.ID), nil, &view, 200)
	if top.EffectiveTemplate != "" || view.Category.EffectiveTemplate != "" {
		t.Fatalf("fallback: %+v %+v", top, view.Category)
	}
}
