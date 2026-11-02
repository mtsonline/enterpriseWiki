package server_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/mail"
)

// smtpFake is an SMTP server with per-recipient answers.
type smtpFake struct {
	addr  string
	conns atomic.Int64
	mu    sync.Mutex
	// rcpt answers by address prefix, e.g. "temp" -> "451 try later"
	answers map[string]string
	data    string // answer to the end of DATA ("" = 250)
	got     map[string]int
	froms   []string
}

func newSMTPFake(t *testing.T) *smtpFake {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := &smtpFake{addr: ln.Addr().String(), answers: map[string]string{}, got: map[string]int{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			f.conns.Add(1)
			go f.serve(conn)
		}
	}()
	return f
}

func (f *smtpFake) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	w := func(s string) { fmt.Fprintf(c, "%s\r\n", s) }
	w("220 fake ESMTP")
	var rcpts []string
	var from string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(line)
		up := strings.ToUpper(cmd)
		switch {
		case strings.HasPrefix(up, "EHLO"):
			w("250-fake")
			w("250 8BITMIME")
		case strings.HasPrefix(up, "MAIL FROM:"):
			from = strings.Trim(cmd[10:], "<> ")
			rcpts = nil
			w("250 ok")
		case strings.HasPrefix(up, "RCPT TO:"):
			addr := strings.Trim(cmd[8:], "<> ")
			f.mu.Lock()
			ans := ""
			for p, a := range f.answers {
				if strings.HasPrefix(addr, p) {
					ans = a
				}
			}
			f.mu.Unlock()
			if ans != "" {
				w(ans)
				continue
			}
			rcpts = append(rcpts, addr)
			w("250 ok")
		case up == "DATA":
			w("354 go")
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
			}
			f.mu.Lock()
			ans := f.data
			if ans == "" {
				for _, x := range rcpts {
					f.got[x]++
				}
				f.froms = append(f.froms, from)
				ans = "250 queued"
			}
			f.mu.Unlock()
			w(ans)
		case up == "RSET":
			rcpts = nil
			w("250 ok")
		case up == "QUIT":
			w("221 bye")
			return
		default:
			w("250 ok")
		}
	}
}

func (f *smtpFake) count(addr string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.got[addr]
}

