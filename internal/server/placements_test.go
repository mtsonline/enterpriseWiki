package server_test

import (
	"fmt"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// TestPlacements: who may change which level, what is accepted, required
// blocks, the levels that apply to a page (only categories the caller may
// see) and the clean-up with categories and people.
func TestPlacements(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var top, sub, secret, u obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Top"}}, &top, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Sub"}}, &sub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Secret"}}, &secret, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", top.ID), map[string]any{"subject_type": "authenticated", "permissions": 1}, nil, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "pia", "password": "Passw0rd!Passw0rd"}, &u, 201)
	pia := e.client()
	pia.login("pia", "Passw0rd!Passw0rd")
	set := func(c *client, level, target string, pls []map[string]any, want int) {
		t.Helper()
		path := "/api/v1/placements/" + level
		if target != "" {
			path += "?target=" + target
		}
		if code := c.do("PUT", path, map[string]any{"placements": pls}, nil); code != want {
			t.Fatalf("set %s %s %v: %d, want %d", level, target, pls, code, want)
		}
	}
	// the site: only with the settings permission; required blocks
	set(pia, "site", "", []map[string]any{}, 403)
	set(admin, "site", "", []map[string]any{{"block": "core.search", "position": "footer", "sort": 10}, {"block": "core.user", "position": "header-tools", "required": true}}, 200)
	// validation
	for _, bad := range []map[string]any{
		{"block": "nobody.thing", "position": "footer"},
		{"block": "core.search", "position": "<script>"},
		{"block": "core.search", "position": "footer", "sort": -1},
		{"block": "../etc", "position": "footer"},
	} {
		set(admin, "site", "", []map[string]any{bad}, 400)
	}
	set(admin, "site", "", []map[string]any{{"block": "core.search", "position": "footer"}, {"block": "core.search", "position": ""}}, 400)
	set(admin, "site", "", []map[string]any{{"block": "core.search", "position": "footer"}, {"block": "core.search", "position": "footer"}}, 400)
	set(admin, "site", "", []map[string]any{{"block": "core.search", "position": "footer", "sort": 10}, {"block": "core.user", "position": "header-tools", "required": true}}, 200)
	// a category: its managers; required blocks cannot be changed there; not required on lower levels
	tgt := fmt.Sprint(top.ID)
	set(pia, "category", tgt, []map[string]any{}, 403)
	set(pia, "category", fmt.Sprint(secret.ID), []map[string]any{}, 404)
	set(admin, "category", tgt, []map[string]any{{"block": "core.user", "position": ""}}, 400)
	set(admin, "category", tgt, []map[string]any{{"block": "core.tree", "position": "", "required": true}}, 400)
	set(admin, "category", tgt, []map[string]any{{"block": "core.tree", "position": ""}}, 200)
	set(admin, "category", fmt.Sprint(secret.ID), []map[string]any{{"block": "core.lang", "position": ""}}, 200)
	// the own level: only with personal placement switched on, only the own
	set(pia, "user", fmt.Sprint(u.ID), []map[string]any{}, 403)
	if err := e.app.Config.Update(func(c *config.Config) error { c.UI.PersonalPlacement = true; return nil }); err != nil {
		t.Fatal(err)
	}
	set(pia, "user", "1", []map[string]any{}, 403)
	set(pia, "user", fmt.Sprint(u.ID), []map[string]any{{"block": "core.bell", "position": "sidebar"}}, 200)
	// the levels of a page: site, the category with its ancestors (only visible ones), own
	type level struct {
		Level  string `json:"level"`
		Target string `json:"target"`
	}
	var levels []level
	pia.must("GET", fmt.Sprintf("/api/v1/placements?category=%d", sub.ID), nil, &levels, 200)
	if fmt.Sprint(levels) != fmt.Sprintf("[{site } {category %d} {user %d}]", top.ID, u.ID) {
		t.Fatalf("levels of a page: %v", levels)
	}
	pia.must("GET", fmt.Sprintf("/api/v1/placements?category=%d", secret.ID), nil, &levels, 200)
	if fmt.Sprint(levels) != fmt.Sprintf("[{site } {user %d}]", u.ID) {
		t.Fatalf("a hidden category leaked its placements: %v", levels)
	}
	guest := e.client()
	guest.must("GET", fmt.Sprintf("/api/v1/placements?category=%d", top.ID), nil, &levels, 200)
	if len(levels) != 1 || levels[0].Level != "site" {
		t.Fatalf("guest: %v", levels)
	}
	// clean-up: a purged category and a deleted person take their placements along
	admin.must("DELETE", fmt.Sprintf("/api/v1/categories/%d", secret.ID), nil, nil, 204)
	admin.must("DELETE", fmt.Sprintf("/api/v1/trash/category/%d", secret.ID), nil, nil, 204)
	admin.must("DELETE", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), nil, nil, 204)
	var n int
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM placements WHERE (level='category' AND target=$1) OR (level='user' AND target=$2)`, fmt.Sprint(secret.ID), fmt.Sprint(u.ID)).Scan(&n); err != nil || n != 0 {
		t.Fatalf("placements left: %d %v", n, err)
	}
	// instances of a block (a menu, a carousel, a content block) are blocks of
	// their own; deleting the instance removes its placements
	var menu obj
	admin.must("POST", "/api/v1/x/core.menu/menus", map[string]any{"name": "Side", "location": "sidebar", "items": []any{}}, &menu, 201)
	var carousel, block obj
	admin.must("POST", "/api/v1/x/core.carousel/carousels", map[string]any{"name": "Top", "position": "content-top", "audience": "all", "max_slides": 5, "enabled": true}, &carousel, 201)
	admin.must("POST", "/api/v1/x/core.blocks/blocks", map[string]any{"name": "Note", "location": "footer", "audience": "all", "enabled": true, "bodies": map[string]any{"en": map[string]any{"format": "markdown", "source": "x"}}}, &block, 201)
	inst := []map[string]any{
		{"block": fmt.Sprintf("core.menu/%d", menu.ID), "position": "footer"},
		{"block": fmt.Sprintf("core.menu/%d", menu.ID), "position": "sidebar", "sort": 5},
		{"block": fmt.Sprintf("core.carousel/%d", carousel.ID), "position": ""},
		{"block": fmt.Sprintf("core.blocks/%d", block.ID), "position": "sidebar"},
	}
	set(admin, "site", "", inst, 200)
	for _, bad := range []string{"core.menu/", "core.menu/a/b", "core.menu/<x>", "nobody.thing/1"} {
		set(admin, "site", "", []map[string]any{{"block": bad, "position": "footer"}}, 400)
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/x/core.menu/menus/%d", menu.ID), nil, nil, 204)
	admin.must("DELETE", fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d", carousel.ID), nil, nil, 204)
	admin.must("DELETE", fmt.Sprintf("/api/v1/x/core.blocks/blocks/%d", block.ID), nil, nil, 204)
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM placements WHERE block LIKE 'core.menu/%' OR block LIKE 'core.carousel/%' OR block LIKE 'core.blocks/%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("placements of deleted instances left: %d %v", n, err)
	}
}
