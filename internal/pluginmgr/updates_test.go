package pluginmgr

import "testing"

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "1.2.3", 0}, {"1.10.0", "1.9.9", 1}, {"7.0.0", "7.0.0-dev", 1}, {"7.0.0-rc1", "7.0.0", -1},
		{"2", "1.9", 1}, {"nightly", "1.0.0", -1}, {"1.0.0", "garbage", 1},
		// versions of git describe rank after their tag and before the next
		// version (the image "latest" is built from main)
		{"v1.2.0-5-gabc1234", "v1.2.0", 1}, {"v1.2.0", "v1.2.0-5-gabc1234", -1}, {"v1.2.0-5-gabc1234-dirty", "v1.2.1", -1},
		{"v1.2.0-12-gabc1234", "v1.2.0-5-gdef5678", 1}, {"v1.2.0-rc1-3-gabc1234", "v1.2.0-rc1", 1}, {"v1.2.0-rc1-3-gabc1234", "v1.2.0", -1},
	} {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("%s vs %s: %d", c.a, c.b, got)
		}
	}
	for in, want := range map[string]string{
		"github.com/a/b": "a/b", "https://github.com/a/b.git": "a/b", "git@github.com:a/b.git": "a/b", "a/b": "a/b", "gitlab.com/a/b": "",
	} {
		got, _ := githubRepo(in)
		if got != want {
			t.Errorf("%s: %q", in, got)
		}
	}
}

func TestSecuritySection(t *testing.T) {
	for _, c := range []struct {
		notes, want string
		ok          bool
	}{
		{"## Features\n- a\n\n## Security\n- fix XSS\n- fix CSRF\n\n## Fixes\n- b", "- fix XSS\n- fix CSRF", true},
		{"# 1.2.0\n### Sicherheit:\nLücke behoben\n#### Details\nmehr\n### Weiteres\nx", "Lücke behoben\n#### Details\nmehr", true},
		{"## SECURITY\r\nfix\r\n", "fix", true},
		{"## Security\n", "", true},
		{"Security fixes are not in a heading\n## Fixes\n- security", "", false},
		{"##Security\nno space after the hashes is no heading", "", false},
		{"", "", false},
	} {
		got, ok := SecuritySection(c.notes)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: %q %v", c.notes, got, ok)
		}
	}
	if got := clip("äöü", 3); got != "ä…" {
		t.Errorf("clip: %q", got)
	}
}
