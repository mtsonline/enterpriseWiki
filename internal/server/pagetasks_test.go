package server_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestPageTasks: task items of pages with @name or a date are kept as data
// (one entry per person, not in blocks shown only to some readers, not
// checkboxes written as HTML); the report shows them; the people of a task
// with the permission for own tasks tick it, editors tick all tasks; a
// tick changes only that item, keeps the author and needs the revision
// that was shown.
func TestPageTasks(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var cat, other obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &cat, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Other"}}, &other, 201)
	users := map[string]obj{}
	for _, n := range []string{"anna", "bob", "cleo", "dora"} {
		var u obj
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": n, "password": "Passw0rd!Passw0rd"}, &u, 201)
		users[n] = u
	}
	acl := func(c obj, u string, body map[string]any) {
		body["subject_type"], body["subject_id"] = "user", users[u].ID
		admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), body, nil, 201)
	}
	// anna and bob read (with the permission for own tasks: preset reader),
	// cleo reads without it, dora edits
	acl(cat, "anna", map[string]any{"preset": "reader"})
	acl(cat, "bob", map[string]any{"preset": "reader"})
	acl(cat, "cleo", map[string]any{"permissions": 1})
	acl(cat, "dora", map[string]any{"preset": "editor"})
	login := func(n string) *client {
		c := e.client()
		c.login(n, "Passw0rd!Passw0rd")
		return c
	}
	anna, bob, cleo, dora := login("anna"), login("bob"), login("cleo"), login("dora")

	md := func(src string) map[string]any { return map[string]any{"format": "markdown", "source": src} }
	src := "Intro\n\n" +
		"<ul><li><input type=\"checkbox\"> raw HTML @anna</li></ul>\n\n" + // no task (would shift the numbers)
		"- [ ] Check the offer @anna :date{value=2026-11-30}\n" + // 1
		"- [ ] Together @anna @bob @cleo\n" + // 2
		"  - [x] Nested done @bob\n" + // 3
		"- [ ] Plain without anyone\n" + // 4: no data
		"- [ ] Unknown @nobody\n" + // 5: no data
		"- [ ] `@anna` in code, due :date{value=2026-01-02}\n\n" + // 6: a date, no person
		":::if{group=nobody}\n- [ ] Secret @anna\n:::\n\n" + // 7: hidden
		"::tasks{}\n"
	var page obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Plan", "body": md(src)}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": other.ID, "type": "core.html", "title": "Elsewhere", "body": md("- [ ] Hidden from anna @anna\n")}, nil, 201)

	type entry struct {
		ElementID int64          `json:"element_id"`
		Rev       int            `json:"rev"`
		Data      map[string]any `json:"data"`
	}
	type result struct {
		Items []entry `json:"items"`
		Total int     `json:"total"`
	}
	query := func(c *client, q string) result {
		var r result
		c.must("GET", "/api/v1/data/core.html.task?sort=page&limit=100"+q, nil, &r, 200)
		return r
	}
	items := func(r result) []string {
		var out []string
		for _, it := range r.Items {
			p, _ := it.Data["person"].(float64)
			name := ""
			for n, u := range users {
				if float64(u.ID) == p {
					name = n
				}
			}
			out = append(out, fmt.Sprintf("%v:%v:%s:%v", it.Data["item"], it.Data["state"], name, it.Data["day"]))
		}
		return out
	}
	all := items(query(admin, ""))
	want := []string{"1:open:anna:2026-11-30", "2:open:anna:<nil>", "2:open:bob:<nil>", "2:open:cleo:<nil>", "3:done:bob:<nil>", "6:open::2026-01-02"}
	if !slices.Equal(all[:len(all)-1], want) || all[len(all)-1] != "1:open:anna:<nil>" {
		t.Fatalf("entries: %v", all)
	}
	if r := query(anna, "&person=me&state=open"); len(r.Items) != 2 {
		t.Fatalf("anna's open tasks (not in the other category): %v", items(r))
	}
	r := query(admin, fmt.Sprintf("&element=%d", page.ID))
	if r.Items[0].Data["title"] != "Check the offer @anna" {
		t.Fatalf("title: %v", r.Items[0].Data["title"])
	}
	rev := r.Items[0].Rev

	// the report: anna may tick her tasks, not the others
	var v struct {
		HTML string `json:"html"`
	}
	anna.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", page.ID), nil, &v, 200)
	if strings.Count(v.HTML, `class="ew-task-tick"`) != 2 || !strings.Contains(v.HTML, "Overdue") || strings.Contains(v.HTML, "Secret") || strings.Contains(v.HTML, "Hidden from anna") {
		t.Fatalf("report for anna: %s", v.HTML)
	}
	dora.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", page.ID), nil, &v, 200)
	if strings.Count(v.HTML, `class="ew-task-tick"`) != 3 { // open tasks: 1, 2, 6
		t.Fatalf("report for the editor: %s", v.HTML)
	}

	tick := func(c *client, n, rev int, done bool, want int) {
		t.Helper()
		c.must("POST", fmt.Sprintf("/api/v1/x/core.html/tasks/%d/%d", page.ID, n), map[string]any{"lang": "en", "rev": rev, "done": done}, nil, want)
	}
	tick(e.client(), 1, rev, true, 401)
	tick(bob, 1, rev, true, 403)    // not his task
	tick(cleo, 2, rev, true, 403)   // her task, without the permission
	tick(anna, 6, rev, true, 403)   // nobody's task: only editors
	tick(anna, 7, rev, true, 404)   // hidden: not a task of the reports
	tick(anna, 9, rev, true, 404)   // no such task
	tick(anna, 1, rev-1, true, 409) // an older revision
	tick(anna, 1, rev, true, 200)
	var cur struct {
		Rev         int             `json:"rev"`
		Body        json.RawMessage `json:"body"`
		UpdatedByID int64           `json:"updated_by_id"`
		CreatedByID int64           `json:"created_by_id"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en&edit=1", page.ID), nil, &cur, 200)
	var body struct{ Source string }
	_ = json.Unmarshal(cur.Body, &body)
	if cur.Rev != rev+1 || body.Source != strings.Replace(src, "- [ ] Check the offer", "- [x] Check the offer", 1) {
		t.Fatalf("after the tick: rev %d, %q", cur.Rev, body.Source)
	}
	var who struct {
		User obj `json:"user"`
	}
	admin.must("GET", "/api/v1/auth/me", nil, &who, 200)
	if cur.CreatedByID != who.User.ID || cur.UpdatedByID != users["anna"].ID {
		t.Fatalf("created by %d, changed by %d", cur.CreatedByID, cur.UpdatedByID)
	}
	var revs []struct {
		Comment  string `json:"comment"`
		AuthorID int64  `json:"author_id"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d/revisions", page.ID), nil, &revs, 200)
	if len(revs) == 0 || !slices.ContainsFunc(revs, func(x struct {
		Comment  string `json:"comment"`
		AuthorID int64  `json:"author_id"`
	}) bool {
		return x.Comment == "Task done: Check the offer @anna" && x.AuthorID == users["anna"].ID
	}) {
		t.Fatalf("revisions: %+v", revs)
	}
	if r := query(anna, "&person=me&state=open"); len(r.Items) != 1 {
		t.Fatalf("anna's open tasks after the tick: %v", items(r))
	}
	// the same again changes nothing; bob ticks the task of all three
	tick(anna, 1, rev+1, true, 200)
	tick(anna, 1, rev, false, 409)
	tick(bob, 2, rev+1, true, 200)
	// the editor reopens any task, also one without people
	tick(dora, 2, rev+2, false, 200)
	tick(dora, 6, rev+3, true, 200)
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en&edit=1", page.ID), nil, &cur, 200)
	_ = json.Unmarshal(cur.Body, &body)
	wantSrc := strings.Replace(strings.Replace(src, "- [ ] Check the offer", "- [x] Check the offer", 1), "- [ ] `@anna` in code", "- [x] `@anna` in code", 1)
	if body.Source != wantSrc {
		t.Fatalf("source after the ticks: %q", body.Source)
	}
}
