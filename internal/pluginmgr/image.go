package pluginmgr

// Docker images: the "lite" image contains only the compiled binary, the
// "full" image additionally the source code and the Go and Node toolchains.
// Additional plugins live in <data>/plugins; the full image builds a binary
// with them into <data>/bin/ewiki-<hash> (hash of core version, architecture
// and the plugin sources). At start the image binary hands over to the built
// binary (see Launch), so builds survive restarts, updates and new
// containers. In a cluster all nodes share <data>: one node builds (file
// lock), the others pick up the result.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/version"
)

// Modes of the plugin manager.
const (
	ModeSource = "source" // own build from a source checkout
	ModeFull   = "full"   // full Docker image (builds into the data directory)
	ModeLite   = "lite"   // lite Docker image (no builds)
)

// Layout describes where plugins, builds and binaries live.
type Layout struct {
	Mode  string // ModeSource, ModeFull or ModeLite
	Extra string // additional plugins (images: <data>/plugins)
	Work  string // build tree (<data>/build/src)
	Cache string // Go, npm and home caches (<data>/build/cache)
	Bin   string // built binaries (<data>/bin)
}

// LayoutFromEnv reads the layout of a Docker image (EWIKI_IMAGE=full|lite,
// EWIKI_DATA=/data); without EWIKI_IMAGE it is a source build.
func LayoutFromEnv() Layout {
	mode := os.Getenv("EWIKI_IMAGE")
	if mode != ModeFull && mode != ModeLite {
		return Layout{Mode: ModeSource}
	}
	d := os.Getenv("EWIKI_DATA")
	if d == "" {
		d = "/data"
	}
	return Layout{Mode: mode, Extra: filepath.Join(d, "plugins"), Work: filepath.Join(d, "build", "src"),
		Cache: filepath.Join(d, "build", "cache"), Bin: filepath.Join(d, "bin")}
}

// Image reports whether the layout is a Docker image.
func (l Layout) Image() bool { return l.Mode == ModeFull || l.Mode == ModeLite }

// BuildMeta is stored next to a built binary.
type BuildMeta struct {
	Hash    string    `json:"hash"`
	Version string    `json:"version"`
	Plugins []string  `json:"plugins"`
	Trigger string    `json:"trigger"` // start | install | remove | update | drop | manual
	BuiltAt time.Time `json:"built_at"`
	By      string    `json:"by"` // node that built it
}

// lastBuild is the persisted state of the last build (shown after restarts).
type lastBuild struct {
	BuildStatus
	Hash string `json:"hash"`
}

// pluginDirs lists the plugin directories (with plugin.json) of dir by id.
func pluginDirs(dir string) map[string]string {
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "all" {
			continue
		}
		if mf, err := ReadManifest(filepath.Join(dir, e.Name())); err == nil {
			out[mf.ID] = e.Name()
		}
	}
	return out
}

// SourceHash identifies the additional plugins together with the core
// version and the architecture. ok is false if there are none.
func SourceHash(l Layout) (hash string, ok bool, err error) {
	dirs := pluginDirs(l.Extra)
	if len(dirs) == 0 {
		return "", false, nil
	}
	h := sha256.New()
	fmt.Fprintf(h, "ewiki %s %s/%s\n", version.Version, runtime.GOOS, runtime.GOARCH)
	ids := make([]string, 0, len(dirs))
	for id := range dirs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		root := filepath.Join(l.Extra, dirs[id])
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if !d.Type().IsRegular() {
				return nil
			}
			b, err := os.ReadFile(p) // #nosec G304 G122 -- plugin sources of the administrator; only regular files
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(l.Extra, p)
			fmt.Fprintf(h, "%s %s %d\n", id, filepath.ToSlash(rel), len(b))
			h.Write(b)
			return nil
		})
		if err != nil {
			return "", false, err
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16], true, nil
}

