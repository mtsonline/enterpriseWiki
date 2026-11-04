package server_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// calendarParts returns the decoded text/calendar parts of the e-mails to addr.
func (c *capture) calendarParts(addr string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	re := regexp.MustCompile(`(?s)Content-Type: text/calendar; charset=utf-8; method=(\w+)\r\nContent-Transfer-Encoding: base64\r\n\r\n(.*?)\r\n--`)
	var out []string
	for _, m := range c.msgs {
		for _, r := range m.Recipients {
			if r != addr {
				continue
			}
			for _, p := range re.FindAllStringSubmatch(string(m.Data), -1) {
				b, _ := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(p[2]), ""))
				out = append(out, p[1]+"\n"+strings.ReplaceAll(string(b), "\r\n ", ""))
			}
		}
	}
	return out
}

func TestEventInvitations(t *testing.T) {
	e := setup(t)
	c := &capture{}
	e.app.Mail.SetTransport(c)
	if err := e.app.Config.Update(func(cfg *config.Config) error { cfg.Mail.From = "wiki@example.com"; return nil }); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "eva", "password": "Passw0rd!Passw0rd", "display_name": "Eva", "email": "eva@example.com"}, nil, 201)
	eva := e.client()
	eva.login("eva", "Passw0rd!Passw0rd")
	var team cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &team, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", team.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	var calEl elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": team.ID, "type": "content.events", "title": "Courses", "body": map[string]any{"view": "list"}}, &calEl, 201)
	vienna := mustLoc("Europe/Vienna")
	now := time.Now().In(vienna)
	start := time.Date(now.Year(), now.Month(), now.Day(), 18, 0, 0, 0, vienna).AddDate(0, 0, 3)
	var ev struct {
		ID  int64  `json:"id"`
		UID string `json:"uid"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/x/content.events/calendars/%d/events", calEl.ID), map[string]any{"title": "Yoga", "starts_at": start, "ends_at": start.Add(time.Hour),
		"tz": "Europe/Vienna", "location": "Hall", "registration": true}, &ev, 201)
	date := start.Format("2006-01-02")
	reg := fmt.Sprintf("/api/v1/x/content.events/events/%d/registrations", ev.ID)
	eva.must("POST", reg, map[string]any{"occurrence": date}, nil, 200)
	e.flushMail(t)
	parts := c.calendarParts("eva@example.com")
	if len(parts) != 1 {
		t.Fatalf("invitations: %d", len(parts))
	}
	inv := parts[0]
	for _, want := range []string{"REQUEST\n", "METHOD:REQUEST", "UID:" + ev.UID + "-" + date, "BEGIN:VTIMEZONE", "DTSTART;TZID=Europe/Vienna:" + start.Format("20060102T150405"),
		"SUMMARY:Yoga", "LOCATION:Hall", "ORGANIZER;CN=\"Courses\":mailto:wiki@example.com", "mailto:eva@example.com", "STATUS:CONFIRMED"} {
		if !strings.Contains(inv, want) {
			t.Errorf("invitation lacks %q:\n%s", want, inv)
		}
	}
	// the whole event moves → updated invitation
	moved := start.Add(2 * time.Hour)
	admin.must("PUT", fmt.Sprintf("/api/v1/x/content.events/events/%d", ev.ID), map[string]any{"title": "Yoga", "starts_at": moved, "ends_at": moved.Add(time.Hour),
		"tz": "Europe/Vienna", "location": "Hall", "registration": true}, nil, 200)
	e.flushMail(t)
	parts = c.calendarParts("eva@example.com")
	if len(parts) != 2 || !strings.Contains(parts[1], "DTSTART;TZID=Europe/Vienna:"+moved.Format("20060102T150405")) || !strings.Contains(parts[1], "UID:"+ev.UID+"-"+date) {
		t.Fatalf("update: %v", parts)
	}
	// unregister → CANCEL removes the entry
	eva.must("DELETE", reg+"?occurrence="+date, nil, nil, 204)
	e.flushMail(t)
	parts = c.calendarParts("eva@example.com")
	if len(parts) != 3 || !strings.HasPrefix(parts[2], "CANCEL\n") || !strings.Contains(parts[2], "STATUS:CANCELLED") || !strings.Contains(parts[2], "UID:"+ev.UID+"-"+date) {
		t.Fatalf("cancel: %v", parts)
	}
}

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

func TestPreviewImage(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var pub cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Public"}}, &pub, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", pub.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": false}, nil, 201)
	img := "/api/v1/files/" + strings.Repeat("ab", 32)
	var withImg, plain elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "core.html", "title": "Team",
		"body": htmlBody(`<p>Hello</p><p><img src="data:image/png;base64,AAAA" alt=""><img src="` + img + `" alt="Team photo"></p>`)}, &withImg, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "core.html", "title": "Plain", "body": htmlBody("<p>Text only</p>")}, &plain, 201)
	ogImage := func(path string) string {
		t.Helper()
		body, _ := e.guestPage(path, "")
		m := regexp.MustCompile(`<meta property="og:image" content="([^"]*)">`).FindStringSubmatch(body)
		if m == nil {
			return ""
		}
		if !strings.Contains(body, `<meta name="twitter:card" content="summary_large_image">`) {
			t.Errorf("no large twitter card on %s", path)
		}
		return m[1]
	}
	// automatic: first loadable image of the page (absolute URL)
	if got := ogImage(fmt.Sprintf("/e/%d", withImg.ID)); !strings.HasPrefix(got, "http") || !strings.HasSuffix(got, img) {
		t.Fatalf("automatic preview image: %q", got)
	}
	if got := ogImage(fmt.Sprintf("/e/%d", plain.ID)); got != "" {
		t.Fatalf("page without image has preview %q", got)
	}
	// own image per page, validated
	if code := admin.do("PATCH", fmt.Sprintf("/api/v1/elements/%d", plain.ID), map[string]any{"preview_image": "javascript:alert(1)"}, nil); code != 400 {
		t.Fatalf("invalid preview image accepted: %d", code)
	}
	var got elem
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", plain.ID), map[string]any{"preview_image": "https://cdn.example.com/cover.jpg"}, nil, 200)
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", plain.ID), nil, &got, 200)
	if got.PreviewImage != "https://cdn.example.com/cover.jpg" {
		t.Fatalf("preview image not stored: %+v", got)
	}
	if g := ogImage(fmt.Sprintf("/e/%d", plain.ID)); g != "https://cdn.example.com/cover.jpg" {
		t.Fatalf("own preview image: %q", g)
	}
	// site default for pages without an image
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", plain.ID), map[string]any{"preview_image": ""}, nil, 200)
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.UI.PreviewImage = "/api/v1/files/" + strings.Repeat("cd", 32)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e.app.Cache.Invalidate(context.Background(), "test")
	if g := ogImage(fmt.Sprintf("/e/%d", plain.ID)); !strings.HasSuffix(g, strings.Repeat("cd", 32)) {
		t.Fatalf("site preview image: %q", g)
	}
}

