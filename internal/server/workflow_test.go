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
	"sync"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// TestWorkflowAccountSteps covers the events of accounts (from every source,
// via the change log) as workflow triggers and the steps for permissions
// (with expiry), group membership and moving pages – with the rights of
// the definer only.
func TestWorkflowAccountSteps(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.client()
	admin.login("admin", adminPW)
	run := func(jobs ...string) {
		t.Helper()
		for _, j := range jobs {
			if err := e.app.Workers.RunOnce(ctx, j); err != nil {
				t.Fatalf("%s: %v", j, err)
			}
		}
	}
	run("identity.events") // the site starts after the existing changes
	const pw = "Passw0rd!Passw0rd"
	var eveID, grp, arch idResp
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "eve", "password": pw, "display_name": "Eve", "roles": []int64{}}, &eveID, 201)
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Newcomers"}, &grp, 201)
	var w, docs cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "HR"}}, &w, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Docs"}}, &docs, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Archive"}}, &arch, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", w.ID), map[string]any{"subject_type": "user", "subject_id": eveID.ID, "preset": "editor"}, nil, 201)
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": docs.ID, "type": "core.html", "title": "Welcome pack",
		"body": map[string]any{"format": "markdown", "source": "hi"}}, &page, 201)

	onboarding := map[string]any{"description": "", "fields": []map[string]any{{"id": "who", "label": "Who", "type": "text"}, {"id": "page", "label": "Page", "type": "text"}},
		"triggers": []map[string]any{{"id": "new", "type": "event", "event": "user.created", "title": "Welcome {{trigger.data.display_name}}",
			"data": map[string]string{"who": "{{trigger.data.username}}", "page": fmt.Sprint(page.ID)}}},
		"steps": []map[string]any{
			{"id": "grant", "name": "Read docs", "type": "access", "next": "join",
				"access": map[string]any{"mode": "grant", "category": docs.ID, "subject": "user", "user": "{{field.who}}", "level": "reader", "days": 30}},
			{"id": "join", "name": "Join", "type": "membership", "next": "move",
				"membership": map[string]any{"mode": "add", "group": grp.ID, "user": "{{field.who}}"}},
			{"id": "move", "name": "Archive", "type": "move", "next": "done", "move": map[string]any{"element": "{{field.page}}", "category": arch.ID}},
			{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}
	// only who manages accounts may react to changes of accounts
	eve := e.client()
	eve.login("eve", pw)
	if code := eve.do("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Sneaky", "body": onboarding}, nil); code != 403 {
		t.Fatalf("account trigger saved by eve: %d", code)
	}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Onboarding", "body": onboarding}, &wf, 201)
	// a group trigger with a filter
	var other idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Others"}, &other, 201)
	var wfG elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Joined", "body": map[string]any{
		"description": "", "fields": []map[string]any{{"id": "who", "label": "Who", "type": "text"}},
		"triggers": []map[string]any{{"id": "g", "type": "event", "event": "group.member_added", "group": grp.ID, "data": map[string]string{"who": "{{trigger.data.username}} in {{trigger.data.group_name}}"}}},
		"steps":    []map[string]any{{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}}, &wfG, 201)

	// webhooks: account events only when chosen explicitly
	var hookAll, hookUsers int64
	if err := e.app.DB.QueryRow(ctx, `INSERT INTO webhooks(name, url) VALUES ('all', 'https://hooks.example.com/a') RETURNING id`).Scan(&hookAll); err != nil {
		t.Fatal(err)
	}
	if err := e.app.DB.QueryRow(ctx, `INSERT INTO webhooks(name, url, events) VALUES ('users', 'https://hooks.example.com/u', '{user.created}') RETURNING id`).Scan(&hookUsers); err != nil {
		t.Fatal(err)
	}
	// a new account (any source) starts the onboarding
	var nia idResp
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "nia", "password": pw, "display_name": "Nia", "roles": []int64{}}, &nia, 201)
	run("identity.events", "workflow.process.triggers", "workflow.process.timers", "workflow.process.timers", "workflow.process.timers")
	var list struct {
		Instances []wfCase `json:"instances"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances?status=", wf.ID), nil, &list, 200)
	if len(list.Instances) != 1 {
		t.Fatalf("onboarding cases: %+v", list.Instances)
	}
	var x wfCase
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", list.Instances[0].ID), nil, &x, 200)
	if x.Status != "done" || x.Data["who"] != "nia" {
		t.Fatalf("onboarding: %+v", x)
	}
	var nAll, nUsers int
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE webhook_id=$1 AND event LIKE 'user.%'), count(*) FILTER (WHERE webhook_id=$2 AND event='user.created')
		FROM webhook_deliveries`, hookAll, hookUsers).Scan(&nAll, &nUsers)
	if nAll != 0 || nUsers != 2 { // eve and nia
		t.Fatalf("webhook deliveries of account events: all-events hook %d, chosen %d", nAll, nUsers)
	}
	var payload string
	_ = e.app.DB.QueryRow(ctx, `SELECT payload::text FROM webhook_deliveries WHERE webhook_id=$1 ORDER BY id DESC LIMIT 1`, hookUsers).Scan(&payload)
	if !strings.Contains(payload, `"username": "nia"`) {
		t.Fatalf("payload: %s", payload)
	}
	var rule int64
	var until time.Time
	if err := e.app.DB.QueryRow(ctx, `SELECT r.id, g.expires_at FROM acl_rules r JOIN plugin_workflow_grants g ON g.rule_id=r.id
		WHERE r.category_id=$1 AND r.subject_type='user' AND r.subject_id=$2`, docs.ID, nia.ID).Scan(&rule, &until); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if d := time.Until(until); d < 29*24*time.Hour || d > 30*24*time.Hour+time.Minute {
		t.Fatalf("grant until %v", until)
	}
	var member bool
	_ = e.app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_groups WHERE user_id=$1 AND group_id=$2)`, nia.ID, grp.ID).Scan(&member)
	var moved elem
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, &moved, 200)
	if !member || moved.CategoryID != arch.ID {
		t.Fatalf("member %v, page in %d", member, moved.CategoryID)
	}
	// the group trigger saw the membership (and only that group)
	run("identity.events", "workflow.process.triggers")
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances?status=", wfG.ID), nil, &list, 200)
	if len(list.Instances) != 1 || list.Instances[0].Data["who"] != "nia in Newcomers" {
		t.Fatalf("group trigger: %+v", list.Instances)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/groups/%d/members/%d", other.ID, nia.ID), nil, nil, 204)
	run("identity.events", "workflow.process.triggers")
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances?status=", wfG.ID), nil, &list, 200)
	if len(list.Instances) != 1 {
		t.Fatalf("group filter ignored: %d cases", len(list.Instances))
	}
	// the permission ends after its time
	if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_workflow_grants SET expires_at = now() - interval '1 minute' WHERE rule_id=$1`, rule); err != nil {
		t.Fatal(err)
	}
	run("workflow.process.grants")
	var left int
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM acl_rules WHERE id=$1`, rule).Scan(&left)
	if left != 0 {
		t.Fatal("expired permission still there")
	}

	// a workflow of eve: the steps run with her rights – she cannot manage groups
	var wfE elem
	eve.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Eve's", "body": map[string]any{
		"description": "", "fields": []map[string]any{{"id": "who", "label": "Who", "type": "text"}},
		"steps": []map[string]any{
			{"id": "join", "name": "Join", "type": "membership", "next": "done", "membership": map[string]any{"mode": "add", "group": grp.ID, "user": "{{starter.username}}"}},
			{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}}, &wfE, 201)
	var c wfCase
	eve.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wfE.ID), map[string]any{"data": map[string]any{"who": "eve"}}, &c, 201)
	run("workflow.process.timers")
	x = wfCase{}
	eve.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", c.ID), nil, &x, 200)
	if x.Status != "open" || !strings.Contains(x.LastError, "403") {
		t.Fatalf("membership with eve's rights: %+v", x)
	}
	_ = e.app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_groups WHERE user_id=$1 AND group_id=$2)`, eveID.ID, grp.ID).Scan(&member)
	if member {
		t.Fatal("eve put herself into a group")
	}

	// values of the start form: refused unless the administrator allows them
	var wfF elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "By form", "body": map[string]any{
		"description": "", "fields": []map[string]any{{"id": "who", "label": "Who", "type": "text"}},
		"steps": []map[string]any{
			{"id": "join", "name": "Join", "type": "membership", "next": "done", "membership": map[string]any{"mode": "add", "group": other.ID, "user": "{{field.who}}"}},
			{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}}, &wfF, 201)
	var cf wfCase
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wfF.ID), map[string]any{"data": map[string]any{"who": "eve"}}, &cf, 201)
	run("workflow.process.timers")
	x = wfCase{}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", cf.ID), nil, &x, 200)
	if x.Status != "open" || !strings.Contains(x.LastError, "start form") {
		t.Fatalf("person from the start form: %+v", x)
	}
	if err := e.app.Config.Update(func(cf *config.Config) error {
		if cf.Settings == nil {
			cf.Settings = map[string]map[string]any{}
		}
		cf.Settings["workflow.process"] = map[string]any{"form_values_for_accounts": true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_workflow_instances SET wake_at=now() WHERE id=$1`, cf.ID); err != nil { // the next automatic attempt
		t.Fatal(err)
	}
	run("workflow.process.timers")
	x = wfCase{}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", cf.ID), nil, &x, 200)
	if x.Status != "done" {
		t.Fatalf("allowed by the administrator: %+v", x)
	}
	// webhooks with all events: account events only when the administrator allows it
	admin.must("PUT", "/api/v1/admin/webhooks/options", map[string]any{"accounts_in_all": true}, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", eveID.ID), map[string]any{"disabled": true}, nil, 200)
	run("identity.events")
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE webhook_id=$1 AND event='user.disabled'`, hookAll).Scan(&nAll)
	if nAll != 1 {
		t.Fatalf("all-events hook with accounts allowed: %d", nAll)
	}
	admin.must("PUT", "/api/v1/admin/webhooks/options", map[string]any{"accounts_in_all": false}, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", eveID.ID), map[string]any{"disabled": false}, nil, 200)
	eve.login("eve", pw)

	// disabling and deleting are events too; the webhook of an administrator gets them
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", nia.ID), map[string]any{"disabled": true}, nil, 200)
	admin.must("DELETE", fmt.Sprintf("/api/v1/admin/users/%d", nia.ID), nil, nil, 204)
	var evs []string
	rows, err := e.app.DB.Query(ctx, `SELECT event FROM identity_changes WHERE user_id=$1 ORDER BY seq`, nia.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		evs = append(evs, s)
	}
	rows.Close()
	if strings.Join(evs, ",") != "user.created,group.member_added,group.member_added,user.disabled,user.deleted" {
		t.Fatalf("changes: %v", evs)
	}
	// group memberships of a person need the right to manage groups
	if code := eve.do("PUT", fmt.Sprintf("/api/v1/admin/groups/%d/members/%d", grp.ID, eveID.ID), nil, nil); code != 403 {
		t.Fatalf("eve changes a group: %d", code)
	}
}

// TestWorkflowFileAndCalendarSteps: a step writes documents (PDF, Word, Excel,
// CSV …) into the media library and another creates a calendar entry –
// both with the rights of the definer only.
func TestWorkflowFileAndCalendarSteps(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.client()
	admin.login("admin", adminPW)
	const pw = "Passw0rd!Passw0rd"
	var w, files, cal cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Flows"}}, &w, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Records"}}, &files, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Dates"}}, &cal, 201)
	var calEl elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cal.ID, "type": "content.events", "title": "Team", "body": map[string]any{"view": "list"}}, &calEl, 201)
	def := map[string]any{"description": "", "fields": []map[string]any{
		{"id": "who", "label": "Name", "type": "text"}, {"id": "amount", "label": "Amount", "type": "text"}, {"id": "date", "label": "Date", "type": "text"}},
		"steps": []map[string]any{
			{"id": "doc", "name": "Record", "type": "file", "next": "meet", "file": map[string]any{"category": files.ID, "name": "Request {{field.who}}",
				"formats": []string{"pdf", "docx", "xlsx", "csv", "md"}, "content": "# Request\nBy {{field.who}} <b>bold</b>\nL={{label.who}}\n{{all}}", "data": true, "store_id": "doc", "store_url": "doc_url"}},
			{"id": "meet", "name": "Meeting", "type": "calendar", "next": "done", "calendar": map[string]any{"calendar": calEl.ID, "title": "Talk with {{field.who}}",
				"start": "{{field.date}}", "minutes": 30, "tz": "Europe/Vienna", "location": "Room 1", "store_id": "event"}},
			{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}
	bad := map[string]any{"description": "", "fields": []map[string]any{}, "steps": []map[string]any{
		{"id": "doc", "name": "Record", "type": "file", "next": "done", "file": map[string]any{"category": files.ID, "formats": []string{"exe"}, "data": true}},
		{"id": "done", "name": "Done", "type": "end"}}}
	if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Bad", "body": bad}, nil); code != 400 {
		t.Fatalf("unknown format: %d", code)
	}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Requests", "body": def}, &wf, 201)
	var c wfCase
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID),
		map[string]any{"data": map[string]any{"who": "Ana", "amount": "=SUM(A1:A9)", "date": "2026-12-03 14:00"}}, &c, 201)
	for range 3 {
		if err := e.app.Workers.RunOnce(ctx, "workflow.process.timers"); err != nil {
			t.Fatal(err)
		}
	}
	var x wfCase
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", c.ID), nil, &x, 200)
	if x.Status != "done" || x.Data["doc"] == "" || x.Data["event"] == "" || !strings.Contains(fmt.Sprint(x.Data["doc_url"]), "/media?category=") {
		t.Fatalf("case: %+v", x)
	}
	rows, err := e.app.DB.Query(ctx, `SELECT id, name FROM media WHERE category_id=$1 ORDER BY id`, files.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for rows.Next() {
		var id int64
		var name string
		_ = rows.Scan(&id, &name)
		got[name] = id
	}
	rows.Close()
	for _, ext := range []string{"pdf", "docx", "xlsx", "csv", "md"} {
		if got["Request Ana."+ext] == 0 {
			t.Fatalf("files: %v", got)
		}
	}
	if fmt.Sprint(got["Request Ana.pdf"]) != fmt.Sprint(x.Data["doc"]) {
		t.Fatalf("stored id %v, files %v", x.Data["doc"], got)
	}
	code, csvBody := admin.rawBody("GET", fmt.Sprintf("/api/v1/media/%d/download", got["Request Ana.csv"]), "")
	if code != 200 || !strings.Contains(csvBody, "'=SUM(A1:A9)") || !strings.Contains(csvBody, "Name,Ana") {
		t.Fatalf("csv %d: %q", code, csvBody)
	}
	code, md := admin.rawBody("GET", fmt.Sprintf("/api/v1/media/%d/download", got["Request Ana.md"]), "")
	if code != 200 || !strings.Contains(md, "# Request") || !strings.Contains(md, "- **Amount:** =SUM(A1:A9)") ||
		!strings.Contains(md, "L=Name\nName: Ana\nAmount: =SUM(A1:A9)\nDate: 2026-12-03 14:00") {
		t.Fatalf("md %d: %q", code, md)
	}
	code, docx := admin.rawBody("GET", fmt.Sprintf("/api/v1/media/%d/download", got["Request Ana.docx"]), "")
	if code != 200 || !strings.HasPrefix(docx, "PK") {
		t.Fatalf("docx %d", code)
	}
	var evr struct {
		Event struct {
			Title    string    `json:"title"`
			StartsAt time.Time `json:"starts_at"`
			EndsAt   time.Time `json:"ends_at"`
			Location string    `json:"location"`
		} `json:"event"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/content.events/events/%v", x.Data["event"]), nil, &evr, 200)
	ev := evr.Event
	vienna, _ := time.LoadLocation("Europe/Vienna")
	if ev.Title != "Talk with Ana" || !ev.StartsAt.Equal(time.Date(2026, 12, 3, 14, 0, 0, 0, vienna)) || ev.EndsAt.Sub(ev.StartsAt) != 30*time.Minute || ev.Location != "Room 1" {
		t.Fatalf("event: %+v", ev)
	}

	// someone who may create workflows here but not upload into the records
	var gus idResp
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "gus", "password": pw, "display_name": "Gus", "roles": []int64{}}, &gus, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", w.ID), map[string]any{"subject_type": "user", "subject_id": gus.ID, "preset": "editor"}, nil, 201)
	g := e.client()
	g.login("gus", pw)
	var wfG elem
	g.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Gus", "body": def}, &wfG, 201)
	var cg wfCase
	g.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wfG.ID), map[string]any{"data": map[string]any{"who": "Bo", "date": "+1d"}}, &cg, 201)
	if err := e.app.Workers.RunOnce(ctx, "workflow.process.timers"); err != nil {
		t.Fatal(err)
	}
	x = wfCase{}
	g.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", cg.ID), nil, &x, 200)
	if x.Status != "open" || x.Step != "doc" || x.LastError == "" {
		t.Fatalf("file with gus's rights: %+v", x)
	}
	var n int
	_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM media WHERE name LIKE 'Request Bo.%'`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d files written without the right", n)
	}
}

