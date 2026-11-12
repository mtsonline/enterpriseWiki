package main

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

// LoadOptions configure the load test.
type LoadOptions struct {
	Stages      string        // concurrent virtual users per stage, e.g. "100,1000,10000"
	Hold        time.Duration // measured time per stage (after the ramp-up)
	Think       time.Duration // mean pause between two requests of a user
	CollabShare float64       // share of users in live editing
	GuestShare  float64       // share of guests
	Spike       int           // simultaneous requests of the spike test (0: none)
	NotifyOps   bool          // include comments and workflow starts (they notify many people) in the mix
}

// Targets are the service levels the stages are judged by (assumptions of
// this test routine, see the report).
type Targets struct {
	ReadP95   float64 `json:"read_p95_ms"`
	WriteP95  float64 `json:"write_p95_ms"`
	LiveP95   float64 `json:"live_p95_ms"`
	ErrorRate float64 `json:"error_rate"`
}

var targets = Targets{ReadP95: 500, WriteP95: 1000, LiveP95: 250, ErrorRate: 0.001}

// StageResult is one stage of the load test.
type StageResult struct {
	Users      int        `json:"users"`
	Guests     int        `json:"guests"`
	Readers    int        `json:"readers"`
	Editors    int        `json:"editors"`
	Live       int        `json:"live_editors"`
	LiveFailed int        `json:"live_failed"` // connections refused or lost
	Seconds    float64    `json:"seconds"`
	Requests   int        `json:"requests"`
	RPS        float64    `json:"requests_per_second"`
	Errors     int        `json:"errors"`
	ErrorRate  float64    `json:"error_rate"`
	Read       OpResult   `json:"read"`
	Write      OpResult   `json:"write"`
	LiveRelay  OpResult   `json:"live"`
	Ops        []OpResult `json:"ops"`
	WikiCPU    float64    `json:"wiki_cpu_cores"`
	WikiRSS    float64    `json:"wiki_rss_mb"`
	GenCPU     float64    `json:"generator_cpu_cores"`
	DBCPU      float64    `json:"database_cpu_cores"` // PostgreSQL processes on this machine (0: none found)
	SystemCPU  float64    `json:"machine_cpu_cores"`  // busy cores of the whole machine
	Cores      int        `json:"machine_cores"`
	Met        bool       `json:"targets_met"`
	Missed     []string   `json:"targets_missed"`
}

// SpikeResult is one operation of the spike test.
type SpikeResult struct {
	Op      string   `json:"op"`
	N       int      `json:"simultaneous"`
	Seconds float64  `json:"seconds"` // until the last answer
	R       OpResult `json:"result"`
}

// LoadResult is written to load.json.
type LoadResult struct {
	Started  time.Time     `json:"started"`
	Target   string        `json:"target"`
	Think    float64       `json:"think_s"`
	Hold     float64       `json:"hold_s"`
	Targets  Targets       `json:"targets"`
	Stages   []StageResult `json:"stages"`
	Spike    []SpikeResult `json:"spike"`
	Capacity int           `json:"capacity_users"` // largest stage that met the targets
	Machine  string        `json:"machine"`
	Notes    []string      `json:"notes"`
}

type loadEnv struct {
	o        Options
	lo       LoadOptions
	w        *World
	cookies  []string // sessions of the QA accounts
	editors  []int    // indexes of accounts that may edit
	pages    []int64  // pages the editors save
	rooms    []int64  // pages for live editing
	readable []int64
	pubURL   string
	script   string
}

func cpuSeconds(pid string) float64 {
	b, err := os.ReadFile("/proc/" + pid + "/stat") // #nosec G304,G703 -- process id given by the operator
	if err != nil {
		return 0
	}
	s := string(b)
	f := strings.Fields(s[strings.LastIndex(s, ")")+2:])
	if len(f) < 13 {
		return 0
	}
	u, _ := strconv.ParseFloat(f[11], 64)
	st, _ := strconv.ParseFloat(f[12], 64)
	return (u + st) / 100
}

func rssMB(pid string) float64 {
	b, err := os.ReadFile("/proc/" + pid + "/status") // #nosec G304,G703 -- process id given by the operator
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmRSS:") {
			kb, _ := strconv.ParseFloat(strings.Fields(l)[1], 64)
			return kb / 1024
		}
	}
	return 0
}