func useSMTPFake(t *testing.T, e *env, f *smtpFake, mod func(*config.Config)) {
	host, port, _ := net.SplitHostPort(f.addr)
	var p int
	fmt.Sscan(port, &p)
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Mail.Transport, c.Mail.From = "smtp", "wiki@example.com"
		c.Mail.SMTP = config.SMTP{Host: host, Port: p, Security: "none"}
		if mod != nil {
			mod(c)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMailDeliveryPerRecipient(t *testing.T) {
	e := setup(t)
	f := newSMTPFake(t)
	useSMTPFake(t, e, f, nil)
	f.answers["temp"] = "451 4.7.1 greylisted, try later"
	f.answers["bad"] = "550 5.1.1 no such user"
	admin := e.client()
	admin.login("admin", adminPW)
	var owner idResp
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "olga", "password": "Passw0rd!Passw0rd", "display_name": "Olga", "email": "olga@example.com"}, &owner, 201)
	ctx := context.Background()
	id, err := e.app.Mail.Enqueue(ctx, e.app.DB, mail.Message{To: []string{"ok@example.com", "temp@example.com"}, Cc: []string{"bad@example.com"},
		Subject: "Hello", Text: "x", Owner: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	var m struct {
		Status     string `json:"status"`
		Recipients []struct {
			Addr     string `json:"addr"`
			Status   string `json:"status"`
			LastCode int    `json:"last_code"`
		} `json:"recipients"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/admin/mail/outbox/%d", id), nil, &m, 200)
	st := map[string]string{}
	for _, r := range m.Recipients {
		st[r.Addr] = fmt.Sprintf("%s/%d", r.Status, r.LastCode)
	}
	if m.Status != "queued" || st["ok@example.com"] != "sent/250" || st["temp@example.com"] != "pending/451" || st["bad@example.com"] != "failed/550" {
		t.Fatalf("after first attempt: %s %v", m.Status, st)
	}
	if f.count("ok@example.com") != 1 {
		t.Fatal("ok recipient not delivered once")
	}
	// the owner gets a report about the permanent failure
	if f.count("olga@example.com") != 1 {
		t.Fatal("no failure report to the owner")
	}
	// temporary failures stay queued (also after many attempts)
	for i := 0; i < 12; i++ {
		if _, err := e.app.DB.Exec(ctx, `UPDATE mail_recipients SET next_attempt_at=now() WHERE status='pending'; UPDATE mail_outbox SET next_attempt_at=now() WHERE status='queued'`); err != nil {
			t.Fatal(err)
		}
		e.flushMail(t)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/admin/mail/outbox/%d", id), nil, &m, 200)
	if m.Status != "queued" {
		t.Fatalf("temporary failure given up: %+v", m)
	}
	delete(f.answers, "temp")
	if _, err := e.app.DB.Exec(ctx, `UPDATE mail_recipients SET next_attempt_at=now() WHERE status='pending'; UPDATE mail_outbox SET next_attempt_at=now() WHERE status='queued'`); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	admin.must("GET", fmt.Sprintf("/api/v1/admin/mail/outbox/%d", id), nil, &m, 200)
	if m.Status != "partial" || f.count("temp@example.com") != 1 || f.count("ok@example.com") != 1 {
		t.Fatalf("after recovery: %+v ok=%d", m, f.count("ok@example.com"))
	}
	// 5xx for the whole message (DATA rejected)
	f.data = "554 5.6.0 message rejected"
	id2, _ := e.app.Mail.Enqueue(ctx, e.app.DB, mail.Message{To: []string{"x@example.com"}, Subject: "Spam?", Text: "x", Owner: -1})
	e.flushMail(t)
	admin.must("GET", fmt.Sprintf("/api/v1/admin/mail/outbox/%d", id2), nil, &m, 200)
	if m.Status != "failed" {
		t.Fatalf("message rejected at DATA: %+v", m)
	}
	f.data = ""
	// retry of failed recipients
	f.answers = map[string]string{}
	admin.must("POST", fmt.Sprintf("/api/v1/admin/mail/outbox/%d/retry", id2), nil, nil, 204)
	e.flushMail(t)
	admin.must("GET", fmt.Sprintf("/api/v1/admin/mail/outbox/%d", id2), nil, &m, 200)
	if m.Status != "sent" {
		t.Fatalf("retry: %+v", m)
	}
}

func TestMailThroughputAndLeases(t *testing.T) {
	e := setup(t)
	f := newSMTPFake(t)
	useSMTPFake(t, e, f, func(c *config.Config) { c.Mail.Concurrency = 4 })
	ctx := context.Background()
	const n = 1500
	if _, err := e.app.DB.Exec(ctx, `INSERT INTO mail_outbox(status, from_addr, to_addrs, subject, body_text) SELECT 'queued', 'wiki@example.com', ARRAY['u'||g||'@example.com'], 'n'||g, 'body' FROM generate_series(1, $1::int) g`, n); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.DB.Exec(ctx, `INSERT INTO mail_recipients(message_id, addr) SELECT id, to_addrs[1] FROM mail_outbox`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	e.flushMail(t)
	took := time.Since(start)
	var sent int
	if err := e.app.DB.QueryRow(ctx, `SELECT count(*) FROM mail_recipients WHERE status='sent'`).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if sent != n {
		t.Fatalf("%d of %d delivered", sent, n)
	}
	if c := f.conns.Load(); c > 8 {
		t.Fatalf("connections are not reused: %d connections", c)
	}
	t.Logf("%d messages in %s (%.0f/s) over %d connections", n, took, float64(n)/took.Seconds(), f.conns.Load())
	// a message claimed by a worker that crashed is taken over after the lease
	id, _ := e.app.Mail.Enqueue(ctx, e.app.DB, mail.Message{To: []string{"late@example.com"}, Subject: "lease", Text: "x"})
	if _, err := e.app.DB.Exec(ctx, `UPDATE mail_outbox SET lease_until=now() + interval '10 minutes' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	if f.count("late@example.com") != 0 {
		t.Fatal("leased message sent twice")
	}
	if _, err := e.app.DB.Exec(ctx, `UPDATE mail_outbox SET lease_until=now() - interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
	if f.count("late@example.com") != 1 {
		t.Fatal("expired lease not taken over")
	}
}

func TestMailDisabled(t *testing.T) {
	e := setup(t)
	off := false
	if err := e.app.Config.Update(func(c *config.Config) error { c.Mail.Enabled = &off; return nil }); err != nil {
		t.Fatal(err)
	}
	id, err := e.app.Mail.Enqueue(context.Background(), e.app.DB, mail.Message{To: []string{"a@example.com"}, Subject: "x", Text: "x"})
	if err != nil || id != 0 {
		t.Fatalf("enqueued while disabled: %d %v", id, err)
	}
	var n int
	_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM mail_outbox`).Scan(&n)
	if n != 0 {
		t.Fatal("outbox not empty")
	}
	admin := e.client()
	admin.login("admin", adminPW)
	if got := admin.do("POST", "/api/v1/admin/mail/test", map[string]string{"to": "a@example.com"}, nil); got != 400 {
		t.Fatalf("test mail while disabled: %d", got)
	}
}

// imapFake serves one mailbox with the given messages.
func imapFake(t *testing.T, msgs [][]byte) (addr string, seen *sync.Map) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	seen = &sync.Map{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				fmt.Fprint(c, "* OK fake IMAP ready\r\n")
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					parts := strings.SplitN(strings.TrimSpace(line), " ", 3)
					tag, cmd := parts[0], strings.ToUpper(parts[1])
					rest := ""
					if len(parts) > 2 {
						rest = parts[2]
					}
					switch {
					case cmd == "LOGIN":
						if rest != `"bounce" "pw"` {
							fmt.Fprintf(c, "%s NO bad login\r\n", tag)
							continue
						}
					case cmd == "SELECT":
						fmt.Fprintf(c, "* %d EXISTS\r\n", len(msgs))
					case cmd == "UID" && strings.HasPrefix(rest, "SEARCH"):
						ids := []string{}
						for i := range msgs {
							if _, ok := seen.Load(i + 1); !ok {
								ids = append(ids, fmt.Sprint(i+1))
							}
						}
						fmt.Fprintf(c, "* SEARCH %s\r\n", strings.Join(ids, " "))
					case cmd == "UID" && strings.HasPrefix(rest, "FETCH"):
						var uid int
						fmt.Sscanf(rest, "FETCH %d", &uid)
						m := msgs[uid-1]
						fmt.Fprintf(c, "* %d FETCH (UID %d BODY[] {%d}\r\n%s)\r\n", uid, uid, len(m), m)
					case cmd == "UID" && strings.HasPrefix(rest, "STORE"):
						var uid int
						fmt.Sscanf(rest, "STORE %d", &uid)
						seen.Store(uid, true)
					case cmd == "LOGOUT":
						fmt.Fprintf(c, "* BYE\r\n%s OK bye\r\n", tag)
						return
					}
					fmt.Fprintf(c, "%s OK done\r\n", tag)
				}
			}(c)
		}
	}()
	return ln.Addr().String(), seen
}

