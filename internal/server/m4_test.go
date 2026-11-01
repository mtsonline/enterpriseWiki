package server_test

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/mail"
)

// capture is a mail transport that records messages.
type capture struct {
	mu   sync.Mutex
	msgs []mail.Envelope
	fail bool
}

func (c *capture) Name() string { return "capture" }
func (c *capture) Send(_ context.Context, e mail.Envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return fmt.Errorf("relay down")
	}
	c.msgs = append(c.msgs, e)
	return nil
}

// to returns the subjects (decoded loosely) per recipient.
func (c *capture) find(to, contains string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.msgs {
		for _, r := range m.Recipients {
			if r == to && strings.Contains(decodeQP(string(m.Data)), contains) {
				return true
			}
		}
	}
	return false
}

func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.msgs)
}

// decodeQP returns the headers (subject decoded) and the decoded body parts.
func decodeQP(s string) string {
	head, body, _ := strings.Cut(s, "\r\n\r\n")
	var out strings.Builder
	dec := new(mime.WordDecoder)
	for _, line := range strings.Split(head, "\r\n") {
		if d, err := dec.DecodeHeader(line); err == nil {
			line = d
		}
		out.WriteString(line + "\n")
	}
	b, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	out.Write(b)
	return out.String()
}

func (e *env) flushMail(t *testing.T) {
	t.Helper()
	if err := e.app.Workers.RunOnce(context.Background(), "mail"); err != nil {
		t.Fatal(err)
	}
}

