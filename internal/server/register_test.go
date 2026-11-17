package server_test

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/limits"
)

// registrationMails captures the mails of registrations instead of sending.
type registrationMails struct {
	mu   sync.Mutex
	sent map[string][]string // address -> tokens ("" = "you already have an account")
}

func (m *registrationMails) install(e *env) {
	m.sent = map[string][]string{}
	e.app.Auth.MailReady = func() bool { return true }
	e.app.Auth.SendRegistration = func(_ context.Context, addr, _, _, link string) error {
		tok := ""
		if link != "" {
			u, _ := url.Parse(link)
			if u.Path != "/register/confirm" {
				panic("link " + link)
			}
			tok = u.Query().Get("token")
		}
		m.mu.Lock()
		m.sent[addr] = append(m.sent[addr], tok)
		m.mu.Unlock()
		return nil
	}
}

func (m *registrationMails) last(addr string) (string, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.sent[addr]
	if len(l) == 0 {
		return "none", 0
	}
	return l[len(l)-1], len(l)
}

// TestRegistration: local accounts people create themselves. Off by
// default; the account exists only after the confirmation link; the answer
// never tells whether an address has an account; domains, groups (never
// administrator groups), limits, newest link only, expiry, user names.
func TestRegistration(t *testing.T) {
	e := setupWith(t, func(c *config.Config) {
		if c.Limits == nil {
			c.Limits = map[string]int64{}
		}
		c.Limits[limits.RegisterPerHour] = 1000
	})
	var mails registrationMails
	mails.install(e)
	admin := e.client()
	admin.login("admin", adminPW)
	reg := func(email, name, pw string) (int, map[string]any) {
		return e.client().rawJSON("POST", "/api/v1/auth/register", map[string]any{"email": email, "display_name": name, "password": pw, "language": "de"})
	}
	const pw = "Neue-Person-2026!"

	// off by default
	if st, _ := reg("x@example.com", "X", pw); st != 404 {
		t.Fatalf("registration while off: %d", st)
	}
	var site siteLogin
	var siteReg struct {
		Login struct {
			Registration bool `json:"registration"`
		} `json:"login"`
	}
	e.client().must("GET", "/api/v1/site", nil, &siteReg, 200)
	if siteReg.Login.Registration {
		t.Fatal("registration offered while off")
	}
	_ = site

	// settings: groups must exist and never carry an administrator role
	var staff, admins struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Staff"}, &staff, 201)
	var roles []struct {
		ID      int64 `json:"id"`
		IsAdmin bool  `json:"is_admin"`
	}
	admin.must("GET", "/api/v1/roles", nil, &roles, 200)
	var adminRole int64
	for _, r := range roles {
		if r.IsAdmin {
			adminRole = r.ID
		}
	}
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Admins", "roles": []int64{adminRole}}, &admins, 201)
	for name, body := range map[string]map[string]any{
		"admin group":   {"enabled": true, "groups": []int64{admins.ID}},
		"missing group": {"enabled": true, "groups": []int64{999999}},
		"bad domain":    {"enabled": true, "domains": []string{"exa mple.com"}},
		"no dot":        {"enabled": true, "domains": []string{"localhost"}},
	} {
		if st, out := admin.rawJSON("PUT", "/api/v1/admin/auth/login", map[string]any{"registration": body}); st != 400 {
			t.Errorf("%s: %d %v", name, st, out)
		}
	}
	admin.must("PUT", "/api/v1/admin/auth/login", map[string]any{"registration": map[string]any{
		"enabled": true, "domains": []string{" Example.com ", "*.example.org", "example.com"}, "groups": []int64{staff.ID, staff.ID}}}, nil, 200)
	if c := e.app.Config.Get().Login.Registration; len(c.Domains) != 2 || c.Domains[0] != "example.com" || len(c.Groups) != 1 {
		t.Fatalf("stored %+v", c)
	}
	e.client().must("GET", "/api/v1/site", nil, &siteReg, 200)
	if !siteReg.Login.Registration {
		t.Fatal("registration not offered")
	}

	// checks of the input
	for name, in := range map[string][3]string{
		"other domain":    {"eve@example.net", "Eve", pw},
		"parent domain":   {"eve@org", "Eve", pw},
		"no name":         {"eve@example.com", " ", pw},
		"weak password":   {"eve@example.com", "Eve", "short"},
		"control in name": {"eve@example.com", "E\u0000ve", pw},
		"bad address":     {"eve example.com", "Eve", pw},
	} {
		if st, out := reg(in[0], in[1], in[2]); st != 400 {
			t.Errorf("%s: %d %v", name, st, out)
		}
	}
	if tok, n := mails.last("eve@example.com"); n != 0 {
		t.Fatalf("mail despite errors: %q", tok)
	}

	// a registration: the account exists only after the link
	if st, out := reg("Anna.Neu@Example.com", "Anna Neu", pw); st != 202 || out["status"] != "check_mail" {
		t.Fatalf("register: %d %v", st, out)
	}
	tok, _ := mails.last("anna.neu@example.com")
	if tok == "" || tok == "none" {
		t.Fatal("no link sent")
	}
	var n int
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM user_emails WHERE email='anna.neu@example.com'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("account before the link: %d %v", n, err)
	}
	if got := e.client().do("POST", "/api/v1/auth/register/confirm", map[string]string{"token": "wrong"}, nil); got != 400 {
		t.Fatalf("wrong token: %d", got)
	}
	var created struct{ Username, Email string }
	e.client().must("POST", "/api/v1/auth/register/confirm", map[string]string{"token": tok}, &created, 200)
	if created.Username != "anna.neu" || created.Email != "anna.neu@example.com" {
		t.Fatalf("created %+v", created)
	}
	if got := e.client().do("POST", "/api/v1/auth/register/confirm", map[string]string{"token": tok}, nil); got != 400 {
		t.Fatalf("link used twice: %d", got)
	}
	// signs in with the address, joined the group, nothing more
	anna := e.client()
	anna.login("anna.neu@example.com", pw)
	var me meResp
	anna.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User == nil || me.User.Provider != "local" || len(me.User.Groups) != 1 || me.User.Groups[0] != staff.ID {
		t.Fatalf("new account %+v", me.User)
	}
	if got := anna.do("GET", "/api/v1/admin/users", nil, nil); got != 403 {
		t.Fatalf("new account is admin: %d", got)
	}
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM audit_log WHERE action='user.register'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit %d %v", n, err)
	}

	// an address with an account: the same answer, the mail says so
	if st, _ := reg("anna.neu@example.com", "Mallory", pw); st != 202 {
		t.Fatalf("known address: %d", st)
	}
	if tok, n := mails.last("anna.neu@example.com"); tok != "" || n != 2 {
		t.Fatalf("known address got a link: %q %d", tok, n)
	}

	// subdomains, user names taken, only the newest link, expiry
	if st, _ := reg("anna.neu@team.example.org", "Anna Zwei", pw); st != 202 {
		t.Fatalf("subdomain: %d", st)
	}
	first, _ := mails.last("anna.neu@team.example.org")
	reg("anna.neu@team.example.org", "Anna Zwei", pw)
	second, _ := mails.last("anna.neu@team.example.org")
	if got := e.client().do("POST", "/api/v1/auth/register/confirm", map[string]string{"token": first}, nil); got != 400 {
		t.Fatalf("older link still valid: %d", got)
	}
	e.client().must("POST", "/api/v1/auth/register/confirm", map[string]string{"token": second}, &created, 200)
	if created.Username != "anna.neu2" {
		t.Fatalf("user name %q", created.Username)
	}
	reg("late@example.com", "Late", pw)
	late, _ := mails.last("late@example.com")
	if _, err := e.app.DB.Exec(t.Context(), `UPDATE registrations_pending SET expires_at = now() - interval '1 minute' WHERE email='late@example.com'`); err != nil {
		t.Fatal(err)
	}
	if got := e.client().do("POST", "/api/v1/auth/register/confirm", map[string]string{"token": late}, nil); got != 400 {
		t.Fatalf("expired link: %d", got)
	}

	// at most 3 links to one address per hour
	for i := range 3 {
		if st, _ := reg("flood@example.com", "Flood", pw); st != 202 {
			t.Fatalf("registration %d: %d", i, st)
		}
	}
	if st, _ := reg("flood@example.com", "Flood", pw); st != 429 {
		t.Fatalf("fourth registration of an address: %d", st)
	}

	// an administrator group written into config.yaml directly is still never joined
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Login.Registration.Groups = []int64{admins.ID, staff.ID}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reg("sneaky@example.com", "Sneaky", pw)
	sneaky, _ := mails.last("sneaky@example.com")
	e.client().must("POST", "/api/v1/auth/register/confirm", map[string]string{"token": sneaky}, nil, 200)
	sc := e.client()
	sc.login("sneaky@example.com", pw)
	sc.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User.IsAdmin || len(me.User.Groups) != 1 || me.User.Groups[0] != staff.ID {
		t.Fatalf("joined an administrator group: %+v", me.User)
	}

	// switched off: the link does not work any more
	reg("off@example.com", "Off", pw)
	off, _ := mails.last("off@example.com")
	admin.must("PUT", "/api/v1/admin/auth/login", map[string]any{"registration": map[string]any{"enabled": false, "domains": []string{"example.com"}}}, nil, 200)
	if got := e.client().do("POST", "/api/v1/auth/register/confirm", map[string]string{"token": off}, nil); got != 404 {
		t.Fatalf("link after switching off: %d", got)
	}

	// CLI over the API: changing one setting keeps the others
	if code, out := runCLI(t, e.cfg, "login-page", "set", "--registration", "on", "--registration-groups", "none"); code != 0 {
		t.Fatalf("cli: %d %s", code, out)
	}
	code, out := runCLI(t, e.cfg, "login-page", "show")
	if code != 0 || !strings.Contains(out, `"enabled": true`) || !strings.Contains(out, `"example.com"`) {
		t.Fatalf("cli show: %d %s", code, out)
	}
	if code, _ := runCLI(t, e.cfg, "login-page", "set", "--registration-groups", "x"); code == 0 {
		t.Fatal("cli accepted a wrong group")
	}
}

// TestRegistrationNeedsMail: without e-mail there is no registration.
func TestRegistrationNeedsMail(t *testing.T) {
	e := setup(t)
	var mails registrationMails
	mails.install(e)
	e.app.Auth.MailReady = func() bool { return false }
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("PUT", "/api/v1/admin/auth/login", map[string]any{"registration": map[string]any{"enabled": true}}, nil, 200)
	var lp struct {
		MailReady bool `json:"mail_ready"`
		Here      bool `json:"registration_here"`
	}
	admin.must("GET", "/api/v1/admin/auth/login", nil, &lp, 200)
	if lp.MailReady || !lp.Here {
		t.Fatalf("admin view %+v", lp)
	}
	if st, _ := e.client().rawJSON("POST", "/api/v1/auth/register", map[string]any{"email": "a@example.com", "display_name": "A", "password": "Neue-Person-2026!"}); st != 404 {
		t.Fatalf("registration without mail: %d", st)
	}
}
