package server_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// TestReadabilityHint: the hint on long sentences is off by default; only
// administrators switch it on for the site (off, offered, on) and set the
// limits, checked by the server; within that each person chooses for
// themselves (API and CLI); the site info carries what the editors need.
func TestReadabilityHint(t *testing.T) {
	e := setup(t)
	ctx := identity.With(context.Background(), identity.CLI())
	if _, err := e.app.Auth.CreateLocalUserUnchecked(ctx, auth.NewUser{Username: "rhea", Password: "Rhea-Passw0rd!", DisplayName: "Rhea"}); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	rhea := e.client()
	rhea.login("rhea", "Rhea-Passw0rd!")

	type siteInfo struct {
		Readability struct {
			Mode          string `json:"mode"`
			SentenceWords int    `json:"sentence_words"`
			WordLetters   int    `json:"word_letters"`
			MaxKB         int    `json:"max_kb"`
		} `json:"readability"`
	}
	var site siteInfo
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if r := site.Readability; r.Mode != "off" || r.SentenceWords != 20 || r.WordLetters != 22 || r.MaxKB != 512 {
		t.Fatalf("defaults: %+v", r)
	}
	type prefs struct {
		Prefs       map[string]string   `json:"prefs"`
		Options     map[string][]string `json:"options"`
		Readability struct {
			Site   string `json:"site"`
			Active bool   `json:"active"`
		} `json:"readability"`
	}
	state := func(c *client) prefs {
		var p prefs
		c.must("GET", "/api/v1/me/prefs", nil, &p, 200)
		return p
	}
	if p := state(rhea); p.Readability.Site != "off" || p.Readability.Active || strings.Join(p.Options["readability"], ",") != "on,off" {
		t.Fatalf("off by default: %+v", p)
	}
	// a person's own choice does nothing while the site has it off
	rhea.must("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"readability": "on"}}, nil, 204)
	if p := state(rhea); p.Readability.Active {
		t.Fatal("on although the site has it off")
	}
	if got := rhea.do("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"readability": "always"}}, nil); got != 400 {
		t.Errorf("invalid personal value: %d", got)
	}

	// only administrators change the site setting
	var cfg struct {
		Config config.Config `json:"config"`
	}
	admin.must("GET", "/api/v1/admin/settings", nil, &cfg, 200)
	ui := cfg.Config.UI
	ui.Readability = config.Readability{Mode: "offered", SentenceWords: 15, WordLetters: 18}
	if got := rhea.do("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil); got != 403 {
		t.Fatalf("a person changes the site setting: %d", got)
	}
	if got := e.client().do("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil); got != 401 && got != 403 {
		t.Fatalf("a guest changes the site setting: %d", got)
	}
	// invalid values are refused, the stored ones stay
	for name, r := range map[string]config.Readability{
		"mode":           {Mode: "always"},
		"few words":      {Mode: "on", SentenceWords: 4},
		"many words":     {Mode: "on", SentenceWords: 101},
		"short words":    {Mode: "on", WordLetters: 7},
		"long words":     {Mode: "on", WordLetters: 61},
		"negative words": {Mode: "on", SentenceWords: -1},
	} {
		bad := ui
		bad.Readability = r
		if got := admin.do("PUT", "/api/v1/admin/settings", map[string]any{"ui": bad}, nil); got != 400 {
			t.Errorf("%s: status %d, want 400", name, got)
		}
	}
	if err := e.app.Config.Update(func(c *config.Config) error { c.UI.Readability.Mode = "sometimes"; return nil }); err == nil {
		t.Error("config.yaml: invalid mode accepted")
	}
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if site.Readability.Mode != "off" {
		t.Fatalf("changed by refused requests: %+v", site.Readability)
	}

	// offered: on for those who switched it on
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil, 200)
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if r := site.Readability; r.Mode != "offered" || r.SentenceWords != 15 || r.WordLetters != 18 {
		t.Fatalf("offered: %+v", r)
	}
	if !state(rhea).Readability.Active || state(admin).Readability.Active {
		t.Fatal("offered: on only for the person who switched it on")
	}
	// on: on for everybody but those who switched it off
	ui.Readability.Mode = "on"
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil, 200)
	rhea.must("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"readability": "off"}}, nil, 204)
	if state(rhea).Readability.Active || !state(admin).Readability.Active {
		t.Fatal("on: off only for the person who switched it off")
	}
	// "" follows the site again
	rhea.must("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"readability": ""}}, nil, 204)
	if p := state(rhea); !p.Readability.Active || p.Prefs["readability"] != "" {
		t.Fatalf("follows the site: %+v", p)
	}
	// saving other settings keeps it
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"lists": map[string]any{"max_items": 0}}, nil, 200)
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if site.Readability.Mode != "on" {
		t.Fatalf("lost by another change: %+v", site.Readability)
	}

	// CLI: the person's choice and its effect
	if code, out := runCLI(t, e.cfg, "pref", "set", "readability", "off", "--as", "rhea"); code != 0 {
		t.Fatalf("pref set: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "pref", "list", "--as", "rhea"); code != 0 || !strings.Contains(out, `"readability": "off"`) || !strings.Contains(out, `"active": false`) {
		t.Fatalf("pref list: %d %s", code, out)
	}
	// CLI: the site setting in config.yaml, checked like in the interface
	if code, _ := runCLI(t, e.cfg, "config", "set", "ui.readability.mode", "sometimes"); code == 0 {
		t.Error("config set accepted an invalid mode")
	}
	if code, out := runCLI(t, e.cfg, "config", "set", "ui.readability.sentence_words", "25"); code != 0 {
		t.Fatalf("config set: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "config", "get", "ui.readability.sentence_words"); code != 0 || strings.TrimSpace(out) != "25" {
		t.Fatalf("config get: %d %q", code, out)
	}
}