// namedCPU sums the CPU seconds of all processes with that name on this machine.
func namedCPU(name string) float64 {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	sum := 0.0
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/comm") // #nosec G304 -- /proc
		if err == nil && strings.TrimSpace(string(b)) == name {
			sum += cpuSeconds(e.Name())
		}
	}
	return sum
}

// busyCPU returns the busy CPU seconds of the machine and its cores.
func busyCPU() (float64, int) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(string(b), "\n")
	f := strings.Fields(lines[0])
	if len(f) < 8 {
		return 0, 0
	}
	busy := 0.0
	for i, v := range f[1:] {
		if i == 3 || i == 4 { // idle, iowait
			continue
		}
		x, _ := strconv.ParseFloat(v, 64)
		busy += x
	}
	cores := 0
	for _, l := range lines[1:] {
		if strings.HasPrefix(l, "cpu") {
			cores++
		}
	}
	return busy / 100, cores
}

func selfCPU() float64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return float64(ru.Utime.Sec+ru.Stime.Sec) + float64(ru.Utime.Usec+ru.Stime.Usec)/1e6
}

func machine() string {
	cpus := 0
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		cpus = strings.Count(string(b), "processor\t")
	}
	mem := ""
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		if f := strings.Fields(strings.SplitN(string(b), "\n", 2)[0]); len(f) > 1 {
			kb, _ := strconv.ParseFloat(f[1], 64)
			mem = fmt.Sprintf(", %.0f GB RAM", kb/1024/1024)
		}
	}
	return fmt.Sprintf("%d CPU cores%s", cpus, mem)
}

