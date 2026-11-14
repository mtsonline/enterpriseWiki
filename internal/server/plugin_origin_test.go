package server_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// localPlugin writes a minimal plugin folder.
func localPlugin(t *testing.T, dir, id string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"id":"`+id+`","name":"`+id+`","version":"1.0.0","kinds":["general"]}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "plugin.go"), []byte("package x\n"), 0o644)
	return dir
}

// TestCorePluginOrigin: external core plugins need a confirmation on every
// way, trusted sources need a recent sign-in, the list shows the origin.
func TestCorePluginOrigin(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	own := t.TempDir()
	kanban := localPlugin(t, filepath.Join(own, "kanban"), "acme.kanban")
	foreign := localPlugin(t, filepath.Join(t.TempDir(), "foreign"), "other.thing")

	// check before installing
	var chk struct {
		Level   string `json:"level"`
		Allowed bool   `json:"allowed"`
	}
	admin.must("POST", "/api/v1/admin/plugins/origin", map[string]any{"source": kanban}, &chk, 200)
	if chk.Level != "external" || !chk.Allowed {
		t.Fatalf("check %+v", chk)
	}
	// without confirmation: refused with a code the interface understands
	if code, body := admin.rawJSON("POST", "/api/v1/admin/plugins/install", map[string]any{"source": foreign}); code != 400 || errCode(body) != "external_not_accepted" {
		t.Fatalf("external without confirmation: %d %v", code, body)
	}
	var res struct {
		ID     string `json:"id"`
		Origin struct {
			Level    string `json:"level"`
			Accepted bool   `json:"accepted"`
		} `json:"origin"`
	}
	admin.must("POST", "/api/v1/admin/plugins/install", map[string]any{"source": foreign, "accept_external": true}, &res, 200)
	if res.ID != "other.thing" || res.Origin.Level != "external" || !res.Origin.Accepted {
		t.Fatalf("external install %+v", res)
	}

	// trusted sources: an API key cannot add one, a fresh session can
	var key struct {
		Token string `json:"token"`
	}
	admin.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "k", "permissions": 15, "allow_admin": true, "expires_at": time.Now().Add(time.Hour)}, &key, 201)
	kc := e.client()
	kc.token = key.Token
	if code := kc.do("POST", "/api/v1/admin/plugins/sources", map[string]any{"prefix": own}, nil); code != 403 {
		t.Fatalf("trusted source added with an API key: %d", code)
	}
	if code := admin.do("POST", "/api/v1/admin/plugins/sources", map[string]any{"prefix": "https://git.example.com"}, nil); code != 400 {
		t.Fatalf("whole host accepted: %d", code)
	}
	admin.must("POST", "/api/v1/admin/plugins/sources", map[string]any{"prefix": own, "note": "own team"}, nil, 200)
	admin.must("POST", "/api/v1/admin/plugins/install", map[string]any{"source": kanban}, &res, 200)
	if res.Origin.Level != "organisation" {
		t.Fatalf("trusted install %+v", res)
	}
	var list struct {
		Origins map[string]struct {
			Level string `json:"level"`
		} `json:"origins"`
	}
	admin.must("GET", "/api/v1/admin/plugins", nil, &list, 200)
	if list.Origins["acme.kanban"].Level != "organisation" || list.Origins["other.thing"].Level != "external" {
		t.Fatalf("origins %+v", list.Origins)
	}

	// a session older than 15 minutes must sign in again
	if _, err := e.app.DB.Exec(t.Context(), `UPDATE sessions SET created_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	old := e.client()
	old.login("admin", adminPW)
	if _, err := e.app.DB.Exec(t.Context(), `UPDATE sessions SET created_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if code, body := old.rawJSON("POST", "/api/v1/admin/plugins/sources", map[string]any{"prefix": "https://git.example.com/team"}); code != 403 || errCode(body) != "reauth_required" {
		t.Fatalf("old session added a trusted source: %d %v", code, body)
	}
	// forbidding is always possible; allowing again needs a recent sign-in
	old.must("PUT", "/api/v1/admin/plugins/sources/external", map[string]any{"allow": false}, nil, 200)
	if code := old.do("POST", "/api/v1/admin/plugins/install", map[string]any{"source": foreign, "accept_external": true}, nil); code != 403 {
		t.Fatalf("external installed while switched off: %d", code)
	}
	if code := old.do("PUT", "/api/v1/admin/plugins/sources/external", map[string]any{"allow": true}, nil); code != 403 {
		t.Fatalf("old session allowed external plugins: %d", code)
	}
	old.must("DELETE", "/api/v1/admin/plugins/sources?prefix="+own, nil, nil, 204)

	// the command line: the same rules through the same API
	if code, out := runCLI(t, e.cfg, "plugin", "sources", "external", "allow"); code != 0 {
		t.Fatalf("cli allow: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "plugin", "install", foreign); code == 0 || !strings.Contains(out, "--accept-external") {
		t.Fatalf("cli external install without the flag: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "plugin", "install", foreign, "--accept-external"); code != 0 || !strings.Contains(out, `"level": "external"`) {
		t.Fatalf("cli external install: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "plugin", "sources", "add", own, "--note", "team"); code != 0 {
		t.Fatalf("cli add: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "plugin", "sources", "check", kanban); code != 0 || !strings.Contains(out, `"level": "organisation"`) {
		t.Fatalf("cli check: %d %s", code, out)
	}
}

// errCode returns error.code of a decoded error answer.
func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}
