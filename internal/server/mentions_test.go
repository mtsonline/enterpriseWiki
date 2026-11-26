package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestMentionsAsTheReaderMaySee: "@name" in pages and comments shows as the
// reader may see the person (decision of the project owner, 26.11.2026).
// Who may find the person gets the display name with the menu
// (data-person); everybody else, guests too, gets "@na****" – or, with
// people.mention_display "name", the display name without the menu. A name
// that is no person is masked the same way, mentions in code stay code, and
// only the author gets the source of a comment back.
func TestMentionsAsTheReaderMaySee(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	const pw = "Passw0rd!Passw0rd"
	var g, ann, bob, carl idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Mention team"}, &g, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ann", "password": pw, "display_name": "Ann Archer", "groups": []int64{g.ID}}, &ann, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": pw, "display_name": "Bob Baker", "groups": []int64{g.ID}}, &bob, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "carl", "password": pw, "display_name": "Carl Cook"}, &carl, 201)
	reader := map[string]*client{"guest": e.client()}
	for _, n := range []string{"ann", "carl"} {
		reader[n] = e.client()
		reader[n].login(n, pw)
	}
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Mentions"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 3, "inherit": true}, nil, 201)
	var el elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Plans",
		"body": map[string]any{"format": "markdown", "source": "Ask @bob, then @carl and @nobody.\n\n- [ ] Order paper @bob\n\nIn code: `@bob`"}}, &el, 201)
	page := func(cl *client) string {
		t.Helper()
		var v struct {
			HTML string `json:"html"`
		}
		cl.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", el.ID), nil, &v, 200)
		return v.HTML
	}
	menuBob := fmt.Sprintf(`data-person="%d">Bob Baker<`, bob.ID)
	// Ann is in Bob's group: Bob with the menu, twice (text and task);
	// Carl is outside it, "nobody" is no person: both masked
	h := page(reader["ann"])
	if strings.Count(h, menuBob) != 2 || !strings.Contains(h, "@ca****") || !strings.Contains(h, "@no****") || strings.Contains(h, "Carl Cook") {
		t.Fatalf("ann sees: %s", h)
	}
	if !strings.Contains(h, "<code>@bob</code>") {
		t.Fatalf("a mention in code is not code any more: %s", h)
	}
	// a guest and Carl may not find Bob: no name, no handle, no id (Carl
	// sees himself with the menu)
	for _, who := range []string{"guest", "carl"} {
		h := page(reader[who])
		if strings.Contains(h, "Bob Baker") || strings.Contains(h, "@bob ") || strings.Contains(h, "@bob,") || strings.Contains(h, fmt.Sprintf(`data-person="%d"`, bob.ID)) ||
			!strings.Contains(h, "@bo****") {
			t.Fatalf("%s sees: %s", who, h)
		}
	}
	// search snippets come from the text of the page: the same there
	var found struct {
		Elements []struct {
			ElementID int64  `json:"element_id"`
			Snippet   string `json:"snippet"`
		} `json:"elements"`
	}
	reader["carl"].must("GET", "/api/v1/search?q=paper", nil, &found, 200)
	var snippet string
	for _, x := range found.Elements {
		if x.ElementID == el.ID {
			snippet = x.Snippet
		}
	}
	if !strings.Contains(snippet, "@bo****") || strings.Contains(snippet, "@bob") {
		t.Fatalf("search snippet for carl: %q (%+v)", snippet, found)
	}
	// "name": the display name for everybody, but the menu only for who may find the person
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"people": map[string]any{"mention_display": "name"}}, nil, 200)
	if h := page(reader["guest"]); !strings.Contains(h, "Bob Baker") || strings.Contains(h, "data-person") || !strings.Contains(h, "@no****") {
		t.Fatalf("guest with names: %s", h)
	}
	if h := page(reader["ann"]); strings.Count(h, menuBob) != 2 {
		t.Fatalf("ann with names: %s", h)
	}
	if code := admin.do("PUT", "/api/v1/admin/settings", map[string]any{"people": map[string]any{"mention_display": "all"}}, nil); code != 400 {
		t.Fatalf("unknown mention_display: %d", code)
	}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"people": map[string]any{"mention_display": "masked"}}, nil, 200)

	// comments: the same, and the source only for its author
	var cm struct {
		ID int64 `json:"id"`
	}
	reader["ann"].must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", el.ID), map[string]any{"body": "Thanks @bob and @carl"}, &cm, 201)
	comments := func(cl *client) (string, string) {
		t.Helper()
		var v struct {
			Comments []struct {
				ID   int64  `json:"id"`
				Body string `json:"body"`
				HTML string `json:"html"`
			} `json:"comments"`
		}
		cl.must("GET", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", el.ID), nil, &v, 200)
		for _, x := range v.Comments {
			if x.ID == cm.ID {
				return x.Body, x.HTML
			}
		}
		t.Fatal("comment not listed")
		return "", ""
	}
	if body, html := comments(reader["ann"]); body != "Thanks @bob and @carl" || !strings.Contains(html, menuBob) || !strings.Contains(html, "@ca****") {
		t.Fatalf("author sees: %q %s", body, html)
	}
	for _, who := range []string{"guest", "carl"} {
		if body, html := comments(reader[who]); body != "" || strings.Contains(html, "Bob Baker") || !strings.Contains(html, "@bo****") {
			t.Fatalf("%s sees the comment as: %q %s", who, body, html)
		}
	}
}
