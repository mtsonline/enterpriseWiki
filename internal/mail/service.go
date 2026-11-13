package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Service is the mail service.
type Service struct {
	pool *pgxpool.Pool
	cfg  *config.Store
	log  *slog.Logger
	// override is used by tests to capture messages.
	override Transport
	// T translates texts of failure reports (set by the application).
	T func(ctx context.Context, lang, key string, params map[string]string) string
	// Kick asks the worker to send now (set by the application).
	Kick func()

	signerMu sync.RWMutex
	signer   Signer
}

// New creates the service.
func New(pool *pgxpool.Pool, cfg *config.Store, log *slog.Logger) *Service {
	return &Service{pool: pool, cfg: cfg, log: log}
}

// SetTransport replaces the configured transport (tests).
func (s *Service) SetTransport(t Transport) { s.override = t }

// Transport returns the effective transport or nil if messages are kept in
// the outbox ("held") because no way of sending is configured.
func (s *Service) Transport() Transport {
	if s.override != nil {
		return s.override
	}
	c := s.cfg.Get().Mail
	sm := c.Sendmail
	if sm == "" {
		sm = DefaultSendmail
	}
	switch c.Transport {
	case "smtp":
		if c.SMTP.Host != "" {
			return smtpTransport{c.SMTP}
		}
	case "sendmail":
		return sendmailTransport{sm}
	case "log":
		return logTransport{s}
	case "none":
		return nil
	default: // auto
		if c.SMTP.Host != "" {
			return smtpTransport{c.SMTP}
		}
		if executable(sm) {
			return sendmailTransport{sm}
		}
	}
	return nil
}

// Status describes the mail setup and the queue for the admin page.
type Status struct {
	Enabled   bool           `json:"enabled"`
	Transport string         `json:"transport"` // effective: smtp, sendmail, log or "" (held)
	From      string         `json:"from"`
	Counts    map[string]int `json:"counts"`     // messages by status
	Delivery  map[string]int `json:"recipients"` // recipients by status
	// OldestPending is the creation time of the oldest undelivered recipient.
	OldestPending *time.Time `json:"oldest_pending"`
	Bounces       bool       `json:"bounces"` // bounce mailbox configured
	Problem       string     `json:"problem,omitempty"`
}

// GetStatus reports the effective transport and the queue.
func (s *Service) GetStatus(ctx context.Context) (*Status, error) {
	c := s.cfg.Get().Mail
	st := &Status{Enabled: c.IsEnabled(), From: c.From, Bounces: c.Bounce.IMAPHost != "",
		Counts: map[string]int{"queued": 0, "sent": 0, "failed": 0, "partial": 0, "held": 0}, Delivery: map[string]int{"pending": 0, "sent": 0, "failed": 0}}
	if t := s.Transport(); t != nil {
		st.Transport = t.Name()
	} else {
		st.Problem = "no mail server configured: messages are kept in the outbox"
	}
	if st.From == "" {
		st.Problem = "mail.from is not set"
	}
	if !st.Enabled {
		st.Problem = "e-mail is switched off (mail.enabled: false)"
	}
	for _, q := range []struct {
		sql string
		m   map[string]int
	}{{`SELECT status, count(*) FROM mail_outbox GROUP BY status`, st.Counts}, {`SELECT status, count(*) FROM mail_recipients GROUP BY status`, st.Delivery}} {
		rows, err := s.pool.Query(ctx, q.sql)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var k string
			var n int
			if err := rows.Scan(&k, &n); err != nil {
				rows.Close()
				return nil, err
			}
			q.m[k] = n
		}
		rows.Close()
	}
	if err := s.pool.QueryRow(ctx, `SELECT min(o.created_at) FROM mail_recipients r JOIN mail_outbox o ON o.id=r.message_id WHERE r.status='pending'`).
		Scan(&st.OldestPending); err != nil {
		return nil, err
	}
	return st, nil
}

