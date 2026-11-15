package server_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/cli"
)

// runCLIAll runs the CLI and returns standard output and errors together.
func runCLIAll(t *testing.T, cfg string, args ...string) (int, string) {
	t.Helper()
	t.Setenv("EWIKI_CONFIG", cfg)
	var out bytes.Buffer
	code := cli.MainIO(args, strings.NewReader(""), &out, &out)
	return code, out.String()
}

// TestCLIFromRegister: actions without a hand-written command are commands
// of the CLI, built from the action register and run through the API.
func TestCLIFromRegister(t *testing.T) {
	e := setup(t)
	installReset(e)
	// listed in the help
	code, out := runCLIAll(t, e.cfg, "help")
	if code != 0 || !strings.Contains(out, "auth password-reset request") || strings.Contains(out, "  plugin sources add ") {
		t.Fatalf("help: %d\n%s", code, out)
	}
	// help of one command
	code, out = runCLIAll(t, e.cfg, "auth", "password-reset", "request", "--help")
	if code != 0 || !strings.Contains(out, "POST /api/v1/auth/password-reset") || !strings.Contains(out, "--email VALUE") {
		t.Fatalf("command help: %d\n%s", code, out)
	}
	if code, out = runCLIAll(t, e.cfg, "help", "smime", "show"); code != 0 || !strings.Contains(out, "usage: ewiki smime show ID") {
		t.Fatalf("help smime show: %d\n%s", code, out)
	}
	// runs through the API: same answer as in the browser (the CLI's own
	// process has no mail, so the action is not offered: 404)
	if code, out = runCLIAll(t, e.cfg, "auth", "password-reset", "request", "--email", "x@example.com"); code == 0 || !strings.Contains(out, "404") {
		t.Fatalf("request: %d\n%s", code, out)
	}
	// a path parameter is required; unknown options are refused
	if code, out = runCLIAll(t, e.cfg, "smime", "show"); code == 0 || !strings.Contains(out, "ID is missing") {
		t.Fatalf("missing id: %d\n%s", code, out)
	}
	if code, out = runCLIAll(t, e.cfg, "smime", "show", "1", "--colour", "red"); code == 0 || !strings.Contains(out, "unknown option --colour") {
		t.Fatalf("unknown option: %d\n%s", code, out)
	}
	// the API answers: a certificate that does not exist
	if code, out = runCLIAll(t, e.cfg, "smime", "show", "999999"); code == 0 || !strings.Contains(out, "404") {
		t.Fatalf("missing certificate: %d\n%s", code, out)
	}
	// typed body options: a value that is no number is refused before the call
	if code, out = runCLIAll(t, e.cfg, "auth", "registration", "request", "--display_name", "A", "--email", "a@example.com", "--password", "x", "--language", "de", "--json", "{}"); code == 0 || !strings.Contains(out, "either with --json or") {
		t.Fatalf("body twice: %d\n%s", code, out)
	}
	// hand-written commands keep working and are not shadowed
	if code, out = runCLIAll(t, e.cfg, "plugin", "sources", "list"); code != 0 || !strings.Contains(out, "sources") {
		t.Fatalf("hand-written command: %d\n%s", code, out)
	}
	// unknown words are still unknown
	if code, _ = runCLIAll(t, e.cfg, "no-such-command"); code != 2 {
		t.Fatalf("unknown command: %d", code)
	}
}