func load(o Options, lo LoadOptions, w *World) error {
	var stages []int
	for _, s := range strings.Split(lo.Stages, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
			stages = append(stages, n)
		}
	}
	if len(stages) == 0 {
		return fmt.Errorf("no stages")
	}
	maxUsers := 0
	for _, n := range stages {
		maxUsers = max(maxUsers, n)
	}
	adm, err := adminClient(o)
	if err != nil {
		return err
	}
	// The virtual users share the QA accounts: each account stands for
	// several persons, so the limits of requests per account are raised by
	// that factor for the load test and set back afterwards. The limits
	// per address (guests) stay as they are - every guest has an address.
	if factor := (maxUsers + len(w.Users) - 1) / max(len(w.Users), 1); factor > 1 {
		reset, err := scaleAccountLimits(adm, int64(factor))
		if err != nil {
			return err
		}
		defer reset()
		fmt.Printf("load: limits of requests per account x%d (%d users share %d accounts)\n", factor, maxUsers, len(w.Users))
	}
	e := &loadEnv{o: o, lo: lo, w: w, readable: w.Typed["core.html"]}
	res := &LoadResult{Started: time.Now(), Target: o.Base, Think: lo.Think.Seconds(), Hold: lo.Hold.Seconds(), Targets: targets, Machine: machine()}

	// sessions of all QA accounts (signing in is measured in the functional test)
	e.cookies = make([]string, len(w.Users))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, u := range w.Users {
		wg.Add(1)
		go func(i int, u QAUser) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			c := newClient(o.Base, u.Name)
			if o.SpoofIPs {
				c.IP = vuIP(i + 1)
			}
			if c.Login(u.Name, QAPassword()).Status == 200 {
				e.cookies[i] = c.Cookie
			}
		}(i, u)
	}
	wg.Wait()
	for i, u := range w.Users {
		if e.cookies[i] == "" {
			return fmt.Errorf("sign-in of %s failed (limit of sign-ins per address? use -spoof-ips with server.trusted_proxies)", u.Name)
		}
		if u.Role == "editor" {
			e.editors = append(e.editors, i)
		}
	}
	// pages to save and rooms for live editing
	mk := func(cat int64, title string) int64 {
		var x idObj
		_ = adm.Do("POST", "/api/v1/elements", map[string]any{"category_id": cat, "type": "core.html", "lang": "de", "title": title, "body": mdBody("# Load\n\n" + strings.Repeat("Text of the page. ", 60))}).JSON(&x)
		return x.ID
	}
	for i := 0; i < 200; i++ {
		if id := mk(w.Pages, fmt.Sprintf("QA load page %03d", i+1)); id != 0 {
			e.pages = append(e.pages, id)
		}
	}
	nRooms := int(math.Ceil(float64(maxUsers)*lo.CollabShare/10)) + 1
	for i := 0; i < nRooms; i++ {
		if id := mk(w.Collab, fmt.Sprintf("QA load live page %03d", i+1)); id != 0 {
			e.rooms = append(e.rooms, id)
		}
	}
	if len(e.pages) == 0 || len(e.rooms) == 0 || len(e.readable) == 0 {
		return fmt.Errorf("could not create the pages of the load test")
	}
	e.readable = append(e.readable, e.pages[:min(50, len(e.pages))]...)
	var pub struct {
		URL string `json:"url"`
	}
	_ = adm.Do("GET", fmt.Sprintf("/api/v1/elements/%d", w.PublicPage), nil).JSON(&pub)
	e.pubURL = pub.URL
	if m := scriptSrc.FindSubmatch(newClient(o.Base, "g").Do("GET", "/", nil).Body); m != nil {
		e.script = string(m[1])
	}

	for _, n := range stages {
		st := e.stage(n)
		res.Stages = append(res.Stages, st)
		if st.Met {
			res.Capacity = max(res.Capacity, n)
		}
		fmt.Printf("load: %6d users: %7.0f req/s, read p95 %6.0f ms, write p95 %6.0f ms, live p95 %6.0f ms, errors %.2f %%, CPU cores: wiki %.1f, database %.1f, generator %.1f, machine %.1f of %d; wiki %4.0f MB; targets met: %v\n",
			n, st.RPS, st.Read.P95, st.Write.P95, st.LiveRelay.P95, st.ErrorRate*100, st.WikiCPU, st.DBCPU, st.GenCPU, st.SystemCPU, st.Cores, st.WikiRSS, st.Met)
		_ = writeJSON(o.Out, "load.json", res)
		time.Sleep(5 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		_ = waitReady(ctx, o.Base)
		cancel()
	}
	if lo.Spike > 0 {
		res.Spike = e.spike(lo.Spike)
	}
	if !lo.NotifyOps {
		note := "Writing comments and starting workflow cases are not part of the load mix (switch them on with -notify-ops); the penetration test checks them with a burst of simultaneous requests (API4-POOL)."
		var pt PentestResult
		if readJSON(o.Out, "pentest.json", &pt) == nil {
			for _, f := range pt.Findings {
				if f.ID == "API4-POOL" && f.Result == "fail" {
					note = "Writing comments and starting workflow cases are left out of the load mix: the penetration test of this run found that many of them at the same moment block the wiki (API4-POOL); they would hide every other number."
				}
			}
		}
		res.Notes = append(res.Notes, note)
	}
	res.Notes = append(res.Notes, "Wiki, database and load generator ran on the same machine ("+res.Machine+"). The generator needs part of the CPU itself, so the numbers are a lower bound for this hardware, not the capacity of a production server.")
	if err := writeJSON(o.Out, "load.json", res); err != nil {
		return err
	}
	fmt.Printf("load: largest stage within the targets: %d users\n", res.Capacity)
	return nil
}

func isWrite(op string) bool { return strings.HasPrefix(op, "write:") }

