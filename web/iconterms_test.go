package web

import "testing"

// TestIconLabel: icon names in words, translated word by word where the
// terms have a word (the same rule as iconLabel in src/core/icons.ts).
func TestIconLabel(t *testing.T) {
	for _, c := range []struct{ lang, name, want string }{
		{"en", "arrow-left", "arrow left"},
		{"de", "arrow-left", "Pfeil links"},
		{"de-AT", "clock", "Uhr"},
		{"de", "zzqx-arrow", "zzqx Pfeil"},
		{"fr", "arrow-left", "arrow left"},
	} {
		if got := IconLabel(c.lang, c.name); got != c.want {
			t.Errorf("IconLabel(%q, %q) = %q, want %q", c.lang, c.name, got, c.want)
		}
	}
}