// TestWorkflowStepsDeadlinesTriggers covers the steps of Workflow 2 (values, messages, pages,
// properties, sub-workflows), deadlines with escalation and the triggers
// (changes, properties, schedules, events, signed webhooks) with their
// rights.
func TestWorkflowStepsDeadlinesTriggers(t *testing.T) {
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
			map[string]any{"id": "task", "name": "Check", "assignees": []map[string]string{{"type": "user", "value": "gus"}},
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
	// the schedule "every minute" starts one case per minute: once in the
	// minute the test began, once more for every minute boundary it crossed
	// (a run across a full minute is no error)
	began := time.Now().Truncate(time.Minute)
	ticksOK := func(n int) bool { return n >= 1 && n <= int(time.Since(began)/time.Minute)+1 }
	// a page in Pages starts a case; one in Secret not (fred cannot see it)
	var p2, p3 elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pages.ID, "type": "core.html", "title": "Fresh page", "body": htmlBody("<p>x</p>")}, &p2, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "core.html", "title": "Hidden page", "body": htmlBody("<p>x</p>")}, &p3, 201)
	triggers()
	triggers()
	n := byTrigger()
	if n["new"] != 1 || n["secret"] != 0 || !ticksOK(n["tick"]) {
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
	if n := byTrigger(); n["status"] != 1 || !ticksOK(n["tick"]) {
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
		map[string]any{"id": "ans", "name": "Answer", "assignees": []map[string]string{{"type": "user", "value": "gus"}},
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

// TestWorkflowBranchesAndHandovers covers parallel branches (fixed and one per value),
// hand-overs of tasks (claim, give back, delegate, ask the starter),
// deputies and forms of steps with field rights.
func TestWorkflowBranchesAndHandovers(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.client()
	admin.login("admin", adminPW)
	const pw = "Passw0rd!Passw0rd"
	var team idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "team"}, &team, 201)
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
	fred, fredID := user("fred")
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
			{"id": "ask", "name": "Region answers", "assignees": []map[string]string{{"type": "user", "value": "gus"}},
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
			{"id": "check", "name": "Check", "assignees": []map[string]string{{"type": "group", "value": "team"}},
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
	// only people who may see the workflow page can take over: the page is
	// for the team and fred only now, so ivy cannot
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d/acl", wf2), map[string]any{"own_rights_only": true}, nil, 200)
	admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/acl", wf2), map[string]any{"subject_type": "group", "subject_id": team.ID, "permissions": 1}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/acl", wf2), map[string]any{"subject_type": "user", "subject_id": fredID, "permissions": 1}, nil, 201)
	act(gus, hc, map[string]any{"action": "_delegate", "to": "ivy"}, 400)
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
		{"id": "check", "name": "Check", "assignees": []map[string]string{{"type": "user", "value": "hal"}}, "actions": []map[string]any{{"id": "ok", "label": "OK", "next": "done"}}},
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

	// ---- several assignees: a person and a group together; any one of them
	// acts. An approval with all its votes (required 0) counts the person
	// and the members of the group together (here ivy, gus and hal).
	wf4 := newWF(map[string]any{"description": "", "fields": []any{}, "steps": []map[string]any{
		{"id": "check", "name": "Check", "assignees": []map[string]string{{"type": "user", "value": "fred"}, {"type": "group", "value": "team"}},
			"actions": []map[string]any{{"id": "ok", "label": "OK", "next": "appr"}}},
		{"id": "appr", "name": "Approve", "type": "approval", "assignees": []map[string]string{{"type": "user", "value": "ivy"}, {"type": "group", "value": fmt.Sprintf("#%d", team.ID)}},
			"required": 0, "next": "done", "next_else": "done"},
		{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}})
	mc := start(wf4, map[string]any{})
	for name, c := range map[string]*client{"fred": fred, "gus": gus, "hal": hal} {
		if x := get(c, mc); !has(x, "ok") || !has(x, "_claim") {
			t.Fatalf("%s may not act on a step for a person and a group: %+v", name, x.Actions)
		}
		if len(tasks(c)) == 0 {
			t.Fatalf("the task is not in the list of %s", name)
		}
	}
	if code := ivy.do("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", mc), nil, nil); code != 404 {
		t.Fatalf("ivy is not an assignee of the first step: %d", code)
	}
	act(gus, mc, map[string]any{"action": "ok"}, 200)
	act(fred, mc, map[string]any{"action": "approve"}, 403) // not an assignee of the approval
	act(ivy, mc, map[string]any{"action": "approve"}, 200)
	act(gus, mc, map[string]any{"action": "approve"}, 200)
	if x := get(admin, mc); x.Status != "open" || x.Step != "appr" {
		t.Fatalf("after two of three votes: %+v", x)
	}
	act(hal, mc, map[string]any{"action": "approve"}, 200)
	if x := get(admin, mc); x.Status != "done" {
		t.Fatalf("after all three votes: %+v", x)
	}

	// ---- the most assignees per step is an expert setting
	three := map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Three", "body": map[string]any{"description": "", "fields": []any{}, "steps": []map[string]any{
		{"id": "check", "name": "Check", "assignees": []map[string]string{{"type": "user", "value": "fred"}, {"type": "user", "value": "gus"}, {"type": "group", "value": "team"}},
			"actions": []map[string]any{{"id": "ok", "label": "OK", "next": "done"}}},
		{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}}
	admin.must("PUT", "/api/v1/admin/limits", map[string]any{"values": map[string]any{"workflow.process.assignees": 2}}, nil, 200)
	if code := admin.do("POST", "/api/v1/elements", three, nil); code != 400 {
		t.Fatalf("three assignees with a limit of 2: %d", code)
	}
	admin.must("PUT", "/api/v1/admin/limits", map[string]any{"values": map[string]any{"workflow.process.assignees": 3}}, nil, 200)
	admin.must("POST", "/api/v1/elements", three, nil, 201)
	admin.must("PUT", "/api/v1/admin/limits", map[string]any{"values": map[string]any{"workflow.process.assignees": 20}}, nil, 200)
}

