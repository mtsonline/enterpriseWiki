package server_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
)

// confirmations captures the confirmation links instead of sending mails.
type confirmations struct {
	mu    sync.Mutex
	links map[string]string // address -> token
}

func (c *confirmations) install(e *env) {
	c.links = map[string]string{}
	e.app.Auth.MailReady = func() bool { return true }
	e.app.Auth.SendConfirmation = func(_ context.Context, _ int64, addr, link string) error {
		u, _ := url.Parse(link)
		c.mu.Lock()
		c.links[addr] = u.Query().Get("token")
		c.mu.Unlock()
		return nil
	}
}

func (c *confirmations) token(addr string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.links[addr]
}

type emailRow struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Verified bool   `json:"verified"`
	Primary  bool   `json:"primary"`
}

func TestEmails(t *testing.T) {
	e := setup(t)
	var conf confirmations
	conf.install(e)
	admin := e.client()
	admin.login("admin", adminPW)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "anna", "password": "Anna-Passw0rd!", "display_name": "Anna", "email": "Anna@Example.org"}, &u, 201)
	anna := e.client()
	anna.login("anna", "Anna-Passw0rd!")
	var list []emailRow
	anna.must("GET", "/api/v1/me/emails", nil, &list, 200)
	if len(list) != 1 || list[0].Email != "anna@example.org" || !list[0].Verified || !list[0].Primary {
		t.Fatalf("address given by the administrator: %+v", list)
	}
	// a second address: unconfirmed until the link is opened
	var added emailRow
	anna.must("POST", "/api/v1/me/emails", map[string]string{"email": "anna.private@example.net"}, &added, 201)
	if added.Verified || added.Primary || conf.token("anna.private@example.net") == "" {
		t.Fatalf("added %+v", added)
	}
	for _, bad := range []string{"no-at", "Anna <a@b.c>", "a@b.c\r\nBcc: x@y.z", strings.Repeat("a", 250) + "@b.cd"} {
		if got := anna.do("POST", "/api/v1/me/emails", map[string]string{"email": bad}, nil); got != 400 {
			t.Errorf("address %q: %d", bad, got)
		}
	}
	// not confirmed: cannot be the primary address, cannot sign in with it
	if got := anna.do("POST", "/api/v1/me/emails/"+strconv.FormatInt(added.ID, 10)+"/primary", nil, nil); got != 400 {
		t.Fatalf("unconfirmed primary: %d", got)
	}
	// somebody else confirms the same address first: anna's link fails then
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "eve", "password": "Eve-Passw0rd!x", "display_name": "Eve"}, nil, 201)
	eve := e.client()
	eve.login("eve", "Eve-Passw0rd!x")
	eve.must("POST", "/api/v1/me/emails", map[string]string{"email": "anna.private@example.net"}, nil, 201)
	// the token of the link is the secret: eve gets her own link, anna's link confirms for anna
	e.client().must("POST", "/api/v1/auth/verify-email", map[string]string{"token": "wrong"}, nil, 400)
	tok := conf.token("anna.private@example.net") // eve's (the latest)
	_ = tok
	// confirm anna's address with anna's link: request it again
	anna.must("POST", "/api/v1/me/emails/"+strconv.FormatInt(added.ID, 10)+"/resend", nil, nil, 204)
	annaTok := conf.token("anna.private@example.net")
	e.client().must("POST", "/api/v1/auth/verify-email", map[string]string{"token": annaTok}, nil, 200)
	if got := e.client().do("POST", "/api/v1/auth/verify-email", map[string]string{"token": annaTok}, nil); got != 400 {
		t.Fatalf("link used twice: %d", got)
	}
	// eve's (older) link no longer works: the address belongs to anna
	var eveList []emailRow
	eve.must("GET", "/api/v1/me/emails", nil, &eveList, 200)
	eve.must("POST", "/api/v1/me/emails/"+strconv.FormatInt(eveList[0].ID, 10)+"/resend", nil, nil, 204)
	if got := e.client().do("POST", "/api/v1/auth/verify-email", map[string]string{"token": conf.token("anna.private@example.net")}, nil); got != 409 {
		t.Fatalf("second owner of a confirmed address: %d", got)
	}
	// sign in with a confirmed address (local account)
	viaMail := e.client()
	viaMail.login("anna.private@example.net", "Anna-Passw0rd!")
	// primary address and removal
	anna.must("POST", "/api/v1/me/emails/"+strconv.FormatInt(added.ID, 10)+"/primary", nil, nil, 204)
	var me meResp
	anna.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User.Email != "anna.private@example.net" {
		t.Fatalf("primary address not mirrored: %s", me.User.Email)
	}
	if got := anna.do("DELETE", "/api/v1/me/emails/"+strconv.FormatInt(added.ID, 10), nil, nil); got != 400 {
		t.Fatalf("primary address removed: %d", got)
	}
	anna.must("GET", "/api/v1/me/emails", nil, &list, 200)
	for _, x := range list {
		if !x.Primary {
			anna.must("DELETE", "/api/v1/me/emails/"+strconv.FormatInt(x.ID, 10), nil, nil, 204)
		}
	}
	// nobody manages the addresses of others without the right
	if got := eve.do("GET", "/api/v1/admin/users/"+strconv.FormatInt(u.ID, 10)+"/emails", nil, nil); got != 403 {
		t.Fatalf("other user's addresses: %d", got)
	}
	admin.must("POST", "/api/v1/admin/users/"+strconv.FormatInt(u.ID, 10)+"/emails", map[string]any{"email": "anna.work@example.com", "verified": true}, nil, 201)
	admin.must("GET", "/api/v1/admin/users/"+strconv.FormatInt(u.ID, 10)+"/emails", nil, &list, 200)
	if len(list) != 2 {
		t.Fatalf("admin list %+v", list)
	}
}