// fingerprint is a cheap summary of a directory tree (names, sizes, times)
// to notice changes, e.g. a plugin copied into the folder.
func fingerprint(dir string) string {
	h := sha256.New()
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == ".backup" || rel == "all") {
			return filepath.SkipDir
		}
		if info, err := d.Info(); err == nil && !d.IsDir() {
			fmt.Fprintf(h, "%s %d %d\n", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

// BinaryPath is the built binary for a hash.
func (l Layout) BinaryPath(hash string) string { return filepath.Join(l.Bin, "ewiki-"+hash) }

func readMeta(path string) *BuildMeta {
	b, err := os.ReadFile(path + ".json") // #nosec G304 -- build metadata next to the binary
	if err != nil {
		return nil
	}
	var m BuildMeta
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return &m
}

// fileLock is an exclusive lock on a file (shared data directory: one build
// at a time, also across containers).
func fileLock(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- lock file of the build directory
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// BuildImage builds a binary with the additional plugins (full image). It
// waits for a build of another node and returns its result if the binary
// already exists (built=false).
func BuildImage(ctx context.Context, root string, l Layout, trigger string, w io.Writer) (path string, meta *BuildMeta, built bool, err error) {
	if l.Mode != ModeFull {
		return "", nil, false, errors.New("building requires the full image (ghcr.io/…/enterprisewiki:full)")
	}
	unlock, err := fileLock(ctx, filepath.Join(l.Bin, ".build.lock"))
	if err != nil {
		return "", nil, false, err
	}
	defer unlock()
	hash, ok, err := SourceHash(l) // after the lock: the plugins may have changed meanwhile
	if err != nil {
		return "", nil, false, err
	}
	if !ok {
		return "", nil, false, errors.New("no additional plugins")
	}
	final := l.BinaryPath(hash)
	if _, err := os.Stat(final); err == nil {
		return final, readMeta(final), false, nil
	}
	_, _ = fmt.Fprintf(w, "==> preparing build tree %s\n", l.Work)
	if err := os.RemoveAll(l.Work); err != nil {
		return "", nil, false, err
	}
	if err := copyTree(root, l.Work); err != nil {
		return "", nil, false, err
	}
	if nm := filepath.Join(root, "web", "node_modules"); exists(nm) {
		if err := os.Symlink(nm, filepath.Join(l.Work, "web", "node_modules")); err != nil {
			return "", nil, false, err
		}
	}
	bundled := pluginDirs(filepath.Join(l.Work, "plugins"))
	extra := pluginDirs(l.Extra)
	var ids []string
	for id, dir := range extra {
		if b, ok := bundled[id]; ok { // a newer version of a bundled plugin
			if err := os.RemoveAll(filepath.Join(l.Work, "plugins", b)); err != nil {
				return "", nil, false, err
			}
		}
		if err := copyDir(filepath.Join(l.Extra, dir), filepath.Join(l.Work, "plugins", DirName(id))); err != nil {
			return "", nil, false, err
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	_, _ = fmt.Fprintf(w, "==> plugins: %s\n", strings.Join(ids, ", "))
	env := buildEnv(l)
	tmp := final + ".tmp"
	run := func(name string, args ...string) error {
		cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- go toolchain with fixed arguments
		cmd.Dir = l.Work
		cmd.Env = append(env, "EWIKI_BUILD_OUT="+tmp)
		cmd.Stdout, cmd.Stderr = w, w
		return cmd.Run()
	}
	if err := run("bash", "scripts/build.sh"); err != nil {
		_ = os.Remove(tmp)
		return "", nil, false, fmt.Errorf("build failed: %w", err)
	}
	if out, err := exec.CommandContext(ctx, tmp, "version").CombinedOutput(); err != nil { // #nosec G204 -- self-test of the binary just built
		_ = os.Remove(tmp)
		return "", nil, false, fmt.Errorf("new binary failed self-test: %v: %s", err, out)
	}
	host, _ := os.Hostname()
	meta = &BuildMeta{Hash: hash, Version: version.Version, Plugins: ids, Trigger: trigger, BuiltAt: time.Now().UTC(), By: host}
	b, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(final+".json", b, 0o600); err != nil {
		return "", nil, false, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return "", nil, false, err
	}
	pruneBinaries(l, 3)
	_ = os.RemoveAll(l.Work) // the tree is rebuilt for every build; keep the data directory small
	_, _ = fmt.Fprintf(w, "==> done: %s\n", final)
	return final, meta, true, nil
}

func buildEnv(l Layout) []string {
	var env []string
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		switch k {
		case "GOCACHE", "GOMODCACHE", "GOPROXY", "GOFLAGS", "GOTOOLCHAIN", "EWIKI_GO_TIDY", "HOME", "npm_config_cache", "EWIKI_BUILD_OUT", "EWIKI_VERSION", "EWIKI_CHILD":
			continue
		}
		env = append(env, e)
	}
	proxy := "https://proxy.golang.org,direct"
	if p := os.Getenv("EWIKI_GOPROXY_FILE"); p != "" && exists(p) {
		// modules of the core are part of the full image: builds work offline
		proxy = "file://" + p + "," + proxy
	}
	return append(env,
		"GOCACHE="+filepath.Join(l.Cache, "go-build"), "GOMODCACHE="+filepath.Join(l.Cache, "go-mod"),
		"GOPROXY="+proxy, "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local", "EWIKI_GO_TIDY=1",
		"HOME="+filepath.Join(l.Cache, "home"), "npm_config_cache="+filepath.Join(l.Cache, "npm"),
		"EWIKI_VERSION="+version.Version, "EWIKI_COMMIT="+version.Commit)
}

// pruneBinaries keeps the newest keep binaries (other nodes may still run them).
func pruneBinaries(l Layout, keep int) {
	matches, _ := filepath.Glob(filepath.Join(l.Bin, "ewiki-*"))
	var bins []string
	for _, p := range matches {
		if !strings.HasSuffix(p, ".json") && !strings.HasSuffix(p, ".tmp") {
			bins = append(bins, p)
		}
	}
	sort.Slice(bins, func(i, j int) bool { return modTime(bins[i]).After(modTime(bins[j])) })
	for i, p := range bins {
		if i >= keep {
			_ = os.Remove(p)
			_ = os.Remove(p + ".json")
		}
	}
}

func modTime(p string) time.Time {
	if st, err := os.Stat(p); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil } // #nosec G703 -- internal paths

// copyTree copies the core source tree for a build (without dependencies,
// build results and backups).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch rel {
			case ".git", "web/node_modules", "web/dist", "bin", "plugins/.backup", "data":
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p) // #nosec G304 G122 -- plugin sources of the administrator; only regular files
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, info.Mode().Perm()|0o200) // #nosec G703 G306 -- copy of the source tree for the build (relative path from WalkDir)
	})
}

