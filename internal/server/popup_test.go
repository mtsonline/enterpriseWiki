package server_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestPopupNotifications: a workflow message marked as pop-up reaches only
// its recipient, as plain text cut to its length, at most core.notify.popups
// at once; once read it is no pop-up any more but stays in the bell.
func TestPopupNotifications(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.client()
	admin.login("admin", adminPW)
	const pw = "Passw0rd!Passw0rd"
	user := func(name string) *client {
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": pw, "display_name": name, "email": name + "@example.com", "roles": []int64{}}, nil, 201)
		c := e.client()
		c.login(name, pw)
		return c
	}
	gus, hal := user("gus"), user("hal")
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Requests"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	end := map[string]any{"id": "done", "name": "Done", "type": "end", "outcome": "success"}
	def := func(channel string) map[string]any {
		return map[string]any{"fields": []map[string]any{{"id": "name", "label": "Name", "type": "text", "required": true}},
			"steps": []map[string]any{{"id": "tell", "name": "Tell", "type": "notify", "next": "done",
				"notify": map[string]any{"to": "starter", "channel": channel, "popup": true, "subject": "Result for {{field.name}}",
					"body": "Line one\n<b>no HTML</b>\n" + strings.Repeat("x", 3000)}}, end}}
	}
	// a pop-up is a message in the wiki: not by e-mail only
	if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "workflow.process", "title": "Bad", "body": def("email")}, nil); code != 400 {
		t.Fatalf("pop-up by e-mail only: %d", code)
	}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "workflow.process", "title": "Request", "body": def("inbox")}, &wf, 201)
	for i := 0; i < 5; i++ {
		gus.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{"name": fmt.Sprintf("Ann %d", i)}}, nil, 201)
	}
	for i := 0; i < 3; i++ {
		if err := e.app.Workers.RunOnce(ctx, "workflow.process.timers"); err != nil {
			t.Fatal(err)
		}
	}
	type item struct {
		ID     int64  `json:"id"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Link   string `json:"link"`
		Popup  bool   `json:"popup"`
		ReadAt *string
	}
	var res struct {
		Items []item `json:"items"`
	}
	gus.must("GET", "/api/v1/me/inbox/popups", nil, &res, 200)
	// at most core.notify.popups (3) at once, the oldest first
	if len(res.Items) != 3 || res.Items[0].Title != "Result for Ann 0" || !res.Items[0].Popup || !strings.HasPrefix(res.Items[0].Link, "/e/") {
		t.Fatalf("pop-ups of the starter: %+v", res.Items)
	}
	// plain text, cut to its length
	if b := res.Items[0].Body; !strings.Contains(b, "<b>no HTML</b>") || utf8.RuneCountInString(b) != 2000 {
		t.Fatalf("text of the pop-up: %d characters: %.80q", utf8.RuneCountInString(b), b)
	}
	// only the recipient: others see none
	for _, c := range []*client{hal, admin} {
		var other struct {
			Items []item `json:"items"`
		}
		c.must("GET", "/api/v1/me/inbox/popups", nil, &other, 200)
		if len(other.Items) != 0 {
			t.Fatalf("pop-ups of somebody else: %+v", other.Items)
		}
	}
	if code := e.client().do("GET", "/api/v1/me/inbox/popups", nil, nil); code != 401 {
		t.Fatalf("pop-ups for a guest: %d", code)
	}
	// shown once: read, no pop-up any more, still in the bell
	ids := []int64{}
	for _, x := range res.Items {
		ids = append(ids, x.ID)
	}
	gus.must("POST", "/api/v1/me/inbox/read", map[string]any{"ids": ids}, nil, 204)
	gus.must("GET", "/api/v1/me/inbox/popups", nil, &res, 200)
	if len(res.Items) != 2 || res.Items[0].Title != "Result for Ann 3" {
		t.Fatalf("pop-ups after reading: %+v", res.Items)
	}
	var inbox struct {
		Items  []item `json:"items"`
		Unread int    `json:"unread"`
	}
	gus.must("GET", "/api/v1/me/inbox", nil, &inbox, 200)
	if len(inbox.Items) != 5 || inbox.Unread != 2 || !inbox.Items[0].Popup {
		t.Fatalf("bell after the pop-ups: %d items, %d unread", len(inbox.Items), inbox.Unread)
	}
}
