package server_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// raw sends a request without failing the test (for goroutines).
func (c *client) send(method, path string, body any) (int, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, c.e.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "ewiki")
	res, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.StatusCode, nil
}

// S-01: many comments at the same moment must not block the pool (each
// one notifies others and loaded them with a second connection while its
// transaction held the first).
func TestManyConcurrentCommentsDoNotBlock(t *testing.T) {
	e := setupWith(t, func(c *config.Config) { c.Database.MaxConns = 4 })
	admin := e.client()
	admin.login("admin", adminPW)
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Talk"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 3, "inherit": true}, nil, 201)
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Event", "body": htmlBody("<p>x</p>")}, &page, 201)
	var users []*client
	for i := range 3 {
		name := fmt.Sprintf("talker%d", i)
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": "Talk-Passw0rd!", "display_name": name, "roles": []int64{}}, nil, 201)
		u := e.client()
		u.login(name, "Talk-Passw0rd!")
		users = append(users, u)
	}
	var first struct{ ID int64 }
	users[0].must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", page.ID), map[string]any{"body": "start"}, &first, 201)
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	start := time.Now()
	for i := range 48 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, err := users[i%3].send("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", page.ID), map[string]any{"body": fmt.Sprintf("reply %d", i), "parent_id": first.ID})
			if err != nil {
				code = -1
			}
			mu.Lock()
			codes[code]++
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if codes[201] != 48 || time.Since(start) > 20*time.Second {
		t.Fatalf("concurrent comments: %v in %s", codes, time.Since(start))
	}
}

// pngHeader returns a PNG whose header claims w × h pixels (only the
// header is valid: enough for the dimension check).
func pngHeader(w, h uint32) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1)))
	b := buf.Bytes()
	binary.BigEndian.PutUint32(b[16:], w) // IHDR data starts at 16
	binary.BigEndian.PutUint32(b[20:], h)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	return b
}

