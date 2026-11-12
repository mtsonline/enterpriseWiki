package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StaticResult is one static scan (written to static.json by "qa static").
type StaticResult struct {
	Tool     string `json:"tool"`
	What     string `json:"what"`
	Ran      bool   `json:"ran"`
	Findings int    `json:"findings"`
	Summary  string `json:"summary"`
	Output   string `json:"output"` // last lines
}

// static runs the scanners of the CI against the repository.
func static(repo, out string) error {
	run := func(dir string, timeout time.Duration, name string, args ...string) (string, int, bool) {
		if _, err := exec.LookPath(name); err != nil {
			return name + " is not installed", -1, false
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed tools
		cmd.Dir = dir
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, &buf
		err := cmd.Run()
		code := 0
		if err != nil {
			code = 1
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
		}
		return buf.String(), code, true
	}
	tail := func(s string, n int) string {
		l := strings.Split(strings.TrimSpace(s), "\n")
		if len(l) > n {
			l = l[len(l)-n:]
		}
		return strings.Join(l, "\n")
	}
	var res []StaticResult
	// govulncheck: known vulnerabilities in code that is called
	o, code, ran := run(repo, 15*time.Minute, "govulncheck", "./...")
	r := StaticResult{Tool: "govulncheck", What: "known vulnerabilities in Go and the modules the code calls", Ran: ran, Output: tail(o, 25)}
	if ran && code != 0 && code != 3 {
		r.Ran, r.Summary = false, fmt.Sprintf("failed with exit code %d (built with another Go version?): ", code)
	} else if ran {
		allow, _ := os.ReadFile(filepath.Join(repo, ".govulncheck-allow")) // #nosec G304 G703 -- file of the repository given on the command line
		var ids []string
		open := 0
		for _, l := range strings.Split(o, "\n") {
			if !strings.HasPrefix(l, "Vulnerability #") {
				continue
			}
			id := strings.TrimSpace(l[strings.Index(l, ":")+1:])
			note := "not in .govulncheck-allow"
			for _, al := range strings.Split(string(allow), "\n") {
				if f := strings.Fields(al); len(f) >= 2 && f[0] == id {
					note = "mitigated in the code, accepted until " + f[1] + " (.govulncheck-allow)"
					if f[1] < time.Now().Format("2006-01-02") {
						note = "acceptance expired on " + f[1]
					}
				}
			}
			if !strings.HasPrefix(note, "mitigated") {
				open++
			}
			ids = append(ids, id+": "+note)
		}
		r.Findings = open
		r.Summary = fmt.Sprintf("%d vulnerabilities in called code, %d of them open", len(ids), open)
		if len(ids) > 0 {
			r.Summary += " (" + strings.Join(ids, "; ") + ")"
		}
	}
	res = append(res, r)
	// gosec: insecure patterns
	o, code, ran = run(repo, 15*time.Minute, "gosec", "-fmt", "json", "-exclude-dir", "tests", "./...")
	r = StaticResult{Tool: "gosec", What: "insecure code patterns (all rules)", Ran: ran}
	if ran {
		var g struct {
			Issues []struct {
				Severity string `json:"severity"`
				RuleID   string `json:"rule_id"`
				Details  string `json:"details"`
				File     string `json:"file"`
				Line     string `json:"line"`
			} `json:"Issues"`
			Stats struct {
				Files int `json:"files"`
				Lines int `json:"lines"`
				Nosec int `json:"nosec"`
				Found int `json:"found"`
			} `json:"Stats"`
		}
		if i := strings.Index(o, "{"); i >= 0 && json.Unmarshal([]byte(o[i:]), &g) == nil {
			r.Findings = g.Stats.Found
			r.Summary = fmt.Sprintf("%d files, %d lines, %d issues, %d documented exceptions (#nosec)", g.Stats.Files, g.Stats.Lines, g.Stats.Found, g.Stats.Nosec)
			for i, is := range g.Issues {
				if i < 15 {
					r.Output += fmt.Sprintf("%s %s %s:%s %s\n", is.Severity, is.RuleID, strings.TrimPrefix(is.File, repo+"/"), is.Line, is.Details)
				}
			}
		} else {
			r.Summary, r.Output = fmt.Sprintf("exit code %d", code), tail(o, 15)
		}
	}
	res = append(res, r)
	// npm audit: frontend and build tools
	o, code, ran = run(filepath.Join(repo, "web"), 5*time.Minute, "npm", "audit", "--json")
	r = StaticResult{Tool: "npm audit", What: "known vulnerabilities in the npm packages (frontend and build tools)", Ran: ran}
	if ran {
		var a struct {
			Metadata struct {
				Vulnerabilities map[string]int `json:"vulnerabilities"`
				Dependencies    map[string]int `json:"dependencies"`
			} `json:"metadata"`
		}
		if json.Unmarshal([]byte(o), &a) == nil && a.Metadata.Vulnerabilities != nil {
			v := a.Metadata.Vulnerabilities
			r.Findings = v["total"]
			r.Summary = fmt.Sprintf("%d packages: %d critical, %d high, %d moderate, %d low", a.Metadata.Dependencies["total"], v["critical"], v["high"], v["moderate"], v["low"])
		} else {
			r.Summary, r.Output = fmt.Sprintf("exit code %d", code), tail(o, 10)
		}
	}
	res = append(res, r)
	// go vet as a baseline
	o, code, ran = run(repo, 10*time.Minute, "go", "vet", "./...")
	vet := StaticResult{Tool: "go vet", What: "suspicious constructs", Ran: ran, Summary: "no findings"}
	if code != 0 {
		vet.Findings, vet.Summary, vet.Output = strings.Count(o, ".go:"), fmt.Sprintf("exit code %d", code), tail(o, 10)
	}
	res = append(res, vet)
	for _, r := range res {
		fmt.Printf("static: %-12s %s\n", r.Tool, r.Summary)
	}
	return writeJSON(out, "static.json", res)
}

var sevOrder = map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "info": 4}

