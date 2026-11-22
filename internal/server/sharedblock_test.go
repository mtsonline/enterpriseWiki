package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestSharedBlock: a block element included in pages shows its current
// text everywhere (also to readers, although blocks are never listed), and
// only to readers who may see the block.
func TestSharedBlock(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var c, closed, rea obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Handbook"}}, &c, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Board"}}, &closed, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rea", "password": "Passw0rd!Passw0rd"}, &rea, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": rea.ID, "permissions": 1}, nil, 201)
	block := func(cat int64, text string) int64 {
		var b obj
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat, "type": "core.block", "title": "Shared", "body": map[string]any{"format": "markdown", "source": text}}, &b, 201)
		return b.ID
	}
	shared := block(c.ID, "Opening hours **okapi** 8–17")
	secret := block(closed.ID, "Board walrus")
	pages := []int64{}
	for i := range 2 {
		var p obj
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": fmt.Sprintf("Page %d", i),
			"body": map[string]any{"format": "markdown", "source": fmt.Sprintf("Intro\n\n::include{page=%d title=false}\n\n::include{page=%d title=false}\n", shared, secret)}}, &p, 201)
		pages = append(pages, p.ID)
	}
	reader := e.client()
	reader.login("rea", "Passw0rd!Passw0rd")
	view := func(id int64) string {
		var v struct {
			HTML string `json:"html"`
		}
		reader.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", id), nil, &v, 200)
		return v.HTML
	}
	for _, p := range pages {
		if h := view(p); !strings.Contains(h, "<strong>okapi</strong>") || strings.Contains(h, "walrus") {
			t.Fatalf("a shared block on page %d: %s", p, h)
		}
	}
	// changed once, it changes everywhere
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", shared), map[string]any{"lang": "en", "title": "Shared", "body": map[string]any{"format": "markdown", "source": "Opening hours lynx 9–18"}}, nil, 200)
	for _, p := range pages {
		if h := view(p); !strings.Contains(h, "lynx") || strings.Contains(h, "okapi") {
			t.Fatalf("a changed shared block on page %d: %s", p, h)
		}
	}
}
