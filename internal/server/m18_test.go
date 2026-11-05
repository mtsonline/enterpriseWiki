package server_test

import (
	"fmt"
	"regexp"
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

// lastCode returns the six-digit code of the latest e-mail to addr.
func (c *capture) lastCode(t *testing.T, addr string) string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	re := regexp.MustCompile(`\b(\d{6})\b`)
	for i := len(c.msgs) - 1; i >= 0; i-- {
		for _, r := range c.msgs[i].Recipients {
			if r == addr {
				if m := re.FindStringSubmatch(decodeQP(string(c.msgs[i].Data))); m != nil {
					return m[1]
				}
			}
		}
	}
	t.Fatalf("no code mailed to %s", addr)
	return ""
}

// TestMFAEmailAndNetworks: second factor by e-mail, allowed networks.
func TestMFAEmailAndNetworks(t *testing.T) {
	e := setup(t)
	cp := &capture{}
	e.app.Mail.SetTransport(cp)
	admin := e.client()
	admin.login("admin", adminPW)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd-1!", "email": "bob@example.com"}, &u, 201)
	bob := e.client()
	bob.login("bob", "Bob-Passw0rd-1!")

	// enable codes by e-mail (confirmed with a code)
	bob.must("POST", "/api/v1/me/mfa/email/code", nil, nil, 204)
	bob.must("POST", "/api/v1/me/mfa/email/enable", map[string]string{"code": "000000x"}, nil, 401)
	bob.must("POST", "/api/v1/me/mfa/email/code", nil, nil, 204)
	bob.must("POST", "/api/v1/me/mfa/email/enable", map[string]string{"code": cp.lastCode(t, "bob@example.com")}, nil, 204)
	var st struct {
		Email bool `json:"email"`
	}
	bob.must("GET", "/api/v1/me/mfa", nil, &st, 200)
	if !st.Email {
		t.Fatal("e-mail factor not enabled")
	}

	// sign-in: password, then the mailed code
	b2 := e.client()
	b2.must("POST", "/api/v1/auth/login", map[string]string{"username": "bob", "password": "Bob-Passw0rd-1!"}, nil, 401)
	code := cp.lastCode(t, "bob@example.com")
	b2.must("POST", "/api/v1/auth/login", map[string]string{"username": "bob", "password": "Bob-Passw0rd-1!", "totp": "123"}, nil, 401)
	b2.must("POST", "/api/v1/auth/login", map[string]string{"username": "bob", "password": "Bob-Passw0rd-1!", "totp": code}, nil, 200)
	// a code works once
	b3 := e.client()
	b3.must("POST", "/api/v1/auth/login", map[string]string{"username": "bob", "password": "Bob-Passw0rd-1!", "totp": code}, nil, 401)

	// allowed networks: the own address must be included
	bob.must("PUT", "/api/v1/me/mfa/networks", map[string]any{"networks": []string{"10.0.0.0/8"}}, nil, 400)
	bob.must("PUT", "/api/v1/me/mfa/networks", map[string]any{"networks": []string{"bad"}}, nil, 400)
	var nets struct {
		Networks []string `json:"networks"`
	}
	bob.must("PUT", "/api/v1/me/mfa/networks", map[string]any{"networks": []string{"127.0.0.0/8", "10.1.2.3", "2001:db8::1/32"}}, &nets, 200)
	if fmt.Sprint(nets.Networks) != "[127.0.0.0/8 10.1.2.3/32 2001:db8::/32]" {
		t.Fatalf("networks: %v", nets.Networks)
	}
	bob.must("GET", "/api/v1/me/mfa", nil, nil, 200)
	// an administrator restricts bob to another network: sessions stop working
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), map[string]any{"networks": []string{"10.0.0.0/8"}}, nil, 200)
	bob.must("GET", "/api/v1/me/mfa", nil, nil, 403)
	b4 := e.client()
	b4.must("POST", "/api/v1/auth/login", map[string]string{"username": "bob", "password": "Bob-Passw0rd-1!"}, nil, 403)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), map[string]any{"networks": []string{}, "reset_totp": true}, nil, 200)
	b4.must("POST", "/api/v1/auth/login", map[string]string{"username": "bob", "password": "Bob-Passw0rd-1!"}, nil, 200)
}