// SenderAllowed reports whether addr may be used as From (the configured
// sender or one of mail.allowed_from, which may contain *@domain).
func (s *Service) SenderAllowed(addr string) bool {
	c := s.cfg.Get().Mail
	addr = strings.ToLower(addr)
	if addr == strings.ToLower(c.From) {
		return true
	}
	for _, a := range c.AllowedFrom {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == addr {
			return true
		}
		if strings.HasPrefix(a, "*@") && strings.HasSuffix(addr, a[1:]) && strings.Count(addr, "@") == 1 {
			return true
		}
	}
	return false
}

// Enabled reports whether e-mail is switched on (mail.enabled).
func (s *Service) Enabled() bool { return s.cfg.Get().Mail.IsEnabled() }

// prepare validates a message and applies the configured sender.
func (s *Service) prepare(m *Message) error {
	c := s.cfg.Get().Mail
	clean := func(list []string) ([]string, error) {
		out := []string{}
		for _, a := range list {
			addr, err := ParseAddress(a)
			if err != nil {
				return nil, apperr.BadRequest("%s", err.Error())
			}
			out = append(out, addr)
		}
		return out, nil
	}
	var err error
	if m.To, err = clean(m.To); err != nil {
		return err
	}
	if m.Cc, err = clean(m.Cc); err != nil {
		return err
	}
	// a recipient gets a message only once
	seen := map[string]bool{}
	dedup := func(list []string) []string {
		out := []string{}
		for _, a := range list {
			if k := strings.ToLower(a); !seen[k] {
				seen[k] = true
				out = append(out, a)
			}
		}
		return out
	}
	m.To, m.Cc = dedup(m.To), dedup(m.Cc)
	if len(m.To) == 0 {
		return apperr.BadRequest("the message has no recipient")
	}
	if len(m.To)+len(m.Cc) > MaxRecipients {
		return apperr.BadRequest("at most %d recipients per message", MaxRecipients)
	}
	if m.ReplyTo != "" {
		if m.ReplyTo, err = ParseAddress(m.ReplyTo); err != nil {
			return apperr.BadRequest("%s", err.Error())
		}
	}
	if m.From != "" {
		from, err := ParseAddress(m.From)
		if err != nil {
			return apperr.BadRequest("%s", err.Error())
		}
		if s.SenderAllowed(from) {
			m.From = from
		} else {
			// not allowed as sender (SPF/DMARC, spoofing): answers still reach it
			if m.ReplyTo == "" {
				m.ReplyTo = from
			}
			m.From = ""
		}
	}
	if m.From == "" {
		m.From = c.From
		if m.FromName == "" {
			m.FromName = c.FromName
		}
	}
	if m.ReplyTo == "" {
		m.ReplyTo = c.ReplyTo
	}
	if m.From == "" {
		m.From = "wiki@localhost"
	}
	m.Subject = CleanHeader(m.Subject)
	if c.SubjectPrefix != "" && !strings.HasPrefix(m.Subject, c.SubjectPrefix) {
		m.Subject = c.SubjectPrefix + " " + m.Subject
	}
	if len([]rune(m.Subject)) > 250 {
		m.Subject = string([]rune(m.Subject)[:250])
	}
	if len(m.Text) > 200000 {
		m.Text = m.Text[:200000]
	}
	if len(m.HTML) > 2000000 {
		m.HTML = ""
	}
	m.FromName = CleanHeader(m.FromName)
	return nil
}

