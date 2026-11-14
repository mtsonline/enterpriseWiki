package server_test

// Sign-in against real systems: Keycloak (OpenID Connect and SAML 2.0) and
// OpenLDAP. They complement the tests with replicated servers in
// sso_test.go. Start the systems with tests/auth/start.sh; without its
// environment the tests are skipped, with EWIKI_TEST_REQUIRE_REAL_AUTH=1
// (CI job "auth") they fail instead.

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func realEnv(t *testing.T, keys ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, k := range keys {
		v := os.Getenv(k)
		if v == "" {
			if os.Getenv("EWIKI_TEST_REQUIRE_REAL_AUTH") == "1" {
				t.Fatalf("%s not set (start the systems with tests/auth/start.sh)", k)
			}
			t.Skipf("%s not set: real sign-in systems are not running (tests/auth/start.sh)", k)
		}
		out[k] = v
	}
	return out
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// ------------------------------------------------------------------ Keycloak admin API

type keycloak struct {
	t     *testing.T
	base  string
	token string
	realm string
}

func newKeycloak(t *testing.T) *keycloak {
	t.Helper()
	env := realEnv(t, "EWIKI_TEST_KEYCLOAK_URL", "EWIKI_TEST_KEYCLOAK_ADMIN", "EWIKI_TEST_KEYCLOAK_PASSWORD")
	k := &keycloak{t: t, base: env["EWIKI_TEST_KEYCLOAK_URL"]}
	res, err := http.PostForm(k.base+"/realms/master/protocol/openid-connect/token", url.Values{
		"grant_type": {"password"}, "client_id": {"admin-cli"},
		"username": {env["EWIKI_TEST_KEYCLOAK_ADMIN"]}, "password": {env["EWIKI_TEST_KEYCLOAK_PASSWORD"]},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		t.Fatalf("keycloak admin token: %v %d", err, res.StatusCode)
	}
	k.token = tok.AccessToken
	// one realm per test: tests stay independent of each other
	k.realm = "ewiki-" + randomHex(t, 4)
	k.call("POST", "/admin/realms", map[string]any{"realm": k.realm, "enabled": true}, nil, 201)
	t.Cleanup(func() { k.call("DELETE", "/admin/realms/"+k.realm, nil, nil, 204) })
	return k
}

// call performs an admin API request; body is JSON or, as string, sent as it is.
func (k *keycloak) call(method, path string, body any, out any, want int) http.Header {
	k.t.Helper()
	var rd io.Reader
	ctype := "application/json"
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
		ctype = "application/xml"
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, k.base+path, rd)
	req.Header.Set("Authorization", "Bearer "+k.token)
	req.Header.Set("Content-Type", ctype)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		k.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		k.t.Fatalf("keycloak %s %s: %d %s", method, path, res.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			k.t.Fatalf("keycloak %s %s: %v", method, path, err)
		}
	}
	return res.Header
}

func (k *keycloak) admin(path string) string { return "/admin/realms/" + k.realm + path }

// user creates a user with a password and the given groups (created as needed).
func (k *keycloak) user(name, email, password string, groups ...string) string {
	for _, g := range groups {
		k.call("POST", k.admin("/groups"), map[string]any{"name": g}, nil, 201)
	}
	paths := make([]string, len(groups))
	for i, g := range groups {
		paths[i] = "/" + g
	}
	h := k.call("POST", k.admin("/users"), map[string]any{
		"username": name, "email": email, "emailVerified": true, "enabled": true,
		"firstName": strings.ToUpper(name[:1]) + name[1:], "lastName": "Example",
		"credentials": []map[string]any{{"type": "password", "value": password, "temporary": false}},
		"groups":      paths,
	}, nil, 201)
	loc := h.Get("Location")
	return loc[strings.LastIndex(loc, "/")+1:]
}

var (
	kcLoginForm = regexp.MustCompile(`<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"`)
	formAction  = regexp.MustCompile(`<form[^>]*action="([^"]+)"`)
	hiddenInput = regexp.MustCompile(`<input[^>]*name="([^"]+)"[^>]*value="([^"]*)"`)
)

