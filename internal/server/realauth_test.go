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
	"maps"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
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

// followForms submits the pages a browser would submit on its own: SAML
// messages in the HTTP-POST binding and, when confirm is set, Keycloak's
// logout confirmation. It returns the first response without such a form.
func followForms(t *testing.T, hc *http.Client, res *http.Response, confirm bool) *http.Response {
	t.Helper()
	for range 10 {
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		act := formAction.FindSubmatch(raw)
		vals := url.Values{}
		for _, m := range hiddenInput.FindAllSubmatch(raw, -1) {
			vals.Set(string(m[1]), html.UnescapeString(string(m[2])))
		}
		saml := vals.Get("SAMLRequest") != "" || vals.Get("SAMLResponse") != ""
		logout := confirm && bytes.Contains(raw, []byte(`name="confirmLogout"`))
		if act == nil || (!saml && !logout) {
			res.Body = io.NopCloser(bytes.NewReader(raw))
			return res
		}
		if logout {
			vals.Set("confirmLogout", "Logout")
		}
		target, err := res.Request.URL.Parse(html.UnescapeString(string(act[1])))
		if err != nil {
			t.Fatal(err)
		}
		if res, err = hc.PostForm(target.String(), vals); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("too many forms in a row")
	return nil
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

	sessionsAtKeycloak := func() int {
		var users []struct {
			ID string `json:"id"`
		}
		k.call("GET", k.admin("/users?username=dave&exact=true"), nil, &users, 200)
		var sessions []any
		k.call("GET", k.admin("/users/"+users[0].ID+"/sessions"), nil, &sessions, 200)
		return len(sessions)
	}
	signIn := func() {
		t.Helper()
		page, err := c.http.Get(e.srv.URL + "/api/v1/auth/sso/shib/start?next=/")
		if err != nil {
			t.Fatal(err)
		}
		// with a running Keycloak session no form is shown, the assertion comes at once
		if bytes.Contains(peek(t, page), []byte(`id="kc-form-login"`)) {
			page = signInAtKeycloak(t, c.http, page, "dave", pw)
		}
		followForms(t, c.http, page, false).Body.Close()
		if me := meOf(t, c); me.User == nil || me.User.Username != "dave" {
			t.Fatal("sign-in for the logout test failed")
		}
	}

	// single logout started in the wiki: a signed LogoutRequest to Keycloak,
	// its LogoutResponse leads back to the sign-in page
	if sessionsAtKeycloak() != 1 {
		t.Fatal("no Keycloak session after the sign-in")
	}
	var out struct {
		Redirect string `json:"redirect"`
	}
	c.must("POST", "/api/v1/auth/logout", nil, &out, 200)
	if !strings.HasPrefix(out.Redirect, k.base+"/realms/"+k.realm+"/protocol/saml") || !strings.Contains(out.Redirect, "SAMLRequest=") {
		t.Fatalf("saml logout redirect %q", out.Redirect)
	}
	res, err = c.http.Get(out.Redirect)
	if err != nil {
		t.Fatal(err)
	}
	res = followForms(t, c.http, res, false)
	res.Body.Close()
	if res.Request.URL.Path != "/login" || res.Request.URL.Query().Get("logged_out") != "1" {
		t.Fatalf("after the SAML logout at %s", res.Request.URL)
	}
	if n := sessionsAtKeycloak(); n != 0 {
		t.Fatalf("Keycloak session not ended by the wiki's LogoutRequest: %d", n)
	}

	// single logout started at Keycloak: Keycloak sends a LogoutRequest through
	// the browser, the wiki ends the session and answers
	signIn()
	res, err = c.http.Get(k.base + "/realms/" + k.realm + "/protocol/openid-connect/logout")
	if err != nil {
		t.Fatal(err)
	}
	followForms(t, c.http, res, true).Body.Close()
	if me := meOf(t, c); me.User != nil {
		t.Fatal("wiki session survived the logout started at Keycloak")
	}
	if n := sessionsAtKeycloak(); n != 0 {
		t.Fatalf("Keycloak session after its own logout: %d", n)
	}
}

// peek reads the body and puts it back.
func peek(t *testing.T, res *http.Response) []byte {
	t.Helper()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(raw))
	return raw
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

// ------------------------------------------------------------------ Active Directory

// TestRealActiveDirectory signs in against Samba as Active Directory domain
// controller (tests/auth/start.sh with EWIKI_AUTH_AD=1): LDAPS with the
// certificate of an own CA, strong authentication required by the server.
func TestRealActiveDirectory(t *testing.T) {
	env := realEnv(t, "EWIKI_TEST_AD_URL", "EWIKI_TEST_AD_CA", "EWIKI_TEST_AD_BIND_DN", "EWIKI_TEST_AD_BIND_PASSWORD", "EWIKI_TEST_AD_USER_PASSWORD")
	e := setup(t)
	pw := env["EWIKI_TEST_AD_USER_PASSWORD"]
	settings := map[string]any{
		"name": "Active Directory", "url": env["EWIKI_TEST_AD_URL"], "ca_cert": env["EWIKI_TEST_AD_CA"],
		"bind_dn": env["EWIKI_TEST_AD_BIND_DN"], "bind_password": env["EWIKI_TEST_AD_BIND_PASSWORD"],
		"base_dn": "DC=example,DC=test", "user_filter": "(&(objectClass=user)(sAMAccountName={username}))",
		"attr_username": "sAMAccountName", "attr_display_name": "displayName", "attr_email": "mail", "attr_subject": "objectGUID",
		"group_mapping": map[string]any{"Wiki Editors": "Editors", "Staff": "Staff"}, "keep_unmapped_groups": false,
	}
	e.setPluginSettings(t, "auth.ldap", settings)
	login := func(c *client, user, password string) int {
		return c.do("POST", "/api/v1/auth/login", map[string]string{"username": user, "password": password}, nil)
	}

	c := e.client()
	if got := login(c, "anna", "wrong-"+pw); got != 401 {
		t.Fatalf("wrong AD password: %d", got)
	}
	// a disabled account is refused by the domain controller
	if got := login(e.client(), "bernd", pw); got != 401 {
		t.Fatalf("disabled AD account signed in: %d", got)
	}
	c.login("anna", pw)
	me := meOf(t, c)
	if me.User == nil || me.User.Username != "anna" || me.User.Email != "anna@example.com" {
		t.Fatalf("ad user %+v", me.User)
	}
	var name struct {
		User struct {
			DisplayName string `json:"display_name"`
		} `json:"user"`
	}
	c.must("GET", "/api/v1/auth/me", nil, &name, 200)
	if name.User.DisplayName != "Anna Example" {
		t.Fatalf("display name %q", name.User.DisplayName)
	}
	// memberOf names the direct groups only
	if g := groupNames(t, e, me.User.Groups); !slices.Equal(g, []string{"Editors"}) {
		t.Fatalf("direct groups %v", g)
	}
	// the stable id is the objectGUID (16 bytes, hex), not the DN: renaming or
	// moving the account in the directory keeps the wiki account
	var subject string
	if err := e.app.DB.QueryRow(t.Context(), `SELECT subject FROM users WHERE username='anna' AND provider='ldap'`).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	if len(subject) != 32 || strings.Contains(subject, "=") {
		t.Fatalf("subject %q is not the objectGUID", subject)
	}

	// nested groups: the AD rule LDAP_MATCHING_RULE_IN_CHAIN finds Staff, which
	// contains Wiki Editors (the search also returns a referral, which is skipped)
	settings["group_attribute"] = ""
	settings["group_base_dn"] = "DC=example,DC=test"
	settings["group_filter"] = "(member:1.2.840.113556.1.4.1941:={dn})"
	e.setPluginSettings(t, "auth.ldap", settings)
	n := e.client()
	n.login("anna", pw)
	if g := groupNames(t, e, meOf(t, n).User.Groups); !slices.Equal(g, []string{"Editors", "Staff"}) {
		t.Fatalf("nested groups %v", g)
	}

	// StartTLS on port 389 works the same
	starttls := maps.Clone(settings)
	starttls["url"] = strings.Replace(env["EWIKI_TEST_AD_URL"], "ldaps://", "ldap://", 1)
	starttls["url"] = strings.Replace(starttls["url"].(string), ":636", ":389", 1)
	starttls["start_tls"] = true
	e.setPluginSettings(t, "auth.ldap", starttls)
	s := e.client()
	s.login("anna", pw)

	// without the CA the certificate is not trusted: no sign-in, no fallback to plain text
	untrusted := maps.Clone(settings)
	delete(untrusted, "ca_cert")
	e.setPluginSettings(t, "auth.ldap", untrusted)
	if got := login(e.client(), "anna", pw); got == 200 {
		t.Fatal("signed in although the server certificate is not trusted")
	}
	// without TLS the domain controller refuses the simple bind (strong authentication)
	plain := maps.Clone(starttls)
	plain["start_tls"] = false
	e.setPluginSettings(t, "auth.ldap", plain)
	if got := login(e.client(), "anna", pw); got == 200 {
		t.Fatal("signed in over plain LDAP")
	}
}

// ------------------------------------------------------------------ Shibboleth header mode

// TestRealShibbolethHeaderMode signs in through Apache with mod_shib in front
// of the wiki (tests/auth/start.sh with EWIKI_AUTH_SHIB=1), configured as in
// the documentation, and tries to forge the identity headers.
func TestRealShibbolethHeaderMode(t *testing.T) {
	env := realEnv(t, "EWIKI_TEST_SHIB_URL", "EWIKI_TEST_SHIB_BACKEND", "EWIKI_TEST_SHIB_PASSWORD")
	e := setup(t)
	ln, err := net.Listen("tcp", env["EWIKI_TEST_SHIB_BACKEND"])
	if err != nil {
		t.Fatalf("backend for Apache: %v", err)
	}
	backend := &http.Server{Handler: e.srv.Config.Handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = backend.Serve(ln) }()
	t.Cleanup(func() { _ = backend.Close() })
	front := env["EWIKI_TEST_SHIB_URL"]
	e.setPluginSettings(t, "auth.shib", map[string]any{"mode": "header", "name": "University",
		"header_username": "REMOTE_USER", "header_email": "mail", "header_display_name": "displayName",
		"header_groups": "isMemberOf", "header_separator": ";",
		"group_mapping": map[string]any{"Professors": "Professors"}, "keep_unmapped_groups": false})
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Server.BaseURL = front
		c.Server.TrustedProxies = []string{"127.0.0.1"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	forged := http.Header{"REMOTE_USER": {"admin"}, "Remote-User": {"admin"}, "eppn": {"admin"}, "uid": {"admin"},
		"mail": {"admin@example.com"}, "isMemberOf": {"Administrators;Professors"}}
	browser := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar, Timeout: 30 * time.Second}
	}
	get := func(hc *http.Client, u string, h http.Header) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", u, nil)
		maps.Copy(req.Header, h)
		res, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	whoami := func(hc *http.Client, base string) string {
		t.Helper()
		res := get(hc, base+"/api/v1/auth/me", nil)
		defer res.Body.Close()
		var me meResp
		if err := json.NewDecoder(res.Body).Decode(&me); err != nil {
			t.Fatalf("me: %v", err)
		}
		if me.User == nil {
			return ""
		}
		return me.User.Username + "/" + me.User.Provider
	}

	// forged headers directly at the wiki, from an address that is not the proxy
	direct := browser()
	direct.Transport = &http.Transport{DialContext: (&net.Dialer{LocalAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 2)}}).DialContext}
	res := get(direct, e.srv.URL+"/api/v1/auth/sso/shib/start", forged)
	res.Body.Close()
	if who := whoami(direct, e.srv.URL); who != "" {
		t.Fatalf("forged headers from a stranger signed in as %s (%d)", who, res.StatusCode)
	}
	if res.Request.URL.Path != "/login" || res.Request.URL.Query().Get("error") == "" {
		t.Fatalf("forged headers from a stranger not refused: %d %s", res.StatusCode, res.Request.URL)
	}

	// forged headers through Apache on paths outside the protected <Location>
	for _, path := range []string{
		"/api/v1/auth/sso/shib//start", "/api/v1/auth/sso//shib/start", "/api/v1/auth/sso/shib/./start",
		"/api/v1/auth/sso/%73hib/start", "/api/v1/auth/sso/shib/%73tart", "/api/v1/auth/sso/shib/start/",
		"/API/v1/auth/sso/shib/start", "/api/v1/auth/sso/shib/start%2f", "/api/v1//auth/sso/shib/start",
	} {
		hc := browser()
		hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if !strings.HasPrefix(req.URL.String(), front) {
				return http.ErrUseLastResponse // on to Keycloak: no session without a password
			}
			maps.Copy(req.Header, forged)
			return nil
		}
		res := get(hc, front+path, forged)
		res.Body.Close()
		if who := whoami(hc, front); who != "" {
			t.Fatalf("forged headers on %s signed in as %s", path, who)
		}
	}

	// the protected path without a Shibboleth session leads to Keycloak; with
	// headers named like attributes mod_shib refuses the request (spoof check)
	hc := browser()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res = get(hc, front+"/api/v1/auth/sso/shib/start", nil)
	res.Body.Close()
	if loc := res.Header.Get("Location"); res.StatusCode != http.StatusFound || !strings.Contains(loc, "/protocol/saml") {
		t.Fatalf("protected path without session: %d %s", res.StatusCode, loc)
	}
	res = get(hc, front+"/api/v1/auth/sso/shib/start", forged)
	res.Body.Close()
	if res.StatusCode < 400 || whoami(hc, front) != "" {
		t.Fatalf("forged attribute headers without session: %d", res.StatusCode)
	}

	// the real sign-in: Apache → Keycloak → Shibboleth session → wiki
	user := browser()
	page := get(user, front+"/api/v1/auth/sso/shib/start?next=/search", nil)
	res = followForms(t, user, signInAtKeycloak(t, user, page, "erika", env["EWIKI_TEST_SHIB_PASSWORD"]), false)
	res.Body.Close()
	if res.Request.URL.Path != "/search" {
		t.Fatalf("after the Shibboleth sign-in at %s", res.Request.URL)
	}
	if who := whoami(user, front); who != "erika/shib" {
		t.Fatalf("signed in as %q", who)
	}
	var me meResp
	if err := json.NewDecoder(get(user, front+"/api/v1/auth/me", nil).Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	if g := groupNames(t, e, me.User.Groups); !slices.Equal(g, []string{"Professors"}) || me.User.Email != "erika@example.com" {
		t.Fatalf("shibboleth groups %v email %q", g, me.User.Email)
	}

	// with a real Shibboleth session, no forged header may change who signs in
	// or which groups count – each header alone, also those mod_shib does not know
	for name, value := range forged {
		req, _ := http.NewRequest("POST", front+"/api/v1/auth/logout", nil)
		req.Header.Set("X-Requested-With", "ewiki")
		if res, err := user.Do(req); err != nil {
			t.Fatal(err)
		} else {
			res.Body.Close()
		}
		if who := whoami(user, front); who != "" {
			t.Fatalf("still signed in after logout: %s", who)
		}
		res = get(user, front+"/api/v1/auth/sso/shib/start?next=/", http.Header{name: value})
		res.Body.Close()
		switch who := whoami(user, front); who {
		case "":
			// refused by mod_shib (spoof check)
			t.Logf("forged %s: refused (%d)", name, res.StatusCode)
		case "erika/shib":
			var me meResp
			if err := json.NewDecoder(get(user, front+"/api/v1/auth/me", nil).Body).Decode(&me); err != nil {
				t.Fatal(err)
			}
			if g := groupNames(t, e, me.User.Groups); !slices.Equal(g, []string{"Professors"}) || me.User.IsAdmin || me.User.Email != "erika@example.com" {
				t.Fatalf("forged header %s changed the identity: groups %v admin %v mail %q", name, g, me.User.IsAdmin, me.User.Email)
			}
			t.Logf("forged %s: ignored, signed in as erika", name)
		default:
			t.Fatalf("forged header %s with a real session signed in as %q", name, who)
		}
	}
}
