package pluginmgr

import "testing"

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "1.2.3", 0}, {"1.10.0", "1.9.9", 1}, {"7.0.0", "7.0.0-dev", 1}, {"7.0.0-rc1", "7.0.0", -1},
		{"2", "1.9", 1}, {"nightly", "1.0.0", -1}, {"1.0.0", "garbage", 1},
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
