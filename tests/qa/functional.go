package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Check is the result of one check over all virtual users.
type Check struct {
	Area     string   `json:"area"`
	Name     string   `json:"name"`
	Pass     int      `json:"pass"`
	Fail     int      `json:"fail"`
	Failures []string `json:"failures,omitempty"` // the first few
}

// Checks collects results from many goroutines.
type Checks struct {
	mu    sync.Mutex
	m     map[string]*Check
	order []string
}

func newChecks() *Checks { return &Checks{m: map[string]*Check{}} }

// OK records one result. detail is kept for failures only.
func (cs *Checks) OK(area, name string, ok bool, detail string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	k := area + "\x00" + name
	c := cs.m[k]
	if c == nil {
		c = &Check{Area: area, Name: name}
		cs.m[k] = c
		cs.order = append(cs.order, k)
	}
	if ok {
		c.Pass++
	} else {
		c.Fail++
		if len(c.Failures) < 5 {
			c.Failures = append(c.Failures, detail)
		}
	}
	return ok
}

func (cs *Checks) list() []Check {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := make([]Check, 0, len(cs.order))
	for _, k := range cs.order {
		out = append(out, *cs.m[k])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Area < out[j].Area })
	return out
}

// FunctionalResult is written to functional.json.
type FunctionalResult struct {
	Started   time.Time  `json:"started"`
	Duration  float64    `json:"duration_s"`
	Users     int        `json:"users"`
	Editors   int        `json:"editors"`
	Readers   int        `json:"readers"`
	Checks    []Check    `json:"checks"`
	Passed    int        `json:"passed"`
	Failed    int        `json:"failed"`
	Requests  int        `json:"requests"`
	Ops       []OpResult `json:"ops"`
	Total     OpResult   `json:"total"`
	Notes     []string   `json:"notes"`
	PeakUsers int        `json:"peak_concurrent_users"`
}

// vu is one virtual user of the functional test.
type vu struct {
	n    int
	u    QAUser
	c    *Client
	w    *World
	cs   *Checks
	edit bool
}

// want records whether r has one of the statuses and returns the result.
func (v *vu) want(area, name string, r *Resp, status ...int) bool {
	ok := r.Err == nil
	if ok {
		ok = false
		for _, s := range status {
			if r.Status == s {
				ok = true
			}
		}
	}
	return v.cs.OK(area, name, ok, fmt.Sprintf("%s: status %d, want %v: %s", v.u.Name, r.Status, status, r.Text(160)))
}

func (v *vu) ok(area, name string, cond bool, format string, a ...any) bool {
	return v.cs.OK(area, name, cond, v.u.Name+": "+fmt.Sprintf(format, a...))
}

// signedOut checks that the session of c is no longer accepted: /auth/me
// shows no user and a protected route answers 401.
func (v *vu) signedOut(name string, c *Client) {
	r := c.DoOp("auth: me", "GET", "/api/v1/auth/me", nil)
	r2 := c.DoOp("denied: ended session", "GET", "/api/v1/me/sessions", nil)
	v.ok("sign-in", name, r.Status == 200 && bytes.Contains(r.Body, []byte(`"user":null`)) && r2.Status == 401, "me: %d %s; sessions: %d", r.Status, r.Text(80), r2.Status)
}

// barrier lets a number of goroutines continue at the same moment.
type barrier struct {
	wg    sync.WaitGroup
	start chan struct{}
	once  sync.Once
}

func newBarrier(n int) *barrier {
	b := &barrier{start: make(chan struct{})}
	b.wg.Add(n)
	go func() { b.wg.Wait(); b.release() }()
	return b
}
func (b *barrier) release() { b.once.Do(func() { close(b.start) }) }

// wait blocks until all participants have arrived (or 60 s have passed).
func (b *barrier) wait() {
	b.wg.Done()
	select {
	case <-b.start:
	case <-time.After(60 * time.Second):
		b.release()
	}
}

// skip is called by participants that cannot take part any more.
func (b *barrier) skip() { b.wg.Done() }

type sharedState struct {
	conflict     *barrier // all editors save the shared page
	conflictRev  int
	conflictOK   atomic.Int64
	conflict409  atomic.Int64
	register     *barrier // everybody registers for the event with limited seats
	approve      *barrier // all editors approve the shared case
	approveOK    atomic.Int64
	collabJoin   *barrier
	collabSend   *barrier
	collabRecv   atomic.Int64
	collabSpoof  atomic.Int64
	relayLatency *Recorder
}

var png1x1 = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0x00, 0x05, 0x00, 0x01, 0xff, 0x89, 0x99, 0x3d, 0x1d, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82}

// uniquePNG returns a valid PNG whose bytes differ per caller (a text chunk
// after the header), so every upload is its own file.
func uniquePNG(tag string) []byte {
	return append(append([]byte{}, png1x1...), []byte("\nqa:"+tag)...)
}

