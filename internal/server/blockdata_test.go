package server_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// TestBlockData: decisions in the text are kept as data for reports when a
// page is saved, restored or copied; queries and the report show only
// entries of pages the reader may see; entries of plugins stay; the data
// can be built again from the latest versions.
func TestBlockData(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var open, board, rea obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Projects"}}, &open, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Board"}}, &board, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rea", "password": "Passw0rd!Passw0rd"}, &rea, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", open.ID), map[string]any{"subject_type": "user", "subject_id": rea.ID, "permissions": 1}, nil, 201)

	var who struct {
		User obj `json:"user"`
	}
	admin.must("GET", "/api/v1/auth/me", nil, &who, 200)
	me := who.User

	md := func(src string) map[string]any { return map[string]any{"format": "markdown", "source": src} }
	full := `Intro

:::decision{title="Use okapi <script>x</script>" status=decided day=2026-11-01 owner="Ana" person=` + fmt.Sprint(rea.ID) + `}
Because it is **fast**.
:::

:::panel{type=info}
:::decision{title="Try lynx" status=open day=2026-12-01 person=` + fmt.Sprint(me.ID) + `}
:::
:::

::decisions{}
`
	var ola obj
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ola", "password": "Passw0rd!Passw0rd"}, &ola, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", ola.ID), map[string]any{"disabled": true}, nil, 200)
	var page, secret obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": open.ID, "type": "core.html", "title": "Plan", "body": md(full)}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": board.ID, "type": "core.html", "title": "Board page",
		"body": md(":::decision{title=\"Buy walrus\" status=decided person=" + fmt.Sprint(ola.ID) + "}\n:::\n")}, &secret, 201)

	reader := e.client()
	reader.login("rea", "Passw0rd!Passw0rd")
	type result struct {
		Items []struct {
			ElementID int64          `json:"element_id"`
			Source    string         `json:"source"`
			Data      map[string]any `json:"data"`
		} `json:"items"`
		Total    int              `json:"total"`
		Complete bool             `json:"complete"`
		People   map[int64]string `json:"people"`
	}
	query := func(c *client, qs string) result {
		t.Helper()
		var r result
		c.must("GET", "/api/v1/data/core.macros.decision?"+qs, nil, &r, 200)
		return r
	}
	titles := func(r result) string {
		var s []string
		for _, it := range r.Items {
			s = append(s, fmt.Sprint(it.Data["title"]))
		}
		return strings.Join(s, "|")
	}

	// the reader sees the decisions of the page they may see, not those of the board
	r := query(reader, "sort=day")
	if r.Total != 2 || titles(r) != "Use okapi <script>x</script>|Try lynx" {
		t.Fatalf("reader: %d %q", r.Total, titles(r))
	}
	if got := r.Items[0].Data; got["status"] != "decided" || got["owner"] != "Ana" || got["day"] != "2026-11-01" || got["text"] != "Because it is fast." || got["person"] != float64(rea.ID) {
		t.Fatalf("data of a decision: %v", got)
	}
	// the names of the people: only those the reader may see
	if len(r.People) != 1 || r.People[rea.ID] != "rea" {
		t.Fatalf("people for the reader: %v", r.People)
	}
	if r := query(admin, ""); r.Total != 3 || len(r.People) != 2 || r.People[me.ID] != "Admin" || r.People[rea.ID] != "rea" {
		t.Fatalf("admin: %d %v", r.Total, r.People)
	}
	if r := query(e.client(), ""); r.Total != 0 {
		t.Fatalf("guest: %d", r.Total)
	}
	// filters
	if r := query(reader, "state=open"); titles(r) != "Try lynx" {
		t.Fatalf("state: %q", titles(r))
	}
	if r := query(reader, "from=2026-11-15"); titles(r) != "Try lynx" {
		t.Fatalf("from: %q", titles(r))
	}
	if r := query(admin, fmt.Sprintf("category=%d", board.ID)); titles(r) != "Buy walrus" {
		t.Fatalf("category: %q", titles(r))
	}
	if r := query(reader, fmt.Sprintf("category=%d", board.ID)); r.Total != 0 {
		t.Fatalf("a category the reader may not see: %d", r.Total)
	}
	if r := query(reader, "q=okapi"); r.Total != 1 {
		t.Fatalf("q: %d", r.Total)
	}
	if r := query(reader, "q=%25"); r.Total != 0 {
		t.Fatalf("q with a wildcard: %d", r.Total)
	}
	reader.must("GET", "/api/v1/data/core.macros.decision?sort=data", nil, nil, 400)
	reader.must("GET", "/api/v1/data/core.macros.decision?from=tomorrow", nil, nil, 400)
	reader.must("GET", "/api/v1/data/nothing.here", nil, nil, 404)

	// the report on the page: escaped, only what the reader may see
	var v struct {
		HTML string `json:"html"`
	}
	reader.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", page.ID), nil, &v, 200)
	if !strings.Contains(v.HTML, "Use okapi &lt;script&gt;") || strings.Contains(v.HTML, "<script>x") || strings.Contains(v.HTML, "walrus") || !strings.Contains(v.HTML, "ew-decisions") {
		t.Fatalf("report: %s", v.HTML)
	}
	// the responsible person: by the rules of the people blocks. The reader
	// sees themselves, but not the admin (no core.users.list_*), in the
	// block and in the report; the admin sees both, but not a disabled user.
	if !strings.Contains(v.HTML, "Responsible: rea, Ana") || !strings.Contains(v.HTML, "<td>rea, Ana</td>") || strings.Contains(v.HTML, ">Admin<") || strings.Contains(v.HTML, "Responsible: Admin") {
		t.Fatalf("people for the reader: %s", v.HTML)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", page.ID), nil, &v, 200)
	if !strings.Contains(v.HTML, "Responsible: rea, Ana") || !strings.Contains(v.HTML, "Responsible: Admin") || !strings.Contains(v.HTML, "<td>Admin</td>") || strings.Contains(v.HTML, "ola") {
		t.Fatalf("people for the admin: %s", v.HTML)
	}
	// a person that is no id is refused when saving the data
	if err := e.app.Content.PutData(identity.With(t.Context(), identity.CLI()), nil, "core.macros", page.ID, "en", "core.macros.decision", 9, map[string]any{"title": "x", "status": "open", "person": "nobody"}); err == nil {
		t.Fatal("a person that is no id was accepted")
	}

	// "my decisions": those the reader is the responsible person of
	var mine obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": open.ID, "type": "core.html", "title": "Mine", "body": md("::decisions{who=mine}\n")}, &mine, 201)
	reader.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", mine.ID), nil, &v, 200)
	if !strings.Contains(v.HTML, `aria-label="My decisions"`) || !strings.Contains(v.HTML, "Use okapi") || strings.Contains(v.HTML, "Try lynx") {
		t.Fatalf("my decisions, reader: %s", v.HTML)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", mine.ID), nil, &v, 200)
	if !strings.Contains(v.HTML, "Try lynx") || strings.Contains(v.HTML, "Use okapi") || strings.Contains(v.HTML, "Buy walrus") {
		t.Fatalf("my decisions, admin: %s", v.HTML)
	}
	// guests have none: never all decisions they may see
	var pub, pubPage obj
	admin.must("POST", "/api/v1/categories", map[string]any{"guest_visible": true, "titles": map[string]string{"en": "Public"}}, &pub, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "core.html", "title": "Open",
		"body": md(":::decision{title=\"Open zebra\" status=open}\n:::\n\n::decisions{who=mine}\n")}, &pubPage, 201)
	guest := e.client()
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", pubPage.ID), nil, &v, 200)
	if !strings.Contains(v.HTML, "Sign in to see your decisions.") || strings.Contains(v.HTML, "<table") {
		t.Fatalf("my decisions, guest: %s", v.HTML)
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", pubPage.ID), nil, nil, 204)

	// a change replaces the entries; restoring brings them back
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Plan", "body": md("No decisions any more")}, nil, 200)
	if r := query(reader, ""); r.Total != 0 {
		t.Fatalf("after a change: %d", r.Total)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/restore", page.ID), map[string]any{"lang": "en", "rev": 1}, nil, 200)
	if r := query(reader, ""); r.Total != 2 {
		t.Fatalf("after restoring: %d", r.Total)
	}
	// a copy has its own entries
	var dup obj
	admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/duplicate", page.ID), map[string]any{}, &dup, 201)
	if r := query(reader, fmt.Sprintf("element=%d", dup.ID)); r.Total != 2 {
		t.Fatalf("copy: %d", r.Total)
	}
	// in the trash: not shown
	admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", dup.ID), nil, nil, 204)
	if r := query(reader, ""); r.Total != 2 {
		t.Fatalf("after the trash: %d", r.Total)
	}

	// entries of a plugin stay when the page is saved; foreign kinds and invalid data are refused
	ctx := identity.With(t.Context(), identity.CLI())
	c := e.app.Content
	if err := c.PutData(ctx, nil, "core.macros", secret.ID, "en", "core.macros.decision", 1, map[string]any{"title": "By plugin", "status": "open"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]any{{"title": "x", "status": "maybe"}, {"title": "x", "status": "open", "extra": 1}, {"status": "open", "day": "1.1.2026"}, {"title": "x"}} {
		if err := c.PutData(ctx, nil, "core.macros", secret.ID, "en", "core.macros.decision", 2, bad); err == nil {
			t.Fatalf("invalid data accepted: %v", bad)
		}
	}
	if err := c.PutData(ctx, nil, "content.team", secret.ID, "en", "core.macros.decision", 3, map[string]any{"title": "x", "status": "open"}); err == nil {
		t.Fatal("a plugin wrote a kind of another plugin")
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", secret.ID), map[string]any{"lang": "en", "title": "Board page", "body": md("Nothing")}, nil, 200)
	if r := query(admin, fmt.Sprintf("element=%d", secret.ID)); titles(r) != "By plugin" || r.Items[0].Source != "core.macros" {
		t.Fatalf("plugin entry after a save: %q", titles(r))
	}

	// building again: from the latest versions; the kind is complete afterwards
	if _, err := e.app.DB.Exec(t.Context(), `DELETE FROM element_data WHERE source='macro'`); err != nil {
		t.Fatal(err)
	}
	member := e.client()
	member.login("rea", "Passw0rd!Passw0rd")
	member.must("POST", "/api/v1/admin/data/rebuild", map[string]any{}, nil, 403)
	admin.must("POST", "/api/v1/admin/data/rebuild", map[string]any{"kind": "nothing.here"}, nil, 404)
	for {
		more, err := c.RebuildData(t.Context(), 1)
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
	}
	if r := query(reader, ""); r.Total != 2 || !r.Complete {
		t.Fatalf("after building: %d %v", r.Total, r.Complete)
	}
	admin.must("POST", "/api/v1/admin/data/rebuild", map[string]any{"kind": "core.macros.decision"}, nil, 200)
	if r := query(reader, ""); r.Complete {
		t.Fatal("complete right after asking to build again")
	}
	var kinds []struct {
		Kind   string `json:"kind"`
		Fields []any  `json:"fields"`
	}
	reader.must("GET", "/api/v1/data", nil, &kinds, 200)
	i := slices.IndexFunc(kinds, func(k struct {
		Kind   string `json:"kind"`
		Fields []any  `json:"fields"`
	}) bool {
		return k.Kind == "core.macros.decision"
	})
	if i < 0 || len(kinds[i].Fields) != 6 {
		t.Fatalf("kinds: %+v", kinds)
	}
}
