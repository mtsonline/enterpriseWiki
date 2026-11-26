package server_test

// The plugin security.crowdsec: rights, checks and the whole flow against a
// real CrowdSec Local API (tests/crowdsec/start.sh). Without its
// environment the part with CrowdSec is skipped; with
// EWIKI_TEST_REQUIRE_CROWDSEC=1 (CI job "crowdsec") it fails instead.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type csDecision struct {
	ID    int64  `json:"id"`
	Value string `json:"value"`
}

type csAllowlist struct {
	Entries []struct {
		Value string `json:"value"`
		Note  string `json:"note"`
	} `json:"entries"`
	Applied  []string `json:"applied"`
	Exchange bool     `json:"exchange"`
	InSync   bool     `json:"in_sync"`
	Error    string   `json:"error"`
}

// csUser creates a person with the given CrowdSec permissions and signs in.
func csUser(t *testing.T, e *env, admin *client, name string, perms ...string) *client {
	t.Helper()
	var role, u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "cs " + name, "permissions": perms}, &role, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": "Crowd-Sec-Pw-" + name, "display_name": name}, &u, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), map[string]any{"roles": []int64{role.ID}}, nil, 200)
	c := e.client()
	c.login(name, "Crowd-Sec-Pw-"+name)
	return c
}

func TestCrowdSec(t *testing.T) {
	t.Setenv("EWIKI_CROWDSEC_EXCHANGE", "") // set per part below
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)

	// not set up: the state says so, lists answer 409
	var st struct {
		MainSite   bool `json:"main_site"`
		Configured bool `json:"configured"`
		Connected  bool `json:"connected"`
		Exchange   bool `json:"exchange"`
	}
	admin.must("GET", "/api/v1/x/security.crowdsec/status", nil, &st, 200)
	if !st.MainSite || st.Configured || st.Connected || st.Exchange {
		t.Fatalf("status without settings: %+v", st)
	}
	admin.must("GET", "/api/v1/x/security.crowdsec/decisions", nil, nil, 409)

	// rights: nothing without them, viewing does not allow changes
	nobody := csUser(t, e, admin, "csnobody")
	nobody.must("GET", "/api/v1/x/security.crowdsec/status", nil, nil, 403)
	viewer := csUser(t, e, admin, "csviewer", "security.crowdsec.view")
	viewer.must("GET", "/api/v1/x/security.crowdsec/allowlist", nil, nil, 200)
	viewer.must("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{}}, nil, 403)
	viewer.must("DELETE", "/api/v1/x/security.crowdsec/decisions/1", nil, nil, 403)
	manager := csUser(t, e, admin, "csmanager", "security.crowdsec.manage")
	manager.must("GET", "/api/v1/x/security.crowdsec/status", nil, nil, 200) // manage includes view

	// without an exchange folder the list cannot change
	manager.must("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{}}, nil, 409)

	// an API key never changes CrowdSec (fresh browser session or the server only)
	dir := t.TempDir()
	t.Setenv("EWIKI_CROWDSEC_EXCHANGE", dir)
	var key struct {
		Token string `json:"token"`
	}
	manager.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "k", "permissions": 15, "expires_at": time.Now().Add(time.Hour)}, &key, 201)
	kc := e.client()
	kc.token = key.Token
	kc.must("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{}}, nil, 403)
	// a session older than 15 minutes must sign in again (a new session:
	// the wiki keeps sessions it has seen in memory for a short time)
	stale := e.client()
	stale.login("csmanager", "Crowd-Sec-Pw-csmanager")
	if _, err := e.app.DB.Exec(t.Context(), `UPDATE sessions SET created_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if code, body := stale.rawJSON("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{}}); code != 403 || errCode(body) != "reauth_required" {
		t.Fatalf("an old session changed the allowed addresses: %d %v", code, body)
	}
	manager = e.client()
	manager.login("csmanager", "Crowd-Sec-Pw-csmanager")
	admin = e.client()
	admin.login("admin", adminPW)

	// hostile and too broad values are refused, nothing is written
	for _, v := range []string{"0.0.0.0/0", "10.0.0.0/7", "::/0", "2001:db8::/16", "-rf", "1.2.3.4 --all", "1.2.3.4\n5.6.7.8", "example.com", "fe80::1%eth0", ""} {
		manager.must("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{map[string]any{"value": v}}}, nil, 400)
	}
	manager.must("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{
		map[string]any{"value": "192.0.2.10", "note": "a\nb"}}}, nil, 400)
	manager.must("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{
		map[string]any{"value": "192.0.2.10"}, map[string]any{"value": "192.0.2.10/32"}}}, nil, 400) // the same twice
	if _, err := os.Stat(filepath.Join(dir, "allowlist.txt")); err == nil {
		t.Fatal("a refused list was written")
	}

	// a valid list is saved in canonical form and written to the exchange file
	var al csAllowlist
	manager.must("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{
		map[string]any{"value": " 198.51.100.7/24 ", "note": "office"}, map[string]any{"value": "::ffff:192.0.2.10"}}}, &al, 200)
	if len(al.Entries) != 2 || al.Entries[0].Value != "192.0.2.10" || al.Entries[1].Value != "198.51.100.0/24" || al.Entries[1].Note != "office" {
		t.Fatalf("entries: %+v", al.Entries)
	}
	data, err := os.ReadFile(filepath.Join(dir, "allowlist.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(data)), "\n"); len(lines) != 3 || lines[1] != "192.0.2.10" || lines[2] != "198.51.100.0/24" || !strings.HasPrefix(lines[0], "#") {
		t.Fatalf("exchange file:\n%s", data)
	}
	var audit struct {
		Total int `json:"total"`
	}
	admin.must("GET", "/api/v1/admin/audit/search?action=security.crowdsec.allowlist", nil, &audit, 200)
	if audit.Total == 0 {
		t.Fatal("no audit entry for the allowed addresses")
	}
	manager.must("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{}}, nil, 200)

	// ---------------------------------------------------------------- real CrowdSec
	url, pw, cscli := os.Getenv("EWIKI_TEST_CROWDSEC_URL"), os.Getenv("EWIKI_TEST_CROWDSEC_PASSWORD"), os.Getenv("EWIKI_TEST_CROWDSEC_CSCLI")
	exchange := os.Getenv("EWIKI_TEST_CROWDSEC_EXCHANGE")
	if url == "" || pw == "" || cscli == "" || exchange == "" {
		if os.Getenv("EWIKI_TEST_REQUIRE_CROWDSEC") == "1" {
			t.Fatal("EWIKI_TEST_CROWDSEC_* not set (start CrowdSec with tests/crowdsec/start.sh)")
		}
		t.Skip("CrowdSec is not running (tests/crowdsec/start.sh)")
	}
	t.Setenv("EWIKI_CROWDSEC_EXCHANGE", exchange)
	run := func(args ...string) {
		t.Helper()
		f := strings.Fields(cscli)
		out, err := exec.CommandContext(context.Background(), f[0], append(f[1:], args...)...).CombinedOutput() // #nosec G204 -- test tool from tests/crowdsec/start.sh
		if err != nil {
			t.Fatalf("cscli %v: %v\n%s", args, err, out)
		}
	}
	// a wrong password says so
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"security.crowdsec": map[string]any{"url": url, "password": "wrong"}}}, nil, 200)
	var st2 struct {
		Connected bool   `json:"connected"`
		Error     string `json:"error"`
	}
	admin.must("GET", "/api/v1/x/security.crowdsec/status", nil, &st2, 200)
	if st2.Connected || !strings.Contains(st2.Error, "could not sign in") {
		t.Fatalf("wrong password: %+v", st2)
	}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"security.crowdsec": map[string]any{"url": url, "password": pw}}}, nil, 200)
	admin.must("GET", "/api/v1/x/security.crowdsec/status", nil, &st2, 200)
	if !st2.Connected {
		t.Fatalf("not connected: %+v", st2)
	}

	waitSync := func(want []string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			var a csAllowlist
			viewer.must("GET", "/api/v1/x/security.crowdsec/allowlist", nil, &a, 200)
			if a.InSync && fmt.Sprint(a.Applied) == fmt.Sprint(want) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("CrowdSec did not apply %v: %+v", want, a)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	// start from an empty allowlist (an earlier run or the examples may
	// have left one in CrowdSec)
	manager.must("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{}}, nil, 200)
	waitSync([]string{})

	// a block appears and can be lifted
	run("decisions", "add", "-i", "203.0.113.50", "-d", "1h", "-R", "ewiki test")
	find := func(v string) *csDecision {
		var list []csDecision
		viewer.must("GET", "/api/v1/x/security.crowdsec/decisions", nil, &list, 200)
		for _, d := range list {
			if d.Value == v {
				return &d
			}
		}
		return nil
	}
	d := find("203.0.113.50")
	if d == nil {
		t.Fatal("the block is not listed")
	}
	viewer.must("GET", "/api/v1/x/security.crowdsec/alerts?limit=5", nil, nil, 200)
	manager.must("DELETE", fmt.Sprintf("/api/v1/x/security.crowdsec/decisions/%d", d.ID), nil, nil, 204)
	if find("203.0.113.50") != nil {
		t.Fatal("the block is still there")
	}
	manager.must("DELETE", fmt.Sprintf("/api/v1/x/security.crowdsec/decisions/%d", d.ID), nil, nil, 404)
	admin.must("GET", "/api/v1/admin/audit/search?action=security.crowdsec.unblock", nil, &audit, 200)
	if audit.Total == 0 {
		t.Fatal("no audit entry for the lifted block")
	}

	// an allowed address: CrowdSec applies it and lifts its block
	run("decisions", "add", "-i", "198.51.100.20", "-d", "1h", "-R", "ewiki test")
	manager.must("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{map[string]any{"value": "198.51.100.0/24", "note": "office"}}}, nil, 200)
	waitSync([]string{"198.51.100.0/24"})
	if find("198.51.100.20") != nil {
		t.Fatal("the block of an allowed address was not lifted")
	}
	manager.must("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{}}, nil, 200)
	waitSync([]string{})

	// the script in the CrowdSec container trusts no line of the file: a
	// hostile file (written by someone else than the wiki) adds only the
	// valid addresses and cannot pass options to cscli
	run("decisions", "add", "-i", "203.0.113.60", "-d", "1h", "-R", "ewiki test")
	hostile := "# test\n--all\n-a\n192.0.2.77 --all\n192.0.2.78;rm -rf /\n$(id)\n192.0.2.79\n\n"
	if err := os.WriteFile(filepath.Join(exchange, "allowlist.txt"), []byte(hostile), 0o600); err != nil {
		t.Fatal(err)
	}
	applied := func() []string {
		var a csAllowlist
		viewer.must("GET", "/api/v1/x/security.crowdsec/allowlist", nil, &a, 200)
		return a.Applied
	}
	deadline := time.Now().Add(30 * time.Second)
	for fmt.Sprint(applied()) != "[192.0.2.79]" {
		if time.Now().After(deadline) {
			t.Fatalf("hostile file: applied %v", applied())
		}
		time.Sleep(200 * time.Millisecond)
	}
	if find("203.0.113.60") == nil {
		t.Fatal("a line of the file deleted other blocks")
	}
	// the wiki puts its own list back (the database is the reference)
	manager.must("PUT", "/api/v1/x/security.crowdsec/allowlist", map[string]any{"entries": []any{}}, nil, 200)
	waitSync([]string{})
}
