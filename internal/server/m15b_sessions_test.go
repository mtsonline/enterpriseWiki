package server_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
)

type sessionRow struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	Provider string `json:"provider"`
	Current  bool   `json:"current"`
}

func signedIn(c *client) bool {
	var me meResp
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	return me.User != nil
}

func TestSessions(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "anna", "password": "Anna-Passw0rd!", "display_name": "Anna"}, &u, 201)
	a, b := e.client(), e.client()
	a.login("anna", "Anna-Passw0rd!")
	b.login("anna", "Anna-Passw0rd!")

	// my sessions: both, the own one marked; the token is never shown
	var list []sessionRow
	b.must("GET", "/api/v1/me/sessions", nil, &list, 200)
	if len(list) != 2 || list[0].Current == list[1].Current {
		t.Fatalf("sessions %+v", list)
	}
	var other sessionRow
	for _, s := range list {
		if !s.Current {
			other = s
		}
	}
	// end the other one: a is signed out at once (no cached principal)
	b.must("DELETE", "/api/v1/me/sessions/"+strconv.FormatInt(other.ID, 10), nil, nil, 204)
	if signedIn(a) || !signedIn(b) {
		t.Fatal("ending a session")
	}
	// sessions of somebody else cannot be ended through the own list
	var adminList []sessionRow
	admin.must("GET", "/api/v1/me/sessions", nil, &adminList, 200)
	if got := b.do("DELETE", "/api/v1/me/sessions/"+strconv.FormatInt(adminList[0].ID, 10), nil, nil); got != 404 || !signedIn(admin) {
		t.Fatalf("foreign session: %d", got)
	}
	// API keys cannot see or end sessions
	var key struct {
		Token string `json:"token"`
	}
	b.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "k", "permissions": 15, "expires_at": time.Now().Add(time.Hour)}, &key, 201)
	kc := e.client()
	kc.token = key.Token
	if got := kc.do("GET", "/api/v1/me/sessions", nil, nil); got != 403 {
		t.Fatalf("api key lists sessions: %d", got)
	}
	if got := kc.do("DELETE", "/api/v1/me/sessions", nil, nil); got != 403 || !signedIn(b) {
		t.Fatalf("api key ends sessions: %d", got)
	}

	// sign out everywhere else
	a.login("anna", "Anna-Passw0rd!")
	b.must("DELETE", "/api/v1/me/sessions", nil, nil, 204)
	if signedIn(a) || !signedIn(b) {
		t.Fatal("sign out everywhere else")
	}
	// own password change: the other sessions end, this one stays
	a.login("anna", "Anna-Passw0rd!")
	b.must("PUT", "/api/v1/me/password", map[string]string{"current": "Anna-Passw0rd!", "new": "Anna-Passw0rd!2"}, nil, 204)
	if signedIn(a) || !signedIn(b) {
		t.Fatal("password change")
	}

	// administrators: list and end the sessions of a user; others may not
	uid := strconv.FormatInt(u.ID, 10)
	admin.must("GET", "/api/v1/admin/users/"+uid+"/sessions", nil, &list, 200)
	if len(list) != 1 || list[0].Current {
		t.Fatalf("admin view %+v", list)
	}
	if got := b.do("GET", "/api/v1/admin/users/1/sessions", nil, nil); got != 403 {
		t.Fatalf("non-admin: %d", got)
	}
	admin.must("DELETE", "/api/v1/admin/users/"+uid+"/sessions", nil, nil, 204)
	if signedIn(b) || !signedIn(admin) {
		t.Fatal("admin ends all sessions")
	}
	if got := admin.do("DELETE", "/api/v1/admin/users/"+uid+"/sessions/999999", nil, nil); got != 404 {
		t.Fatalf("unknown session: %d", got)
	}
}

