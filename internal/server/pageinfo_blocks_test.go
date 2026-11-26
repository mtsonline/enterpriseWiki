package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestPageInfoBlocks: tag cloud, changes of a page and contributors are read
// with the permissions of the reader: tags of closed pages and categories
// do not count, the changes of a page the reader may not see are not
// there, and people are named only by their display name.
func TestPageInfoBlocks(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var cat, secretCat, rea, wri obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Area"}}, &cat, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Closed"}, "parent_id": cat.ID}, &secretCat, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rea", "password": "Passw0rd!Passw0rd", "email": "rea@example.com"}, &rea, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "wri.login", "display_name": "Wilma Writer", "password": "Passw0rd!Passw0rd", "email": "wri@example.com"}, &wri, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "user", "subject_id": rea.ID, "permissions": 1, "inherit": false}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "user", "subject_id": wri.ID, "permissions": 7}, nil, 201)
	reader, writer := e.client(), e.client()
	reader.login("rea", "Passw0rd!Passw0rd")
	writer.login("wri.login", "Passw0rd!Passw0rd")
	page := func(c *client, cat int64, title, src string, tags []string) int64 {
		t.Helper()
		var el obj
		c.must("POST", "/api/v1/elements", map[string]any{"category_id": cat, "type": "core.html", "title": title, "tags": tags, "body": map[string]any{"format": "markdown", "source": src}}, &el, 201)
		return el.ID
	}
	view := func(c *client, id int64) string {
		t.Helper()
		var v struct {
			HTML string `json:"html"`
		}
		c.must("GET", fmt.Sprintf("/api/v1/elements/%d", id), nil, &v, 200)
		return v.HTML
	}
	page(admin, cat.ID, "A", "a", []string{"okapi", "budget"})
	page(admin, cat.ID, "B", "b", []string{"okapi"})
	secret := page(admin, secretCat.ID, "Secret", "s", []string{"okapi", "salaries-secret"})
	// a page written by wilma, changed twice
	doc := page(writer, cat.ID, "Doc", "first", nil)
	writer.must("PUT", fmt.Sprintf("/api/v1/elements/%d", doc), map[string]any{"lang": "en", "title": "Doc", "base_rev": 1, "comment": "fixed <typo>", "body": map[string]any{"format": "markdown", "source": "second"}}, nil, 200)

	host := page(admin, cat.ID, "Blocks", fmt.Sprintf("::tagcloud{label=\"All tags\"}\n\n::tagcloud{category=%d}\n\n::tagcloud{category=%d layout=list}\n\n::pagechanges{page=%d}\n\n::contributors{page=%d}\n\n::pagechanges{page=%d}\n", cat.ID, cat.ID, doc, doc, secret), nil)
	h := view(reader, host)
	// tags: only what the reader may see counts (okapi twice, not three times)
	for _, want := range []string{"#okapi", "#budget", "2 pages", `href="/search?category=` + itoa(cat.ID) + `&amp;tags=okapi"`, `aria-label="Tags"`} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q", want)
		}
	}
	// also in the cloud of the whole wiki (there every page is checked on its own)
	if !strings.Contains(h, `aria-label="All tags"`) || strings.Contains(h, "salaries-secret") || strings.Contains(h, "3 pages") {
		t.Fatalf("tags of a closed category counted:\n%s", h)
	}
	// changes and contributors: display names only, the note escaped
	for _, want := range []string{"Wilma Writer", "fixed &lt;typo&gt;", fmt.Sprintf(`href="/e/%d/history"`, doc), "2 versions"} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in\n%s", want, h)
		}
	}
	if strings.Contains(h, "wri.login") {
		t.Fatalf("a login name is shown:\n%s", h)
	}
	// the changes of a page the reader may not see are not there
	if !strings.Contains(h, fmt.Sprintf("the page %d is not there or not visible to you", secret)) {
		t.Fatalf("changes of a closed page:\n%s", h)
	}
	// the administrator sees the closed page too
	if h := view(admin, host); !strings.Contains(h, "salaries-secret") || !strings.Contains(h, "3 pages") {
		t.Fatalf("administrator:\n%s", h)
	}
}