// Enqueue validates m and writes it with one row per recipient to the
// outbox using q (pass the transaction of the change that causes the
// message, so the message exists exactly when the change does). It is sent
// by the worker; without a configured transport it is kept ("held").
// When e-mail is switched off (mail.enabled: false) nothing is stored and 0
// is returned.
func (s *Service) Enqueue(ctx context.Context, q db.Querier, m Message) (int64, error) {
	if !s.Enabled() {
		return 0, nil
	}
	if err := s.prepare(&m); err != nil {
		return 0, err
	}
	status := "queued"
	if s.Transport() == nil {
		status = "held"
	}
	owner := m.Owner
	if owner == 0 {
		owner = identity.From(ctx).ID()
	}
	var ownerID *int64
	if owner > 0 {
		ownerID = &owner
	}
	var id int64
	err := q.QueryRow(ctx, `INSERT INTO mail_outbox(status, from_addr, from_name, reply_to, to_addrs, cc_addrs, subject, body_text, body_html, source, ref, owner_id, report, priority, body_calendar)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id`, status, m.From, m.FromName, m.ReplyTo, m.To, m.Cc, m.Subject, m.Text, m.HTML,
		m.Source, m.Ref, ownerID, !m.NoReport, m.Priority, m.Calendar).Scan(&id)
	if err != nil {
		return 0, err
	}
	if _, err := q.Exec(ctx, `INSERT INTO mail_recipients(message_id, addr, kind)
		SELECT $1::bigint, a, 'to' FROM unnest($2::text[]) a UNION ALL SELECT $1::bigint, a, 'cc' FROM unnest($3::text[]) a`, id, m.To, m.Cc); err != nil {
		return 0, err
	}
	if s.Kick != nil {
		s.Kick()
	}
	return id, nil
}

// SendNow sends a message immediately (bypassing the outbox), e.g. a test.
func (s *Service) SendNow(ctx context.Context, m Message) error {
	if !s.Enabled() {
		return apperr.BadRequest("e-mail is switched off (mail.enabled: false)")
	}
	if err := s.prepare(&m); err != nil {
		return err
	}
	t := s.Transport()
	if t == nil {
		return apperr.BadRequest("no mail server is configured (mail.transport / mail.smtp.host) and no sendmail binary was found")
	}
	data, err := s.render(ctx, BuildInput{From: m.From, FromName: m.FromName, ReplyTo: m.ReplyTo, To: m.To, Cc: m.Cc, Subject: m.Subject, Text: m.Text, HTML: m.HTML, Calendar: m.Calendar, Now: time.Now()}, 0)
	if err != nil {
		return apperr.BadRequest("the message cannot be signed: %s", err.Error())
	}
	return t.Send(ctx, Envelope{From: s.envelopeFrom(m.From, 0, 0), Recipients: append(append([]string{}, m.To...), m.Cc...), Data: data})
}

// envelopeFrom is the SMTP envelope sender (Return-Path): the bounce
// address (with VERP token per recipient) or the From address.
func (s *Service) envelopeFrom(from string, msgID, rcptID int64) string {
	b := s.cfg.Get().Mail.Bounce
	if b.Address == "" {
		return from
	}
	if b.VERP && rcptID > 0 {
		if i := strings.LastIndex(b.Address, "@"); i > 0 {
			return fmt.Sprintf("%s+m%dr%d%s", b.Address[:i], msgID, rcptID, b.Address[i:])
		}
	}
	return b.Address
}

// backoff for temporary failures; after the last step every hour, forever.
// (1, 2, 5, 15, 30 minutes, then every hour; see deliver)

// HeldRequeueAge: held messages younger than this are sent automatically as
// soon as a transport is configured; older ones wait for an administrator.
const HeldRequeueAge = 72 * time.Hour

// LeaseTime is how long a worker may work on a claimed message before
// another worker takes it over (crash safety).
const LeaseTime = 10 * time.Minute

type claimed struct {
	id                      int64
	from, fromName, replyTo string
	to, cc                  []string
	subject, text, html     string
	calendar                string
	ownerID                 *int64
	report                  bool
}

type rcpt struct {
	id   int64
	addr string
}

// limiter spreads sends over time (mail.rate_per_minute).
type limiter struct {
	mu   sync.Mutex
	next time.Time
	gap  time.Duration
}

