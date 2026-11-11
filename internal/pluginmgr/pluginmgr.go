// Package pluginmgr installs, upgrades and removes plugins in the source tree
// and rebuilds the binary.
//
// Plugins are Go packages compiled into the binary. Installing a plugin
// copies (or git-clones) it into plugins/<dir>, regenerates
// plugins/all/zz_generated.go and rebuilds backend and frontend with
// scripts/build.sh. A failed build never touches the running binary.
package pluginmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

// ModulePath is the Go module path of the core.
const ModulePath = "github.com/mtsonline/enterprisewiki"

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,63}$`)

// Manager manages plugin sources.
type Manager struct {
	root    string // source tree (contains go.mod)
	cfg     *config.Store
	mu      sync.Mutex
	build   BuildStatus
	restart func()
	updates updateCache
	layout  Layout
	cluster Cluster
	log     *slog.Logger
	// restartPending: a build finished but the server still runs the old binary
	restartPending bool
	trigger        string // trigger of the running build
	fp             string // fingerprint of the plugin folder (watch)
}

// BuildStatus reports the last build.
type BuildStatus struct {
	Running    bool       `json:"running"`
	OK         bool       `json:"ok"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Log        string     `json:"log"`
	Trigger    string     `json:"trigger,omitempty"`
}

// New creates a manager. restart is called to restart the process.
// The layout is read from the environment (Docker images, see LayoutFromEnv).
func New(root string, cfg *config.Store, restart func()) *Manager {
	m := &Manager{root: root, cfg: cfg, restart: restart, layout: LayoutFromEnv(), log: slog.Default()}
	if m.layout.Image() {
		if st := loadLastBuild(m.layout); st != nil {
			m.build = st.BuildStatus
		}
	}
	return m
}

// SetLayout overrides the layout (tests).
func (m *Manager) SetLayout(l Layout) { m.layout = l }

// Mode returns source, full or lite.
func (m *Manager) Mode() string { return m.layout.Mode }

// PluginsDir returns the directory new plugins are installed into (and
// watched for plugins copied there): plugins/ of the source tree, or
// <data>/plugins in the Docker images.
func (m *Manager) PluginsDir() string {
	if m.layout.Image() {
		return m.layout.Extra
	}
	return filepath.Join(m.root, "plugins")
}

// CanBuild reports whether the source tree and toolchains are available.
func (m *Manager) CanBuild() bool { return m.BuildUnavailable() == "" }

// BuildUnavailable explains why plugins cannot be built here ("" = they can):
// lite (lite image) or toolchain (no source tree, go or npm).
func (m *Manager) BuildUnavailable() string {
	if m.layout.Mode == ModeLite {
		return "lite"
	}
	if _, err := os.Stat(filepath.Join(m.root, "go.mod")); err != nil {
		return "toolchain"
	}
	if _, err := exec.LookPath("go"); err != nil {
		return "toolchain"
	}
	if _, err := exec.LookPath("npm"); err != nil {
		return "toolchain"
	}
	return ""
}

// ReadManifest reads plugin.json of a plugin directory.
func ReadManifest(dir string) (*sdk.Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json")) // #nosec G304 -- manifest in the plugin directory
	if err != nil {
		return nil, fmt.Errorf("plugin.json not found in %s", dir)
	}
	var mf sdk.Manifest
	if err := json.Unmarshal(b, &mf); err != nil {
		return nil, fmt.Errorf("plugin.json: %w", err)
	}
	if !idRe.MatchString(mf.ID) {
		return nil, fmt.Errorf("plugin.json: invalid id %q", mf.ID)
	}
	if mf.Name == "" || mf.Version == "" {
		return nil, errors.New("plugin.json: name and version are required")
	}
	if err := mf.Validate(); err != nil {
		return nil, fmt.Errorf("plugin.json: %w", err)
	}
	return &mf, nil
}

// DirName returns the directory name for a plugin id (core.html -> core_html).
func DirName(id string) string { return strings.NewReplacer(".", "_", "-", "_").Replace(id) }

