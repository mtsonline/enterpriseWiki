package server_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/plugins/mfa_totp"
)

type mfaStep struct {
	Kind     string   `json:"kind"`
	Factor   int      `json:"factor"`
	Methods  []string `json:"methods"`
	Passed   int      `json:"passed"`
	Required int      `json:"required"`
}

type mfaAnswer struct {
	Done bool     `json:"done"`
	Step *mfaStep `json:"step"`
	Next string   `json:"next"`
}

func (c *client) mfaInfo(tok string) mfaStep {
	c.e.t.Helper()
	var st mfaStep
	c.must("POST", "/api/v1/auth/mfa/info", map[string]string{"token": tok}, &st, 200)
	return st
}

// totpNow returns the code of secret for the current step plus offset.
func totpNow(secret string, offset int64) string {
	code, _ := mfa_totp.TOTPCode(secret, time.Now().Unix()/30+offset)
	return code
}

// reuseTOTP lets the test use codes again (a code works once per 30 s step).
func reuseTOTP(t *testing.T, e *env) {
	t.Helper()
	if _, err := e.app.DB.Exec(t.Context(), `UPDATE plugin_mfa_totp SET last_step = 0`); err != nil {
		t.Fatal(err)
	}
}

// enrollTOTP sets up the authenticator app for c in the profile.
func enrollTOTP(t *testing.T, c *client) string {
	t.Helper()
	var setup struct {
		Secret string `json:"secret"`
		URI    string `json:"uri"`
	}
	c.must("POST", "/api/v1/me/mfa/totp/begin", map[string]any{}, &setup, 200)
	if !strings.HasPrefix(setup.URI, "otpauth://totp/") {
		t.Fatalf("uri %q", setup.URI)
	}
	if got := c.do("POST", "/api/v1/me/mfa/totp/finish", map[string]string{"code": "000000"}, nil); got != 401 {
		t.Fatalf("wrong setup code: %d", got)
	}
	// a wrong answer ends the setup: start again
	c.must("POST", "/api/v1/me/mfa/totp/begin", map[string]any{}, &setup, 200)
	c.must("POST", "/api/v1/me/mfa/totp/finish", map[string]string{"code": totpNow(setup.Secret, 0)}, nil, 204)
	return setup.Secret
}

func setChain(t *testing.T, admin *client, provider string, methods []string, required int) {
	t.Helper()
	upd := map[string]any{"required": required}
	if methods == nil {
		upd["all"] = true
	} else {
		upd["methods"] = methods
	}
	admin.must("PUT", "/api/v1/admin/mfa", map[string]any{"providers": map[string]any{provider: upd}}, nil, 200)
}