func TestMailOutbox(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	// no transport configured: messages are held
	if err := e.app.Config.Update(func(c *config.Config) error { c.Mail.Transport = "none"; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := admin.do("POST", "/api/v1/admin/mail/test", map[string]string{"to": "a@example.com"}, nil); got != 400 {
		t.Fatalf("test without transport: %d", got)
	}
	id, err := e.app.Mail.Enqueue(context.Background(), e.app.DB, mail.Message{To: []string{"Bob <bob@example.com>"}, Subject: "Hi\r\nBcc: evil@x", Text: "x", Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	var list []struct {
		ID      int64    `json:"id"`
		Status  string   `json:"status"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
	}
	admin.must("GET", "/api/v1/admin/mail/outbox", nil, &list, 200)
	if len(list) != 1 || list[0].Status != "held" || list[0].To[0] != "bob@example.com" || strings.ContainsAny(list[0].Subject, "\r\n") {
		t.Fatalf("outbox %+v", list)
	}
	if _, err := e.app.Mail.Enqueue(context.Background(), e.app.DB, mail.Message{To: []string{"-oQ/tmp@x.com"}, Subject: "x", Text: "x"}); err == nil {
		t.Fatal("address starting with - accepted")
	}
	// settings through the API: the password is masked and kept
	var st struct {
		Config struct {
			Mail config.Mail `json:"mail"`
		} `json:"config"`
	}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"mail": map[string]any{"transport": "smtp", "from": "wiki@example.com",
		"smtp": map[string]any{"host": "127.0.0.1", "port": 2525, "security": "none", "username": "u", "password": "s3cret"}}}, nil, 200)
	admin.must("GET", "/api/v1/admin/settings", nil, &st, 200)
	if st.Config.Mail.SMTP.Password != "********" {
		t.Fatalf("password not masked: %q", st.Config.Mail.SMTP.Password)
	}
	m := st.Config.Mail
	m.FromName = "Wiki"
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"mail": m}, nil, 200)
	if e.app.Config.Get().Mail.SMTP.Password != "s3cret" || e.app.Config.Get().Mail.FromName != "Wiki" {
		t.Fatalf("password lost: %+v", e.app.Config.Get().Mail)
	}
	if got := admin.do("PUT", "/api/v1/admin/settings", map[string]any{"mail": map[string]any{"transport": "carrier-pigeon"}}, nil); got != 400 {
		t.Fatalf("invalid transport accepted: %d", got)
	}
	// with a transport, held messages are sent by the worker
	c := &capture{}
	e.app.Mail.SetTransport(c)
	e.flushMail(t)
	if !c.find("bob@example.com", "Subject: Hi Bcc: evil@x") {
		t.Fatalf("held message not sent or header injection: %+v", c.msgs)
	}
	// failures are retried later and reported
	c.fail = true
	if _, err := e.app.Mail.Enqueue(context.Background(), e.app.DB, mail.Message{To: []string{"x@example.com"}, Subject: "retry", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	admin.must("GET", "/api/v1/admin/mail/outbox?status=queued", nil, &list, 200)
	if len(list) != 1 {
		t.Fatalf("failed message not queued for retry: %+v", list)
	}
	var ms struct {
		Transport string         `json:"transport"`
		Counts    map[string]int `json:"counts"`
	}
	admin.must("GET", "/api/v1/admin/mail", nil, &ms, 200)
	if ms.Transport != "capture" || ms.Counts["sent"] != 1 || ms.Counts["queued"] != 1 {
		t.Fatalf("status %+v", ms)
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/admin/mail/outbox/%d", list[0].ID), nil, nil, 204)
	admin.must("POST", fmt.Sprintf("/api/v1/admin/mail/outbox/%d/retry", id), nil, nil, 404) // already sent
	admin.must("POST", "/api/v1/admin/jobs/mail/run", nil, nil, 204)
	if got := e.client().do("GET", "/api/v1/admin/mail/outbox", nil, nil); got != 401 {
		t.Fatalf("guest reads outbox: %d", got)
	}
}

// fakeSMTP is a minimal SMTP server (no TLS) for the SMTP transport test.
func fakeSMTP(t *testing.T) (addr string, got chan string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan string, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				w := func(s string) { fmt.Fprintf(c, "%s\r\n", s) }
				w("220 fake ESMTP")
				var log strings.Builder
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd := strings.ToUpper(strings.TrimSpace(line))
					log.WriteString(strings.TrimSpace(line) + "\n")
					switch {
					case strings.HasPrefix(cmd, "EHLO"):
						w("250-fake")
						w("250 AUTH PLAIN LOGIN")
					case strings.HasPrefix(cmd, "AUTH PLAIN"):
						w("235 ok")
					case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
						w("250 ok")
					case cmd == "DATA":
						w("354 go")
						for {
							l, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if l == ".\r\n" {
								break
							}
							log.WriteString(l)
						}
						w("250 queued")
					case cmd == "QUIT":
						w("221 bye")
						got <- log.String()
						return
					default:
						w("250 ok")
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), got
}

func TestSMTPTransport(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	addr, got := fakeSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	var p int
	fmt.Sscan(port, &p)
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"mail": map[string]any{"transport": "smtp", "from": "wiki@example.com", "from_name": "Wiki",
		"smtp": map[string]any{"host": host, "port": p, "security": "none", "username": "user", "password": "pw"}}}, nil, 200)
	admin.must("POST", "/api/v1/admin/mail/test", map[string]string{"to": "ops@example.com"}, nil, 200)
	select {
	case s := <-got:
		for _, want := range []string{"AUTH PLAIN", "MAIL FROM:<wiki@example.com>", "RCPT TO:<ops@example.com>", "From: \"Wiki\" <wiki@example.com>", "multipart/alternative"} {
			if !strings.Contains(s, want) {
				t.Errorf("SMTP session lacks %q:\n%s", want, s)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no SMTP session")
	}
	// STARTTLS required but not offered: clear error
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"mail": map[string]any{"transport": "smtp", "from": "wiki@example.com",
		"smtp": map[string]any{"host": host, "port": p, "security": "starttls"}}}, nil, 200)
	if code := admin.do("POST", "/api/v1/admin/mail/test", map[string]string{"to": "ops@example.com"}, nil); code != 400 {
		t.Fatalf("starttls downgrade: %d", code)
	}
}

type caseResp struct {
	ID       int64  `json:"id"`
	Step     string `json:"step"`
	Status   string `json:"status"`
	Outcome  string `json:"outcome"`
	Progress string `json:"progress"`
	Actions  []struct {
		ID string `json:"id"`
	} `json:"actions"`
	LastError string `json:"last_error"`
	Events    []struct {
		Action string `json:"action"`
		Label  string `json:"action_label"`
		Note   string `json:"note"`
	} `json:"events"`
}

func TestWorkflowStepTypes(t *testing.T) {
	e := setup(t)
	c := &capture{}
	e.app.Mail.SetTransport(c)
	if err := e.app.Config.Update(func(cf *config.Config) error {
		cf.Mail.From = "wiki@example.com"
		cf.Mail.AllowedFrom = []string{"*@hr.example.com"}
		cf.Server.BaseURL = "https://wiki.example.com"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var hook struct {
		sync.Mutex
		bodies []string
		fail   bool
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m := hmac.New(sha256.New, []byte("topsecret"))
		m.Write(b)
		hook.Lock()
		defer hook.Unlock()
		if r.Header.Get("X-Ewiki-Signature") != "sha256="+hex.EncodeToString(m.Sum(nil)) {
			http.Error(w, "bad signature", 401)
			return
		}
		if hook.fail {
			http.Error(w, "down", 503)
			return
		}
		hook.bodies = append(hook.bodies, string(b))
	}))
	defer srv.Close()
	if err := e.app.Config.Update(func(cf *config.Config) error {
		cf.Plugins.Macros.HTTPAllow = []string{srv.URL + "/"}
		cf.Plugins.Macros.HTTPAllowPrivate = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	var root cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Requests"}}, &root, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	var mgr idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "managers"}, &mgr, 200)
	users := map[string]idResp{}
	for _, u := range []struct{ name, email string }{{"ann", "ann@example.com"}, {"ben", "ben@example.com"}, {"cid", "cid@example.com"}} {
		var x idResp
		body := map[string]any{"username": u.name, "password": "Passw0rd!Passw0rd", "display_name": strings.ToUpper(u.name[:1]) + u.name[1:], "email": u.email}
		if u.name != "cid" {
			body["groups"] = []int64{mgr.ID}
		}
		admin.must("POST", "/api/v1/admin/users", body, &x, 201)
		users[u.name] = x
	}
	login := func(n string) *client { c := e.client(); c.login(n, "Passw0rd!Passw0rd"); return c }
	ann, ben, cid := login("ann"), login("ben"), login("cid")

	def := map[string]any{
		"description": "Purchase request",
		"fields": []map[string]any{
			{"id": "item", "label": "Item", "type": "text", "required": true},
			{"id": "amount", "label": "Amount", "type": "number", "required": true},
			{"id": "contact", "label": "Contact", "type": "email", "required": true},
		},
		"steps": []map[string]any{
			{"id": "confirm", "name": "Confirmation", "type": "email", "next": "check",
				"email": map[string]any{"to": "{{field.contact}}, starter", "from": "jobs@hr.example.com", "reply_to": "", "subject": "Request {{case.id}}: {{field.item}}",
					"body": "Hello {{starter.name}},\nwe received {{field.item}} ({{field.amount}} EUR).\n{{case.url}}"}},
			{"id": "check", "name": "Amount check", "type": "condition", "match": "all", "rules": []map[string]any{{"field": "amount", "op": "gt", "value": "1000"}},
				"next": "approve", "next_else": "auto_ok"},
			{"id": "approve", "name": "Four eyes", "type": "approval", "assignee": map[string]string{"type": "group", "value": "managers"}, "required": 0,
				"due_days": 2, "next": "export", "next_else": "rejected_mail"},
			{"id": "export", "name": "Send to ERP", "type": "webhook", "webhook": map[string]any{"url": srv.URL + "/erp", "secret": "topsecret"}, "next": "wait"},
			{"id": "wait", "name": "Delivery time", "type": "delay", "delay": map[string]any{"days": 3}, "next": "approved"},
			{"id": "auto_ok", "name": "Small amount", "type": "end", "outcome": "success"},
			{"id": "approved", "name": "Approved", "type": "end", "outcome": "success"},
			{"id": "rejected_mail", "name": "Rejection", "type": "email", "next": "rejected",
				"email": map[string]any{"to": "starter", "from": "spoof@evil.example", "subject": "Rejected: {{field.item}}", "body": "{{actor.name}} said: {{last.note}}"}},
			{"id": "rejected", "name": "Rejected", "type": "end", "outcome": "failure"},
		},
	}
	// loops of automatic steps are rejected at runtime; invalid definitions at save time
	bad := map[string]any{"fields": []any{}, "steps": []map[string]any{{"id": "c", "name": "C", "type": "condition", "rules": []map[string]any{{"field": "nope", "op": "equals"}}, "next": "c", "next_else": "c"}}}
	if got := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "workflow.process", "title": "Bad", "body": bad}, nil); got != 400 {
		t.Fatalf("condition on unknown field accepted: %d", got)
	}
	var el elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "workflow.process", "title": "Purchase", "body": def}, &el, 201)
	start := func(item string, amount float64) caseResp {
		var x caseResp
		cid.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", el.ID), map[string]any{"data": map[string]any{"item": item, "amount": amount, "contact": "buyer@example.com"}}, &x, 201)
		return x
	}
	// small amount: e-mail + condition (false) -> end
	small := start("Pens", 20)
	if small.Status != "done" || small.Step != "auto_ok" {
		t.Fatalf("small request: %+v", small)
	}
	e.flushMail(t)
	if !c.find("buyer@example.com", "Subject: Request") || !c.find("cid@example.com", "we received Pens (20 EUR)") ||
		!c.find("buyer@example.com", fmt.Sprintf("https://wiki.example.com/e/%d?case=%d", el.ID, small.ID)) {
		t.Fatalf("confirmation e-mail: %d messages", c.count())
	}
	if !c.find("buyer@example.com", "From: <jobs@hr.example.com>") {
		t.Fatalf("allowed sender not used: %+v", c.msgs)
	}
	// big amount: approval by all managers
	big := start("Laptop", 2500)
	if big.Status != "open" || big.Step != "approve" {
		t.Fatalf("big request: %+v", big)
	}
	e.flushMail(t)
	if !c.find("ann@example.com", "New task: Laptop") || !c.find("ben@example.com", "Four eyes") {
		t.Fatalf("task notification missing")
	}
	var x caseResp
	ann.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", big.ID), nil, &x, 200)
	if x.Progress != "0/2" || len(x.Actions) != 2 {
		t.Fatalf("approval state: %+v", x)
	}
	if got := cid.do("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", big.ID), map[string]any{"action": "approve"}, nil); got != 403 {
		t.Fatalf("starter approves: %d", got)
	}
	ann.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", big.ID), map[string]any{"action": "approve"}, &x, 200)
	if x.Step != "approve" || x.Progress != "1/2" {
		t.Fatalf("after first approval: %+v", x)
	}
	if got := ann.do("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", big.ID), map[string]any{"action": "approve"}, nil); got != 409 {
		t.Fatalf("double vote: %d", got)
	}
	hook.fail = true
	ben.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", big.ID), map[string]any{"action": "approve"}, &x, 200)
	if x.Step != "export" || x.Status != "open" {
		t.Fatalf("after second approval: %+v", x)
	}
	// webhook fails three times -> waits for a manager
	for i := 0; i < 3; i++ {
		if _, err := e.app.DB.Exec(context.Background(), `UPDATE plugin_workflow_instances SET wake_at=now() WHERE id=$1 AND wake_at IS NOT NULL`, big.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.app.Workers.RunOnce(context.Background(), "workflow.process.timers"); err != nil {
			t.Fatal(err)
		}
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", big.ID), nil, &x, 200)
	if x.Step != "export" || !strings.Contains(x.LastError, "503") || len(x.Actions) != 2 || x.Actions[0].ID != "_retry" {
		t.Fatalf("failed webhook: %+v", x)
	}
	hook.fail = false
	admin.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", big.ID), map[string]any{"action": "_retry"}, nil, 200)
	if err := e.app.Workers.RunOnce(context.Background(), "workflow.process.timers"); err != nil {
		t.Fatal(err)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", big.ID), nil, &x, 200)
	if x.Step != "wait" || len(hook.bodies) != 1 || !strings.Contains(hook.bodies[0], `"item":"Laptop"`) {
		t.Fatalf("webhook retry: %+v %v", x, hook.bodies)
	}
	// delay: nothing happens before the time, then the case continues
	if err := e.app.Workers.RunOnce(context.Background(), "workflow.process.timers"); err != nil {
		t.Fatal(err)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", big.ID), nil, &x, 200)
	if x.Step != "wait" {
		t.Fatalf("delay ended early: %+v", x)
	}
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE plugin_workflow_instances SET wake_at=now() - interval '1 minute' WHERE id=$1`, big.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Workers.RunOnce(context.Background(), "workflow.process.timers"); err != nil {
		t.Fatal(err)
	}
	cid.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", big.ID), nil, &x, 200)
	if x.Status != "done" || x.Outcome != "success" {
		t.Fatalf("after delay: %+v", x)
	}
	// rejection: one veto, e-mail with actor and note; the spoofed sender becomes Reply-To
	other := start("Yacht", 99999)
	ben.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", other.ID), map[string]any{"action": "reject", "note": "Too expensive"}, &x, 200)
	if x.Status != "done" || x.Step != "rejected" || x.Outcome != "failure" {
		t.Fatalf("after rejection: %+v", x)
	}
	e.flushMail(t)
	if !c.find("cid@example.com", "Ben said: Too expensive") || !c.find("cid@example.com", "Reply-To: <spoof@evil.example>") || c.find("cid@example.com", "From: <spoof@evil.example>") {
		t.Fatal("rejection e-mail wrong")
	}
	if !c.find("cid@example.com", "Completed: Yacht") {
		t.Fatal("starter not notified about the end")
	}
	// reminders: overdue approval reminds those who did not vote yet
	third := start("Desk", 1500)
	ann.must("POST", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", third.ID), map[string]any{"action": "approve"}, nil, 200)
	before := c.count()
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE plugin_workflow_instances SET step_entered_at=now() - interval '3 days' WHERE id=$1`, third.ID); err != nil {
		t.Fatal(err)
	}
	// the approval of ann must still count after moving the step start back
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE plugin_workflow_events SET at=now() - interval '2 days' WHERE instance_id=$1 AND action='approve'`, third.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Workers.RunOnce(context.Background(), "workflow.process.timers"); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	if !c.find("ben@example.com", "Reminder: Desk") || c.find("ann@example.com", "Reminder: Desk") {
		t.Fatalf("reminder: %d new messages", c.count()-before)
	}
	// notification preferences
	var prefs struct {
		Prefs map[string]bool `json:"prefs"`
	}
	ben.must("PUT", "/api/v1/me/notifications", map[string]bool{"tasks": false}, nil, 204)
	ben.must("GET", "/api/v1/me/notifications", nil, &prefs, 200)
	if prefs.Prefs["tasks"] || !prefs.Prefs["comments"] {
		t.Fatalf("prefs %+v", prefs)
	}
	start("Chair", 2000)
	e.flushMail(t)
	if c.find("ben@example.com", "New task: Chair") || !c.find("ann@example.com", "New task: Chair") {
		t.Fatal("preference ignored")
	}
	var meta struct {
		Placeholders   []string `json:"placeholders"`
		MailConfigured bool     `json:"mail_configured"`
	}
	ann.must("GET", "/api/v1/x/workflow.process/meta", nil, &meta, 200)
	if !meta.MailConfigured || len(meta.Placeholders) < 10 {
		t.Fatalf("meta %+v", meta)
	}
}

func TestCommentNotifications(t *testing.T) {
	e := setup(t)
	c := &capture{}
	e.app.Mail.SetTransport(c)
	admin := e.client()
	admin.login("admin", adminPW)
	var root, secret cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &root, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": root.ID, "titles": map[string]string{"en": "Board"}}, &secret, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root.ID), map[string]any{"subject_type": "authenticated", "permissions": 7, "inherit": false}, nil, 201)
	for _, n := range []string{"amy", "bo"} {
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": n, "password": "Passw0rd!Passw0rd", "display_name": n, "email": n + "@example.com"}, nil, 201)
	}
	amy, bo := e.client(), e.client()
	amy.login("amy", "Passw0rd!Passw0rd")
	bo.login("bo", "Passw0rd!Passw0rd")
	var el elem
	amy.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Plan", "body": htmlBody("<p>x</p>")}, &el, 201)
	var c1 struct {
		ID int64 `json:"id"`
	}
	bo.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", el.ID), map[string]any{"body": "Looks good"}, &c1, 201)
	amy.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", el.ID), map[string]any{"body": "Thanks!", "parent_id": c1.ID}, nil, 201)
	e.flushMail(t)
	if !c.find("amy@example.com", "bo (bo@local) commented on") || !c.find("bo@example.com", "amy (amy@local) commented on") || c.find("amy@example.com", "Thanks!") {
		t.Fatalf("comment notifications: %d", c.count())
	}
	// a page in a hidden category: commenters' notifications only go to users who can see it
	var hidden elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "core.html", "title": "Secret", "body": htmlBody("<p>x</p>")}, &hidden, 201)
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE elements SET created_by=(SELECT id FROM users WHERE username='bo') WHERE id=$1`, hidden.ID); err != nil {
		t.Fatal(err)
	}
	n := c.count()
	admin.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", hidden.ID), map[string]any{"body": "internal"}, nil, 201)
	e.flushMail(t)
	if c.count() != n {
		t.Fatal("notification about a page the recipient cannot see")
	}
}

// ---------------------------------------------------------------- external search

// fakeEngine implements the parts of the Meilisearch and OpenSearch APIs the
// plugin uses (substring search).
type fakeEngine struct {
	mu   sync.Mutex
	docs map[string]map[string]any
	down bool
	auth string
}

func (f *fakeEngine) match(q string, d map[string]any, tags []string) bool {
	text := strings.ToLower(fmt.Sprint(d["title"], " ", d["text"], " ", d["tags"]))
	for _, w := range strings.Fields(strings.ToLower(q)) {
		if !strings.Contains(text, w) {
			return false
		}
	}
	for _, t := range tags {
		found := false
		for _, x := range d["tags"].([]any) {
			found = found || x == t
		}
		if !found {
			return false
		}
	}
	return true
}

func (f *fakeEngine) sorted() []map[string]any {
	ids := make([]string, 0, len(f.docs))
	for id := range f.docs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []map[string]any{}
	for _, id := range ids {
		out = append(out, f.docs[id])
	}
	return out
}

func (f *fakeEngine) deleteElements(ids []any) {
	for k, d := range f.docs {
		for _, id := range ids {
			if fmt.Sprint(d["element_id"]) == fmt.Sprint(id) {
				delete(f.docs, k)
			}
		}
	}
}

func (f *fakeEngine) meili() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.down {
			http.Error(w, "down", 503)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+f.auth {
			http.Error(w, `{"message":"invalid key"}`, 403)
			return
		}
		task := func() { w.WriteHeader(202); w.Write([]byte(`{"taskUid":1}`)) }
		p := r.URL.Path
		var body map[string]any
		var arr []map[string]any
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		_ = json.Unmarshal(data, &arr)
		switch {
		case p == "/health" || strings.HasPrefix(p, "/tasks/"):
			w.Write([]byte(`{"status":"succeeded"}`))
		case r.Method == "DELETE" && strings.HasPrefix(p, "/indexes/"):
			f.docs = map[string]map[string]any{}
			task()
		case r.Method == "POST" && p == "/indexes", r.Method == "PATCH":
			task()
		case r.Method == "POST" && strings.HasSuffix(p, "/documents/delete"):
			ids := strings.Trim(strings.TrimPrefix(body["filter"].(string), "element_id IN "), "[]")
			list := []any{}
			for _, s := range strings.Split(ids, ",") {
				list = append(list, strings.TrimSpace(s))
			}
			f.deleteElements(list)
			task()
		case r.Method == "POST" && strings.HasSuffix(p, "/documents"):
			for _, d := range arr {
				f.docs[d["id"].(string)] = d
			}
			task()
		case strings.HasSuffix(p, "/stats"):
			fmt.Fprintf(w, `{"numberOfDocuments":%d}`, len(f.docs))
		case strings.HasSuffix(p, "/search"):
			tags := []string{}
			if fs, ok := body["filter"].(string); ok {
				for _, part := range strings.Split(fs, " AND ") {
					tags = append(tags, strings.Trim(strings.TrimPrefix(part, "tags = "), `"`))
				}
			}
			hits := []map[string]any{}
			for _, d := range f.sorted() {
				if f.match(body["q"].(string), d, tags) {
					hits = append(hits, map[string]any{"element_id": d["element_id"], "lang": d["lang"], "title": d["title"], "_rankingScore": 0.9,
						"_formatted": map[string]any{"text": "\x01" + body["q"].(string) + "\x02 <b>bold</b>"}})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"hits": hits})
		default:
			w.Write([]byte(`{}`))
		}
	})
}

