package server_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/cli"
)

// runCLIIn runs the CLI with stdin and returns exit code, stdout and stderr.
func runCLIIn(t *testing.T, cfg, stdin string, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("EWIKI_CONFIG", cfg)
	var out, errOut bytes.Buffer
	code := cli.MainIO(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// TestCLIGoesThroughTheAPI (phase 3): the hand-written commands that used
// to call the services directly now run through the API. They keep their
// output, get the checks of the API and leave the same audit entries.
func TestCLIGoesThroughTheAPI(t *testing.T) {
	e := setup(t)
	ok := func(stdin string, args ...string) string {
		t.Helper()
		code, out, errOut := runCLIIn(t, e.cfg, stdin, args...)
		if code != 0 {
			t.Fatalf("ewiki %s: exit %d\n%s%s", strings.Join(args, " "), code, out, errOut)
		}
		return out
	}
	failsIn := func(stdin, want string, args ...string) {
		t.Helper()
		code, out, errOut := runCLIIn(t, e.cfg, stdin, args...)
		if code == 0 || !strings.Contains(out+errOut, want) {
			t.Fatalf("ewiki %s: exit %d, want an error with %q\n%s%s", strings.Join(args, " "), code, want, out, errOut)
		}
	}
	fails := func(want string, args ...string) { t.Helper(); failsIn("", want, args...) }
	audited := func(action string) {
		t.Helper()
		var n int
		if err := e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action=$1 AND via='cli'`, action).Scan(&n); err != nil || n == 0 {
			t.Fatalf("no audit entry %s from the command line (%d, %v)", action, n, err)
		}
	}

	// users
	if out := ok("Una-Passw0rd!x\n", "user", "create", "una", "--password-stdin", "--display-name", "Una"); !strings.Contains(out, "created una@local") {
		t.Fatalf("create: %s", out)
	}
	audited("user.create")
	if out := ok("", "user", "list", "una"); !strings.Contains(out, "una@local") || !strings.Contains(out, "Una") {
		t.Fatalf("list: %s", out)
	}
	failsIn("short\n", "HTTP 400", "user", "create", "weak", "--password-stdin") // the API checks the password
	ok("", "user", "alias", "una", "una.u")
	ok("", "user", "disable", "una.u")
	ok("", "user", "enable", "una")
	ok("", "user", "unlock", "una")
	if out := ok("New-Passw0rd!xy\n", "user", "passwd", "una", "--password-stdin"); !strings.Contains(out, "password changed") {
		t.Fatalf("passwd: %s", out)
	}
	if out := ok("", "user", "sessions", "una"); !strings.Contains(out, "LAST SEEN") {
		t.Fatalf("sessions: %s", out)
	}
	if out := ok("", "user", "logout", "una"); !strings.Contains(out, "signed out") {
		t.Fatalf("logout: %s", out)
	}
	fails("not found", "user", "disable", "nobody")
	// the API refuses to disable the last administrator (a check of the service, kept)
	fails("", "user", "disable", "admin")

	// SCIM clients: the API checks the provider
	fails("unknown sign-in provider", "scim", "create", "Directory", "--provider", "local")
	idp := newFakeOIDC(t)
	e.setPluginSettings(t, "auth.oidc", map[string]any{"issuer": idp.srv.URL, "client_id": "wiki", "client_secret": "s3cret"})
	out := ok("", "scim", "create", "Directory", "--provider", "oidc")
	if !strings.Contains(out, "token (shown once): ") || !strings.Contains(out, "/scim/v2") {
		t.Fatalf("scim create: %s", out)
	}
	if out := ok("", "scim", "list"); !strings.Contains(out, "Directory") {
		t.Fatalf("scim list: %s", out)
	}

	// mail, jobs, cache, retention, signature keys
	if out := ok("", "mail", "status"); !strings.Contains(out, "{") {
		t.Fatalf("mail status: %s", out)
	}
	ok("", "mail", "list")
	if out := ok("", "jobs", "list"); !strings.Contains(out, "mail.bounces") {
		t.Fatalf("jobs list: %s", out)
	}
	ok("", "jobs", "run", "search.changes.cleanup")
	fails("", "jobs", "run", "no.such.job")
	ok("", "cache", "status")
	ok("", "cache", "clear")
	if out := ok("", "retention", "run"); !strings.Contains(out, "revisions") {
		t.Fatalf("retention: %s", out)
	}
	ok("", "signature", "keys")

	// core plugins
	if out := ok("", "plugin", "list"); !strings.Contains(out, "core.comments") {
		t.Fatalf("plugin list: %s", out)
	}
	ok("", "plugin", "disable", "core.comments")
	audited("plugin.disable")
	ok("", "plugin", "enable", "core.comments")
	fails("", "plugin", "remove", "no.such.plugin")

	// config: an approved exception that records itself
	ok("", "config", "set", "retention.trash_days", "45")
	audited("settings.config_set")
}
