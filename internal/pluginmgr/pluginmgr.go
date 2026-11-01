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
	"io/fs"
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
}

// BuildStatus reports the last build.
type BuildStatus struct {
	Running    bool       `json:"running"`
	OK         bool       `json:"ok"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Log        string     `json:"log"`
}

// New creates a manager. restart is called to restart the process.
func New(root string, cfg *config.Store, restart func()) *Manager {
	return &Manager{root: root, cfg: cfg, restart: restart}
}

// PluginsDir returns the plugin source directory.
func (m *Manager) PluginsDir() string { return filepath.Join(m.root, "plugins") }

// CanBuild reports whether the source tree and toolchains are available.
func (m *Manager) CanBuild() bool {
	if _, err := os.Stat(filepath.Join(m.root, "go.mod")); err != nil {
		return false
	}
	if _, err := exec.LookPath("go"); err != nil {
		return false
	}
	_, err := exec.LookPath("npm")
	return err == nil
}

// ReadManifest reads plugin.json of a plugin directory.
func ReadManifest(dir string) (*sdk.Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
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
	return &mf, nil
}

// DirName returns the directory name for a plugin id (core.html -> core_html).
func DirName(id string) string { return strings.NewReplacer(".", "_", "-", "_").Replace(id) }

// OnDisk lists the plugins present in the source tree.
func (m *Manager) OnDisk() (map[string]*sdk.Manifest, map[string]string, error) {
	entries, err := os.ReadDir(m.PluginsDir())
	if err != nil {
		return nil, nil, err
	}
	mans := map[string]*sdk.Manifest{}
	dirs := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "all" {
			continue
		}
		mf, err := ReadManifest(filepath.Join(m.PluginsDir(), e.Name()))
		if err != nil {
			continue
		}
		mans[mf.ID] = mf
		dirs[mf.ID] = e.Name()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	gitURL, local, ref, err := m.resolve(source, ref)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "ewiki-plugin-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
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
	if err := Generate(m.root); err != nil {
		return nil, err
	}
	return res, nil
}

func (m *Manager) backup(dirName string) error {
	bdir := filepath.Join(m.PluginsDir(), ".backup")
	if err := os.MkdirAll(bdir, 0o755); err != nil {
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
	return Generate(m.root)
}

// StartBuild runs scripts/build.sh in the background. On success the new
// binary replaces the running executable; call ScheduleRestart to activate it.
func (m *Manager) StartBuild(_ context.Context) (*BuildStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.build.Running {
		return nil, apperr.Conflict("a build is already running")
	}
	if !m.CanBuild() {
		return nil, apperr.BadRequest("building requires the source tree with go.mod and the go and npm toolchains on this server")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	m.build = BuildStatus{Running: true, StartedAt: &now}
	go m.runBuild(exe)
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

func (m *Manager) runBuild(exe string) {
	out := exe + ".new"
	var buf lockedBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(m.root, "scripts", "build.sh"))
	cmd.Dir = m.root
	cmd.Env = append(os.Environ(), "EWIKI_BUILD_OUT="+out)
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	if err == nil {
		// verify the new binary starts
		if vout, verr := exec.Command(out, "version").CombinedOutput(); verr != nil {
			err = fmt.Errorf("new binary failed self-test: %v: %s", verr, vout)
		} else {
			err = os.Rename(out, exe)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.build.Running, m.build.FinishedAt, m.build.OK = false, &now, err == nil
	log := buf.b.String()
	if err != nil {
		log += "\nBUILD FAILED: " + err.Error()
	} else {
		log += "\nBuild successful. Restart to activate the new version."
	}
	if len(log) > 20000 {
		log = log[len(log)-20000:]
	}
	m.build.Log = log
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
			return os.MkdirAll(t, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil // never follow symlinks from foreign sources
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(t, b, 0o644)
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
	if err := os.MkdirAll(filepath.Join(pdir, "all"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pdir, "all", "zz_generated.go"), []byte(src), 0o644)
}
