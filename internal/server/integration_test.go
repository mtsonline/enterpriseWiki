package server_test

// End-to-end API tests against a real PostgreSQL database.
// Set EWIKI_TEST_DATABASE_URL (the database is wiped!) to run them:
//
//	EWIKI_TEST_DATABASE_URL=postgres://wiki:wiki@localhost:5432/wiki_test go test ./internal/server/

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
	"github.com/mtsonline/enterprisewiki/internal/server"
	_ "github.com/mtsonline/enterprisewiki/plugins/all"
)

type env struct {
	t   *testing.T
	srv *httptest.Server
	app *app.App
	cfg string
}

type client struct {
	e     *env
	http  *http.Client
	token string // bearer token (API key / SSH token)
	csrf  bool
}

const adminPW = "Admin-Passw0rd!"

func setup(t *testing.T) *env {
	t.Helper()
	url := os.Getenv("EWIKI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("EWIKI_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	dir := t.TempDir()
	c := config.Defaults()
	c.Database.URL = "${EWIKI_TEST_DATABASE_URL}"
	c.I18n.Languages = []string{"en", "de", "fr"}
	c.Plugins.Disabled = []string{}
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := config.WriteFile(cfgPath, c); err != nil {
		t.Fatal(err)
	}
	var logOut io.Writer = io.Discard
	if os.Getenv("EWIKI_TEST_VERBOSE") != "" {
		logOut = os.Stderr
	}
	log := slog.New(slog.NewTextHandler(logOut, nil))
	a, err := app.New(ctx, app.Options{ConfigPath: cfgPath, Log: log, Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	cctx := identity.With(ctx, identity.CLI())
	rid, err := a.Auth.EnsureRole(cctx, "Administrators", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Auth.CreateLocalUserUnchecked(cctx, auth.NewUser{Username: "admin", Password: adminPW, DisplayName: "Admin", Roles: []int64{rid}}); err != nil {
		t.Fatal(err)
	}
	web := fstest.MapFS{"index.html": {Data: []byte("<!DOCTYPE html><html><head><title>test</title></head><body><main id=\"app\"></main></body></html>")}}
	srv := server.New(a, web, pluginmgr.New(dir, a.Config, nil))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &env{t: t, srv: ts, app: a, cfg: cfgPath}
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar}, csrf: true}
}

// do performs a request and decodes the JSON response into out (if not nil).
func (c *client) do(method, path string, body any, out any) int {
	c.e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	if c.csrf {
		req.Header.Set("X-Requested-With", "ewiki")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if out != nil && len(data) > 0 && res.StatusCode < 300 {
		if err := json.Unmarshal(data, out); err != nil {
			c.e.t.Fatalf("%s %s: decode %q: %v", method, path, data, err)
		}
	}
	if res.StatusCode >= 500 {
		c.e.t.Errorf("%s %s: %d %s", method, path, res.StatusCode, data)
	}
	return res.StatusCode
}

func (c *client) must(method, path string, body any, out any, want int) {
	c.e.t.Helper()
	if got := c.do(method, path, body, out); got != want {
		c.e.t.Fatalf("%s %s: status %d, want %d", method, path, got, want)
	}
}

func (c *client) login(user, pw string) {
	c.e.t.Helper()
	c.must("POST", "/api/v1/auth/login", map[string]string{"username": user, "password": pw}, nil, 200)
}

type cat struct {
	ID          int64             `json:"id"`
	ParentID    *int64            `json:"parent_id"`
	Number      string            `json:"number"`
	Titles      map[string]string `json:"titles"`
	HasChildren bool              `json:"has_children"`
	Permissions []string          `json:"permissions"`
}

type elem struct {
	ID           int64  `json:"id"`
	CategoryID   int64  `json:"category_id"`
	Rev          int    `json:"rev"`
	Title        string `json:"title"`
	HTML         string `json:"html"`
	Fallback     bool   `json:"fallback"`
	ShownLang    string `json:"shown_lang"`
	Translated   int    `json:"translated_percent"`
	UpToDate     bool   `json:"up_to_date"`
	Translations []struct {
		Lang   string `json:"lang"`
		Status string `json:"status"`
	} `json:"translations"`
	Body json.RawMessage `json:"body"`
}

func htmlBody(s string) map[string]string { return map[string]string{"format": "html", "source": s} }

func TestAPI(t *testing.T) {
	e := setup(t)
	admin := e.client()
	guest := e.client()

	// ------------------------------------------------------------ authentication
	if got := admin.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong"}, nil); got != 401 {
		t.Fatalf("wrong password: %d", got)
	}
	admin.login("admin", adminPW)
	var me struct {
		User *struct {
			IsAdmin bool `json:"is_admin"`
		} `json:"user"`
	}
	admin.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User == nil || !me.User.IsAdmin {
		t.Fatal("admin not recognized")
	}
	noCSRF := *admin
	noCSRF.csrf = false
	if got := noCSRF.do("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "X"}}, nil); got != 403 {
		t.Fatalf("missing CSRF header accepted: %d", got)
	}

	// ------------------------------------------------------------ categories
	var root, sub, sub2 cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Handbook", "de": "Handbuch"}, "tags": []string{"Docs", "#docs", "hr"}}, &root, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": root.ID, "titles": map[string]string{"en": "Internal"}}, &sub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": root.ID, "titles": map[string]string{"en": "Second"}}, &sub2, 201)
	if sub.Number != "1.1" || sub2.Number != "1.2" {
		t.Fatalf("outline numbers: %s %s", sub.Number, sub2.Number)
	}
	var roots []cat
	guest.must("GET", "/api/v1/categories?parent=0", nil, &roots, 200)
	if len(roots) != 1 || roots[0].HasChildren {
		t.Fatalf("guest must see the first level only, without hints about hidden children: %+v", roots)
	}
	if got := guest.do("GET", fmt.Sprintf("/api/v1/categories/%d", sub.ID), nil, nil); got != 401 {
		t.Fatalf("guest can see subcategory: %d", got)
	}
	// move sub2 into sub, then back
	var moved cat
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/move", sub2.ID), map[string]any{"parent_id": sub.ID}, &moved, 200)
	if moved.Number != "1.1.1" {
		t.Fatalf("moved number %s", moved.Number)
	}
	if got := admin.do("POST", fmt.Sprintf("/api/v1/categories/%d/move", sub.ID), map[string]any{"parent_id": sub2.ID}, nil); got != 400 {
		t.Fatalf("moving into own subtree must fail: %d", got)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/reorder", root.ID), map[string]any{"order": []int64{sub.ID}}, nil, 204)
	var tree []struct {
		ID    int64 `json:"id"`
		Depth int   `json:"depth"`
	}
	admin.must("GET", "/api/v1/categories/tree", nil, &tree, 200)
	if len(tree) != 3 || tree[2].Depth != 3 {
		t.Fatalf("tree %+v", tree)
	}

	// ------------------------------------------------------------ elements, versions, translations
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "lang": "en", "title": "Onboarding",
		"body": htmlBody(`<p>Welcome aboard zebrafish</p><script>alert(1)</script><img src="x" onerror="alert(2)">`), "tags": []string{"hr"}}, &page, 201)
	if strings.Contains(page.HTML, "<script") || strings.Contains(page.HTML, "onerror") {
		t.Fatalf("XSS not sanitized: %s", page.HTML)
	}
	var saved elem
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Onboarding", "body": htmlBody("<p>Version two</p>"), "base_rev": 1}, &saved, 200)
	if saved.Rev != 2 {
		t.Fatalf("rev %d", saved.Rev)
	}
	if got := admin.do("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "x", "body": htmlBody("x"), "base_rev": 1}, nil); got != 409 {
		t.Fatalf("stale base_rev must conflict: %d", got)
	}
	var de elem
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "de", "title": "Einarbeitung", "body": htmlBody("<p>Version zwei</p>"), "base_rev": 0}, &de, 200)
	if de.Translated != 66 || de.UpToDate {
		t.Fatalf("translation status (en+de of en,de,fr): %d%% up_to_date=%v", de.Translated, de.UpToDate)
	}
	var fr elem
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=fr", page.ID), nil, &fr, 200)
	if !fr.Fallback || fr.ShownLang != "en" {
		t.Fatalf("missing translation must fall back to the original: %+v", fr)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Onboarding", "body": htmlBody("<p>Version three</p>")}, nil, 200)
	var check elem
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=de", page.ID), nil, &check, 200)
	status := map[string]string{}
	for _, tr := range check.Translations {
		status[tr.Lang] = tr.Status
	}
	if status["en"] != "original" || status["de"] != "outdated" || status["fr"] != "missing" {
		t.Fatalf("translation states %v", status)
	}
	// history & restore (nothing is overwritten)
	var revs []struct {
		Lang string `json:"lang"`
		Rev  int    `json:"rev"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d/revisions?lang=en", page.ID), nil, &revs, 200)
	if len(revs) != 3 {
		t.Fatalf("revisions %v", revs)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d/revisions/en/1", page.ID), nil, nil, 200)
	var restored elem
	admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/restore", page.ID), map[string]any{"lang": "en", "rev": 2}, &restored, 200)
	if restored.Rev != 4 || !strings.Contains(restored.HTML, "Version two") {
		t.Fatalf("restore: rev %d html %s", restored.Rev, restored.HTML)
	}
	var preview struct {
		HTML string `json:"html"`
	}
	admin.must("POST", "/api/v1/elements/preview", map[string]any{"type": "core.html", "body": map[string]string{"format": "markdown", "source": "# Hi\n\n**bold**"}}, &preview, 200)
	if !strings.Contains(preview.HTML, "<strong>bold</strong>") {
		t.Fatalf("markdown preview %s", preview.HTML)
	}

	// ------------------------------------------------------------ search & tags
	var sr struct {
		Elements []struct {
			ElementID int64  `json:"element_id"`
			Snippet   string `json:"snippet"`
		} `json:"elements"`
		Categories []cat `json:"categories"`
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Onboarding", "body": htmlBody("<p>The zebrafish &lt;b&gt;guide</p>")}, nil, 200)
	admin.must("GET", "/api/v1/search?q=zebra&lang=en", nil, &sr, 200)
	if len(sr.Elements) != 1 || !strings.Contains(sr.Elements[0].Snippet, "<mark>zebrafish</mark>") || strings.Contains(sr.Elements[0].Snippet, "<b>") {
		t.Fatalf("search %+v", sr)
	}
	guest.must("GET", "/api/v1/search?q=zebra", nil, &sr, 200)
	if len(sr.Elements) != 0 {
		t.Fatal("guest finds hidden content")
	}
	admin.must("GET", "/api/v1/search?tags=hr", nil, &sr, 200)
	if len(sr.Elements) != 1 || len(sr.Categories) != 1 {
		t.Fatalf("tag search %+v", sr)
	}
	var tags []struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	admin.must("GET", "/api/v1/tags", nil, &tags, 200)
	if len(tags) == 0 || tags[0].Tag != "hr" {
		t.Fatalf("tags %+v", tags)
	}

	// ------------------------------------------------------------ users, groups, permissions
	var group struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "HR", "description": "Human resources"}, &group, 200)
	var bob struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd!x", "display_name": "Bob"}, &bob, 201)
	if got := admin.do("POST", "/api/v1/admin/users", map[string]any{"username": "weak", "password": "short"}, nil); got != 400 {
		t.Fatalf("weak password accepted: %d", got)
	}
	bobc := e.client()
	bobc.login("bob", "Bob-Passw0rd!x")
	if got := bobc.do("GET", fmt.Sprintf("/api/v1/categories/%d", sub.ID), nil, nil); got != 404 {
		t.Fatalf("bob sees hidden category: %d", got)
	}
	if got := bobc.do("GET", "/api/v1/admin/users", nil, nil); got != 403 {
		t.Fatalf("bob reaches admin api: %d", got)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", bob.ID), map[string]any{"groups": []int64{group.ID}}, nil, 200)
	var rule struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root.ID), map[string]any{"subject_type": "group", "subject_id": group.ID, "permissions": 1, "inherit": true}, &rule, 201)
	bobc = e.client()
	bobc.login("bob", "Bob-Passw0rd!x")
	bobc.must("GET", fmt.Sprintf("/api/v1/categories/%d", sub2.ID), nil, nil, 200)
	if got := bobc.do("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "hack", "body": htmlBody("x")}, nil); got != 403 {
		t.Fatalf("bob can edit without permission: %d", got)
	}
	var acl struct {
		Own []struct {
			ID int64 `json:"id"`
		} `json:"own"`
		Inherited []struct {
			ID int64 `json:"id"`
		} `json:"inherited"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d/acl", sub.ID), nil, &acl, 200)
	if len(acl.Inherited) != 1 {
		t.Fatalf("inherited rules %+v", acl)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", sub.ID), map[string]any{"subject_type": "user", "subject_id": bob.ID, "permissions": 3, "inherit": false}, nil, 201)
	bobc.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Onboarding", "body": htmlBody("<p>edited by bob</p>")}, nil, 200)
	if got := bobc.do("POST", fmt.Sprintf("/api/v1/categories/%d/acl", sub.ID), map[string]any{"subject_type": "guest", "permissions": 1}, nil); got != 403 {
		t.Fatalf("bob can manage permissions: %d", got)
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/acl/%d", rule.ID), nil, nil, 204)
	admin.must("GET", fmt.Sprintf("/api/v1/admin/groups/%d/members", group.ID), nil, nil, 200)
	admin.must("GET", fmt.Sprintf("/api/v1/admin/users/%d", bob.ID), nil, nil, 200)
	var lookup []map[string]any
	bobc.must("GET", "/api/v1/users/lookup?q=ad", nil, &lookup, 200)

	// ------------------------------------------------------------ API keys
	var key struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	admin.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "ci", "permissions": 1, "category_id": sub.ID, "expires_at": time.Now().Add(24 * time.Hour)}, &key, 201)
	kc := e.client()
	kc.token = key.Token
	kc.csrf = false
	kc.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 200)
	if got := kc.do("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "x", "body": htmlBody("x")}, nil); got != 403 {
		t.Fatalf("read-only key can write: %d", got)
	}
	if got := kc.do("GET", fmt.Sprintf("/api/v1/categories/%d", root.ID), nil, nil); got != 404 {
		t.Fatalf("key restricted to a subtree reaches its parent: %d", got)
	}
	if got := kc.do("GET", "/api/v1/admin/users", nil, nil); got != 403 {
		t.Fatalf("key without allow_admin reaches admin api: %d", got)
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/me/api-keys/%d", key.ID), nil, nil, 204)
	if got := kc.do("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil); got != 401 {
		t.Fatalf("revoked key still works: %d", got)
	}

	// ------------------------------------------------------------ SSH key login
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	sshPub, _ := ssh.NewPublicKey(pub)
	admin.must("POST", "/api/v1/me/ssh-keys", map[string]string{"name": "laptop", "public_key": string(ssh.MarshalAuthorizedKey(sshPub))}, nil, 201)
	var ch struct {
		Challenge string `json:"challenge"`
	}
	guest.must("POST", "/api/v1/auth/ssh/challenge", map[string]string{"username": "admin"}, &ch, 200)
	var tok struct {
		Token string `json:"token"`
	}
	guest.must("POST", "/api/v1/auth/ssh/verify", map[string]string{"challenge": ch.Challenge, "signature": SignSSH(signer, []byte(ch.Challenge), auth.SSHNamespace)}, &tok, 200)
	sc := e.client()
	sc.token = tok.Token
	sc.must("GET", "/api/v1/admin/users", nil, nil, 200)
	if got := guest.do("POST", "/api/v1/auth/ssh/verify", map[string]string{"challenge": ch.Challenge, "signature": SignSSH(signer, []byte(ch.Challenge), auth.SSHNamespace)}, nil); got != 401 {
		t.Fatalf("challenge reused: %d", got)
	}

	// ------------------------------------------------------------ rollback
	time.Sleep(20 * time.Millisecond)
	before := time.Now()
	time.Sleep(20 * time.Millisecond)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Changed later", "body": htmlBody("<p>later</p>")}, nil, 200)
	var late elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "Late page", "body": htmlBody("x")}, &late, 201)
	var rb struct {
		Revisions int `json:"revisions"`
		Trashed   int `json:"trashed"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/rollback", root.ID), map[string]any{"at": before, "recursive": true}, &rb, 200)
	if rb.Revisions != 1 || rb.Trashed != 1 {
		t.Fatalf("rollback %+v", rb)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", page.ID), nil, &check, 200)
	if check.Title != "Onboarding" {
		t.Fatalf("rollback title %q", check.Title)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d/history", sub.ID), nil, nil, 200)

	// ------------------------------------------------------------ duplicate, trash, restore, purge
	var dup cat
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/duplicate", sub.ID), map[string]any{"parent_id": root.ID}, &dup, 201)
	if dup.Titles["en"] != "Internal (copy)" {
		t.Fatalf("duplicate %+v", dup)
	}
	var dupEl struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/duplicate", page.ID), map[string]any{"category_id": dup.ID}, &dupEl, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/move", dupEl.ID), map[string]any{"category_id": sub.ID}, nil, 200)
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", dupEl.ID), map[string]any{"tags": []string{"moved"}}, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", sub.ID), map[string]any{"default_element_id": page.ID, "titles": map[string]string{"en": "Internal", "de": "Intern"}}, nil, 200)
	admin.must("DELETE", fmt.Sprintf("/api/v1/categories/%d", sub.ID), nil, nil, 204)
	if got := admin.do("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil); got != 404 {
		t.Fatalf("element of trashed category visible: %d", got)
	}
	var trash []struct {
		Type     string `json:"type"`
		ID       int64  `json:"id"`
		Contains int    `json:"contains"`
	}
	admin.must("GET", "/api/v1/trash", nil, &trash, 200)
	if len(trash) != 2 { // the category (with its subtree) and the late page from the rollback
		t.Fatalf("trash %+v", trash)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/trash/category/%d/restore", sub.ID), nil, nil, 204)
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, nil, 200)
	admin.must("DELETE", fmt.Sprintf("/api/v1/trash/element/%d", late.ID), nil, nil, 204)
	admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", dupEl.ID), nil, nil, 204)
	admin.must("POST", fmt.Sprintf("/api/v1/trash/element/%d/restore", dupEl.ID), nil, nil, 204)

	// ------------------------------------------------------------ plugins, settings, i18n
	var types []struct {
		ID string `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/plugins/example.notice/disable", nil, nil, 204)
	admin.must("GET", "/api/v1/element-types", nil, &types, 200)
	for _, ty := range types {
		if ty.ID == "example.notice" {
			t.Fatal("disabled element type still listed")
		}
	}
	if got := admin.do("GET", "/api/v1/x/example.notice/tones", nil, nil); got != 404 {
		t.Fatalf("route of disabled plugin reachable: %d", got)
	}
	if got := admin.do("POST", "/api/v1/admin/plugins/core.html/disable", nil, nil); got != 204 {
		t.Fatalf("disable core.html: %d", got)
	}
	admin.must("POST", "/api/v1/admin/plugins/core.html/enable", nil, nil, 204)
	admin.must("POST", "/api/v1/admin/plugins/example.notice/enable", nil, nil, 204)
	admin.must("GET", "/api/v1/x/example.notice/tones", nil, nil, 200)
	if got := admin.do("POST", "/api/v1/admin/plugins/auth.local/disable", nil, nil); got != 400 {
		t.Fatalf("required plugin disabled: %d", got)
	}
	var notice elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "example.notice", "title": "Note",
		"body": map[string]string{"format": "html", "source": "<p>Careful</p>", "tone": "warning"}}, &notice, 201)
	if !strings.Contains(notice.HTML, "ew-notice--warning") {
		t.Fatalf("inherited element type render %s", notice.HTML)
	}
	var plugins struct {
		Installed []struct {
			ID     string `json:"id"`
			Active bool   `json:"active"`
		} `json:"installed"`
	}
	admin.must("GET", "/api/v1/admin/plugins", nil, &plugins, 200)
	if len(plugins.Installed) < 6 {
		t.Fatalf("plugins %+v", plugins)
	}
	var settings map[string]any
	admin.must("GET", "/api/v1/admin/settings", nil, &settings, 200)
	ui := map[string]any{"site_name": "Test Wiki", "template": "theme.confluence", "home_category": 0}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil, 200)
	raw, _ := os.ReadFile(e.cfg)
	if !strings.Contains(string(raw), "site_name: Test Wiki") || !strings.Contains(string(raw), "${EWIKI_TEST_DATABASE_URL}") {
		t.Fatalf("settings not written to config file (or env reference lost):\n%s", raw)
	}
	admin.must("POST", "/api/v1/admin/languages", map[string]string{"code": "it"}, nil, 204)
	admin.must("DELETE", "/api/v1/admin/languages/it", nil, nil, 204)
	if got := admin.do("DELETE", "/api/v1/admin/languages/en", nil, nil); got != 400 {
		t.Fatalf("default language removed: %d", got)
	}
	var st []struct {
		Lang    string `json:"lang"`
		Percent int    `json:"percent"`
	}
	guest.must("GET", "/api/v1/i18n/status", nil, &st, 200)
	if len(st) != 3 || st[0].Percent != 100 {
		t.Fatalf("i18n status %+v", st)
	}
	admin.must("PUT", "/api/v1/admin/i18n/fr", map[string]string{"common.save": "Enregistrer"}, nil, 204)
	var msgs map[string]string
	guest.must("GET", "/api/v1/i18n/fr", nil, &msgs, 200)
	if msgs["common.save"] != "Enregistrer" || msgs["common.cancel"] != "Cancel" {
		t.Fatal("custom translation / fallback not applied")
	}
	var strs []map[string]any
	admin.must("GET", "/api/v1/admin/i18n/fr", nil, &strs, 200)
	admin.must("GET", "/api/v1/admin/audit?limit=5", nil, nil, 200)
	admin.must("POST", "/api/v1/admin/retention/run", nil, nil, 200)

	// ------------------------------------------------------------ menus (plugin API)
	var menu struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/x/core.menu/menus", map[string]any{"name": "Main", "location": "header-nav",
		"items": []map[string]any{{"label": map[string]string{"en": "Handbook"}, "category_id": root.ID}, {"label": map[string]string{"en": "Secret"}, "category_id": sub.ID}}}, &menu, 200)
	var menus []struct {
		Items []any `json:"items"`
	}
	guest.must("GET", "/api/v1/x/core.menu/menus?location=header-nav", nil, &menus, 200)
	if len(menus) != 1 || len(menus[0].Items) != 1 {
		t.Fatalf("menu items not filtered by permission: %+v", menus)
	}
	if got := admin.do("POST", "/api/v1/x/core.menu/menus", map[string]any{"name": "Bad", "location": "header-nav", "items": []map[string]any{{"label": map[string]string{"en": "x"}, "url": "javascript:alert(1)"}}}, nil); got != 400 {
		t.Fatalf("javascript: URL accepted: %d", got)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/x/core.menu/menus/%d", menu.ID), map[string]any{"name": "Main", "location": "footer", "items": []any{}}, nil, 200)
	admin.must("DELETE", fmt.Sprintf("/api/v1/x/core.menu/menus/%d", menu.ID), nil, nil, 204)

	// ------------------------------------------------------------ profile, roles, deletion, lockout
	admin.must("PUT", "/api/v1/me", map[string]string{"display_name": "Admin A"}, nil, 204)
	bobc.must("PUT", "/api/v1/me/password", map[string]string{"current": "Bob-Passw0rd!x", "new": "Bob-Passw0rd!y"}, nil, 204)
	var role struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Editors"}, &role, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/roles/%d", role.ID), map[string]any{"name": "Editors", "description": "x"}, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/groups/%d", group.ID), map[string]any{"name": "HR", "roles": []int64{role.ID}}, nil, 200)
	admin.must("DELETE", fmt.Sprintf("/api/v1/admin/roles/%d", role.ID), nil, nil, 204)
	admin.must("DELETE", fmt.Sprintf("/api/v1/admin/groups/%d", group.ID), nil, nil, 204)
	var adminID int64
	if err := e.app.DB.QueryRow(context.Background(), `SELECT id FROM users WHERE username='admin'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if got := admin.do("PUT", fmt.Sprintf("/api/v1/admin/users/%d", adminID), map[string]any{"roles": []int64{}}, nil); got != 409 {
		t.Fatalf("last admin removed: %d", got)
	}
	for i := 0; i < 10; i++ {
		e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "bob", "password": "wrong"}, nil)
	}
	if got := e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "bob", "password": "Bob-Passw0rd!y"}, nil); got != 429 {
		t.Fatalf("no lockout after failed logins: %d", got)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", bob.ID), map[string]any{"unlock": true}, nil, 200)
	admin.must("DELETE", fmt.Sprintf("/api/v1/admin/users/%d", bob.ID), nil, nil, 204)
	admin.must("POST", "/api/v1/auth/logout", nil, nil, 204)
	admin.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User != nil {
		t.Fatal("still logged in after logout")
	}
	guest.must("GET", "/api/v1/site", nil, nil, 200)
	guest.must("GET", "/api/v1/auth/providers", nil, nil, 200)
	guest.must("GET", "/readyz", nil, nil, 200)

	t.Run("every API route has a frontend binding", func(t *testing.T) { checkRouteCoverage(t) })
}

// checkRouteCoverage verifies that every registered API route is used by the
// frontend (core or plugin web code) or by a documented client script.
func checkRouteCoverage(t *testing.T) {
	root := filepath.Join("..", "..")
	var corpus strings.Builder
	for _, dir := range []string{"web/src", "plugins", "scripts"} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".sh") {
				b, _ := os.ReadFile(p)
				corpus.Write(b)
			}
			return nil
		})
	}
	src := corpus.String()
	exempt := map[string]bool{
		"/readyz":                              true, // used by load balancers / orchestrators
		"/api/v1/auth/sso/{provider}/callback": true, // called by the identity provider
	}
	ph := regexp.MustCompile(`\\\{[a-z_]+\\\}`)
	for _, r := range httpx.Routes() {
		_, path, _ := strings.Cut(r, " ")
		if exempt[path] {
			continue
		}
		rel := strings.TrimPrefix(path, "/api/v1/")
		if strings.HasPrefix(rel, "x/") { // plugin route: frontend uses its own base constant
			parts := strings.SplitN(rel, "/", 3)
			if len(parts) == 3 {
				rel = parts[2]
			}
		}
		pat := ph.ReplaceAllString(regexp.QuoteMeta(rel), `(?:\$\{[^}]+\}|[A-Za-z0-9_.-]+)`)
		re := regexp.MustCompile("[`'\"/]" + pat + "(?:[`'\"?/$]|$)")
		if !re.MatchString(src) {
			t.Errorf("route %q has no frontend binding", r)
		}
	}
}

// SignSSH produces an armored SSHSIG signature like `ssh-keygen -Y sign`.
func SignSSH(s ssh.Signer, msg []byte, ns string) string {
	ws := func(b *bytes.Buffer, v []byte) {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(v)))
		b.Write(l[:])
		b.Write(v)
	}
	h := sha512.Sum512(msg)
	var signed bytes.Buffer
	signed.WriteString("SSHSIG")
	ws(&signed, []byte(ns))
	ws(&signed, nil)
	ws(&signed, []byte("sha512"))
	ws(&signed, h[:])
	sig, err := s.Sign(rand.Reader, signed.Bytes())
	if err != nil {
		panic(err)
	}
	var blob bytes.Buffer
	blob.WriteString("SSHSIG")
	binary.Write(&blob, binary.BigEndian, uint32(1)) //nolint:errcheck
	ws(&blob, s.PublicKey().Marshal())
	ws(&blob, []byte(ns))
	ws(&blob, nil)
	ws(&blob, []byte("sha512"))
	ws(&blob, ssh.Marshal(sig))
	return "-----BEGIN SSH SIGNATURE-----\n" + base64.StdEncoding.EncodeToString(blob.Bytes()) + "\n-----END SSH SIGNATURE-----\n"
}