func functional(o Options, w *World) error {
	adm, err := adminClient(o)
	if err != nil {
		return err
	}
	cs, rec := newChecks(), newRecorder()
	res := &FunctionalResult{Started: time.Now(), Users: len(w.Users)}
	editors := 0
	for _, u := range w.Users {
		if u.Role == "editor" {
			editors++
		}
	}
	res.Editors, res.Readers = editors, len(w.Users)-editors
	var rev struct {
		Rev int `json:"rev"`
	}
	_ = adm.Do("GET", fmt.Sprintf("/api/v1/elements/%d?edit=1", w.SharedPage), nil).JSON(&rev)
	st := &sharedState{conflict: newBarrier(editors), conflictRev: rev.Rev, register: newBarrier(len(w.Users)), approve: newBarrier(editors),
		collabJoin: newBarrier(editors), collabSend: newBarrier(editors), relayLatency: rec}

	var wg sync.WaitGroup
	startAll := make(chan struct{})
	var running, peak atomic.Int64
	for i, u := range w.Users {
		wg.Add(1)
		go func(i int, u QAUser) {
			defer wg.Done()
			c := newClient(o.Base, u.Name)
			c.rec = rec
			if o.SpoofIPs {
				c.IP = vuIP(i + 1)
			}
			v := &vu{n: i, u: u, c: c, w: w, cs: cs, edit: u.Role == "editor"}
			<-startAll
			n := running.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			v.journey(st)
			running.Add(-1)
		}(i, u)
	}
	close(startAll)
	wg.Wait()
	res.PeakUsers = int(peak.Load())

	// ---- consistency after all users are done (as administrator)
	g := &vu{u: QAUser{Name: "admin"}, c: adm, w: w, cs: cs, edit: true}
	var revs []json.RawMessage
	_ = adm.Do("GET", fmt.Sprintf("/api/v1/elements/%d/revisions?lang=de", w.SharedPage), nil).JSON(&revs)
	okSaves, conflicts := int(st.conflictOK.Load()), int(st.conflict409.Load())
	g.ok("integrity", "simultaneous saves of one page: exactly one wins, all others get 409", okSaves == 1 && conflicts == editors-1,
		"%d saves succeeded, %d conflicts, %d editors", okSaves, conflicts, editors)
	g.ok("integrity", "simultaneous saves: one new version per successful save", len(revs) == st.conflictRev+okSaves,
		"%d versions, expected %d", len(revs), st.conflictRev+okSaves)
	var regs struct {
		Registrations []struct {
			UserID int64  `json:"user_id"`
			Status string `json:"status"`
		} `json:"registrations"`
		Items []struct {
			UserID int64  `json:"user_id"`
			Status string `json:"status"`
		} `json:"items"`
	}
	rr := adm.Do("GET", fmt.Sprintf("/api/v1/x/content.events/events/%d/registrations?occurrence=%s", w.SharedEvent, w.EventDate), nil)
	_ = rr.JSON(&regs)
	list := regs.Registrations
	if len(list) == 0 {
		list = regs.Items
	}
	if len(list) == 0 {
		_ = rr.JSON(&list)
	}
	confirmed, seen, dup := 0, map[int64]bool{}, 0
	for _, x := range list {
		if x.Status != "waiting" && x.Status != "waitlist" && x.Status != "interested" {
			confirmed++
		}
		if seen[x.UserID] {
			dup++
		}
		seen[x.UserID] = true
	}
	g.ok("integrity", "event with limited seats: never more confirmed registrations than seats", rr.Status == 200 && confirmed <= w.EventCapacity && confirmed > 0,
		"status %d, %d confirmed of %d seats (%d entries): %s", rr.Status, confirmed, w.EventCapacity, len(list), rr.Text(200))
	g.ok("integrity", "event registration: nobody is registered twice", dup == 0, "%d double registrations", dup)
	g.ok("integrity", "shared workflow case: exactly one approval is accepted", st.approveOK.Load() == 1, "%d approvals accepted", st.approveOK.Load())
	g.ok("live editing", "every editor received the changes of the others in the room", int(st.collabRecv.Load()) == editors,
		"%d of %d editors received changes", st.collabRecv.Load(), editors)
	g.ok("live editing", "names of cursors come from the server (a forged name is replaced)", st.collabSpoof.Load() == 0, "%d forged names relayed", st.collabSpoof.Load())
	g.want("integrity", "audit log lists the changes", adm.Do("GET", "/api/v1/admin/audit?limit=5", nil), 200)
	g.want("integrity", "wiki is healthy after the test (/readyz)", adm.Do("GET", "/readyz", nil), 200)

	ops, total := rec.Results()
	g.ok("integrity", "no server errors (5xx) or failed connections during the whole test", total.Errors == 0, "%d of %d requests failed: %v", total.Errors, total.Count, total.Status)
	res.Ops, res.Total, res.Requests = ops, total, total.Count
	res.Checks = cs.list()
	for _, c := range res.Checks {
		if c.Fail == 0 {
			res.Passed++
		} else {
			res.Failed++
		}
	}
	res.Duration = round1(time.Since(res.Started).Seconds())
	if err := writeJSON(o.Out, "functional.json", res); err != nil {
		return err
	}
	fmt.Printf("functional: %d users at once, %d checks passed, %d failed, %d requests, %d errors, p95 %.0f ms, %.0f s\n",
		res.PeakUsers, res.Passed, res.Failed, total.Count, total.Errors, total.P95, res.Duration)
	for _, c := range res.Checks {
		if c.Fail > 0 {
			fmt.Printf("  FAIL [%s] %s (%d of %d): %s\n", c.Area, c.Name, c.Fail, c.Pass+c.Fail, c.Failures[0])
		}
	}
	return nil
}

var scriptSrc = regexp.MustCompile(`<script[^>]+src="([^"]+)"`)

