package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
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
// pendingMFA signs in with a password and returns the token of the pending
// sign-in (the second factor is still missing).
func (c *client) pendingMFA(user, pw string) string {
	c.e.t.Helper()
	b, _ := json.Marshal(map[string]string{"username": user, "password": pw})
	req, _ := http.NewRequest("POST", c.e.srv.URL+"/api/v1/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "ewiki")
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Error struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		} `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != 401 || out.Error.Code != "mfa_required" || out.Error.Detail == "" {
		c.e.t.Fatalf("login of %s: %d %+v", user, res.StatusCode, out)
	}
	return out.Error.Detail
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

// TestCrossSiteImages: only profile photos of shared accounts are served
// across sites; other images stay on their site.
func TestCrossSiteImages(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/instance/sites", map[string]any{"name": "docs", "hostnames": []string{"docs.test"}}, nil, 201)
	d := e.client()
	d.host = "docs.test"
	d.login("admin", adminPW)
	pic := func(c color.RGBA) []byte {
		img := image.NewRGBA(image.Rect(0, 0, 3, 3))
		img.Set(1, 1, c)
		var b bytes.Buffer
		_ = png.Encode(&b, img)
		return b.Bytes()
	}
	_, secret := d.upload(t, "secret.png", pic(color.RGBA{200, 1, 1, 255}))
	_, photo := d.upload(t, "me.png", pic(color.RGBA{1, 200, 1, 255}))
	if secret.Hash == "" || photo.Hash == "" {
		t.Fatal("upload failed")
	}
	d.must("GET", "/api/v1/files/"+secret.Hash, nil, nil, 200)
	admin.must("GET", "/api/v1/files/"+secret.Hash, nil, nil, 404) // an image of docs is not served by main
	d.must("PUT", "/api/v1/me/profile", map[string]string{"photo": photo.Hash}, nil, 200)
	admin.must("GET", "/api/v1/files/"+photo.Hash, nil, nil, 200) // the profile photo of the shared account is
}
