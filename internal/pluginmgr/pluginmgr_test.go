package pluginmgr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

func TestGenerateAndManifest(t *testing.T) {
	root := t.TempDir()
	mk := func(dir, manifest string, gofile bool) {
		p := filepath.Join(root, "plugins", dir)
		os.MkdirAll(p, 0o755)
		os.WriteFile(filepath.Join(p, "plugin.json"), []byte(manifest), 0o644)
		if gofile {
			os.WriteFile(filepath.Join(p, "plugin.go"), []byte("package "+dir+"\n"), 0o644)
		}
	}
	mk("good_one", `{"id":"good.one","name":"Good","version":"1.0.0","kinds":["general"]}`, true)
	mk("no_go", `{"id":"no.go","name":"No Go","version":"1.0.0","kinds":["general"]}`, false)
	mk("bad_id", `{"id":"Bad ID","name":"Bad","version":"1"}`, true)
	if err := Generate(root); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "plugins", "all", "zz_generated.go"))
	s := string(b)
	if !strings.Contains(s, "plugins/good_one") || strings.Contains(s, "no_go") || strings.Contains(s, "bad_id") {
		t.Fatalf("generated:\n%s", s)
	}
	if DirName("theme.wiki.dark") != "theme_wiki_dark" {
		t.Fatal("dirname")
	}
}

// A wiki without a plugins folder (no source tree, nothing installed yet)
// lists no extra plugins and refuses removals clearly, without exposing the
// path of the server (it used to answer 500 with "stat …: no such file").
func TestNoPluginsFolder(t *testing.T) {
	m := New(t.TempDir(), nil, nil)
	m.SetLayout(Layout{Mode: ModeSource})
	mans, dirs, err := m.OnDisk()
	if err != nil || len(dirs) != 0 {
		t.Fatalf("on disk: %v %v %v", mans, dirs, err)
	}
	err = m.Remove(context.Background(), "nothing.here")
	if code, _, _ := apperr.Status(err); code != 404 {
		t.Fatalf("remove unknown: %v (%d)", err, code)
	}
}

// The command line never starts a background build in its own short
// process (the API of the command line, e.g. "ewiki api POST …/build").
func TestNoBackgroundBuildOnTheCommandLine(t *testing.T) {
	m := New(t.TempDir(), nil, nil).NoBackgroundBuild()
	if _, err := m.StartBuild(context.Background(), "manual"); err == nil {
		t.Fatal("the command line started a background build")
	}
	if m.BuildStatus().Running {
		t.Fatal("build running")
	}
}
