// Command releaseage checks that every npm package version a change brings
// into web/package-lock.json was published at least -min ago (default 3
// days). Renovate waits this long before it proposes an update
// (minimumReleaseAge), but cannot work out the age for lock file
// maintenance and pinning; this check covers those pull requests (and every
// other change of the lock file), so that a freshly published, possibly
// compromised version never reaches the product.
//
//	go run ./tools/releaseage -old old-lock.json -new web/package-lock.json [-min 72h]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type lockFile struct {
	Packages map[string]struct {
		Name     string `json:"name"`
		Version  string `json:"version"`
		Resolved string `json:"resolved"`
		Link     bool   `json:"link"`
	} `json:"packages"`
}

// pkg is a package version from the public npm registry.
type pkg struct{ name, version string }

const registry = "https://registry.npmjs.org/"

// versions returns the registry package versions of a lock file.
func versions(path string) (map[pkg]bool, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path given by the caller (CI)
	if err != nil {
		return nil, err
	}
	var lf lockFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := map[pkg]bool{}
	for key, p := range lf.Packages {
		if key == "" || p.Link || !strings.HasPrefix(p.Resolved, registry) {
			continue // the project itself, links and packages from elsewhere
		}
		name := p.Name
		if name == "" {
			name = key[strings.LastIndex(key, "node_modules/")+len("node_modules/"):]
		}
		out[pkg{name, p.Version}] = true
	}
	return out, nil
}

// changed returns the versions in newer that are not in older, sorted.
func changed(older, newer map[pkg]bool) []pkg {
	var out []pkg
	for p := range newer {
		if !older[p] {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].name+"@"+out[i].version < out[j].name+"@"+out[j].version
	})
	return out
}

var client = &http.Client{Timeout: 30 * time.Second}

// published reads when a version was published.
func published(base string, p pkg) (time.Time, error) {
	res, err := client.Get(base + url.PathEscape(p.name)) // #nosec G107 -- registry address plus escaped package name
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("%s: HTTP %d", p.name, res.StatusCode)
	}
	var meta struct {
		Time map[string]time.Time `json:"time"`
	}
	if err := json.NewDecoder(res.Body).Decode(&meta); err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", p.name, err)
	}
	t, ok := meta.Time[p.version]
	if !ok {
		return time.Time{}, fmt.Errorf("%s@%s: no publication time in the registry", p.name, p.version)
	}
	return t, nil
}

// check returns the problems: versions younger than min (or of unknown age).
func check(base string, ps []pkg, min time.Duration, now time.Time) []string {
	var problems []string
	for _, p := range ps {
		t, err := published(base, p)
		switch {
		case err != nil:
			problems = append(problems, err.Error())
		case now.Sub(t) < min:
			problems = append(problems, fmt.Sprintf("%s@%s was published %s ago (%s), less than %s", p.name, p.version,
				now.Sub(t).Round(time.Hour), t.Format(time.RFC3339), min))
		}
	}
	return problems
}

func main() {
	oldPath := flag.String("old", "", "lock file before the change")
	newPath := flag.String("new", "web/package-lock.json", "lock file after the change")
	min := flag.Duration("min", 72*time.Hour, "minimum age of a new version")
	flag.Parse()
	if *oldPath == "" {
		fmt.Fprintln(os.Stderr, "releaseage: -old is required")
		os.Exit(2)
	}
	older, err := versions(*oldPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "releaseage:", err)
		os.Exit(1)
	}
	newer, err := versions(*newPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "releaseage:", err)
		os.Exit(1)
	}
	ps := changed(older, newer)
	problems := check(registry, ps, *min, time.Now())
	for _, p := range problems {
		fmt.Println("::error::" + p)
	}
	if len(problems) > 0 {
		fmt.Printf("releaseage: %d of %d new package versions are too young; the check passes once they are %s old\n", len(problems), len(ps), *min)
		os.Exit(1)
	}
	fmt.Printf("releaseage: %d new package versions, all at least %s old\n", len(ps), *min)
}