// OnDisk lists the plugins present in the source tree.
// In the Docker images these are the bundled plugins of the image plus the
// additional ones in <data>/plugins (which win); dirs then only lists the
// additional ones.
func (m *Manager) OnDisk() (map[string]*sdk.Manifest, map[string]string, error) {
	mans := map[string]*sdk.Manifest{}
	dirs := map[string]string{}
	if m.layout.Image() {
		bundled := filepath.Join(m.root, "plugins")
		for _, d := range pluginDirs(bundled) {
			if mf, err := ReadManifest(filepath.Join(bundled, d)); err == nil {
				mans[mf.ID] = mf
			}
		}
		if len(mans) == 0 { // no source in the image (lite): the compiled plugins are the bundled ones
			for _, p := range sdk.Registered() {
				mf := p.Manifest()
				mans[mf.ID] = &mf
			}
		}
	}
	dir := m.PluginsDir()
	if _, err := os.Stat(dir); err != nil {
		if m.layout.Image() && errors.Is(err, os.ErrNotExist) {
			return mans, dirs, nil
		}
		return nil, nil, err
	}
	for id, d := range pluginDirs(dir) {
		mf, err := ReadManifest(filepath.Join(dir, d))
		if err != nil {
			continue
		}
		mans[id] = mf
		dirs[id] = d
	}
	return mans, dirs, nil
}

// Change is a difference between the source tree and the running binary.
type Change struct {
	ID      string `json:"id"`
	Action  string `json:"action"` // install | upgrade | remove
	Version string `json:"version"`
}