// journey is everything one user does. Steps that need others (saving the
// same page, registering for the same event) wait for everybody.
func (v *vu) journey(st *sharedState) {
	w, c := v.w, v.c
	p := func(format string, a ...any) string { return fmt.Sprintf(format, a...) }
	var joined [5]bool // barriers this user has passed
	defer func() {
		// whoever stops early (panic, failed sign-in) must not block the others
		rec := recover()
		for i, b := range []*barrier{st.register, st.conflict, st.approve, st.collabJoin, st.collabSend} {
			if !joined[i] && (i == 0 || v.edit) {
				b.skip()
			}
		}
		if rec != nil {
			v.ok("integrity", "virtual user finished without internal error of the test", false, "%v", rec)
		}
	}()

	// ---- sign-in
	bad := newClient(c.Base, v.u.Name)
	bad.IP, bad.rec = c.IP, c.rec
	v.want("sign-in", "wrong password is refused", bad.DoOp("auth: wrong password", "POST", "/api/v1/auth/login", map[string]string{"username": v.u.Name, "password": "wrong-" + QAPassword()}), 401)
	if !v.want("sign-in", "sign-in with user name and password", c.Login(v.u.Name, QAPassword()), 200) {
		return
	}
	var me struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		IsAdmin  bool   `json:"is_admin"`
	}
	r := c.DoOp("auth: me", "GET", "/api/v1/auth/me", nil)
	_ = r.JSON(&me)
	if me.Username == "" {
		var wrap struct {
			User json.RawMessage `json:"user"`
		}
		if r.JSON(&wrap) == nil && wrap.User != nil {
			_ = json.Unmarshal(wrap.User, &me)
		}
	}
	v.ok("sign-in", "session belongs to the signed-in user and is no administrator", r.Status == 200 && me.Username == v.u.Name && !me.IsAdmin, "status %d user %q admin %v", r.Status, me.Username, me.IsAdmin)
	v.want("sign-in", "list of own sessions", c.DoOp("me: sessions", "GET", "/api/v1/me/sessions", nil), 200)

	// ---- reading and navigation
	v.want("navigation", "site information", c.DoOp("read: site", "GET", "/api/v1/site", nil), 200)
	r = c.DoOp("read: category tree", "GET", "/api/v1/categories/tree", nil)
	var tree []catNode
	_ = r.JSON(&tree)
	seesTeam, seesPrivate := false, false
	for _, n := range tree {
		seesTeam = seesTeam || n.ID == w.Team
		seesPrivate = seesPrivate || n.ID == w.Private
	}
	v.ok("navigation", "category tree shows the team area", r.Status == 200 && seesTeam, "status %d, team visible %v", r.Status, seesTeam)
	v.ok("permissions", "category tree hides the private area", !seesPrivate, "private category %d is listed", w.Private)
	v.want("navigation", "category with its elements", c.DoOp("read: category", "GET", p("/api/v1/categories/%d", w.Team), nil), 200)
	v.want("navigation", "subcategories", c.DoOp("read: categories", "GET", p("/api/v1/categories?parent=%d", w.Team), nil), 200)
	v.want("navigation", "list of elements (sorted, paged)", c.DoOp("read: element list", "GET", p("/api/v1/elements?category=%d&recursive=1&sort=updated&limit=20", w.Team), nil), 200)
	v.want("navigation", "spaces directory", c.DoOp("read: spaces", "GET", "/api/v1/spaces", nil), 200)
	v.want("navigation", "element types and macros", c.DoOp("read: element types", "GET", "/api/v1/element-types", nil), 200)
	v.want("navigation", "macro list", c.DoOp("read: macros", "GET", "/api/v1/macros", nil), 200)
	v.want("navigation", "UI texts of a language", c.DoOp("read: i18n", "GET", "/api/v1/i18n/de", nil), 200)
	for typ, ids := range w.Typed {
		id := ids[v.n%len(ids)]
		r := c.DoOp("read: element "+typ, "GET", p("/api/v1/elements/%d?lang=de", id), nil)
		var e struct {
			HTML string `json:"html"`
			URL  string `json:"url"`
		}
		_ = r.JSON(&e)
		v.ok("element types", "read "+typ, r.Status == 200, "element %d: status %d %s", id, r.Status, r.Text(120))
		if typ == "core.html" {
			v.ok("element types", "page is rendered to HTML", strings.Contains(e.HTML, "<"), "element %d has no html", id)
			if e.URL != "" {
				h := c.DoOp("read: page (HTML shell)", "GET", e.URL, nil)
				v.ok("navigation", "readable address of a page answers with the application", h.Status == 200 && strings.Contains(h.Header.Get("Content-Type"), "text/html"), "%s: status %d", e.URL, h.Status)
				if m := scriptSrc.FindSubmatch(h.Body); m != nil && v.n%10 == 0 {
					a := c.DoOp("read: static script", "GET", string(m[1]), nil)
					v.ok("navigation", "frontend script is delivered", a.Status == 200 && len(a.Body) > 1000, "%s: status %d, %d bytes", m[1], a.Status, len(a.Body))
				}
			}
		}
	}
	v.want("element types", "blog: list of posts", c.DoOp("read: blog posts", "GET", p("/api/v1/x/content.blog/elements/%d/posts", w.Blog), nil), 200)
	v.want("element types", "FAQ: list of questions", c.DoOp("read: faq", "GET", p("/api/v1/x/content.faq/elements/%d/questions", w.FAQ), nil), 200)
	from, to := time.Now().UTC().Format(time.RFC3339), time.Now().Add(90*24*time.Hour).UTC().Format(time.RFC3339)
	v.want("element types", "calendar: events of a period", c.DoOp("read: calendar", "GET", p("/api/v1/x/content.events/calendars/%d/events?from=%s&to=%s", w.Calendar, from, to), nil), 200)
	v.want("element types", "calendar: upcoming events", c.DoOp("read: upcoming events", "GET", "/api/v1/x/content.events/upcoming", nil), 200)
	if ids := w.Typed["content.team"]; len(ids) > 0 {
		v.want("element types", "team page: members", c.DoOp("read: team", "GET", p("/api/v1/x/content.team/elements/%d/members", ids[0]), nil), 200)
	}
	if ids := w.Typed["core.attachment"]; len(ids) > 0 {
		r := c.DoOp("read: attachment download", "GET", p("/api/v1/x/core.attachment/elements/%d/file", ids[0]), nil)
		v.ok("files", "download of an attached file (PDF)", r.Status == 200 && bytes.HasPrefix(r.Body, []byte("%PDF")), "status %d, %d bytes", r.Status, len(r.Body))
	}
	v.want("navigation", "feed of a category answers (on or off)", c.DoOp("read: feed", "GET", p("/api/v1/feeds/categories/%d?format=atom", w.Team), nil), 200, 403, 404)

	// ---- search
	r = c.DoOp("search: full text", "GET", "/api/v1/search?q=demo&lang=de", nil)
	v.ok("search", "full-text search finds pages", r.Status == 200 && bytes.Contains(r.Body, []byte("snippet")), "status %d: %s", r.Status, r.Text(120))
	r = c.DoOp("search: in category", "GET", p("/api/v1/search?q=wiki&category=%d", w.Team), nil)
	v.want("search", "search within a category", r, 200)
	r = c.DoOp("search: secret", "GET", "/api/v1/search?q="+w.Secret, nil)
	v.ok("permissions", "search never shows content of the private area", r.Status == 200 && !bytes.Contains(r.Body, []byte("QA secret page")), "secret page found: %s", r.Text(200))
	v.want("search", "tag cloud", c.DoOp("search: tags", "GET", "/api/v1/tags", nil), 200)
	v.want("search", "people lookup", c.DoOp("search: users lookup", "GET", "/api/v1/users/lookup?q=qa", nil), 200, 403)

	// ---- what nobody but administrators may do
	r = c.DoOp("denied: private page", "GET", p("/api/v1/elements/%d", w.SecretPage), nil)
	v.ok("permissions", "private page cannot be read", (r.Status == 403 || r.Status == 404) && !bytes.Contains(r.Body, []byte(w.Secret)), "status %d: %s", r.Status, r.Text(120))
	v.want("permissions", "private page cannot be changed", c.DoOp("denied: private page write", "PUT", p("/api/v1/elements/%d", w.SecretPage),
		map[string]any{"lang": "de", "title": "x", "body": htmlBody("<p>x</p>"), "base_rev": 1}), 403, 404)
	v.want("permissions", "versions of the private page cannot be read", c.DoOp("denied: private revisions", "GET", p("/api/v1/elements/%d/revisions", w.SecretPage), nil), 403, 404)
	v.want("permissions", "user administration is refused", c.DoOp("denied: admin users", "GET", "/api/v1/admin/users", nil), 403)
	v.want("permissions", "settings are refused", c.DoOp("denied: admin settings", "GET", "/api/v1/admin/settings", nil), 403)
	v.want("permissions", "audit log is refused", c.DoOp("denied: admin audit", "GET", "/api/v1/admin/audit", nil), 403)
	v.want("permissions", "rules of the team area cannot be changed", c.DoOp("denied: acl", "POST", p("/api/v1/categories/%d/acl", w.Team),
		map[string]any{"subject_type": "user", "subject_id": v.u.ID, "preset": "manager"}), 403)
	if !v.edit {
		v.want("permissions", "reader cannot create a page", c.DoOp("denied: reader creates", "POST", "/api/v1/elements",
			map[string]any{"category_id": w.Pages, "type": "core.html", "lang": "de", "title": "no", "body": htmlBody("<p>x</p>")}), 403)
		v.want("permissions", "reader cannot change a page", c.DoOp("denied: reader edits", "PUT", p("/api/v1/elements/%d", w.SharedPage),
			map[string]any{"lang": "de", "title": "no", "body": htmlBody("<p>x</p>"), "base_rev": 1}), 403)
		v.want("permissions", "reader cannot upload", c.Upload("denied: reader uploads", "/api/v1/media", map[string]string{"meta": p(`{"category_id":%d}`, w.Pages)}, "x.png", uniquePNG(v.u.Name)), 403)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		conn, status, err := dialCollab(ctx, c, w.CollabPages[0], "de", "")
		cancel()
		if conn != nil {
			_ = conn.CloseNow()
		}
		v.ok("live editing", "reader cannot join live editing", err != nil && status == 403, "status %d, err %v", status, err)
		v.want("permissions", "reader asks for access (request is sent)", c.DoOp("write: access request", "POST", "/api/v1/access-requests",
			map[string]any{"type": "category", "id": w.Private, "message": "QA"}), 200)
	}

	// ---- personal functions (everybody)
	target := w.Typed["core.html"][v.n%len(w.Typed["core.html"])]
	r = c.DoOp("write: reaction", "POST", p("/api/v1/x/core.reactions/element/%d", target), map[string]string{"emoji": "👍"})
	v.ok("collaboration", "reaction on a page", r.Status == 200 && bytes.Contains(r.Body, []byte(`"mine":true`)), "status %d: %s", r.Status, r.Text(120))
	v.want("collaboration", "reactions of a page", c.DoOp("read: reactions", "GET", p("/api/v1/x/core.reactions/element/%d", target), nil), 200)
	v.want("personal", "add a favorite", c.DoOp("write: favorite", "POST", "/api/v1/x/core.favorites/", map[string]any{"type": "element", "id": target}), 200, 201)
	v.want("personal", "remove a favorite", c.DoOp("write: favorite remove", "DELETE", p("/api/v1/x/core.favorites/element/%d", target), nil), 200, 204)
	r = c.DoOp("write: watch", "PUT", "/api/v1/me/watches", map[string]any{"target_type": "element", "target_id": target, "mode": "link"})
	var watch idObj
	_ = r.JSON(&watch)
	v.want("personal", "watch a page", r, 200, 201)
	v.want("personal", "list of watches", c.DoOp("read: watches", "GET", "/api/v1/me/watches", nil), 200)
	if watch.ID != 0 {
		v.want("personal", "stop watching", c.DoOp("write: watch remove", "DELETE", p("/api/v1/me/watches/%d", watch.ID), nil), 200, 204)
	}
	v.want("personal", "inbox", c.DoOp("read: inbox (polling)", "GET", "/api/v1/me/inbox", nil), 200)
	v.want("personal", "notification settings", c.DoOp("read: notifications", "GET", "/api/v1/me/notifications", nil), 200)
	v.want("personal", "change own profile", c.DoOp("write: profile", "PUT", "/api/v1/me/profile", map[string]string{"job_title": "QA " + v.u.Role, "department": "Test"}), 200, 204)
	r = c.DoOp("read: profile", "GET", "/api/v1/me/profile", nil)
	v.ok("personal", "profile keeps the change", r.Status == 200 && bytes.Contains(r.Body, []byte("QA "+v.u.Role)), "status %d: %s", r.Status, r.Text(160))
	v.want("personal", "choose the editor (preference)", c.DoOp("write: preferences", "PUT", "/api/v1/me", map[string]any{"display_name": p("QA %s %04d", v.u.Role, v.n+1), "language": "de", "prefs": map[string]string{"editor": "visual"}}), 200, 204)
	v.want("personal", "preferences", c.DoOp("read: preferences", "GET", "/api/v1/me/prefs", nil), 200)
	v.want("personal", "personal feed key", c.DoOp("write: feed token", "POST", "/api/v1/me/feed-token", nil), 200, 201)
	v.want("personal", "delete the feed key", c.DoOp("write: feed token remove", "DELETE", "/api/v1/me/feed-token", nil), 200, 204)
	v.want("personal", "own tasks of workflows", c.DoOp("read: tasks", "GET", "/api/v1/x/workflow.process/tasks", nil), 200)
	v.want("personal", "own event registrations", c.DoOp("read: my registrations", "GET", "/api/v1/x/content.events/me/registrations", nil), 200)
	v.want("personal", "media library", c.DoOp("read: media list", "GET", "/api/v1/media", nil), 200)
	v.want("personal", "permission overview texts", c.DoOp("read: permissions", "GET", "/api/v1/permissions", nil), 200)

	// comments: what the page says (can_comment) is what the server does
	var cm struct {
		Can bool `json:"can_comment"`
	}
	_ = c.DoOp("read: comments", "GET", p("/api/v1/x/core.comments/elements/%d/comments", target), nil).JSON(&cm)
	r = c.DoOp("write: comment", "POST", p("/api/v1/x/core.comments/elements/%d/comments", target), map[string]string{"body": "QA comment of " + v.u.Name})
	if cm.Can {
		var cmt idObj
		_ = r.JSON(&cmt)
		if v.want("collaboration", "write a comment", r, 201, 200) {
			v.want("collaboration", "answer a comment", c.DoOp("write: comment reply", "POST", p("/api/v1/x/core.comments/elements/%d/comments", target), map[string]any{"body": "reply", "parent_id": cmt.ID}), 201, 200)
			v.want("collaboration", "change own comment", c.DoOp("write: comment edit", "PUT", p("/api/v1/x/core.comments/comments/%d", cmt.ID), map[string]string{"body": "QA comment (edited)"}), 200)
			r = c.DoOp("read: comments", "GET", p("/api/v1/x/core.comments/elements/%d/comments", target), nil)
			v.ok("collaboration", "comment list shows the changed comment", bytes.Contains(r.Body, []byte("QA comment (edited)")), "not found: %s", r.Text(160))
			v.want("collaboration", "delete own comment", c.DoOp("write: comment delete", "DELETE", p("/api/v1/x/core.comments/comments/%d", cmt.ID), nil), 200, 204)
		}
	} else {
		v.want("permissions", "comment is refused where the page says so", r, 403)
	}

	// ---- event with limited seats: everybody at the same moment
	joined[0] = true
	st.register.wait()
	r = c.DoOp("write: event registration (all at once)", "POST", p("/api/v1/x/content.events/events/%d/registrations", w.SharedEvent), map[string]any{"occurrence": w.EventDate})
	v.want("events", "register for an event (seat or waiting list)", r, 200, 201)

	// ---- password: change, other sessions end, change back
	if v.n%4 == 0 {
		other := newClient(c.Base, v.u.Name)
		other.IP, other.rec = c.IP, c.rec
		if other.Login(v.u.Name, QAPassword()).Status == 200 {
			np := QAPassword() + "-new"
			if v.want("sign-in", "change own password", c.DoOp("auth: change password", "PUT", "/api/v1/me/password", map[string]string{"current": QAPassword(), "new": np}), 200, 204) {
				v.signedOut("password change ends the other sessions", other)
				v.want("sign-in", "this session survives the password change", c.DoOp("auth: me", "GET", "/api/v1/auth/me", nil), 200)
				v.want("sign-in", "change the password back", c.DoOp("auth: change password", "PUT", "/api/v1/me/password", map[string]string{"current": np, "new": QAPassword()}), 200, 204)
			}
			v.want("sign-in", "weak new password is refused", c.DoOp("auth: weak password", "PUT", "/api/v1/me/password", map[string]string{"current": QAPassword(), "new": "short"}), 400)
		}
	}

	if v.edit {
		v.editor(st, &joined)
	}

	// ---- API key (read only)
	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	r = c.DoOp("write: API key", "POST", "/api/v1/me/api-keys", map[string]any{"name": "qa", "permissions": 1, "expires_at": exp})
	var key struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	_ = r.JSON(&key)
	if v.want("API", "create a read-only API key", r, 201) && key.Token != "" {
		k := newClient(c.Base, v.u.Name+"/key")
		k.Bearer, k.IP, k.rec = key.Token, c.IP, c.rec
		v.want("API", "API key reads a page", k.DoOp("read: page with API key", "GET", p("/api/v1/elements/%d", target), nil), 200)
		v.want("API", "read-only API key cannot write", k.DoOp("denied: API key write", "POST", "/api/v1/elements",
			map[string]any{"category_id": w.Pages, "type": "core.html", "lang": "de", "title": "key", "body": htmlBody("<p>x</p>")}), 403)
		v.want("API", "API key cannot read the private page", k.DoOp("denied: API key private", "GET", p("/api/v1/elements/%d", w.SecretPage), nil), 403, 404)
		v.want("API", "list of own API keys", c.DoOp("read: API keys", "GET", "/api/v1/me/api-keys", nil), 200)
		v.want("API", "revoke the API key", c.DoOp("write: API key revoke", "DELETE", p("/api/v1/me/api-keys/%d", key.ID), nil), 200, 204)
		v.want("API", "revoked API key no longer works", k.DoOp("denied: revoked API key", "GET", p("/api/v1/elements/%d", target), nil), 401)
	}

	// ---- sign-out
	old := c.Cookie
	v.want("sign-in", "sign out", c.DoOp("auth: logout", "POST", "/api/v1/auth/logout", nil), 200, 204)
	c.Cookie = old
	v.signedOut("session is invalid after signing out", c)
}