func TestMFAChain(t *testing.T) {
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

	// the authenticator app: a sign-in needs its code afterwards
	secret := enrollTOTP(t, bob)
	c := e.client()
	tok := c.pendingMFA("bob", "Bob-Passw0rd-1!")
	if st := c.mfaInfo(tok); st.Kind != "verify" || st.Factor != 2 || fmt.Sprint(st.Methods) != "[totp]" || st.Passed != 1 {
		t.Fatalf("step %+v", st)
	}
	if signedIn(c) {
		t.Fatal("signed in after the password alone")
	}
	c.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "totp", "code": "000000"}, nil, 401)
	c.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "email", "code": "1"}, nil, 400) // not asked now
	var ans mfaAnswer
	c.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "totp", "code": totpNow(secret, 1)}, &ans, 200)
	if !ans.Done || !signedIn(c) {
		t.Fatalf("after the code: %+v", ans)
	}
	// the same code does not work twice; a pending sign-in is used once
	c.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "totp", "code": totpNow(secret, 1)}, nil, 401)
	d := e.client()
	tok = d.pendingMFA("bob", "Bob-Passw0rd-1!")
	d.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "totp", "code": totpNow(secret, 1)}, nil, 401)
	// five wrong answers end the pending sign-in
	for range 4 {
		d.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "totp", "code": "999999"}, nil, 401)
	}
	d.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "totp", "code": totpNow(secret, 0)}, nil, 401)
	if signedIn(d) {
		t.Fatal("signed in after too many attempts")
	}

	// codes by e-mail as an alternative at the same factor
	var sent struct {
		Address string `json:"address"`
	}
	bob.must("POST", "/api/v1/me/mfa/email/begin", map[string]any{}, &sent, 200)
	if sent.Address != "b***@example.com" {
		t.Fatalf("masked address %q", sent.Address)
	}
	bob.must("POST", "/api/v1/me/mfa/email/finish", map[string]string{"code": cp.lastCode(t, "bob@example.com")}, nil, 204)
	f := e.client()
	tok = f.pendingMFA("bob", "Bob-Passw0rd-1!")
	if st := f.mfaInfo(tok); !slices.Equal(st.Methods, []string{"totp", "email"}) && !slices.Equal(st.Methods, []string{"email", "totp"}) {
		t.Fatalf("alternatives %+v", st)
	}
	f.must("POST", "/api/v1/auth/mfa/challenge", map[string]string{"token": tok, "method": "email"}, &sent, 200)
	f.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "email", "code": cp.lastCode(t, "bob@example.com")}, &ans, 200)
	if !ans.Done {
		t.Fatalf("e-mail code %+v", ans)
	}

	// a chain: the e-mail code as factor 3 after the app
	admin.must("PUT", "/api/v1/admin/mfa", map[string]any{"factors": map[string]int{"mfa.email": 3}}, nil, 200)
	g := e.client()
	reuseTOTP(t, e)
	tok = g.pendingMFA("bob", "Bob-Passw0rd-1!")
	g.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "totp", "code": totpNow(secret, 0)}, &ans, 200)
	if ans.Done || ans.Step == nil || ans.Step.Factor != 3 || fmt.Sprint(ans.Step.Methods) != "[email]" || signedIn(g) {
		t.Fatalf("after factor 2: %+v", ans)
	}
	g.must("POST", "/api/v1/auth/mfa/challenge", map[string]string{"token": tok, "method": "email"}, nil, 200)
	g.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "email", "code": cp.lastCode(t, "bob@example.com")}, &ans, 200)
	if !ans.Done || !signedIn(g) {
		t.Fatalf("after factor 3: %+v", ans)
	}

	// per provider: local accounts only use the e-mail code
	setChain(t, admin, "local", []string{"email"}, 1)
	var mine struct {
		Methods []string `json:"methods"`
		Applies bool     `json:"applies"`
	}
	bob.must("GET", "/api/v1/me/mfa", nil, &mine, 200)
	h := e.client()
	tok = h.pendingMFA("bob", "Bob-Passw0rd-1!")
	if st := h.mfaInfo(tok); fmt.Sprint(st.Methods) != "[email]" || st.Factor != 3 {
		t.Fatalf("chain of local accounts %+v", st)
	}
	// no factors for local accounts: the password alone
	setChain(t, admin, "local", []string{}, 1)
	e.client().login("bob", "Bob-Passw0rd-1!")
	if got := bob.do("POST", "/api/v1/me/mfa/totp/begin", map[string]any{}, nil); got != 400 {
		t.Fatalf("setup of a factor not used: %d", got)
	}

	// required factors: someone without factors sets one up while signing in
	setChain(t, admin, "local", nil, 2)
	admin.must("PUT", "/api/v1/admin/mfa", map[string]any{"factors": map[string]int{"mfa.email": 2}}, nil, 200)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "nora", "password": "Nora-Passw0rd-1!"}, nil, 201)
	n := e.client()
	tok = n.pendingMFA("nora", "Nora-Passw0rd-1!")
	st := n.mfaInfo(tok)
	if st.Kind != "enroll" || !slices.Contains(st.Methods, "totp") || st.Required != 2 {
		t.Fatalf("enroll step %+v", st)
	}
	n.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "totp", "code": "123456"}, nil, 400)
	var setup struct {
		Secret string `json:"secret"`
	}
	n.must("POST", "/api/v1/auth/mfa/enroll/begin", map[string]string{"token": tok, "method": "totp"}, &setup, 200)
	n.must("POST", "/api/v1/auth/mfa/enroll/finish", map[string]string{"token": tok, "method": "totp", "code": totpNow(setup.Secret, 0)}, &ans, 200)
	if !ans.Done || !signedIn(n) {
		t.Fatalf("after setting up during the sign-in: %+v", ans)
	}

	// configuration checks
	if got := admin.do("PUT", "/api/v1/admin/mfa", map[string]any{"factors": map[string]int{"mfa.totp": 1}}, nil); got != 400 {
		t.Fatalf("totp as factor 1: %d", got)
	}
	if got := admin.do("PUT", "/api/v1/admin/mfa", map[string]any{"providers": map[string]any{"local": map[string]any{"methods": []string{"totp"}, "required": 3}}}, nil); got != 400 {
		t.Fatalf("more factors than the chain has: %d", got)
	}
	if got := bob.do("GET", "/api/v1/admin/mfa", nil, nil); got != 403 {
		t.Fatalf("chain by a user: %d", got)
	}

	// administrators reset all factors
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), map[string]any{"reset_mfa": true}, nil, 200)
	setChain(t, admin, "local", nil, 1)
	e.client().login("bob", "Bob-Passw0rd-1!")
}

