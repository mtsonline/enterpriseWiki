package server_test

import (
	"fmt"
	"reflect"
	"testing"
)

// TestEditorTools: the site and categories choose the buttons of the
// formatting toolbar (inherited downwards, [] = inherited, ["*"] = all);
// unknown buttons are refused; only those who manage a category change it;
// saving other settings keeps the site's choice.
func TestEditorTools(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type cat struct {
		ID        int64    `json:"id"`
		Tools     []string `json:"editor_tools"`
		Effective []string `json:"effective_editor_tools"`
	}
	var top, sub, got cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Docs"}}, &top, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Howto"}}, &sub, 201)
	get := func(id int64) cat {
		var r struct {
			Category cat `json:"category"`
		}
		admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", id), nil, &r, 200)
		return r.Category
	}
	if got = get(sub.ID); got.Tools != nil || got.Effective != nil {
		t.Fatalf("new category: %+v (want inherited from the site)", got)
	}

	// the category above chooses; the subcategory inherits
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", top.ID), map[string]any{"editor_tools": []string{"strong", "em", "link"}}, nil, 200)
	if got = get(sub.ID); got.Tools != nil || !reflect.DeepEqual(got.Effective, []string{"strong", "em", "link"}) {
		t.Fatalf("inherited: %+v", got)
	}
	// the subcategory shows all again; [] inherits again
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"editor_tools": []string{"*"}}, nil, 200)
	if got = get(sub.ID); !reflect.DeepEqual(got.Effective, []string{"*"}) {
		t.Fatalf("all: %+v", got)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"editor_tools": []string{}}, nil, 200)
	if got = get(sub.ID); got.Tools != nil || !reflect.DeepEqual(got.Effective, []string{"strong", "em", "link"}) {
		t.Fatalf("inherited again: %+v", got)
	}
	// other changes keep the choice
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", top.ID), map[string]any{"titles": map[string]string{"en": "Docs 2"}}, nil, 200)
	if got = get(top.ID); !reflect.DeepEqual(got.Tools, []string{"strong", "em", "link"}) {
		t.Fatalf("lost by another change: %+v", got)
	}

	// invalid choices
	for name, v := range map[string][]string{
		"unknown":  {"strong", "blink"},
		"twice":    {"strong", "strong"},
		"all+more": {"*", "strong"},
	} {
		if code := admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d", top.ID), map[string]any{"editor_tools": v}, nil); code != 400 {
			t.Fatalf("%s: %d", name, code)
		}
	}

	// only those who manage the category
	var ed struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ed", "password": "Passw0rd!Passw0rd"}, &ed, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", top.ID), map[string]any{"subject_type": "user", "subject_id": ed.ID, "permissions": 1 | 2 | 4}, nil, 201)
	edc := e.client()
	edc.login("ed", "Passw0rd!Passw0rd")
	if code := edc.do("PUT", fmt.Sprintf("/api/v1/categories/%d", top.ID), map[string]any{"editor_tools": []string{"*"}}, nil); code != 403 {
		t.Fatalf("editor changed the toolbar of the category: %d", code)
	}

	// the site's choice: shown in the site information, kept when other settings are saved
	var settings struct {
		Config map[string]any `json:"config"`
	}
	admin.must("GET", "/api/v1/admin/settings", nil, &settings, 200)
	ui := settings.Config["ui"].(map[string]any)
	ui["editor_tools"] = []string{"strong", "em"}
	ui["personal_placement"] = true
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil, 200)
	if code := admin.do("PUT", "/api/v1/admin/settings", map[string]any{"ui": map[string]any{"site_name": "x", "editor_tools": []string{"nope"}}}, nil); code != 400 {
		t.Fatalf("unknown button for the site: %d", code)
	}
	var site struct {
		Tools []string `json:"editor_tools"`
	}
	admin.must("GET", "/api/v1/site", nil, &site, 200)
	if !reflect.DeepEqual(site.Tools, []string{"strong", "em"}) {
		t.Fatalf("site: %+v", site)
	}
}

// TestDuplicateKeepsProtection: a copied category keeps its classification,
// watermark and downloads setting (also a subcategory further down), so the
// copied content is not less protected than the original.
func TestDuplicateKeepsProtection(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type cat struct {
		ID                      int64   `json:"id"`
		Classification          *int    `json:"classification"`
		EffectiveClassification int     `json:"effective_classification"`
		Watermark               *string `json:"watermark"`
		Downloads               *string `json:"downloads"`
	}
	var top, secret, target, cp cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Projects"}}, &top, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Board"}}, &secret, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", secret.ID), map[string]any{"classification": 2, "watermark": "off", "downloads": "allow"}, nil, 200)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "core.html", "title": "Minutes", "body": map[string]any{"format": "html", "source": "<p>x</p>"}}, nil, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Archive"}}, &target, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/duplicate", top.ID), map[string]any{"parent_id": target.ID}, &cp, 201)
	var subs []cat
	admin.must("GET", fmt.Sprintf("/api/v1/categories?parent=%d", cp.ID), nil, &subs, 200)
	if len(subs) != 1 {
		t.Fatalf("copy: %+v", subs)
	}
	var got struct {
		Category cat `json:"category"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", subs[0].ID), nil, &got, 200)
	c := got.Category
	if c.Classification == nil || *c.Classification != 2 || c.EffectiveClassification != 2 {
		t.Fatalf("the copy of a confidential category is not confidential: %+v", c)
	}
	if c.Watermark == nil || *c.Watermark != "off" || c.Downloads == nil || *c.Downloads != "allow" {
		t.Fatalf("watermark/downloads not copied: %+v", c)
	}
}
