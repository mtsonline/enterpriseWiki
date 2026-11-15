package server_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

// TestListRights: who may list users, groups and roles. By default
// signed-in users see only their own groups and roles and the people of
// their groups; with the *_all permissions everything; with neither
// nothing. Members and permissions are only shown to managers, and sharing
// accepts only what the caller may list.
func TestListRights(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)

	var r1, r2, ga, gb struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Readers", "permissions": []string{}}, &r1, 200)
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Secret role", "permissions": []string{}}, &r2, 200)
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Team A", "roles": []int64{r1.ID}}, &ga, 200)
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Team B"}, &gb, 200)
	user := func(name string, groups ...int64) int64 {
		var u struct {
			ID int64 `json:"id"`
		}
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": "List-Passw0rd!" + name, "display_name": name}, &u, 201)
		for _, g := range groups {
			admin.must("PUT", fmt.Sprintf("/api/v1/admin/groups/%d/members/%d", g, u.ID), nil, nil, 200)
		}
		return u.ID
	}
	user("lisa", ga.ID)
	otto := user("otto", ga.ID)
	zoe := user("zoe", gb.ID)
	lisa := e.client()
	lisa.login("lisa", "List-Passw0rd!lisa")

	names := func(path string) ([]string, []map[string]json.RawMessage) {
		t.Helper()
		var list []map[string]json.RawMessage
		lisa.must("GET", path, nil, &list, 200)
		var out []string
		for _, x := range list {
			var n string
			_ = json.Unmarshal(x["name"], &n)
			out = append(out, n)
		}
		return out, list
	}
	found := func(q string) []string {
		t.Helper()
		var list []struct {
			Handle string `json:"handle"`
		}
		lisa.must("GET", "/api/v1/users/lookup?q="+q, nil, &list, 200)
		var out []string
		for _, u := range list {
			out = append(out, u.Handle)
		}
		return out
	}
	var c struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Shared"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	share := func(users, groups, roles []int64) int {
		return lisa.do("POST", "/api/v1/x/core.share/check", map[string]any{"target_type": "category", "target_id": c.ID, "users": users, "groups": groups, "roles": roles}, nil)
	}

	// default: only the own ones, names without members or permissions
	gs, raw := names("/api/v1/groups")
	if !slices.Equal(gs, []string{"Team A"}) {
		t.Fatalf("own groups: %v", gs)
	}
	if _, ok := raw[0]["members"]; ok {
		t.Fatalf("members shown without core.groups.manage: %s", raw[0]["members"])
	}
	rs, raw := names("/api/v1/roles")
	if !slices.Equal(rs, []string{"Readers"}) {
		t.Fatalf("own roles: %v", rs)
	}
	if _, ok := raw[0]["permissions"]; ok {
		t.Fatalf("permissions shown without core.roles.manage")
	}
	if got := found("ot"); len(got) != 1 {
		t.Fatalf("user of the own group: %v", got)
	}
	if got := found("zo"); len(got) != 0 {
		t.Fatalf("user of another group: %v", got)
	}
	if code := share(nil, []int64{gb.ID}, nil); code != 403 {
		t.Fatalf("sharing with another group: %d", code)
	}
	if code := share([]int64{zoe}, nil, nil); code != 403 {
		t.Fatalf("sharing with a user of another group: %d", code)
	}
	if code := share(nil, nil, []int64{r2.ID}); code != 403 {
		t.Fatalf("sharing with another role: %d", code)
	}

	// all
	var defs struct {
		Authenticated []string `json:"authenticated"`
	}
	admin.must("GET", "/api/v1/admin/permissions", nil, &defs, 200)
	base := slices.DeleteFunc(slices.Clone(defs.Authenticated), func(k string) bool {
		return slices.Contains([]string{"core.users.list_own", "core.groups.list_own", "core.roles.list_own"}, k)
	})
	setDefaults := func(extra ...string) {
		admin.must("PUT", "/api/v1/admin/permissions/authenticated", map[string]any{"permissions": append(slices.Clone(base), extra...)}, nil, 204)
		lisa = e.client()
		lisa.login("lisa", "List-Passw0rd!lisa")
	}
	setDefaults("core.users.list_all", "core.groups.list_all", "core.roles.list_all")
	if gs, _ := names("/api/v1/groups"); len(gs) < 2 || !slices.Contains(gs, "Team B") {
		t.Fatalf("all groups: %v", gs)
	}
	if rs, _ := names("/api/v1/roles"); !slices.Contains(rs, "Secret role") {
		t.Fatalf("all roles: %v", rs)
	}
	if got := found("zo"); len(got) != 1 {
		t.Fatalf("all users: %v", got)
	}
	if code := share([]int64{zoe, otto}, []int64{gb.ID}, []int64{r2.ID}); code != 200 {
		t.Fatalf("sharing with all: %d", code)
	}

	// none
	setDefaults()
	for _, p := range []string{"/api/v1/groups", "/api/v1/roles", "/api/v1/users/lookup?q=ot"} {
		if code := lisa.do("GET", p, nil, nil); code != 403 {
			t.Fatalf("%s without permission: %d", p, code)
		}
	}
	if code := share([]int64{otto}, nil, nil); code != 403 {
		t.Fatalf("sharing without permission: %d", code)
	}

	// managers see members and permissions
	var groups []map[string]json.RawMessage
	admin.must("GET", "/api/v1/groups", nil, &groups, 200)
	if _, ok := groups[0]["members"]; !ok {
		t.Fatalf("no members for an administrator: %v", groups[0])
	}
}
