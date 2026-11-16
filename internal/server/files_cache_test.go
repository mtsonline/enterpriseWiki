package server_test

import (
	"fmt"
	"testing"
)

// TestEmbeddedFileFollowsPage: a file embedded in a page is delivered by
// GET /api/v1/files to those who see the page, at once after the page
// changes. Before 16.11.2026 a cache kept the categories of a file for a
// minute: a page moved to a protected category still handed out its file
// to its former readers, and a file just embedded was refused.
func TestEmbeddedFileFollowsPage(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var open, closed cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Open"}}, &open, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Closed"}}, &closed, 201)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rita", "password": "Rita-Passw0rd!x", "display_name": "Rita"}, &u, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", open.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 1, "inherit": true}, nil, 201)
	rita := e.client()
	rita.login("rita", "Rita-Passw0rd!x")

	hash := admin.uploadPNG()
	file := "/api/v1/files/" + hash
	rita.must("GET", file, nil, nil, 404) // not embedded anywhere she can see (remembered)

	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": open.ID, "type": "core.html", "title": "Page",
		"body": htmlBody(`<p><img src="` + file + `" alt="dot"></p>`)}, &page, 201)
	rita.must("GET", file, nil, nil, 200) // just embedded: delivered at once

	admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/move", page.ID), map[string]any{"category_id": closed.ID}, nil, 200)
	rita.must("GET", file, nil, nil, 404) // moved away: no longer delivered
}
