package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

func TestAuditLog(t *testing.T) {
	e := setup(t)
	if err := e.app.Config.Update(func(c *config.Config) error { c.Webhooks.AllowPrivate = true; return nil }); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	// failed and successful sign-ins with IP address and browser
	bad := e.client()
	if code := bad.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong"}, nil); code != 401 {
		t.Fatalf("wrong password: %d", code)
	}
	type res struct {
		Entries []struct {
			Action    string          `json:"action"`
			Actor     string          `json:"actor"`
			IP        string          `json:"ip"`
			UserAgent string          `json:"user_agent"`
			Via       string          `json:"via"`
			After     json.RawMessage `json:"after"`
		} `json:"entries"`
		Total int      `json:"total"`
		Areas []string `json:"areas"`
	}
	var r res
	admin.must("GET", "/api/v1/admin/audit/search?area=auth", nil, &r, 200)
	actions := map[string]bool{}
	for _, x := range r.Entries {
		actions[x.Action] = true
		if x.IP == "" {
			t.Errorf("no IP address in %s", x.Action)
		}
	}
	if !actions["auth.login"] || !actions["auth.login_failed"] {
		t.Fatalf("sign-ins not logged: %+v", r.Entries)
	}
	// text search and filters
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Zebra handbook"}}, &c, 201)
	admin.must("GET", "/api/v1/admin/audit/search?q=zebra", nil, &r, 200)
	if r.Total != 1 || r.Entries[0].Action != "category.create" || r.Entries[0].Via != "session" {
		t.Fatalf("text search: %+v", r)
	}
	admin.must("GET", "/api/v1/admin/audit/search?action=category.*&from=2000-01-01", nil, &r, 200)
	if r.Total < 1 {
		t.Fatal("action prefix filter")
	}
	// switch an area off (security areas stay on)
	recv := make(chan []byte, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Ewiki-Signature") == "" {
			w.WriteHeader(400)
			return
		}
		recv <- b
	}))
	t.Cleanup(srv.Close)
	admin.must("PUT", "/api/v1/admin/audit/settings", map[string]any{"disabled": []string{"category", "user"}, "log_logins": true, "log_denied": true,
		"forward": []map[string]any{{"id": "hook", "name": "SIEM", "type": "webhook", "address": srv.URL, "secret": "s3cret", "enabled": true},
			{"id": "file", "name": "File", "type": "file", "address": "audit.jsonl", "enabled": true}}}, nil, 204)
	var st struct {
		Settings struct {
			Disabled []string `json:"disabled"`
			Forward  []struct {
				Secret string `json:"secret"`
			} `json:"forward"`
		} `json:"settings"`
	}
	admin.must("GET", "/api/v1/admin/audit/settings", nil, &st, 200)
	if fmt.Sprint(st.Settings.Disabled) != "[category]" || st.Settings.Forward[0].Secret != "********" {
		t.Fatalf("settings: %+v", st.Settings)
	}
	// a test or save with the mask and another address is refused: the
	// stored secret never goes to an address nobody entered it for
	var stolen atomic.Int32
	thief := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { stolen.Add(1) }))
	defer thief.Close()
	moved := map[string]any{"id": "hook", "name": "SIEM", "type": "webhook", "address": thief.URL, "secret": "********", "enabled": true}
	if code := admin.do("POST", "/api/v1/admin/audit/forward/test", moved, nil); code != 400 {
		t.Fatalf("test with the mask to another address: %d", code)
	}
	if code := admin.do("PUT", "/api/v1/admin/audit/settings", map[string]any{"forward": []map[string]any{moved}}, nil); code != 400 {
		t.Fatalf("saved the mask for another address: %d", code)
	}
	if stolen.Load() != 0 {
		t.Fatal("the stored secret was sent to the new address")
	}
	if code := admin.do("PUT", "/api/v1/admin/audit/settings", map[string]any{"forward": []map[string]any{{"name": "x", "type": "file", "address": "../etc/passwd"}}}, nil); code != 400 {
		t.Fatalf("file outside the log folder accepted: %d", code)
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Yak handbook"}}, nil, 201)
	admin.must("GET", "/api/v1/admin/audit/search?q=yak", nil, &r, 200)
	if r.Total != 0 {
		t.Fatalf("disabled area logged: %+v", r)
	}
	// denied requests are logged
	bob := e.client()
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Passw0rd!Passw0rd"}, nil, 201)
	bob.login("bob", "Passw0rd!Passw0rd")
	bob.do("GET", "/api/v1/admin/settings", nil, nil)
	admin.must("GET", "/api/v1/admin/audit/search?action=access.denied", nil, &r, 200)
	if r.Total != 1 || r.Entries[0].Actor != "bob@local" {
		t.Fatalf("denied request not logged: %+v", r)
	}
	// forwarding: new entries reach the webhook and the file
	if err := e.app.Workers.RunOnce(context.Background(), "audit.forward"); err != nil {
		t.Fatal(err)
	}
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Forwarded role"}, nil, 201)
	if err := e.app.Workers.RunOnce(context.Background(), "audit.forward"); err != nil {
		t.Fatal(err)
	}
	select {
	case b := <-recv:
		if !strings.Contains(string(b), "role.save") || !strings.Contains(string(b), "Forwarded role") {
			t.Fatalf("webhook body: %s", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing forwarded to the webhook")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(e.app.Config.Path()), "logs", "audit.jsonl"))
	if err != nil || !strings.Contains(string(data), `"action":"role.save"`) {
		t.Fatalf("file destination: %v %s", err, data)
	}
	// export
	resp, err := http.Get(e.srv.URL + "/api/v1/admin/audit/export?format=csv")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("export without sign-in: %d", resp.StatusCode)
	}
}