func TestPluginsLiteImage(t *testing.T) {
	t.Setenv("EWIKI_IMAGE", "lite")
	t.Setenv("EWIKI_DATA", t.TempDir())
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var st struct {
		Mode             string   `json:"mode"`
		CanBuild         bool     `json:"can_build"`
		BuildUnavailable string   `json:"build_unavailable"`
		Removable        []string `json:"removable"`
	}
	admin.must("GET", "/api/v1/admin/plugins", nil, &st, 200)
	if st.Mode != "lite" || st.CanBuild || st.BuildUnavailable != "lite" || len(st.Removable) != 0 {
		t.Fatalf("lite status: %+v", st)
	}
	if code := admin.do("POST", "/api/v1/admin/plugins/install", map[string]any{"source": "github.com/x/y"}, nil); code != 400 {
		t.Fatalf("install in the lite image: %d", code)
	}
	if code := admin.do("POST", "/api/v1/admin/plugins/build", nil, nil); code != 400 {
		t.Fatalf("build in the lite image: %d", code)
	}
	if code := admin.do("DELETE", "/api/v1/admin/plugins/core.html", nil, nil); code != 400 && code != 404 {
		t.Fatalf("remove a bundled plugin: %d", code)
	}
	var opt struct {
		Auto bool `json:"auto_restart_on_drop"`
	}
	admin.must("PUT", "/api/v1/admin/plugins/options", map[string]any{"auto_restart_on_drop": true}, &opt, 200)
	if !opt.Auto || !e.app.Config.Get().Plugins.AutoRestartOnDrop {
		t.Fatal("option not saved")
	}
}
