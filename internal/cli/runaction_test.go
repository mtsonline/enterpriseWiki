package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestCLIWithoutCommand: an action without a command line way (approved
// exception, e.g. setting up a second factor) answers with an error. Before
// 16.11.2026 the command line crashed with a nil pointer.
func TestCLIWithoutCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := RunAction("core.me.mfa.setup.begin", nil, strings.NewReader(""), &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "has no command") || !strings.Contains(errOut.String(), "browser") {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
}