func (l *limiter) wait(ctx context.Context) error {
	if l == nil || l.gap == 0 {
		return nil
	}
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	at := l.next
	l.next = l.next.Add(l.gap)
	l.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Until(at)):
		return nil
	}
}

// Process sends due messages with mail.concurrency parallel connections
// until the queue is empty (called by the worker). It returns the number of
// delivered recipients.
func (s *Service) Process(ctx context.Context) (int, error) {
	if !s.Enabled() {
		return 0, nil
	}
	t := s.Transport()
	if t == nil {
		return 0, nil
	}
	if _, err := s.pool.Exec(ctx, `UPDATE mail_outbox SET status='queued', next_attempt_at=now() WHERE status='held' AND created_at > now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(HeldRequeueAge.Seconds()))); err != nil {
		return 0, err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE mail_recipients r SET next_attempt_at=now() FROM mail_outbox o
		WHERE o.id=r.message_id AND o.status='queued' AND r.status='pending' AND r.next_attempt_at > o.next_attempt_at AND o.next_attempt_at <= now()`); err != nil {
		return 0, err
	}
	cfg := s.cfg.Get().Mail
	conc := cfg.Concurrency
	if conc <= 0 {
		conc = 4
	}
	var lim *limiter
	if cfg.RatePerMinute > 0 {
		lim = &limiter{gap: time.Minute / time.Duration(cfg.RatePerMinute)}
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	var mu sync.Mutex
	var total int
	var firstErr error
	var wg sync.WaitGroup
	for w := 0; w < conc; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := s.work(ctx, t, lim)
			mu.Lock()
			total += n
			if err != nil && firstErr == nil && ctx.Err() == nil {
				firstErr = err
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return total, firstErr
}

// work claims and sends batches until there is nothing left.
func (s *Service) work(ctx context.Context, t Transport, lim *limiter) (int, error) {
	var sess Session
	defer func() {
		if sess != nil {
			_ = sess.Close()
		}
	}()
	total := 0
	for ctx.Err() == nil {
		batch, err := s.claim(ctx, 10)
		if err != nil {
			return total, err
		}
		if len(batch) == 0 {
			return total, nil
		}
		for _, m := range batch {
			if err := lim.wait(ctx); err != nil {
				s.release(context.WithoutCancel(ctx), m.id)
				continue
			}
			if sess == nil {
				if sess, err = open(ctx, t); err != nil {
					// cannot connect: everything stays queued
					s.log.Warn("mail server not reachable", "err", err)
					s.deferMessage(context.WithoutCancel(ctx), m.id, err.Error())
					sess = nil
					continue
				}
			}
			n, broken, err := s.deliver(ctx, sess, m)
			total += n
			if err != nil {
				return total, err
			}
			if broken {
				_ = sess.Close()
				sess = nil
			}
		}
	}
	return total, nil
}

// claim leases up to n due messages to this worker.
func (s *Service) claim(ctx context.Context, n int) ([]claimed, error) {
	rows, err := s.pool.Query(ctx, `UPDATE mail_outbox o SET lease_until = now() + $2::interval
		WHERE o.id IN (SELECT id FROM mail_outbox WHERE status='queued' AND next_attempt_at <= now()
			AND (lease_until IS NULL OR lease_until < now()) ORDER BY priority DESC, id LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING o.id, o.from_addr, o.from_name, o.reply_to, o.to_addrs, o.cc_addrs, o.subject, o.body_text, o.body_html, o.owner_id, o.report, o.body_calendar`,
		n, fmt.Sprintf("%d seconds", int(LeaseTime.Seconds())))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (claimed, error) {
		var c claimed
		return c, r.Scan(&c.id, &c.from, &c.fromName, &c.replyTo, &c.to, &c.cc, &c.subject, &c.text, &c.html, &c.ownerID, &c.report, &c.calendar)
	})
}

func (s *Service) release(ctx context.Context, id int64) {
	_, _ = s.pool.Exec(ctx, `UPDATE mail_outbox SET lease_until=NULL WHERE id=$1`, id)
}

// deferMessage keeps a message queued after a connection problem.
func (s *Service) deferMessage(ctx context.Context, id int64, msg string) {
	_, _ = s.pool.Exec(ctx, `UPDATE mail_recipients SET attempts=attempts+1, last_error=$2, last_code=0,
		next_attempt_at=now() + interval '1 minute' WHERE message_id=$1 AND status='pending'`, id, truncate(msg))
	_ = s.finalize(ctx, id)
}

func truncate(s string) string {
	if len(s) > 1000 {
		return s[:1000]
	}
	return s
}

// deliver sends one claimed message to its pending recipients and records
// the result per recipient. broken reports that the session must be reopened.
func (s *Service) deliver(ctx context.Context, sess Session, m claimed) (sent int, broken bool, err error) {
	rows, err := s.pool.Query(ctx, `SELECT id, addr FROM mail_recipients WHERE message_id=$1 AND status='pending' ORDER BY id`, m.id)
	if err != nil {
		return 0, false, err
	}
	rcpts, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (rcpt, error) {
		var x rcpt
		return x, r.Scan(&x.id, &x.addr)
	})
	if err != nil {
		return 0, false, err
	}
	data, err := s.render(ctx, BuildInput{From: m.from, FromName: m.fromName, ReplyTo: m.replyTo, To: m.to, Cc: m.cc, Subject: m.subject, Text: m.text, HTML: m.html, Calendar: m.calendar,
		Headers: map[string]string{"X-Ewiki-Message": strconv.FormatInt(m.id, 10)}, Now: time.Now()}, m.id)
	if err != nil {
		// cannot be signed now (e.g. expired certificate with "hold"): stays queued
		s.holdMessage(context.WithoutCancel(ctx), m.id, "S/MIME: "+err.Error())
		return 0, false, nil
	}
	// one SMTP transaction for all recipients, or one per recipient with VERP
	groups := [][]rcpt{rcpts}
	b := s.cfg.Get().Mail.Bounce
	if b.Address != "" && b.VERP {
		groups = nil
		for _, r := range rcpts {
			groups = append(groups, []rcpt{r})
		}
	}
	results := map[int64]RcptResult{}
	for _, g := range groups {
		if len(g) == 0 {
			continue
		}
		var rid int64
		if len(g) == 1 && b.VERP {
			rid = g[0].id
		}
		env := Envelope{From: s.envelopeFrom(m.from, m.id, rid), Data: data}
		for _, r := range g {
			env.Recipients = append(env.Recipients, r.addr)
		}
		sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		res, derr := sess.Deliver(sctx, env)
		cancel()
		if derr != nil {
			var pe *PermanentError
			code, msg := 0, derr.Error()
			if errors.As(derr, &pe) {
				code, msg = pe.Code, pe.Msg
			} else {
				broken = true
			}
			for _, r := range g {
				results[r.id] = RcptResult{Addr: r.addr, Code: code, Msg: msg}
			}
			if broken {
				// keep the rest for the next session
				break
			}
			continue
		}
		byAddr := map[string]RcptResult{}
		for _, x := range res {
			byAddr[strings.ToLower(x.Addr)] = x
		}
		for _, r := range g {
			x, ok := byAddr[strings.ToLower(r.addr)]
			if !ok {
				x = RcptResult{Addr: r.addr, Code: 0, Msg: "no answer for this recipient"}
			}
			results[r.id] = x
		}
	}
	var failed []RcptResult
	err = pgx.BeginFunc(context.WithoutCancel(ctx), s.pool, func(tx pgx.Tx) error {
		for _, r := range rcpts {
			x, ok := results[r.id]
			if !ok { // not tried (broken session): try again soon
				continue
			}
			switch {
			case x.Code >= 200 && x.Code <= 299:
				sent++
				_, err = tx.Exec(ctx, `UPDATE mail_recipients SET status='sent', sent_at=now(), attempts=attempts+1, last_code=$2, last_error='' WHERE id=$1`, r.id, x.Code)
			case x.Code >= 500:
				failed = append(failed, x)
				_, err = tx.Exec(ctx, `UPDATE mail_recipients SET status='failed', failed_at=now(), attempts=attempts+1, last_code=$2, last_error=$3 WHERE id=$1`,
					r.id, x.Code, truncate(x.Msg))
			default: // 4xx or connection problems: stays pending, retried without limit
				_, err = tx.Exec(ctx, `UPDATE mail_recipients SET attempts=attempts+1, last_code=$2, last_error=$3,
					next_attempt_at = now() + make_interval(secs => CASE attempts + 1 WHEN 1 THEN 60 WHEN 2 THEN 120 WHEN 3 THEN 300
						WHEN 4 THEN 900 WHEN 5 THEN 1800 ELSE 3600 END) WHERE id=$1`, r.id, x.Code, truncate(x.Msg))
			}
			if err != nil {
				return err
			}
		}
		if err := s.finalizeTx(ctx, tx, m.id); err != nil {
			return err
		}
		if len(failed) > 0 && m.report && m.ownerID != nil {
			return s.report(ctx, tx, *m.ownerID, m.id, m.subject, failed)
		}
		return nil
	})
	return sent, broken, err
}

