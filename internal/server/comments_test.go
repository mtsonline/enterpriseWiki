package server_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

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
	var audit struct {
		Entries []struct {
			Action string `json:"action"`
		} `json:"entries"`
	}
	admin.must("GET", "/api/v1/admin/audit/search?entity_type=comment", nil, &audit, 200)
	if len(audit.Entries) < 5 {
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

func TestCommentNotifications(t *testing.T) {
	e := setup(t)
	c := &capture{}
	e.app.Mail.SetTransport(c)
	admin := e.client()
	admin.login("admin", adminPW)
	var root, secret cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &root, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": root.ID, "titles": map[string]string{"en": "Board"}}, &secret, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root.ID), map[string]any{"subject_type": "authenticated", "permissions": 7, "inherit": false}, nil, 201)
	for _, n := range []string{"amy", "bo"} {
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": n, "password": "Passw0rd!Passw0rd", "display_name": n, "email": n + "@example.com"}, nil, 201)
	}
	amy, bo := e.client(), e.client()
	amy.login("amy", "Passw0rd!Passw0rd")
	bo.login("bo", "Passw0rd!Passw0rd")
	var el elem
	amy.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Plan", "body": htmlBody("<p>x</p>")}, &el, 201)
	var c1 struct {
		ID int64 `json:"id"`
	}
	bo.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", el.ID), map[string]any{"body": "Looks good"}, &c1, 201)
	amy.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", el.ID), map[string]any{"body": "Thanks!", "parent_id": c1.ID}, nil, 201)
	e.flushMail(t)
	if !c.find("amy@example.com", "bo commented on") || !c.find("bo@example.com", "amy commented on") || c.find("amy@example.com", "Thanks!") {
		t.Fatalf("comment notifications: %d", c.count())
	}
	// a page in a hidden category: commenters' notifications only go to users who can see it
	var hidden elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "core.html", "title": "Secret", "body": htmlBody("<p>x</p>")}, &hidden, 201)
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE elements SET created_by=(SELECT id FROM users WHERE username='bo') WHERE id=$1`, hidden.ID); err != nil {
		t.Fatal(err)
	}
	n := c.count()
	admin.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", hidden.ID), map[string]any{"body": "internal"}, nil, 201)
	e.flushMail(t)
	if c.count() != n {
		t.Fatal("notification about a page the recipient cannot see")
	}
}
