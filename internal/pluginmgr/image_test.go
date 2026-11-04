package pluginmgr

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

func writePlugin(t *testing.T, dir, id, ver string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mf := `{"id":"` + id + `","name":"` + id + `","version":"` + ver + `"}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(mf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeRoot is a source tree whose build script writes a shell script as
// "binary" (prints the plugin directories of the build tree on "version").
func fakeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writePlugin(t, filepath.Join(root, "plugins", "core_html"), "core.html", "1.0.0")
	writePlugin(t, filepath.Join(root, "plugins", "theme_wiki"), "theme.wiki", "1.0.0")
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(root, "scripts"), 0o755)
	script := `set -e
test "$EWIKI_GO_TIDY" = 1
case "$GOPROXY" in file://*) ;; *) [ -z "$EWIKI_GOPROXY_FILE" ] || exit 3 ;; esac
dirs=$(ls plugins | tr '\n' ' ')
printf '#!/bin/sh\necho "%s"\n' "$dirs" > "$EWIKI_BUILD_OUT"
chmod +x "$EWIKI_BUILD_OUT"
`
	if err := os.WriteFile(filepath.Join(root, "scripts", "build.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestImageLayoutBuildAndLaunch(t *testing.T) {
	root := fakeRoot(t)
	data := t.TempDir()
	t.Setenv("EWIKI_IMAGE", "full")
	t.Setenv("EWIKI_DATA", data)
	t.Setenv("EWIKI_CHILD", "")
	l := LayoutFromEnv()
	if l.Mode != ModeFull || l.Extra != filepath.Join(data, "plugins") {
		t.Fatalf("layout %+v", l)
	}
	if _, ok, _ := SourceHash(l); ok {
		t.Fatal("hash without additional plugins")
	}
	if p := Launch(context.Background(), root, filepath.Join(data, "config.yaml"), false, nil); p != "" {
		t.Fatalf("launch without plugins: %s", p)
	}
	// an additional plugin and a newer version of a bundled one
	writePlugin(t, filepath.Join(l.Extra, "my_plugin"), "my.plugin", "0.1.0")
	writePlugin(t, filepath.Join(l.Extra, "html_new"), "core.html", "2.0.0")
	h1, ok, err := SourceHash(l)
	if err != nil || !ok {
		t.Fatal(err)
	}
	var log bytes.Buffer
	p, meta, built, err := BuildImage(context.Background(), root, l, "install", &log)
	if err != nil || !built {
		t.Fatalf("build: %v %v\n%s", built, err, log.String())
	}
	if p != l.BinaryPath(h1) || meta.Trigger != "install" || strings.Join(meta.Plugins, ",") != "core.html,my.plugin" {
		t.Fatalf("result %s %+v", p, meta)
	}
	out, _ := runOut(p)
	if !strings.Contains(out, "core_html") || !strings.Contains(out, "my_plugin") || !strings.Contains(out, "theme_wiki") {
		t.Fatalf("build tree: %q", out)
	}
	// built already: a second node only picks it up
	_, meta2, built, err := BuildImage(context.Background(), root, l, "drop", &log)
	if err != nil || built || meta2.Trigger != "install" {
		t.Fatalf("second build: %v %v %+v", built, err, meta2)
	}
	if got := Launch(context.Background(), root, "", false, nil); got != p {
		t.Fatalf("launch picks %q, want %q", got, p)
	}
	// changing a plugin changes the hash
	_ = os.WriteFile(filepath.Join(l.Extra, "my_plugin", "plugin.go"), []byte("package x\n// v2\n"), 0o644)
	if h2, _, _ := SourceHash(l); h2 == h1 {
		t.Fatal("hash did not change")
	}
	// the manager sees bundled and additional plugins; bundled ones cannot be removed
	st, err := config.Open(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(root, st, nil)
	disk, dirs, err := m.OnDisk()
	if err != nil || disk["theme.wiki"] == nil || disk["core.html"].Version != "2.0.0" || dirs["my.plugin"] != "my_plugin" || dirs["theme.wiki"] != "" {
		t.Fatalf("on disk: %v %v %v", disk, dirs, err)
	}
	if err := m.Remove(context.Background(), "theme.wiki"); err == nil || !strings.Contains(err.Error(), "only be disabled") {
		t.Fatalf("remove bundled: %v", err)
	}
	if err := m.Remove(context.Background(), "my.plugin"); err != nil {
		t.Fatal(err)
	}
	if m.takeTrigger() != "remove" {
		t.Fatal("trigger not recorded")
	}
	// lite: no builds, no installs
	t.Setenv("EWIKI_IMAGE", "lite")
	lm := New(root, st, nil)
	if lm.BuildUnavailable() != "lite" {
		t.Fatalf("lite can build: %q", lm.BuildUnavailable())
	}
	if _, err := lm.Install(context.Background(), root, ""); err == nil || !strings.Contains(err.Error(), "lite") {
		t.Fatalf("lite install: %v", err)
	}
}

func runOut(p string) (string, error) {
	b, err := execCommand(p, "version")
	return string(b), err
}

func execCommand(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func TestWatchBuildsDroppedPlugins(t *testing.T) {
	root := fakeRoot(t)
	data := t.TempDir()
	t.Setenv("EWIKI_IMAGE", "full")
	t.Setenv("EWIKI_DATA", data)
	st, err := config.Open(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	restarts := make(chan struct{}, 4)
	m := New(root, st, func() { restarts <- struct{}{} })
	if !m.CanBuild() {
		t.Skip("go and npm are needed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Watch(ctx, 30*time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	waitBuilt := func() {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !(m.RestartPending() && !m.BuildStatus().Running) {
			if time.Now().After(deadline) {
				t.Fatalf("no build: %+v", m.BuildStatus())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	// copied into the folder: build, but no automatic restart
	writePlugin(t, filepath.Join(m.PluginsDir(), "dropped"), "dropped.plugin", "1.0.0")
	waitBuilt()
	if b := m.BuildStatus(); !b.OK || b.Trigger != "drop" {
		t.Fatalf("build: %+v", b)
	}
	select {
	case <-restarts:
		t.Fatal("restarted after a dropped plugin")
	case <-time.After(500 * time.Millisecond):
	}
	// with auto_restart_on_drop the server restarts
	if err := st.Update(func(c *config.Config) error { c.Plugins.AutoRestartOnDrop = true; return nil }); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.restartPending = false
	m.mu.Unlock()
	writePlugin(t, filepath.Join(m.PluginsDir(), "second"), "second.plugin", "1.0.0")
	waitBuilt()
	select {
	case <-restarts:
	case <-time.After(3 * time.Second):
		t.Fatal("no restart with auto_restart_on_drop")
	}
}