// finalize recomputes the status of a message from its recipients.
func (s *Service) finalize(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return s.finalizeTx(ctx, tx, id) })
}

func (s *Service) finalizeTx(ctx context.Context, tx pgx.Tx, id int64) error {
	_, err := tx.Exec(ctx, `UPDATE mail_outbox o SET
		status = CASE WHEN x.pending > 0 THEN (CASE WHEN o.status='held' THEN 'held' ELSE 'queued' END)
			WHEN x.failed = 0 THEN 'sent' WHEN x.sent = 0 THEN 'failed' ELSE 'partial' END,
		next_attempt_at = COALESCE(x.next, o.next_attempt_at),
		sent_at = CASE WHEN x.pending = 0 AND x.sent > 0 THEN COALESCE(o.sent_at, now()) ELSE o.sent_at END,
		attempts = x.attempts, last_error = x.last_error, lease_until = NULL
		FROM (SELECT count(*) FILTER (WHERE status='pending') AS pending, count(*) FILTER (WHERE status='sent') AS sent,
				count(*) FILTER (WHERE status='failed') AS failed, min(next_attempt_at) FILTER (WHERE status='pending') AS next,
				COALESCE(max(attempts), 0) AS attempts,
				COALESCE((SELECT last_error FROM mail_recipients WHERE message_id=$1 AND last_error <> '' ORDER BY attempts DESC, id LIMIT 1), '') AS last_error
			FROM mail_recipients WHERE message_id=$1) x
		WHERE o.id=$1`, id)
	if err != nil {
		return err
	}
	return scrubFinished(ctx, tx, id)
}