// shibLogin signs in with the header based single sign-on (test proxy).
func shibLogin(t *testing.T, e *env, c *client, user, mail string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/auth/sso/shib/start", nil)
	req.Header.Set("eppn", user)
	req.Header.Set("mail", mail)
	res, err := c.noRedirect().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestIdentityLinking(t *testing.T) {
	e := setup(t)
	var conf confirmations
	conf.install(e)
	if err := e.app.Config.Update(func(c *config.Config) error { c.Server.TrustedProxies = []string{"127.0.0.1"}; return nil }); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bea", "password": "Bea-Passw0rd!x", "display_name": "Bea", "email": "bea@uni.example"}, nil, 201)

	// 1. untrusted provider: the sign-in waits for confirmation with the existing account
	e.setPluginSettings(t, "auth.shib", map[string]any{"mode": "header", "header_username": "eppn", "email_verified": true})
	c := e.client()
	res := shibLogin(t, e, c, "bea-sso", "bea@uni.example")
	loc := res.Header.Get("Location")
	if !strings.HasPrefix(loc, "/login?link=") {
		t.Fatalf("expected a link request, got %d %s", res.StatusCode, loc)
	}
	lu, _ := url.Parse(loc)
	token := lu.Query().Get("link")
	var info auth.LinkInfo
	e.client().must("POST", "/api/v1/auth/link/info", map[string]string{"token": token}, &info, 200)
	if info.Provider != "shib" || info.Email != "bea@uni.example" {
		t.Fatalf("link info %+v", info)
	}
	// another account cannot take the sign-in
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "mallory", "password": "Mallory-Passw0rd!", "display_name": "M"}, nil, 201)
	m := e.client()
	m.login("mallory", "Mallory-Passw0rd!")
	if got := m.do("POST", "/api/v1/me/identities/link", map[string]string{"token": token}, nil); got != 403 {
		t.Fatalf("foreign link: %d", got)
	}
	// (the failed attempt used the token up) - sign in again and link as bea
	res = shibLogin(t, e, c, "bea-sso", "bea@uni.example")
	lu, _ = url.Parse(res.Header.Get("Location"))
	bea := e.client()
	bea.login("bea", "Bea-Passw0rd!x")
	bea.must("POST", "/api/v1/me/identities/link", map[string]string{"token": lu.Query().Get("link")}, nil, 200)
	// now the sign-in leads to bea's account
	c2 := e.client()
	if res := shibLogin(t, e, c2, "bea-sso", "bea@uni.example"); res.StatusCode != 303 || strings.HasPrefix(res.Header.Get("Location"), "/login") {
		t.Fatalf("linked sign-in: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	var me meResp
	c2.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User == nil || me.User.Username != "bea" {
		t.Fatalf("linked sign-in is %+v", me.User)
	}
	var ids []auth.Identity
	bea.must("GET", "/api/v1/me/identities", nil, &ids, 200)
	if len(ids) != 2 || !ids[0].Primary || ids[1].Provider != "shib" {
		t.Fatalf("identities %+v", ids)
	}

	// 2. trusted provider with confirmed addresses: linked automatically
	e.setPluginSettings(t, "auth.shib", map[string]any{"mode": "header", "header_username": "eppn", "email_verified": true, "trust_email": true})
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "carl", "password": "Carl-Passw0rd!x", "display_name": "Carl", "email": "carl@uni.example"}, nil, 201)
	c3 := e.client()
	if res := shibLogin(t, e, c3, "carl-sso", "CARL@uni.example"); res.StatusCode != 303 || strings.HasPrefix(res.Header.Get("Location"), "/login") {
		t.Fatalf("trusted link: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	c3.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User.Username != "carl" {
		t.Fatalf("trusted sign-in went to %s", me.User.Username)
	}
	// 3. trusted, but the provider does not confirm the address: no automatic link
	e.setPluginSettings(t, "auth.shib", map[string]any{"mode": "header", "header_username": "eppn", "trust_email": true})
	if res := shibLogin(t, e, e.client(), "carl-other", "carl@uni.example"); !strings.HasPrefix(res.Header.Get("Location"), "/login?link=") {
		t.Fatalf("unconfirmed address linked: %s", res.Header.Get("Location"))
	}
	// 4. invitation: the first trusted sign-in takes over the invited account
	e.setPluginSettings(t, "auth.shib", map[string]any{"mode": "header", "header_username": "eppn", "email_verified": true, "trust_email": true, "jit": false})
	var staff struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Staff"}, &staff, 200)
	var inv struct {
		ID       int64  `json:"id"`
		Provider string `json:"provider"`
	}
	admin.must("POST", "/api/v1/admin/users/invite", map[string]any{"email": "dora@uni.example", "display_name": "Dora", "groups": []int64{staff.ID}}, &inv, 201)
	if inv.Provider != "invite" {
		t.Fatalf("invite %+v", inv)
	}
	if got := admin.do("POST", "/api/v1/admin/users/invite", map[string]any{"email": "dora@uni.example"}, nil); got != 409 {
		t.Fatalf("second invitation: %d", got)
	}
	c4 := e.client()
	if res := shibLogin(t, e, c4, "dora-sso", "dora@uni.example"); res.StatusCode != 303 || strings.HasPrefix(res.Header.Get("Location"), "/login") {
		t.Fatalf("invited sign-in: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	var me2 struct {
		User struct {
			ID       int64   `json:"id"`
			Provider string  `json:"provider"`
			Groups   []int64 `json:"groups"`
		} `json:"user"`
	}
	c4.must("GET", "/api/v1/auth/me", nil, &me2, 200)
	if me2.User.ID != inv.ID || me2.User.Provider != "shib" || len(me2.User.Groups) != 1 {
		t.Fatalf("invited account %+v", me2.User)
	}
	// 5. without JIT, strangers get no account
	if res := shibLogin(t, e, e.client(), "stranger", "stranger@else.example"); !strings.HasPrefix(res.Header.Get("Location"), "/login?error=") {
		t.Fatalf("stranger without JIT: %s", res.Header.Get("Location"))
	}
	var n int
	_ = e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM users WHERE subject='stranger'`).Scan(&n)
	if n != 0 {
		t.Fatal("account created without JIT")
	}
	// unlink: the further sign-in of bea
	bea.must("DELETE", "/api/v1/me/identities?provider=shib&subject=bea-sso", nil, nil, 204)
	if got := bea.do("DELETE", "/api/v1/me/identities?provider=local&subject=bea", nil, nil); got != 404 {
		t.Fatalf("the own sign-in removed: %d", got)
	}
}

func TestIdentityCLI(t *testing.T) {
	e := setup(t)
	if code, out := runCLI(t, e.cfg, "user", "invite", "ina@example.org", "--display-name", "Ina"); code != 0 || !strings.Contains(out, "invited") {
		t.Fatalf("invite: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "user", "email-add", "ina@example.org", "ina.work@example.org"); code != 0 {
		t.Fatalf("email-add: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "user", "email-primary", "ina@example.org", "ina.work@example.org"); code != 0 {
		t.Fatalf("email-primary: %d %s", code, out)
	}
	code, out := runCLI(t, e.cfg, "user", "emails", "ina.work@example.org")
	if code != 0 || !strings.Contains(out, "ina.work@example.org  true       true") {
		t.Fatalf("emails: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "user", "identities", "ina@example.org"); code != 0 || !strings.Contains(out, "invite") {
		t.Fatalf("identities: %d %s", code, out)
	}
}
