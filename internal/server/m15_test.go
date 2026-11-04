package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// TestPermissionModel covers global permissions of roles, category
// administrators, managers and editors, presets, named actions of plugins
// (workflows) and the central overview.
func TestPermissionModel(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	pw := "Passw0rd!Passw0rd"
	role := func(name string, perms ...string) int64 {
		var r struct {
			ID int64 `json:"id"`
		}
		admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": name, "permissions": perms}, &r, 200)
		return r.ID
	}
	user := func(name string, roles ...int64) (*client, int64) {
		var u struct {
			ID int64 `json:"id"`
		}
		if roles == nil {
			roles = []int64{}
		}
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": pw, "display_name": name, "roles": roles}, &u, 201)
		c := e.client()
		c.login(name, pw)
		return c, u.ID
	}
	if code := admin.do("POST", "/api/v1/admin/roles", map[string]any{"name": "x", "permissions": []string{"core.nope"}}, nil); code != 400 {
		t.Fatalf("unknown permission accepted: %d", code)
	}
	usermgr := role("User managers", "core.users.manage")
	rolemgr := role("Role managers", "core.roles.manage", "core.users.manage")
	catadmin := role("Category admins", "core.categories.admin")
	var adminRole int64
	var roles []struct {
		ID      int64  `json:"id"`
		IsAdmin bool   `json:"is_admin"`
		Name    string `json:"name"`
	}
	admin.must("GET", "/api/v1/roles", nil, &roles, 200)
	for _, r := range roles {
		if r.IsAdmin {
			adminRole = r.ID
		}
	}

	// global permission: user management only
	bob, _ := user("bob", usermgr)
	bob.must("GET", "/api/v1/admin/users", nil, nil, 200)
	if code := bob.do("GET", "/api/v1/admin/settings", nil, nil); code != 403 {
		t.Fatalf("settings without permission: %d", code)
	}
	if code := bob.do("POST", "/api/v1/admin/roles", map[string]any{"name": "y"}, nil); code != 403 {
		t.Fatalf("roles without permission: %d", code)
	}
	if code := bob.do("POST", "/api/v1/admin/users", map[string]any{"username": "evil", "password": pw, "roles": []int64{adminRole}}, nil); code != 403 {
		t.Fatalf("user manager assigned the admin role: %d", code)
	}
	var adminUser []struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	admin.must("GET", "/api/v1/admin/users?q=admin", nil, &adminUser, 200)
	if code := bob.do("PUT", fmt.Sprintf("/api/v1/admin/users/%d", adminUser[0].ID), map[string]any{"password": "Other-Passw0rd!x"}, nil); code != 403 {
		t.Fatalf("user manager changed an administrator: %d", code)
	}
	// role manager cannot grant what they do not have
	carol, _ := user("carol", rolemgr)
	carol.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Helpers", "permissions": []string{"core.users.manage"}}, nil, 200)
	if code := carol.do("POST", "/api/v1/admin/roles", map[string]any{"name": "Too much", "permissions": []string{"core.settings.manage"}}, nil); code != 403 {
		t.Fatalf("role manager granted a permission they do not have: %d", code)
	}
	if code := carol.do("POST", "/api/v1/admin/roles", map[string]any{"name": "Admins 2", "is_admin": true}, nil); code != 403 {
		t.Fatalf("role manager created an admin role: %d", code)
	}
	// defaults of every signed-in user
	var me struct {
		User struct {
			Permissions []string `json:"permissions"`
		} `json:"user"`
	}
	bob.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if !slices.Contains(me.User.Permissions, "core.share.use") || !slices.Contains(me.User.Permissions, "core.users.manage") {
		t.Fatalf("permissions of bob: %v", me.User.Permissions)
	}

	// category administrator: top level and structure, no content
	dave, _ := user("dave", catadmin)
	var top, sub cat
	dave.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Faculty"}}, &top, 201)
	dave.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Institute"}}, &sub, 201)
	// the creator of a top-level category has all permissions on it; in other
	// categories a category admin manages the structure, but not the content
	var other, otherSub cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Administration"}}, &other, 201)
	dave.must("POST", "/api/v1/categories", map[string]any{"parent_id": other.ID, "titles": map[string]string{"en": "Finance"}}, &otherSub, 201)
	if code := dave.do("POST", "/api/v1/elements", map[string]any{"category_id": otherSub.ID, "type": "core.html", "title": "x", "body": htmlBody("<p>x</p>")}, nil); code != 403 {
		t.Fatalf("category admin created content: %d", code)
	}
	var acl struct {
		Own []struct {
			SubjectType string `json:"subject_type"`
			SubjectID   *int64 `json:"subject_id"`
		} `json:"own"`
	}
	dave.must("GET", fmt.Sprintf("/api/v1/categories/%d/acl", top.ID), nil, &acl, 200)
	if len(acl.Own) != 1 || acl.Own[0].SubjectType != "user" {
		t.Fatalf("rules of a new top-level category: %+v", acl.Own)
	}
	erin, erinID := user("erin")
	frank, frankID := user("frank")
	gina, ginaID := user("gina")
	harry, harryID := user("harry")
	grant := func(c *client, catID, uid int64, preset string) {
		c.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", catID), map[string]any{"subject_type": "user", "subject_id": uid, "preset": preset}, nil, 201)
	}
	grant(dave, top.ID, erinID, "manager")
	grant(dave, sub.ID, frankID, "editor")
	grant(dave, sub.ID, ginaID, "editor")
	grant(dave, sub.ID, harryID, "manager")

	// manager: subcategories, but not on the top level
	var sub2 cat
	erin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Office"}}, &sub2, 201)
	if code := erin.do("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Other faculty"}}, nil); code != 403 {
		t.Fatalf("manager created a top-level category: %d", code)
	}
	if code := erin.do("POST", fmt.Sprintf("/api/v1/categories/%d/move", sub2.ID), map[string]any{"parent_id": 0}, nil); code != 403 && code != 400 {
		t.Fatalf("manager moved a category to the top level: %d", code)
	}
	// editor: content, but not the category
	var page elem
	frank.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "Rules", "body": htmlBody("<p>x</p>")}, &page, 201)
	if code := frank.do("POST", "/api/v1/categories", map[string]any{"parent_id": sub.ID, "titles": map[string]string{"en": "Mine"}}, nil); code != 403 {
		t.Fatalf("editor created a subcategory: %d", code)
	}
	if code := frank.do("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"titles": map[string]string{"en": "Renamed"}}, nil); code != 403 {
		t.Fatalf("editor renamed the category: %d", code)
	}

	// workflows: the creator (editor preset) or a workflow administrator (manager preset)
	def := map[string]any{"description": "", "fields": []any{}, "steps": []map[string]any{
		{"id": "check", "name": "Check", "assignee": map[string]string{"type": "user", "value": "gina"}, "actions": []map[string]any{{"id": "ok", "label": "OK", "next": "done"}}},
		{"id": "done", "name": "Done", "actions": []any{}, "outcome": "success"}}}
	var wf elem
	frank.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "workflow.process", "title": "Request", "body": def}, &wf, 201)
	def2 := map[string]any{"description": "", "fields": []any{}, "steps": []map[string]any{
		{"id": "check", "name": "Check", "assignee": map[string]string{"type": "user", "value": "harry"}, "actions": []map[string]any{{"id": "ok", "label": "OK", "next": "done"}}},
		{"id": "done", "name": "Done", "actions": []any{}, "outcome": "success"}}}
	save := func(c *client, body any) int {
		return c.do("PUT", fmt.Sprintf("/api/v1/elements/%d", wf.ID), map[string]any{"lang": "en", "title": "Request", "body": body}, nil)
	}
	if code := save(gina, def2); code != 403 {
		t.Fatalf("another editor changed the workflow: %d", code)
	}
	if code := save(frank, def); code != 200 {
		t.Fatalf("creator changed the workflow: %d", code)
	}
	if code := save(harry, def2); code != 200 {
		t.Fatalf("workflow admin changed the workflow: %d", code)
	}
	var log []struct {
		Action string `json:"action"`
	}
	admin.must("GET", "/api/v1/admin/audit?action=workflow.admin_change", nil, &log, 200)
	found := false
	for _, l := range log {
		found = found || l.Action == "workflow.admin_change"
	}
	if !found {
		t.Fatalf("admin change of the workflow not logged: %+v", log)
	}
	// only the assignee carries out a step – not even administrators
	var x struct {
		ID      int64 `json:"id"`
		Actions []struct {
			ID string `json:"id"`
		} `json:"actions"`
	}
	frank.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{}}, &x, 201)
	if code := admin.do("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", x.ID), map[string]any{"action": "ok"}, nil); code == 200 {
		t.Fatal("administrator carried out a step assigned to someone else")
	}
	harry.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", x.ID), map[string]any{"action": "ok"}, nil, 200)

	// central overview: effective permissions with their origin
	var rows []struct {
		ID        int64    `json:"id"`
		Effective []string `json:"effective"`
		Sources   []struct {
			Inherited bool `json:"inherited"`
		} `json:"sources"`
	}
	dave.must("GET", fmt.Sprintf("/api/v1/permissions/overview?subject_type=user&subject_id=%d", frankID), nil, &rows, 200)
	var subRow *struct {
		ID        int64    `json:"id"`
		Effective []string `json:"effective"`
		Sources   []struct {
			Inherited bool `json:"inherited"`
		} `json:"sources"`
	}
	for i := range rows {
		if rows[i].ID == sub.ID {
			subRow = &rows[i]
		}
	}
	if subRow == nil || !slices.Contains(subRow.Effective, "edit") || !slices.Contains(subRow.Effective, "workflow.process.create") || slices.Contains(subRow.Effective, "manage") || len(subRow.Sources) != 1 {
		t.Fatalf("overview of frank: %+v", subRow)
	}
	// an editor does not see the overview of categories they cannot manage
	frank.must("GET", fmt.Sprintf("/api/v1/permissions/overview?subject_type=user&subject_id=%d", frankID), nil, &rows, 200)
	if len(rows) != 0 {
		t.Fatalf("editor sees categories in the overview: %+v", rows)
	}
	var catalog struct {
		Permissions []struct {
			Key   string `json:"key"`
			Scope string `json:"scope"`
		} `json:"permissions"`
	}
	frank.must("GET", "/api/v1/permissions", nil, &catalog, 200)
	keys := []string{}
	for _, p := range catalog.Permissions {
		keys = append(keys, p.Key)
	}
	for _, k := range []string{"core.categories.admin", "workflow.process.admin", "content.events.attendees", "core.comments.moderate", "core.menu.edit"} {
		if !slices.Contains(keys, k) {
			t.Fatalf("permission %s not declared: %v", k, keys)
		}
	}
}

