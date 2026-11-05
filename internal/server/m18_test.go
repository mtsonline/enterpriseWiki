package server_test

import (
	"fmt"
	"testing"
)

type siteInfo struct {
	Name      string   `json:"name"`
	Title     string   `json:"title"`
	Hostnames []string `json:"hostnames"`
	Disabled  bool     `json:"disabled"`
	Running   bool     `json:"running"`
}

// TestMultiSite: sites with their own content, sessions, rights and plugin
// switches; shared users; instance administrators.
func TestMultiSite(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)

	var me struct {
		User struct {
			InstanceAdmin bool `json:"instance_admin"`
		} `json:"user"`
	}
	admin.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if !me.User.InstanceAdmin {
		t.Fatal("the administrator of main must be an instance administrator")
	}

	// create a site
	var si siteInfo
	admin.must("POST", "/api/v1/instance/sites", map[string]any{"name": "docs", "title": "Docs", "hostnames": []string{"Docs.Example.TEST:8080"}}, &si, 201)
	if si.Name != "docs" || len(si.Hostnames) != 1 || si.Hostnames[0] != "docs.example.test" || !si.Running {
		t.Fatalf("site: %+v", si)
	}
	admin.must("POST", "/api/v1/instance/sites", map[string]any{"name": "docs2", "hostnames": []string{"docs.example.test"}}, nil, 409)
	admin.must("POST", "/api/v1/instance/sites", map[string]any{"name": "Bad-Name", "hostnames": []string{"x.test"}}, nil, 400)
	admin.must("POST", "/api/v1/instance/sites", map[string]any{"name": "nohost"}, nil, 400)
	var list []siteInfo
	admin.must("GET", "/api/v1/instance/sites", nil, &list, 200)
	if len(list) != 2 || list[0].Name != "main" {
		t.Fatalf("sites: %+v", list)
	}

	// a session of main is not valid on another site
	admin.host = "docs.example.test"
	var site struct {
		Site string `json:"site"`
		Name string `json:"name"`
	}
	admin.must("GET", "/api/v1/site", nil, &site, 200)
	if site.Site != "docs" {
		t.Fatalf("site by host: %+v", site)
	}
	admin.must("GET", "/api/v1/instance/sites", nil, nil, 401)
	// the shared account signs in on the site; it is an instance
	// administrator there as well
	admin.login("admin", adminPW)
	admin.must("GET", "/api/v1/instance/sites", nil, nil, 200)

	// separate content
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Docs only"}}, &c, 201)
	admin.host = ""
	var mainCats []cat
	admin.must("GET", "/api/v1/categories", nil, &mainCats, 200)
	for _, x := range mainCats {
		if x.Titles["en"] == "Docs only" {
			t.Fatal("category of docs visible on main")
		}
	}

	// shared users, rights per site: a site administrator of docs
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "siteadmin", "password": "Site-Admin-Pw1!", "display_name": "Site Admin"}, &u, 201)
	var roles []struct {
		ID      int64 `json:"id"`
		IsAdmin bool  `json:"is_admin"`
	}
	admin.host = "docs.example.test"
	admin.must("GET", "/api/v1/roles", nil, &roles, 200)
	var adminRole int64
	for _, r := range roles {
		if r.IsAdmin {
			adminRole = r.ID
		}
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), map[string]any{"roles": []int64{adminRole}}, nil, 200)

	sa := e.client()
	sa.host = "docs.example.test"
	sa.login("siteadmin", "Site-Admin-Pw1!")
	sa.must("GET", "/api/v1/admin/users", nil, nil, 200)
	// shared data and instance functions are for instance administrators
	sa.must("POST", "/api/v1/admin/users", map[string]any{"username": "x2", "password": "Xx-Passw0rd-12"}, nil, 403)
	sa.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), map[string]any{"display_name": "Renamed"}, nil, 403)
	sa.must("GET", "/api/v1/instance/sites", nil, nil, 403)
	sa.must("PUT", "/api/v1/admin/license", map[string]string{"key": "x"}, nil, 403)
	// role assignments of the own site are allowed
	sa.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), map[string]any{"roles": []int64{adminRole}}, nil, 200)
	// on main the same user has no rights
	sm := e.client()
	sm.login("siteadmin", "Site-Admin-Pw1!")
	sm.must("GET", "/api/v1/admin/users", nil, nil, 403)

	// plugins per site
	sa.must("POST", "/api/v1/admin/plugins/core.reactions/disable", nil, nil, 204)
	active := func(c *client) bool {
		var st struct {
			Plugins []string `json:"plugins"`
		}
		c.must("GET", "/api/v1/site", nil, &st, 200)
		for _, p := range st.Plugins {
			if p == "core.reactions" {
				return true
			}
		}
		return false
	}
	if active(sa) || !active(sm) {
		t.Fatal("plugin switches must be per site")
	}
	// settings of the site: own name, instance settings untouched
	var st struct {
		Config map[string]any `json:"config"`
	}
	sa.must("GET", "/api/v1/admin/settings", nil, &st, 200)
	ui := st.Config["ui"].(map[string]any)
	ui["site_name"] = "Docs Wiki"
	sa.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": ui}, nil, 200)
	sa.must("GET", "/api/v1/site", nil, &site, 200)
	if site.Name != "Docs Wiki" {
		t.Fatalf("site name of docs: %+v", site)
	}
	sm.must("GET", "/api/v1/site", nil, &site, 200)
	if site.Name == "Docs Wiki" {
		t.Fatal("site name of docs leaked to main")
	}

	// instance administrators
	admin.host = ""
	admin.must("POST", "/api/v1/instance/admins", map[string]any{"user_id": u.ID}, nil, 204)
	sa.must("GET", "/api/v1/instance/sites", nil, nil, 200)
	admin.must("DELETE", fmt.Sprintf("/api/v1/instance/admins/%d", u.ID), nil, nil, 204)
	sa.must("GET", "/api/v1/instance/sites", nil, nil, 403)

	// disable and delete
	admin.must("PATCH", "/api/v1/instance/sites/docs", map[string]any{"disabled": true}, &si, 200)
	if si.Running {
		t.Fatal("disabled site still running")
	}
	sa.must("GET", "/api/v1/site", nil, &site, 200)
	if site.Site != "main" {
		t.Fatalf("host of a disabled site must fall back to main: %+v", site)
	}
	admin.must("PATCH", "/api/v1/instance/sites/main", map[string]any{"disabled": true}, nil, 400)
	admin.must("DELETE", "/api/v1/instance/sites/main", nil, nil, 400)
	admin.must("DELETE", "/api/v1/instance/sites/docs", nil, nil, 204)
	admin.must("GET", "/api/v1/instance/sites", nil, &list, 200)
	if len(list) != 1 {
		t.Fatalf("sites after delete: %+v", list)
	}
}