// stage runs n virtual users for the hold time and returns the measurements.
func (e *loadEnv) stage(n int) StageResult {
	rec := newRecorder()
	rec.off.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	st := StageResult{Users: n}
	var liveFailed atomic.Int64
	ramp := time.Duration(math.Min(20, math.Max(2, float64(n)/400)) * float64(time.Second))
	nLive := int(float64(n) * e.lo.CollabShare)
	nGuest := int(float64(n) * e.lo.GuestShare)
	nEdit := int(float64(n) * 0.2)
	for i := 0; i < n; i++ {
		kind := "reader"
		switch {
		case i < nLive:
			kind = "live"
			st.Live++
		case i < nLive+nGuest:
			kind = "guest"
			st.Guests++
		case i < nLive+nGuest+nEdit:
			kind = "editor"
			st.Editors++
		default:
			st.Readers++
		}
		wg.Add(1)
		go func(i int, kind string) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(float64(ramp) * float64(i) / float64(n))):
			}
			e.user(ctx, i, kind, rec, &liveFailed)
		}(i, kind)
	}
	time.Sleep(ramp + 2*time.Second)
	cpu0, gen0, t0 := cpuSeconds(e.o.ServerPID), selfCPU(), time.Now()
	db0 := namedCPU("postgres")
	sys0, cores := busyCPU()
	rec.off.Store(false)
	time.Sleep(e.lo.Hold)
	rec.off.Store(true)
	el := time.Since(t0).Seconds()
	if e.o.ServerPID != "" {
		st.WikiCPU = round1((cpuSeconds(e.o.ServerPID) - cpu0) / el)
		st.WikiRSS = round1(rssMB(e.o.ServerPID))
	}
	st.GenCPU = round1((selfCPU() - gen0) / el)
	st.DBCPU = round1((namedCPU("postgres") - db0) / el)
	sys1, _ := busyCPU()
	st.SystemCPU, st.Cores = round1((sys1-sys0)/el), cores
	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(40 * time.Second):
	}
	transport.CloseIdleConnections()

	ops, _ := rec.Results()
	st.Ops, st.Seconds, st.LiveFailed = ops, round1(el), int(liveFailed.Load())
	// totals of reading, writing and live editing
	rec.mu.Lock()
	var rd, wr, lv []float32
	rs, ws := map[int]int{}, map[int]int{}
	for name, s := range rec.ops {
		switch {
		case strings.HasPrefix(name, "live:"):
			lv = append(lv, s.dur...)
		case isWrite(name):
			wr = append(wr, s.dur...)
			for k, v := range s.status {
				ws[k] += v
			}
		default:
			rd = append(rd, s.dur...)
			for k, v := range s.status {
				rs[k] += v
			}
		}
	}
	rec.mu.Unlock()
	sortF := func(d []float32) []float32 {
		sortFloat32(d)
		return d
	}
	st.Read, st.Write, st.LiveRelay = summarize("read", sortF(rd), rs), summarize("write", sortF(wr), ws), summarize("live", sortF(lv), nil)
	st.Requests = st.Read.Count + st.Write.Count
	st.Errors = st.Read.Errors + st.Write.Errors + st.Read.Rejected + st.Write.Rejected
	if st.Requests > 0 {
		st.ErrorRate = float64(st.Errors) / float64(st.Requests)
	}
	st.RPS = round1(float64(st.Requests) / el)
	miss := func(cond bool, format string, a ...any) {
		if cond {
			st.Missed = append(st.Missed, fmt.Sprintf(format, a...))
		}
	}
	miss(st.Read.P95 > targets.ReadP95, "reading p95 %.0f ms > %.0f ms", st.Read.P95, targets.ReadP95)
	miss(st.Write.P95 > targets.WriteP95, "writing p95 %.0f ms > %.0f ms", st.Write.P95, targets.WriteP95)
	miss(st.Live > 0 && st.LiveRelay.P95 > targets.LiveP95, "live editing p95 %.0f ms > %.0f ms", st.LiveRelay.P95, targets.LiveP95)
	miss(st.ErrorRate > targets.ErrorRate, "error rate %.2f %% > %.2f %%", st.ErrorRate*100, targets.ErrorRate*100)
	miss(st.Live > 0 && float64(st.LiveFailed) > 0.01*float64(st.Live), "%d of %d live editing connections failed", st.LiveFailed, st.Live)
	miss(st.Requests == 0, "no requests")
	st.Met = len(st.Missed) == 0
	return st
}

func sortFloat32(d []float32) {
	// insertion into a sorted copy would be slow for millions of values
	if len(d) < 2 {
		return
	}
	quick32(d, 0, len(d)-1)
}

func quick32(a []float32, lo, hi int) {
	for lo < hi {
		p := a[(lo+hi)/2]
		i, j := lo, hi
		for i <= j {
			for a[i] < p {
				i++
			}
			for a[j] > p {
				j--
			}
			if i <= j {
				a[i], a[j] = a[j], a[i]
				i++
				j--
			}
		}
		if j-lo < hi-i {
			quick32(a, lo, j)
			lo = i
		} else {
			quick32(a, i, hi)
			hi = j
		}
	}
}

