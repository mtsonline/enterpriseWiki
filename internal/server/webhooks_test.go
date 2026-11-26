package server_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

func TestWebhooksCommentsWorkflows(t *testing.T) {
	e := setup(t)
	if err := e.app.Config.Update(func(c *config.Config) error { c.Webhooks.AllowPrivate = true; return nil }); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []map[string]any
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev map[string]any
		_ = json.NewDecoder(r.Body).Decode(&ev)
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	}))
	t.Cleanup(recv.Close)
	events := func() map[string]map[string]any {
		t.Helper()
		if err := e.app.Workers.RunOnce(context.Background(), "webhooks"); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		out := map[string]map[string]any{}
		for _, ev := range got {
			out[ev["event"].(string)] = ev
		}
		got = nil
		return out
	}
	admin := e.client()
	admin.login("admin", adminPW)
	var root cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &root, 201)
	admin.must("POST", "/api/v1/admin/webhooks", map[string]any{"name": "All", "url": recv.URL, "format": "json",
		"events": []string{"comment.create", "workflow.started", "workflow.completed", "workflow.cancelled"}}, nil, 201)

	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Handbook", "body": htmlBody("<p>Hi</p>")}, &page, 201)
	var c struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", page.ID), map[string]any{"body": "Please   check\nchapter 2"}, &c, 201)
	ev := events()["comment.create"]
	if ev == nil {
		t.Fatalf("no comment webhook")
	}
	obj := ev["object"].(map[string]any)
	data := ev["data"].(map[string]any)
	if obj["title"] != "Handbook" || !strings.HasSuffix(obj["url"].(string), fmt.Sprintf("/e/%d#comment-%d", page.ID, c.ID)) || data["excerpt"] != "Please check chapter 2" || data["reply"] != false {
		t.Fatalf("comment webhook: %+v", ev)
	}

	def := map[string]any{"description": "", "fields": []map[string]any{{"id": "subject", "label": "Subject", "type": "text", "required": true}},
		"steps": []map[string]any{
			{"id": "check", "name": "Check", "assignee": map[string]string{"type": "starter"},
				"actions": []map[string]any{{"id": "ok", "label": "OK", "next": "done"}}},
			{"id": "done", "name": "Done", "actions": []any{}, "outcome": "success"},
		}}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "workflow.process", "title": "Request", "body": def}, &wf, 201)
	var x struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{"subject": "Laptop"}}, &x, 201)
	ev = events()["workflow.started"]
	if ev == nil || ev["object"].(map[string]any)["type"] != "workflow_case" || ev["data"].(map[string]any)["workflow"] != "Request" {
		t.Fatalf("started webhook: %+v", ev)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", x.ID), map[string]any{"action": "ok"}, nil, 200)
	ev = events()["workflow.completed"]
	if ev == nil || ev["data"].(map[string]any)["outcome"] != "success" || !strings.HasSuffix(ev["object"].(map[string]any)["url"].(string), fmt.Sprintf("/e/%d?case=%d", wf.ID, x.ID)) {
		t.Fatalf("completed webhook: %+v", ev)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{"subject": "Phone"}}, &x, 201)
	events()
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/cancel", x.ID), map[string]any{}, nil, 200)
	if ev = events()["workflow.cancelled"]; ev == nil || ev["data"].(map[string]any)["status"] != "cancelled" {
		t.Fatalf("cancelled webhook: %+v", ev)
	}
}

