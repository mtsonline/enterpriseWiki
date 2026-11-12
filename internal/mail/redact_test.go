package mail

import (
	"regexp"
	"testing"
)

func TestRedact(t *testing.T) {
	RegisterRedaction(regexp.MustCompile(`(/x/link\?t=)[A-Za-z0-9_.\-]+`))
	RegisterRedaction(regexp.MustCompile(`(/x/link\?t=)[A-Za-z0-9_.\-]+`)) // twice: once
	got := Redact("- Agree: https://w.example/x/link?t=abc.DEF-_1\n- No: https://w.example/x/link?t=zz9\nother")
	want := "- Agree: https://w.example/x/link?t=[hidden]\n- No: https://w.example/x/link?t=[hidden]\nother"
	if got != want {
		t.Fatalf("%q", got)
	}
}