// pause waits the think time (exponentially distributed, at most 4x the mean).
func (e *loadEnv) pause(ctx context.Context, r *rand.Rand) bool {
	if e.lo.Think <= 0 {
		return ctx.Err() == nil
	}
	d := time.Duration(math.Min(r.ExpFloat64(), 4) * float64(e.lo.Think))
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// user is one virtual user until the stage ends.
func (e *loadEnv) user(ctx context.Context, i int, kind string, rec *Recorder, liveFailed *atomic.Int64) {
	r := rand.New(rand.NewPCG(uint64(i), 7)) // #nosec G404 -- load mix, not security
	c := newClient(e.o.Base, kind)
	c.rec = rec
	c.hc.Timeout = 30 * time.Second
	if e.o.SpoofIPs {
		c.IP = vuIP(1000 + i)
	}
	w := e.w
	p := fmt.Sprintf
	pick := func(ids []int64) int64 { return ids[r.IntN(len(ids))] }
	switch kind {
	case "guest":
		for e.pause(ctx, r) {
			switch x := r.IntN(100); {
			case x < 30:
				c.DoOp("read: guest opens a page (HTML)", "GET", e.pubURL, nil)
			case x < 60:
				c.DoOp("read: guest page content", "GET", p("/api/v1/elements/%d?lang=de", w.PublicPage), nil)
			case x < 72:
				c.DoOp("read: site information", "GET", "/api/v1/site", nil)
			case x < 84:
				c.DoOp("read: category tree", "GET", "/api/v1/categories/tree", nil)
			case x < 95:
				c.DoOp("read: full-text search", "GET", "/api/v1/search?q=qapublicword", nil)
			default:
				if e.script != "" {
					c.DoOp("read: frontend script", "GET", e.script, nil)
				}
			}
		}
	case "reader", "editor":
		ui := i % len(e.cookies)
		if kind == "editor" {
			ui = e.editors[i%len(e.editors)]
		}
		c.Cookie = e.cookies[ui]
		var draft idObj
		words := []string{"wiki", "demo", "seite", "projekt", "termine", "rechte", "load", "text"}
		read := func(x int) {
			switch {
			case x < 40:
				c.DoOp("read: page content", "GET", p("/api/v1/elements/%d?lang=de", pick(e.readable)), nil)
			case x < 50:
				c.DoOp("read: category tree", "GET", "/api/v1/categories/tree", nil)
			case x < 57:
				c.DoOp("read: category", "GET", p("/api/v1/categories/%d", pick(w.TeamCats)), nil)
			case x < 62:
				c.DoOp("read: element list", "GET", p("/api/v1/elements?category=%d&recursive=1&sort=updated&limit=20", w.Team), nil)
			case x < 72:
				c.DoOp("read: full-text search", "GET", "/api/v1/search?q="+words[r.IntN(len(words))], nil)
			case x < 86:
				c.DoOp("read: inbox (polling)", "GET", "/api/v1/me/inbox", nil)
			case x < 90:
				c.DoOp("read: comments", "GET", p("/api/v1/x/core.comments/elements/%d/comments", pick(e.readable)), nil)
			case x < 93:
				c.DoOp("read: blog posts", "GET", p("/api/v1/x/content.blog/elements/%d/posts", w.Blog), nil)
			case x < 96:
				c.DoOp("read: calendar", "GET", p("/api/v1/x/content.events/calendars/%d/events?from=%s&to=%s", w.Calendar, time.Now().Format("2006-01-02"), time.Now().AddDate(0, 2, 0).Format("2006-01-02")), nil)
			case x < 98:
				c.DoOp("read: properties report", "POST", "/api/v1/x/core.properties/query", map[string]any{"sources": []map[string]any{{"kind": "category", "id": w.Team, "sub": true}}, "limit": 20})
			default:
				c.DoOp("read: media library", "GET", "/api/v1/media", nil)
			}
		}
		for e.pause(ctx, r) {
			x := r.IntN(100)
			if kind == "reader" || x < 50 {
				read(r.IntN(100))
				continue
			}
			switch {
			case x < 65: // open the editor and save
				id := pick(e.pages)
				var cur struct {
					Rev int `json:"rev"`
				}
				_ = c.DoOp("read: page for editing", "GET", p("/api/v1/elements/%d?edit=1", id), nil).JSON(&cur)
				if !e.pause(ctx, r) {
					return
				}
				c.DoOp("write: save page", "PUT", p("/api/v1/elements/%d", id), map[string]any{"lang": "de", "title": "QA load page", "body": mdBody("# Load\n\n" + strings.Repeat("Changed text. ", 80)), "base_rev": cur.Rev})
			case x < 82:
				body := map[string]any{"category_id": w.Pages, "type": "core.html", "lang": "de", "title": "QA load draft", "body": mdBody(strings.Repeat("Draft text. ", 80))}
				if draft.ID == 0 {
					_ = c.DoOp("write: draft (autosave)", "POST", "/api/v1/drafts", body).JSON(&draft)
				} else {
					c.DoOp("write: draft (autosave)", "PUT", p("/api/v1/drafts/%d", draft.ID), body)
				}
			case x < 90:
				c.DoOp("write: preview while editing", "POST", "/api/v1/elements/preview", map[string]any{"type": "core.html", "body": mdBody("# T\n\n" + strings.Repeat("Preview **text** with [link](/x). ", 40) + "\n\n:::panel{type=info}\nx\n:::\n")})
			case x < 93 && e.lo.NotifyOps:
				c.DoOp("write: comment", "POST", p("/api/v1/x/core.comments/elements/%d/comments", pick(e.readable)), map[string]string{"body": "Load test comment."})
			case x < 94 && e.lo.NotifyOps:
				c.DoOp("write: workflow start", "POST", p("/api/v1/x/workflow.process/elements/%d/instances", w.Workflow), map[string]any{"data": map[string]any{"subject": "load"}})
			case x < 95:
				c.DoOp("write: reaction", "POST", p("/api/v1/x/core.reactions/element/%d", pick(e.readable)), map[string]string{"emoji": "👍"})
			case x < 98:
				c.DoOp("write: page view counter", "POST", p("/api/v1/elements/%d/view", pick(e.readable)), nil)
			default:
				c.DoOp("write: create page", "POST", "/api/v1/elements", map[string]any{"category_id": w.Pages, "type": "core.html", "lang": "de", "title": "QA load new page", "body": mdBody("New page.")})
			}
		}
	case "live":
		ui := e.editors[i%len(e.editors)]
		c.Cookie = e.cookies[ui]
		room := e.rooms[(i/10)%len(e.rooms)]
		dctx, dc := context.WithTimeout(ctx, 30*time.Second)
		conn, _, err := dialCollab(dctx, c, room, "de", "")
		dc()
		if err != nil {
			if ctx.Err() == nil {
				liveFailed.Add(1)
				rec.Add("live: connect", 0, 0)
			}
			return
		}
		defer func() { _ = conn.CloseNow() }()
		me := uint64(i) + 1 // #nosec G115 -- small index
		go func() {
			for {
				typ, data, err := conn.Read(ctx)
				if err != nil {
					if ctx.Err() == nil {
						liveFailed.Add(1)
					}
					return
				}
				if typ != websocket.MessageBinary {
					continue
				}
				if s, age, ok := parseSync(data); ok && s != me {
					rec.Add("live: change reaches another editor", age, 200)
				}
			}
		}()
		// typing: a change every 2 s on average, the cursor with every second change
		tick := max(e.lo.Think/3, 500*time.Millisecond)
		if e.lo.Think <= 0 {
			tick = 500 * time.Millisecond
		}
		clock := uint64(0)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration((0.5 + r.Float64()) * float64(tick))):
			}
			wctx, wc := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Write(wctx, websocket.MessageBinary, syncMsg(me, 120))
			if err == nil && clock%2 == 0 {
				err = conn.Write(wctx, websocket.MessageBinary, awarenessMsg(me, clock, "x"))
			}
			wc()
			clock++
			if err != nil {
				return
			}
		}
	}
}

