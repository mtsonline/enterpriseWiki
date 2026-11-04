package server_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

type idResp struct {
	ID int64 `json:"id"`
}

func TestMacros(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var macros []struct {
		Name    string `json:"name"`
		Plugin  string `json:"plugin"`
		HasBody bool   `json:"has_body"`
	}
	admin.must("GET", "/api/v1/macros", nil, &macros, 200)
	names := map[string]bool{}
	for _, m := range macros {
		names[m.Name] = true
	}
	for _, n := range []string{"toc", "children", "recent", "panel", "expand", "status", "columns", "section", "hero", "cards", "button", "spacer", "shell", "http"} {
		if !names[n] {
			t.Errorf("macro %s missing", n)
		}
	}
	var root, child cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Handbook"}}, &root, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": root.ID, "titles": map[string]string{"en": "Chapter <1>"}}, &child, 201)
	src := `<div data-macro="toc"></div><h2>Intro</h2><p>x</p><h2>Intro</h2>` +
		`<div data-macro="panel" data-params='{"type":"warning","title":"Careful <b>"}'><p>Hot <script>alert(1)</script></p>` +
		`<div data-macro="status" data-params='{"text":"done","color":"green"}'></div></div>` +
		`<div data-macro="children"></div><div data-macro="nope"></div>` +
		`<div data-macro="hero" data-params='{"title":"Welcome","button":"Go","link":"javascript:alert(1)"}'></div>`
	var el elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Start", "body": htmlBody(src)}, &el, 201)
	for _, want := range []string{
		`<a href="#intro">Intro</a>`, `<a href="#intro-2">Intro</a>`, `id="intro-2"`,
		`ew-macro-panel--warning`, `Careful &lt;b&gt;`, `ew-status--green">DONE`,
		fmt.Sprintf(`href="/c/%d">Chapter &lt;1&gt;`, child.ID), `macro &#34;nope&#34; is not available`,
		`ewl-hero`, `href="#">Go`,
	} {
		if !strings.Contains(el.HTML, want) {
			t.Errorf("rendered page lacks %q:\n%s", want, el.HTML)
		}
	}
	if strings.Contains(el.HTML, "<script") || strings.Contains(el.HTML, "javascript:") {
		t.Fatalf("unsafe output: %s", el.HTML)
	}
	// the hidden child category is not listed for a user without access
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": false}, nil, 201)
	var bob idResp
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd!x", "display_name": "Bob"}, &bob, 201)
	bobc := e.client()
	bobc.login("bob", "Bob-Passw0rd!x")
	var bel elem
	bobc.must("GET", fmt.Sprintf("/api/v1/elements/%d", el.ID), nil, &bel, 200)
	if strings.Contains(bel.HTML, "Chapter") {
		t.Fatalf("children macro leaks hidden category: %s", bel.HTML)
	}
	// disabling the plugin disables its macros
	admin.must("POST", "/api/v1/admin/plugins/core.layout/disable", nil, nil, 204)
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", el.ID), nil, &el, 200)
	if !strings.Contains(el.HTML, `macro &#34;hero&#34; is not available`) {
		t.Fatalf("disabled macro still rendered: %s", el.HTML)
	}
	// macro security settings cannot be changed through the API
	if got := admin.do("PUT", "/api/v1/admin/settings", map[string]any{"macros": map[string]any{"shell_enabled": true}}, nil); got != 400 {
		t.Fatalf("shell switched on through the API: %d", got)
	}
}

