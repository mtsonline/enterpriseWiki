package server_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestWorkflow3Accounts covers the events of accounts (from every source,
// via the change log) as workflow triggers and the steps for permissions
// (with expiry), group membership and moving pages – with the rights of
// the definer only.
func TestWorkflow3Accounts(t *testing.T) {
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
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Newcomers"}, &grp, 200)
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
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Others"}, &other, 200)
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
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/groups/%d/members/%d", other.ID, nia.ID), nil, nil, 200)
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
			{"id": "join", "name": "Join", "type": "membership", "next": "done", "membership": map[string]any{"mode": "add", "group": grp.ID, "user": "{{field.who}}"}},
			{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}}, &wfE, 201)
	var c wfCase
	eve.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wfE.ID), map[string]any{"data": map[string]any{"who": "eve"}}, &c, 201)
	run("workflow.process.timers")
	eve.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", c.ID), nil, &x, 200)
	if x.Status != "open" || !strings.Contains(x.LastError, "403") {
		t.Fatalf("membership with eve's rights: %+v", x)
	}
	_ = e.app.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_groups WHERE user_id=$1 AND group_id=$2)`, eveID.ID, grp.ID).Scan(&member)
	if member {
		t.Fatal("eve put herself into a group")
	}

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
