package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLock(t *testing.T, pkgs map[string]map[string]any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"packages": pkgs})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "package-lock.json")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func entry(name, version string) map[string]any {
	return map[string]any{"version": version, "resolved": registry + name + "/-/x-" + version + ".tgz"}
}

func TestChangedVersions(t *testing.T) {
	older, err := versions(writeLock(t, map[string]map[string]any{
		"":                              {"name": "web"},
		"node_modules/a":                entry("a", "1.0.0"),
		"node_modules/@s/b":             entry("@s/b", "2.0.0"),
		"node_modules/a/node_modules/c": entry("c", "1.0.0"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	newer, err := versions(writeLock(t, map[string]map[string]any{
		"":                              {"name": "web"},
		"node_modules/a":                entry("a", "1.0.0"),    // unchanged
		"node_modules/@s/b":             entry("@s/b", "2.1.0"), // updated, scoped
		"node_modules/d":                entry("d", "0.1.0"),    // new
		"node_modules/x/node_modules/c": entry("c", "1.0.0"),    // moved, same version
		"node_modules/l":                {"link": true, "version": "9.9.9"},
		"node_modules/g":                {"version": "1.0.0", "resolved": "git+https://example.com/g.git"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	got := changed(older, newer)
	if len(got) != 2 || got[0] != (pkg{"@s/b", "2.1.0"}) || got[1] != (pkg{"d", "0.1.0"}) {
		t.Fatalf("changed: %+v", got)
	}
}

func TestCheckAge(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/@s%2Fb":
			_ = json.NewEncoder(w).Encode(map[string]any{"time": map[string]string{"2.1.0": now.Add(-30 * time.Hour).Format(time.RFC3339)}})
		case "/d":
			_ = json.NewEncoder(w).Encode(map[string]any{"time": map[string]string{"0.1.0": now.Add(-100 * time.Hour).Format(time.RFC3339)}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	problems := check(srv.URL+"/", []pkg{{"@s/b", "2.1.0"}, {"d", "0.1.0"}, {"gone", "1.0.0"}, {"d", "9.9.9"}}, 72*time.Hour, now)
	if len(problems) != 3 || !strings.Contains(problems[0], "@s/b@2.1.0 was published 30h0m0s ago") ||
		!strings.Contains(problems[1], "gone") || !strings.Contains(problems[2], "d@9.9.9: no publication time") {
		t.Fatalf("problems: %q", problems)
	}
}

// TestRealRegistry asks the real npm registry (as the CI does).
func TestRealRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the npm registry")
	}
	old := []pkg{{"left-pad", "1.3.0"}} // published 2018
	if p := check(registry, old, 72*time.Hour, time.Now()); len(p) != 0 {
		t.Fatalf("old version refused: %q", p)
	}
	if p := check(registry, old, 100*365*24*time.Hour, time.Now()); len(p) != 1 {
		t.Fatalf("too young version accepted: %q", p)
	}
}
