package pluginmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