func TestWebhooks(t *testing.T) {
	e := setup(t)
	if err := e.app.Config.Update(func(c *config.Config) error { c.Webhooks.AllowPrivate = true; return nil }); err != nil {
		t.Fatal(err)
	}
	type req struct {
		header http.Header
		body   []byte
		path   string
	}
	var mu sync.Mutex
	var got []req
	fail := false
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		got = append(got, req{r.Header.Clone(), b, r.URL.Path})
		if fail {
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(recv.Close)
	deliver := func() {
		t.Helper()
		if err := e.app.Workers.RunOnce(context.Background(), "webhooks"); err != nil {
			t.Fatal(err)
		}
	}
	received := func(path string) []req {
		mu.Lock()
		defer mu.Unlock()
		var out []req
		for _, r := range got {
			if r.path == path {
				out = append(out, r)
			}
		}
		return out
	}
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "max", "password": "Passw0rd!Passw0rd", "display_name": "Max", "email": "max@example.com"}, nil, 201)
	max := e.client()
	max.login("max", "Passw0rd!Passw0rd")
	if code := max.do("GET", "/api/v1/admin/webhooks", nil, nil); code != 403 {
		t.Fatalf("webhooks for non-admins: %d", code)
	}
	var docs, other cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Docs"}}, &docs, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Other"}}, &other, 201)
	if code := admin.do("POST", "/api/v1/admin/webhooks", map[string]any{"name": "x", "url": "ftp://x", "format": "json"}, nil); code != 400 {
		t.Fatalf("invalid url: %d", code)
	}
	var hook struct {
		ID     int64  `json:"id"`
		Secret string `json:"secret"`
	}
	admin.must("POST", "/api/v1/admin/webhooks", map[string]any{"name": "Integration", "url": recv.URL + "/json", "format": "json", "secret": "generate",
		"events": []string{"element.create", "element.update"}, "category_id": docs.ID}, &hook, 201)
	if len(hook.Secret) != 48 {
		t.Fatalf("secret: %+v", hook)
	}
	admin.must("POST", "/api/v1/admin/webhooks", map[string]any{"name": "Teams", "url": recv.URL + "/teams", "format": "teams"}, nil, 201)
	admin.must("POST", "/api/v1/admin/webhooks", map[string]any{"name": "Slack", "url": recv.URL + "/slack", "format": "slack", "events": []string{"element.update"}}, nil, 201)

	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": docs.ID, "type": "core.html", "title": "Onboarding", "body": htmlBody("<p>x</p>")}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": other.ID, "type": "core.html", "title": "Elsewhere", "body": htmlBody("<p>x</p>")}, nil, 201)
	deliver()
	js := received("/json")
	if len(js) != 1 {
		t.Fatalf("json deliveries: %d", len(js))
	}
	var ev struct {
		Event  string `json:"event"`
		Actor  string `json:"actor"`
		Object struct {
			Type  string `json:"type"`
			ID    int64  `json:"id"`
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"object"`
	}
	if err := json.Unmarshal(js[0].body, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Event != "element.create" || ev.Object.ID != page.ID || ev.Object.Title != "Onboarding" || !strings.HasSuffix(ev.Object.URL, fmt.Sprintf("/e/%d", page.ID)) || ev.Actor != "Admin" {
		t.Fatalf("payload: %s", js[0].body)
	}
	mac := hmac.New(sha256.New, []byte(hook.Secret))
	mac.Write(js[0].body)
	if js[0].header.Get("X-Ewiki-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) || js[0].header.Get("X-Ewiki-Event") != "element.create" {
		t.Fatalf("headers: %v", js[0].header)
	}
	tm := received("/teams")
	if len(tm) != 2 || !strings.Contains(string(tm[0].body), "application/vnd.microsoft.card.adaptive") || !strings.Contains(string(tm[0].body)+string(tm[1].body), "Onboarding") {
		t.Fatalf("teams: %d %s", len(tm), tm)
	}
	if len(received("/slack")) != 0 {
		t.Fatal("slack got an event it did not subscribe to")
	}
	// update → slack; failures are retried later
	fail = true
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Onboarding guide", "body": htmlBody("<p>y</p>"), "base_rev": 1}, nil, 200)
	deliver()
	sl := received("/slack")
	if len(sl) != 1 || !strings.Contains(string(sl[0].body), `"text"`) || !strings.Contains(string(sl[0].body), "Onboarding guide") {
		t.Fatalf("slack: %v", sl)
	}
	var detail struct {
		Deliveries []struct {
			ID       int64  `json:"id"`
			Status   string `json:"status"`
			Attempts int    `json:"attempts"`
		} `json:"deliveries"`
	}
	var list struct {
		Webhooks []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			LastStatus int    `json:"last_status"`
			Pending    int    `json:"pending"`
		} `json:"webhooks"`
	}
	admin.must("GET", "/api/v1/admin/webhooks", nil, &list, 200)
	var slackID int64
	for _, w := range list.Webhooks {
		if w.Name == "Slack" && w.LastStatus == 500 && w.Pending == 1 {
			slackID = w.ID
		}
	}
	if slackID == 0 {
		t.Fatalf("failed delivery not pending: %+v", list)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/admin/webhooks/%d", slackID), nil, &detail, 200)
	if len(detail.Deliveries) != 1 || detail.Deliveries[0].Status != "pending" || detail.Deliveries[0].Attempts != 1 {
		t.Fatalf("deliveries: %+v", detail)
	}
	fail = false
	admin.must("POST", fmt.Sprintf("/api/v1/admin/webhooks/%d/deliveries/%d/retry", slackID, detail.Deliveries[0].ID), nil, nil, 204)
	deliver()
	admin.must("GET", fmt.Sprintf("/api/v1/admin/webhooks/%d", slackID), nil, &detail, 200)
	if detail.Deliveries[0].Status != "delivered" {
		t.Fatalf("after retry: %+v", detail)
	}
	// test event, disabled hooks, deletion
	admin.must("POST", fmt.Sprintf("/api/v1/admin/webhooks/%d/test", hook.ID), nil, nil, 204)
	deliver()
	if n := len(received("/json")); n != 3 { // create, failed update, test
		t.Fatalf("test event: %d", n)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/webhooks/%d", hook.ID), map[string]any{"name": "Integration", "url": recv.URL + "/json", "format": "json", "enabled": false}, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Onboarding v2", "body": htmlBody("<p>z</p>"), "base_rev": 2}, nil, 200)
	deliver()
	if n := len(received("/json")); n != 3 {
		t.Fatalf("disabled webhook delivered: %d", n)
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/admin/webhooks/%d", hook.ID), nil, nil, 204)
}