// TestSeparateAccounts: a site with its own users, groups and roles is
// isolated from the accounts of the instance.
func TestSeparateAccounts(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)

	// a first administrator is required
	admin.must("POST", "/api/v1/instance/sites", map[string]any{"name": "intra", "hostnames": []string{"intra.test"}, "accounts": "separate"}, nil, 400)
	admin.must("POST", "/api/v1/instance/sites", map[string]any{"name": "intra", "hostnames": []string{"intra.test"}, "accounts": "bogus",
		"admin": map[string]string{"username": "boss", "password": "Intra-Passw0rd!"}}, nil, 400)
	// weak password: rejected, no site left behind
	admin.must("POST", "/api/v1/instance/sites", map[string]any{"name": "intra", "hostnames": []string{"intra.test"}, "accounts": "separate",
		"admin": map[string]string{"username": "admin", "password": "short"}}, nil, 400)
	var sites []struct {
		Name     string `json:"name"`
		Accounts string `json:"accounts"`
	}
	admin.must("GET", "/api/v1/instance/sites", nil, &sites, 200)
	if len(sites) != 1 {
		t.Fatalf("failed creation left a site: %+v", sites)
	}
	// same user name as the instance administrator, but another account
	var si struct {
		Accounts string `json:"accounts"`
	}
	admin.must("POST", "/api/v1/instance/sites", map[string]any{"name": "intra", "hostnames": []string{"intra.test"}, "accounts": "separate",
		"admin": map[string]string{"username": "admin", "password": "Intra-Passw0rd!"}}, &si, 201)
	if si.Accounts != "separate" {
		t.Fatalf("accounts: %+v", si)
	}
	admin.must("PATCH", "/api/v1/instance/sites/intra", map[string]any{"accounts": "shared"}, nil, 400)

	in := e.client()
	in.host = "intra.test"
	// the password of the instance account does not work here
	in.must("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": adminPW}, nil, 401)
	in.login("admin", "Intra-Passw0rd!")
	var me struct {
		User struct {
			ID            int64 `json:"id"`
			IsAdmin       bool  `json:"is_admin"`
			InstanceAdmin bool  `json:"instance_admin"`
		} `json:"user"`
	}
	in.must("GET", "/api/v1/auth/me", nil, &me, 200)
	// the ID of the site account may equal that of an instance administrator:
	// it must never grant instance rights
	if !me.User.IsAdmin || me.User.InstanceAdmin {
		t.Fatalf("site administrator: %+v", me.User)
	}
	in.must("GET", "/api/v1/instance/sites", nil, nil, 403)
	in.must("POST", "/api/v1/instance/admins", map[string]any{"user_id": me.User.ID}, nil, 403)

	// the site manages its own accounts; the instance's are invisible
	in.must("POST", "/api/v1/admin/users", map[string]any{"username": "erika", "password": "Erika-Passw0rd!", "display_name": "Erika"}, nil, 201)
	var users []struct {
		Username string `json:"username"`
	}
	in.must("GET", "/api/v1/admin/users", nil, &users, 200)
	if len(users) != 2 {
		t.Fatalf("site users: %+v", users)
	}
	admin.must("GET", "/api/v1/admin/users", nil, &users, 200)
	for _, u := range users {
		if u.Username == "erika" {
			t.Fatal("account of the separate site visible on main")
		}
	}
	var look []struct {
		Handle string `json:"handle"`
	}
	in.must("GET", "/api/v1/users/lookup?q=adm", nil, &look, 200)
	if len(look) != 1 {
		t.Fatalf("lookup on the site sees other accounts: %+v", look)
	}

	// the license counts the accounts of all sites
	var lic struct {
		ActiveUsers int `json:"active_users"`
	}
	admin.must("GET", "/api/v1/admin/license", nil, &lic, 200)
	if lic.ActiveUsers != 3 { // admin (main) + admin and erika (intra)
		t.Fatalf("licensed users: %d", lic.ActiveUsers)
	}

	admin.must("DELETE", "/api/v1/instance/sites/intra", nil, nil, 204)
}