// report e-mails the owner of a message which recipients did not get it.
func (s *Service) report(ctx context.Context, tx pgx.Tx, owner, msgID int64, subject string, failed []RcptResult) error {
	us, err := s.Users(ctx, tx, []int64{owner})
	if err != nil || len(us) == 0 || us[0].Email == "" {
		return err
	}
	u := us[0]
	var lines strings.Builder
	for _, f := range failed {
		fmt.Fprintf(&lines, "- %s: %d %s\n", f.Addr, f.Code, f.Msg)
	}
	tr := s.T
	if tr == nil {
		tr = func(_ context.Context, _, key string, _ map[string]string) string { return key }
	}
	params := map[string]string{"name": u.Name, "subject": subject, "recipients": lines.String(), "id": strconv.FormatInt(msgID, 10)}
	_, err = s.Enqueue(ctx, tx, Message{To: []string{u.Email}, Subject: tr(ctx, u.Lang, "mail.report_subject", params),
		Text: tr(ctx, u.Lang, "mail.report_body", params), Source: "mail.report", Ref: fmt.Sprintf("message:%d", msgID), NoReport: true, Owner: -1, Priority: 1})
	return err
}

// ---------------------------------------------------------------- outbox (admin)

// OutboxEntry is a message in the outbox (admin view).
type OutboxEntry struct {
	ID         int64       `json:"id"`
	Status     string      `json:"status"`
	From       string      `json:"from"`
	To         []string    `json:"to"`
	Cc         []string    `json:"cc"`
	Subject    string      `json:"subject"`
	Body       string      `json:"body,omitempty"`
	Source     string      `json:"source"`
	Ref        string      `json:"ref"`
	Attempts   int         `json:"attempts"`
	LastError  string      `json:"last_error"`
	CreatedAt  time.Time   `json:"created_at"`
	SentAt     *time.Time  `json:"sent_at"`
	NextTry    time.Time   `json:"next_attempt_at"`
	Owner      *string     `json:"owner"`
	Pending    int         `json:"pending"`
	Delivered  int         `json:"delivered"`
	Failed     int         `json:"failed"`
	Recipients []Recipient `json:"recipients,omitempty"`
}