// signInAtKeycloak fills in the login form of Keycloak (the page the browser
// was sent to) and returns the response after the redirects.
func signInAtKeycloak(t *testing.T, hc *http.Client, page *http.Response, user, password string) *http.Response {
	t.Helper()
	raw, _ := io.ReadAll(page.Body)
	page.Body.Close()
	m := kcLoginForm.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("no Keycloak login form (%d %s): %.400s", page.StatusCode, page.Request.URL, raw)
	}
	res, err := hc.PostForm(html.UnescapeString(string(m[1])), url.Values{"username": {user}, "password": {password}, "credentialId": {""}})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func meOf(t *testing.T, c *client) meResp {
	t.Helper()
	var me meResp
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	return me
}

// ------------------------------------------------------------------ OpenID Connect

func TestRealOIDCKeycloak(t *testing.T) {
	k := newKeycloak(t)
	e := setup(t)
	if _, err := e.app.Auth.EnsureRole(t.Context(), "Editors", "", false); err != nil {
		t.Fatal(err)
	}
	pw := randomHex(t, 12)
	uid := k.user("carol", "carol@example.com", pw, "wiki-editors", "sales")
	secret := randomHex(t, 16)
	sso := e.srv.URL + "/api/v1/auth/sso/oidc"
	k.call("POST", k.admin("/clients"), map[string]any{
		"clientId": "wiki", "protocol": "openid-connect", "publicClient": false, "secret": secret,
		"standardFlowEnabled": true, "redirectUris": []string{sso + "/callback"},
		"attributes": map[string]string{
			"backchannel.logout.url": sso + "/logout", "backchannel.logout.session.required": "true",
			"post.logout.redirect.uris": e.srv.URL + "/*", "pkce.code.challenge.method": "S256",
		},
		"protocolMappers": []map[string]any{{
			"name": "groups", "protocol": "openid-connect", "protocolMapper": "oidc-group-membership-mapper",
			"config": map[string]string{"claim.name": "groups", "full.path": "false", "id.token.claim": "true", "access.token.claim": "true", "userinfo.token.claim": "true"},
		}},
	}, nil, 201)
	e.setPluginSettings(t, "auth.oidc", map[string]any{
		"name": "Keycloak", "issuer": k.base + "/realms/" + k.realm, "client_id": "wiki", "client_secret": secret,
		"role_mapping": map[string]any{"wiki-editors": "Editors"}, "group_mapping": map[string]any{"sales": "Sales"}, "keep_unmapped_groups": false,
	})

	c := e.client()
	page, err := c.http.Get(sso + "/start?next=/search")
	if err != nil {
		t.Fatal(err)
	}
	// a wrong password stays at Keycloak, no session in the wiki
	bad := signInAtKeycloak(t, c.http, page, "carol", "wrong-"+pw)
	if !strings.HasPrefix(bad.Request.URL.String(), k.base) {
		t.Fatalf("wrong password left Keycloak: %s", bad.Request.URL)
	}
	if me := meOf(t, c); me.User != nil {
		t.Fatal("session without a valid sign-in")
	}
	res := signInAtKeycloak(t, c.http, bad, "carol", pw)
	res.Body.Close()
	if res.Request.URL.Path != "/search" {
		t.Fatalf("after sign-in at %s, want /search", res.Request.URL)
	}
	me := meOf(t, c)
	if me.User == nil || me.User.Username != "carol" || me.User.Provider != "oidc" || me.User.Email != "carol@example.com" {
		t.Fatalf("oidc user %+v", me.User)
	}
	if g := groupNames(t, e, me.User.Groups); !slices.Equal(g, []string{"Sales"}) {
		t.Fatalf("groups %v", g)
	}
	var roles int
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE r.name='Editors' AND ur.source='oidc'`).Scan(&roles); err != nil || roles != 1 {
		t.Fatalf("role mapping: %v %d", err, roles)
	}

	// back-channel logout: ending the session at Keycloak ends it in the wiki
	k.call("POST", k.admin("/users/"+uid+"/logout"), nil, nil, 204)
	if me := meOf(t, c); me.User != nil {
		t.Fatal("wiki session survived the back-channel logout")
	}

	// RP-initiated logout: signing out in the wiki continues at Keycloak
	page, err = c.http.Get(sso + "/start?next=/")
	if err != nil {
		t.Fatal(err)
	}
	signInAtKeycloak(t, c.http, page, "carol", pw).Body.Close()
	if me := meOf(t, c); me.User == nil {
		t.Fatal("second sign-in failed")
	}
	var out struct {
		Redirect string `json:"redirect"`
	}
	c.must("POST", "/api/v1/auth/logout", nil, &out, 200)
	if !strings.HasPrefix(out.Redirect, k.base+"/realms/"+k.realm+"/protocol/openid-connect/logout") {
		t.Fatalf("logout redirect %q", out.Redirect)
	}
	res, err = c.http.Get(out.Redirect)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	var sessions []any
	k.call("GET", k.admin("/users/"+uid+"/sessions"), nil, &sessions, 200)
	if len(sessions) != 0 {
		t.Fatalf("Keycloak session not ended: %v", sessions)
	}
}

// ------------------------------------------------------------------ SAML 2.0

func TestRealSAMLKeycloak(t *testing.T) {
	k := newKeycloak(t)
	e := setup(t)
	pw := randomHex(t, 12)
	k.user("dave", "dave@example.com", pw, "faculty", "library")
	idp := k.base + "/realms/" + k.realm + "/protocol/saml/descriptor"
	res, err := http.Get(idp)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("idp metadata: %v", err)
	}
	idpXML, _ := io.ReadAll(res.Body)
	res.Body.Close()
	e.setPluginSettings(t, "auth.shib", map[string]any{"mode": "saml", "idp_metadata": string(idpXML),
		"attr_username": "uid", "attr_email": "email", "attr_display_name": "displayName", "attr_groups": "groups",
		"group_mapping": map[string]any{"faculty": "Faculty"}, "keep_unmapped_groups": false})

	// Keycloak learns the wiki from its service provider metadata
	res, err = http.Get(e.srv.URL + "/api/v1/x/auth.shib/metadata")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("sp metadata: %v", err)
	}
	spXML, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var cl map[string]any
	k.call("POST", k.admin("/client-description-converter"), string(spXML), &cl, 200)
	attr := func(name, prop string) map[string]any {
		return map[string]any{"name": name, "protocol": "saml", "protocolMapper": "saml-user-property-mapper",
			"config": map[string]string{"user.attribute": prop, "attribute.name": name, "attribute.nameformat": "Basic"}}
	}
	cl["protocolMappers"] = []map[string]any{attr("uid", "username"), attr("email", "email"), attr("displayName", "firstName"),
		{"name": "groups", "protocol": "saml", "protocolMapper": "saml-group-membership-mapper",
			"config": map[string]string{"attribute.name": "groups", "full.path": "false", "single": "false", "attribute.nameformat": "Basic"}}}
	k.call("POST", k.admin("/clients"), cl, nil, 201)

	c := e.client()
	page, err := c.http.Get(e.srv.URL + "/api/v1/auth/sso/shib/start?next=/search")
	if err != nil {
		t.Fatal(err)
	}
	res = signInAtKeycloak(t, c.http, page, "dave", pw)
	// HTTP-POST binding: Keycloak answers with a form that the browser submits to the wiki
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	act := formAction.FindSubmatch(raw)
	if act == nil {
		t.Fatalf("no SAML response form: %.400s", raw)
	}
	vals := url.Values{}
	for _, m := range hiddenInput.FindAllSubmatch(raw, -1) {
		vals.Set(string(m[1]), html.UnescapeString(string(m[2])))
	}
	if vals.Get("SAMLResponse") == "" {
		t.Fatalf("no SAMLResponse: %.400s", raw)
	}
	res, err = c.http.PostForm(html.UnescapeString(string(act[1])), vals)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.Request.URL.Path != "/search" {
		t.Fatalf("after the assertion at %s, want /search", res.Request.URL)
	}
	me := meOf(t, c)
	if me.User == nil || me.User.Username != "dave" || me.User.Provider != "shib" || me.User.Email != "dave@example.com" {
		t.Fatalf("saml user %+v", me.User)
	}
	if g := groupNames(t, e, me.User.Groups); !slices.Equal(g, []string{"Faculty"}) {
		t.Fatalf("saml groups %v", g)
	}
	// the same assertion cannot be used a second time
	other := e.client()
	res, err = other.noRedirect().PostForm(html.UnescapeString(string(act[1])), vals)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if me := meOf(t, other); me.User != nil {
		t.Fatal("replayed SAML assertion accepted")
	}
}

// ------------------------------------------------------------------ LDAP

func TestRealLDAP(t *testing.T) {
	env := realEnv(t, "EWIKI_TEST_LDAP_URL", "EWIKI_TEST_LDAP_BIND_DN", "EWIKI_TEST_LDAP_BIND_PASSWORD", "EWIKI_TEST_LDAP_USER_PASSWORD")
	e := setup(t)
	settings := map[string]any{
		"url": env["EWIKI_TEST_LDAP_URL"], "bind_dn": env["EWIKI_TEST_LDAP_BIND_DN"], "bind_password": env["EWIKI_TEST_LDAP_BIND_PASSWORD"],
		"base_dn": "ou=people,dc=example,dc=com", "user_filter": "(uid={username})",
		"role_mapping": map[string]any{"wiki-admins": "Administrators"}, "group_mapping": map[string]any{"engineering": "Engineers"},
		"keep_unmapped_groups": false,
	}
	e.setPluginSettings(t, "auth.ldap", settings)
	pw := env["EWIKI_TEST_LDAP_USER_PASSWORD"]
	c := e.client()
	for _, wrong := range []string{"wrong", ""} {
		if got := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "alice", "password": wrong}, nil); got != 401 {
			t.Fatalf("wrong LDAP password %q accepted: %d", wrong, got)
		}
	}
	// LDAP filter injection must not match other entries
	if got := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "*", "password": pw}, nil); got != 401 {
		t.Fatalf("wildcard user name accepted: %d", got)
	}
	c.login("alice", pw)
	me := meOf(t, c)
	if me.User == nil || me.User.Provider != "ldap" || me.User.Email != "alice@example.com" || !me.User.IsAdmin {
		t.Fatalf("ldap user %+v", me.User)
	}
	if g := groupNames(t, e, me.User.Groups); !slices.Equal(g, []string{"Engineers"}) {
		t.Fatalf("groups from memberOf: %v", g)
	}
	// the same with a group search instead of memberOf
	settings["group_attribute"] = ""
	settings["group_base_dn"] = "ou=groups,dc=example,dc=com"
	settings["group_filter"] = "(member={dn})"
	settings["group_mapping"] = map[string]any{"sales": "Sales"}
	e.setPluginSettings(t, "auth.ldap", settings)
	b := e.client()
	b.login("bob", pw)
	me = meOf(t, b)
	if g := groupNames(t, e, me.User.Groups); !slices.Equal(g, []string{"Sales"}) || me.User.IsAdmin {
		t.Fatalf("bob: groups %v admin %v", g, me.User.IsAdmin)
	}
	// connection test of the administration
	admin := e.client()
	admin.login("admin", adminPW)
	var res struct {
		OK bool `json:"ok"`
	}
	admin.must("POST", "/api/v1/x/auth.ldap/test", map[string]string{"username": "bob", "password": pw}, &res, 200)
	if !res.OK {
		t.Fatal("ldap test endpoint failed")
	}
}
