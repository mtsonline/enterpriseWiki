package server_test

import (
	"strconv"
	"testing"
)

func TestAlias(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var anna, bert struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "anna", "password": "Anna-Passw0rd!", "display_name": "Anna"}, &anna, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bert", "password": "Bert-Passw0rd!", "display_name": "Bert"}, &bert, 201)
	a, b := e.client(), e.client()
	a.login("anna", "Anna-Passw0rd!")
	b.login("bert", "Bert-Passw0rd!")

	// own user name: unique (case-insensitive), never a local user name, valid characters
	a.must("PUT", "/api/v1/me/alias", map[string]string{"alias": "anni"}, nil, 204)
	for alias, want := range map[string]int{"ANNI": 409, "admin": 409, "a b": 400, "x": 400, "no@at": 400} {
		if got := b.do("PUT", "/api/v1/me/alias", map[string]string{"alias": alias}, nil); got != want {
			t.Errorf("alias %q: %d, want %d", alias, got, want)
		}
	}
	// sign in with it; a local account named like it cannot be created
	c := e.client()
	c.login("anni", "Anna-Passw0rd!")
	var me meResp
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User == nil || me.User.Username != "anna" {
		t.Fatalf("signed in as %+v", me.User)
	}
	if got := admin.do("POST", "/api/v1/admin/users", map[string]any{"username": "Anni", "password": "Anni-Passw0rd!", "display_name": "x"}, nil); got != 409 {
		t.Fatalf("local user named like an alias: %d", got)
	}
	// administrators set it; users only see their own data
	admin.must("PUT", "/api/v1/admin/users/"+strconv.FormatInt(bert.ID, 10)+"/alias", map[string]string{"alias": "bertl"}, nil, 204)
	if got := a.do("PUT", "/api/v1/admin/users/"+strconv.FormatInt(bert.ID, 10)+"/alias", map[string]string{"alias": "x1"}, nil); got != 403 {
		t.Fatalf("user sets another alias: %d", got)
	}
	var list []struct {
		Alias *string `json:"alias"`
	}
	admin.must("GET", "/api/v1/admin/users?q=bertl", nil, &list, 200)
	if len(list) != 1 || list[0].Alias == nil || *list[0].Alias != "bertl" {
		t.Fatalf("user list %+v", list)
	}

	// from a sign-in provider: claim preferred_username becomes the wiki user name
	idp := newFakeOIDC(t)
	e.setPluginSettings(t, "auth.oidc", map[string]any{"issuer": idp.srv.URL, "client_id": "wiki", "client_secret": "s3cret",
		"claim_username": "email", "username_from": "preferred_username"})
	o := e.client()
	idp.login(t, e, o)
	var p struct {
		User struct {
			Alias       string `json:"alias"`
			AliasSource string `json:"alias_source"`
		} `json:"user"`
	}
	o.must("GET", "/api/v1/auth/me", nil, &p, 200)
	if p.User.Alias != "carol" || p.User.AliasSource != "oidc" {
		t.Fatalf("alias from the provider %+v", p.User)
	}
	if got := o.do("PUT", "/api/v1/me/alias", map[string]string{"alias": "caro"}, nil); got != 400 {
		t.Fatalf("managed alias changed: %d", got)
	}
	// a provider name that is taken stays unset, the sign-in works
	a.must("PUT", "/api/v1/me/alias", map[string]string{"alias": "dave"}, nil, 204)
	e.setPluginSettings(t, "auth.oidc", map[string]any{"issuer": idp.srv.URL, "client_id": "wiki", "client_secret": "s3cret",
		"claim_username": "email", "username_from": "nonexistent"})
	idp.login(t, e, e.client())
}
