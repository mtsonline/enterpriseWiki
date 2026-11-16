package server_test

import (
	"context"
	"fmt"
	"testing"
)

// clonePage copies an element n times (with its search documents), so that a
// test gets many matches without n API calls.
func clonePage(t *testing.T, e *env, id int64, n int) {
	t.Helper()
	ctx := context.Background()
	var cols string
	if err := e.app.DB.QueryRow(ctx, `SELECT string_agg(quote_ident(column_name), ',' ORDER BY ordinal_position)
		FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'elements'
			AND column_name NOT IN ('id', 'short_url') AND is_generated = 'NEVER'`).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	// #nosec G202 -- column names come from the catalog, quoted
	_, err := e.app.DB.Exec(ctx, `WITH src AS (SELECT `+cols+` FROM elements WHERE id = $1),
		copies AS (INSERT INTO elements (`+cols+`) SELECT src.* FROM src, generate_series(1, $2::int) RETURNING id)
		INSERT INTO search_docs (element_id, lang, category_id, title, body_text, tags, tsv)
			SELECT c.id, d.lang, d.category_id, d.title, d.body_text, d.tags, d.tsv FROM copies c, search_docs d WHERE d.element_id = $1`, id, n)
	if err != nil {
		t.Fatal(err)
	}
}

// crowdedSearch checks that a person finds the pages they may see even when
// far more matches exist that they may not see (more than any result window
// of the search engines), and that changed permissions reach the index.
// sync brings an external index up to date (nil for PostgreSQL); engine is
// the search that must answer.
func crowdedSearch(t *testing.T, e *env, admin *client, engine string, sync func()) {
	t.Helper()
	if sync == nil {
		sync = func() {}
	}
	var secret, open cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Secret needles"}}, &secret, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Open needles"}}, &open, 201)
	var hidden, visible elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "core.html", "title": "Needle needle secret", "body": htmlBody("<p>needle needle needle haystack needle</p>")}, &hidden, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": open.ID, "type": "core.html", "title": "Open meadow", "body": htmlBody("<p>a needle in the meadow</p>")}, &visible, 201)
	// the secret pages rank higher: without the permission filter in the
	// query they fill every result window
	clonePage(t, e, hidden.ID, 1100)
	var nina struct {
		ID int64 `json:"id"`
	}
	pw := "Nina-Passw0rd!x"
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "nina-" + randomHex(t, 3), "password": pw, "display_name": "Nina"}, &nina, 201)
	var u struct {
		Username string `json:"username"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/admin/users/%d", nina.ID), nil, &u, 200)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", open.ID), map[string]any{"subject_type": "user", "subject_id": nina.ID, "permissions": 1, "inherit": true}, nil, 201)
	sync()
	c := e.client()
	c.login(u.Username, pw)
	var res struct {
		Engine   string `json:"engine"`
		Elements []struct {
			ElementID  int64 `json:"element_id"`
			CategoryID int64 `json:"category_id"`
		} `json:"elements"`
	}
	c.must("GET", "/api/v1/search?q=needle", nil, &res, 200)
	if res.Engine != engine || len(res.Elements) != 1 || res.Elements[0].ElementID != visible.ID {
		t.Fatalf("crowded out (%s): %+v", res.Engine, res.Elements)
	}
	// a new permission reaches the index
	var rule struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", secret.ID), map[string]any{"subject_type": "user", "subject_id": nina.ID, "permissions": 1, "inherit": true}, &rule, 201)
	sync()
	c.must("GET", "/api/v1/search?q=needle", nil, &res, 200)
	if len(res.Elements) < 2 {
		t.Fatalf("new permission not in the index (%s): %d hits", res.Engine, len(res.Elements))
	}
	// a removed permission applies at once (the wiki checks every hit), and reaches the index
	admin.must("DELETE", fmt.Sprintf("/api/v1/acl/%d", rule.ID), nil, nil, 204)
	for _, step := range []string{"before sync", "after sync"} {
		if step == "after sync" {
			sync()
		}
		c.must("GET", "/api/v1/search?q=needle", nil, &res, 200)
		for _, h := range res.Elements {
			if h.CategoryID == secret.ID {
				t.Fatalf("%s: removed permission still shows %d (%s)", step, h.ElementID, res.Engine)
			}
		}
		// until the index follows (seconds), the open page may be crowded out
		if step == "after sync" && len(res.Elements) != 1 {
			t.Fatalf("%s: %d hits (%s)", step, len(res.Elements), res.Engine)
		}
	}
}

func TestSearchNotCrowdedOut(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	crowdedSearch(t, e, admin, "postgres", nil)
}
