//go:build unix

package shellcmd

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/identity"
)

func TestRun(t *testing.T) {
	out, code, err := Run(context.Background(), "echo hi; echo err >&2; exit 3", nil, 5*time.Second, "")
	if err != nil || code != 3 || out != "hi\nerr\n" {
		t.Fatalf("%q %d %v", out, code, err)
	}
	start := time.Now()
	if _, _, err := Run(context.Background(), "sleep 10 & sleep 10", nil, 300*time.Millisecond, ""); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("timeout: %v", err)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("process group not killed")
	}
	out, _, _ = Run(context.Background(), "head -c 100000 /dev/zero | tr '\\0' a", nil, 5*time.Second, "")
	if !strings.HasSuffix(out, "[… output truncated]") || len(out) > MaxOutput+40 {
		t.Fatalf("not truncated: %d", len(out))
	}
	out, _, _ = Run(context.Background(), "env", nil, 5*time.Second, "")
	if strings.Contains(out, "EWIKI") || strings.Contains(out, os.Getenv("HOME")+"\n") && os.Getenv("HOME") != "" && strings.Contains(out, "HOME="+os.Getenv("HOME")+"\n") {
		t.Fatalf("environment leaks: %s", out)
	}
	if os.Geteuid() == 0 {
		out, _, err = Run(context.Background(), "id -u", nil, 5*time.Second, "")
		if err == nil && strings.TrimSpace(out) == "0" {
			t.Fatal("command ran as root")
		}
	}
}

func TestApprovers(t *testing.T) {
	admin := &identity.Principal{UserID: 1, Username: "admin", Provider: "local", IsAdmin: true, Via: "session"}
	if CanApprove(admin, nil) || !CanApprove(admin, []string{"admin@local"}) {
		t.Fatal("approver list ignored")
	}
	key := &identity.Principal{UserID: 1, Username: "admin", Provider: "local", IsAdmin: true, Via: "apikey", KeyAllowAdmin: true}
	if CanApprove(key, []string{"admin@local"}) {
		t.Fatal("API keys must not approve shell commands")
	}
	if !CanApprove(identity.CLI(), nil) || CanApprove(nil, []string{"guest"}) {
		t.Fatal("cli/guest")
	}
	if Hash(Normalize("  echo x\r\n")) != Hash("echo x") {
		t.Fatal("normalize")
	}
}

// TestEnv: values reach the command only as environment variables with
// safe names; the command text stays the same.
func TestEnv(t *testing.T) {
	if EnvName("first name-2") != "EWIKI_FIELD_FIRST_NAME_2" || EnvName("Größe") != "EWIKI_FIELD_GR__E" {
		t.Fatalf("names: %s %s", EnvName("first name-2"), EnvName("Größe"))
	}
	out, code, err := Run(context.Background(), `printf '%s' "$EWIKI_FIELD_NAME"`, []string{"EWIKI_FIELD_NAME=a'; id; echo '"}, 5*time.Second, "")
	if err != nil || code != 0 || out != "a'; id; echo '" {
		t.Fatalf("a value is data, never code: %q %d %v", out, code, err)
	}
}