func TestMFANetwork(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "nils", "password": "Nils-Passw0rd-1!"}, &u, 201)
	nils := e.client()
	nils.login("nils", "Nils-Passw0rd-1!")
	// the own networks must include the current address
	nils.must("PUT", "/api/v1/x/mfa.network/me", map[string]any{"networks": []string{"10.0.0.0/8"}}, nil, 400)
	nils.must("PUT", "/api/v1/x/mfa.network/me", map[string]any{"networks": []string{"bad"}}, nil, 400)
	var nets struct {
		Networks []string `json:"networks"`
	}
	nils.must("PUT", "/api/v1/x/mfa.network/me", map[string]any{"networks": []string{"127.0.0.0/8", "10.1.2.3", "2001:db8::1/32"}}, &nets, 200)
	if fmt.Sprint(nets.Networks) != "[127.0.0.0/8 10.1.2.3/32 2001:db8::/32]" {
		t.Fatalf("networks: %v", nets.Networks)
	}
	// inside the networks the factor passes without any action
	e.client().login("nils", "Nils-Passw0rd-1!")

	// an administrator restricts nils to another network: sessions stop working (enforce_always)
	admin.must("PUT", fmt.Sprintf("/api/v1/x/mfa.network/users/%d", u.ID), map[string]any{"networks": []string{"10.0.0.0/8"}}, nil, 200)
	time.Sleep(10 * time.Millisecond)
	e.app.Bus.Publish(t.Context(), "auth", "")
	if got := nils.do("GET", "/api/v1/auth/me", nil, nil); got != 403 {
		t.Fatalf("session outside the networks: %d", got)
	}
	if got := e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "nils", "password": "Nils-Passw0rd-1!"}, nil); got != 403 {
		t.Fatalf("sign-in outside the networks: %d", got)
	}
	// only a factor: outside the networks the authenticator app is the alternative
	e.setPluginSettings(t, "mfa.network", map[string]any{"enforce_always": false})
	admin.must("PUT", fmt.Sprintf("/api/v1/x/mfa.network/users/%d", u.ID), map[string]any{"networks": []string{}}, nil, 200)
	n2 := e.client()
	n2.login("nils", "Nils-Passw0rd-1!")
	secret := enrollTOTP(t, n2)
	admin.must("PUT", fmt.Sprintf("/api/v1/x/mfa.network/users/%d", u.ID), map[string]any{"networks": []string{"10.0.0.0/8"}}, nil, 200)
	c := e.client()
	tok := c.pendingMFA("nils", "Nils-Passw0rd-1!")
	if st := c.mfaInfo(tok); fmt.Sprint(st.Methods) != "[totp]" {
		t.Fatalf("alternative outside the networks %+v", st)
	}
	var ans mfaAnswer
	c.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "totp", "code": totpNow(secret, 1)}, &ans, 200)
	if !ans.Done {
		t.Fatalf("%+v", ans)
	}
	// inside the networks no code is needed
	admin.must("PUT", fmt.Sprintf("/api/v1/x/mfa.network/users/%d", u.ID), map[string]any{"networks": []string{"127.0.0.1"}}, nil, 200)
	e.client().login("nils", "Nils-Passw0rd-1!")
	// as its own factor (no alternative) outside the networks: refused
	admin.must("PUT", "/api/v1/admin/mfa", map[string]any{"factors": map[string]int{"mfa.network": 3}}, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/x/mfa.network/users/%d", u.ID), map[string]any{"networks": []string{"10.0.0.0/8"}}, nil, 200)
	g := e.client()
	reuseTOTP(t, e)
	tok = g.pendingMFA("nils", "Nils-Passw0rd-1!")
	if got := g.do("POST", "/api/v1/auth/mfa/verify", map[string]string{"token": tok, "method": "totp", "code": totpNow(secret, 0)}, nil); got != 403 || signedIn(g) {
		t.Fatalf("network factor outside: %d", got)
	}
	// users cannot set others' networks
	if got := nils.do("PUT", fmt.Sprintf("/api/v1/x/mfa.network/users/%d", 1), map[string]any{"networks": []string{}}, nil); got != 403 && got != 401 {
		t.Fatalf("user sets networks of another: %d", got)
	}
}

