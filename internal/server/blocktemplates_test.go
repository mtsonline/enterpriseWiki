package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestBlockTemplates: block templates are templates of the kind "block"
// (Markdown of a page), listed apart from page templates, of a kind that
// cannot change, never used as a page template.
func TestBlockTemplates(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type tpl struct {
		ID   int64  `json:"id"`
		Kind string `json:"kind"`
		Type string `json:"type"`
		Body struct {
			Source string `json:"source"`
		} `json:"body"`
	}
	var b, p tpl
	admin.must("POST", "/api/v1/templates", map[string]any{"kind": "block", "name": "Contact box", "type": "content.faq", "global": true,
		"body": map[string]any{"format": "markdown", "source": ":::panel{type=info}\nCall us\n:::\n"}}, &b, 201)
	if b.Kind != "block" || b.Type != "core.html" {
		t.Fatalf("block template: %+v", b)
	}
	admin.must("POST", "/api/v1/templates", map[string]any{"name": "Minutes", "type": "core.html", "global": true, "body": map[string]any{"format": "markdown", "source": "## Agenda"}}, &p, 201)
	if p.Kind != "page" {
		t.Fatalf("default kind: %+v", p)
	}
	ids := func(q string) []int64 {
		t.Helper()
		var l []tpl
		admin.must("GET", "/api/v1/templates"+q, nil, &l, 200)
		var out []int64
		for _, x := range l {
			out = append(out, x.ID)
		}
		return out
	}
	if l := ids(""); len(l) != 1 || l[0] != p.ID {
		t.Errorf("page templates: %v", l)
	}
	if l := ids("?kind=block"); len(l) != 1 || l[0] != b.ID {
		t.Errorf("block templates: %v", l)
	}
	if c := admin.do("GET", "/api/v1/templates?kind=evil", nil, nil); c != 400 {
		t.Errorf("unknown kind: %d", c)
	}
	var one tpl
	admin.must("GET", fmt.Sprintf("/api/v1/templates/%d", b.ID), nil, &one, 200)
	if one.Kind != "block" || !strings.Contains(one.Body.Source, ":::panel") {
		t.Errorf("one block template: %+v", one)
	}
	// the kind stays
	if c := admin.do("PUT", fmt.Sprintf("/api/v1/templates/%d", b.ID), map[string]any{"kind": "page", "name": "x", "type": "core.html", "global": true, "body": map[string]any{"format": "markdown", "source": "x"}}, nil); c != 400 {
		t.Errorf("kind changed: %d", c)
	}
	// a block template is no template for a new page
	var cat struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Blocks"}}, &cat, 201)
	var el struct {
		ID   int64  `json:"id"`
		HTML string `json:"html"`
	}
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Start", "body": map[string]any{"format": "markdown", "source": fmt.Sprintf("::newpage{template=%d}\n", b.ID)}}, &el, 201)
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", el.ID), nil, &el, 200)
	if strings.Contains(el.HTML, "/new?") {
		t.Errorf("a block template as a page template: %s", el.HTML)
	}
	// the toolbar button can be chosen
	var cfg struct {
		Config map[string]any `json:"config"`
	}
	admin.must("GET", "/api/v1/admin/settings", nil, &cfg, 200)
	ui, _ := cfg.Config["ui"].(map[string]any)
	ui["editor_tools"] = []string{"block", "macro", "snippet"}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil, 200)
}