func TestShellMacro(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell")
	}
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var root cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Ops"}}, &root, 201)
	body := htmlBody(`<div data-macro="shell" data-params='{"command":"echo \"<b>hello</b> $((40+2))\""}'></div>`)
	var el elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Status", "body": body}, &el, 201)
	if !strings.Contains(el.HTML, "disabled on this server") {
		t.Fatalf("shell ran while disabled: %s", el.HTML)
	}
	if err := e.app.Config.Update(func(c *config.Config) error { c.Plugins.Macros.ShellEnabled = true; return nil }); err != nil {
		t.Fatal(err)
	}
	get := func() string {
		var x elem
		admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", el.ID), nil, &x, 200)
		return x.HTML
	}
	if h := get(); !strings.Contains(h, "waiting for approval") {
		t.Fatalf("unapproved command: %s", h)
	}
	var list struct {
		Commands []struct {
			Hash    string `json:"hash"`
			Command string `json:"command"`
			Status  string `json:"status"`
		} `json:"commands"`
		CanApprove bool `json:"can_approve"`
	}
	admin.must("GET", "/api/v1/x/macro.shell/commands", nil, &list, 200)
	if len(list.Commands) != 1 || list.Commands[0].Status != "pending" || list.CanApprove {
		t.Fatalf("commands %+v", list)
	}
	hash := list.Commands[0].Hash
	// an admin who is not listed as approver cannot approve
	if got := admin.do("POST", "/api/v1/x/macro.shell/commands/"+hash+"/approve", nil, nil); got != 403 {
		t.Fatalf("approve without being superadmin: %d", got)
	}
	if err := e.app.Config.Update(func(c *config.Config) error { c.Plugins.Macros.ShellApprovers = []string{"admin@local"}; return nil }); err != nil {
		t.Fatal(err)
	}
	admin.must("POST", "/api/v1/x/macro.shell/commands/"+hash+"/approve", nil, nil, 204)
	if h := get(); !strings.Contains(h, "&lt;b&gt;hello&lt;/b&gt; 42") {
		t.Fatalf("approved command output: %s", h)
	}
	// changing the command needs a new approval
	body = htmlBody(`<div data-macro="shell" data-params='{"command":"echo changed"}'></div>`)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", el.ID), map[string]any{"lang": "en", "title": "Status", "body": body}, nil, 200)
	if h := get(); !strings.Contains(h, "waiting for approval") {
		t.Fatalf("changed command ran without approval: %s", h)
	}
	admin.must("POST", "/api/v1/x/macro.shell/commands/"+hash+"/revoke", nil, nil, 204)
	admin.must("DELETE", "/api/v1/x/macro.shell/commands/"+hash, nil, nil, 204)
	var created struct {
		Hash string `json:"hash"`
	}
	admin.must("POST", "/api/v1/x/macro.shell/commands", map[string]any{"command": "echo pre", "approve": true}, &created, 200)
	if len(created.Hash) != 64 {
		t.Fatalf("register command: %+v", created)
	}
}