func mdEscape(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// report writes report.md from the result files of a run.
func report(dir, title, version string) error {
	var fn FunctionalResult
	var pt PentestResult
	var ld LoadResult
	var st []StaticResult
	hasFn := readJSON(dir, "functional.json", &fn) == nil
	hasPt := readJSON(dir, "pentest.json", &pt) == nil
	hasLd := readJSON(dir, "load.json", &ld) == nil
	hasSt := readJSON(dir, "static.json", &st) == nil
	if !hasFn && !hasPt && !hasLd {
		return fmt.Errorf("no result files in %s", dir)
	}
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("# %s – test report", title)
	w("")
	w("| | |\n|---|---|")
	if version != "" {
		w("| Version under test | %s |", version)
	}
	w("| Report created | %s |", time.Now().Format("2006-01-02 15:04 MST"))
	if hasLd {
		w("| Test machine | %s (wiki, database and load generator together) |", ld.Machine)
	}
	w("| Test routine | `tests/qa` of the repository (this report is generated by `qa report`) |")
	w("| Methods | OWASP Web Security Testing Guide v4.2, OWASP ASVS 4.0.3, OWASP API Security Top 10 2023, CVSS 3.1, ISO/IEC 25010 (performance efficiency, reliability), ISO/IEC/IEEE 29119 (test documentation) |")
	w("")
	w("> This is an automated internal test. It does not replace a penetration test by an independent, accredited body, which certifications (ISO/IEC 27001, SOC 2, BSI) ask for.")
	w("")

	// ---- summary
	w("## 1 Summary")
	w("")
	w("| Test | Result |\n|---|---|")
	if hasFn {
		w("| Functional test, %d users at the same time | %d of %d checks passed, %d requests, %d failed requests |", fn.PeakUsers, fn.Passed, fn.Passed+fn.Failed, fn.Requests, fn.Total.Errors)
	}
	if hasPt {
		c := pt.Counts
		w("| Penetration test | %d checks: %d passed, %d failed (%d critical, %d high, %d medium, %d low), %d notes, %d not tested |", len(pt.Findings), c["pass"], c["fail"], c["fail:critical"], c["fail:high"], c["fail:medium"], c["fail:low"]+c["fail:info"], c["info"], c["skipped"])
	}
	if hasLd && len(ld.Stages) > 0 {
		last := ld.Stages[len(ld.Stages)-1]
		w("| Load test | up to %d users at the same time; largest stage within the targets: %d users; at %d users %.0f requests/s, reading p95 %.0f ms, errors %.2f %% |", last.Users, ld.Capacity, last.Users, last.RPS, last.Read.P95, last.ErrorRate*100)
	}
	if hasSt {
		parts := []string{}
		for _, s := range st {
			if s.Ran {
				parts = append(parts, fmt.Sprintf("%s: %d", s.Tool, s.Findings))
			}
		}
		w("| Static scans | findings: %s |", strings.Join(parts, ", "))
	}
	w("")

	// ---- pentest
	if hasPt {
		fails := []Finding{}
		for _, f := range pt.Findings {
			if f.Result == "fail" {
				fails = append(fails, f)
			}
		}
		sort.SliceStable(fails, func(i, j int) bool {
			if sevOrder[fails[i].Severity] != sevOrder[fails[j].Severity] {
				return sevOrder[fails[i].Severity] < sevOrder[fails[j].Severity]
			}
			return fails[i].Score > fails[j].Score
		})
		w("## 2 Penetration test")
		w("")
		w("Target %s, %s, %d requests in %.0f s. Grey box: the test knows the routes (docs/developer/api.md) and uses an administrator, two editors, a reader and a guest.", pt.Target, pt.Started.Format("2006-01-02 15:04"), pt.Requests, pt.Duration)
		w("")
		w("### 2.1 Findings")
		w("")
		if len(fails) == 0 {
			w("No check failed.")
		} else {
			w("Each finding is a check that failed; its title says what should hold.")
			w("")
			w("| # | Severity | CVSS 3.1 | Not met | Check |\n|---|---|---|---|---|")
			for i, f := range fails {
				w("| %d | %s | %.1f | %s | `%s` |", i+1, f.Severity, f.Score, mdEscape(f.Title), f.ID)
			}
			w("")
			for i, f := range fails {
				w("#### %d. Not met: %s", i+1, f.Title)
				w("")
				w("- **Severity:** %s", f.Severity)
				if f.Vector != "" {
					w("- **CVSS 3.1:** %.1f (`%s`)", f.Score, f.Vector)
				}
				w("- **References:** %s", strings.Join(f.Refs, "; "))
				w("- **Observed:** %s", f.Detail)
				w("- **Recommendation:** %s", f.Fix)
				w("")
			}
		}
		w("### 2.2 Notes (no failure, for hardening)")
		w("")
		for _, f := range pt.Findings {
			if f.Result == "info" {
				w("- **%s** (`%s`): %s", f.Title, f.ID, f.Detail)
			}
		}
		w("")
		w("### 2.3 All checks")
		w("")
		w("| Area | Check | Result | References |\n|---|---|---|---|")
		for _, f := range pt.Findings {
			w("| %s | %s (`%s`) | %s | %s |", f.Area, mdEscape(f.Title), f.ID, f.Result, mdEscape(strings.Join(f.Refs, "; ")))
		}
		w("")
		w("### 2.4 Evidence of the passed checks")
		w("")
		for _, f := range pt.Findings {
			if f.Result == "pass" || f.Result == "skipped" {
				w("- `%s` %s: %s", f.ID, f.Result, cut(f.Detail, 700))
			}
		}
		w("")
		w("### 2.5 Not covered by this test")
		w("")
		w("- TLS configuration, certificates and the reverse proxy (the test instance speaks plain HTTP).")
		w("- Script running in the browser (DOM-based XSS), see the browser tests in `web/tests`.")
		w("- Single sign-on with a real identity provider (OIDC, SAML, LDAP), SCIM clients, office servers (WOPI) and outgoing e-mail: covered by the Go tests of the repository with simulated servers.")
		w("- Operating system, container image and network of the production environment; denial of service at network level; social engineering.")
		w("")
	}
	if hasSt {
		w("### 2.6 Static scans")
		w("")
		w("| Tool | Looks for | Result |\n|---|---|---|")
		for _, s := range st {
			res := s.Summary
			if !s.Ran {
				res = "not run: " + s.Summary + s.Output
			}
			w("| %s | %s | %s |", s.Tool, s.What, mdEscape(res))
		}
		w("")
		for _, s := range st {
			if s.Ran && s.Findings > 0 && s.Output != "" {
				w("**%s**\n\n```\n%s\n```\n", s.Tool, s.Output)
			}
		}
	}

	// ---- functional
	if hasFn {
		w("## 3 Functional test with %d users at the same time", fn.PeakUsers)
		w("")
		w("%d editors and %d readers sign in at the same moment and each goes through every function: %d checks, %d requests in %.0f s. Steps that need the others (saving the same page, registering for an event with limited seats, approving one workflow case, live editing in shared rooms) start at the same moment for everybody.",
			fn.Editors, fn.Readers, fn.Passed+fn.Failed, fn.Requests, fn.Duration)
		w("")
		failed := false
		for _, c := range fn.Checks {
			if c.Fail > 0 {
				if !failed {
					w("### 3.1 Failed checks")
					w("")
					w("| Area | Check | Failed | First failure |\n|---|---|---|---|")
					failed = true
				}
				w("| %s | %s | %d of %d | %s |", c.Area, mdEscape(c.Name), c.Fail, c.Pass+c.Fail, mdEscape(cut(c.Failures[0], 260)))
			}
		}
		if !failed {
			w("All checks passed.")
		}
		w("")
		w("### 3.2 Checks by area")
		w("")
		areas := map[string][2]int{}
		var order []string
		for _, c := range fn.Checks {
			a := areas[c.Area]
			if a == [2]int{} {
				order = append(order, c.Area)
			}
			if c.Fail == 0 {
				a[0]++
			} else {
				a[1]++
			}
			areas[c.Area] = a
		}
		w("| Area | Passed | Failed |\n|---|---|---|")
		for _, a := range order {
			w("| %s | %d | %d |", a, areas[a][0], areas[a][1])
		}
		w("")
		w("### 3.3 Response times with %d users at the same time", fn.PeakUsers)
		w("")
		w("| Operation | Requests | p50 ms | p95 ms | p99 ms | max ms | Errors |\n|---|---|---|---|---|---|---|")
		for _, o := range fn.Ops {
			w("| %s | %d | %.0f | %.0f | %.0f | %.0f | %d |", o.Op, o.Count, o.P50, o.P95, o.P99, o.Max, o.Errors)
		}
		w("| **all** | %d | %.0f | %.0f | %.0f | %.0f | %d |", fn.Total.Count, fn.Total.P50, fn.Total.P95, fn.Total.P99, fn.Total.Max, fn.Total.Errors)
		w("")
		w("### 3.4 All checks")
		w("")
		w("| Area | Check | Users passed | Users failed |\n|---|---|---|---|")
		for _, c := range fn.Checks {
			w("| %s | %s | %d | %d |", c.Area, mdEscape(c.Name), c.Pass, c.Fail)
		}
		w("")
	}

	// ---- load
	if hasLd {
		w("## 4 Load test")
		w("")
		w("Virtual users with a mean think time of %.1f s between two requests, %.0f s measured per stage after the ramp-up. Mix: guests read public pages; readers read pages, lists, search and poll the inbox; editors additionally save pages, drafts (autosave), previews and reactions; live editors hold a WebSocket connection in rooms of 10 and send a change about every 2 s.",
			ld.Think, ld.Hold)
		w("")
		w("Targets (assumptions of this routine, ISO/IEC 25010 time behaviour): reading p95 ≤ %.0f ms, writing p95 ≤ %.0f ms, live editing p95 ≤ %.0f ms, errors ≤ %.1f %%.", ld.Targets.ReadP95, ld.Targets.WriteP95, ld.Targets.LiveP95, ld.Targets.ErrorRate*100)
		w("")
		w("### 4.1 Stages")
		w("")
		w("| Users | Guests / readers / editors / live | Requests/s | Reading p50 / p95 / p99 ms | Writing p50 / p95 / p99 ms | Live editing p50 / p95 ms | Errors | CPU cores: wiki / database / generator / machine | Wiki MB | Targets |\n|---|---|---|---|---|---|---|---|---|---|")
		for _, s := range ld.Stages {
			met := "met"
			if !s.Met {
				met = "missed: " + strings.Join(s.Missed, "; ")
			}
			w("| %d | %d / %d / %d / %d | %.0f | %.0f / %.0f / %.0f | %.0f / %.0f / %.0f | %.0f / %.0f | %.2f %% | %.1f / %.1f / %.1f / %.1f of %d | %.0f | %s |", s.Users, s.Guests, s.Readers, s.Editors, s.Live, s.RPS,
				s.Read.P50, s.Read.P95, s.Read.P99, s.Write.P50, s.Write.P95, s.Write.P99, s.LiveRelay.P50, s.LiveRelay.P95, s.ErrorRate*100, s.WikiCPU, s.DBCPU, s.GenCPU, s.SystemCPU, s.Cores, s.WikiRSS, met)
		}
		w("")
		w("Largest stage within the targets: **%d users**.", ld.Capacity)
		w("")
		for _, n := range ld.Notes {
			w("- %s", n)
		}
		w("")
		w("### 4.2 Response times per function (p95 in ms per stage)")
		w("")
		names := map[string]bool{}
		for _, s := range ld.Stages {
			for _, o := range s.Ops {
				names[o.Op] = true
			}
		}
		var ops []string
		for n := range names {
			ops = append(ops, n)
		}
		sort.Strings(ops)
		head, sep := "| Function |", "|---|"
		for _, s := range ld.Stages {
			head += fmt.Sprintf(" %d |", s.Users)
			sep += "---|"
		}
		w("%s\n%s", head, sep)
		for _, n := range ops {
			line := "| " + n + " |"
			for _, s := range ld.Stages {
				v := "–"
				for _, o := range s.Ops {
					if o.Op == n {
						v = fmt.Sprintf("%.0f", o.P95)
						if o.Errors > 0 {
							v += fmt.Sprintf(" (%d err)", o.Errors)
						}
					}
				}
				line += " " + v + " |"
			}
			w("%s", line)
		}
		w("")
		if len(ld.Spike) > 0 {
			w("### 4.3 Spike: %d requests at the same moment", ld.Spike[0].N)
			w("")
			w("| Function | All answered after | p50 ms | p95 ms | p99 ms | max ms | Errors | Answers |\n|---|---|---|---|---|---|---|---|")
			for _, s := range ld.Spike {
				w("| %s | %.1f s | %.0f | %.0f | %.0f | %.0f | %d | %s |", s.Op, s.Seconds, s.R.P50, s.R.P95, s.R.P99, s.R.Max, s.R.Errors, mdEscape(fmt.Sprint(s.R.Status)))
			}
			w("")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(b.String()), 0o600); err != nil {
		return err
	}
	fmt.Printf("report: %s\n", filepath.Join(dir, "report.md"))
	return nil
}
