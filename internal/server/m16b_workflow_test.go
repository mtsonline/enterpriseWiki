package server_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

type wfCase struct {
	ID        int64             `json:"id"`
	Status    string            `json:"status"`
	Step      string            `json:"step"`
	Outcome   string            `json:"outcome"`
	Data      map[string]any    `json:"data"`
	Trigger   map[string]string `json:"trigger"`
	LastError string            `json:"last_error"`
	Actions   []struct {
		ID string `json:"id"`
	} `json:"actions"`
	Events []struct {
		Action string `json:"action"`
		Label  string `json:"action_label"`
		Note   string `json:"note"`
	} `json:"events"`
}

// TestWorkflow2 covers the steps of Workflow 2 (values, messages, pages,
// properties, sub-workflows), deadlines with escalation and the triggers
// (changes, properties, schedules, events, signed webhooks) with their
// rights.
func TestWorkflow2(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if err := e.app.Config.Update(func(cf *config.Config) error {
		cf.Server.BaseURL = "https://wiki.example.com"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	const pw = "Passw0rd!Passw0rd"
	user := func(name string) (*client, int64) {
		var u idResp
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": pw, "display_name": name, "email": name + "@example.com", "roles": []int64{}}, &u, 201)
		c := e.client()
		c.login(name, pw)
		return c, u.ID
	}
	fred, fredID := user("fred") // defines the workflows
	gus, _ := user("gus")
	hal, _ := user("hal")
	mkcat := func(name string) cat {
		var c cat
		admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": name}}, &c, 201)
		return c
	}
	w, pages, secret := mkcat("Workflows"), mkcat("Pages"), mkcat("Secret")
	for _, c := range []cat{w, pages} {
		admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
		admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": fredID, "preset": "editor"}, nil, 201)
	}
	admin.must("POST", "/api/v1/x/core.properties/defs", map[string]any{"key": "status", "type": "text", "labels": map[string]string{"en": "Status"}}, nil, 201)

	timers := func() {
		t.Helper()
		if err := e.app.Workers.RunOnce(ctx, "workflow.process.timers"); err != nil {
			t.Fatal(err)
		}
	}
	triggers := func() {
		t.Helper()
		if err := e.app.Workers.RunOnce(ctx, "workflow.process.triggers"); err != nil {
			t.Fatal(err)
		}
	}
	get := func(c *client, id int64) wfCase {
		t.Helper()
		var x wfCase
		c.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", id), nil, &x, 200)
		return x
	}
	newWF := func(title string, def map[string]any) elem {
		t.Helper()
		var el elem
		fred.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": title, "body": def}, &el, 201)
		return el
	}
	start := func(c *client, wf int64, data map[string]any) wfCase {
		t.Helper()
		var x wfCase
		c.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf), map[string]any{"data": data}, &x, 201)
		return x
	}
	cases := func(wf int64) []wfCase {
		t.Helper()
		var res struct {
			Instances []wfCase `json:"instances"`
		}
		admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf), nil, &res, 200)
		return res.Instances
	}
	end := map[string]any{"id": "done", "name": "Done", "type": "end", "outcome": "success"}

	// ---- validation
	bad := func(def map[string]any, why string) {
		t.Helper()
		if code := fred.do("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Bad", "body": def}, nil); code != 400 {
			t.Fatalf("%s: %d", why, code)
		}
	}
	steps := func(s ...map[string]any) []map[string]any { return append(s, end) }
	bad(map[string]any{"fields": []any{}, "steps": steps(map[string]any{"id": "c", "name": "C", "type": "set", "next": "done",
		"set": []map[string]any{{"field": "x", "formula": "nope +"}}})}, "broken formula")
	bad(map[string]any{"fields": []any{}, "steps": steps(map[string]any{"id": "h", "name": "H", "type": "http", "next": "done",
		"http": map[string]any{"url": "https://{{field.host}}/x"}})}, "placeholder in the host")
	bad(map[string]any{"fields": []any{}, "steps": steps(), "triggers": []map[string]any{{"id": "s", "type": "schedule", "cron": "61 * * * *"}}}, "bad schedule")
	bad(map[string]any{"fields": []any{}, "steps": steps(), "triggers": []map[string]any{{"id": "c", "type": "content", "events": []string{"create"}}}}, "content trigger without sources")
	bad(map[string]any{"fields": []any{}, "steps": steps(), "triggers": []map[string]any{{"id": "h", "type": "webhook", "allow_from": []string{"not an address!"}}}}, "bad sender")
	bad(map[string]any{"fields": []any{}, "steps": steps(), "triggers": []map[string]any{{"id": "s", "type": "schedule", "cron": "@daily", "run_as": "actor"}}}, "schedule as actor")

	// ---- automatic steps, deadline with escalation
	wf1 := newWF("Onboarding", map[string]any{"description": "", "fields": []map[string]any{
		{"id": "name", "label": "Name", "type": "text", "required": true}, {"id": "amount", "label": "Amount", "type": "number"}},
		"steps": steps(
			map[string]any{"id": "calc", "name": "Calc", "type": "set", "next": "note", "set": []map[string]any{
				{"field": "total", "formula": "amount * 2"}, {"field": "greeting", "value": "Hi {{field.name}}"}}},
			map[string]any{"id": "note", "name": "Note", "type": "notify", "next": "mk",
				"notify": map[string]any{"to": "user:gus", "subject": "New {{case.title}}", "body": "{{field.greeting}}", "channel": "inbox"}},
			map[string]any{"id": "mk", "name": "Page", "type": "create", "next": "pr",
				"create": map[string]any{"category": pages.ID, "title": "Page for {{field.name}}", "body": "Total: {{field.total}}", "store_id": "page"}},
			map[string]any{"id": "pr", "name": "Props", "type": "props", "next": "task",
				"props": map[string]any{"element": "{{field.page}}", "values": map[string]string{"status": "new"}}},
			map[string]any{"id": "task", "name": "Check", "assignee": map[string]string{"type": "user", "value": "gus"},
				"actions":  []map[string]any{{"id": "ok", "label": "OK", "next": "done"}},
				"deadline": map[string]any{"hours": 2, "warn": []int{1}, "escalate": map[string]any{"action": "reassign", "assignee": map[string]string{"type": "user", "value": "hal"}}}},
		)})
	c1 := start(gus, wf1.ID, map[string]any{"name": "Ann", "amount": "21"})
	for i := 0; i < 3; i++ {
		timers()
	}
	x := get(admin, c1.ID)
	if x.Step != "task" || x.Data["total"] != "42" || x.Data["greeting"] != "Hi Ann" || x.Data["page"] == nil {
		t.Fatalf("automatic steps: %+v", x)
	}
	pageID, _ := strconv.ParseInt(fmt.Sprint(x.Data["page"]), 10, 64)
	var page struct {
		Title       string `json:"title"`
		CategoryID  int64  `json:"category_id"`
		CreatedByID *int64 `json:"created_by_id"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", pageID), nil, &page, 200)
	if page.Title != "Page for Ann" || page.CategoryID != pages.ID || page.CreatedByID == nil || *page.CreatedByID != fredID {
		t.Fatalf("page of the create step (by the definer): %+v", page)
	}
	var pv struct {
		Items []struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		} `json:"items"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/core.properties/elements/%d", pageID), nil, &pv, 200)
	if b, _ := json.Marshal(pv); !strings.Contains(string(b), `"status"`) || !strings.Contains(string(b), `new`) {
		t.Fatalf("properties of the props step: %s", b)
	}
	var inbox struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
	}
	gus.must("GET", "/api/v1/me/inbox", nil, &inbox, 200)
	if b, _ := json.Marshal(inbox); !strings.Contains(string(b), "New Ann") {
		t.Fatalf("notification of the notify step: %s", b)
	}
	if len(x.Actions) != 0 {
		t.Fatal("administrator sees actions of a step of gus")
	}
	if x := get(gus, c1.ID); len(x.Actions) == 0 || x.Actions[0].ID != "ok" {
		t.Fatalf("gus has no action: %+v", x)
	}
	// the deadline passes: hal takes over
	if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_workflow_instances SET due_at=now() - interval '1 minute', check_at=now() - interval '1 minute' WHERE id=$1`, c1.ID); err != nil {
		t.Fatal(err)
	}
	timers()
	if x := get(gus, c1.ID); len(x.Actions) != 0 {
		t.Fatal("gus still has the task after the escalation")
	}
	if x := get(hal, c1.ID); len(x.Actions) == 0 || x.Actions[0].ID != "ok" {
		t.Fatalf("hal did not get the task: %+v", x)
	}

	// ---- triggers
	wf2 := newWF("Watcher", map[string]any{"description": "", "no_manual": true,
		"fields": []map[string]any{{"id": "what", "label": "What", "type": "text"}},
		"steps":  steps(map[string]any{"id": "s", "name": "S", "type": "set", "next": "done", "set": []map[string]any{{"field": "seen", "value": "yes"}}}),
		"triggers": []map[string]any{
			{"id": "new", "type": "content", "sources": fmt.Sprintf("category:%d", pages.ID), "events": []string{"create"}, "data": map[string]string{"what": "{{trigger.object.title}}"}},
			{"id": "secret", "type": "content", "sources": fmt.Sprintf("category:%d", secret.ID), "events": []string{"create"}},
			{"id": "status", "type": "content", "sources": fmt.Sprintf("category:%d", pages.ID), "prop": map[string]string{"field": "status", "op": "equals", "value": "done"}},
			{"id": "tick", "type": "schedule", "cron": "* * * * *"},
			{"id": "hook", "type": "webhook", "title": "Order {{trigger.data.order.id}}", "data": map[string]string{"what": "{{trigger.data.order.item}}"}},
			{"id": "chain", "type": "event", "event": "workflow.completed", "workflow": wf1.ID, "outcome": "success"},
		}})
	if code := fred.do("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf2.ID), map[string]any{"data": map[string]any{}}, nil); code != 403 {
		t.Fatalf("manual start of a workflow with triggers only: %d", code)
	}
	byTrigger := func() map[string]int {
		t.Helper()
		n := map[string]int{}
		for _, x := range cases(wf2.ID) {
			var full wfCase
			admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", x.ID), nil, &full, 200)
			n[full.Trigger["trigger.id"]]++
		}
		return n
	}
	// a page in Pages starts a case; one in Secret not (fred cannot see it)
	var p2, p3 elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pages.ID, "type": "core.html", "title": "Fresh page", "body": htmlBody("<p>x</p>")}, &p2, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "core.html", "title": "Hidden page", "body": htmlBody("<p>x</p>")}, &p3, 201)
	triggers()
	triggers()
	n := byTrigger()
	if n["new"] != 1 || n["secret"] != 0 || n["tick"] != 1 {
		t.Fatalf("starts by trigger: %v", n)
	}
	for _, x := range cases(wf2.ID) {
		full := get(admin, x.ID)
		if full.Trigger["trigger.id"] == "new" && (full.Data["what"] != "Fresh page" || full.Status != "done" || full.Data["seen"] != "yes") {
			t.Fatalf("case of the content trigger: %+v", full)
		}
	}
	// a property becomes "done": once
	setStatus := func(v string) {
		fred.must("PUT", fmt.Sprintf("/api/v1/x/core.properties/elements/%d", p2.ID), map[string]any{"values": map[string]any{"status": v}}, nil, 200)
	}
	setStatus("done")
	setStatus("done")
	triggers()
	if n := byTrigger(); n["status"] != 1 || n["tick"] != 1 {
		t.Fatalf("property trigger: %v", n)
	}
	setStatus("open")
	setStatus("done")
	triggers()
	if n := byTrigger(); n["status"] != 2 {
		t.Fatalf("property trigger after a change back: %v", n)
	}

	// ---- incoming webhook
	var sec struct {
		Secret string `json:"secret"`
		URL    string `json:"url"`
	}
	if code := gus.do("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/triggers/hook/secret", wf2.ID), nil, nil); code != 403 {
		t.Fatalf("someone else created a secret: %d", code)
	}
	fred.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/triggers/hook/secret", wf2.ID), nil, &sec, 200)
	if len(sec.Secret) != 64 || !strings.HasSuffix(sec.URL, fmt.Sprintf("/hooks/%d/hook", wf2.ID)) {
		t.Fatalf("secret: %+v", sec)
	}
	var defBody struct {
		Body json.RawMessage `json:"body"`
	}
	fred.must("GET", fmt.Sprintf("/api/v1/elements/%d?edit=1", wf2.ID), nil, &defBody, 200)
	if strings.Contains(string(defBody.Body), sec.Secret) {
		t.Fatal("the secret is part of the workflow")
	}
	hookPath := fmt.Sprintf("/api/v1/x/workflow.process/hooks/%d/hook", wf2.ID)
	send := func(c *http.Client, body string, ts time.Time, secret, delivery string, csrf bool) int {
		t.Helper()
		tss := strconv.FormatInt(ts.Unix(), 10)
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(tss + "." + body))
		req, _ := http.NewRequest("POST", e.srv.URL+hookPath, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Ewiki-Timestamp", tss)
		req.Header.Set("X-Ewiki-Signature", "sha256="+hex.EncodeToString(m.Sum(nil)))
		if delivery != "" {
			req.Header.Set("X-Ewiki-Delivery", delivery)
		}
		if csrf {
			req.Header.Set("X-Requested-With", "ewiki")
		}
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		return res.StatusCode
	}
	plain := &http.Client{}
	order := `{"order":{"id":"A-17","item":"Chair"}}`
	if code := send(plain, order, time.Now(), sec.Secret, "d1", false); code != 202 {
		t.Fatalf("signed webhook: %d", code)
	}
	if code := send(plain, order, time.Now(), sec.Secret, "d1", false); code != 202 { // repeated delivery: accepted, started once
		t.Fatalf("repeated webhook: %d", code)
	}
	for name, code := range map[string]int{
		"wrong secret": send(plain, order, time.Now(), strings.Repeat("0", 64), "d2", false),
		"old":          send(plain, order, time.Now().Add(-10*time.Minute), sec.Secret, "d3", false),
		"other route":  fred.do("POST", hookPath, map[string]any{"order": map[string]any{"id": "X"}}, nil), // a session never counts
	} {
		if code != 403 {
			t.Fatalf("webhook %s: %d", name, code)
		}
	}
	triggers()
	if n := byTrigger(); n["hook"] != 1 {
		t.Fatalf("webhook starts: %v", n)
	}
	for _, x := range cases(wf2.ID) {
		if full := get(admin, x.ID); full.Trigger["trigger.id"] == "hook" && full.Data["what"] != "Chair" {
			t.Fatalf("data of the webhook: %+v", full)
		}
	}

	// ---- chains: the end of a case of Onboarding starts Watcher
	hal.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", c1.ID), map[string]any{"action": "ok"}, nil, 200)
	triggers()
	if n := byTrigger(); n["chain"] != 1 {
		t.Fatalf("chain: %v", n)
	}

	// ---- the definer's rights: a page in a category fred may not write
	wf3 := newWF("Sneaky", map[string]any{"description": "", "fields": []any{}, "steps": steps(
		map[string]any{"id": "mk", "name": "Page", "type": "create", "next": "done", "create": map[string]any{"category": secret.ID, "title": "Planted"}})})
	c3 := start(fred, wf3.ID, map[string]any{})
	for i := 0; i < 4; i++ {
		if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_workflow_instances SET wake_at=now() WHERE id=$1 AND wake_at IS NOT NULL`, c3.ID); err != nil {
			t.Fatal(err)
		}
		timers()
	}
	if x := get(admin, c3.ID); x.Status != "open" || x.LastError == "" {
		t.Fatalf("create step without rights: %+v", x)
	}
	var planted int
	if err := e.app.DB.QueryRow(ctx, `SELECT count(*) FROM elements WHERE titles->>'en'='Planted'`).Scan(&planted); err != nil || planted != 0 {
		t.Fatalf("a page was created without rights: %d %v", planted, err)
	}

	// ---- sub-workflow: waits for the other case and takes its values
	child := newWF("Child", map[string]any{"description": "", "fields": []map[string]any{{"id": "q", "label": "Q", "type": "text"}}, "steps": steps(
		map[string]any{"id": "ans", "name": "Answer", "assignee": map[string]string{"type": "user", "value": "gus"},
			"actions": []map[string]any{{"id": "ok", "label": "OK", "next": "set"}}},
		map[string]any{"id": "set", "name": "Set", "type": "set", "next": "done", "set": []map[string]any{{"field": "answer", "value": "42 for {{field.q}}"}}})})
	parent := newWF("Parent", map[string]any{"description": "", "fields": []any{}, "steps": steps(
		map[string]any{"id": "sub", "name": "Ask", "type": "subflow", "next": "done", "subflow": map[string]any{"workflow": child.ID, "wait": true,
			"data": map[string]string{"q": "{{case.title}}"}, "return": map[string]string{"got": "answer"}}})})
	pc := start(fred, parent.ID, map[string]any{})
	timers()
	if x := get(admin, pc.ID); x.Status != "open" || x.Step != "sub" {
		t.Fatalf("parent does not wait: %+v", x)
	}
	kids := cases(child.ID)
	if len(kids) != 1 {
		t.Fatalf("child cases: %d", len(kids))
	}
	gus.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", kids[0].ID), map[string]any{"action": "ok"}, nil, 200)
	timers()
	if x := get(admin, pc.ID); x.Status != "done" || !strings.HasPrefix(fmt.Sprint(x.Data["got"]), "42 for ") {
		t.Fatalf("parent after the child: %+v", x)
	}

	// ---- HTTP step: only the allow list, values escaped, the answer taken over
	var gotQuery string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"status":"granted","items":[{"id":7}]}}`))
	}))
	defer api.Close()
	ask := newWF("Ask", map[string]any{"description": "", "fields": []map[string]any{{"id": "q", "label": "Q", "type": "text"}}, "steps": steps(
		map[string]any{"id": "h", "name": "Ask", "type": "http", "next": "done",
			"http": map[string]any{"url": api.URL + "/check?q={{field.q}}", "map": map[string]string{"status": "result.status", "first": "result.items.0.id"}}})})
	run := func(id int64) wfCase {
		for i := 0; i < 4; i++ {
			if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_workflow_instances SET wake_at=now() WHERE id=$1 AND wake_at IS NOT NULL`, id); err != nil {
				t.Fatal(err)
			}
			timers()
		}
		return get(admin, id)
	}
	c4 := start(fred, ask.ID, map[string]any{"q": "a&b=c"})
	if x := run(c4.ID); x.Status != "open" || !strings.Contains(x.LastError, "allow list") || gotQuery != "" {
		t.Fatalf("http step outside of the allow list: %+v (query %q)", x, gotQuery)
	}
	if err := e.app.Config.Update(func(cf *config.Config) error {
		cf.Plugins.Macros.HTTPAllow = []string{api.URL + "/"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c5 := start(fred, ask.ID, map[string]any{"q": "a&b=c"})
	if x := run(c5.ID); x.Status != "open" || !strings.Contains(x.LastError, "internal") {
		t.Fatalf("http step to an internal address without permission: %+v", x)
	}
	if err := e.app.Config.Update(func(cf *config.Config) error {
		cf.Plugins.Macros.HTTPAllowPrivate = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c6 := start(fred, ask.ID, map[string]any{"q": "a&b=c"})
	if x := run(c6.ID); x.Status != "done" || x.Data["status"] != "granted" || x.Data["first"] != "7" || gotQuery != "a&b=c" {
		t.Fatalf("http step: %+v (query %q)", x, gotQuery)
	}
}

// TestWorkflow2Part2 covers parallel branches (fixed and one per value),
// hand-overs of tasks (claim, give back, delegate, ask the starter),
// deputies and forms of steps with field rights.
func TestWorkflow2Part2(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.client()
	admin.login("admin", adminPW)
	const pw = "Passw0rd!Passw0rd"
	var team idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "team"}, &team, 200)
	user := func(name string, groups ...int64) (*client, int64) {
		var u idResp
		if groups == nil {
			groups = []int64{}
		}
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": pw, "display_name": name, "email": name + "@example.com", "roles": []int64{}, "groups": groups}, &u, 201)
		c := e.client()
		c.login(name, pw)
		return c, u.ID
	}
	gus, _ := user("gus", team.ID)
	hal, _ := user("hal", team.ID)
	fred, _ := user("fred")
	ivy, _ := user("ivy")
	var w cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Flows"}}, &w, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", w.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	timers := func() {
		t.Helper()
		for i := 0; i < 3; i++ {
			if err := e.app.Workers.RunOnce(ctx, "workflow.process.timers"); err != nil {
				t.Fatal(err)
			}
		}
	}
	get := func(c *client, id int64) wfCase {
		t.Helper()
		var x wfCase
		c.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", id), nil, &x, 200)
		return x
	}
	has := func(x wfCase, action string) bool {
		for _, a := range x.Actions {
			if a.ID == action {
				return true
			}
		}
		return false
	}
	act := func(c *client, id int64, body map[string]any, want int) {
		t.Helper()
		c.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", id), body, nil, want)
	}
	newWF := func(def map[string]any) int64 {
		t.Helper()
		var el elem
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "WF", "body": def}, &el, 201)
		return el.ID
	}
	start := func(wf int64, data map[string]any) int64 {
		t.Helper()
		var x wfCase
		admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf), map[string]any{"data": data}, &x, 201)
		return x.ID
	}
	tasks := func(c *client) []wfCase {
		t.Helper()
		var r struct {
			Tasks []wfCase `json:"tasks"`
		}
		c.must("GET", "/api/v1/x/workflow.process/tasks", nil, &r, 200)
		return r.Tasks
	}

	// ---- one branch per region, two of three are enough
	wf := newWF(map[string]any{"description": "", "fields": []map[string]any{{"id": "regions", "label": "Regions", "type": "textarea"}},
		"steps": []map[string]any{
			{"id": "par", "name": "Ask the regions", "type": "parallel", "next": "sum", "parallel": map[string]any{"branches": []string{"ask"}, "for_each": "regions", "require": 2}},
			{"id": "ask", "name": "Region answers", "assignee": map[string]string{"type": "user", "value": "gus"},
				"actions": []map[string]any{{"id": "ok", "label": "OK", "next": "note"}}},
			{"id": "note", "name": "Note", "type": "set", "next": "sum", "set": []map[string]any{{"field": "last", "value": "{{field.item}}"}}},
			{"id": "sum", "name": "Summary", "type": "set", "next": "done", "set": []map[string]any{{"field": "summary", "value": "done after {{field.last}}"}}},
			{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}})
	pc := start(wf, map[string]any{"regions": "North\nSouth\nEast"})
	timers()
	if x := get(admin, pc); x.Status != "open" || x.Step != "par" {
		t.Fatalf("parallel: %+v", x)
	}
	ts := tasks(gus)
	if len(ts) != 3 {
		var dump []string
		rows, _ := e.app.DB.Query(ctx, `SELECT id::text || ' ' || step || ' ' || status || ' ' || branch::text || ' ' || last_error || ' ' || coalesce(wake_at::text,'-') FROM plugin_workflow_instances`)
		for rows.Next() {
			var s string
			_ = rows.Scan(&s)
			dump = append(dump, s)
		}
		rows.Close()
		t.Fatalf("branch tasks: %d %v %+v", len(ts), dump, get(admin, pc))
	}
	var list struct {
		Instances []wfCase `json:"instances"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf), nil, &list, 200)
	if len(list.Instances) != 1 {
		t.Fatalf("branches are listed as cases: %d", len(list.Instances))
	}
	act(gus, ts[0].ID, map[string]any{"action": "ok"}, 200)
	timers()
	if x := get(admin, pc); x.Status != "open" {
		t.Fatalf("continued after one of three: %+v", x)
	}
	act(gus, ts[1].ID, map[string]any{"action": "ok"}, 200)
	timers()
	x := get(admin, pc)
	if x.Status != "done" || !strings.HasPrefix(fmt.Sprint(x.Data["summary"]), "done after ") {
		t.Fatalf("after two of three: %+v", x)
	}
	if b := get(admin, ts[2].ID); b.Status != "cancelled" {
		t.Fatalf("the third branch: %+v", b)
	}

	// ---- hand-overs: claim, give back, delegate, ask the starter; a form with field rights
	wf2 := newWF(map[string]any{"description": "", "fields": []map[string]any{
		{"id": "topic", "label": "Topic", "type": "text"}, {"id": "salary", "label": "Salary", "type": "number"},
		{"id": "decision", "label": "Decision", "type": "textarea", "later": true}},
		"steps": []map[string]any{
			{"id": "check", "name": "Check", "assignee": map[string]string{"type": "group", "value": "team"},
				"actions": []map[string]any{{"id": "ok", "label": "OK", "next": "done"}},
				"form":    []map[string]string{{"field": "decision", "mode": "required"}, {"field": "salary", "mode": "hidden"}}},
			{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}})
	var meta struct {
		Fields []struct {
			ID string `json:"id"`
		} `json:"fields"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf2), nil, &meta, 200)
	for _, f := range meta.Fields {
		if f.ID == "decision" {
			t.Fatal("a later field is in the start form")
		}
	}
	hc := start(wf2, map[string]any{"topic": "Raise", "salary": "5000", "decision": "sneaked in"})
	g := get(gus, hc)
	if _, ok := g.Data["salary"]; ok || g.Data["decision"] != nil || !has(g, "ok") || !has(g, "_claim") || !has(g, "_delegate") || !has(g, "_ask") {
		t.Fatalf("assignee view: %+v", g)
	}
	if a := get(admin, hc); a.Data["salary"] == nil {
		t.Fatal("the starter does not see the hidden field")
	}
	act(gus, hc, map[string]any{"action": "_claim"}, 200)
	if code := hal.do("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", hc), nil, nil); code != 404 {
		t.Fatalf("hal still sees the claimed task: %d", code)
	}
	act(gus, hc, map[string]any{"action": "_release"}, 200)
	if h := get(hal, hc); !has(h, "ok") {
		t.Fatal("given back: hal has no task")
	}
	act(gus, hc, map[string]any{"action": "_delegate", "to": "nobody"}, 400)
	act(gus, hc, map[string]any{"action": "_delegate", "to": "fred", "note": "you know this"}, 200)
	if f := get(fred, hc); !has(f, "ok") {
		t.Fatalf("fred did not get the task: %+v", f)
	}
	if g := gus.do("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", hc), nil, nil); g != 200 {
		t.Fatalf("gus (former assignee) lost the case: %d", g)
	}
	act(fred, hc, map[string]any{"action": "_ask"}, 400) // a question needs a note
	act(fred, hc, map[string]any{"action": "_ask", "note": "Which raise?"}, 200)
	if s := get(admin, hc); len(s.Actions) != 1 || s.Actions[0].ID != "_answer" {
		t.Fatalf("the starter must answer: %+v", s.Actions)
	}
	if f := get(fred, hc); has(f, "ok") {
		t.Fatal("fred decides while the starter is asked")
	}
	act(admin, hc, map[string]any{"action": "_answer", "note": "10 %"}, 200)
	act(fred, hc, map[string]any{"action": "ok"}, 400) // the decision is required
	act(fred, hc, map[string]any{"action": "ok", "data": map[string]any{"decision": "approved"}}, 200)
	if d := get(admin, hc); d.Status != "done" || d.Data["decision"] != "approved" {
		t.Fatalf("decision with the form: %+v", d)
	}

	// ---- deputies: ivy stands in for hal
	wf3 := newWF(map[string]any{"description": "", "fields": []any{}, "steps": []map[string]any{
		{"id": "check", "name": "Check", "assignee": map[string]string{"type": "user", "value": "hal"}, "actions": []map[string]any{{"id": "ok", "label": "OK", "next": "done"}}},
		{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}})
	today := time.Now().Format("2006-01-02")
	hal.must("PUT", "/api/v1/x/workflow.process/me/deputy", map[string]any{"deputy": "hal", "from": today, "until": today}, nil, 400)
	hal.must("PUT", "/api/v1/x/workflow.process/me/deputy", map[string]any{"deputy": "ivy", "from": today, "until": time.Now().AddDate(0, 0, 7).Format("2006-01-02")}, nil, 200)
	dc := start(wf3, map[string]any{})
	if i := get(ivy, dc); !has(i, "ok") {
		t.Fatalf("the deputy has no task: %+v", i)
	}
	if len(tasks(ivy)) != 1 {
		t.Fatal("the task of the deputy is not in her list")
	}
	hal.must("DELETE", "/api/v1/x/workflow.process/me/deputy", nil, nil, 204)
	if code := ivy.do("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", dc), nil, nil); code != 404 {
		t.Fatalf("after the end of the deputyship: %d", code)
	}
}