// TestHardening covers the findings S-02 to S-05, S-07 to S-09 and F-01,
// F-02, P-02 of the test report of 12.11.2026.
func TestHardening(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()

	// S-02: guests send small bodies only; JSON of signed-in callers is bounded
	big := strings.Repeat("x", 200<<10)
	if code := guest.do("POST", "/api/v1/auth/login", map[string]string{"username": big, "password": "x"}, nil); code != 413 {
		t.Fatalf("guest 200 KB body: %d", code)
	}
	if code := admin.do("POST", "/api/v1/elements/preview", map[string]any{"type": "core.html", "body": map[string]string{"markdown": strings.Repeat("y", 5<<20)}}, nil); code != 413 {
		t.Fatalf("5 MB JSON: %d", code)
	}

	// S-03: long user names are neither processed nor logged in full
	long := strings.Repeat("n", 40<<10)
	if code := guest.do("POST", "/api/v1/auth/login", map[string]string{"username": long, "password": "x"}, nil); code != 401 {
		t.Fatalf("long user name: %d", code)
	}
	var size int
	if err := e.app.DB.QueryRow(context.Background(), `SELECT coalesce(max(length(after::text)), 0) FROM audit_log WHERE action='auth.login_failed'`).Scan(&size); err != nil || size > 2048 {
		t.Fatalf("audit entry of a failed sign-in: %d bytes (%v)", size, err)
	}
	// a large snapshot is shortened
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Media"}}, &c, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Big", "body": htmlBody("<p>" + strings.Repeat("z ", 150<<10) + "</p>")}, nil, 201)
	if err := e.app.DB.QueryRow(context.Background(), `SELECT coalesce(max(length(coalesce(after::text,'') || coalesce(before::text,''))), 0) FROM audit_log`).Scan(&size); err != nil || size > 140<<10 {
		t.Fatalf("largest audit entry: %d bytes (%v)", size, err)
	}

	// S-05: successful sign-ins of many accounts from one address are not limited
	for i := range 35 {
		name := fmt.Sprintf("office%d", i)
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": "Office-Passw0rd!", "display_name": name, "roles": []int64{}}, nil, 201)
		u := e.client()
		u.login(name, "Office-Passw0rd!")
	}
	// failed ones are
	limited := false
	for range 35 {
		if code := guest.do("POST", "/api/v1/auth/login", map[string]string{"username": "office1", "password": "wrong"}, nil); code == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("failed sign-ins are not limited")
	}

	// S-07: signing in needs the CSRF header too
	raw := e.client()
	raw.csrf = false
	if code := raw.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": adminPW}, nil); code != 403 {
		t.Fatalf("login without X-Requested-With: %d", code)
	}

	// S-08: negative paging values are refused
	for _, q := range []string{"limit=-1", "offset=-5", "limit=abc", "page=-2"} {
		if code := admin.do("GET", "/api/v1/elements?"+q, nil, nil); code != 400 {
			t.Fatalf("%s: %d", q, code)
		}
	}

	// S-09: oversized images are refused when uploaded
	if code, _ := admin.upload(t, "huge.png", pngHeader(20000, 20000)); code != 422 {
		t.Fatalf("20000×20000 PNG as file: %d", code)
	}
	if code, _ := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": c.ID, "title": "Huge"}, "huge.png", pngHeader(20000, 20000)); code != 422 {
		t.Fatalf("20000×20000 PNG as medium: %d", code)
	}
	if code, _ := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": c.ID, "title": "Ok"}, "ok.png", pngHeader(1, 1)); code != 201 {
		t.Fatalf("small PNG: %d", code)
	}

	// F-01: a group given by "#ID" may handle the step (and is shown by name)
	var g struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Approvers"}, &g, 200)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "appr", "password": "Appr-Passw0rd!", "display_name": "Appr", "roles": []int64{}, "groups": []int64{g.ID}}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	def := map[string]any{"description": "", "fields": []any{},
		"steps": []map[string]any{
			{"id": "check", "name": "Check", "assignee": map[string]string{"type": "group", "value": fmt.Sprintf("#%d", g.ID)},
				"actions": []map[string]any{{"id": "ok", "label": "OK", "next": "done"}}},
			{"id": "done", "name": "Done", "actions": []any{}, "outcome": "success"},
		}}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "workflow.process", "title": "Request", "body": def}, &wf, 201)
	if html := viewHTML(t, admin, wf.ID); !strings.Contains(html, "Approvers") {
		t.Fatalf("group by ID not shown by name: %s", html)
	}
	var x struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{}}, &x, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/groups/%d", g.ID), map[string]any{"name": "Approvers (renamed)"}, nil, 200)
	ap := e.client()
	ap.login("appr", "Appr-Passw0rd!")
	ap.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", x.ID), map[string]any{"action": "ok"}, nil, 200)

	// F-02: a copied category's blog points to the copy
	var news cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "News"}}, &news, 201)
	var blog elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": news.ID, "type": "content.blog", "title": "Blog",
		"body": map[string]any{"category_id": news.ID, "more_sources": fmt.Sprintf("category:%d; tag:hr", news.ID)}}, &blog, 201)
	var cp cat
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/duplicate", news.ID), map[string]any{}, &cp, 201)
	var body []byte
	if err := e.app.DB.QueryRow(context.Background(), `SELECT r.body FROM element_revisions r JOIN elements e ON e.id=r.element_id WHERE e.category_id=$1 AND e.type='content.blog'`, cp.ID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), fmt.Sprintf(`"category_id": %d`, cp.ID)) || !strings.Contains(string(body), fmt.Sprintf("category:%d;", cp.ID)) {
		t.Fatalf("copied blog: %s", body)
	}

	// P-02: the text search in snapshots finds recent entries
	var res []map[string]any
	admin.must("GET", "/api/v1/admin/audit?q=Approvers", nil, &res, 200)
	if len(res) < 2 {
		t.Fatal("audit text search finds nothing")
	}
}

// S-04: requests per account and address are limited (with Retry-After).
func TestRateLimit(t *testing.T) {
	e := setupWith(t, func(c *config.Config) {
		c.Server.RateLimit = config.RateLimit{PerSecond: 1, Burst: 15, GuestPerSec: 1, GuestBurstN: 10, ExpensivePerSec: 1, ExpensiveBurstN: 3}
	})
	admin := e.client()
	admin.login("admin", adminPW) // before the guest uses up the address
	guest := e.client()
	codes := map[int]int{}
	for range 20 {
		codes[guest.do("GET", "/api/v1/site", nil, nil)]++
	}
	if codes[429] == 0 || codes[200] > 11 {
		t.Fatalf("guest: %v", codes)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/site", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 429 || res.Header.Get("Retry-After") == "" {
		t.Fatalf("429 with Retry-After: %d %q", res.StatusCode, res.Header.Get("Retry-After"))
	}
	// expensive requests have their own, stricter bucket (per account)
	codes = map[int]int{}
	for range 6 {
		codes[admin.do("GET", "/api/v1/search?q=x", nil, nil)]++
	}
	if codes[429] == 0 || codes[200] > 4 {
		t.Fatalf("search: %v", codes)
	}
}
