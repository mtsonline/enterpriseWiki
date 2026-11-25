package security_crowdsec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonical(t *testing.T) {
	ok := map[string]string{
		"192.0.2.10":        "192.0.2.10",
		" 192.0.2.10 ":      "192.0.2.10",
		"::ffff:192.0.2.10": "192.0.2.10",
		"192.0.2.10/32":     "192.0.2.10",
		"198.51.100.7/24":   "198.51.100.0/24",
		"10.0.0.0/8":        "10.0.0.0/8",
		"2001:DB8::1":       "2001:db8::1",
		"2001:db8::/32":     "2001:db8::/32",
		"2001:db8::/24":     "2001:d00::/24",
	}
	for in, want := range ok {
		got, err := canonical(in)
		if err != nil || got != want {
			t.Errorf("canonical(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0.0.0.0/0", "10.0.0.0/7", "::/0", "2001:db8::/23", "-rf", "--all", "1.2.3.4 --all",
		"1.2.3.4\n5.6.7.8", "example.com", "fe80::1%eth0", "1.2.3", "1.2.3.4/33", "$(id)"} {
		if got, err := canonical(in); err == nil {
			t.Errorf("canonical(%q) = %q, want an error", in, got)
		}
	}
}

func TestCleanEntries(t *testing.T) {
	out, err := cleanEntries([]Entry{{Value: "198.51.100.9/24", Note: " office "}, {Value: "192.0.2.1"}}, 10)
	if err != nil || len(out) != 2 || out[0].Value != "192.0.2.1" || out[1].Value != "198.51.100.0/24" || out[1].Note != "office" {
		t.Fatalf("%+v %v", out, err)
	}
	for name, in := range map[string][]Entry{
		"twice":     {{Value: "192.0.2.1"}, {Value: "192.0.2.1/32"}},
		"too many":  {{Value: "192.0.2.1"}, {Value: "192.0.2.2"}, {Value: "192.0.2.3"}},
		"line":      {{Value: "192.0.2.1", Note: "a\nb"}},
		"long note": {{Value: "192.0.2.1", Note: strings.Repeat("x", noteMax+1)}},
	} {
		if _, err := cleanEntries(in, 2); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestWriteAllowlist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "exchange") // created on demand
	if err := writeAllowlist(dir, []string{"192.0.2.1", "198.51.100.0/24"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, AllowlistFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != fileHeader+"192.0.2.1\n198.51.100.0/24\n" {
		t.Fatalf("file:\n%s", b)
	}
	// unchanged content: the file is not written again
	st, _ := os.Stat(filepath.Join(dir, AllowlistFile))
	if err := writeAllowlist(dir, []string{"192.0.2.1", "198.51.100.0/24"}); err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(filepath.Join(dir, AllowlistFile))
	if !st.ModTime().Equal(st2.ModTime()) {
		t.Fatal("written again without a change")
	}
	// no temporary files are left
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("files in the exchange folder: %v", ents)
	}
}