// TestWorkflow2Part2b covers links in task e-mails (once, only for the
// person, without signing in only when allowed and below confidential),
// test runs, versions, the evaluation and the retention of cases.
func TestWorkflow2Part2b(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if err := e.app.Config.Update(func(cf *config.Config) error {
		cf.Mail.From = "wiki@example.com"
		cf.Server.BaseURL = "https://wiki.example.com"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	const pw = "Passw0rd!Passw0rd"
	var gusID idResp
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "gus", "password": pw, "display_name": "Gus", "email": "gus@example.com", "roles": []int64{}}, &gusID, 201)
	gus := e.client()
	gus.login("gus", pw)
	var w cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Flows"}}, &w, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", w.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	def := map[string]any{"description": "", "links": true, "retention": map[string]any{"days": 1, "mode": "anonymize"},
		"fields": []map[string]any{{"id": "who", "label": "Who", "type": "text"}},
		"steps": []map[string]any{
			{"id": "mail", "name": "Mail", "type": "email", "next": "check", "email": map[string]any{"to": "outside@example.org", "subject": "Hi", "body": "x"}},
			{"id": "check", "name": "Check", "assignee": map[string]string{"type": "user", "value": "gus"},
				"actions": []map[string]any{{"id": "ok", "label": "Agree", "next": "done"}, {"id": "no", "label": "Refuse", "next": "done", "note": "required"}}},
			{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Leave", "body": def}, &wf, 201)
	start := func(c *client, body map[string]any, want int) wfCase {
		t.Helper()
		var x wfCase
		c.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), body, &x, want)
		return x
	}
	tokenOf := func(label string) string {
		t.Helper()
		var body string
		if err := e.app.DB.QueryRow(ctx, `SELECT body_text FROM mail_outbox WHERE 'gus@example.com' = ANY(to_addrs) ORDER BY id DESC LIMIT 1`).Scan(&body); err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "- "+label+": ") {
				_, tok, _ := strings.Cut(line, "?t=")
				return tok
			}
		}
		t.Fatalf("no link %q in: %s", label, body)
		return ""
	}
	guest := e.client()
	info := func(tok string) (int, map[string]any) {
		var m map[string]any
		code := guest.do("GET", "/api/v1/x/workflow.process/links/info?t="+url.QueryEscape(tok), nil, &m)
		return code, m
	}
	c1 := start(admin, map[string]any{"data": map[string]any{"who": "Ann"}}, 201)
	tok := tokenOf("Agree")
	if code, m := info(tok); code != 200 || m["without_login"] != false || m["mine"] != false || m["label"] != "Agree" {
		t.Fatalf("info: %d %v", code, m)
	}
	if code := guest.do("POST", "/api/v1/x/workflow.process/links/act", map[string]any{"t": tok}, nil); code != 403 {
		t.Fatalf("decision without login while not allowed: %d", code)
	}
	if err := e.app.Config.Update(func(cf *config.Config) error {
		if cf.Settings == nil {
			cf.Settings = map[string]map[string]any{}
		}
		cf.Settings["workflow.process"] = map[string]any{"links_without_login": true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	bad := tok[:len(tok)-2] + "xx"
	if code := guest.do("POST", "/api/v1/x/workflow.process/links/act", map[string]any{"t": bad}, nil); code != 400 {
		t.Fatalf("changed link: %d", code)
	}
	guest.must("POST", "/api/v1/x/workflow.process/links/act", map[string]any{"t": tok}, nil, 200)
	var x wfCase
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", c1.ID), nil, &x, 200)
	if x.Status != "done" {
		t.Fatalf("after the link: %+v", x)
	}
	if code := guest.do("POST", "/api/v1/x/workflow.process/links/act", map[string]any{"t": tok}, nil); code != 409 {
		t.Fatalf("link used twice: %d", code)
	}
	// confidential: never without signing in
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", w.ID), map[string]any{"classification": 2}, nil, 200)
	start(admin, map[string]any{"data": map[string]any{"who": "Bob"}}, 201)
	if code := guest.do("POST", "/api/v1/x/workflow.process/links/act", map[string]any{"t": tokenOf("Agree")}, nil); code != 403 {
		t.Fatalf("confidential without login: %d", code)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", w.ID), map[string]any{"classification": -1}, nil, 200)

	// test run: only who may change the workflow; nothing leaves, all tasks to the tester
	if code := gus.do("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"test": true}, nil); code != 403 {
		t.Fatalf("test run by someone else: %d", code)
	}
	var mails int
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM mail_outbox`).Scan(&mails)
	tc := start(admin, map[string]any{"test": true, "data": map[string]any{"who": "Test"}}, 201)
	var after int
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM mail_outbox`).Scan(&after)
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", tc.ID), nil, &x, 200)
	if after != mails || x.Step != "check" || len(x.Actions) == 0 || x.Events[1].Action != "test_skipped" {
		t.Fatalf("test run: mails %d→%d %+v", mails, after, x)
	}
	if code := gus.do("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", tc.ID), nil, nil); code != 404 {
		t.Fatalf("gus sees the test run: %d", code)
	}
	var list struct {
		Instances []wfCase `json:"instances"`
		CanTest   bool     `json:"can_test"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances?status=", wf.ID), nil, &list, 200)
	for _, c := range list.Instances {
		if c.ID == tc.ID {
			t.Fatal("test run among the cases")
		}
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances?test=1&status=", wf.ID), nil, &list, 200)
	if len(list.Instances) != 1 || !list.CanTest {
		t.Fatalf("test runs: %+v", list)
	}

	// evaluation
	var st struct {
		Done     int            `json:"done"`
		Open     int            `json:"open"`
		Outcomes map[string]int `json:"outcomes"`
		Steps    []struct {
			ID     string `json:"id"`
			Visits int    `json:"visits"`
			Open   int    `json:"open"`
		} `json:"steps"`
		Versions []int `json:"versions"`
	}
	if code := gus.do("GET", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/stats", wf.ID), nil, nil); code != 403 {
		t.Fatalf("stats for gus: %d", code)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/stats", wf.ID), nil, &st, 200)
	if st.Done != 1 || st.Open != 1 || st.Outcomes["success"] != 1 || len(st.Versions) != 1 || st.Versions[0] != 1 {
		t.Fatalf("stats: %+v", st)
	}

	// retention: anonymized a day after closing
	if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_workflow_instances SET closed_at = now() - interval '2 days' WHERE id=$1`, c1.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Workers.RunOnce(ctx, "workflow.process.retention"); err != nil {
		t.Fatal(err)
	}
	var anon wfCase
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", c1.ID), nil, &anon, 200)
	if anon.Data["who"] != nil || len(anon.Events) == 0 {
		t.Fatalf("anonymized: %+v", anon)
	}
	var title string
	_ = e.app.DB.QueryRow(ctx, `SELECT title FROM plugin_workflow_instances WHERE id=$1`, c1.ID).Scan(&title)
	if title != "–" {
		t.Fatalf("title after anonymizing: %q", title)
	}
}

// TestWorkflowLinkPrivacy: the validity of decision links is chosen per
// workflow and never passes the deadline of the step; the tokens are
// hidden in the outbox and removed from delivered messages.
func TestWorkflowLinkPrivacy(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	f := newSMTPFake(t)
	useSMTPFake(t, e, f, func(cf *config.Config) { cf.Server.BaseURL = "https://wiki.example.com" })
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "lia", "password": "Passw0rd!Passw0rd", "display_name": "Lia", "email": "lia@example.com", "roles": []int64{}}, nil, 201)
	var w cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Flows"}}, &w, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", w.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	def := func(hours int, deadline map[string]any) map[string]any {
		return map[string]any{"description": "", "links": true, "link_hours": hours,
			"fields": []map[string]any{{"id": "who", "label": "Who", "type": "text"}},
			"steps": []map[string]any{
				{"id": "check", "name": "Check", "assignee": map[string]string{"type": "user", "value": "lia"}, "deadline": deadline,
					"actions": []map[string]any{{"id": "ok", "label": "Agree", "next": "done"}}},
				{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}
	}
	if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Too long",
		"body": def(91*24, nil)}, nil); code != 400 {
		t.Fatalf("links valid longer than allowed: %d", code)
	}
	expOf := func(id int64) (time.Time, string, int64) {
		t.Helper()
		var mid int64
		var body string
		if err := e.app.DB.QueryRow(ctx, `SELECT id, body_text FROM mail_outbox WHERE 'lia@example.com' = ANY(to_addrs) AND ref=$1`, fmt.Sprintf("case:%d", id)).Scan(&mid, &body); err != nil {
			t.Fatal(err)
		}
		_, tok, ok := strings.Cut(body, "/workflow/link?t=")
		if !ok {
			t.Fatalf("no link: %s", body)
		}
		tok, _, _ = strings.Cut(tok, "\n")
		payload, _, _ := strings.Cut(tok, ".")
		raw, err := base64.RawURLEncoding.DecodeString(payload)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(string(raw), "|")
		exp, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
		return time.Unix(exp, 0), tok, mid
	}
	start := func(wf elem) int64 {
		var x wfCase
		admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{"who": "A"}}, &x, 201)
		return x.ID
	}
	// chosen validity: 3 hours
	var wf1 elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Short", "body": def(3, nil)}, &wf1, 201)
	exp, tok, mid := expOf(start(wf1))
	if d := time.Until(exp); d < 2*time.Hour+50*time.Minute || d > 3*time.Hour+time.Minute {
		t.Fatalf("validity of 3 hours: %v", d)
	}
	// never past the deadline of the step (1 hour)
	var wf2 elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Deadline",
		"body": def(48, map[string]any{"hours": 1})}, &wf2, 201)
	c2 := start(wf2)
	exp2, _, _ := expOf(c2)
	var due time.Time
	_ = e.app.DB.QueryRow(ctx, `SELECT due_at FROM plugin_workflow_instances WHERE id=$1`, c2).Scan(&due)
	if exp2.After(due) || time.Until(exp2) > time.Hour+time.Minute {
		t.Fatalf("link valid until %v, deadline %v", exp2, due)
	}
	// the outbox never shows the token
	var m struct {
		Body string `json:"body"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/admin/mail/outbox/%d", mid), nil, &m, 200)
	if strings.Contains(m.Body, tok) || !strings.Contains(m.Body, "/workflow/link?t=[hidden]") {
		t.Fatalf("outbox shows: %s", m.Body)
	}
	var list []struct {
		Body string `json:"body"`
	}
	admin.must("GET", "/api/v1/admin/mail/outbox?q=lia@example.com", nil, &list, 200)
	for _, x := range list {
		if strings.Contains(x.Body, "/workflow/link?t=") && !strings.Contains(x.Body, "?t=[hidden]") {
			t.Fatalf("outbox list shows a token: %s", x.Body)
		}
	}
	// delivered: the token is gone from the database too
	e.flushMail(t)
	var stored, status string
	_ = e.app.DB.QueryRow(ctx, `SELECT body_text, status FROM mail_outbox WHERE id=$1`, mid).Scan(&stored, &status)
	if status != "sent" || strings.Contains(stored, tok) || !strings.Contains(stored, "[hidden]") {
		t.Fatalf("after delivery (%s): %s", status, stored)
	}
}
