// Command uicoverage checks that the browser test uses every action that has
// a way in the interface. The browser test records the API requests the
// pages send (method, path, status); this command maps them to actions with
// the same patterns the server registers and compares the result with the
// register.
//
// An action counts as covered when the interface got a successful answer
// (status below 400) at least once: a refusal does not show that the
// function works. Actions not covered yet stay in
// internal/actions/ui-missing.txt; that list may only shrink.
//
//	go run ./tools/uicoverage -requests web/ui-requests.json
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/actions"
	_ "github.com/mtsonline/enterprisewiki/plugins/all" // register files of the core plugins
)

// Request is one API request recorded by the browser test.
type Request struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
}

func main() {
	reqFile := flag.String("requests", "web/ui-requests.json", "requests recorded by the browser test")
	missingFile := flag.String("missing", "internal/actions/ui-missing.txt", "actions not covered yet")
	write := flag.Bool("write", false, "write the missing list with the actions not covered (creates it, afterwards only shrinks it)")
	flag.Parse()
	if err := run(*reqFile, *missingFile, *write, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "uicoverage:", err)
		os.Exit(1)
	}
}

func run(reqFile, missingFile string, write bool, out io.Writer) error {
	all, err := actions.All()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(reqFile) // #nosec G304 -- path given by the developer or the CI
	if err != nil {
		return err
	}
	var reqs []Request
	if err := json.Unmarshal(b, &reqs); err != nil {
		return fmt.Errorf("%s: %w", reqFile, err)
	}
	if len(reqs) == 0 {
		return errors.New("no requests recorded: did the browser test run completely?")
	}
	covered, unknown, err := Cover(all, reqs)
	if err != nil {
		return err
	}
	missing, err := readList(missingFile)
	if err != nil {
		return err
	}
	var problems []string
	for _, u := range unknown {
		problems = append(problems, "the interface sent a request no action describes: "+u)
	}
	ids := map[string]bool{}
	var notCovered []string
	for _, a := range all {
		ids[a.ID] = true
		if !NeedsUI(a) {
			if missing[a.ID] {
				problems = append(problems, a.ID+" has no way in the interface: remove it from "+missingFile)
			}
			continue
		}
		switch {
		case covered[a.ID] && missing[a.ID]:
			problems = append(problems, a.ID+" is covered now: remove it from "+missingFile)
		case !covered[a.ID] && !missing[a.ID]:
			problems = append(problems, a.ID+" is never used successfully by the browser test (entry "+a.UI.Entry+")")
		}
		if !covered[a.ID] {
			notCovered = append(notCovered, a.ID)
		}
	}
	for id := range missing {
		if !ids[id] {
			problems = append(problems, id+" in "+missingFile+" is not in the register")
		}
	}
	need := 0
	for _, a := range all {
		if NeedsUI(a) {
			need++
		}
	}
	fmt.Fprintf(out, "interface coverage: %d of %d actions used successfully by the browser test\n", need-len(notCovered), need)
	if write {
		if _, err := os.Stat(missingFile); err == nil && len(notCovered) > len(missing) {
			return fmt.Errorf("-write only shrinks the list (%d now, %d not covered)", len(missing), len(notCovered))
		}
		return writeList(missingFile, notCovered)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return errors.New(strings.Join(problems, "\n  "))
	}
	return nil
}

// NeedsUI reports whether the action must be reachable in the interface.
func NeedsUI(a actions.Action) bool {
	return a.UI != nil && !a.HasException("WAY_UI")
}

// Cover maps the requests to actions with the patterns the server
// registers. It returns the actions with a successful answer and the
// requests that match no action.
func Cover(all []actions.Action, reqs []Request) (map[string]bool, []string, error) {
	mux := http.NewServeMux()
	byPattern := map[string]string{}
	for _, a := range all {
		p := a.Route()
		if _, dup := byPattern[p]; dup {
			continue // several actions on one route (documented by the register check)
		}
		byPattern[p] = a.ID
		if err := register(mux, p); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", a.ID, err)
		}
	}
	covered := map[string]bool{}
	unknownSet := map[string]bool{}
	for _, r := range reqs {
		hr := httptest.NewRequest(r.Method, r.Path, nil)
		_, pattern := mux.Handler(hr)
		id, ok := byPattern[pattern]
		if !ok {
			unknownSet[r.Method+" "+r.Path] = true
			continue
		}
		if r.Status > 0 && r.Status < 400 {
			covered[id] = true
		}
	}
	unknown := make([]string, 0, len(unknownSet))
	for u := range unknownSet {
		unknown = append(unknown, u)
	}
	slices.Sort(unknown)
	return covered, unknown, nil
}

// register adds a pattern; ServeMux panics on conflicts, which become errors.
func register(mux *http.ServeMux, pattern string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pattern %q: %v", pattern, r)
		}
	}()
	mux.HandleFunc(pattern, func(http.ResponseWriter, *http.Request) {})
	return nil
}

func readList(path string) (map[string]bool, error) {
	f, err := os.Open(path) // #nosec G304 -- path given by the developer or the CI
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]bool{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = true
	}
	return out, s.Err()
}

func writeList(path string, ids []string) error {
	slices.Sort(ids)
	var b strings.Builder
	b.WriteString("# Actions with a way in the interface that the browser test does not use\n")
	b.WriteString("# successfully yet. Checked by tools/uicoverage; this list may only shrink.\n")
	for _, id := range ids {
		b.WriteString(id + "\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