func TestSessionIdle(t *testing.T) {
	e := setup(t)
	c := e.client()
	c.login("admin", adminPW)
	ctx := t.Context()
	// background requests (polling) do not keep the session alive
	if _, err := e.app.DB.Exec(ctx, `UPDATE sessions SET last_seen_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	e.app.Bus.Publish(ctx, "auth", "")
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/me/inbox?limit=20", nil)
	req.Header.Set("X-Ewiki-Background", "1")
	res, err := c.http.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("background request: %v %d", err, res.StatusCode)
	}
	res.Body.Close()
	var idle float64
	if err := e.app.DB.QueryRow(ctx, `SELECT extract(epoch FROM now() - last_seen_at) FROM sessions`).Scan(&idle); err != nil || idle < 3500 {
		t.Fatalf("background request counted as activity: %v %v", err, idle)
	}
	// longer idle than session_idle (12h): the session has ended
	if _, err := e.app.DB.Exec(ctx, `UPDATE sessions SET last_seen_at = now() - interval '13 hours'`); err != nil {
		t.Fatal(err)
	}
	e.app.Bus.Publish(ctx, "auth", "")
	if signedIn(c) {
		t.Fatal("idle session still valid")
	}
}

func TestSingleLogoutOIDC(t *testing.T) {
	e := setup(t)
	idp := newFakeOIDC(t)
	e.setPluginSettings(t, "auth.oidc", map[string]any{"issuer": idp.srv.URL, "client_id": "wiki", "client_secret": "s3cret"})
	a, b := e.client(), e.client()
	idp.login(t, e, a)
	idp.login(t, e, b)
	var list []sessionRow
	a.must("GET", "/api/v1/me/sessions", nil, &list, 200)
	if len(list) != 2 || list[0].Provider != "oidc" {
		t.Fatalf("sessions %+v", list)
	}

	// RP-initiated logout: the browser continues at the end_session_endpoint
	var out struct {
		Redirect string `json:"redirect"`
	}
	a.must("POST", "/api/v1/auth/logout", nil, &out, 200)
	u, err := url.Parse(out.Redirect)
	if err != nil || !strings.HasPrefix(out.Redirect, idp.srv.URL+"/logout?") {
		t.Fatalf("logout redirect %q", out.Redirect)
	}
	q := u.Query()
	if q.Get("tenant") != "1" || q.Get("client_id") != "wiki" || q.Get("id_token_hint") == "" || q.Get("post_logout_redirect_uri") != e.srv.URL+"/login?logged_out=1" {
		t.Fatalf("logout parameters %v", q)
	}
	if signedIn(a) {
		t.Fatal("still signed in")
	}
	// single_logout off: only the wiki session ends
	e.setPluginSettings(t, "auth.oidc", map[string]any{"issuer": idp.srv.URL, "client_id": "wiki", "client_secret": "s3cret", "single_logout": false})
	c := e.client()
	idp.login(t, e, c)
	if got := c.do("POST", "/api/v1/auth/logout", nil, nil); got != 204 {
		t.Fatalf("logout without single logout: %d", got)
	}

	// back-channel logout ends exactly the session the provider names
	idp.login(t, e, a)
	var sidA, sidB string
	rows, _ := e.app.DB.Query(t.Context(), `SELECT sso_session FROM sessions WHERE sso_provider='oidc' ORDER BY id`)
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		if sidB == "" {
			sidB = s
		} else {
			sidA = s
		}
	}
	rows.Close()
	if sidA == "" || sidB == "" || sidA == sidB {
		t.Fatalf("sids %q %q", sidA, sidB)
	}
	now := time.Now()
	claims := func(mod func(map[string]any)) map[string]any {
		c := map[string]any{"iss": idp.srv.URL, "aud": "wiki", "sub": "u-123", "sid": sidA, "iat": now.Unix(), "exp": now.Add(2 * time.Minute).Unix(),
			"jti": "jti-" + strconv.FormatInt(now.UnixNano(), 10), "events": map[string]any{"http://schemas.openid.net/event/backchannel-logout": map[string]any{}}}
		if mod != nil {
			mod(c)
		}
		return c
	}
	post := func(token string) int {
		res, err := http.PostForm(e.srv.URL+"/api/v1/auth/sso/oidc/logout", url.Values{"logout_token": {token}})
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	bad := map[string]string{
		"foreign key":  idp.sign(claims(nil), other, "logout+jwt"),
		"no event":     idp.sign(claims(func(c map[string]any) { delete(c, "events") }), nil, "logout+jwt"),
		"nonce":        idp.sign(claims(func(c map[string]any) { c["nonce"] = "n" }), nil, "logout+jwt"),
		"other client": idp.sign(claims(func(c map[string]any) { c["aud"] = "other" }), nil, "logout+jwt"),
		"old":          idp.sign(claims(func(c map[string]any) { c["iat"] = now.Add(-time.Hour).Unix() }), nil, "logout+jwt"),
		"no jti":       idp.sign(claims(func(c map[string]any) { delete(c, "jti") }), nil, "logout+jwt"),
		"no sub/sid":   idp.sign(claims(func(c map[string]any) { delete(c, "sub"); delete(c, "sid") }), nil, "logout+jwt"),
		"garbage":      "x.y.z",
	}
	for name, tok := range bad {
		if got := post(tok); got != 400 {
			t.Errorf("%s: %d", name, got)
		}
	}
	if !signedIn(a) || !signedIn(b) {
		t.Fatal("an invalid logout token ended a session")
	}
	good := idp.sign(claims(nil), nil, "logout+jwt")
	if got := post(good); got != 200 {
		t.Fatalf("back-channel logout: %d", got)
	}
	if signedIn(a) || !signedIn(b) {
		t.Fatal("back-channel logout ended the wrong sessions")
	}
	if got := post(good); got != 400 {
		t.Fatalf("replayed logout token: %d", got)
	}
	// sub only: all sessions of the user at this provider
	if got := post(idp.sign(claims(func(c map[string]any) { delete(c, "sid"); c["jti"] = "j2" }), nil, "logout+jwt")); got != 200 || signedIn(b) {
		t.Fatalf("logout by sub: %d", got)
	}
	// unknown provider
	res, _ := http.PostForm(e.srv.URL+"/api/v1/auth/sso/nope/logout", url.Values{"logout_token": {good}})
	if res.StatusCode != 404 {
		t.Fatalf("unknown provider: %d", res.StatusCode)
	}
}

func TestSingleLogoutSAML(t *testing.T) {
	e := setup(t)
	f := newFakeSAML(t, e, map[string]any{"attr_username": "uid"})
	a, b := e.client(), e.client()
	f.login(t, e, a, "/")
	f.login(t, e, b, "/")

	// SP-initiated: a signed LogoutRequest to the SLO endpoint of the IdP
	var out struct {
		Redirect string `json:"redirect"`
	}
	a.must("POST", "/api/v1/auth/logout", nil, &out, 200)
	u, err := url.Parse(out.Redirect)
	if err != nil || !strings.HasPrefix(out.Redirect, f.srv.URL+"/slo?") || u.Query().Get("SAMLRequest") == "" || u.Query().Get("Signature") == "" {
		t.Fatalf("logout redirect %q", out.Redirect)
	}
	if signedIn(a) || !signedIn(b) {
		t.Fatal("SP-initiated logout")
	}

	// IdP-initiated: LogoutRequest signed by the IdP (HTTP-POST binding)
	slo := e.srv.URL + "/api/v1/auth/sso/shib/logout"
	idpAsSender := &saml.ServiceProvider{EntityID: f.idp.Metadata().EntityID, Key: f.idp.Key.(*rsa.PrivateKey), Certificate: f.idp.Certificate,
		IDPMetadata: f.spMD, SignatureMethod: "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"}
	request := func(signer *saml.ServiceProvider, nameID, index string) string {
		req, err := signer.MakeLogoutRequest(slo, nameID, saml.WithSessionIndex(index))
		if err != nil {
			t.Fatal(err)
		}
		doc := etree.NewDocument()
		doc.SetRoot(req.Element())
		var buf bytes.Buffer
		if _, err := doc.WriteTo(&buf); err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(buf.Bytes())
	}
	post := func(c *client, msg string) *http.Response {
		res, err := c.noRedirect().PostForm(slo, url.Values{"SAMLRequest": {msg}, "RelayState": {"r1"}})
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	// forged: signed by somebody else
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	forger := &saml.ServiceProvider{EntityID: idpAsSender.EntityID, Key: key, Certificate: f.idp.Certificate, IDPMetadata: f.spMD, SignatureMethod: idpAsSender.SignatureMethod}
	if res := post(b, request(forger, "dave-persistent", "1")); res.StatusCode != 400 || !signedIn(b) {
		t.Fatalf("forged LogoutRequest: %d", res.StatusCode)
	}
	// unsigned
	unsigned := &saml.ServiceProvider{EntityID: idpAsSender.EntityID, IDPMetadata: f.spMD}
	if res := post(b, request(unsigned, "dave-persistent", "1")); res.StatusCode != 400 || !signedIn(b) {
		t.Fatalf("unsigned LogoutRequest: %d", res.StatusCode)
	}
	// another NameID: nothing ends
	if res := post(b, request(idpAsSender, "somebody-else", "1")); res.StatusCode != 302 || !signedIn(b) {
		t.Fatalf("other NameID: %d", res.StatusCode)
	}
	msg := request(idpAsSender, "dave-persistent", "1")
	res := post(b, msg)
	loc, _ := url.Parse(res.Header.Get("Location"))
	if res.StatusCode != 302 || !strings.HasPrefix(loc.String(), f.srv.URL+"/slo?") || loc.Query().Get("SAMLResponse") == "" || loc.Query().Get("RelayState") != "r1" {
		t.Fatalf("LogoutResponse %d %s", res.StatusCode, loc)
	}
	if signedIn(b) {
		t.Fatal("IdP-initiated logout did not end the session")
	}
	// replay is refused
	f.login(t, e, b, "/")
	if res := post(b, msg); res.StatusCode != 400 || !signedIn(b) {
		t.Fatalf("replayed LogoutRequest: %d", res.StatusCode)
	}
	// the LogoutResponse to our own request leads to the login page
	res, err = b.noRedirect().Get(slo + "?SAMLResponse=bogus")
	if err != nil || res.StatusCode != 303 || res.Header.Get("Location") != e.srv.URL+"/login?logged_out=1" {
		t.Fatalf("logout response: %v %d %s", err, res.StatusCode, res.Header.Get("Location"))
	}
}
