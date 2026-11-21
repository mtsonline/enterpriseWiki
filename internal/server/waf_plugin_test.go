package server_test

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/plugins/core_waf"
)

// TestWAFPlugin: the firewall plugin is shipped but off; switched on it
// logs only (default) or blocks attacks in addresses, lets the wiki's own
// requests with code in the content through, and skips static files.
func TestWAFPlugin(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var list struct {
		Installed []struct {
			ID         string `json:"id"`
			Enabled    bool   `json:"enabled"`
			Active     bool   `json:"active"`
			DefaultOff bool   `json:"default_off"`
		} `json:"installed"`
	}
	admin.must("GET", "/api/v1/admin/plugins", nil, &list, 200)
	found := false
	for _, p := range list.Installed {
		if p.ID == "core.waf" {
			found = true
			if p.Enabled || p.Active || !p.DefaultOff {
				t.Fatalf("core.waf not off by default: %+v", p)
			}
		}
	}
	if !found {
		t.Fatal("core.waf not shipped")
	}
	attack := "/api/v1/elements?x=" + url.QueryEscape("<script>alert(document.cookie)</script>")
	if code := admin.do("GET", attack, nil, nil); code == 403 {
		t.Fatal("blocked while the plugin is off")
	}
	admin.must("POST", "/api/v1/admin/plugins/core.waf/enable", nil, nil, 204)
	var st struct {
		Mode   string `json:"mode"`
		Loaded bool   `json:"loaded"`
	}
	admin.must("GET", "/api/v1/x/core.waf/status", nil, &st, 200)
	if st.Mode != "detect" || !st.Loaded {
		t.Fatalf("status: %+v", st)
	}
	// log only: the attack passes
	if code := admin.do("GET", attack, nil, nil); code == 403 {
		t.Fatal("mode detect blocked")
	}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"core.waf": map[string]any{"mode": "block", "paranoia": "1"}}}, nil, 200)
	if res, body := admin.raw(attack, nil); res.StatusCode != 403 || !strings.Contains(body, `"waf_blocked"`) {
		t.Fatalf("attack in the address: %d %s", res.StatusCode, body)
	}
	// a guest too, and other paths than the API
	guest := e.client()
	if code := guest.do("GET", "/wiki/x?q="+url.QueryEscape("' OR 1=1 --; <script>alert(1)</script>"), nil, nil); code != 403 {
		t.Fatalf("attack of a guest on a page: %d", code)
	}
	// the wiki's own requests with code, SQL and HTML in the content pass,
	// also PUT, and searching for code
	var cat struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "WAF"}}, &cat, 201)
	src := "Sort the list\nset the value\n```html\n<script src=\"a.js\"></script><button onclick=\"go()\">x</button>\n```\n```sql\nSELECT * FROM users WHERE id = 1 OR 1=1; DROP TABLE x; --\n```\n../../etc/hosts"
	var el struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Code <b>samples</b>", "body": map[string]any{"format": "markdown", "source": src}}, &el, 201)
	admin.must("PUT", "/api/v1/elements/"+itoa(el.ID), map[string]any{"title": "Code samples 2", "body": map[string]any{"format": "markdown", "source": src + "\n<?php system($_GET['x']); ?>"}}, nil, 200)
	admin.must("GET", "/api/v1/search?q="+url.QueryEscape("<script> SELECT * FROM users"), nil, nil, 200)
	// static files are not checked
	if code := guest.do("GET", "/assets/none.js?x="+url.QueryEscape("<script>alert(1)</script>"), nil, nil); code == 403 {
		t.Fatal("static files checked")
	}
	// strictness 2 still lets the wiki work
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"core.waf": map[string]any{"mode": "block", "paranoia": "2"}}}, nil, 200)
	admin.must("GET", "/api/v1/elements/"+itoa(el.ID), nil, nil, 200)
	// off again: nothing is checked
	admin.must("POST", "/api/v1/admin/plugins/core.waf/disable", nil, nil, 204)
	if code := admin.do("GET", attack, nil, nil); code == 403 {
		t.Fatal("blocked after switching off")
	}
}

// TestWAFPluginExclusions: the plugin carries the same exclusions as the
// file for other firewalls.
func TestWAFPluginExclusions(t *testing.T) {
	b, err := os.ReadFile("../../docker/waf/ewiki-crs-exclusions.conf")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != strings.TrimSpace(core_waf.Exclusions) {
		t.Fatal("plugins/core_waf/exclusions.conf differs from docker/waf/ewiki-crs-exclusions.conf")
	}
}
