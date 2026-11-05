package mail

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	netmail "net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Bounce is a permanent delivery failure found in a bounce mail.
type Bounce struct {
	MessageID int64 // 0 if unknown
	RcptID    int64 // 0 if unknown (VERP gives both)
	Addr      string
	Status    string // e.g. 5.1.1
	Diagnosis string
}

var verpRe = regexp.MustCompile(`\+m(\d+)r(\d+)@`)

// ParseBounce extracts permanent failures from a bounce mail (RFC 3464
// delivery status notifications, VERP addresses, X-Ewiki-Message header of
// the returned message). Delays (4.x.x) are ignored.
func ParseBounce(raw []byte) []Bounce {
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	var verpMsg, verpRcpt int64
	for _, h := range []string{"To", "Delivered-To", "X-Original-To", "Envelope-To", "X-Envelope-To"} {
		if m := verpRe.FindStringSubmatch(msg.Header.Get(h)); m != nil {
			verpMsg, _ = strconv.ParseInt(m[1], 10, 64)
			verpRcpt, _ = strconv.ParseInt(m[2], 10, 64)
			break
		}
	}
	var out []Bounce
	var msgID int64
	var status []Bounce
	var walk func(ct string, r io.Reader, depth int)
	walk = func(ct string, r io.Reader, depth int) {
		if depth > 5 {
			return
		}
		mt, params, _ := mime.ParseMediaType(ct)
		switch {
		case strings.HasPrefix(mt, "multipart/"):
			mr := multipart.NewReader(r, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err != nil {
					return
				}
				walk(p.Header.Get("Content-Type"), p, depth+1)
			}
		case mt == "message/delivery-status" || mt == "message/global-delivery-status":
			status = append(status, parseDSN(r)...)
		case mt == "message/rfc822" || mt == "text/rfc822-headers" || mt == "message/rfc822-headers":
			b, _ := io.ReadAll(io.LimitReader(r, 1<<20))
			if m := regexp.MustCompile(`(?mi)^X-Ewiki-Message:\s*(\d+)`).FindSubmatch(b); m != nil {
				msgID, _ = strconv.ParseInt(string(m[1]), 10, 64)
			}
		default:
			b, _ := io.ReadAll(io.LimitReader(r, 1<<20))
			if msgID == 0 {
				if m := regexp.MustCompile(`(?mi)^X-Ewiki-Message:\s*(\d+)`).FindSubmatch(b); m != nil {
					msgID, _ = strconv.ParseInt(string(m[1]), 10, 64)
				}
			}
		}
	}
	walk(msg.Header.Get("Content-Type"), msg.Body, 0)
	for _, s := range status {
		s.MessageID = msgID
		if verpMsg > 0 {
			s.MessageID, s.RcptID = verpMsg, verpRcpt
		}
		out = append(out, s)
	}
	if len(out) == 0 && verpMsg > 0 && len(status) == 0 {
		// a bounce without a machine readable report, but with VERP: assume failure
		subj := strings.ToLower(msg.Header.Get("Subject"))
		if strings.Contains(subj, "undeliver") || strings.Contains(subj, "failure") || strings.Contains(subj, "unzustellbar") || strings.Contains(subj, "returned") {
			out = append(out, Bounce{MessageID: verpMsg, RcptID: verpRcpt, Status: "5.0.0", Diagnosis: msg.Header.Get("Subject")})
		}
	}
	return out
}

// parseDSN reads the per-recipient blocks of a delivery status report.
func parseDSN(r io.Reader) []Bounce {
	var out []Bounce
	sc := bufio.NewScanner(io.LimitReader(r, 1<<20))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	cur := map[string]string{}
	var last string
	flush := func() {
		action := strings.ToLower(strings.TrimSpace(cur["action"]))
		st := strings.TrimSpace(cur["status"])
		rcpt := cur["final-recipient"]
		if rcpt == "" {
			rcpt = cur["original-recipient"]
		}
		if i := strings.Index(rcpt, ";"); i >= 0 {
			rcpt = rcpt[i+1:]
		}
		rcpt = strings.Trim(strings.TrimSpace(rcpt), "<>")
		if rcpt != "" && action == "failed" && strings.HasPrefix(st, "5") {
			out = append(out, Bounce{Addr: rcpt, Status: st, Diagnosis: strings.TrimSpace(cur["diagnostic-code"])})
		}
		cur = map[string]string{}
	}
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && last != "" {
			cur[last] += " " + strings.TrimSpace(line)
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		last = strings.ToLower(strings.TrimSpace(k))
		cur[last] = strings.TrimSpace(v)
	}
	flush()
	return out
}

