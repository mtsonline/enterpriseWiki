// Command testshard splits the tests of a package into shards of about the
// same running time, so the CI runs them in parallel jobs.
//
//	go run ./tools/testshard -shard 2 -of 3 ./internal/server
//
// prints a -run pattern with the tests of shard 2 (counted from 1). Each
// test is in exactly one shard. The running times come from durations.txt
// (go run ./tools/testshard -record < go-test-json-output); a test that is
// not listed counts as one second.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

func main() {
	shard := flag.Int("shard", 1, "the shard to print (1..of)")
	of := flag.Int("of", 1, "number of shards")
	durations := flag.String("durations", "tools/testshard/durations.txt", "file with the running time of each test (name seconds)")
	record := flag.Bool("record", false, "read go test -json from stdin and write the durations file")
	flag.Parse()
	if *record {
		if err := recordDurations(os.Stdin, *durations); err != nil {
			fail(err)
		}
		return
	}
	if flag.NArg() != 1 || *of < 1 || *shard < 1 || *shard > *of {
		fail(fmt.Errorf("usage: testshard -shard N -of M PACKAGE"))
	}
	out, err := exec.Command("go", "test", "-list", "^Test", flag.Arg(0)).Output() // #nosec G204 -- package from the command line of the CI
	if err != nil {
		fail(fmt.Errorf("go test -list: %w", err))
	}
	var tests []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "Test") {
			tests = append(tests, strings.TrimSpace(l))
		}
	}
	times, err := readDurations(*durations)
	if err != nil {
		fail(err)
	}
	groups := split(tests, times, *of)
	fmt.Print("^(" + strings.Join(groups[*shard-1], "|") + ")$")
}

// split assigns the tests to n shards: the longest first, each to the
// shard with the least time so far (ties: the lower shard).
func split(tests []string, times map[string]float64, n int) [][]string {
	sorted := append([]string(nil), tests...)
	dur := func(t string) float64 {
		if d, ok := times[t]; ok {
			return d
		}
		return 1
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if dur(sorted[i]) != dur(sorted[j]) {
			return dur(sorted[i]) > dur(sorted[j])
		}
		return sorted[i] < sorted[j]
	})
	groups := make([][]string, n)
	load := make([]float64, n)
	for _, t := range sorted {
		best := 0
		for i := 1; i < n; i++ {
			if load[i] < load[best] {
				best = i
			}
		}
		groups[best] = append(groups[best], t)
		load[best] += dur(t)
	}
	for i := range groups {
		sort.Strings(groups[i])
		if len(groups[i]) == 0 {
			// a pattern that matches no test (go test then reports "no tests to run")
			groups[i] = []string{"TestNoTestInThisShard"}
		}
	}
	return groups
}

func readDurations(path string) (map[string]float64, error) {
	times := map[string]float64{}
	f, err := os.Open(path) // #nosec G304 -- file of the repository
	if os.IsNotExist(err) {
		return times, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, secs, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !ok || strings.HasPrefix(name, "#") {
			continue
		}
		if d, err := strconv.ParseFloat(secs, 64); err == nil {
			times[name] = d
		}
	}
	return times, sc.Err()
}

func recordDurations(r io.Reader, path string) error {
	times := map[string]float64{}
	dec := json.NewDecoder(r)
	for {
		var ev struct {
			Action  string
			Test    string
			Elapsed float64
		}
		if err := dec.Decode(&ev); err == io.EOF {
			break
		} else if err != nil {
			continue
		}
		if ev.Test != "" && !strings.Contains(ev.Test, "/") && (ev.Action == "pass" || ev.Action == "fail") {
			times[ev.Test] = ev.Elapsed
		}
	}
	names := make([]string, 0, len(times))
	for n := range times {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("# running time of each test in seconds (race detector), for tools/testshard\n")
	for _, n := range names {
		fmt.Fprintf(&b, "%s %.1f\n", n, times[n])
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "testshard:", err)
	os.Exit(1)
}
