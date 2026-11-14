package pluginmgr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

func TestNormalizeSource(t *testing.T) {
	for in, want := range map[string]string{
		"https://GitHub.com/Acme/Kanban.git":        "https://github.com/Acme/Kanban",
		"https://github.com/acme/kanban/":           "https://github.com/acme/kanban",
		"github.com/acme/kanban":                    "https://github.com/acme/kanban",
		"git@git.example.com:team/kanban.git":       "ssh://git.example.com/team/kanban",
		"ssh://git@git.example.com/team/kanban.git": "ssh://git.example.com/team/kanban",
		"https://git.example.com./team/x":           "https://git.example.com/team/x",
		"https://git.example.com:443/team/x":        "https://git.example.com/team/x",
		"ssh://git@git.example.com:22/team/x":       "ssh://git.example.com/team/x",
		"https://git.example.com:8443/team/x":       "https://git.example.com:8443/team/x",
		"HTTPS://git.example.com/team/x/":           "https://git.example.com/team/x",
	} {
		if got, err := NormalizeSource(in); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "http://git.example.com/team/x", "file:///etc", "https://user:secret@git.example.com/x",
		"https://git.example.com/team/../other/x", "https://git.example.com/x?ref=1", "https://git.example.com/x#y", "https:///x",
		// encodings, backslashes, control characters and empty segments a
		// git server might read differently than the prefix check
		"https://git.example.com/team/%2e%2e/evil/x", "https://git.example.com/team%2F..%2Fevil/x", `https://git.example.com/team/..\evil`,
		"https://git.example.com/team/x\x00", "https://git.example.com/team/x\ty", "https://git.example.com/team//x", "git@git.example.com:team/../evil"} {
		if got, err := NormalizeSource(bad); err == nil {
			t.Errorf("%q accepted as %q", bad, got)
		}
	}
}

func TestPrefixMatches(t *testing.T) {
	const p = "https://git.example.com/wiki"
	for src, want := range map[string]bool{
		"https://git.example.com/wiki":             true,
		"https://git.example.com/wiki/kanban":      true,
		"https://git.example.com/wiki-evil/kanban": false, // not at a path boundary
		"https://git.example.com/wikikanban":       false,
		"https://git.example.com.evil.test/wiki/x": false, // other host
		"ssh://git.example.com/wiki/x":             false, // other scheme
	} {
		if got := prefixMatches(src, p); got != want {
			t.Errorf("%s: %v, want %v", src, got, want)
		}
	}
	if _, err := NormalizeTrusted("https://git.example.com"); err == nil {
		t.Error("a whole host accepted as trusted source")
	}
	if _, err := NormalizeTrusted("relative/dir"); err == nil {
		t.Error("a relative directory accepted as trusted source")
	}
}

// TestOfficialPlugins keeps officialPlugins in step with the plugins/ folder
// of the repository.
func TestOfficialPlugins(t *testing.T) {
	m := &Manager{root: filepath.Join("..", "..")}
	mans, _, err := m.OnDisk()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for id := range mans {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, officialPlugins) {
		t.Fatalf("officialPlugins (internal/pluginmgr/official.go) differs from plugins/:\nlist %v\ndisk %v", officialPlugins, ids)
	}
}

// gitRepo creates a real git repository holding a plugin.
func gitRepo(t *testing.T, dir, id string) string {
	t.Helper()
	writePlugin(t, dir, id, "1.0.0")
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "v1"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out[:len(out)-1])
}

func TestInstallOrigin(t *testing.T) {
	root := fakeRoot(t)
	st, err := config.Open(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(root, st, nil)
	ctx := context.Background()
	own := t.TempDir()
	trustedDir := filepath.Join(own, "team")
	commit := gitRepo(t, filepath.Join(trustedDir, "kanban"), "acme.kanban")
	foreign := filepath.Join(t.TempDir(), "evil")
	writePlugin(t, foreign, "evil.plugin", "1.0.0")
	// a plugin trying to declare itself official
	_ = os.WriteFile(filepath.Join(foreign, originFile), []byte(`{"level":"official"}`), 0o644)

	// nothing trusted yet: both are external and need the confirmation
	if _, err := m.Install(ctx, filepath.Join(trustedDir, "kanban"), "", InstallOptions{}); !errors.Is(err, ErrExternalNotAccepted) {
		t.Fatalf("external without confirmation: %v", err)
	}
	if err := st.Update(func(c *config.Config) error {
		return SetTrustedSources(c, []config.TrustedSource{{Prefix: trustedDir, Note: "own team"}})
	}); err != nil {
		t.Fatal(err)
	}
	res, err := m.Install(ctx, filepath.Join(trustedDir, "kanban"), "", InstallOptions{By: "admin@local"})
	if err != nil || res.Origin.Level != LevelOrganisation || res.Origin.Commit != commit || res.Origin.InstalledBy != "admin@local" {
		t.Fatalf("trusted install: %+v %v", res, err)
	}
	// a sibling folder sharing the name prefix is not trusted
	sibling := trustedDir + "-other"
	writePlugin(t, sibling, "sib.plugin", "1.0.0")
	if c, _ := m.CheckSource(sibling); c == nil || c.Level != LevelExternal {
		t.Fatalf("sibling folder: %+v", c)
	}

	// external: refused without, installed with the confirmation; the
	// record coming with the sources is replaced
	if _, err := m.Install(ctx, foreign, "", InstallOptions{}); !errors.Is(err, ErrExternalNotAccepted) {
		t.Fatalf("external without confirmation: %v", err)
	}
	res, err = m.Install(ctx, foreign, "", InstallOptions{AcceptExternal: true, By: "cli"})
	if err != nil || res.Origin.Level != LevelExternal || !res.Origin.Accepted {
		t.Fatalf("external install: %+v %v", res, err)
	}
	// updating needs the confirmation again
	if _, err := m.Install(ctx, foreign, "", InstallOptions{}); !errors.Is(err, ErrExternalNotAccepted) {
		t.Fatalf("external update without confirmation: %v", err)
	}
	// switched off: refused even with the confirmation
	off := false
	_ = st.Update(func(c *config.Config) error { c.Plugins.AllowExternal = &off; return nil })
	if _, err := m.Install(ctx, foreign, "", InstallOptions{AcceptExternal: true}); err == nil {
		t.Fatal("external installed although switched off")
	}

	// a plugin folder copied by hand has no record: unknown
	writePlugin(t, filepath.Join(root, "plugins", "dropped"), "dropped.plugin", "1.0.0")
	origins := m.Origins()
	if origins["acme.kanban"].Level != LevelOrganisation || origins["evil.plugin"].Level != LevelExternal ||
		origins["dropped.plugin"].Level != LevelUnknown || origins["core.html"].Level != LevelOfficial {
		t.Fatalf("origins %+v", origins)
	}
	// a wiki without plugin folder still lists the origins
	if o := New(t.TempDir(), st, nil).Origins(); o == nil {
		t.Fatal("no origins without plugin folder")
	}
	var rec Origin
	b, _ := os.ReadFile(filepath.Join(root, "plugins", "evil_plugin", originFile))
	if json.Unmarshal(b, &rec) != nil || rec.Level != LevelExternal {
		t.Fatalf("record of the external plugin: %s", b)
	}
}
