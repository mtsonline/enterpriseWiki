package server_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/internal/server"

	"github.com/mtsonline/enterprisewiki/internal/actions"
	"github.com/mtsonline/enterprisewiki/internal/httpx"
)

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
		checkActionPermission(t, a)
		if !slices.Contains(routes, a.Route()) {
			t.Errorf("%s: %s: the server has no route %s", a.Source, a.ID, a.Route())
		}
	}

	// every route has an entry
	missing := 0
	for _, r := range routes {
		if _, ok := byRoute[r]; !ok {
			missing++
			t.Errorf("route %s has no entry in the action register (internal/actions/core/*.yaml or plugins/<plugin>/actions.yaml)", r)
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
	// every route in the source is recorded (h.RecordRoute), so none escapes
	// the register: a route on the mux alone would not be listed above
	for r := range comments {
		if !slices.Contains(routes, r) {
			t.Errorf("route %s is in the source but not recorded: register it with s.handle, host.Handle or h.RecordRoute", r)
		}
	}
	checkAPIDoc(t, all)
	t.Logf("action register: %d entries, %d routes without an entry", len(all), missing)
}

var (
	coreRouteRe   = regexp.MustCompile(`(?:s\.handle\(|s\.wopiRoute\(|h\.RecordRoute\(|Mux\.HandleFunc\(|^\s*)"((?:GET|POST|PUT|PATCH|DELETE) /[^"]*)"`)
	pluginRouteRe = regexp.MustCompile(`\.Handle\("(GET|POST|PUT|PATCH|DELETE)", "/([^"]*)"`)
	actionRe      = regexp.MustCompile(`^\s*// action: (\S+)`)
	// a route that a shared helper registers names its action where the
	// helper is called: "// action: <id> for <METHOD> <path>" (path as in
	// host.Handle for a plugin)
	actionForRe = regexp.MustCompile(`^\s*// action: (\S+) for ((?:GET|POST|PUT|PATCH|DELETE) /\S*)$`)
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
			if m := actionForRe.FindStringSubmatch(l); m != nil {
				route := m[2]
				if plugin != "" {
					method, path, _ := strings.Cut(route, " ")
					route = method + " /api/v1/x/" + plugin + path
				}
				out[route] = m[1]
				continue
			}
			var route string
			if m := coreRouteRe.FindStringSubmatch(l); m != nil && (plugin == "" || strings.Contains(m[1], " /api/v1/x/"+plugin+"/")) {
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

// checkActionPermission checks that a global permission of the register
// exists, and that it is the one the server checks for an administrative
// route (adminPerms).
func checkActionPermission(t *testing.T, a actions.Action) {
	t.Helper()
	p := a.Permission
	if p == nil || p.Scope != "SCOPE_GLOBAL" {
		return
	}
	if _, ok := perm.Lookup(p.Key); !ok && p.Key != "core.admin" {
		t.Errorf("%s: %s: permission %q does not exist", a.Source, a.ID, p.Key)
	}
	if a.Plugin != "" {
		return
	}
	if want := server.RoutePerm(a.Route()); want != "" && want != p.Key {
		t.Errorf("%s: %s: permission %q, but the server checks %q (adminPerms)", a.Source, a.ID, p.Key, want)
	}
}

var apiDocRowRe = regexp.MustCompile("^\\| `(GET|POST|PUT|PATCH|DELETE)` \\| `([^`]+)` \\|")

// checkAPIDoc compares the endpoint tables of the API reference (German and
// English) with the register: every action is listed once, nothing else.
func checkAPIDoc(t *testing.T, all []actions.Action) {
	t.Helper()
	want := map[string]bool{}
	for _, a := range all {
		want[a.Route()] = true
	}
	for _, lang := range []string{"de", "en"} {
		path := filepath.Join("..", "..", "docs", lang, "developer", "api.md")
		b, err := os.ReadFile(path) // #nosec G304 -- file of this repository
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, l := range strings.Split(string(b), "\n") {
			m := apiDocRowRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			r := m[1] + " " + m[2]
			switch {
			case seen[r]:
				t.Errorf("%s: %s is listed twice", path, r)
			case !want[r]:
				t.Errorf("%s: %s is not in the action register", path, r)
			}
			seen[r] = true
		}
		for r := range want {
			if !seen[r] {
				t.Errorf("%s: %s is missing in the endpoint tables", path, r)
			}
		}
	}
}