// Recipient is the delivery state of one recipient.
type Recipient struct {
	ID        int64      `json:"id"`
	Addr      string     `json:"addr"`
	Kind      string     `json:"kind"`
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastCode  int        `json:"last_code"`
	LastError string     `json:"last_error"`
	NextTry   time.Time  `json:"next_attempt_at"`
	SentAt    *time.Time `json:"sent_at"`
	FailedAt  *time.Time `json:"failed_at"`
	Bounced   bool       `json:"bounced"`
}

const outboxCols = `o.id, o.status, o.from_addr, o.to_addrs, o.cc_addrs, o.subject, o.body_text, o.source, o.ref, o.attempts, o.last_error,
	o.created_at, o.sent_at, o.next_attempt_at, (SELECT u.username || '@' || u.provider FROM users u WHERE u.id=o.owner_id),
	(SELECT count(*) FROM mail_recipients r WHERE r.message_id=o.id AND r.status='pending'),
	(SELECT count(*) FROM mail_recipients r WHERE r.message_id=o.id AND r.status='sent'),
	(SELECT count(*) FROM mail_recipients r WHERE r.message_id=o.id AND r.status='failed')`

func scanOutbox(r pgx.CollectableRow) (OutboxEntry, error) {
	var o OutboxEntry
	err := r.Scan(&o.ID, &o.Status, &o.From, &o.To, &o.Cc, &o.Subject, &o.Body, &o.Source, &o.Ref, &o.Attempts, &o.LastError,
		&o.CreatedAt, &o.SentAt, &o.NextTry, &o.Owner, &o.Pending, &o.Delivered, &o.Failed)
	o.Body = Redact(o.Body) // secrets of the recipients are never shown
	return o, err
}

