// Command outdated checks that every component is on its newest stable
// version: Go toolchain, Go modules (direct), npm packages of the frontend
// and the Node.js LTS of the build. Renovate updates them automatically
// after a waiting period (minimumReleaseAge); anything that is still behind
// after the grace period means the automatic update is stuck (failing CI,
// unmerged pull request) and the command fails.
//
//	go run ./tools/outdated [-grace 7d]
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type finding struct {
	what, current, latest string
	since                 time.Time
}

var client = &http.Client{Timeout: 30 * time.Second}

func getJSON(u string, v any) error {
	res, err := client.Get(u) // #nosec G107 -- fixed registry addresses
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", u, res.Status)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(v)
}

func main() {
	grace := flag.Duration("grace", 7*24*time.Hour, "how long a newer version may be pending (Renovate waits 3 days)")
	flag.Parse()
	var found []finding
	var errs []string
	check := func(name string, fn func() ([]finding, error)) {
		f, err := fn()
		if err != nil {
			errs = append(errs, name+": "+err.Error())
		}
		found = append(found, f...)
	}
	check("go toolchain", goToolchain)
	check("go modules", goModules)
	check("npm", npmPackages)
	check("node", nodeLTS)

	sort.Slice(found, func(i, j int) bool { return found[i].what < found[j].what })
	late := 0
	for _, f := range found {
		age := time.Since(f.since).Round(time.Hour)
		mark := "pending"
		if f.since.IsZero() || age > *grace {
			mark = "OUTDATED"
			late++
		}
		fmt.Printf("%-9s %-45s %-22s -> %-22s (released %s ago)\n", mark, f.what, f.current, f.latest, age)
	}
	for _, e := range errs {
		fmt.Println("ERROR    ", e)
	}
	if late > 0 || len(errs) > 0 {
		fmt.Printf("\n%d component(s) behind for more than %s – check the Renovate pull requests and the dependency dashboard.\n", late, *grace)
		os.Exit(1)
	}
	fmt.Println("all components are on the newest stable version (or within the waiting period)")
}

// goToolchain compares the toolchain line of go.mod with the newest Go release.
func goToolchain() ([]finding, error) {
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		return nil, err
	}
	m := regexp.MustCompile(`(?m)^toolchain go(\S+)`).FindSubmatch(mod)
	if m == nil {
		return nil, fmt.Errorf("no toolchain line in go.mod")
	}
	cur := string(m[1])
	// the toolchain module of the Go proxy lists every release
	res, err := client.Get("https://proxy.golang.org/golang.org/toolchain/@v/list")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	stable := regexp.MustCompile(`^v0\.0\.1-go(\d+)\.(\d+)\.(\d+)\.linux-amd64$`)
	latest, best := "", [3]int{}
	for _, l := range strings.Fields(string(body)) {
		m := stable.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		var v [3]int
		for i := range v {
			v[i], _ = strconv.Atoi(m[i+1])
		}
		if v[0] > best[0] || v[0] == best[0] && (v[1] > best[1] || v[1] == best[1] && v[2] > best[2]) {
			best, latest = v, fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
		}
	}
	if latest == "" {
		return nil, fmt.Errorf("no stable Go release found")
	}
	if latest == cur {
		return nil, nil
	}
	var info struct{ Time time.Time }
	_ = getJSON("https://proxy.golang.org/golang.org/toolchain/@v/v0.0.1-go"+latest+".linux-amd64.info", &info)
	return []finding{{"go toolchain", cur, latest, info.Time}}, nil
}

// goModules lists direct module requirements with a newer version.
func goModules() ([]finding, error) {
	cmd := exec.Command("go", "list", "-u", "-m", "-json", "all")
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	var res []finding
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var m struct {
			Path     string
			Version  string
			Main     bool
			Indirect bool
			Update   *struct {
				Version string
				Time    time.Time
			}
		}
		if err := dec.Decode(&m); err != nil {
			return res, err
		}
		if m.Main || m.Indirect || m.Update == nil {
			continue
		}
		res = append(res, finding{"go " + m.Path, m.Version, m.Update.Version, m.Update.Time})
	}
	return res, nil
}

// npmPackages compares the installed versions (package-lock.json) of the
// dependencies in web/package.json with the newest release.
func npmPackages() ([]finding, error) {
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	for f, v := range map[string]any{"web/package.json": &pkg, "web/package-lock.json": &lock} {
		b, err := os.ReadFile(f) // #nosec G304 -- fixed file names of the repository
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, v); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
	}
	names := []string{}
	for n := range pkg.Dependencies {
		names = append(names, n)
	}
	for n := range pkg.DevDependencies {
		names = append(names, n)
	}
	sort.Strings(names)
	var res []finding
	for _, n := range names {
		cur := lock.Packages["node_modules/"+n].Version
		var meta struct {
			DistTags map[string]string    `json:"dist-tags"`
			Time     map[string]time.Time `json:"time"`
		}
		if err := getJSON("https://registry.npmjs.org/"+url.PathEscape(n), &meta); err != nil {
			return res, err
		}
		latest := meta.DistTags["latest"]
		if latest != "" && latest != cur {
			res = append(res, finding{"npm " + n, cur, latest, meta.Time[latest]})
		}
	}
	return res, nil
}

// nodeLTS compares the Node.js major of the Dockerfile with the newest LTS.
func nodeLTS() ([]finding, error) {
	f, err := os.Open("Dockerfile")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cur := ""
	re := regexp.MustCompile(`^FROM .*node:(\d+)`)
	for sc := bufio.NewScanner(f); sc.Scan(); {
		if m := re.FindStringSubmatch(sc.Text()); m != nil {
			cur = m[1]
			break
		}
	}
	var rel []struct {
		Version string `json:"version"`
		Date    string `json:"date"`
		LTS     any    `json:"lts"`
	}
	if err := getJSON("https://nodejs.org/dist/index.json", &rel); err != nil {
		return nil, err
	}
	for _, r := range rel {
		if b, ok := r.LTS.(bool); ok && !b {
			continue
		}
		major := strings.SplitN(strings.TrimPrefix(r.Version, "v"), ".", 2)[0]
		if major == cur {
			return nil, nil
		}
		// the first release of the newest LTS line counts as its date
		first := r.Date
		for _, o := range rel {
			if strings.HasPrefix(o.Version, "v"+major+".") && o.LTS != false {
				first = o.Date
			}
		}
		t, _ := time.Parse("2006-01-02", first)
		return []finding{{"node (Dockerfile, CI)", cur, major + " LTS", t}}, nil
	}
	return nil, fmt.Errorf("no LTS release found")
}