func TestHTTPMacro(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/users":
			w.Write([]byte(`{"data":{"users":[{"name":"Ann","age":30},{"name":"<Bob>","age":41}]}}`))
		case "/redirect":
			http.Redirect(w, r, "http://example.invalid/", http.StatusFound)
		default:
			w.Write([]byte("plain <text>"))
		}
	}))
	defer upstream.Close()
	var root cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Data"}}, &root, 201)
	page := func(params string) string {
		var el elem
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "x",
			"body": htmlBody(`<div data-macro="http" data-params='` + params + `'></div>`)}, &el, 201)
		return el.HTML
	}
	if h := page(`{"url":"` + upstream.URL + `/x"}`); !strings.Contains(h, "not on the allow list") {
		t.Fatalf("not allowed URL fetched: %s", h)
	}
	if err := e.app.Config.Update(func(c *config.Config) error { c.Plugins.Macros.HTTPAllow = []string{upstream.URL + "/"}; return nil }); err != nil {
		t.Fatal(err)
	}
	if h := page(`{"url":"` + upstream.URL + `/x"}`); !strings.Contains(h, "internal") {
		t.Fatalf("loopback address fetched: %s", h)
	}
	if err := e.app.Config.Update(func(c *config.Config) error { c.Plugins.Macros.HTTPAllowPrivate = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if h := page(`{"url":"` + upstream.URL + `/x","cache":"0"}`); !strings.Contains(h, "plain &lt;text&gt;") {
		t.Fatalf("text: %s", h)
	}
	if h := page(`{"url":"` + upstream.URL + `/api/users","format":"json","path":"data.users"}`); !strings.Contains(h, "<th scope=\"col\">name</th>") || !strings.Contains(h, "&lt;Bob&gt;") {
		t.Fatalf("json table: %s", h)
	}
	if h := page(`{"url":"` + upstream.URL + `/redirect"}`); !strings.Contains(h, "not on the allow list") {
		t.Fatalf("redirect outside the allow list followed: %s", h)
	}
}

func TestComments(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var root cat
	admin.must("POST", "/api/v1/categories", map[string]any{"guest_visible": true, "titles": map[string]string{"en": "Team"}}, &root, 201)
	var el elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Plan", "body": htmlBody("<p>x</p>")}, &el, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	var bob idResp
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd!x", "display_name": "Bob"}, &bob, 201)
	bobc := e.client()
	bobc.login("bob", "Bob-Passw0rd!x")
	guest := e.client()
	base := fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", el.ID)
	// guests may read the first level (default guest right), but not comment
	var gl struct {
		CanComment bool `json:"can_comment"`
	}
	guest.must("GET", base, nil, &gl, 200)
	if gl.CanComment {
		t.Fatal("guest may comment")
	}
	if got := guest.do("POST", base, map[string]any{"body": "spam"}, nil); got != 401 {
		t.Fatalf("guest comments: %d", got)
	}
	type comment struct {
		ID        int64  `json:"id"`
		ParentID  *int64 `json:"parent_id"`
		HTML      string `json:"html"`
		Body      string `json:"body"`
		CanEdit   bool   `json:"can_edit"`
		CanDelete bool   `json:"can_delete"`
		DeletedAt any    `json:"deleted_at"`
	}
	var c1, c2, c3 comment
	bobc.must("POST", base, map[string]any{"body": "Looks good <script>x</script>\nsee https://example.com/a?b=1&c=2."}, &c1, 201)
	if !strings.Contains(c1.HTML, "&lt;script&gt;") || !strings.Contains(c1.HTML, `<a href="https://example.com/a?b=1&amp;c=2"`) || !strings.Contains(c1.HTML, "<br>") {
		t.Fatalf("comment html: %s", c1.HTML)
	}
	admin.must("POST", base, map[string]any{"body": "Thanks", "parent_id": c1.ID}, &c2, 201)
	bobc.must("POST", base, map[string]any{"body": "Reply to reply", "parent_id": c2.ID}, &c3, 201)
	if c3.ParentID == nil || *c3.ParentID != c1.ID {
		t.Fatalf("reply not attached to thread: %+v", c3)
	}
	if got := bobc.do("PUT", fmt.Sprintf("/api/v1/x/core.comments/comments/%d", c2.ID), map[string]any{"body": "hijack"}, nil); got != 403 {
		t.Fatalf("bob edits admin's comment: %d", got)
	}
	if got := bobc.do("DELETE", fmt.Sprintf("/api/v1/x/core.comments/comments/%d", c2.ID), nil, nil); got != 403 {
		t.Fatalf("bob deletes admin's comment: %d", got)
	}
	bobc.must("PUT", fmt.Sprintf("/api/v1/x/core.comments/comments/%d", c1.ID), map[string]any{"body": "Edited"}, nil, 200)
	admin.must("DELETE", fmt.Sprintf("/api/v1/x/core.comments/comments/%d", c1.ID), nil, nil, 204)
	var list struct {
		Comments   []comment `json:"comments"`
		CanComment bool      `json:"can_comment"`
	}
	bobc.must("GET", base, nil, &list, 200)
	if len(list.Comments) != 3 || list.Comments[0].DeletedAt == nil || list.Comments[0].Body != "" || !list.CanComment {
		t.Fatalf("list after delete: %+v", list)
	}
	var audit []struct {
		Action string `json:"action"`
	}
	admin.must("GET", "/api/v1/admin/audit?entity_type=comment", nil, &audit, 200)
	if len(audit) < 5 {
		t.Fatalf("comment changes not audited: %+v", audit)
	}
	// stricter setting: commenting needs Edit permission
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Settings = map[string]map[string]any{"core.comments": {"write_permission": "edit"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := bobc.do("POST", base, map[string]any{"body": "x"}, nil); got != 403 {
		t.Fatalf("view-only user comments with write_permission=edit: %d", got)
	}
}

func TestWorkflow(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var root cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Jobs"}}, &root, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	var hr idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "hr"}, &hr, 200)
	var ann, bob, carl idResp
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ann", "password": "Ann-Passw0rd!x", "display_name": "Ann", "groups": []int64{hr.ID}}, &ann, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd!x", "display_name": "Bob"}, &bob, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "carl", "password": "Carl-Passw0rd!x", "display_name": "Carl"}, &carl, 201)
	annc, bobc, carlc := e.client(), e.client(), e.client()
	annc.login("ann", "Ann-Passw0rd!x")
	bobc.login("bob", "Bob-Passw0rd!x")
	carlc.login("carl", "Carl-Passw0rd!x")

	def := map[string]any{
		"description": "Apply here.\n\nWe answer quickly.",
		"fields": []map[string]any{
			{"id": "name", "label": "Name", "type": "text", "required": true},
			{"id": "email", "label": "E-mail", "type": "email", "required": true},
			{"id": "position", "label": "Position", "type": "select", "options": []string{"Dev", "Ops"}, "required": true},
			{"id": "ok", "label": "Privacy", "type": "checkbox", "required": true},
		},
		"steps": []map[string]any{
			{"id": "screening", "name": "Screening", "assignee": map[string]string{"type": "group", "value": "hr"},
				"actions": []map[string]any{{"id": "accept", "label": "Accept", "next": "hired"}, {"id": "reject", "label": "Reject", "next": "rejected", "note": "required"}}},
			{"id": "hired", "name": "Hired", "actions": []any{}, "outcome": "success"},
			{"id": "rejected", "name": "Rejected", "actions": []any{}, "outcome": "failure"},
		},
	}
	bad := map[string]any{"description": "", "fields": []any{}, "steps": []map[string]any{{"id": "a", "name": "A", "assignee": map[string]string{"type": "group", "value": "hr"},
		"actions": []map[string]any{{"id": "x", "label": "X", "next": "missing"}}}}}
	if got := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "workflow.process", "title": "Bad", "body": bad}, nil); got != 400 {
		t.Fatalf("invalid workflow accepted: %d", got)
	}
	var el elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "workflow.process", "title": "Application", "body": def}, &el, 201)
	if !strings.Contains(el.HTML, fmt.Sprintf(`data-ew-widget="workflow" data-element="%d"`, el.ID)) || !strings.Contains(el.HTML, "<p>We answer quickly.</p>") {
		t.Fatalf("workflow html: %s", el.HTML)
	}
	base := "/api/v1/x/workflow.process"
	type inst struct {
		ID      int64  `json:"id"`
		Title   string `json:"title"`
		Step    string `json:"step"`
		Status  string `json:"status"`
		Outcome string `json:"outcome"`
		Actions []struct {
			ID string `json:"id"`
		} `json:"actions"`
		CanCancel bool `json:"can_cancel"`
		Events    []struct {
			Action string `json:"action"`
			Note   string `json:"note"`
		} `json:"events"`
	}
	if got := bobc.do("POST", fmt.Sprintf("%s/elements/%d/instances", base, el.ID), map[string]any{"data": map[string]any{"name": "Bob", "email": "nope", "position": "Dev", "ok": true}}, nil); got != 400 {
		t.Fatalf("invalid e-mail accepted: %d", got)
	}
	if got := bobc.do("POST", fmt.Sprintf("%s/elements/%d/instances", base, el.ID), map[string]any{"data": map[string]any{"name": "Bob", "email": "bob@example.com", "position": "Dev"}}, nil); got != 400 {
		t.Fatalf("missing confirmation accepted: %d", got)
	}
	var c1, c2 inst
	bobc.must("POST", fmt.Sprintf("%s/elements/%d/instances", base, el.ID), map[string]any{"data": map[string]any{"name": "Bob Builder", "email": "bob@example.com", "position": "Dev", "ok": true, "extra": "dropped"}}, &c1, 201)
	if c1.Title != "Bob Builder" || c1.Step != "screening" || c1.Status != "open" || len(c1.Actions) != 0 || !c1.CanCancel {
		t.Fatalf("started case: %+v", c1)
	}
	bobc.must("POST", fmt.Sprintf("%s/elements/%d/instances", base, el.ID), map[string]any{"data": map[string]any{"name": "Second", "email": "b2@example.com", "position": "Ops", "ok": true}}, &c2, 201)
	// carl (no relation) sees nothing; ann (hr) sees both and may act
	var list struct {
		Instances []inst `json:"instances"`
		CanStart  bool   `json:"can_start"`
	}
	carlc.must("GET", fmt.Sprintf("%s/elements/%d/instances", base, el.ID), nil, &list, 200)
	if len(list.Instances) != 0 || !list.CanStart {
		t.Fatalf("carl sees cases: %+v", list)
	}
	if got := carlc.do("GET", fmt.Sprintf("%s/instances/%d", base, c1.ID), nil, nil); got != 404 {
		t.Fatalf("carl opens a case: %d", got)
	}
	var tasks struct {
		Tasks   []inst `json:"tasks"`
		Started []inst `json:"started"`
	}
	annc.must("GET", base+"/tasks", nil, &tasks, 200)
	if len(tasks.Tasks) != 2 {
		t.Fatalf("ann's tasks: %+v", tasks)
	}
	bobc.must("GET", base+"/tasks", nil, &tasks, 200)
	if len(tasks.Tasks) != 0 || len(tasks.Started) != 2 {
		t.Fatalf("bob's tasks: %+v", tasks)
	}
	if got := bobc.do("POST", fmt.Sprintf("%s/instances/%d/actions", base, c1.ID), map[string]any{"action": "accept"}, nil); got != 403 {
		t.Fatalf("applicant accepts himself: %d", got)
	}
	if got := annc.do("POST", fmt.Sprintf("%s/instances/%d/actions", base, c1.ID), map[string]any{"action": "reject"}, nil); got != 400 {
		t.Fatalf("reject without note: %d", got)
	}
	var done inst
	annc.must("POST", fmt.Sprintf("%s/instances/%d/actions", base, c1.ID), map[string]any{"action": "reject", "note": "No vacancy"}, &done, 200)
	if done.Status != "done" || done.Step != "rejected" || done.Outcome != "failure" || len(done.Events) != 2 || done.Events[1].Note != "No vacancy" {
		t.Fatalf("after reject: %+v", done)
	}
	if got := annc.do("POST", fmt.Sprintf("%s/instances/%d/actions", base, c1.ID), map[string]any{"action": "accept"}, nil); got != 409 {
		t.Fatalf("action on closed case: %d", got)
	}
	// the applicant cancels the second case
	if got := annc.do("POST", fmt.Sprintf("%s/instances/%d/cancel", base, c2.ID), map[string]any{}, nil); got != 403 {
		t.Fatalf("hr cancels: %d", got)
	}
	bobc.must("POST", fmt.Sprintf("%s/instances/%d/cancel", base, c2.ID), map[string]any{"note": "found another job"}, &done, 200)
	if done.Status != "cancelled" {
		t.Fatalf("cancel: %+v", done)
	}
	// editing the process does not change running cases
	def["steps"].([]map[string]any)[0]["name"] = "Renamed"
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", el.ID), map[string]any{"lang": "en", "title": "Application", "body": def}, nil, 200)
	var detail struct {
		Definition struct {
			Steps []struct {
				Name string `json:"name"`
			} `json:"steps"`
		} `json:"definition"`
	}
	bobc.must("GET", fmt.Sprintf("%s/instances/%d", base, c1.ID), nil, &detail, 200)
	if detail.Definition.Steps[0].Name != "Screening" {
		t.Fatalf("snapshot changed: %+v", detail)
	}
}