// Outbox lists messages (newest first). q filters by subject or recipient.
func (s *Service) Outbox(ctx context.Context, status, q string, limit, offset int) ([]OutboxEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT `+outboxCols+` FROM mail_outbox o
		WHERE ($1='' OR o.status=$1) AND ($4='' OR o.subject ILIKE '%'||$4||'%' OR EXISTS (SELECT 1 FROM mail_recipients r WHERE r.message_id=o.id AND r.addr ILIKE '%'||$4||'%'))
		ORDER BY o.id DESC LIMIT $2 OFFSET $3`, status, limit, offset, q)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanOutbox)
}

// Get returns a message with its recipients.
func (s *Service) Get(ctx context.Context, id int64) (*OutboxEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+outboxCols+` FROM mail_outbox o WHERE o.id=$1`, id)
	if err != nil {
		return nil, err
	}
	o, err := pgx.CollectExactlyOneRow(rows, scanOutbox)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("message")
	}
	if err != nil {
		return nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT id, addr, kind, status, attempts, last_code, last_error, next_attempt_at, sent_at, failed_at, bounced
		FROM mail_recipients WHERE message_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	if o.Recipients, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Recipient]); err != nil {
		return nil, err
	}
	return &o, nil
}

// Retry queues failed recipients (and held messages) again.
func (s *Service) Retry(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE mail_outbox SET status='queued', next_attempt_at=now(), lease_until=NULL WHERE id=$1 AND status IN ('failed','held','partial','queued')`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("message that can be retried")
		}
		if _, err := tx.Exec(ctx, `UPDATE mail_recipients SET status='pending', next_attempt_at=now(), failed_at=NULL, bounced=false
			WHERE message_id=$1 AND status IN ('failed','pending')`, id); err != nil {
			return err
		}
		return s.finalizeTx(ctx, tx, id)
	})
}

// Delete removes a message from the outbox.
func (s *Service) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM mail_outbox WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("message")
	}
	return nil
}

// Cleanup removes completely delivered messages older than days (0 = keep).
// Messages with failed recipients are kept for the administrators.
func (s *Service) Cleanup(ctx context.Context, days int) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM mail_outbox WHERE status='sent' AND sent_at < now() - make_interval(days => $1)`, days)
	return tag.RowsAffected(), err
}

// ---------------------------------------------------------------- recipients

// User is a possible recipient.
type User struct {
	ID     int64           `json:"id"`
	Name   string          `json:"name"`
	Handle string          `json:"handle"`
	Email  string          `json:"email"`
	Lang   string          `json:"lang"`
	Notify map[string]bool `json:"notify"`
}

// Wants reports whether the user receives notifications of kind (default on).
func (u User) Wants(kind string) bool {
	v, ok := u.Notify[kind]
	if !ok {
		v = DefaultOn(kind)
	}
	return u.Email != "" && v
}

// Users loads active users with their address, language and preferences.
func (s *Service) Users(ctx context.Context, q db.Querier, ids []int64) ([]User, error) {
	if len(ids) == 0 {
		return []User{}, nil
	}
	if q == nil {
		q = s.pool
	}
	def := s.cfg.Get().I18n.DefaultLanguage
	rows, err := q.Query(ctx, `SELECT id, CASE WHEN display_name='' THEN username ELSE display_name END, username || '@' || provider, email,
		CASE WHEN language='' THEN $2 ELSE language END, notify FROM users WHERE id = ANY($1) AND NOT disabled ORDER BY id`, ids, def)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[User])
}

// NotificationKinds are the kinds users can switch on and off.
var NotificationKinds = []string{"tasks", "cases", "comments", "watches"}

// KindLabels maps kinds registered by plugins to their UI label keys
// (core kinds: notify.kind.<kind>).
var KindLabels = map[string]string{}

// KindsOffByDefault lists kinds whose e-mails users have to switch on
// (e.g. reactions); all other kinds are on until switched off.
var KindsOffByDefault = map[string]bool{}

// DefaultOn reports whether e-mails of kind are on without a preference.
func DefaultOn(kind string) bool { return !KindsOffByDefault[kind] }

// SetPreferences stores the notification preferences of a user.
func (s *Service) SetPreferences(ctx context.Context, uid int64, prefs map[string]bool) error {
	clean := map[string]bool{}
	for _, k := range NotificationKinds {
		if v, ok := prefs[k]; ok {
			clean[k] = v
		}
	}
	tag, err := s.pool.Exec(ctx, `UPDATE users SET notify=$2 WHERE id=$1`, uid, clean)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("user not found")
	}
	return nil
}
