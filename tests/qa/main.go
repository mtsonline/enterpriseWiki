// Command qa is the test routine of Enterprise Wiki against a running
// instance: functional test with concurrent users, load test, penetration
// test and the report of all results. See README.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

const usage = `usage: qa <command> [flags]

  seed         create the QA area, groups and accounts (removes an older QA area)
  functional   every function with N concurrent users          -> functional.json
  load         staged load test with response times            -> load.json
  pentest      penetration test (OWASP WSTG / ASVS / API Top 10) -> pentest.json
  static       scanners of the CI (govulncheck, gosec, npm audit) -> static.json
  report       report of all result files                      -> report.md
  cleanup      remove the QA area, accounts and groups
  all          seed, functional, load, pentest, static, report

Flags (all commands):
  -base URL        wiki under test (default http://127.0.0.1:8080)
  -admin NAME      administrator (default admin); password from EWIKI_QA_ADMIN_PASSWORD
  -out DIR         result files (default ./qa-results)
  -users N         QA accounts (default 100)
  -ca FILE         trust this certificate authority (test instance behind a reverse proxy
                   with its own authority; also EWIKI_QA_CA)
  -spoof-ips       one client address per virtual user via X-Forwarded-For
                   (the instance must trust this machine: server.trusted_proxies)
  -force           allow a target that is not on this machine or a private network

Never run this against a production wiki: the tests create and delete
content, lock accounts, send very large requests and put the server under
load until it no longer answers in time.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	var o Options
	fs.StringVar(&o.Base, "base", "http://127.0.0.1:8080", "")
	fs.StringVar(&o.AdminUser, "admin", "admin", "")
	fs.StringVar(&o.Out, "out", "qa-results", "")
	fs.IntVar(&o.Users, "users", 100, "")
	fs.BoolVar(&o.SpoofIPs, "spoof-ips", false, "")
	fs.BoolVar(&o.Force, "force", false, "")
	var lo LoadOptions
	fs.StringVar(&lo.Stages, "stages", "100,500,1000,2500,5000,10000", "load: concurrent virtual users per stage")
	fs.DurationVar(&lo.Hold, "hold", 40*time.Second, "load: duration of a stage")
	fs.DurationVar(&lo.Think, "think", 5*time.Second, "load: mean think time between two requests of a user (0: none)")
	fs.Float64Var(&lo.CollabShare, "collab", 0.10, "load: share of the users that edit live (WebSocket)")
	fs.Float64Var(&lo.GuestShare, "guests", 0.30, "load: share of the users that are guests")
	fs.IntVar(&lo.Spike, "spike", 10000, "load: simultaneous requests of the spike test (0: none)")
	fs.BoolVar(&lo.NotifyOps, "notify-ops", false, "load: include comments and workflow starts in the mix")
	fs.StringVar(&o.ServerPID, "server-pid", os.Getenv("EWIKI_QA_SERVER_PID"), "process id of the wiki on this machine (memory and CPU in the results)")
	ca := fs.String("ca", os.Getenv("EWIKI_QA_CA"), "certificate authority (PEM file) of the test instance behind a reverse proxy")
	fs.StringVar(&o.Routes, "routes", "", "pentest: route list (default: docs/developer/api.md of the repository)")
	repo := fs.String("repo", ".", "static: root of the repository")
	title := fs.String("title", "Enterprise Wiki", "report: name of the product")
	version := fs.String("version", "", "report: version under test")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	_ = fs.Parse(os.Args[2:])
	o.AdminPass = os.Getenv("EWIKI_QA_ADMIN_PASSWORD")
	if *ca != "" {
		fail(trustCA(*ca))
	}

	if cmd == "report" {
		fail(report(o.Out, *title, *version))
		return
	}
	if cmd == "static" {
		fail(static(*repo, o.Out))
		return
	}
	if !localTarget(o.Base) && !o.Force {
		fail(fmt.Errorf("%s is not on this machine or a private network; use -force only for a test instance you own", o.Base))
	}
	if o.AdminPass == "" {
		fail(fmt.Errorf("set EWIKI_QA_ADMIN_PASSWORD (password of -admin)"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	fail(waitReady(ctx, o.Base))
	cancel()

	world := func() *World {
		var w World
		if err := readJSON(o.Out, "world.json", &w); err != nil || w.Base != o.Base {
			fail(fmt.Errorf("no test data for %s in %s: run \"qa seed\" first", o.Base, o.Out))
		}
		return &w
	}
	doSeed := func() *World {
		w, err := seed(o)
		fail(err)
		fail(writeJSON(o.Out, "world.json", w))
		fmt.Printf("seed: %d accounts, QA area %d, %d element types\n", len(w.Users), w.Root, len(w.Typed))
		return w
	}
	switch cmd {
	case "seed":
		doSeed()
	case "functional":
		fail(functional(o, world()))
	case "load":
		fail(load(o, lo, world()))
	case "pentest":
		fail(pentest(o, world()))
	case "cleanup":
		fail(cleanup(o))
	case "all":
		// the penetration test comes last: it leaves locked accounts, huge
		// audit entries and blocked connections behind
		w := doSeed()
		fail(functional(o, w))
		w = doSeed()
		fail(load(o, lo, w))
		w = doSeed()
		fail(pentest(o, w))
		fail(static(*repo, o.Out))
		fail(report(o.Out, *title, *version))
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "qa:", err)
		os.Exit(1)
	}
}