// ApplyBounce marks the recipient of a bounce as failed and reports it to
// the owner of the message. It returns false if no recipient matched.
func (s *Service) ApplyBounce(ctx context.Context, b Bounce) (bool, error) {
	var rid, mid int64
	var addr string
	var err error
	switch {
	case b.RcptID > 0:
		err = s.pool.QueryRow(ctx, `SELECT id, message_id, addr FROM mail_recipients WHERE id=$1 AND message_id=$2`, b.RcptID, b.MessageID).Scan(&rid, &mid, &addr)
	case b.MessageID > 0:
		err = s.pool.QueryRow(ctx, `SELECT id, message_id, addr FROM mail_recipients WHERE message_id=$1 AND lower(addr)=lower($2)`, b.MessageID, b.Addr).Scan(&rid, &mid, &addr)
	default: // only the address: the most recent delivery to it within a week
		err = s.pool.QueryRow(ctx, `SELECT id, message_id, addr FROM mail_recipients WHERE lower(addr)=lower($1) AND status='sent'
			AND sent_at > now() - interval '7 days' ORDER BY sent_at DESC LIMIT 1`, b.Addr).Scan(&rid, &mid, &addr)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	diag := strings.TrimSpace("bounce " + b.Status + " " + b.Diagnosis)
	code, _ := strconv.Atoi(strings.ReplaceAll(b.Status, ".", ""))
	if code < 500 || code > 599 {
		code = 550
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE mail_recipients SET status='failed', bounced=true, failed_at=now(), last_code=$2, last_error=$3 WHERE id=$1 AND status <> 'failed'`,
			rid, code, truncate(diag))
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		if err := s.finalizeTx(ctx, tx, mid); err != nil {
			return err
		}
		var owner *int64
		var report bool
		var subject string
		if err := tx.QueryRow(ctx, `SELECT owner_id, report, subject FROM mail_outbox WHERE id=$1`, mid).Scan(&owner, &report, &subject); err != nil {
			return err
		}
		if owner != nil && report {
			return s.report(ctx, tx, *owner, mid, subject, []RcptResult{{Addr: addr, Code: code, Msg: diag}})
		}
		return nil
	})
	return err == nil, err
}

// ProcessBounces reads unread mails of the bounce mailbox, applies the
// failures and marks the mails read (or deletes them). Returns the number of
// failed recipients found.
func (s *Service) ProcessBounces(ctx context.Context) (int, error) {
	b := s.cfg.Get().Mail.Bounce
	if b.IMAPHost == "" || !s.Enabled() {
		return 0, nil
	}
	c, err := dialIMAP(b.IMAPHost, b.IMAPPort, b.Security, b.CA, b.SkipVerify, 20*time.Second)
	if err != nil {
		return 0, err
	}
	defer c.logout()
	if err := c.login(b.Username, b.Password); err != nil {
		return 0, err
	}
	folder := b.Folder
	if folder == "" {
		folder = "INBOX"
	}
	if err := c.selectFolder(folder); err != nil {
		return 0, err
	}
	uids, err := c.unseen()
	if err != nil {
		return 0, err
	}
	n := 0
	for i, uid := range uids {
		if i >= 500 || ctx.Err() != nil {
			break
		}
		raw, err := c.fetch(uid)
		if err != nil {
			return n, err
		}
		for _, x := range ParseBounce(raw) {
			ok, err := s.ApplyBounce(ctx, x)
			if err != nil {
				return n, err
			}
			if ok {
				n++
			}
		}
		if err := c.markSeen(uid, b.Delete); err != nil {
			return n, err
		}
	}
	if b.Delete && len(uids) > 0 {
		_ = c.expunge()
	}
	return n, nil
}
