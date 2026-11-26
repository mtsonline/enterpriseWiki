package server_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Personal settings: only registered keys and values, per user, removable;
// the CLI (--as) shows and sets them.
func TestPrefs(t *testing.T) {
	e := setup(t)
	ctx := identity.With(context.Background(), identity.CLI())
	if _, err := e.app.Auth.CreateLocalUserUnchecked(ctx, auth.NewUser{Username: "carol", Password: "Carol-Passw0rd!", DisplayName: "Carol"}); err != nil {
		t.Fatal(err)
	}
	c := e.client()
	c.login("carol", "Carol-Passw0rd!")
	var p struct {
		Prefs   map[string]string   `json:"prefs"`
		Options map[string][]string `json:"options"`
	}
	c.must("GET", "/api/v1/me/prefs", nil, &p, 200)
	if len(p.Prefs) != 0 || len(p.Options["editor"]) != 2 {
		t.Fatalf("initial prefs %+v", p)
	}
	c.must("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"editor": "simple"}}, nil, 204)
	for _, bad := range []map[string]string{{"editor": "fancy"}, {"unknown": "x"}, {"editor": "<script>"}} {
		if got := c.do("PUT", "/api/v1/me", map[string]any{"prefs": bad}, nil); got != 400 {
			t.Errorf("prefs %v: status %d, want 400", bad, got)
		}
	}
	var me struct {
		User struct {
			Prefs map[string]string `json:"prefs"`
		} `json:"user"`
	}
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User.Prefs["editor"] != "simple" {
		t.Fatalf("prefs after update: %v", me.User.Prefs)
	}
	// other users are not affected
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("GET", "/api/v1/me/prefs", nil, &p, 200)
	if p.Prefs["editor"] != "" {
		t.Fatalf("admin prefs changed: %v", p.Prefs)
	}
	// help texts: off by default, "show" switches them on, nothing else
	if len(p.Options["help"]) != 1 || p.Options["help"][0] != "show" {
		t.Fatalf("help option: %v", p.Options["help"])
	}
	if got := c.do("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"help": "always"}}, nil); got != 400 {
		t.Errorf("help=always: %d", got)
	}
	c.must("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"help": "show"}}, nil, 204)
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User.Prefs["help"] != "show" || me.User.Prefs["editor"] != "simple" {
		t.Fatalf("prefs with help: %v", me.User.Prefs)
	}
	c.must("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"help": ""}}, nil, 204)
	// "" removes the setting
	c.must("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"editor": ""}}, nil, 204)
	c.must("GET", "/api/v1/me/prefs", nil, &p, 200)
	if _, ok := p.Prefs["editor"]; ok {
		t.Fatalf("editor not removed: %v", p.Prefs)
	}
	// guests have no settings
	if got := e.client().do("GET", "/api/v1/me/prefs", nil, nil); got != 401 {
		t.Errorf("guest prefs: %d", got)
	}
	if got := e.client().do("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"editor": "simple"}}, nil); got != 401 {
		t.Errorf("guest PUT prefs: %d", got)
	}
	// CLI
	if code, out := runCLI(t, e.cfg, "pref", "set", "editor", "simple", "--as", "carol"); code != 0 {
		t.Fatalf("pref set: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "pref", "list", "--as", "carol"); code != 0 || !strings.Contains(out, `"simple"`) {
		t.Fatalf("pref list: %d %s", code, out)
	}
	if code, _ := runCLI(t, e.cfg, "pref", "list"); code == 0 {
		t.Error("pref without --as must fail")
	}
}
