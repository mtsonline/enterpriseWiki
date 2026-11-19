package server_test

import (
	"testing"
)

// TestPlacementOptions: options of placed blocks are checked on saving
// (schema, contrast, room, anchors), stored, returned with the level and with
// the levels that apply to a page, and the schema is public.
func TestPlacementOptions(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var schema struct {
		Version int `json:"version"`
		Groups  []struct {
			ID string `json:"id"`
		} `json:"groups"`
	}
	e.client().must("GET", "/api/v1/block-options", nil, &schema, 200)
	if schema.Version != 1 || len(schema.Groups) < 5 {
		t.Fatalf("schema: %+v", schema)
	}
	opts := map[string]any{"bg_color": "#102040", "text_color": "#ffffff", "padding_top": map[string]any{"desktop": 6, "mobile": 2}, "hide": []string{"mobile"}, "anchor": "search-box"}
	admin.must("PUT", "/api/v1/placements/site", map[string]any{"placements": []map[string]any{
		{"block": "core.search", "position": "footer", "sort": 10, "options": opts},
		{"block": "core.user", "position": "header-tools"},
	}}, nil, 200)
	type pl struct {
		Block   string         `json:"block"`
		Options map[string]any `json:"options"`
	}
	var level struct {
		Placements []pl `json:"placements"`
	}
	admin.must("GET", "/api/v1/placements/site", nil, &level, 200)
	found := false
	for _, p := range level.Placements {
		switch p.Block {
		case "core.search":
			found = true
			if p.Options["bg_color"] != "#102040" || p.Options["anchor"] != "search-box" {
				t.Fatalf("stored options: %v", p.Options)
			}
		case "core.user":
			if len(p.Options) != 0 {
				t.Fatalf("a block without options got some: %v", p.Options)
			}
		}
	}
	if !found {
		t.Fatal("placement with options missing")
	}
	// the levels of a page carry the options (also for guests)
	var levels []struct {
		Level      string `json:"level"`
		Placements []pl   `json:"placements"`
	}
	e.client().must("GET", "/api/v1/placements", nil, &levels, 200)
	ok := false
	for _, l := range levels {
		for _, p := range l.Placements {
			if l.Level == "site" && p.Block == "core.search" && p.Options["text_color"] == "#ffffff" {
				ok = true
			}
		}
	}
	if !ok {
		t.Fatalf("resolved levels without the options: %+v", levels)
	}
	// refused: low contrast, unknown option, three columns on the phone, a
	// picture of another site, a hidden block with options, an anchor twice
	for name, pls := range map[string][]map[string]any{
		"low contrast":    {{"block": "core.search", "position": "footer", "options": map[string]any{"bg_color": "#ffffff", "text_color": "#bbbbbb"}}},
		"unknown option":  {{"block": "core.search", "position": "footer", "options": map[string]any{"style": "x"}}},
		"columns":         {{"block": "core.search", "position": "footer", "options": map[string]any{"columns": 3}}},
		"foreign picture": {{"block": "core.search", "position": "footer", "options": map[string]any{"bg_image": "https://example.com/a.png", "text_color": "#ffffff", "overlay_color": "#000000", "overlay_opacity": 80}}},
		"hidden":          {{"block": "core.search", "position": "", "options": map[string]any{"radius": 4}}},
		"anchor twice": {
			{"block": "core.search", "position": "footer", "options": map[string]any{"anchor": "x"}},
			{"block": "core.user", "position": "header-tools", "options": map[string]any{"anchor": "x"}},
		},
	} {
		if code := admin.do("PUT", "/api/v1/placements/site", map[string]any{"placements": pls}, nil); code != 400 {
			t.Fatalf("%s: %d, want 400", name, code)
		}
	}
	// nothing of the refused changes was stored
	admin.must("GET", "/api/v1/placements/site", nil, &level, 200)
	for _, p := range level.Placements {
		if p.Block == "core.search" && p.Options["bg_color"] != "#102040" {
			t.Fatalf("a refused change was stored: %v", p.Options)
		}
	}
}