// spike sends n requests at the same moment for a few operations.
func (e *loadEnv) spike(n int) []SpikeResult {
	w := e.w
	p := fmt.Sprintf
	type op struct {
		name, m, path string
		guest         bool
		body          any
	}
	ops := []op{
		{"guest opens a page (HTML)", "GET", e.pubURL, true, nil},
		{"guest reads page content (API)", "GET", p("/api/v1/elements/%d?lang=de", w.PublicPage), true, nil},
		{"user reads page content (API)", "GET", p("/api/v1/elements/%d?lang=de", e.readable[0]), false, nil},
		{"user loads the category tree", "GET", "/api/v1/categories/tree", false, nil},
		{"user searches (full text)", "GET", "/api/v1/search?q=wiki", false, nil},
		{"editor preview while editing", "POST", "/api/v1/elements/preview", false, map[string]any{"type": "core.html", "body": mdBody("# T\n\n" + strings.Repeat("Preview **text**. ", 40))}},
		{"editor saves a draft (autosave)", "POST", "/api/v1/drafts", false, map[string]any{"category_id": w.Pages, "type": "core.html", "lang": "de", "title": "QA spike draft", "body": mdBody(strings.Repeat("Draft text. ", 80))}},
	}
	var out []SpikeResult
	for _, o := range ops {
		rec := newRecorder()
		start := make(chan struct{})
		var wg, ready sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			ready.Add(1)
			go func(i int) {
				defer wg.Done()
				c := newClient(e.o.Base, "spike")
				c.rec = rec
				c.hc.Timeout = 60 * time.Second
				if e.o.SpoofIPs {
					c.IP = vuIP(20000 + i)
				}
				if !o.guest {
					c.Cookie = e.cookies[e.editors[i%len(e.editors)]]
				}
				ready.Done()
				<-start
				c.DoOp(o.name, o.m, o.path, o.body)
			}(i)
		}
		ready.Wait()
		t0 := time.Now()
		close(start)
		wg.Wait()
		ops, _ := rec.Results()
		sr := SpikeResult{Op: o.name, N: n, Seconds: round1(time.Since(t0).Seconds())}
		if len(ops) > 0 {
			sr.R = ops[0]
		}
		out = append(out, sr)
		fmt.Printf("spike: %6d x %-34s all answered after %5.1f s, p50 %6.0f ms, p95 %6.0f ms, max %6.0f ms, errors %d %v\n", n, o.name, sr.Seconds, sr.R.P50, sr.R.P95, sr.R.Max, sr.R.Errors, sr.R.Status)
		transport.CloseIdleConnections()
		time.Sleep(4 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		_ = waitReady(ctx, e.o.Base)
		cancel()
	}
	return out
}