func TestMailBounces(t *testing.T) {
	e := setup(t)
	f := newSMTPFake(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var owner idResp
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "olga", "password": "Passw0rd!Passw0rd", "display_name": "Olga", "email": "olga@example.com"}, &owner, 201)
	ctx := context.Background()
	useSMTPFake(t, e, f, func(c *config.Config) { c.Mail.Bounce = config.Bounce{Address: "bounces@example.com", VERP: true} })
	id, _ := e.app.Mail.Enqueue(ctx, e.app.DB, mail.Message{To: []string{"gone@example.com", "fine@example.com"}, Subject: "News", Text: "x", Owner: owner.ID})
	e.flushMail(t)
	f.mu.Lock()
	froms := append([]string{}, f.froms...)
	f.mu.Unlock()
	if len(froms) != 2 || !strings.HasPrefix(froms[0], fmt.Sprintf("bounces+m%dr", id)) {
		t.Fatalf("VERP envelope senders: %v", froms)
	}
	var rid int64
	if err := e.app.DB.QueryRow(ctx, `SELECT id FROM mail_recipients WHERE addr='gone@example.com'`).Scan(&rid); err != nil {
		t.Fatal(err)
	}
	dsn := strings.ReplaceAll(fmt.Sprintf(`From: MAILER-DAEMON@example.net
To: bounces+m%dr%d@example.com
Subject: Undelivered Mail Returned to Sender
MIME-Version: 1.0
Content-Type: multipart/report; report-type=delivery-status; boundary="B"

--B
Content-Type: text/plain

Your message could not be delivered.
--B
Content-Type: message/delivery-status

Reporting-MTA: dns; mx.example.net

Final-Recipient: rfc822; gone@example.com
Action: failed
Status: 5.1.1
Diagnostic-Code: smtp; 550 5.1.1 user unknown

--B
Content-Type: text/rfc822-headers

X-Ewiki-Message: %d
Subject: News

--B--
`, id, rid, id), "\n", "\r\n")
	delayed := strings.ReplaceAll(`From: MAILER-DAEMON@example.net
To: bounces@example.com
Subject: Delayed Mail (still being retried)
Content-Type: multipart/report; report-type=delivery-status; boundary="C"

--C
Content-Type: message/delivery-status

Final-Recipient: rfc822; fine@example.com
Action: delayed
Status: 4.4.1

--C--
`, "\n", "\r\n")
	addr, seen := imapFake(t, [][]byte{[]byte(dsn), []byte(delayed)})
	host, port, _ := net.SplitHostPort(addr)
	var p int
	fmt.Sscan(port, &p)
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Mail.Bounce.IMAPHost, c.Mail.Bounce.IMAPPort, c.Mail.Bounce.Security = host, p, "none"
		c.Mail.Bounce.Username, c.Mail.Bounce.Password = "bounce", "pw"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	admin.must("POST", "/api/v1/admin/jobs/mail.bounces/run", nil, nil, 204)
	var m struct {
		Status     string `json:"status"`
		Recipients []struct {
			Addr    string `json:"addr"`
			Status  string `json:"status"`
			Bounced bool   `json:"bounced"`
		} `json:"recipients"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/admin/mail/outbox/%d", id), nil, &m, 200)
	if m.Status != "partial" {
		t.Fatalf("after bounce: %+v", m)
	}
	for _, r := range m.Recipients {
		if (r.Addr == "gone@example.com") != (r.Status == "failed" && r.Bounced) {
			t.Fatalf("recipient %+v", r)
		}
	}
	if _, ok := seen.Load(1); !ok {
		t.Fatal("bounce not marked as read")
	}
	if _, ok := seen.Load(2); !ok {
		t.Fatal("delay notice not marked as read")
	}
	e.flushMail(t)
	if f.count("olga@example.com") != 1 {
		t.Fatal("owner not informed about the bounce")
	}
}
