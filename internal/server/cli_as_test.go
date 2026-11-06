package server_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/cli"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// runCLI runs the ewiki CLI in-process and returns exit code and stdout.
func runCLI(t *testing.T, cfg string, args ...string) (int, string) {
	t.Helper()
	t.Setenv("EWIKI_CONFIG", cfg)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	code := cli.Main(args)
	os.Stdout = old
	_ = w.Close()
	return code, <-done
}

// TestCLIAsNeverEscalates: "--as USER" must carry exactly that user's
// rights, never those of the CLI superuser it runs under.
func TestCLIAsNeverEscalates(t *testing.T) {
	e := setup(t)
	ctx := identity.With(context.Background(), identity.CLI())
	if _, err := e.app.Auth.CreateLocalUserUnchecked(ctx, auth.NewUser{Username: "bob", Password: "Bob-Passw0rd!x", DisplayName: "Bob"}); err != nil {
		t.Fatal(err)
	}
	// Superuser: admin endpoints work.
	if code, _ := runCLI(t, e.cfg, "api", "GET", "/api/v1/admin/users"); code != 0 {
		t.Fatalf("cli admin call: exit %d", code)
	}
	// As a plain user: admin endpoints are refused.
	for _, p := range []string{"/api/v1/admin/users", "/api/v1/admin/settings", "/api/v1/admin/permissions", "/api/v1/instance/sites"} {
		if code, out := runCLI(t, e.cfg, "api", "GET", p, "--as", "bob"); code == 0 {
			t.Errorf("--as bob GET %s succeeded: %s", p, out)
		}
	}
	// Shell approval is a superuser privilege of the CLI, not of --as (not
	// even --as an administrator who is not a listed approver).
	if err := e.app.Config.Update(func(c *config.Config) error { c.Plugins.Macros.ShellEnabled = true; return nil }); err != nil {
		t.Fatal(err)
	}
	code, out := runCLI(t, e.cfg, "api", "GET", "/api/v1/x/macro.shell/commands", "--as", "admin")
	if code != 0 {
		t.Fatalf("--as admin shell commands: exit %d", code)
	}
	{
		var v struct {
			CanApprove bool `json:"can_approve"`
		}
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatal(err)
		}
		if v.CanApprove {
			t.Fatal("--as admin may approve shell commands without being an approver")
		}
	}
	// --as admin works like the admin (not more: still a normal handle).
	code, out = runCLI(t, e.cfg, "api", "GET", "/api/v1/auth/me", "--as", "admin")
	if code != 0 || !strings.Contains(out, `"via": "cli-as"`) {
		t.Fatalf("--as admin /me: exit %d %s", code, out)
	}
}

func TestPrincipalCLIHelpers(t *testing.T) {
	if !identity.CLI().IsCLI() {
		t.Fatal("CLI() is not IsCLI")
	}
	as := &identity.Principal{UserID: 5, Username: "bob", Provider: "local", Via: "cli-as"}
	if as.IsCLI() || !as.InProcess() || as.Handle() != "bob@local" {
		t.Fatalf("cli-as principal: IsCLI=%v InProcess=%v Handle=%s", as.IsCLI(), as.InProcess(), as.Handle())
	}
	// A real user carrying Via "cli" (should never happen) is not the CLI either.
	if (&identity.Principal{UserID: 1, Via: "cli"}).IsCLI() {
		t.Fatal("user with Via cli treated as CLI superuser")
	}
	var guest *identity.Principal
	if guest.IsCLI() || guest.InProcess() {
		t.Fatal("guest treated as CLI")
	}
}
