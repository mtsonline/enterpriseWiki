package content

import "testing"

// TestMaskMentions: every mention keeps "@" and two characters, an e-mail
// address and trailing punctuation stay as they are.
func TestMaskMentions(t *testing.T) {
	for in, want := range map[string]string{
		"Ask @bob, then @carl@oidc.": "Ask @bo****, then @ca****.",
		"mail bob@example.com":       "mail bob@example.com",
		"@a and (@xy)":               "@a**** and (@xy****)",
	} {
		if got := MaskMentions(in); got != want {
			t.Errorf("MaskMentions(%q) = %q, want %q", in, got, want)
		}
	}
}