func TestUpdateCheck(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/mtsonline/enterpriseWiki/releases/latest":
			w.Write([]byte(`{"tag_name":"v99.0.0","html_url":"https://github.com/mtsonline/enterpriseWiki/releases/tag/v99.0.0","body":"Big release"}`))
		case "/repos/acme/ewiki-plugin-foo/releases/latest":
			http.NotFound(w, r)
		case "/repos/acme/ewiki-plugin-foo/tags":
			w.Write([]byte(`[{"name":"v1.2.0"},{"name":"v1.10.0"},{"name":"nightly"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Plugins.Updates.APIURL = gh.URL
		c.Plugins.Catalog = []config.CatalogEntry{{ID: "example.notice", Name: "Notice", Repo: "github.com/acme/ewiki-plugin-foo"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Items []struct {
			ID        string `json:"id"`
			Latest    string `json:"latest"`
			Available bool   `json:"available"`
			Error     string `json:"error"`
		} `json:"items"`
	}
	admin.must("GET", "/api/v1/admin/updates?force=1", nil, &rep, 200)
	got := map[string]string{}
	for _, it := range rep.Items {
		got[it.ID] = fmt.Sprintf("%s/%v/%s", it.Latest, it.Available, it.Error)
	}
	if got["core"] != "99.0.0/true/" || got["example.notice"] != "1.10.0/true/" {
		t.Fatalf("update report: %+v", got)
	}
	if code := admin.do("POST", "/api/v1/admin/updates/core", nil, nil); code != 400 {
		t.Fatalf("core update through the API: %d", code)
	}
}