// editor is the part of the journey for users who may edit.
func (v *vu) editor(st *sharedState, joined *[5]bool) {
	w, c := v.w, v.c
	p := func(format string, a ...any) string { return fmt.Sprintf(format, a...) }
	type el struct {
		ID   int64  `json:"id"`
		Rev  int    `json:"rev"`
		HTML string `json:"html"`
	}
	get := func(id int64, q string) (el, *Resp) {
		var e el
		r := c.DoOp("read: page for editing", "GET", p("/api/v1/elements/%d?edit=1%s", id, q), nil)
		_ = r.JSON(&e)
		return e, r
	}

	// ---- page: create, edit, versions, translation, move, trash
	marker := "qa-marker-" + v.u.Name
	r := c.DoOp("write: create page", "POST", "/api/v1/elements", map[string]any{"category_id": w.Pages, "type": "core.html", "lang": "de", "title": "QA page of " + v.u.Name,
		"body": mdBody("# Title\n\nFirst version " + marker + "\n\n- [ ] task\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n:status[OK]{color=green}\n"), "tags": []string{"qa"}})
	var page el
	_ = r.JSON(&page)
	if v.want("pages", "create a page (Markdown)", r, 201) && page.ID != 0 {
		e, r := get(page.ID, "")
		v.ok("pages", "new page is version 1 and rendered (table, task list, macro)", r.Status == 200 && e.Rev == 1 && strings.Contains(e.HTML, "<table") && strings.Contains(e.HTML, "ew-status") && strings.Contains(e.HTML, marker),
			"rev %d: %s", e.Rev, r.Text(200))
		r = c.DoOp("write: save page", "PUT", p("/api/v1/elements/%d", page.ID), map[string]any{"lang": "de", "title": "QA page of " + v.u.Name, "body": mdBody("Second version " + marker), "comment": "v2", "base_rev": 1})
		v.want("pages", "save a new version", r, 200)
		r = c.DoOp("write: save page (stale)", "PUT", p("/api/v1/elements/%d", page.ID), map[string]any{"lang": "de", "title": "stale", "body": mdBody("stale"), "base_rev": 1})
		v.want("pages", "saving on an outdated version is a conflict (409)", r, 409)
		var revs []json.RawMessage
		r = c.DoOp("read: versions", "GET", p("/api/v1/elements/%d/revisions?lang=de", page.ID), nil)
		_ = r.JSON(&revs)
		v.ok("pages", "history lists both versions", r.Status == 200 && len(revs) == 2, "status %d, %d versions", r.Status, len(revs))
		r = c.DoOp("read: old version", "GET", p("/api/v1/elements/%d/revisions/de/1", page.ID), nil)
		v.ok("pages", "old version keeps its content", r.Status == 200 && bytes.Contains(r.Body, []byte("First version")), "status %d: %s", r.Status, r.Text(120))
		v.want("pages", "restore version 1", c.DoOp("write: restore version", "POST", p("/api/v1/elements/%d/restore", page.ID), map[string]any{"lang": "de", "rev": 1}), 200)
		e, _ = get(page.ID, "")
		v.ok("pages", "restoring creates version 3 with the old content", e.Rev == 3 && strings.Contains(e.HTML, "First version"), "rev %d", e.Rev)
		v.want("languages", "save a translation (English)", c.DoOp("write: translation", "PUT", p("/api/v1/elements/%d", page.ID),
			map[string]any{"lang": "en", "title": "QA page (en)", "body": mdBody("English text " + marker), "base_rev": 0}), 200)
		r = c.DoOp("read: translation", "GET", p("/api/v1/elements/%d?lang=en", page.ID), nil)
		v.ok("languages", "translation is delivered in its language", r.Status == 200 && bytes.Contains(r.Body, []byte("English text")), "status %d: %s", r.Status, r.Text(120))
		v.want("pages", "change tags and position (PATCH)", c.DoOp("write: patch page", "PATCH", p("/api/v1/elements/%d", page.ID), map[string]any{"tags": []string{"qa", "qa-" + v.u.Role}}), 200)
		r = c.DoOp("search: own page", "GET", "/api/v1/search?q="+marker, nil)
		v.ok("search", "a new page is found by the full-text search at once", r.Status == 200 && bytes.Contains(r.Body, []byte(p(`"element_id":%d`, page.ID))), "page %d not in: %s", page.ID, r.Text(200))
		r = c.DoOp("write: duplicate page", "POST", p("/api/v1/elements/%d/duplicate", page.ID), map[string]any{"category_id": w.Pages})
		var dup el
		_ = r.JSON(&dup)
		v.want("pages", "duplicate a page", r, 201, 200)
		v.want("pages", "move a page to another category", c.DoOp("write: move page", "POST", p("/api/v1/elements/%d/move", page.ID), map[string]any{"category_id": w.BlogCat}), 200)
		v.want("pages", "count a page view", c.DoOp("write: page view", "POST", p("/api/v1/elements/%d/view", page.ID), nil), 200, 204)
		// properties
		r = c.DoOp("write: properties", "PUT", p("/api/v1/x/core.properties/elements/%d", page.ID), map[string]any{"values": map[string]any{"qa_room": "B 2.14", "qa_budget": 400},
			"free": map[string]any{"qa_room": map[string]string{"type": "text", "label": "Room"}, "qa_budget": map[string]string{"type": "integer", "label": "Budget"}}})
		v.want("properties", "set free properties of a page", r, 200)
		r = c.DoOp("read: properties", "GET", p("/api/v1/x/core.properties/elements/%d", page.ID), nil)
		v.ok("properties", "properties are stored", r.Status == 200 && bytes.Contains(r.Body, []byte("B 2.14")), "status %d: %s", r.Status, r.Text(160))
		r = c.DoOp("read: properties query", "POST", "/api/v1/x/core.properties/query", map[string]any{"sources": []map[string]any{{"kind": "category", "id": w.Team, "sub": true}}, "limit": 20})
		v.want("properties", "query over the properties of a category", r, 200)
		v.want("properties", "history of the properties", c.DoOp("read: properties history", "GET", p("/api/v1/x/core.properties/history/%d", page.ID), nil), 200)
		// trash
		if dup.ID != 0 {
			v.want("trash", "delete a page (to the trash)", c.DoOp("write: delete page", "DELETE", p("/api/v1/elements/%d", dup.ID), nil), 200, 204)
			v.want("trash", "deleted page is gone", c.DoOp("read: deleted page", "GET", p("/api/v1/elements/%d", dup.ID), nil), 404, 410)
			r = c.DoOp("read: trash", "GET", "/api/v1/trash", nil)
			v.ok("trash", "trash lists the deleted page", r.Status == 200 && bytes.Contains(r.Body, []byte(p(`"id":%d`, dup.ID))), "status %d", r.Status)
			v.want("trash", "restore from the trash", c.DoOp("write: restore from trash", "POST", p("/api/v1/trash/element/%d/restore", dup.ID), nil), 200, 204)
			v.want("trash", "restored page is back", c.DoOp("read: page", "GET", p("/api/v1/elements/%d", dup.ID), nil), 200)
			v.want("trash", "delete it again", c.DoOp("write: delete page", "DELETE", p("/api/v1/elements/%d", dup.ID), nil), 200, 204)
		}
	}
	v.want("pages", "preview while editing", c.DoOp("write: preview", "POST", "/api/v1/elements/preview", map[string]any{"type": "core.html", "body": mdBody("**bold** :::panel{type=info}\nText\n:::")}), 200)
	v.want("pages", "HTML to Markdown conversion", c.DoOp("write: to-markdown", "POST", "/api/v1/x/core.html/to-markdown", map[string]any{"source": "<h2>T</h2><p>x <strong>y</strong></p>"}), 200)

	// ---- drafts and templates
	r = c.DoOp("write: draft (autosave)", "POST", "/api/v1/drafts", map[string]any{"category_id": w.Pages, "type": "core.html", "lang": "de", "title": "QA draft of " + v.u.Name, "body": htmlBody("<p>draft</p>")})
	var d idObj
	_ = r.JSON(&d)
	if v.want("drafts", "save a draft", r, 201) && d.ID != 0 {
		v.want("drafts", "update the draft (autosave)", c.DoOp("write: draft (autosave)", "PUT", p("/api/v1/drafts/%d", d.ID),
			map[string]any{"category_id": w.Pages, "type": "core.html", "lang": "de", "title": "QA draft of " + v.u.Name, "body": htmlBody("<p>draft 2</p>")}), 200)
		v.want("drafts", "list of own drafts", c.DoOp("read: drafts", "GET", "/api/v1/drafts", nil), 200)
		r = c.DoOp("write: publish draft", "POST", p("/api/v1/drafts/%d/publish", d.ID), map[string]any{})
		v.want("drafts", "publish the draft as a page", r, 200, 201)
	}
	r = c.DoOp("write: template", "POST", "/api/v1/templates", map[string]any{"name": "QA template " + v.u.Name, "type": "core.html", "title": "Minutes", "body": htmlBody("<h2>Agenda</h2>")})
	var tpl idObj
	_ = r.JSON(&tpl)
	if v.want("drafts", "save a page template", r, 201) && tpl.ID != 0 {
		v.want("drafts", "list of templates", c.DoOp("read: templates", "GET", "/api/v1/templates", nil), 200)
		v.want("drafts", "delete the template", c.DoOp("write: template delete", "DELETE", p("/api/v1/templates/%d", tpl.ID), nil), 200, 204)
	}

	// ---- files and media
	img := uniquePNG(v.u.Name)
	r = c.Upload("write: upload file", "/api/v1/files", nil, "qa-"+v.u.Name+".png", img)
	var f struct {
		Hash string `json:"hash"`
		Name string `json:"name"`
		Mime string `json:"mime"`
		Size int64  `json:"size"`
	}
	_ = r.JSON(&f)
	if v.want("files", "upload an image", r, 200, 201) && f.Hash != "" {
		r = c.DoOp("read: image", "GET", "/api/v1/files/"+f.Hash, nil)
		v.ok("files", "uploaded image is delivered unchanged", r.Status == 200 && bytes.Equal(r.Body, img), "status %d, %d bytes (sent %d)", r.Status, len(r.Body), len(img))
		r = c.DoOp("write: create attachment", "POST", "/api/v1/elements", map[string]any{"category_id": w.Pages, "type": "core.attachment", "lang": "de", "title": "QA file of " + v.u.Name,
			"body": map[string]any{"file": f, "description": "QA"}})
		var att idObj
		_ = r.JSON(&att)
		if v.want("files", "file as an element (attachment)", r, 201) {
			r = c.DoOp("read: attachment download", "GET", p("/api/v1/x/core.attachment/elements/%d/file", att.ID), nil)
			v.ok("files", "attachment download is the uploaded file", r.Status == 200 && bytes.Equal(r.Body, img), "status %d, %d bytes", r.Status, len(r.Body))
		}
	}
	r = c.Upload("write: media upload", "/api/v1/media", map[string]string{"meta": p(`{"category_id":%d,"title":"QA medium %s"}`, w.Pages, v.u.Name)}, "qa-m-"+v.u.Name+".png", img)
	var m idObj
	_ = r.JSON(&m)
	if v.want("media", "upload to the media library", r, 200, 201) && m.ID != 0 {
		v.want("media", "medium details", c.DoOp("read: medium", "GET", p("/api/v1/media/%d", m.ID), nil), 200)
		r = c.DoOp("read: medium download", "GET", p("/api/v1/media/%d/download", m.ID), nil)
		v.ok("media", "download is the uploaded file", r.Status == 200 && bytes.Equal(r.Body, img), "status %d, %d bytes", r.Status, len(r.Body))
		v.want("media", "view in the browser", c.DoOp("read: medium view", "GET", p("/api/v1/media/%d/view", m.ID), nil), 200)
		v.want("media", "change title and description", c.DoOp("write: medium update", "PATCH", p("/api/v1/media/%d", m.ID), map[string]any{"title": "QA medium (changed)", "alt": "one pixel"}), 200)
		v.want("media", "new version of the file", c.Upload("write: medium new version", p("/api/v1/media/%d/file", m.ID), nil, "qa-m2.png", uniquePNG(v.u.Name+"-v2")), 200, 201)
		r = c.DoOp("read: medium versions", "GET", p("/api/v1/media/%d/versions", m.ID), nil)
		var vs []json.RawMessage
		_ = r.JSON(&vs)
		v.ok("media", "both versions are kept", r.Status == 200 && len(vs) == 2, "status %d, %d versions: %s", r.Status, len(vs), r.Text(120))
		v.want("media", "where a medium is used", c.DoOp("read: medium usage", "GET", p("/api/v1/media/%d/usage", m.ID), nil), 200)
		v.want("media", "deleting a medium needs the right to manage (editors are refused)", c.DoOp("denied: medium delete", "DELETE", p("/api/v1/media/%d", m.ID), nil), 403)
	}

	// ---- blog, FAQ, calendar
	r = c.DoOp("write: blog post", "POST", p("/api/v1/x/content.blog/elements/%d/posts", w.Blog), map[string]any{"title": "QA post of " + v.u.Name, "lang": "de", "body": mdBody("Post")})
	v.want("element types", "blog: write a post", r, 201, 200)
	r = c.DoOp("write: faq question", "POST", p("/api/v1/x/content.faq/elements/%d/questions", w.FAQ), map[string]any{"title": "QA question of " + v.u.Name + "?", "lang": "de", "body": mdBody("Answer")})
	v.want("element types", "FAQ: add a question", r, 201, 200)
	start := time.Now().Add(time.Duration(48+v.n) * time.Hour).UTC().Truncate(time.Hour)
	r = c.DoOp("write: event", "POST", p("/api/v1/x/content.events/calendars/%d/events", w.Calendar), map[string]any{"title": "QA event of " + v.u.Name, "starts_at": start, "ends_at": start.Add(time.Hour),
		"tz": "Europe/Vienna", "rrule": "FREQ=WEEKLY;COUNT=4", "registration": true, "capacity": 5, "reminders": []int{60}})
	var ev idObj
	_ = r.JSON(&ev)
	if v.want("events", "create a recurring event", r, 201, 200) && ev.ID != 0 {
		v.want("events", "event details", c.DoOp("read: event", "GET", p("/api/v1/x/content.events/events/%d", ev.ID), nil), 200)
		v.want("events", "change the event", c.DoOp("write: event update", "PUT", p("/api/v1/x/content.events/events/%d", ev.ID), map[string]any{"title": "QA event (moved)", "starts_at": start.Add(time.Hour), "ends_at": start.Add(2 * time.Hour),
			"tz": "Europe/Vienna", "rrule": "FREQ=WEEKLY;COUNT=4", "registration": true, "capacity": 5}), 200)
		v.want("events", "event as iCalendar file", c.DoOp("read: event ical", "GET", p("/api/v1/x/content.events/events/%d/ical", ev.ID), nil), 200)
		v.want("events", "delete the event", c.DoOp("write: event delete", "DELETE", p("/api/v1/x/content.events/events/%d", ev.ID), nil), 200, 204)
	}
	v.want("events", "withdraw the registration of the shared event", c.DoOp("write: event unregister", "DELETE", p("/api/v1/x/content.events/events/%d/registrations?occurrence=%s", w.SharedEvent, w.EventDate), nil), 200, 204)

	// ---- workflow: start an own case, approve the shared one together
	r = c.DoOp("write: workflow start", "POST", p("/api/v1/x/workflow.process/elements/%d/instances", w.Workflow), map[string]any{"data": map[string]any{"subject": "QA " + v.u.Name}})
	var inst idObj
	_ = r.JSON(&inst)
	if v.want("workflow", "start a case", r, 201, 200) && inst.ID != 0 {
		v.want("workflow", "case with its history", c.DoOp("read: workflow case", "GET", p("/api/v1/x/workflow.process/instances/%d", inst.ID), nil), 200)
		v.want("workflow", "approve the own case (member of the responsible group)", c.DoOp("write: workflow action", "POST", p("/api/v1/x/workflow.process/instances/%d/actions", inst.ID), map[string]any{"action": "ok", "note": "QA"}), 200)
		v.want("workflow", "a finished case accepts no further action", c.DoOp("write: workflow action (finished)", "POST", p("/api/v1/x/workflow.process/instances/%d/actions", inst.ID), map[string]any{"action": "ok"}), 400, 403, 409)
	}
	joined[2] = true
	st.approve.wait()
	r = c.DoOp("write: workflow action (all at once)", "POST", p("/api/v1/x/workflow.process/instances/%d/actions", w.SharedCase), map[string]any{"action": "ok"})
	if r.Status == 200 {
		st.approveOK.Add(1)
	}
	v.want("workflow", "approving a shared case at the same moment never fails with a server error", r, 200, 400, 403, 409)

	// ---- everybody saves the same page at the same moment
	joined[1] = true
	st.conflict.wait()
	r = c.DoOp("write: save page (all at once)", "PUT", p("/api/v1/elements/%d", w.SharedPage), map[string]any{"lang": "de", "title": "QA shared page", "body": mdBody("saved by " + v.u.Name), "base_rev": st.conflictRev})
	switch r.Status {
	case 200:
		st.conflictOK.Add(1)
	case 409:
		st.conflict409.Add(1)
	}
	v.want("pages", "saving the same page at the same moment: saved or conflict, never an error", r, 200, 409)

	// ---- live editing
	v.live(st, joined)
}

