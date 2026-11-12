package server_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
)

// TestSecretsSealed runs the wiki with secrets.mode pq: signing keys of
// webhooks, secrets of authenticator apps, of workflows and of forms are
// stored encrypted and bound to their row, still work, are never part of
// a page and are not copied with it; rewrap switches between the modes.
func TestSecretsSealed(t *testing.T) {
	e := setupWith(t, func(c *config.Config) {
		c.Secrets.Mode = "pq"
		c.Webhooks.AllowPrivate = true
	})
	ctx := context.Background()
	key := filepath.Join(filepath.Dir(e.cfg), "secrets.key")
	if st, err := os.Stat(key); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", st, err)
	}
	admin := e.client()
	admin.login("admin", adminPW)

	// a receiver that checks the signatures
	var mu sync.Mutex
	signed := map[string]int{}
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		for _, sec := range []string{"core-secret", "flow-secret"} {
			m := hmac.New(sha256.New, []byte(sec))
			m.Write(b)
			if r.Header.Get("X-Ewiki-Signature") == "sha256="+hex.EncodeToString(m.Sum(nil)) {
				signed[sec]++
			}
		}
	}))
	defer recv.Close()
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Plugins.Macros.HTTPAllow = []string{recv.URL + "/"}
		c.Plugins.Macros.HTTPAllowPrivate = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sealedIn := func(q, what string) string {
		t.Helper()
		var v string
		if err := e.app.DB.QueryRow(ctx, q).Scan(&v); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if !strings.HasPrefix(v, "ews1:q:") {
			t.Fatalf("%s is not sealed: %.40q", what, v)
		}
		return v
	}

	// core webhook
	admin.must("POST", "/api/v1/admin/webhooks", map[string]any{"name": "All", "url": recv.URL, "format": "json", "secret": "core-secret",
		"events": []string{"element.create"}}, nil, 201)
	sealedIn(`SELECT secret FROM webhooks LIMIT 1`, "webhook secret")

	// authenticator app
	totp := enrollTOTP(t, admin)
	v := sealedIn(`SELECT secret FROM plugin_mfa_totp LIMIT 1`, "totp secret")
	if strings.Contains(v, totp) {
		t.Fatal("totp secret in clear")
	}

	// workflow with a signed webhook step: the secret never is part of the page
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Flows"}}, &c, 201)
	def := map[string]any{"description": "", "fields": []any{}, "steps": []map[string]any{
		{"id": "hook", "name": "Hook", "type": "webhook", "next": "done", "webhook": map[string]any{"url": recv.URL + "/x", "secret": "flow-secret"}},
		{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "workflow.process", "title": "Signed", "body": def}, &wf, 201)
	var raw map[string]any
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?edit=1", wf.ID), nil, &raw, 200)
	if b := fmt.Sprint(raw["body"]); strings.Contains(b, "flow-secret") || !strings.Contains(b, "sec_") {
		t.Fatalf("workflow body: %s", b)
	}
	var revs int
	if err := e.app.DB.QueryRow(ctx, `SELECT count(*) FROM element_revisions WHERE body::text LIKE '%flow-secret%'`).Scan(&revs); err != nil || revs != 0 {
		t.Fatalf("secret in a revision: %d %v", revs, err)
	}
	sealedIn(`SELECT convert_from(value, 'UTF8') FROM element_secrets WHERE element_id IS NOT NULL LIMIT 1`, "workflow secret")
	run := func(id int64) wfCase {
		for i := 0; i < 4; i++ {
			if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_workflow_instances SET wake_at=now() WHERE id=$1 AND wake_at IS NOT NULL`, id); err != nil {
				t.Fatal(err)
			}
			if err := e.app.Workers.RunOnce(ctx, "workflow.process.timers"); err != nil {
				t.Fatal(err)
			}
		}
		var x wfCase
		admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", id), nil, &x, 200)
		return x
	}
	var x wfCase
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{}}, &x, 201)
	if got := run(x.ID); got.Status != "done" {
		t.Fatalf("signed workflow webhook: %+v", got)
	}
	// saving again without the secret keeps it
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?edit=1", wf.ID), nil, &raw, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", wf.ID), map[string]any{"title": "Signed", "body": raw["body"], "base_rev": 1}, nil, 200)
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID), map[string]any{"data": map[string]any{}}, &x, 201)
	if got := run(x.ID); got.Status != "done" {
		t.Fatalf("after saving again: %+v", got)
	}
	// a copy does not carry the secret along
	var cp elem
	admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/duplicate", wf.ID), map[string]any{"category_id": c.ID}, &cp, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", cp.ID), map[string]any{"data": map[string]any{}}, &x, 201)
	if got := run(x.ID); got.Status != "open" || !strings.Contains(got.LastError, "missing") {
		t.Fatalf("copied secret: %+v", got)
	}
	if err := e.app.Workers.RunOnce(ctx, "webhooks"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if signed["core-secret"] == 0 || signed["flow-secret"] != 2 {
		t.Fatalf("signatures: %v", signed)
	}
	mu.Unlock()

	// a form: the token secret is sealed
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.forms", "title": "F",
		"body": map[string]any{"fields": []map[string]any{{"id": "a", "type": "text", "label": "A"}}, "targets": map[string]any{"store": true,
			"webhook": map[string]any{"url": recv.URL + "/f", "secret": "form-secret"}}}}, &wf, 201)
	admin.must("GET", fmt.Sprintf("/api/v1/x/content.forms/elements/%d/start", wf.ID), nil, nil, 200)
	sealedIn(`SELECT convert_from(secret, 'UTF8') FROM plugin_form_secret`, "form token secret")
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?edit=1", wf.ID), nil, &raw, 200)
	if b := fmt.Sprint(raw["body"]); strings.Contains(b, "form-secret") || !strings.Contains(b, "sec_") {
		t.Fatalf("form body: %s", b)
	}

	// status and rewrap: to plain (with the key) and back
	var st struct {
		Mode   string        `json:"mode"`
		Loaded bool          `json:"key_loaded"`
		Values secrets.Stats `json:"values"`
	}
	admin.must("GET", "/api/v1/admin/secrets", nil, &st, 200)
	if st.Mode != "pq" || !st.Loaded || st.Values.Plain != 0 || st.Values.PQ < 5 {
		t.Fatalf("status: %+v", st)
	}
	plain, err := secrets.Load("plain", key, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := secrets.Rewrap(ctx, e.app.DB, plain); err != nil || n != st.Values.PQ {
		t.Fatalf("rewrap to plain: %d %v", n, err)
	}
	if n, err := secrets.Rewrap(ctx, e.app.DB, e.app.Instance.Secrets); err != nil || n != st.Values.PQ {
		t.Fatalf("rewrap to pq: %d %v", n, err)
	}
	// a value moved to another row cannot be opened
	if _, err := e.app.DB.Exec(ctx, `UPDATE webhooks SET secret=(SELECT convert_from(value, 'UTF8') FROM element_secrets LIMIT 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.Rewrap(ctx, e.app.DB, e.app.Instance.Secrets); err == nil {
		t.Fatal("a moved secret was opened")
	}
	// only administrators of the instance see the status
	u := e.client()
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "una", "password": "Passw0rd!Passw0rd", "display_name": "Una", "roles": []int64{}}, nil, 201)
	u.login("una", "Passw0rd!Passw0rd")
	if code := u.do("GET", "/api/v1/admin/secrets", nil, nil); code != 403 {
		t.Fatalf("status for a user: %d", code)
	}
}