// Launch is called at the start of the image binary: if a binary with the
// additional plugins exists (or, for "serve" in the full image, can be
// built), it returns its path to hand over to. On a failed build the image
// binary keeps running without the additional plugins and the failure is
// shown in the administration.
func Launch(ctx context.Context, root, configPath string, serve bool, log *slog.Logger) string {
	l := LayoutFromEnv()
	if !l.Image() || os.Getenv("EWIKI_CHILD") != "" {
		return ""
	}
	hash, ok, err := SourceHash(l)
	if err != nil {
		log.Error("reading additional plugins", "dir", l.Extra, "err", err)
		return ""
	}
	if !ok {
		return ""
	}
	if p := l.BinaryPath(hash); exists(p) {
		return p
	}
	if l.Mode == ModeLite {
		if serve {
			log.Warn("additional plugins are ignored in the lite image; use the full image to build them", "dir", l.Extra)
		}
		return ""
	}
	if !serve {
		return ""
	}
	log.Info("building a binary with the additional plugins (this takes a few minutes)", "dir", l.Extra)
	listen := ":8080"
	if st, err := config.Open(configPath); err == nil && st.Get().Server.Listen != "" {
		listen = st.Get().Server.Listen
	}
	stop := servePlaceholder(listen, log)
	defer stop()
	start := time.Now()
	var buf lockedBuffer
	p, _, _, err := BuildImage(ctx, root, l, "start", io.MultiWriter(&buf, os.Stderr))
	saveLastBuild(l, hash, start, &buf, err)
	if err != nil {
		log.Error("build with the additional plugins failed; starting without them", "err", err)
		return ""
	}
	return p
}

// servePlaceholder answers requests while the binary is being built at the
// start ("being updated", 503 with Retry-After).
func servePlaceholder(addr string, log *slog.Logger) func() {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Warn("placeholder page during the build", "addr", addr, "err", err)
		return func() {}
	}
	page := `<!doctype html><html lang="de"><meta charset="utf-8"><meta http-equiv="refresh" content="15">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1"><title>Wird aktualisiert – Updating</title>` +
		`<body style="font:1.1rem/1.5 system-ui,sans-serif;max-width:36rem;margin:15vh auto;padding:0 1rem;color:#1d2733">` +
		`<h1>Das Wiki wird aktualisiert</h1><p>Neue Erweiterungen werden eingebaut. Das dauert einige Minuten; diese Seite lädt sich selbst neu.</p>` +
		`<h2 lang="en">The wiki is being updated</h2><p lang="en">New plugins are being built. This takes a few minutes; this page reloads itself.</p></body></html>`
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":"updating","message":"the wiki is being updated"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(page))
	})}
	go func() { _ = srv.Serve(ln) }()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

func saveLastBuild(l Layout, hash string, start time.Time, buf *lockedBuffer, err error) {
	now := time.Now()
	st := lastBuild{BuildStatus: BuildStatus{OK: err == nil, StartedAt: &start, FinishedAt: &now, Log: tail(buf.String(), err)}, Hash: hash}
	b, _ := json.Marshal(st)
	_ = os.MkdirAll(l.Bin, 0o750)
	_ = os.WriteFile(filepath.Join(l.Bin, "last-build.json"), b, 0o600)
}

func loadLastBuild(l Layout) *lastBuild {
	b, err := os.ReadFile(filepath.Join(l.Bin, "last-build.json"))
	if err != nil {
		return nil
	}
	var st lastBuild
	if json.Unmarshal(b, &st) != nil {
		return nil
	}
	return &st
}

func tail(log string, err error) string {
	if err != nil {
		log += "\nBUILD FAILED: " + err.Error()
	} else {
		log += "\nBuild successful."
	}
	if len(log) > 20000 {
		log = log[len(log)-20000:]
	}
	return log
}