// TestWorkflowLinksTestRunsRetention covers links in task e-mails (once, only for the
// person, without signing in only when allowed and below confidential),
// test runs, versions, the evaluation and the retention of cases.
func TestWorkflowLinksTestRunsRetention(t *testing.T) {
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
			{"id": "check", "name": "Check", "assignees": []map[string]string{{"type": "user", "value": "gus"}},
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
	// a changed signature (a character in the middle, always different) and
	// a non-canonical spelling of the same signature (other unused bits in
	// the last character) are refused
	i := strings.LastIndex(tok, ".") + 10
	swap := byte('A')
	if tok[i] == 'A' {
		swap = 'B'
	}
	last := tok[len(tok)-1]
	other := strings.IndexByte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", last) ^ 1 // flips an unused bit (32 bytes: 2 bits used)
	for _, bad := range []string{tok[:i] + string(swap) + tok[i+1:],
		tok[:len(tok)-1] + string("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"[other])} {
		if code := guest.do("POST", "/api/v1/x/workflow.process/links/act", map[string]any{"t": bad}, nil); code != 400 {
			t.Fatalf("changed link %q: %d", bad, code)
		}
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
				{"id": "check", "name": "Check", "assignees": []map[string]string{{"type": "user", "value": "lia"}}, "deadline": deadline,
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

func TestWorkflow(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var root cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Jobs"}}, &root, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	var hr idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "hr"}, &hr, 201)
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
			{"id": "screening", "name": "Screening", "assignees": []map[string]string{{"type": "group", "value": "hr"}},
				"actions": []map[string]any{{"id": "accept", "label": "Accept", "next": "hired"}, {"id": "reject", "label": "Reject", "next": "rejected", "note": "required"}}},
			{"id": "hired", "name": "Hired", "actions": []any{}, "outcome": "success"},
			{"id": "rejected", "name": "Rejected", "actions": []any{}, "outcome": "failure"},
		},
	}
	bad := map[string]any{"description": "", "fields": []any{}, "steps": []map[string]any{{"id": "a", "name": "A", "assignees": []map[string]string{{"type": "group", "value": "hr"}},
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

func TestWorkflowStepTypes(t *testing.T) {
	e := setup(t)
	c := &capture{}
	e.app.Mail.SetTransport(c)
	if err := e.app.Config.Update(func(cf *config.Config) error {
		cf.Mail.From = "wiki@example.com"
		cf.Mail.AllowedFrom = []string{"*@hr.example.com"}
		cf.Server.BaseURL = "https://wiki.example.com"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var hook struct {
		sync.Mutex
		bodies []string
		fail   bool
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m := hmac.New(sha256.New, []byte("topsecret"))
		m.Write(b)
		hook.Lock()
		defer hook.Unlock()
		if r.Header.Get("X-Ewiki-Signature") != "sha256="+hex.EncodeToString(m.Sum(nil)) {
			http.Error(w, "bad signature", 401)
			return
		}
		if hook.fail {
			http.Error(w, "down", 503)
			return
		}
		hook.bodies = append(hook.bodies, string(b))
	}))
	defer srv.Close()
	if err := e.app.Config.Update(func(cf *config.Config) error {
		cf.Plugins.Macros.HTTPAllow = []string{srv.URL + "/"}
		cf.Plugins.Macros.HTTPAllowPrivate = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	var root cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Requests"}}, &root, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	var mgr idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "managers"}, &mgr, 201)
	users := map[string]idResp{}
	for _, u := range []struct{ name, email string }{{"ann", "ann@example.com"}, {"ben", "ben@example.com"}, {"cid", "cid@example.com"}} {
		var x idResp
		body := map[string]any{"username": u.name, "password": "Passw0rd!Passw0rd", "display_name": strings.ToUpper(u.name[:1]) + u.name[1:], "email": u.email}
		if u.name != "cid" {
			body["groups"] = []int64{mgr.ID}
		}
		admin.must("POST", "/api/v1/admin/users", body, &x, 201)
		users[u.name] = x
	}
	login := func(n string) *client { c := e.client(); c.login(n, "Passw0rd!Passw0rd"); return c }
	ann, ben, cid := login("ann"), login("ben"), login("cid")

	def := map[string]any{
		"description": "Purchase request",
		"fields": []map[string]any{
			{"id": "item", "label": "Item", "type": "text", "required": true},
			{"id": "amount", "label": "Amount", "type": "number", "required": true},
			{"id": "contact", "label": "Contact", "type": "email", "required": true},
		},
		"steps": []map[string]any{
			{"id": "confirm", "name": "Confirmation", "type": "email", "next": "check",
				"email": map[string]any{"to": "{{field.contact}}, starter", "from": "jobs@hr.example.com", "reply_to": "", "subject": "Request {{case.id}}: {{field.item}}",
					"body": "Hello {{starter.name}},\nwe received {{field.item}} ({{field.amount}} EUR).\n{{case.url}}"}},
			{"id": "check", "name": "Amount check", "type": "condition", "match": "all", "rules": []map[string]any{{"field": "amount", "op": "gt", "value": "1000"}},
				"next": "approve", "next_else": "auto_ok"},
			{"id": "approve", "name": "Four eyes", "type": "approval", "assignees": []map[string]string{{"type": "group", "value": "managers"}}, "required": 0,
				"due_days": 2, "next": "export", "next_else": "rejected_mail"},
			{"id": "export", "name": "Send to ERP", "type": "webhook", "webhook": map[string]any{"url": srv.URL + "/erp", "secret": "topsecret"}, "next": "wait"},
			{"id": "wait", "name": "Delivery time", "type": "delay", "delay": map[string]any{"days": 3}, "next": "approved"},
			{"id": "auto_ok", "name": "Small amount", "type": "end", "outcome": "success"},
			{"id": "approved", "name": "Approved", "type": "end", "outcome": "success"},
			{"id": "rejected_mail", "name": "Rejection", "type": "email", "next": "rejected",
				"email": map[string]any{"to": "starter", "from": "spoof@evil.example", "subject": "Rejected: {{field.item}}", "body": "{{actor.name}} said: {{last.note}}"}},
			{"id": "rejected", "name": "Rejected", "type": "end", "outcome": "failure"},
		},
	}
	// loops of automatic steps are rejected at runtime; invalid definitions at save time
	bad := map[string]any{"fields": []any{}, "steps": []map[string]any{{"id": "c", "name": "C", "type": "condition", "rules": []map[string]any{{"field": "nope", "op": "equals"}}, "next": "c", "next_else": "c"}}}
	if got := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "workflow.process", "title": "Bad", "body": bad}, nil); got != 400 {
		t.Fatalf("condition on unknown field accepted: %d", got)
	}
	var el elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "workflow.process", "title": "Purchase", "body": def}, &el, 201)
	start := func(item string, amount float64) caseResp {
		var x caseResp
		cid.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", el.ID), map[string]any{"data": map[string]any{"item": item, "amount": amount, "contact": "buyer@example.com"}}, &x, 201)
		return x
	}
	// small amount: e-mail + condition (false) -> end
	small := start("Pens", 20)
	if small.Status != "done" || small.Step != "auto_ok" {
		t.Fatalf("small request: %+v", small)
	}
	e.flushMail(t)
	if !c.find("buyer@example.com", "Subject: Request") || !c.find("cid@example.com", "we received Pens (20 EUR)") ||
		!c.find("buyer@example.com", fmt.Sprintf("https://wiki.example.com/e/%d?case=%d", el.ID, small.ID)) {
		t.Fatalf("confirmation e-mail: %d messages", c.count())
	}
	if !c.find("buyer@example.com", "From: <jobs@hr.example.com>") {
		t.Fatalf("allowed sender not used: %+v", c.msgs)
	}
	// big amount: approval by all managers
	big := start("Laptop", 2500)
	if big.Status != "open" || big.Step != "approve" {
		t.Fatalf("big request: %+v", big)
	}
	e.flushMail(t)
	if !c.find("ann@example.com", "New task: Laptop") || !c.find("ben@example.com", "Four eyes") {
		t.Fatalf("task notification missing")
	}
	var x caseResp
	ann.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", big.ID), nil, &x, 200)
	if x.Progress != "0/2" || len(x.Actions) != 2 {
		t.Fatalf("approval state: %+v", x)
	}
	if got := cid.do("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", big.ID), map[string]any{"action": "approve"}, nil); got != 403 {
		t.Fatalf("starter approves: %d", got)
	}
	ann.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", big.ID), map[string]any{"action": "approve"}, &x, 200)
	if x.Step != "approve" || x.Progress != "1/2" {
		t.Fatalf("after first approval: %+v", x)
	}
	if got := ann.do("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", big.ID), map[string]any{"action": "approve"}, nil); got != 409 {
		t.Fatalf("double vote: %d", got)
	}
	hook.fail = true
	ben.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", big.ID), map[string]any{"action": "approve"}, &x, 200)
	if x.Step != "export" || x.Status != "open" {
		t.Fatalf("after second approval: %+v", x)
	}
	// webhook fails three times -> waits for a manager
	for i := 0; i < 3; i++ {
		if _, err := e.app.DB.Exec(context.Background(), `UPDATE plugin_workflow_instances SET wake_at=now() WHERE id=$1 AND wake_at IS NOT NULL`, big.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.app.Workers.RunOnce(context.Background(), "workflow.process.timers"); err != nil {
			t.Fatal(err)
		}
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", big.ID), nil, &x, 200)
	if x.Step != "export" || !strings.Contains(x.LastError, "503") || len(x.Actions) != 2 || x.Actions[0].ID != "_retry" {
		t.Fatalf("failed webhook: %+v", x)
	}
	hook.fail = false
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", big.ID), map[string]any{"action": "_retry"}, nil, 200)
	if err := e.app.Workers.RunOnce(context.Background(), "workflow.process.timers"); err != nil {
		t.Fatal(err)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", big.ID), nil, &x, 200)
	if x.Step != "wait" || len(hook.bodies) != 1 || !strings.Contains(hook.bodies[0], `"item":"Laptop"`) {
		t.Fatalf("webhook retry: %+v %v", x, hook.bodies)
	}
	// delay: nothing happens before the time, then the case continues
	if err := e.app.Workers.RunOnce(context.Background(), "workflow.process.timers"); err != nil {
		t.Fatal(err)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", big.ID), nil, &x, 200)
	if x.Step != "wait" {
		t.Fatalf("delay ended early: %+v", x)
	}
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE plugin_workflow_instances SET wake_at=now() - interval '1 minute' WHERE id=$1`, big.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Workers.RunOnce(context.Background(), "workflow.process.timers"); err != nil {
		t.Fatal(err)
	}
	cid.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", big.ID), nil, &x, 200)
	if x.Status != "done" || x.Outcome != "success" {
		t.Fatalf("after delay: %+v", x)
	}
	// rejection: one veto, e-mail with actor and note; the spoofed sender becomes Reply-To
	other := start("Yacht", 99999)
	ben.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", other.ID), map[string]any{"action": "reject", "note": "Too expensive"}, &x, 200)
	if x.Status != "done" || x.Step != "rejected" || x.Outcome != "failure" {
		t.Fatalf("after rejection: %+v", x)
	}
	e.flushMail(t)
	if !c.find("cid@example.com", "Ben said: Too expensive") || !c.find("cid@example.com", "Reply-To: <spoof@evil.example>") || c.find("cid@example.com", "From: <spoof@evil.example>") {
		t.Fatal("rejection e-mail wrong")
	}
	if !c.find("cid@example.com", "Completed: Yacht") {
		t.Fatal("starter not notified about the end")
	}
	// reminders: overdue approval reminds those who did not vote yet
	third := start("Desk", 1500)
	ann.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", third.ID), map[string]any{"action": "approve"}, nil, 200)
	before := c.count()
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE plugin_workflow_instances SET step_entered_at=now() - interval '3 days' WHERE id=$1`, third.ID); err != nil {
		t.Fatal(err)
	}
	// the approval of ann must still count after moving the step start back
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE plugin_workflow_events SET at=now() - interval '2 days' WHERE instance_id=$1 AND action='approve'`, third.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Workers.RunOnce(context.Background(), "workflow.process.timers"); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	if !c.find("ben@example.com", "Reminder: Desk") || c.find("ann@example.com", "Reminder: Desk") {
		t.Fatalf("reminder: %d new messages", c.count()-before)
	}
	// notification preferences
	var prefs struct {
		Prefs map[string]bool `json:"prefs"`
	}
	ben.must("PUT", "/api/v1/me/notifications", map[string]bool{"tasks": false}, nil, 204)
	ben.must("GET", "/api/v1/me/notifications", nil, &prefs, 200)
	if prefs.Prefs["tasks"] || !prefs.Prefs["comments"] {
		t.Fatalf("prefs %+v", prefs)
	}
	start("Chair", 2000)
	e.flushMail(t)
	if c.find("ben@example.com", "New task: Chair") || !c.find("ann@example.com", "New task: Chair") {
		t.Fatal("preference ignored")
	}
	var meta struct {
		Placeholders   []string `json:"placeholders"`
		MailConfigured bool     `json:"mail_configured"`
	}
	ann.must("GET", "/api/v1/x/workflow.process/meta", nil, &meta, 200)
	if !meta.MailConfigured || len(meta.Placeholders) < 10 {
		t.Fatalf("meta %+v", meta)
	}
}

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

type caseResp struct {
	ID       int64  `json:"id"`
	Step     string `json:"step"`
	Status   string `json:"status"`
	Outcome  string `json:"outcome"`
	Progress string `json:"progress"`
	Actions  []struct {
		ID string `json:"id"`
	} `json:"actions"`
	LastError string `json:"last_error"`
	Events    []struct {
		Action string `json:"action"`
		Label  string `json:"action_label"`
		Note   string `json:"note"`
	} `json:"events"`
}