func (f *fakeEngine) opensearch() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.down {
			http.Error(w, "down", 503)
			return
		}
		if u, p, _ := r.BasicAuth(); u != "elastic" || p != f.auth {
			http.Error(w, "unauthorized", 401)
			return
		}
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		p := r.URL.Path
		switch {
		case p == "/":
			w.Write([]byte(`{"version":{"number":"2.15.0"}}`))
		case r.Method == "DELETE":
			f.docs = map[string]map[string]any{}
			w.Write([]byte(`{}`))
		case r.Method == "PUT":
			w.Write([]byte(`{"acknowledged":true}`))
		case p == "/_bulk":
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			for i := 0; i+1 < len(lines); i += 2 {
				var meta map[string]map[string]any
				var doc map[string]any
				json.Unmarshal([]byte(lines[i]), &meta)
				json.Unmarshal([]byte(lines[i+1]), &doc)
				f.docs[meta["index"]["_id"].(string)] = doc
			}
			w.Write([]byte(`{"errors":false,"items":[]}`))
		case strings.HasSuffix(p, "/_delete_by_query"):
			q := body["query"].(map[string]any)["terms"].(map[string]any)["element_id"].([]any)
			f.deleteElements(q)
			w.Write([]byte(`{}`))
		case strings.HasSuffix(p, "/_count"):
			fmt.Fprintf(w, `{"count":%d}`, len(f.docs))
		case strings.HasSuffix(p, "/_search"):
			b := body["query"].(map[string]any)["bool"].(map[string]any)
			q := b["must"].([]any)[0].(map[string]any)["multi_match"].(map[string]any)["query"].(string)
			tags := []string{}
			for _, x := range b["filter"].([]any) {
				tags = append(tags, x.(map[string]any)["term"].(map[string]any)["tags"].(string))
			}
			hits := []map[string]any{}
			for _, d := range f.sorted() {
				if f.match(q, d, tags) {
					hits = append(hits, map[string]any{"_score": 3.2, "_source": map[string]any{"element_id": d["element_id"], "lang": d["lang"], "title": d["title"]},
						"highlight": map[string]any{"text": []string{"…\x01" + q + "\x02…"}}})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"hits": map[string]any{"hits": hits}})
		default:
			w.Write([]byte(`{}`))
		}
	})
}

