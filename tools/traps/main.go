// Command traps checks the tests themselves: it builds a deliberate bug into
// a security-critical place (traps.go), runs the tests responsible and
// expects them to fail. A trap the tests do not catch is a finding: those
// tests are green although they do not check what they claim.
//
// Run before every release, from the root of the repository, with a test
// database (the API integration tests need it):
//
//	EWIKI_TEST_DATABASE_URL=postgres://… go run ./tools/traps
//	go run ./tools/traps -list            # the traps
//	go run ./tools/traps -only cli.as,auth.csrf
//
// First the tests run unchanged and must pass (otherwise nothing can be
// judged). Every changed file is restored after its trap, also on Ctrl-C;
// files with uncommitted changes are refused, so nothing of yours is lost.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"
)

func main() {
	list := flag.Bool("list", false, "list the traps and exit")
	only := flag.String("only", "", "comma-separated ids of the traps to run")
	timeout := flag.Duration("timeout", 15*time.Minute, "time limit of one test run")
	flag.Parse()

	chosen := traps
	if *only != "" {
		want := map[string]bool{}
		for _, id := range strings.Split(*only, ",") {
			want[strings.TrimSpace(id)] = true
		}
		chosen = nil
		for _, t := range traps {
			if want[t.ID] {
				chosen = append(chosen, t)
				delete(want, t.ID)
			}
		}
		if len(want) > 0 {
			fail("unknown traps: %v", keys(want))
		}
	}
	if *list {
		for _, t := range chosen {
			fmt.Printf("%-32s %s\n%-32s %s  %s %s\n", t.ID, t.About, "", t.File, t.Pkg, t.Tests)
		}
		return
	}
	if os.Getenv("EWIKI_TEST_DATABASE_URL") == "" {
		fail("EWIKI_TEST_DATABASE_URL is not set: the API integration tests would be skipped and every trap would look caught")
	}

	// every trap must still fit the code, and its file must be unchanged
	outdated := 0
	files := map[string]bool{}
	for _, t := range chosen {
		src, err := os.ReadFile(t.File)
		if err != nil {
			fmt.Printf("OUTDATED  %-32s %v\n", t.ID, err)
			outdated++
			continue
		}
		if n := bytes.Count(src, []byte(t.Old)); n != 1 {
			fmt.Printf("OUTDATED  %-32s the guarded code occurs %d times in %s (want exactly once): adjust the trap\n", t.ID, n, t.File)
			outdated++
		}
		files[t.File] = true
	}
	if outdated > 0 {
		fail("%d trap(s) outdated", outdated)
	}
	for f := range files {
		if out, err := exec.Command("git", "status", "--porcelain", "--", f).Output(); err != nil || len(bytes.TrimSpace(out)) > 0 { // #nosec G204 -- f is a file of the repository named in traps.go
			fail("%s has uncommitted changes (or git failed): commit or stash them first", f)
		}
	}

	// restore on Ctrl-C
	var current struct {
		file string
		src  []byte
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		if current.file != "" {
			_ = os.WriteFile(current.file, current.src, 0o600) // #nosec G703 -- a file of the repository named in traps.go
			fmt.Fprintf(os.Stderr, "\nrestored %s\n", current.file)
		}
		os.Exit(130)
	}()

	// the tests must pass without traps
	type run struct{ pkg, tests string }
	baseline := map[run]bool{}
	for _, t := range chosen {
		baseline[run{t.Pkg, t.Tests}] = true
	}
	fmt.Printf("baseline: %d test run(s) without traps\n", len(baseline))
	for r := range baseline {
		if ok, out := goTest(r.pkg, r.tests, *timeout); !ok {
			fmt.Print(tail(out, 40))
			fail("the tests %s in %s fail without any trap: nothing can be judged", r.tests, r.pkg)
		}
	}

	missed := 0
	for _, t := range chosen {
		src, _ := os.ReadFile(t.File)
		mode := os.FileMode(0o644)
		if st, err := os.Stat(t.File); err == nil {
			mode = st.Mode().Perm()
		}
		current.file, current.src = t.File, src
		if err := os.WriteFile(t.File, bytes.Replace(src, []byte(t.Old), []byte(t.New), 1), mode); err != nil { // #nosec G703 -- a file of the repository named in traps.go
			fail("%s: %v", t.ID, err)
		}
		ok, out := goTest(t.Pkg, t.Tests, *timeout)
		if err := os.WriteFile(t.File, src, mode); err != nil { // #nosec G703 -- a file of the repository named in traps.go
			fail("could not restore %s: %v (restore it with git checkout)", t.File, err)
		}
		current.file = ""
		switch {
		case !ok && strings.Contains(out, "--- FAIL"):
			fmt.Printf("caught    %-32s %s\n", t.ID, firstFail(out))
		case !ok:
			fmt.Printf("BROKEN    %-32s the trap does not compile or the run did not test anything:\n%s", t.ID, tail(out, 15))
			missed++
		default:
			fmt.Printf("MISSED    %-32s %s – the tests %s stay green\n", t.ID, t.About, t.Tests)
			missed++
		}
	}
	if missed > 0 {
		fail("%d of %d trap(s) not caught: the tests do not check what they claim", missed, len(chosen))
	}
	fmt.Printf("traps: all %d caught\n", len(chosen))
}

// goTest runs the tests and reports whether they passed.
func goTest(pkg, pattern string, timeout time.Duration) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-run", pattern, pkg) // #nosec G204 -- package and pattern come from traps.go
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return err == nil, out.String()
}

// firstFail returns the first failing test and its message.
func firstFail(out string) string {
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "--- FAIL") {
			msg := ""
			if i+1 < len(lines) {
				msg = strings.TrimSpace(lines[i+1])
			}
			if len(msg) > 120 {
				msg = msg[:120] + "…"
			}
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "---")) + "  " + msg
		}
	}
	return ""
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "traps: "+format+"\n", a...)
	os.Exit(1)
}
