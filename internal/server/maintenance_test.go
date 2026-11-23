package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// raw503 sends a request and gives status, body and headers (also for 5xx,
// which client.do counts as an error).
func (c *client) raw503(method, path string, body any) (int, string, http.Header) {
	c.e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "ewiki")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(data), res.Header
}

// TestMaintenance: read-only lets everybody read and only administrators
// change; closed lets only administrators in; signing in always works;
// background jobs wait; only administrators switch it.
func TestMaintenance(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var c cat
	var bob struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Open"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": false}, nil, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Passw0rd!Passw0rd"}, &bob, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": bob.ID, "permissions": 7}, nil, 201)
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Hours", "body": htmlBody("<p>Open from okapi to lynx</p>")}, &page, 201)
	edit := func(cl *client, text string) (int, string) {
		st, body, _ := cl.raw503("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Hours", "body": htmlBody("<p>" + text + "</p>")})
		return st, body
	}
	user := e.client()
	user.login("bob", "Passw0rd!Passw0rd")
	if st, _ := edit(user, "before"); st != 200 {
		t.Fatalf("edit before maintenance: %d", st)
	}
	// only administrators switch it; values are checked
	user.must("PUT", "/api/v1/admin/maintenance", map[string]any{"mode": "closed"}, nil, 403)
	admin.must("PUT", "/api/v1/admin/maintenance", map[string]any{"mode": "later"}, nil, 400)
	admin.must("PUT", "/api/v1/admin/maintenance", map[string]any{"mode": "read_only", "until": "tomorrow"}, nil, 400)
	admin.must("PUT", "/api/v1/admin/maintenance", map[string]any{"mode": "read_only", "message": map[string]string{"en": strings.Repeat("x", 2001)}}, nil, 400)
	if e.app.Workers.Paused() {
		t.Fatal("jobs paused without maintenance")
	}

	// read-only
	admin.must("PUT", "/api/v1/admin/maintenance", map[string]any{"mode": "read_only", "message": map[string]string{"en": "Moving the server.", "de": " "}, "until": "2030-01-01T18:00:00+01:00"}, nil, 200)
	var site struct {
		Maintenance struct {
			Mode    string            `json:"mode"`
			Message map[string]string `json:"message"`
		} `json:"maintenance"`
	}
	guest := e.client()
	guest.must("GET", "/api/v1/site", nil, &site, 200)
	if site.Maintenance.Mode != "read_only" || site.Maintenance.Message["en"] != "Moving the server." || len(site.Maintenance.Message) != 1 {
		t.Fatalf("site: %+v", site.Maintenance)
	}
	st, body := edit(user, "during")
	if st != 503 || !strings.Contains(body, `"maintenance"`) {
		t.Fatalf("edit while read-only: %d %s", st, body)
	}
	if st, _, hdr := user.raw503("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", page.ID), map[string]any{"body": "hi"}); st != 503 || hdr.Get("Retry-After") != "86400" {
		t.Fatalf("comment while read-only: %d %q", st, hdr.Get("Retry-After"))
	}
	if st, _, _ := user.raw503("GET", fmt.Sprintf("/api/v1/collab/elements/%d/en", page.ID), nil); st != 503 {
		t.Fatalf("live editing while read-only: %d", st)
	}
	user.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 200)
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 200)
	// signing in and out still works; administrators still change
	again := e.client()
	again.login("bob", "Passw0rd!Passw0rd")
	again.must("POST", "/api/v1/auth/logout", nil, nil, 204)
	if st, body := edit(admin, "by the administrator"); st != 200 {
		t.Fatalf("administrator while read-only: %d %s", st, body)
	}
	if !e.app.Workers.Paused() {
		t.Fatal("jobs run during maintenance")
	}
	admin.must("PUT", "/api/v1/admin/maintenance", map[string]any{"mode": "read_only", "run_jobs": true}, nil, 200)
	if e.app.Workers.Paused() {
		t.Fatal("jobs paused although they should run")
	}

	if html, _ := e.guestPage(fmt.Sprintf("/e/%d", page.ID), ""); !strings.Contains(html, "by the administrator") {
		t.Fatalf("guests read the page while read-only:\n%s", html)
	}
	// closed: only what is needed to say so and to sign in
	admin.must("PUT", "/api/v1/admin/maintenance", map[string]any{"mode": "closed"}, nil, 200)
	for _, cl := range []*client{guest, user} {
		if st, _, _ := cl.raw503("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil); st != 503 {
			t.Fatalf("reading while closed: %d", st)
		}
		if st, _, _ := cl.raw503("GET", "/api/v1/search?q=okapi", nil); st != 503 {
			t.Fatalf("searching while closed: %d", st)
		}
		cl.must("GET", "/api/v1/site", nil, nil, 200)
		cl.must("GET", "/api/v1/i18n/en", nil, nil, 200)
		cl.must("GET", "/api/v1/auth/me", nil, nil, 200)
	}
	if st, _, _ := guest.raw503("POST", "/scim/v2/Users", map[string]any{}); st != 503 {
		t.Fatalf("SCIM while closed: %d", st)
	}
	late := e.client()
	late.login("bob", "Passw0rd!Passw0rd")
	if st, _, _ := late.raw503("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil); st != 503 {
		t.Fatalf("reading after signing in while closed: %d", st)
	}
	// the page for guests is the plain application: no content
	html, _ := e.guestPage(fmt.Sprintf("/e/%d", page.ID), "")
	if strings.Contains(html, "by the administrator") {
		t.Fatalf("content shown while closed:\n%s", html)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 200)
	admin.must("GET", "/api/v1/admin/maintenance", nil, nil, 200)

	// off again: back to normal
	admin.must("PUT", "/api/v1/admin/maintenance", map[string]any{"mode": ""}, nil, 200)
	if st, body := edit(user, "after"); st != 200 {
		t.Fatalf("edit after maintenance: %d %s", st, body)
	}
	var after map[string]any
	guest.must("GET", "/api/v1/site", nil, &after, 200)
	if after["maintenance"] != nil {
		t.Fatalf("site after maintenance: %v", after["maintenance"])
	}
}