// Pending lists changes that need a rebuild to take effect.
func (m *Manager) Pending() []Change {
	out := []Change{}
	disk, _, err := m.OnDisk()
	if err != nil {
		return out
	}
	compiled := map[string]sdk.Manifest{}
	for _, p := range sdk.Registered() {
		compiled[p.Manifest().ID] = p.Manifest()
	}
	for id, mf := range disk {
		c, ok := compiled[id]
		switch {
		case !ok:
			out = append(out, Change{id, "install", mf.Version})
		case c.Version != mf.Version:
			out = append(out, Change{id, "upgrade", mf.Version})
		}
	}
	for id, c := range compiled {
		if _, ok := disk[id]; !ok {
			out = append(out, Change{id, "remove", c.Version})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// InstallResult describes an installation.
type InstallResult struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Dir             string `json:"dir"`
	Upgrade         bool   `json:"upgrade"`
	RebuildRequired bool   `json:"rebuild_required"`
}

func (m *Manager) resolve(source, ref string) (gitURL, localDir, outRef string, err error) {
	source = strings.TrimSpace(source)
	for _, c := range m.cfg.Get().Plugins.Catalog {
		if c.ID == source {
			source = c.Repo
			if ref == "" {
				ref = c.Ref
			}
		}
	}
	switch {
	case source == "":
		return "", "", "", apperr.BadRequest("source is required")
	case strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "git@") || strings.HasPrefix(source, "ssh://"):
		return source, "", ref, nil
	case strings.HasPrefix(source, "github.com/") || strings.HasPrefix(source, "gitlab.com/"):
		return "https://" + strings.TrimSuffix(source, ".git") + ".git", "", ref, nil
	}
	if st, err := os.Stat(source); err == nil && st.IsDir() {
		return "", source, ref, nil
	}
	return "", "", "", apperr.BadRequest("source must be a catalog id, a git URL, github.com/owner/repo or an existing local directory")
}

// Install installs or upgrades a plugin from source (catalog id, git URL,
// github.com/owner/repo or local directory). A rebuild is required afterwards.
func (m *Manager) Install(ctx context.Context, source, ref string) (*InstallResult, error) {
	if m.layout.Mode == ModeLite {
		return nil, apperr.BadRequest("the lite image cannot install plugins: use the full image (ghcr.io/mtsonline/enterprisewiki:full) or your own build")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.PluginsDir(), 0o750); err != nil {
		return nil, err
	}
	gitURL, local, ref, err := m.resolve(source, ref)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "ewiki-plugin-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	src := filepath.Join(tmp, "src")
	if gitURL != "" {
		args := []string{"clone", "--depth", "1"}
		if ref != "" {
			args = append(args, "--branch", ref)
		}
		args = append(args, "--", gitURL, src)
		cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(cctx, "git", args...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, apperr.BadRequest("git clone failed: %s", strings.TrimSpace(string(out)))
		}
		_ = os.RemoveAll(filepath.Join(src, ".git"))
	} else if err := copyDir(local, src); err != nil {
		return nil, err
	}
	mf, err := ReadManifest(src)
	if err != nil {
		return nil, apperr.BadRequest("%s", err.Error())
	}
	if err := checkGoPackage(src); err != nil {
		return nil, apperr.BadRequest("%s", err.Error())
	}
	_, dirs, err := m.OnDisk()
	if err != nil {
		return nil, err
	}
	dirName := DirName(mf.ID)
	if d, ok := dirs[mf.ID]; ok {
		dirName = d
	}
	target := filepath.Join(m.PluginsDir(), dirName)
	res := &InstallResult{ID: mf.ID, Name: mf.Name, Version: mf.Version, Dir: "plugins/" + dirName, RebuildRequired: true}
	if _, err := os.Stat(target); err == nil {
		res.Upgrade = true
		if err := m.backup(dirName); err != nil {
			return nil, err
		}
	}
	if err := copyDir(src, target); err != nil {
		return nil, err
	}
	if !m.layout.Image() {
		if err := Generate(m.root); err != nil {
			return nil, err
		}
	}
	m.markTrigger("install")
	return res, nil
}

// markTrigger notes why the plugin folder changed, for the server watching
// it (e.g. after "ewiki plugin install" in another process).
func (m *Manager) markTrigger(trigger string) {
	_ = os.WriteFile(filepath.Join(m.PluginsDir(), ".trigger"), []byte(trigger), 0o600)
}

func (m *Manager) takeTrigger() string {
	p := filepath.Join(m.PluginsDir(), ".trigger")
	b, err := os.ReadFile(p) // #nosec G304 -- fixed file in the plugin directory
	if err != nil {
		return ""
	}
	_ = os.Remove(p)
	return strings.TrimSpace(string(b))
}

func (m *Manager) backup(dirName string) error {
	bdir := filepath.Join(m.PluginsDir(), ".backup")
	if err := os.MkdirAll(bdir, 0o750); err != nil {
		return err
	}
	return os.Rename(filepath.Join(m.PluginsDir(), dirName), filepath.Join(bdir, dirName+"-"+time.Now().Format("20060102-150405")))
}

// Remove moves a plugin's source to plugins/.backup. Required plugins
// cannot be removed. A rebuild is required afterwards.
func (m *Manager) Remove(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	disk, dirs, err := m.OnDisk()
	if err != nil {
		return err
	}
	mf, ok := disk[id]
	if !ok {
		return apperr.NotFound("plugin source " + id)
	}
	if _, extra := dirs[id]; !extra {
		return apperr.BadRequest("plugin %s is part of the image and can only be disabled", id)
	}
	if mf.Required {
		return apperr.BadRequest("plugin %s is required and cannot be removed", id)
	}
	for oid, o := range disk {
		for _, d := range o.Extends {
			if d == id {
				return apperr.Conflict("plugin %s extends %s; remove it first", oid, id)
			}
		}
	}
	if err := m.backup(dirs[id]); err != nil {
		return err
	}
	m.markTrigger("remove")
	if m.layout.Image() {
		return nil
	}
	return Generate(m.root)
}

// StartBuild runs scripts/build.sh in the background. On success the new
// binary replaces the running executable; call ScheduleRestart to activate it.
//
// trigger says why (install, remove, update, drop, manual) and decides what
// happens afterwards: install, remove and update restart automatically
// (one node after the other in a cluster); after plugins were copied into
// the folder (drop) only with plugins.auto_restart_on_drop, otherwise the
// administration offers "Restart now".
func (m *Manager) StartBuild(_ context.Context, trigger string) (*BuildStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.build.Running {
		return nil, apperr.Conflict("a build is already running")
	}
	switch m.BuildUnavailable() {
	case "lite":
		return nil, apperr.BadRequest("the lite image cannot build plugins: use the full image (ghcr.io/mtsonline/enterprisewiki:full) or your own build")
	case "toolchain":
		return nil, apperr.BadRequest("building requires the source tree with go.mod and the go and npm toolchains on this server")
	}
	if trigger == "" {
		trigger = "manual"
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	m.build = BuildStatus{Running: true, StartedAt: &now, Trigger: trigger}
	m.takeTrigger()
	m.fp = fingerprint(m.PluginsDir())
	if m.layout.Image() {
		go m.runImageBuild(trigger) // #nosec G118 -- the build outlives the request on purpose
	} else {
		go m.runBuild(exe, trigger) // #nosec G118 -- the build outlives the request on purpose
	}
	st := m.build
	return &st, nil
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (m *Manager) runBuild(exe, trigger string) {
	out := exe + ".new"
	var buf lockedBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(m.root, "scripts", "build.sh")) // #nosec G204 -- build script of the installation
	cmd.Dir = m.root
	cmd.Env = append(os.Environ(), "EWIKI_BUILD_OUT="+out)
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	if err == nil {
		// verify the new binary starts
		if vout, verr := exec.Command(out, "version").CombinedOutput(); verr != nil { // #nosec G204 -- self-test of the binary just built
			err = fmt.Errorf("new binary failed self-test: %v: %s", verr, vout)
		} else {
			err = os.Rename(out, exe)
		}
	}
	m.finishBuild(&buf, err, trigger)
}

// runImageBuild builds into the data directory (full image); the restart
// hands over to the new binary.
func (m *Manager) runImageBuild(trigger string) {
	var buf lockedBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	hash, ok, err := SourceHash(m.layout)
	var meta *BuildMeta
	switch {
	case err != nil:
	case !ok:
		// all additional plugins removed: the image binary is the right one
		_, _ = fmt.Fprintln(&buf, "No additional plugins: the image binary is used.")
	default:
		var built bool
		// the build output also goes to the server log (docker compose logs)
		_, meta, built, err = BuildImage(ctx, m.root, m.layout, trigger, io.MultiWriter(&buf, os.Stderr))
		if err == nil && !built {
			_, _ = fmt.Fprintln(&buf, "Already built (by "+metaBy(meta)+").")
			if meta != nil && meta.Trigger != "" && meta.Trigger != "start" {
				trigger = meta.Trigger // e.g. installed on another node: restart like there
			}
		}
	}
	start := *m.BuildStatus().StartedAt
	saveLastBuild(m.layout, hash, start, &buf, err)
	m.finishBuild(&buf, err, trigger)
}

func metaBy(meta *BuildMeta) string {
	if meta == nil || meta.By == "" {
		return "another node"
	}
	return meta.By
}

func (m *Manager) finishBuild(buf *lockedBuffer, err error, trigger string) {
	m.mu.Lock()
	now := time.Now()
	m.build.Running, m.build.FinishedAt, m.build.OK = false, &now, err == nil
	m.build.Log = tail(buf.String(), err)
	auto := false
	if err == nil {
		m.restartPending = true
		auto = trigger == "install" || trigger == "remove" || trigger == "update" ||
			(trigger == "drop" && m.cfg.Get().Plugins.AutoRestartOnDrop)
		if auto {
			m.build.Log += " The server restarts automatically."
		} else {
			m.build.Log += " Restart to activate the new version."
		}
	}
	m.mu.Unlock()
	if err != nil {
		m.log.Error("plugin build failed", "trigger", trigger, "err", err)
		return
	}
	m.log.Info("plugin build finished", "trigger", trigger, "restart", auto)
	if auto {
		m.RestartAll(context.Background())
	}
}

// BuildStatus returns the state of the last build.
func (m *Manager) BuildStatus() BuildStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.build
}

// ScheduleRestart restarts the process (graceful shutdown, then re-exec).
func (m *Manager) ScheduleRestart() error {
	if m.restart == nil {
		return apperr.BadRequest("restart is not supported in this mode")
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		m.restart()
	}()
	return nil
}

func checkGoPackage(dir string) error {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	if len(matches) == 0 {
		return errors.New("plugin contains no Go files in its root directory")
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		t := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(t, 0o750)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil // never follow symlinks from foreign sources
		}
		b, err := os.ReadFile(p) // #nosec G304 G122 -- plugin sources of the administrator; symlinks are skipped
		if err != nil {
			return err
		}
		return os.WriteFile(t, b, 0o600) // #nosec G703 -- target below the build directory (relative path from WalkDir)
	})
}

// Generate writes plugins/all/zz_generated.go importing every plugin
// directory that contains a plugin.json.
func Generate(root string) error {
	pdir := filepath.Join(root, "plugins")
	entries, err := os.ReadDir(pdir)
	if err != nil {
		return err
	}
	var imports []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "all" {
			continue
		}
		if _, err := ReadManifest(filepath.Join(pdir, e.Name())); err != nil {
			continue
		}
		if checkGoPackage(filepath.Join(pdir, e.Name())) != nil {
			continue
		}
		imports = append(imports, fmt.Sprintf("\t_ %q", ModulePath+"/plugins/"+e.Name()))
	}
	sort.Strings(imports)
	src := "// Code generated by \"ewiki plugin generate\"; DO NOT EDIT.\n\n" +
		"// Package all imports every installed plugin so it is compiled into the binary.\n" +
		"package all\n\nimport (\n" + strings.Join(imports, "\n") + "\n)\n"
	if err := os.MkdirAll(filepath.Join(pdir, "all"), 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pdir, "all", "zz_generated.go"), []byte(src), 0o600)
}