// TestMFAAfterSSO: the factors of the wiki also after single sign-on,
// unless the provider's chain has none.
func TestMFAAfterSSO(t *testing.T) {
	e := setup(t)
	idp := newFakeOIDC(t)
	e.setPluginSettings(t, "auth.oidc", map[string]any{"issuer": idp.srv.URL, "client_id": "wiki", "client_secret": "s3cret"})
	c := e.client()
	idp.login(t, e, c)
	secret := enrollTOTP(t, c)
	var st struct {
		Applies bool     `json:"applies"`
		Methods []string `json:"methods"`
	}
	c.must("GET", "/api/v1/me/mfa", nil, &st, 200)
	if !st.Applies || fmt.Sprint(st.Methods) != "[totp]" {
		t.Fatalf("mfa status %+v", st)
	}
	// the next single sign-on waits for the code (token in a cookie)
	d := e.client()
	if loc := idp.loginAt(t, e, d, "oidc"); loc != "/login?mfa=1" {
		t.Fatalf("after SSO: %s", loc)
	}
	if signedIn(d) {
		t.Fatal("signed in without the factor")
	}
	var info mfaStep
	d.must("POST", "/api/v1/auth/mfa/info", map[string]string{}, &info, 200)
	if fmt.Sprint(info.Methods) != "[totp]" {
		t.Fatalf("step %+v", info)
	}
	var ans mfaAnswer
	d.must("POST", "/api/v1/auth/mfa/verify", map[string]string{"method": "totp", "code": totpNow(secret, 1)}, &ans, 200)
	if !ans.Done || !signedIn(d) || ans.Next != "/" {
		t.Fatalf("after the factor: %+v", ans)
	}
	if got := e.client().do("POST", "/api/v1/auth/mfa/info", map[string]string{}, nil); got != 401 {
		t.Fatalf("pending sign-in without the cookie: %d", got)
	}
	// the provider does the factors: no factors in its chain
	admin := e.client()
	admin.login("admin", adminPW)
	setChain(t, admin, "oidc", []string{}, 1)
	f := e.client()
	if loc := idp.loginAt(t, e, f, "oidc"); loc != "/" || !signedIn(f) {
		t.Fatalf("no factors: %s", loc)
	}
	f.must("GET", "/api/v1/me/mfa", nil, &st, 200)
	if st.Applies {
		t.Fatal("factors offered although the provider has none")
	}
}

// TestMFASecurity: who may change the chain and the own factors.
func TestMFASecurity(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "eve", "password": "Eve-Passw0rd-1!"}, nil, 201)
	eve := e.client()
	eve.login("eve", "Eve-Passw0rd-1!")

	// only administrators see or change the chain
	eve.must("GET", "/api/v1/admin/mfa", nil, nil, 403)
	eve.must("PUT", "/api/v1/admin/mfa", map[string]any{"providers": map[string]any{"local": map[string]any{"all": true, "required": 1}}}, nil, 403)
	// factor 1 only for factors without password; unknown factors refused
	admin.must("PUT", "/api/v1/admin/mfa", map[string]any{"factors": map[string]int{"mfa.totp": 1}}, nil, 400)
	admin.must("PUT", "/api/v1/admin/mfa", map[string]any{"factors": map[string]int{"mfa.totp": 10}}, nil, 400)
	admin.must("PUT", "/api/v1/admin/mfa", map[string]any{"providers": map[string]any{"local": map[string]any{"methods": []string{"nope"}, "required": 1}}}, nil, 400)

	// a factor outside the own chain cannot be set up
	setChain(t, admin, "local", []string{"email"}, 1)
	eve.must("POST", "/api/v1/me/mfa/totp/begin", map[string]any{}, nil, 400)
	setChain(t, admin, "local", nil, 1)

	// API keys cannot change factors or networks
	var key struct {
		Token string `json:"token"`
	}
	eve.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "k", "permissions": 15, "expires_at": time.Now().Add(time.Hour)}, &key, 201)
	kc := e.client()
	kc.token = key.Token
	kc.must("POST", "/api/v1/me/mfa/totp/begin", map[string]any{}, nil, 403)
	kc.must("PUT", "/api/v1/x/mfa.network/me", map[string]any{"networks": []string{"10.0.0.0/8"}}, nil, 403)
	kc.must("DELETE", "/api/v1/x/mfa.totp/me", nil, nil, 403)

	// a pending sign-in only sets a factor up when the sign-in needs it
	secret := enrollTOTP(t, eve)
	c := e.client()
	tok := c.pendingMFA("eve", "Eve-Passw0rd-1!")
	c.must("POST", "/api/v1/auth/mfa/enroll/begin", map[string]any{"token": tok, "method": "email"}, nil, 400)
	// an unknown or used token does nothing
	c.must("POST", "/api/v1/auth/mfa/verify", map[string]any{"token": "x" + tok, "method": "totp", "code": totpNow(secret, 0)}, nil, 401)
	reuseTOTP(t, e)
	c.must("POST", "/api/v1/auth/mfa/verify", map[string]any{"token": tok, "method": "totp", "code": totpNow(secret, 0)}, nil, 200)
	c.must("POST", "/api/v1/auth/mfa/enroll/begin", map[string]any{"token": tok, "method": "email"}, nil, 401)
}
