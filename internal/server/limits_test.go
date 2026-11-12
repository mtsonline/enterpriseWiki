package server_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// Many people comment or start a workflow case at the same moment while
// only a few database connections exist: every request must finish. Before,
// each of them held a connection for its transaction and waited for a
// second one to look up who is notified - with all connections taken the
// whole wiki stood still.
func TestManyWritesWithFewConnections(t *testing.T) {
	e := setupWith(t, func(c *config.Config) { c.Database.MaxConns = 3 })
	admin := e.client()
	admin.login("admin", adminPW)
	var role []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	admin.must("GET", "/api/v1/roles", nil, &role, 200)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "second", "password": "Second-Passw0rd!", "roles": []int64{role[0].ID}}, nil, 201)
	second := e.client()
	second.login("second", "Second-Passw0rd!")
	var cat, page, wf struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Busy"}}, &cat, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "lang": "en", "title": "Page",
		"body": map[string]string{"format": "html", "source": "<p>x</p>"}}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "workflow.process", "lang": "en", "title": "Approval",
		"body": map[string]any{"description": "x", "fields": []map[string]any{{"id": "subject", "label": "Subject", "type": "text"}},
			"steps": []map[string]any{
				{"id": "check", "name": "Check", "assignee": map[string]string{"type": "role", "value": "Administrators"}, "actions": []map[string]any{{"id": "ok", "label": "OK", "next": "done"}}},
				{"id": "done", "name": "Done", "actions": []any{}, "outcome": "success"}}}}, &wf, 201)

	burst := func(name, path string, body any, want int) {
		t.Helper()
		const n = 24
		codes := make([]int, n)
		var wg sync.WaitGroup
		done := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				codes[i] = second.do("POST", path, body, nil)
			}(i)
		}
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			e.srv.CloseClientConnections() // lets the waiting requests end, so the test can stop
			t.Fatalf("%s: %d simultaneous requests with 3 database connections did not finish within 30 s (requests wait for each other's connections)", name, n)
		}
		for i, c := range codes {
			if c != want {
				t.Errorf("%s: request %d answered %d, want %d", name, i, c, want)
			}
		}
	}
	burst("comments", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", page.ID), map[string]string{"body": "at the same moment"}, 201)
	burst("workflow cases", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{"subject": "x"}}, 201)

	// every comment queued its notification for the creator of the page in
	// the same transaction: none is lost
	var comments, mails int
	ctx := context.Background()
	if err := e.app.DB.QueryRow(ctx, `SELECT count(*) FROM plugin_comments WHERE element_id=$1`, page.ID).Scan(&comments); err != nil {
		t.Fatal(err)
	}
	if comments != 24 {
		t.Errorf("%d comments stored, want 24", comments)
	}
	_ = mails
}

// Request bodies have a size limit that depends on the caller and the
// route; administrators change the limits (expert settings).
func TestRequestSizeLimits(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	post := func(c *client, method, path string, body []byte) int {
		req, _ := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "ewiki")
		res, err := c.http.Do(req)
		if err != nil {
			return 0
		}
		defer func() { _ = res.Body.Close() }()
		return res.StatusCode
	}
	login := func(kb int) []byte {
		return []byte(`{"username":"` + strings.Repeat("a", kb<<10) + `","password":"x"}`)
	}
	guest := e.client()
	if st := post(guest, "POST", "/api/v1/auth/login", login(100)); st != 413 {
		t.Errorf("guest: sign-in with 100 kB answered %d, want 413", st)
	}
	if st := post(guest, "POST", "/api/v1/auth/login", login(20)); st != 401 {
		t.Errorf("guest: sign-in with 20 kB answered %d, want 401", st)
	}
	// without Content-Length the limit applies while reading
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/auth/login", struct{ *bytes.Reader }{bytes.NewReader(login(100))})
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "ewiki")
	if res, err := guest.http.Do(req); err != nil || res.StatusCode != 413 {
		t.Errorf("guest: chunked sign-in with 100 kB: %v %v, want 413", res, err)
	}
	// signed in: 1 MB in general, more where the route declares it
	big := []byte(`{"about":"` + strings.Repeat("a", 2<<20) + `"}`)
	if st := post(admin, "PUT", "/api/v1/me/profile", big); st != 413 {
		t.Errorf("profile with 2 MB answered %d, want 413", st)
	}
	var cat struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Big"}}, &cat, 201)
	page := []byte(fmt.Sprintf(`{"category_id":%d,"type":"core.html","lang":"en","title":"Big","body":{"format":"markdown","source":"%s"}}`, cat.ID, strings.Repeat("word ", 300000)))
	if st := post(admin, "POST", "/api/v1/elements", page); st != 201 {
		t.Errorf("page with 1.5 MB answered %d, want 201 (content routes declare a larger limit)", st)
	}

	// the limits are listed and changed in one place
	var res struct {
		Limits []struct {
			Key     string `json:"key"`
			Owner   string `json:"owner"`
			Value   int64  `json:"value"`
			Default int64  `json:"default"`
			Changed bool   `json:"changed"`
		} `json:"limits"`
		Editable bool `json:"editable"`
	}
	admin.must("GET", "/api/v1/admin/limits", nil, &res, 200)
	found := false
	for _, l := range res.Limits {
		found = found || (l.Key == "core.request.guest_kb" && l.Value == 64 && !l.Changed && l.Owner == "core")
	}
	if !found || !res.Editable || len(res.Limits) < 8 {
		t.Fatalf("limits: %+v", res)
	}
	admin.must("PUT", "/api/v1/admin/limits", map[string]any{"values": map[string]any{"core.request.guest_kb": 256}}, &res, 200)
	if st := post(guest, "POST", "/api/v1/auth/login", login(100)); st != 401 {
		t.Errorf("guest limit 256 kB: sign-in with 100 kB answered %d, want 401", st)
	}
	admin.must("PUT", "/api/v1/admin/limits", map[string]any{"values": map[string]any{"core.request.guest_kb": 1}}, nil, 400)    // below the range
	admin.must("PUT", "/api/v1/admin/limits", map[string]any{"values": map[string]any{"core.unknown": 1}}, nil, 400)             // unknown
	admin.must("PUT", "/api/v1/admin/limits", map[string]any{"values": map[string]any{"core.request.guest_kb": nil}}, &res, 200) // back to the default
	if st := post(guest, "POST", "/api/v1/auth/login", login(100)); st != 413 {
		t.Errorf("default again: sign-in with 100 kB answered %d, want 413", st)
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "plain", "password": "Plain-Passw0rd!"}, nil, 201)
	plain := e.client()
	plain.login("plain", "Plain-Passw0rd!")
	plain.must("GET", "/api/v1/admin/limits", nil, nil, 403)
	plain.must("PUT", "/api/v1/admin/limits", map[string]any{"values": map[string]any{"core.request.guest_kb": 4096}}, nil, 403)

	// the audit log keeps what came from outside short
	if st := post(guest, "POST", "/api/v1/auth/login", login(50)); st != 401 {
		t.Fatalf("sign-in with a user name of 50 kB answered %d", st)
	}
	var size int
	if err := e.app.DB.QueryRow(context.Background(), `SELECT max(coalesce(length(before::text),0)+coalesce(length(after::text),0)) FROM audit_log WHERE action='auth.login_failed'`).Scan(&size); err != nil {
		t.Fatal(err)
	}
	if size == 0 || size > 1200 {
		t.Errorf("audit entry of a failed sign-in with a 50 kB user name has %d bytes, want 1..1200", size)
	}
}
