package server_test

import (
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// TestChoiceCheckboxes: the length up to which lists of groups, roles and
// similar are checkboxes (longer ones: a list with a filter) is a site
// setting; 0 means the default, the server refuses values outside 0..50 on
// every way (API, CLI), and the site info carries the value in effect.
func TestChoiceCheckboxes(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var site struct {
		ChoiceCheckboxes int `json:"choice_checkboxes"`
	}
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if site.ChoiceCheckboxes != config.ChoiceCheckboxesDefault {
		t.Fatalf("default: %d", site.ChoiceCheckboxes)
	}
	var cfg struct {
		Config config.Config `json:"config"`
	}
	admin.must("GET", "/api/v1/admin/settings", nil, &cfg, 200)
	ui := cfg.Config.UI
	for _, n := range []int{-1, config.ChoiceCheckboxesMax + 1} {
		ui.ChoiceCheckboxes = n
		if got := admin.do("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil); got != 400 {
			t.Errorf("%d: status %d, want 400", n, got)
		}
	}
	ui.ChoiceCheckboxes = 12
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil, 200)
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if site.ChoiceCheckboxes != 12 {
		t.Fatalf("set: %d", site.ChoiceCheckboxes)
	}
	// CLI: checked like in the interface
	if code, _ := runCLI(t, e.cfg, "config", "set", "ui.choice_checkboxes", "51"); code == 0 {
		t.Error("config set accepted 51")
	}
	if code, out := runCLI(t, e.cfg, "config", "set", "ui.choice_checkboxes", "0"); code != 0 {
		t.Fatalf("config set: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "config", "get", "ui.choice_checkboxes"); code != 0 || strings.TrimSpace(out) != "0" {
		t.Fatalf("config get: %d %q", code, out)
	}
	// 0 through the API: the site info shows the default
	ui.ChoiceCheckboxes = 0
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil, 200)
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if site.ChoiceCheckboxes != config.ChoiceCheckboxesDefault {
		t.Fatalf("0 is the default: %d", site.ChoiceCheckboxes)
	}
}
