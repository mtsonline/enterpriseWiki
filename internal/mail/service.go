package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/db"
)

// Service is the mail service.
type Service struct {
	pool *pgxpool.Pool
	cfg  *config.Store
	log  *slog.Logger
	// override is used by tests to capture messages.
	override Transport
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

// Status describes the mail setup for the admin page.
type Status struct {
	Transport string         `json:"transport"` // effective: smtp, sendmail, log or "" (held)
	From      string         `json:"from"`
	Counts    map[string]int `json:"counts"`
	Problem   string         `json:"problem,omitempty"`
}

// GetStatus reports the effective transport and outbox counts.
func (s *Service) GetStatus(ctx context.Context) (*Status, error) {
	st := &Status{From: s.cfg.Get().Mail.From, Counts: map[string]int{"queued": 0, "sent": 0, "failed": 0, "held": 0}}
	if t := s.Transport(); t != nil {
		st.Transport = t.Name()
	} else {
		st.Problem = "no mail server configured: messages are kept in the outbox"
	}
	if st.From == "" {
		st.Problem = "mail.from is not set"
	}
	rows, err := s.pool.Query(ctx, `SELECT status, count(*) FROM mail_outbox GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		st.Counts[k] = n
	}
	return st, rows.Err()
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

// prepare validates a message and applies the configured sender.
func (s *Service) prepare(m *Message) error {
	c := s.cfg.Get().Mail
	clean := func(list []string) ([]string, error) {
		out := []string{}
		seen := map[string]bool{}
		for _, a := range list {
			addr, err := ParseAddress(a)
			if err != nil {
				return nil, apperr.BadRequest("%s", err.Error())
			}
			if !seen[strings.ToLower(addr)] {
				seen[strings.ToLower(addr)] = true
				out = append(out, addr)
			}
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
	m.FromName = CleanHeader(m.FromName)
	return nil
}

// Enqueue validates m and writes it to the outbox using q (pass the
// transaction of the change that causes the message). It is sent by the
// worker; without a configured transport it is kept ("held").
func (s *Service) Enqueue(ctx context.Context, q db.Querier, m Message) (int64, error) {
	if err := s.prepare(&m); err != nil {
		return 0, err
	}
	status := "queued"
	if s.Transport() == nil {
		status = "held"
	}
	var id int64
	err := q.QueryRow(ctx, `INSERT INTO mail_outbox(status, from_addr, from_name, reply_to, to_addrs, cc_addrs, subject, body_text, source, ref)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, status, m.From, m.FromName, m.ReplyTo, m.To, m.Cc, m.Subject, m.Text, m.Source, m.Ref).Scan(&id)
	return id, err
}

// SendNow sends a message immediately (bypassing the outbox), e.g. a test.
func (s *Service) SendNow(ctx context.Context, m Message) error {
	if err := s.prepare(&m); err != nil {
		return err
	}
	t := s.Transport()
	if t == nil {
		return apperr.BadRequest("no mail server is configured (mail.transport / mail.smtp.host) and no sendmail binary was found")
	}
	return t.Send(ctx, Envelope{From: m.From, Recipients: append(append([]string{}, m.To...), m.Cc...),
		Data: Build(m.From, m.FromName, m.ReplyTo, m.To, m.Cc, m.Subject, m.Text, time.Now())})
}

var backoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour}

// MaxAttempts is the number of tries before a message is marked failed.
const MaxAttempts = 8

// HeldRequeueAge: held messages younger than this are sent automatically as
// soon as a transport is configured; older ones wait for an administrator.
const HeldRequeueAge = 72 * time.Hour

// Process sends due messages (called by the worker). It returns the number
// of messages sent.
func (s *Service) Process(ctx context.Context) (int, error) {
	t := s.Transport()
	if t == nil {
		return 0, nil
	}
	if _, err := s.pool.Exec(ctx, `UPDATE mail_outbox SET status='queued', next_attempt_at=now() WHERE status='held' AND created_at > now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(HeldRequeueAge.Seconds()))); err != nil {
		return 0, err
	}
	sent := 0
	for i := 0; i < 10; i++ { // up to 10 batches per run
		n, err := s.batch(ctx, t)
		sent += n
		if err != nil || n == 0 {
			return sent, err
		}
	}
	return sent, nil
}

type queued struct {
	id                      int64
	from, fromName, replyTo string
	to, cc                  []string
	subject, body           string
	attempts                int
}

func (s *Service) batch(ctx context.Context, t Transport) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, from_addr, from_name, reply_to, to_addrs, cc_addrs, subject, body_text, attempts
			FROM mail_outbox WHERE status='queued' AND next_attempt_at <= now() ORDER BY id LIMIT 20 FOR UPDATE SKIP LOCKED`)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (queued, error) {
			var q queued
			return q, r.Scan(&q.id, &q.from, &q.fromName, &q.replyTo, &q.to, &q.cc, &q.subject, &q.body, &q.attempts)
		})
		if err != nil {
			return err
		}
		for _, q := range list {
			sctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			err := t.Send(sctx, Envelope{From: q.from, Recipients: append(append([]string{}, q.to...), q.cc...),
				Data: Build(q.from, q.fromName, q.replyTo, q.to, q.cc, q.subject, q.body, time.Now())})
			cancel()
			if err == nil {
				if _, err := tx.Exec(ctx, `UPDATE mail_outbox SET status='sent', sent_at=now(), attempts=attempts+1, last_error='' WHERE id=$1`, q.id); err != nil {
					return err
				}
				n++
				continue
			}
			s.log.Warn("sending e-mail failed", "id", q.id, "attempt", q.attempts+1, "err", err)
			status, next := "queued", time.Now()
			if q.attempts+1 >= MaxAttempts {
				status = "failed"
			} else {
				next = next.Add(backoff[min(q.attempts, len(backoff)-1)])
			}
			msg := err.Error()
			if len(msg) > 1000 {
				msg = msg[:1000]
			}
			if _, err := tx.Exec(ctx, `UPDATE mail_outbox SET status=$2, attempts=attempts+1, last_error=$3, next_attempt_at=$4 WHERE id=$1`,
				q.id, status, msg, next); err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}

// OutboxEntry is a message in the outbox (admin view).
type OutboxEntry struct {
	ID        int64      `json:"id"`
	Status    string     `json:"status"`
	From      string     `json:"from"`
	To        []string   `json:"to"`
	Cc        []string   `json:"cc"`
	Subject   string     `json:"subject"`
	Body      string     `json:"body,omitempty"`
	Source    string     `json:"source"`
	Ref       string     `json:"ref"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"last_error"`
	CreatedAt time.Time  `json:"created_at"`
	SentAt    *time.Time `json:"sent_at"`
}

// Outbox lists messages (newest first).
func (s *Service) Outbox(ctx context.Context, status string, limit, offset int) ([]OutboxEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT id, status, from_addr, to_addrs, cc_addrs, subject, body_text, source, ref, attempts, last_error, created_at, sent_at
		FROM mail_outbox WHERE $1='' OR status=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[OutboxEntry])
}

// Retry queues a failed or held message again.
func (s *Service) Retry(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE mail_outbox SET status='queued', next_attempt_at=now(), attempts=0 WHERE id=$1 AND status IN ('failed','held')`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("failed or held message")
	}
	return nil
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

// Cleanup removes sent messages older than days (0 = keep).
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
	return u.Email != "" && (!ok || v)
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
var NotificationKinds = []string{"tasks", "cases", "comments"}

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
