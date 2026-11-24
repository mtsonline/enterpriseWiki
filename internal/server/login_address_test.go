package server_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/auth"
)

// dirProvider is a password provider that knows people only by their user
// name in the directory, like LDAP with (uid={username}).
type dirProvider struct {
	id    string
	users map[string]string // user name -> password
	calls atomic.Int32
}

func (d *dirProvider) ID() string   { return d.id }
func (d *dirProvider) Name() string { return "Directory " + d.id }
func (d *dirProvider) Authenticate(_ context.Context, username, password string) (*auth.ExternalIdentity, error) {
	d.calls.Add(1)
	pw, ok := d.users[username]
	if !ok {
		return nil, auth.ErrUnknownUser
	}
	if password != pw {
		return nil, auth.ErrBadCredentials
	}
	return &auth.ExternalIdentity{Provider: d.id, Subject: "s-" + username, Username: username,
		DisplayName: username, Email: username + "@" + d.id + ".example.com"}, nil
}

// TestLoginWithAnyAddress: a person signs in with any confirmed address of
// her account, also when the directory does not know that address (decision
// of 15.11.2026). The account id is the reference; the address only finds
// the account and its own sign-ins.
func TestLoginWithAnyAddress(t *testing.T) {
	e := setup(t)
	var conf confirmations
	conf.install(e)
	dir := &dirProvider{id: "dirx", users: map[string]string{"anna.dir": "Dir-Passw0rd!a", "otto.dir": "Dir-Passw0rd!o"}}
	// owned by an always active core plugin, so that it counts as enabled
	e.app.Auth.RegisterProvider("core.html", dir)

	anna := e.client()
	anna.login("anna.dir", "Dir-Passw0rd!a")
	annaID := userID(t, anna)
	var added emailRow
	anna.must("POST", "/api/v1/me/emails", map[string]string{"email": "anna.home@example.net"}, &added, 201)
	e.client().must("POST", "/api/v1/auth/verify-email", map[string]string{"token": conf.token("anna.home@example.net")}, nil, 200)

	// the directory knows only "anna.dir"; the wiki finds the account by the
	// address and asks the directory with her name there
	for _, typed := range []string{"anna.home@example.net", "Anna.Home@Example.NET"} {
		c := e.client()
		c.login(typed, "Dir-Passw0rd!a")
		if id := userID(t, c); id != annaID {
			t.Fatalf("%s signed in as %d, want %d", typed, id, annaID)
		}
	}
	// wrong password: refused, and the directory was asked once only
	before := dir.calls.Load()
	if got := e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "anna.home@example.net", "password": "wrong-Passw0rd!"}, nil); got != 401 {
		t.Fatalf("wrong password: %d", got)
	}
	if n := dir.calls.Load() - before; n != 1 {
		t.Fatalf("directory asked %d times for one wrong password", n)
	}
	// another person's password never opens anna's account through her address
	otto := e.client()
	otto.login("otto.dir", "Dir-Passw0rd!o")
	if got := e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "anna.home@example.net", "password": "Dir-Passw0rd!o"}, nil); got != 401 {
		t.Fatalf("otto's password with anna's address: %d", got)
	}
	// an unconfirmed address does not sign in
	anna.must("POST", "/api/v1/me/emails", map[string]string{"email": "anna.new@example.net"}, nil, 201)
	if got := e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "anna.new@example.net", "password": "Dir-Passw0rd!a"}, nil); got != 401 {
		t.Fatalf("unconfirmed address: %d", got)
	}
	// a disabled account stays closed on this way too
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("PUT", "/api/v1/admin/users/"+itoa(annaID), map[string]any{"disabled": true}, nil, 200)
	if got := e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "anna.home@example.net", "password": "Dir-Passw0rd!a"}, nil); got != 401 {
		t.Fatalf("disabled account: %d", got)
	}
}

func userID(t *testing.T, c *client) int64 {
	t.Helper()
	var me struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User.ID == 0 {
		t.Fatal("no user id in /auth/me")
	}
	return me.User.ID
}