func TestExternalSearch(t *testing.T) {
	for _, engine := range []string{"meilisearch", "opensearch"} {
		t.Run(engine, func(t *testing.T) {
			e := setup(t)
			admin := e.client()
			admin.login("admin", adminPW)
			f := &fakeEngine{docs: map[string]map[string]any{}, auth: "k3y"}
			var h http.Handler = f.meili()
			settings := map[string]any{"engine": engine, "api_key": "k3y"}
			if engine == "opensearch" {
				h = f.opensearch()
				settings = map[string]any{"engine": engine, "username": "elastic", "password": "k3y"}
			}
			srv := httptest.NewServer(h)
			defer srv.Close()
			settings["url"] = srv.URL
			var root, hidden cat
			admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Docs"}}, &root, 201)
			admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": root.ID, "titles": map[string]string{"en": "Hidden"}}, &hidden, 201)
			var e1, e2 elem
			admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Zebra guide", "tags": []string{"animals"}, "body": htmlBody("<p>All about zebras</p>")}, &e1, 201)
			admin.must("POST", "/api/v1/elements", map[string]any{"category_id": hidden.ID, "type": "core.html", "title": "Secret zebra", "body": htmlBody("<p>zebra plans</p>")}, &e2, 201)
			admin.must("PUT", "/api/v1/admin/settings", map[string]any{"plugin_settings": map[string]any{"search.external": settings}}, nil, 200)
			run := func() {
				if err := e.app.Workers.RunOnce(context.Background(), "search.external.sync"); err != nil {
					t.Fatal(err)
				}
			}
			var res struct {
				Engine   string `json:"engine"`
				Elements []struct {
					ElementID int64  `json:"element_id"`
					Snippet   string `json:"snippet"`
				} `json:"elements"`
			}
			run()
			if len(f.docs) != 2 {
				t.Fatalf("index after rebuild: %d docs", len(f.docs))
			}
			var st struct {
				Active          bool  `json:"active"`
				EngineDocuments int64 `json:"engine_documents"`
			}
			admin.must("GET", "/api/v1/x/search.external/status", nil, &st, 200)
			if !st.Active || st.EngineDocuments != 2 {
				t.Fatalf("status %+v", st)
			}
			admin.must("POST", "/api/v1/x/search.external/test", nil, nil, 200)
			// guest only sees the first level (default guest right): the hidden page is filtered by the core
			guest := e.client()
			guest.must("GET", "/api/v1/search?q=zebra", nil, &res, 200)
			if res.Engine != engine || len(res.Elements) != 1 || res.Elements[0].ElementID != e1.ID || !strings.Contains(res.Elements[0].Snippet, "<mark>zebra</mark>") ||
				strings.Contains(res.Elements[0].Snippet, "<b>") {
				t.Fatalf("guest search: %+v", res)
			}
			admin.must("GET", "/api/v1/search?q=zebra&tags=animals", nil, &res, 200)
			if len(res.Elements) != 1 {
				t.Fatalf("tag filter: %+v", res)
			}
			// changes follow: new page, trash
			var e3 elem
			admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Okapi", "body": htmlBody("<p>okapi facts</p>")}, &e3, 201)
			admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", e1.ID), nil, nil, 204)
			run()
			admin.must("GET", "/api/v1/search?q=okapi", nil, &res, 200)
			if res.Engine != engine || len(res.Elements) != 1 || res.Elements[0].ElementID != e3.ID {
				t.Fatalf("new page not indexed: %+v", res)
			}
			for _, d := range f.docs {
				if fmt.Sprint(d["element_id"]) == fmt.Sprint(e1.ID) {
					t.Fatal("trashed page still in the engine")
				}
			}
			// engine down: the built-in search answers
			f.mu.Lock()
			f.down = true
			f.mu.Unlock()
			admin.must("GET", "/api/v1/search?q=okapi", nil, &res, 200)
			if res.Engine != "postgres" || len(res.Elements) != 1 {
				t.Fatalf("fallback: %+v", res)
			}
			f.mu.Lock()
			f.down = false
			f.mu.Unlock()
			// rebuild on demand
			admin.must("POST", "/api/v1/x/search.external/reindex", nil, nil, 204)
			time.Sleep(300 * time.Millisecond)
			run()
			if len(f.docs) != 2 {
				t.Fatalf("after reindex: %d docs", len(f.docs))
			}
			// disabling the plugin switches back to PostgreSQL
			admin.must("POST", "/api/v1/admin/plugins/search.external/disable", nil, nil, 204)
			admin.must("GET", "/api/v1/search?q=okapi", nil, &res, 200)
			if res.Engine != "postgres" {
				t.Fatalf("disabled plugin still used: %s", res.Engine)
			}
		})
	}
}
