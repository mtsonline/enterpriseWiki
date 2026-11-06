package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// Deleting permanently from the trash leaves nothing that points to the
// object: favorites, reactions (also of comments), watches, addresses ...
func TestPurgeRemovesReferences(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	skip := []string{"audit_log", "worker_jobs", "worker_runs", "sessions", "search_changes", "mail_outbox", "mail_recipients"}
	before := dbSnapshot(t, e, skip...)

	var cat, sub, page struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Temp"}}, &cat, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": cat.ID, "titles": map[string]string{"en": "Sub"}}, &sub, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "Page", "body": htmlBody("<p>x</p>")}, &page, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Renamed page", "body": htmlBody("<p>y</p>")}, nil, 200)
	var cm struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", page.ID), map[string]any{"body": "hi"}, &cm, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/x/core.reactions/element/%d", page.ID), map[string]any{"emoji": "👍"}, nil, 200)
	admin.must("POST", fmt.Sprintf("/api/v1/x/core.reactions/comment/%d", cm.ID), map[string]any{"emoji": "❤️"}, nil, 200)
	admin.must("POST", fmt.Sprintf("/api/v1/x/core.reactions/category/%d", sub.ID), map[string]any{"emoji": "🎉"}, nil, 200)
	if c := admin.do("POST", "/api/v1/x/core.favorites/", map[string]any{"type": "element", "id": page.ID}, nil); c >= 300 {
		t.Fatalf("favorite: %d", c)
	}
	if c := admin.do("POST", "/api/v1/x/core.favorites/", map[string]any{"type": "category", "id": sub.ID}, nil); c >= 300 {
		t.Fatalf("favorite: %d", c)
	}

	if c := admin.do("DELETE", fmt.Sprintf("/api/v1/categories/%d", cat.ID), nil, nil); c >= 300 {
		t.Fatalf("delete: %d", c)
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/trash/category/%d", cat.ID), nil, nil, 204)
	if d := diff(before, dbSnapshot(t, e, skip...)); len(d) > 0 {
		t.Fatalf("purge left rows behind:\n%s", strings.Join(d, "\n"))
	}
}