// scaleAccountLimits multiplies the limits of requests per account (expert
// settings) and returns the function that restores the previous values.
// Wikis without these limits are left alone.
func scaleAccountLimits(adm *Client, factor int64) (func(), error) {
	var cur struct {
		Limits []struct {
			Key     string `json:"key"`
			Value   int64  `json:"value"`
			Default int64  `json:"default"`
			Max     int64  `json:"max"`
		} `json:"limits"`
	}
	if r := adm.Do("GET", "/api/v1/admin/limits", nil); r.Status != 200 || r.JSON(&cur) != nil {
		return func() {}, nil
	}
	scaled, before := map[string]any{}, map[string]any{}
	for _, l := range cur.Limits {
		switch l.Key {
		case "core.rate.user_per_second", "core.rate.user_burst", "core.rate.expensive_per_second", "core.rate.expensive_burst":
			scaled[l.Key] = min(l.Value*factor, l.Max)
			before[l.Key] = l.Value
			if l.Value == l.Default {
				before[l.Key] = nil
			}
		}
	}
	if len(scaled) == 0 {
		return func() {}, nil
	}
	if err := must(adm.Do("PUT", "/api/v1/admin/limits", map[string]any{"values": scaled}), 200, 204); err != nil {
		return nil, fmt.Errorf("raising the limits per account: %w", err)
	}
	return func() {
		if err := must(adm.Do("PUT", "/api/v1/admin/limits", map[string]any{"values": before}), 200, 204); err != nil {
			fmt.Fprintln(os.Stderr, "load: the limits per account were not restored:", err)
		}
	}, nil
}
