package server_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crewjam/saml"
	"github.com/go-jose/go-jose/v4"
	"github.com/jimlambrt/gldap/testdirectory"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
)

// browser is a client that does not follow redirects automatically.
func (c *client) noRedirect() *http.Client {
	return &http.Client{Jar: c.http.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (e *env) setPluginSettings(t *testing.T, id string, s map[string]any) {
	t.Helper()
	if err := e.app.Config.Update(func(c *config.Config) error {
		if c.Settings == nil {
			c.Settings = map[string]map[string]any{}
		}
		c.Settings[id] = s
		c.Server.BaseURL = e.srv.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type meResp struct {
	User *struct {
		Username string  `json:"username"`
		Provider string  `json:"provider"`
		Email    string  `json:"email"`
		Groups   []int64 `json:"groups"`
		IsAdmin  bool    `json:"is_admin"`
	} `json:"user"`
}

func groupNames(t *testing.T, e *env, ids []int64) []string {
	var names []string
	for _, id := range ids {
		var n string
		if err := e.app.DB.QueryRow(t.Context(), `SELECT name FROM groups WHERE id=$1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	return names
}

// ------------------------------------------------------------------ LDAP

func TestLDAPLogin(t *testing.T) {
	e := setup(t)
	d := testdirectory.Start(t, testdirectory.WithNoTLS(t), testdirectory.WithDefaults(t, &testdirectory.Defaults{AllowAnonymousBind: true}))
	users := testdirectory.NewUsers(t, []string{"alice", "bob"}, testdirectory.WithMembersOf(t, "admins"))
	d.SetUsers(users...)
	d.SetGroups(testdirectory.NewGroup(t, "engineering", []string{"alice"}))
	e.setPluginSettings(t, "auth.ldap", map[string]any{
		"url": fmt.Sprintf("ldap://%s:%d", d.Host(), d.Port()), "base_dn": testdirectory.DefaultUserDN,
		"user_filter": "(cn={username})", "attr_username": "name", "attr_email": "email", "attr_display_name": "name",
		"group_base_dn": testdirectory.DefaultGroupDN, "group_filter": "(member={dn})",
		"role_mapping":   map[string]any{"admins": "Administrators"},
		"group_mapping":  map[string]any{"engineering": "Engineers"},
		"default_groups": []any{"Staff"},
	})
	c := e.client()
	if got := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "alice", "password": "wrong"}, nil); got != 401 {
		t.Fatalf("wrong LDAP password accepted: %d", got)
	}
	if got := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "alice", "password": ""}, nil); got != 401 {
		t.Fatalf("empty LDAP password accepted: %d", got)
	}
	c.login("alice", "password")
	var me meResp
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User.Provider != "ldap" || me.User.Email != "alice@example.com" || !me.User.IsAdmin {
		t.Fatalf("ldap user %+v", me.User)
	}
	names := strings.Join(groupNames(t, e, me.User.Groups), ",")
	if !strings.Contains(names, "Engineers") || !strings.Contains(names, "Staff") || !strings.Contains(names, "admins") {
		t.Fatalf("groups %s", names)
	}
	// admin test endpoint
	var res struct {
		OK bool `json:"ok"`
	}
	c.must("POST", "/api/v1/x/auth.ldap/test", map[string]string{"username": "bob", "password": "password"}, &res, 200)
	if !res.OK {
		t.Fatal("ldap test endpoint failed")
	}
	// local accounts still win and work while LDAP is down
	e.setPluginSettings(t, "auth.ldap", map[string]any{"url": "ldap://127.0.0.1:1", "base_dn": "dc=x"})
	e.client().login("admin", adminPW)
	var providers []map[string]string
	e.client().must("GET", "/api/v1/auth/providers", nil, &providers, 200)
	providers = slices.DeleteFunc(providers, func(p map[string]string) bool { return p["kind"] == "passwordless" })
	if len(providers) != 2 {
		t.Fatalf("providers %v", providers)
	}
}

// ------------------------------------------------------------------ OIDC

type fakeOIDC struct {
	srv      *httptest.Server
	key      *rsa.PrivateKey
	codes    map[string]map[string]string
	clientID string
}

func newFakeOIDC(t *testing.T) *fakeOIDC {
	f := &fakeOIDC{codes: map[string]map[string]string{}, clientID: "wiki"}
	f.key, _ = rsa.GenerateKey(rand.Reader, 2048)
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize",
			"token_endpoint": f.srv.URL + "/token", "jwks_uri": f.srv.URL + "/jwks", "userinfo_endpoint": f.srv.URL + "/userinfo",
			"end_session_endpoint": f.srv.URL + "/logout?tenant=1", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("client_id") != f.clientID || q.Get("code_challenge_method") != "S256" {
			http.Error(w, "bad request", 400)
			return
		}
		code := fmt.Sprintf("code-%d", time.Now().UnixNano())
		f.codes[code] = map[string]string{"nonce": q.Get("nonce"), "challenge": q.Get("code_challenge"), "redirect": q.Get("redirect_uri")}
		http.Redirect(w, r, q.Get("redirect_uri")+"?code="+code+"&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		c, ok := f.codes[r.FormValue("code")]
		sum := sha256.Sum256([]byte(r.FormValue("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != c["challenge"] || r.FormValue("redirect_uri") != c["redirect"] {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		delete(f.codes, r.FormValue("code"))
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: f.key, KeyID: "k1"}}, (&jose.SignerOptions{}).WithType("JWT"))
		claims, _ := json.Marshal(map[string]any{"iss": f.srv.URL, "aud": f.clientID, "sub": "u-123", "exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(), "nonce": c["nonce"], "sid": "sid-" + r.FormValue("code"), "preferred_username": "carol", "email": "carol@example.com", "name": "Carol C",
			"groups": []string{"wiki-editors", "sales"}})
		jws, _ := signer.Sign(claims)
		tok, _ := jws.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": tok})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"sub": "u-123"})
	})
	return f
}

// sign returns claims as a JWT signed with key (the provider's by default).
func (f *fakeOIDC) sign(claims map[string]any, key *rsa.PrivateKey, typ jose.ContentType) string {
	if key == nil {
		key = f.key
	}
	signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}}, (&jose.SignerOptions{}).WithType(typ))
	b, _ := json.Marshal(claims)
	jws, _ := signer.Sign(b)
	tok, _ := jws.CompactSerialize()
	return tok
}

// login signs c in through the provider (start, authorize, callback).
func (f *fakeOIDC) login(t *testing.T, e *env, c *client) {
	t.Helper()
	if loc := f.loginAt(t, e, c, "oidc"); loc != "/" {
		t.Fatalf("callback redirect %s", loc)
	}
}

// loginAt runs the sign-in at provider id and returns where the callback
// sent the browser.
func (f *fakeOIDC) loginAt(t *testing.T, e *env, c *client, provider string) string {
	t.Helper()
	hc := c.noRedirect()
	res, err := hc.Get(e.srv.URL + "/api/v1/auth/sso/" + provider + "/start?next=/")
	if err != nil || res.StatusCode != 302 {
		t.Fatalf("start: %v %v", err, res.StatusCode)
	}
	if res, err = hc.Get(res.Header.Get("Location")); err != nil || res.StatusCode != 302 {
		t.Fatalf("authorize: %v %v", err, res.StatusCode)
	}
	if res, err = hc.Get(res.Header.Get("Location")); err != nil || res.StatusCode != 303 {
		t.Fatalf("callback: %v %v", err, res.StatusCode)
	}
	return res.Header.Get("Location")
}

func TestOIDCLogin(t *testing.T) {
	e := setup(t)
	idp := newFakeOIDC(t)
	if _, err := e.app.Auth.EnsureRole(t.Context(), "Editors", "", false); err != nil {
		t.Fatal(err)
	}
	e.setPluginSettings(t, "auth.oidc", map[string]any{"name": "Company SSO", "issuer": idp.srv.URL, "client_id": "wiki", "client_secret": "s3cret",
		"role_mapping": map[string]any{"wiki-editors": "Editors"}, "keep_unmapped_groups": false, "group_mapping": map[string]any{"sales": "Sales"}})
	c := e.client()
	hc := c.noRedirect()
	// 1. start -> provider
	res, err := hc.Get(e.srv.URL + "/api/v1/auth/sso/oidc/start?next=/c/1")
	if err != nil || res.StatusCode != 302 {
		t.Fatalf("start: %v %v", err, res.StatusCode)
	}
	// 2. provider -> callback
	res, err = hc.Get(res.Header.Get("Location"))
	if err != nil || res.StatusCode != 302 {
		t.Fatalf("authorize: %v %v", err, res.StatusCode)
	}
	cb := res.Header.Get("Location")
	// 3. callback -> session + redirect to next
	res, err = hc.Get(cb)
	if err != nil || res.StatusCode != 303 || res.Header.Get("Location") != "/c/1" {
		t.Fatalf("callback: %v %d %s", err, res.StatusCode, res.Header.Get("Location"))
	}
	var me meResp
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User == nil || me.User.Username != "carol" || me.User.Provider != "oidc" || me.User.Email != "carol@example.com" {
		t.Fatalf("oidc user %+v", me.User)
	}
	if g := groupNames(t, e, me.User.Groups); len(g) != 1 || g[0] != "Sales" {
		t.Fatalf("groups %v", g)
	}
	var roles int
	e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE r.name='Editors' AND ur.source='oidc'`).Scan(&roles)
	if roles != 1 {
		t.Fatal("role mapping not applied")
	}
	// replaying the callback must fail (state is single use)
	res, _ = e.client().noRedirect().Get(cb)
	if !strings.HasPrefix(res.Header.Get("Location"), "/login?error=") {
		t.Fatalf("state reuse: %s", res.Header.Get("Location"))
	}
	// open redirect protection
	res, _ = e.client().noRedirect().Get(e.srv.URL + "/api/v1/auth/sso/oidc/start?next=//evil.example")
	loc, _ := url.Parse(res.Header.Get("Location"))
	res, _ = e.client().noRedirect().Get(loc.String())
	res, _ = e.client().noRedirect().Get(res.Header.Get("Location"))
	if res.Header.Get("Location") != "/" {
		t.Fatalf("open redirect: %s", res.Header.Get("Location"))
	}
	var providers []map[string]string
	e.client().must("GET", "/api/v1/auth/providers", nil, &providers, 200)
	found := false
	for _, p := range providers {
		found = found || (p["id"] == "oidc" && p["kind"] == "redirect" && p["name"] == "Company SSO")
	}
	if !found {
		t.Fatalf("oidc not offered: %v", providers)
	}
	// secrets are masked in the settings API and kept when saving the mask
	admin := e.client()
	admin.login("admin", adminPW)
	var st struct {
		Config struct {
			Settings map[string]map[string]any `json:"plugin_settings"`
		} `json:"config"`
	}
	admin.must("GET", "/api/v1/admin/settings", nil, &st, 200)
	if st.Config.Settings["auth.oidc"]["client_secret"] != "********" {
		t.Fatalf("secret not masked: %v", st.Config.Settings["auth.oidc"]["client_secret"])
	}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": st.Config.Settings}, nil, 200)
	raw, _ := os.ReadFile(e.cfg)
	if !strings.Contains(string(raw), "client_secret: s3cret") {
		t.Fatal("masked secret overwrote the stored secret")
	}
	admin.must("GET", "/api/v1/x/auth.oidc/check", nil, nil, 200)
}

// ------------------------------------------------------------------ SAML

type idpSessions struct{}

func (idpSessions) GetSession(w http.ResponseWriter, r *http.Request, req *saml.IdpAuthnRequest) *saml.Session {
	return &saml.Session{ID: "s1", CreateTime: time.Now(), ExpireTime: time.Now().Add(time.Hour), Index: "1",
		NameID: "dave-persistent", UserName: "dave", UserEmail: "dave@example.org", UserCommonName: "Dave D", Groups: []string{"staff", "faculty"}}
}

type idpSPs struct{ md *saml.EntityDescriptor }

func (p idpSPs) GetServiceProvider(r *http.Request, id string) (*saml.EntityDescriptor, error) {
	if id != p.md.EntityID {
		return nil, os.ErrNotExist
	}
	return p.md, nil
}

// fakeSAML is a SAML IdP for tests; login signs c in through it.
type fakeSAML struct {
	idp  *saml.IdentityProvider
	srv  *httptest.Server
	spMD *saml.EntityDescriptor
}

func newFakeSAML(t *testing.T, e *env, settings map[string]any) *fakeSAML {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "idp"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(der)
	mux := http.NewServeMux()
	isrv := httptest.NewServer(mux)
	t.Cleanup(isrv.Close)
	md, _ := url.Parse(isrv.URL + "/metadata")
	sso, _ := url.Parse(isrv.URL + "/sso")
	slo, _ := url.Parse(isrv.URL + "/slo")
	idp := &saml.IdentityProvider{Key: key, Certificate: cert, MetadataURL: *md, SSOURL: *sso, LogoutURL: *slo, SessionProvider: idpSessions{}}
	idpXML, _ := xml.Marshal(idp.Metadata())
	s := map[string]any{"mode": "saml", "idp_metadata": string(idpXML)}
	for k, v := range settings {
		s[k] = v
	}
	e.setPluginSettings(t, "auth.shib", s)
	// the IdP knows our SP from its metadata endpoint
	res, err := http.Get(e.srv.URL + "/api/v1/x/auth.shib/metadata")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("sp metadata: %v %v", err, res.StatusCode)
	}
	spMD := &saml.EntityDescriptor{}
	b, _ := io.ReadAll(res.Body)
	if err := xml.Unmarshal(b, spMD); err != nil {
		t.Fatal(err)
	}
	idp.ServiceProviderProvider = idpSPs{spMD}
	mux.HandleFunc("/sso", idp.ServeSSO)
	return &fakeSAML{idp: idp, srv: isrv, spMD: spMD}
}

// login signs c in through the IdP and returns where the wiki sent it.
func (f *fakeSAML) login(t *testing.T, e *env, c *client, next string) string {
	t.Helper()
	hc := c.noRedirect()
	res, err := hc.Get(e.srv.URL + "/api/v1/auth/sso/shib/start?next=" + url.QueryEscape(next))
	if err != nil || res.StatusCode != 302 {
		t.Fatalf("start %v %d", err, res.StatusCode)
	}
	res, err = hc.Get(res.Header.Get("Location")) // IdP renders an auto-submit form
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("idp sso %v %d", err, res.StatusCode)
	}
	page, _ := io.ReadAll(res.Body)
	field := func(name string) string {
		m := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindSubmatch(page)
		if m == nil {
			t.Fatalf("field %s missing in IdP form", name)
		}
		return html.UnescapeString(string(m[1]))
	}
	res, err = hc.PostForm(e.srv.URL+"/api/v1/auth/sso/shib/callback", url.Values{"SAMLResponse": {field("SAMLResponse")}, "RelayState": {field("RelayState")}})
	if err != nil || res.StatusCode != 303 {
		t.Fatalf("acs %v %d", err, res.StatusCode)
	}
	return res.Header.Get("Location")
}

func TestSAMLLogin(t *testing.T) {
	e := setup(t)
	f := newFakeSAML(t, e, map[string]any{"attr_username": "uid",
		"attr_groups": "eduPersonAffiliation", "group_mapping": map[string]any{"faculty": "Faculty"}, "keep_unmapped_groups": false})
	c := e.client()
	if loc := f.login(t, e, c, "/search"); loc != "/search" {
		t.Fatalf("acs redirect %s", loc)
	}
	var me meResp
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User == nil || me.User.Username != "dave" || me.User.Provider != "shib" {
		t.Fatalf("saml user %+v", me.User)
	}
	if g := groupNames(t, e, me.User.Groups); len(g) != 1 || g[0] != "Faculty" {
		t.Fatalf("saml groups %v", g)
	}
}

func TestShibHeaderMode(t *testing.T) {
	e := setup(t)
	e.setPluginSettings(t, "auth.shib", map[string]any{"mode": "header", "header_username": "eppn", "header_groups": "isMemberOf"})
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/auth/sso/shib/start", nil)
	req.Header.Set("eppn", "erin@uni.example")
	req.Header.Set("isMemberOf", "students;library")
	hc := e.client().noRedirect()
	// not from a trusted proxy: headers are ignored
	res, _ := hc.Do(req)
	if !strings.HasPrefix(res.Header.Get("Location"), "/login?error=") {
		t.Fatalf("untrusted header login accepted: %s", res.Header.Get("Location"))
	}
	if err := e.app.Config.Update(func(c *config.Config) error { c.Server.TrustedProxies = []string{"127.0.0.1"}; return nil }); err != nil {
		t.Fatal(err)
	}
	c := e.client()
	req, _ = http.NewRequest("GET", e.srv.URL+"/api/v1/auth/sso/shib/start", nil)
	req.Header.Set("eppn", "erin@uni.example")
	req.Header.Set("isMemberOf", "students;library")
	res, _ = c.noRedirect().Do(req)
	if res.StatusCode != 303 {
		t.Fatalf("header login %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	var me meResp
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User == nil || me.User.Username != "erin@uni.example" || len(me.User.Groups) != 2 {
		t.Fatalf("header user %+v", me.User)
	}
}

// TestSAMLSPKeySealed: the private key of the SAML service provider is
// stored sealed with secrets.mode like every other secret, sign-in works
// with it, and rewrap brings an old plain key into the current mode.
func TestSAMLSPKeySealed(t *testing.T) {
	e := setupWith(t, func(c *config.Config) { c.Secrets.Mode = "pq" })
	ctx := t.Context()
	f := newFakeSAML(t, e, map[string]any{"attr_username": "uid"})
	var stored string
	if err := e.app.DB.QueryRow(ctx, `SELECT key_pem FROM plugin_shib_sp WHERE id=1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "ews1:q:") || strings.Contains(stored, "PRIVATE KEY") {
		t.Fatalf("SP key not sealed: %.40q", stored)
	}
	// bound to its row: copied elsewhere it does not open
	if _, err := e.app.Instance.Secrets.OpenString(stored, secrets.AAD("plugin_shib_sp", "2")); err == nil {
		t.Fatal("sealed SP key opens for another row")
	}
	c := e.client()
	if loc := f.login(t, e, c, "/"); loc != "/" {
		t.Fatalf("sign-in with the sealed key: %s", loc)
	}

	// a key stored in plain (mode plain before) still works and rewrap seals it
	plain, err := e.app.Instance.Secrets.OpenString(stored, secrets.AAD("plugin_shib_sp", "1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.DB.Exec(ctx, `UPDATE plugin_shib_sp SET key_pem=$1`, plain); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/admin/secrets/rewrap", nil, nil, 200)
	if err := e.app.DB.QueryRow(ctx, `SELECT key_pem FROM plugin_shib_sp WHERE id=1`).Scan(&stored); err != nil || !strings.HasPrefix(stored, "ews1:q:") {
		t.Fatalf("rewrap left the SP key plain: %.40q %v", stored, err)
	}
}
