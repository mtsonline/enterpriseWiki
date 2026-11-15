package server_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/actions"
	"github.com/mtsonline/enterprisewiki/internal/httpx"
)

// missingFile lists the routes that have no register entry yet (phase 2 of
// the action register plan fills it area by area). It may only shrink: a
// new route without an entry, or a listed route that has an entry or no
// longer exists, fails the test. EWIKI_ACTIONS_UPDATE=1 rewrites it.
const missingFile = "../actions/missing.txt"

// TestActionRegister is stage 1 of the parity check: the register and the
// routes of the server agree, every route names its action id in a comment,
// and every action has its ways (checked by actions.Validate).
func TestActionRegister(t *testing.T) {
	_ = setup(t) // registers all routes of the core and the core plugins
	all, err := actions.All()
	if err != nil {
		t.Fatalf("register:\n%v", err)
	}
	routes := httpx.Routes()
	byRoute := map[string]actions.Action{}
	for _, a := range all {
		byRoute[a.Route()] = a
		if !slices.Contains(routes, a.Route()) {
			t.Errorf("%s: %s: the server has no route %s", a.Source, a.ID, a.Route())
		}
	}

	// routes without an entry: only those still listed as missing
	var missing []string
	for _, r := range routes {
		if _, ok := byRoute[r]; !ok {
			missing = append(missing, r)
		}
	}
	if os.Getenv("EWIKI_ACTIONS_UPDATE") == "1" {
		header := "# Routes without an entry in the action register (phase 2 fills them area\n# by area; the list may only shrink). Rewritten with EWIKI_ACTIONS_UPDATE=1.\n"
		if err := os.WriteFile(missingFile, []byte(header+strings.Join(missing, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	listed := readMissing(t)
	for _, r := range missing {
		if !listed[r] {
			t.Errorf("route %s has no entry in the action register (internal/actions/core/*.yaml or plugins/<plugin>/actions.yaml)", r)
		}
	}
	for r := range listed {
		switch _, has := byRoute[r]; {
		case has:
			t.Errorf("%s has an entry now: remove it from internal/actions/missing.txt", r)
		case !slices.Contains(routes, r):
			t.Errorf("%s no longer exists: remove it from internal/actions/missing.txt", r)
		}
	}

	// every registered route names its action id in a comment (CLAUDE.md 4.6)
	comments := routeComments(t)
	for _, a := range all {
		id, found := comments[a.Route()]
		switch {
		case !found:
			t.Errorf("%s: %s: no route %s found in the source", a.Source, a.ID, a.Route())
		case id == "":
			t.Errorf("%s: route %s needs the comment // action: %s", a.ID, a.Route(), a.ID)
		case id != a.ID:
			t.Errorf("route %s: comment says %q, register says %q", a.Route(), id, a.ID)
		}
	}
	t.Logf("action register: %d entries, %d routes still without an entry", len(all), len(missing))
}

func readMissing(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open(missingFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" && !strings.HasPrefix(l, "#") {
			out[l] = true
		}
	}
	return out
}

var (
	coreRouteRe   = regexp.MustCompile(`(?:s\.handle|s\.wopiRoute|h\.RecordRoute|Mux\.HandleFunc)\("((?:GET|POST|PUT|PATCH|DELETE) /[^"]*)"`)
	pluginRouteRe = regexp.MustCompile(`\.Handle\("(GET|POST|PUT|PATCH|DELETE)", "/([^"]*)"`)
	actionRe      = regexp.MustCompile(`^\s*// action: (\S+)`)
)

// routeComments finds the routes in the source of the server and the core
// plugins, with the id of the "// action:" comment in the lines just above
// ("" if there is none).
func routeComments(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	scan := func(path, plugin string) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(b), "\n")
		for i, l := range lines {
			var route string
			if m := coreRouteRe.FindStringSubmatch(l); m != nil && plugin == "" {
				route = m[1]
			} else if m := pluginRouteRe.FindStringSubmatch(l); m != nil && plugin != "" {
				route = fmt.Sprintf("%s /api/v1/x/%s/%s", m[1], plugin, m[2])
			} else {
				continue
			}
			id := ""
			for j := i - 1; j >= 0 && j >= i-4; j-- {
				if m := actionRe.FindStringSubmatch(lines[j]); m != nil {
					id = m[1]
					break
				}
				if !strings.HasPrefix(strings.TrimSpace(lines[j]), "//") {
					break
				}
			}
			if prev, ok := out[route]; !ok || prev == "" {
				out[route] = id
			}
		}
	}
	walk := func(dir, plugin string) {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			scan(p, plugin)
			return nil
		})
	}
	walk(filepath.Join("..", "..", "internal"), "")
	dirs, _ := os.ReadDir(filepath.Join("..", "..", "plugins"))
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join("..", "..", "plugins", d.Name())
		walk(dir, pluginID(dir, d.Name()))
	}
	return out
}

// pluginID reads the id of a core plugin from its manifest (plugin.json).
func pluginID(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json")) // #nosec G304 -- files of this repository
	if err == nil {
		var m struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(b, &m) == nil && m.ID != "" {
			return m.ID
		}
	}
	return strings.ReplaceAll(name, "_", ".")
}
