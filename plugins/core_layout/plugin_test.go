package core_layout

import "testing"

func TestSafeURL(t *testing.T) {
	for in, want := range map[string]string{
		"/c/1": "/c/1", "https://x.org/a?b=1": "https://x.org/a?b=1", "mailto:a@b.c": "mailto:a@b.c",
		"javascript:alert(1)": "#", " JavaScript:alert(1)": "#", "data:text/html,x": "#", "//evil.org": "#", "": "#",
	} {
		if got := SafeURL(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