// live joins the live editing room of the user, sends a change and a
// cursor with a forged name and expects the changes of the others.
func (v *vu) live(st *sharedState, joined *[5]bool) {
	w, c := v.w, v.c
	room := w.CollabPages[v.n%len(w.CollabPages)]
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	conn, status, err := dialCollab(ctx, c, room, "de", "")
	if !v.ok("live editing", "editor joins the live editing of a page (WebSocket)", err == nil, "status %d: %v", status, err) {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	got := make(chan []byte, 512)
	ctrl := make(chan []byte, 64)
	go func() {
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				close(got)
				return
			}
			if typ == websocket.MessageText {
				select {
				case ctrl <- data:
				default:
				}
				continue
			}
			select {
			case got <- data:
			default:
			}
		}
	}()
	select {
	case m := <-ctrl:
		v.ok("live editing", "server announces the people in the room", bytes.Contains(m, []byte(`"type":"peers"`)), "%s", m)
	case <-time.After(15 * time.Second):
		v.ok("live editing", "server announces the people in the room", false, "no message within 15 s")
	}
	joined[3] = true
	st.collabJoin.wait() // everybody is in a room
	time.Sleep(300 * time.Millisecond)
	me := uint64(v.u.ID) // #nosec G115 -- ids are positive
	werr := conn.Write(ctx, websocket.MessageBinary, awarenessMsg(me, 1, "Mallory (forged)"))
	if werr == nil {
		werr = conn.Write(ctx, websocket.MessageBinary, syncMsg(me, 200))
	}
	v.ok("live editing", "send a change and the cursor", werr == nil, "%v", werr)
	joined[4] = true
	st.collabSend.wait() // everybody has sent
	deadline := time.After(20 * time.Second)
	received := false
	for done := false; !done; {
		select {
		case m, ok := <-got:
			if !ok {
				done = true
				break
			}
			if s, age, ok := parseSync(m); ok && s != me {
				if !received {
					st.relayLatency.Add("live: change reaches another editor", age, 200)
				}
				received = true
			}
			if name, ok := parseAwarenessUser(m); ok && strings.Contains(name, "Mallory") {
				st.collabSpoof.Add(1)
			}
		case <-time.After(1500 * time.Millisecond):
			done = received
		case <-deadline:
			done = true
		}
	}
	if received {
		st.collabRecv.Add(1)
	}
	// a page saved while editing live: the notes element type saves by itself, pages are saved by the editor
	r := c.DoOp("write: save page (live room)", "PUT", fmt.Sprintf("/api/v1/elements/%d", room), map[string]any{"lang": "de", "title": "QA live page", "body": mdBody("live " + v.u.Name), "base_rev": 1})
	v.want("live editing", "saving from a live room: saved or conflict", r, 200, 409)
	_ = conn.Close(websocket.StatusNormalClosure, "")
}