func TestAuditLog(t *testing.T) {
	e := setup(t)
	if err := e.app.Config.Update(func(c *config.Config) error { c.Webhooks.AllowPrivate = true; return nil }); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	// failed and successful sign-ins with IP address and browser
	bad := e.client()
	if code := bad.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong"}, nil); code != 401 {
		t.Fatalf("wrong password: %d", code)
	}
	type res struct {
		Entries []struct {
			Action    string          `json:"action"`
			Actor     string          `json:"actor"`
			IP        string          `json:"ip"`
			UserAgent string          `json:"user_agent"`
			Via       string          `json:"via"`
			After     json.RawMessage `json:"after"`
		} `json:"entries"`
		Total int      `json:"total"`
		Areas []string `json:"areas"`
	}
	var r res
	admin.must("GET", "/api/v1/admin/audit/search?area=auth", nil, &r, 200)
	actions := map[string]bool{}
	for _, x := range r.Entries {
		actions[x.Action] = true
		if x.IP == "" {
			t.Errorf("no IP address in %s", x.Action)
		}
	}
	if !actions["auth.login"] || !actions["auth.login_failed"] {
		t.Fatalf("sign-ins not logged: %+v", r.Entries)
	}
	// text search and filters
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Zebra handbook"}}, &c, 201)
	admin.must("GET", "/api/v1/admin/audit/search?q=zebra", nil, &r, 200)
	if r.Total != 1 || r.Entries[0].Action != "category.create" || r.Entries[0].Via != "session" {
		t.Fatalf("text search: %+v", r)
	}
	admin.must("GET", "/api/v1/admin/audit/search?action=category.*&from=2000-01-01", nil, &r, 200)
	if r.Total < 1 {
		t.Fatal("action prefix filter")
	}
	// switch an area off (security areas stay on)
	recv := make(chan []byte, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Ewiki-Signature") == "" {
			w.WriteHeader(400)
			return
		}
		recv <- b
	}))
	t.Cleanup(srv.Close)
	admin.must("PUT", "/api/v1/admin/audit/settings", map[string]any{"disabled": []string{"category", "user"}, "log_logins": true, "log_denied": true,
		"forward": []map[string]any{{"id": "hook", "name": "SIEM", "type": "webhook", "address": srv.URL, "secret": "s3cret", "enabled": true},
			{"id": "file", "name": "File", "type": "file", "address": "audit.jsonl", "enabled": true}}}, nil, 204)
	var st struct {
		Settings struct {
			Disabled []string `json:"disabled"`
			Forward  []struct {
				Secret string `json:"secret"`
			} `json:"forward"`
		} `json:"settings"`
	}
	admin.must("GET", "/api/v1/admin/audit/settings", nil, &st, 200)
	if fmt.Sprint(st.Settings.Disabled) != "[category]" || st.Settings.Forward[0].Secret != "********" {
		t.Fatalf("settings: %+v", st.Settings)
	}
	if code := admin.do("PUT", "/api/v1/admin/audit/settings", map[string]any{"forward": []map[string]any{{"name": "x", "type": "file", "address": "../etc/passwd"}}}, nil); code != 400 {
		t.Fatalf("file outside the log folder accepted: %d", code)
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Yak handbook"}}, nil, 201)
	admin.must("GET", "/api/v1/admin/audit/search?q=yak", nil, &r, 200)
	if r.Total != 0 {
		t.Fatalf("disabled area logged: %+v", r)
	}
	// denied requests are logged
	bob := e.client()
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Passw0rd!Passw0rd"}, nil, 201)
	bob.login("bob", "Passw0rd!Passw0rd")
	bob.do("GET", "/api/v1/admin/settings", nil, nil)
	admin.must("GET", "/api/v1/admin/audit/search?action=access.denied", nil, &r, 200)
	if r.Total != 1 || r.Entries[0].Actor != "bob@local" {
		t.Fatalf("denied request not logged: %+v", r)
	}
	// forwarding: new entries reach the webhook and the file
	if err := e.app.Workers.RunOnce(context.Background(), "audit.forward"); err != nil {
		t.Fatal(err)
	}
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Forwarded role"}, nil, 200)
	if err := e.app.Workers.RunOnce(context.Background(), "audit.forward"); err != nil {
		t.Fatal(err)
	}
	select {
	case b := <-recv:
		if !strings.Contains(string(b), "role.save") || !strings.Contains(string(b), "Forwarded role") {
			t.Fatalf("webhook body: %s", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing forwarded to the webhook")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(e.app.Config.Path()), "logs", "audit.jsonl"))
	if err != nil || !strings.Contains(string(data), `"action":"role.save"`) {
		t.Fatalf("file destination: %v %s", err, data)
	}
	// export
	resp, err := http.Get(e.srv.URL + "/api/v1/admin/audit/export?format=csv")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("export without sign-in: %d", resp.StatusCode)
	}
}

func TestDraftsAndTemplates(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	pw := "Passw0rd!Passw0rd"
	user := func(name string) (*client, int64) {
		var u struct {
			ID int64 `json:"id"`
		}
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": pw, "email": name + "@example.com"}, &u, 201)
		c := e.client()
		c.login(name, pw)
		return c, u.ID
	}
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "News"}}, &c, 201)
	frank, frankID := user("frank")
	gina, ginaID := user("gina")
	rita, ritaID := user("rita")
	for _, uid := range []int64{frankID, ginaID} {
		admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": uid, "preset": "editor"}, nil, 201)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": ritaID, "preset": "reader"}, nil, 201)

	// a new element as draft: invisible for readers, editable together
	var d struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		UpdatedBy string `json:"updated_by"`
	}
	frank.must("POST", "/api/v1/drafts", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Draft news", "body": htmlBody("<p>secret plan</p>")}, &d, 201)
	if code := rita.do("GET", fmt.Sprintf("/api/v1/drafts/%d", d.ID), nil, nil); code != 404 {
		t.Fatalf("reader sees a draft: %d", code)
	}
	gina.must("PUT", fmt.Sprintf("/api/v1/drafts/%d", d.ID), map[string]any{"title": "Draft news (edited)", "body": htmlBody("<p>plan</p>")}, &d, 200)
	var list []struct {
		ID int64 `json:"id"`
	}
	frank.must("GET", fmt.Sprintf("/api/v1/drafts?category=%d", c.ID), nil, &list, 200)
	rita.must("GET", fmt.Sprintf("/api/v1/drafts?category=%d", c.ID), nil, &list, 200)
	if len(list) != 0 {
		t.Fatalf("reader lists drafts: %+v", list)
	}
	var els struct {
		Total int `json:"total"`
	}
	rita.must("GET", fmt.Sprintf("/api/v1/elements?category=%d", c.ID), nil, &els, 200)
	if els.Total != 0 {
		t.Fatalf("draft listed as element: %+v", els)
	}
	// publish: the element appears, the draft is gone
	var pub struct {
		Element struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			HTML  string `json:"html"`
		} `json:"element"`
	}
	frank.must("POST", fmt.Sprintf("/api/v1/drafts/%d/publish", d.ID), map[string]any{}, &pub, 200)
	if pub.Element.Title != "Draft news (edited)" || !strings.Contains(pub.Element.HTML, "plan") {
		t.Fatalf("published: %+v", pub)
	}
	if code := frank.do("GET", fmt.Sprintf("/api/v1/drafts/%d", d.ID), nil, nil); code != 404 {
		t.Fatalf("draft still there: %d", code)
	}
	elID := pub.Element.ID
	// changes as draft: readers keep seeing the published version
	rita.must("PUT", "/api/v1/me/watches", map[string]any{"target_type": "element", "target_id": elID}, nil, 200)
	frank.must("POST", "/api/v1/drafts", map[string]any{"element_id": elID, "lang": "en", "title": "Draft news", "body": htmlBody("<p>new version</p>"), "base_rev": 1}, &d, 201)
	var el elem
	rita.must("GET", fmt.Sprintf("/api/v1/elements/%d", elID), nil, &el, 200)
	if strings.Contains(el.HTML, "new version") {
		t.Fatal("reader sees the draft")
	}
	// scheduled publication, without notifying the watchers
	at := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	var sch struct {
		Scheduled *time.Time `json:"scheduled"`
	}
	frank.must("POST", fmt.Sprintf("/api/v1/drafts/%d/publish", d.ID), map[string]any{"at": at, "notify": false}, &sch, 200)
	if sch.Scheduled == nil {
		t.Fatal("not scheduled")
	}
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE element_drafts SET publish_at = now() - interval '1 minute' WHERE id=$1`, d.ID); err != nil {
		t.Fatal(err)
	}
	var before int
	_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM watch_events WHERE user_id=$1`, ritaID).Scan(&before)
	if err := e.app.Workers.RunOnce(context.Background(), "drafts.publish"); err != nil {
		t.Fatal(err)
	}
	rita.must("GET", fmt.Sprintf("/api/v1/elements/%d", elID), nil, &el, 200)
	if !strings.Contains(el.HTML, "new version") || el.Rev != 2 {
		t.Fatalf("scheduled draft not published: rev %d %s", el.Rev, el.HTML)
	}
	var after int
	_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM watch_events WHERE user_id=$1`, ritaID).Scan(&after)
	if after != before {
		t.Fatal("watchers notified although notify=false")
	}

	// templates: personal ones, global ones only with permission
	var tpl struct {
		ID     int64 `json:"id"`
		Global bool  `json:"global"`
	}
	frank.must("POST", "/api/v1/templates", map[string]any{"name": "Meeting notes", "type": "core.html", "title": "Meeting", "body": htmlBody("<h2>Agenda</h2>")}, &tpl, 201)
	var tpls []struct {
		ID   int64           `json:"id"`
		Body json.RawMessage `json:"body"`
	}
	gina.must("GET", "/api/v1/templates", nil, &tpls, 200)
	if len(tpls) != 0 {
		t.Fatalf("personal template visible to others: %+v", tpls)
	}
	if code := frank.do("POST", "/api/v1/templates", map[string]any{"name": "For all", "type": "core.html", "body": htmlBody("x"), "global": true}, nil); code != 403 {
		t.Fatalf("global template without permission: %d", code)
	}
	admin.must("POST", "/api/v1/templates", map[string]any{"name": "Official", "type": "core.html", "body": htmlBody("<p>logo</p>"), "global": true}, &tpl, 201)
	gina.must("GET", "/api/v1/templates?type=core.html", nil, &tpls, 200)
	if len(tpls) != 1 {
		t.Fatalf("global template: %+v", tpls)
	}
	gina.must("GET", fmt.Sprintf("/api/v1/templates/%d", tpl.ID), nil, &tpls[0], 200)
	if !strings.Contains(string(tpls[0].Body), "logo") {
		t.Fatalf("template body: %s", tpls[0].Body)
	}
	if code := gina.do("DELETE", fmt.Sprintf("/api/v1/templates/%d", tpl.ID), nil, nil); code != 403 {
		t.Fatalf("deleted a global template without permission: %d", code)
	}
}

func TestNotificationsShareMentionsAssignedWatches(t *testing.T) {
	e := setup(t)
	mc := &capture{}
	e.app.Mail.SetTransport(mc)
	if err := e.app.Config.Update(func(cfg *config.Config) error { cfg.Mail.From = "wiki@example.com"; return nil }); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	pw := "Passw0rd!Passw0rd"
	user := func(name string) (*client, int64) {
		var u struct {
			ID int64 `json:"id"`
		}
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": pw, "display_name": strings.ToUpper(name[:1]) + name[1:], "email": name + "@example.com"}, &u, 201)
		c := e.client()
		c.login(name, pw)
		return c, u.ID
	}
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &c, 201)
	anna, annaID := user("anna")
	ben, benID := user("ben")
	_, carlID := user("carl") // no access
	var grp struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Staff"}, &grp, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", benID), map[string]any{"groups": []int64{grp.ID}}, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", carlID), map[string]any{"groups": []int64{grp.ID}}, nil, 200)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": annaID, "preset": "editor"}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": benID, "preset": "reader"}, nil, 201)
	var page elem
	anna.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Plan", "body": htmlBody("<p>Hello</p>")}, &page, 201)

	// share with a group: carl may not see the page and is skipped
	var res struct {
		Recipients    int      `json:"recipients"`
		WithoutAccess []string `json:"without_access"`
	}
	anna.must("POST", "/api/v1/x/core.share/check", map[string]any{"target_type": "element", "target_id": page.ID, "groups": []int64{grp.ID}}, &res, 200)
	if res.Recipients != 1 || len(res.WithoutAccess) != 1 || res.WithoutAccess[0] != "Carl" {
		t.Fatalf("check: %+v", res)
	}
	anna.must("POST", "/api/v1/x/core.share/share", map[string]any{"target_type": "element", "target_id": page.ID, "groups": []int64{grp.ID}, "message": "Please read"}, &res, 200)
	var inbox struct {
		Items []struct {
			ID    int64  `json:"id"`
			Kind  string `json:"kind"`
			Title string `json:"title"`
			Link  string `json:"link"`
		} `json:"items"`
		Unread int `json:"unread"`
	}
	ben.must("GET", "/api/v1/me/inbox", nil, &inbox, 200)
	if inbox.Unread != 1 || inbox.Items[0].Kind != "share" || inbox.Items[0].Link != fmt.Sprintf("/e/%d", page.ID) {
		t.Fatalf("ben's notifications: %+v", inbox)
	}
	if err := e.app.Workers.RunOnce(context.Background(), "mail"); err != nil {
		t.Fatal(err)
	}
	if !mc.find("ben@example.com", "Please read") || mc.find("carl@example.com", "Plan") {
		t.Fatal("share e-mail")
	}
	ben.must("POST", "/api/v1/me/inbox/read", map[string]any{}, nil, 204)
	ben.must("GET", "/api/v1/me/inbox", nil, &inbox, 200)
	if inbox.Unread != 0 {
		t.Fatal("not marked as read")
	}

	// mentions: once per page, only people with access, e-mail can be switched off
	ben.must("PUT", "/api/v1/me/notifications", map[string]bool{"mention": false}, nil, 204)
	anna.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Plan", "body": htmlBody("<p>Hello @ben and @carl, see you</p>")}, nil, 200)
	anna.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Plan", "body": htmlBody("<p>Hello @ben and @carl, see you!</p>")}, nil, 200)
	ben.must("GET", "/api/v1/me/inbox", nil, &inbox, 200)
	mentions := 0
	for _, n := range inbox.Items {
		if n.Kind == "mention" {
			mentions++
		}
	}
	if mentions != 1 {
		t.Fatalf("mentions of ben: %+v", inbox.Items)
	}
	var carlN int
	_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE user_id=$1 AND kind='mention'`, carlID).Scan(&carlN)
	if carlN != 0 {
		t.Fatal("mentioned person without access was notified")
	}
	if err := e.app.Workers.RunOnce(context.Background(), "mail"); err != nil {
		t.Fatal(err)
	}
	if mc.find("ben@example.com", "mentioned") {
		t.Fatal("mention e-mail although switched off")
	}
	// mention in a comment
	ben.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", page.ID), map[string]any{"body": "Thanks @anna!"}, nil, 201)
	anna.must("GET", "/api/v1/me/inbox", nil, &inbox, 200)
	if len(inbox.Items) == 0 || inbox.Items[0].Kind != "mention" {
		t.Fatalf("comment mention: %+v", inbox.Items)
	}

	// assigned watches: for a group, optional ones can be unsubscribed
	if code := anna.do("POST", "/api/v1/admin/watch-assignments", map[string]any{"subject_type": "group", "subject_id": grp.ID, "target_type": "category", "target_id": c.ID}, nil); code != 403 {
		t.Fatalf("assignment without permission: %d", code)
	}
	var as struct {
		ID      int64 `json:"id"`
		Members int   `json:"members"`
	}
	admin.must("POST", "/api/v1/admin/watch-assignments", map[string]any{"subject_type": "group", "subject_id": grp.ID, "target_type": "category", "target_id": c.ID, "mandatory": false}, &as, 201)
	if as.Members != 2 {
		t.Fatalf("assignment members: %+v", as)
	}
	var mine []struct {
		ID        int64 `json:"id"`
		Mandatory bool  `json:"mandatory"`
	}
	ben.must("GET", "/api/v1/me/watch-assignments", nil, &mine, 200)
	if len(mine) != 1 {
		t.Fatalf("ben's assignments: %+v", mine)
	}
	countEvents := func(uid int64) int {
		var n int
		_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM watch_events WHERE user_id=$1`, uid).Scan(&n)
		return n
	}
	anna.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Plan 2", "body": htmlBody("<p>v3</p>")}, nil, 200)
	if countEvents(benID) == 0 {
		t.Fatal("assigned watch not effective")
	}
	ben.must("PUT", fmt.Sprintf("/api/v1/me/watch-assignments/%d", as.ID), map[string]any{"subscribed": false}, nil, 204)
	_, _ = e.app.DB.Exec(context.Background(), `DELETE FROM watch_events`)
	anna.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Plan 3", "body": htmlBody("<p>v4</p>")}, nil, 200)
	if countEvents(benID) != 0 {
		t.Fatal("unsubscribed optional watch still effective")
	}
	// mandatory: cannot be unsubscribed
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/watch-assignments/%d", as.ID), map[string]any{"subject_type": "group", "subject_id": grp.ID, "target_type": "category", "target_id": c.ID, "mandatory": true}, nil, 200)
	anna.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Plan 4", "body": htmlBody("<p>v5</p>")}, nil, 200)
	if countEvents(benID) == 0 {
		t.Fatal("mandatory watch not effective")
	}
	if code := ben.do("PUT", fmt.Sprintf("/api/v1/me/watch-assignments/%d", as.ID), map[string]any{"subscribed": false}, nil); code != 403 {
		t.Fatalf("unsubscribed from a mandatory watch: %d", code)
	}
}
