package server_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/limits"
)

type resetMail struct{ addr, token, provider string }

// installReset captures the mails of "Forgot your password?" (they go out
// in the background) on a channel.
func installReset(e *env) chan resetMail {
	ch := make(chan resetMail, 32)
	e.app.Auth.MailReady = func() bool { return true }
	e.app.Auth.SendPasswordReset = func(_ context.Context, addr, _, _, link, provider string) error {
		tok := ""
		if link != "" {
			u, _ := url.Parse(link)
			if u.Path != "/password-reset/confirm" {
				panic("link " + link)
			}
			tok = u.Query().Get("token")
		}
		ch <- resetMail{addr, tok, provider}
		return nil
	}
	return ch
}

func nextMail(t *testing.T, ch chan resetMail) resetMail {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(10 * time.Second):
		t.Fatal("no mail")
		return resetMail{}
	}
}

func noMail(t *testing.T, e *env, ch chan resetMail) {
	t.Helper()
	e.app.Auth.WaitBackground()
	select {
	case m := <-ch:
		t.Fatalf("unexpected mail %+v", m)
	default:
	}
}

// TestPasswordReset: "Forgot your password?" for local accounts. Same answer
// for every address; link once, newest only, one hour; all sessions end;
// lockout lifted; further factors stay; accounts without a wiki password get
// a note instead of a link; switched off by the administrators: gone.
func TestPasswordReset(t *testing.T) {
	e := setupWith(t, func(c *config.Config) {
		if c.Limits == nil {
			c.Limits = map[string]int64{}
		}
		c.Limits[limits.ResetPerHour] = 1000
	})
	ch := installReset(e)
	admin := e.client()
	admin.login("admin", adminPW)
	const oldPW, newPW = "Alte-Passwort-2026!", "Neues-Passwort-2026!"
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rita", "password": oldPW, "display_name": "Rita", "email": "rita@example.com"}, nil, 201)
	ask := func(addr string) (int, map[string]any) {
		return e.client().rawJSON("POST", "/api/v1/auth/password-reset", map[string]any{"email": addr})
	}
	confirm := func(tok, pw string) int {
		return e.client().do("POST", "/api/v1/auth/password-reset/confirm", map[string]any{"token": tok, "password": pw}, nil)
	}

	// on by default (with e-mail)
	var site struct {
		Login struct {
			PasswordReset bool `json:"password_reset"`
		} `json:"login"`
	}
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if !site.Login.PasswordReset {
		t.Fatal("not offered by default")
	}

	// unknown address: same answer, no mail
	if st, out := ask("nobody@example.com"); st != 202 || out["status"] != "check_mail" {
		t.Fatalf("unknown: %d %v", st, out)
	}
	if st, _ := ask("not an address"); st != 400 {
		t.Fatalf("bad address: %d", st)
	}

	// a session that knew the old password, and a lockout
	old := e.client()
	old.login("rita", oldPW)
	if _, err := e.app.DB.Exec(t.Context(), `UPDATE users SET failed_logins=9, locked_until=now()+interval '1 hour' WHERE username='rita'`); err != nil {
		t.Fatal(err)
	}

	if st, _ := ask("Rita@Example.com"); st != 202 {
		t.Fatalf("known: %d", st)
	}
	first := nextMail(t, ch)
	if first.addr != "rita@example.com" || first.token == "" {
		t.Fatalf("mail %+v", first)
	}
	noMail(t, e, ch) // the unknown address got nothing
	ask("rita@example.com")
	second := nextMail(t, ch)
	if confirm(first.token, newPW) != 400 {
		t.Fatal("older link still valid")
	}
	if confirm(second.token, "short") != 400 {
		t.Fatal("weak password accepted")
	}
	// the weak attempt did not use up the link
	if got := confirm(second.token, newPW); got != 200 {
		t.Fatalf("reset: %d", got)
	}
	if confirm(second.token, newPW) != 400 {
		t.Fatal("link used twice")
	}
	if got := old.do("GET", "/api/v1/me/sessions", nil, nil); got != 401 {
		t.Fatalf("old session still valid: %d", got)
	}
	if got := e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "rita", "password": oldPW}, nil); got == 200 {
		t.Fatal("old password still works")
	}
	e.client().login("rita@example.com", newPW) // lockout lifted
	var n int
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM audit_log WHERE action='user.password_reset'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit %d %v", n, err)
	}

	// expired link
	ask("rita@example.com")
	late := nextMail(t, ch)
	if _, err := e.app.DB.Exec(t.Context(), `UPDATE password_resets SET expires_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if confirm(late.token, newPW+"x") != 400 {
		t.Fatal("expired link accepted")
	}

	// an account without a wiki password: a note, no link
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "lars", "password": oldPW, "display_name": "Lars", "email": "lars@example.com"}, nil, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "dora", "password": oldPW, "display_name": "Dora", "email": "dora@example.com"}, nil, 201)
	if _, err := e.app.DB.Exec(t.Context(), `UPDATE users SET provider='ldap', password_hash=NULL WHERE username='lars';
		UPDATE users SET disabled=true WHERE username='dora'`); err != nil {
		t.Fatal(err)
	}
	ask("lars@example.com")
	if m := nextMail(t, ch); m.token != "" || m.provider != "ldap" {
		t.Fatalf("external account got %+v", m)
	}
	// a disabled account gets nothing
	ask("dora@example.com")
	noMail(t, e, ch)

	// at most 3 links to one address per hour
	for range 2 {
		ask("flood@example.com")
	}
	if st, _ := ask("flood@example.com"); st != 202 {
		t.Fatalf("third: %d", st)
	}
	if st, _ := ask("flood@example.com"); st != 429 {
		t.Fatalf("fourth: %d", st)
	}

	// switched off by the administrators
	admin.must("PUT", "/api/v1/admin/auth/login", map[string]any{"password_reset": false}, nil, 200)
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if site.Login.PasswordReset {
		t.Fatal("still offered")
	}
	if st, _ := ask("rita@example.com"); st != 404 {
		t.Fatalf("while off: %d", st)
	}
	if confirm("x", newPW) != 404 {
		t.Fatal("confirm while off")
	}
	// CLI over the API: switch it on again, other settings stay
	if code, out := runCLI(t, e.cfg, "login-page", "set", "--password-reset", "on"); code != 0 {
		t.Fatalf("cli: %d %s", code, out)
	}
	if code, _ := runCLI(t, e.cfg, "login-page", "set", "--password-reset", "maybe"); code == 0 {
		t.Fatal("cli accepted a wrong value")
	}
}
