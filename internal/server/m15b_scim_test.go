package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"
)

type scimCall struct {
	t     *testing.T
	base  string
	token string
}

func (s scimCall) do(method, path string, body any) (int, map[string]any) {
	s.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, s.base+"/scim/v2"+path, r)
	req.Header.Set("Content-Type", "application/scim+json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	out := map[string]any{}
	data, _ := io.ReadAll(res.Body)
	if len(data) > 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			s.t.Fatalf("%s %s: %d %q", method, path, res.StatusCode, data)
		}
	}
	if res.StatusCode >= 500 {
		s.t.Errorf("%s %s: %d %s", method, path, res.StatusCode, data)
	}
	return res.StatusCode, out
}

func (s scimCall) must(method, path string, body any, want int) map[string]any {
	s.t.Helper()
	got, out := s.do(method, path, body)
	if got != want {
		s.t.Fatalf("%s %s: %d, want %d: %v", method, path, got, want, out)
	}
	return out
}

func TestSCIM(t *testing.T) {
	e := setup(t)
	idp := newFakeOIDC(t)
	e.setPluginSettings(t, "auth.oidc", map[string]any{"issuer": idp.srv.URL, "client_id": "wiki", "client_secret": "s3cret"})
	admin := e.client()
	admin.login("admin", adminPW)
	if got := admin.do("POST", "/api/v1/admin/scim/clients", map[string]any{"name": "x", "provider": "local"}, nil); got != 400 {
		t.Fatalf("client for local accounts: %d", got)
	}
	var created struct {
		Client struct {
			ID int64 `json:"id"`
		} `json:"client"`
		Token string `json:"token"`
		URL   string `json:"url"`
	}
	admin.must("POST", "/api/v1/admin/scim/clients", map[string]any{"name": "Entra", "provider": "oidc", "subject_from": "externalId"}, &created, 201)
	if created.URL != e.srv.URL+"/scim/v2" || len(created.Token) < 40 {
		t.Fatalf("client %+v", created)
	}
	sc := scimCall{t, e.srv.URL, created.Token}

	// authentication: only the client's token
	if got, _ := (scimCall{t, e.srv.URL, ""}).do("GET", "/ServiceProviderConfig", nil); got != 401 {
		t.Fatalf("no token: %d", got)
	}
	if got, _ := (scimCall{t, e.srv.URL, "scim_wrong"}).do("GET", "/Users", nil); got != 401 {
		t.Fatalf("wrong token: %d", got)
	}
	var key struct {
		Token string `json:"token"`
	}
	admin.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "k", "permissions": 15, "allow_admin": true, "expires_at": time.Now().Add(time.Hour)}, &key, 201)
	if got, _ := (scimCall{t, e.srv.URL, key.Token}).do("GET", "/Users", nil); got != 401 {
		t.Fatalf("API key at SCIM: %d", got)
	}
	if got := admin.do("GET", "/scim/v2/Users", nil, nil); got != 401 {
		t.Fatalf("session at SCIM: %d", got)
	}
	cfg := sc.must("GET", "/ServiceProviderConfig", nil, 200)
	if cfg["patch"].(map[string]any)["supported"] != true {
		t.Fatalf("config %v", cfg)
	}
	sc.must("GET", "/ResourceTypes", nil, 200)
	sc.must("GET", "/Schemas", nil, 200)

	// create a person (as Entra ID does: look up first, then create)
	list := sc.must("GET", "/Users?filter="+url.QueryEscape(`userName eq "carol@example.com"`), nil, 200)
	if list["totalResults"].(float64) != 0 {
		t.Fatalf("lookup %v", list)
	}
	user := map[string]any{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:User"}, "userName": "carol@example.com", "externalId": "u-123",
		"name": map[string]any{"givenName": "Carol", "familyName": "C"}, "emails": []any{map[string]any{"value": "carol@example.com", "type": "work", "primary": true}},
		"active": true}
	u := sc.must("POST", "/Users", user, 201)
	uid := u["id"].(string)
	if u["displayName"] != "Carol C" || u["active"] != true || u["meta"].(map[string]any)["location"] != e.srv.URL+"/scim/v2/Users/"+uid {
		t.Fatalf("created %v", u)
	}
	if got, out := sc.do("POST", "/Users", user); got != 409 || out["scimType"] != "uniqueness" {
		t.Fatalf("duplicate: %d %v", got, out)
	}
	for _, bad := range []map[string]any{{"userName": "with space"}, {"userName": ""}, {"userName": "x@y", "externalId": ""}} {
		if got, _ := sc.do("POST", "/Users", bad); got != 400 {
			t.Errorf("invalid user %v: %d", bad, got)
		}
	}
	if got, out := sc.do("GET", "/Users?filter="+url.QueryEscape(`title co "x"`), nil); got != 400 || out["scimType"] != "invalidFilter" {
		t.Fatalf("unsupported filter: %d %v", got, out)
	}
	list = sc.must("GET", "/Users?filter="+url.QueryEscape(`externalId eq "u-123"`), nil, 200)
	if list["totalResults"].(float64) != 1 {
		t.Fatalf("by externalId %v", list)
	}

	// groups: members only among the client's people
	g := sc.must("POST", "/Groups", map[string]any{"displayName": "Sales EMEA", "members": []any{map[string]any{"value": uid}}}, 201)
	gid := g["id"].(string)
	if len(g["members"].([]any)) != 1 {
		t.Fatalf("group %v", g)
	}
	if got, _ := sc.do("PATCH", "/Groups/"+gid, map[string]any{"Operations": []any{map[string]any{"op": "add", "path": "members", "value": []any{map[string]any{"value": "1"}}}}}); got != 400 {
		t.Fatalf("administrator added by SCIM: %d", got)
	}
	if got, _ := sc.do("GET", "/Users/1", nil); got != 404 {
		t.Fatalf("administrator visible: %d", got)
	}
	if got, _ := sc.do("POST", "/Groups", map[string]any{"displayName": "Sales EMEA"}); got != 409 {
		t.Fatalf("duplicate group: %d", got)
	}

	// the first sign-in finds the provisioned account (subject = externalId)
	c := e.client()
	idp.login(t, e, c)
	var me meResp
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User == nil || me.User.Email != "carol@example.com" || len(me.User.Groups) == 0 {
		t.Fatalf("signed-in user %+v", me.User)
	}
	var meID struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	c.must("GET", "/api/v1/auth/me", nil, &meID, 200)
	if strconv.FormatInt(meID.User.ID, 10) != uid {
		t.Fatalf("sign-in created another account: %d vs %s", meID.User.ID, uid)
	}
	if names := groupNames(t, e, me.User.Groups); !slices.Contains(names, "Sales EMEA") {
		t.Fatalf("SCIM groups lost at sign-in: %v", names)
	}

	// PATCH as Entra ID sends it: disable -> signed out at once
	sc.must("PATCH", "/Users/"+uid, map[string]any{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []any{map[string]any{"op": "Replace", "path": "active", "value": "False"}}}, 200)
	if signedIn(c) {
		t.Fatal("disabled person still signed in")
	}
	u = sc.must("PATCH", "/Users/"+uid, map[string]any{"Operations": []any{
		map[string]any{"op": "replace", "value": map[string]any{"active": true, "displayName": "Carol Clark"}},
		map[string]any{"op": "replace", "path": `emails[type eq "work"].value`, "value": "carol.clark@example.com"},
		map[string]any{"op": "add", "path": "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:department", "value": "Sales"},
	}}, 200)
	emails := u["emails"].([]any)
	if u["displayName"] != "Carol Clark" || u["active"] != true || len(emails) != 1 || emails[0].(map[string]any)["value"] != "carol.clark@example.com" {
		t.Fatalf("patched %v", u)
	}
	// groups: remove the member again
	g = sc.must("PATCH", "/Groups/"+gid, map[string]any{"Operations": []any{map[string]any{"op": "remove", "path": `members[value eq "` + uid + `"]`}}}, 200)
	if len(g["members"].([]any)) != 0 {
		t.Fatalf("member not removed %v", g)
	}
	// local accounts are out of reach
	var local struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "lou", "password": "Lou-Passw0rd!x", "display_name": "Lou"}, &local, 201)
	if got, _ := sc.do("PUT", "/Users/"+strconv.FormatInt(local.ID, 10), map[string]any{"userName": "lou", "externalId": "x"}); got != 404 {
		t.Fatalf("local account changed by SCIM: %d", got)
	}

	// delete: disabled by default; the group is deleted
	sc.must("DELETE", "/Groups/"+gid, nil, 204)
	sc.must("DELETE", "/Users/"+uid, nil, 204)
	var disabled bool
	if err := e.app.DB.QueryRow(t.Context(), `SELECT disabled FROM users WHERE id=$1`, uid).Scan(&disabled); err != nil || !disabled {
		t.Fatalf("deleted person: %v %v", err, disabled)
	}
	// a removed client's token stops working
	admin.must("DELETE", "/api/v1/admin/scim/clients/"+strconv.FormatInt(created.Client.ID, 10), nil, nil, 204)
	if got, _ := sc.do("GET", "/Users", nil); got != 401 {
		t.Fatalf("token of a removed client: %d", got)
	}
	// only administrators manage clients
	if got := c.do("GET", "/api/v1/admin/scim/clients", nil, nil); got != 401 && got != 403 {
		t.Fatalf("clients by a user: %d", got)
	}
}
