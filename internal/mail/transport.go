package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/tlsutil"
)

// Envelope is a rendered message with its SMTP envelope.
type Envelope struct {
	From       string
	Recipients []string
	Data       []byte
}

// Transport delivers messages. Send returns a *PermanentError for
// message-level permanent failures (5xx); every other error is temporary.
type Transport interface {
	Name() string
	Send(ctx context.Context, e Envelope) error
}

// PermanentError is a permanent (5xx) failure of a whole message.
type PermanentError struct {
	Code int
	Msg  string
}

func (e *PermanentError) Error() string { return fmt.Sprintf("%d %s", e.Code, e.Msg) }

// RcptResult is the outcome for one recipient: Code 2xx delivered,
// 4xx (or 0) temporary, 5xx permanent.
type RcptResult struct {
	Addr string
	Code int
	Msg  string
}

// Session delivers several messages over one connection.
type Session interface {
	// Deliver sends one message. An error means the whole message failed
	// (temporary unless *PermanentError) and the session should be closed.
	Deliver(ctx context.Context, e Envelope) ([]RcptResult, error)
	Close() error
}

// SessionTransport can reuse connections and report per recipient.
type SessionTransport interface {
	Transport
	Open(ctx context.Context) (Session, error)
}

// open returns a session for any transport.
func open(ctx context.Context, t Transport) (Session, error) {
	if st, ok := t.(SessionTransport); ok {
		return st.Open(ctx)
	}
	return &simpleSession{t}, nil
}

// simpleSession adapts a plain Transport: all recipients share the result.
type simpleSession struct{ t Transport }

func (s *simpleSession) Deliver(ctx context.Context, e Envelope) ([]RcptResult, error) {
	if err := s.t.Send(ctx, e); err != nil {
		return nil, err
	}
	out := make([]RcptResult, len(e.Recipients))
	for i, r := range e.Recipients {
		out[i] = RcptResult{Addr: r, Code: 250}
	}
	return out, nil
}

func (s *simpleSession) Close() error { return nil }

// ---------------------------------------------------------------- SMTP

type smtpTransport struct{ c config.SMTP }

func (t smtpTransport) Name() string { return "smtp" }

// loginAuth implements the LOGIN mechanism (still required by some servers,
// e.g. Microsoft 365) on top of an encrypted connection.
type loginAuth struct{ user, pass string }

func (a loginAuth) Start(si *smtp.ServerInfo) (string, []byte, error) {
	if !si.TLS && !isLocal(si.Name) {
		return "", nil, errors.New("refusing to send the password over an unencrypted connection")
	}
	return "LOGIN", nil, nil
}

func (a loginAuth) Next(from []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(from))) {
	case "username:":
		return []byte(a.user), nil
	case "password:":
		return []byte(a.pass), nil
	}
	return nil, fmt.Errorf("unexpected LOGIN challenge %q", from)
}

func isLocal(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// smtpCode extracts the reply code of an SMTP error (0 if none).
func smtpCode(err error) (int, string) {
	var te *textproto.Error
	if errors.As(err, &te) {
		return te.Code, te.Msg
	}
	return 0, err.Error()
}

// classify turns an SMTP error into a permanent error (5xx) or keeps it.
func classify(err error, what string) error {
	code, msg := smtpCode(err)
	if code >= 500 {
		return &PermanentError{Code: code, Msg: what + ": " + msg}
	}
	return fmt.Errorf("%s: %w", what, err)
}

type smtpSession struct {
	c    *smtp.Client
	conn net.Conn
}

// Open connects, says hello, starts TLS and authenticates.
func (t smtpTransport) Open(ctx context.Context) (Session, error) {
	port := t.c.Port
	sec := t.c.Security
	if sec == "" {
		sec = "starttls"
	}
	if port == 0 {
		port = map[string]int{"starttls": 587, "tls": 465, "none": 25}[sec]
	}
	addr := net.JoinHostPort(t.c.Host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	tlsCfg, err := tlsutil.Client(t.c.Host, t.c.CA, t.c.SkipVerify)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	if sec == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	c, err := smtp.NewClient(conn, t.c.Host)
	if err != nil {
		_ = conn.Close()
		return nil, classify(err, "smtp greeting")
	}
	fail := func(e error) (Session, error) { _ = c.Close(); return nil, e } // the first error is the one to report
	helo := t.c.HeloName
	if helo == "" {
		if h, err := os.Hostname(); err == nil && strings.Contains(h, ".") {
			helo = h
		} else {
			helo = "localhost"
		}
	}
	if err := c.Hello(helo); err != nil {
		return fail(classify(err, "HELO"))
	}
	if sec == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fail(errors.New("the server does not offer STARTTLS (set mail.smtp.security to tls or none)"))
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fail(fmt.Errorf("STARTTLS: %w", err))
		}
	}
	if t.c.Username != "" {
		ok, mechs := c.Extension("AUTH")
		if !ok {
			return fail(errors.New("the server does not offer authentication"))
		}
		var auth smtp.Auth = smtp.PlainAuth("", t.c.Username, t.c.Password, t.c.Host)
		if !strings.Contains(" "+strings.ToUpper(mechs)+" ", " PLAIN ") && strings.Contains(strings.ToUpper(mechs), "LOGIN") {
			auth = loginAuth{t.c.Username, t.c.Password}
		}
		if err := c.Auth(auth); err != nil {
			// wrong credentials are a configuration problem: retry later
			code, msg := smtpCode(err)
			return fail(fmt.Errorf("authentication failed: %d %s", code, msg))
		}
	}
	return &smtpSession{c: c, conn: conn}, nil
}

// Deliver sends one message; recipients rejected with 4xx/5xx are reported
// individually, the others receive the message.
func (s *smtpSession) Deliver(ctx context.Context, e Envelope) ([]RcptResult, error) {
	_ = s.conn.SetDeadline(time.Now().Add(2 * time.Minute))
	if err := s.c.Mail(e.From); err != nil {
		return nil, classify(err, "MAIL FROM")
	}
	res := make([]RcptResult, 0, len(e.Recipients))
	accepted := 0
	for _, r := range e.Recipients {
		if err := s.c.Rcpt(r); err != nil {
			code, msg := smtpCode(err)
			if code == 0 { // connection problem
				return nil, err
			}
			res = append(res, RcptResult{Addr: r, Code: code, Msg: msg})
			continue
		}
		accepted++
		res = append(res, RcptResult{Addr: r, Code: 250})
	}
	if accepted == 0 {
		_ = s.c.Reset()
		return res, nil
	}
	w, err := s.c.Data()
	if err != nil {
		return nil, classify(err, "DATA")
	}
	if _, err := w.Write(e.Data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, classify(err, "DATA")
	}
	return res, nil
}

func (s *smtpSession) Close() error {
	_ = s.c.Quit()
	return s.c.Close()
}

// Send delivers one message over a new connection (tests, single sends).
func (t smtpTransport) Send(ctx context.Context, e Envelope) error {
	sess, err := t.Open(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()
	res, err := sess.Deliver(ctx, e)
	if err != nil {
		return err
	}
	for _, r := range res {
		if r.Code >= 500 {
			return &PermanentError{Code: r.Code, Msg: r.Addr + ": " + r.Msg}
		}
		if r.Code < 200 || r.Code > 299 {
			return fmt.Errorf("%s: %d %s", r.Addr, r.Code, r.Msg)
		}
	}
	return nil
}

// ---------------------------------------------------------------- sendmail

type sendmailTransport struct{ path string }

func (t sendmailTransport) Name() string { return "sendmail" }

func (t sendmailTransport) Send(ctx context.Context, e Envelope) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// addresses are validated; never pass anything that sendmail would read as an option
	for _, a := range append([]string{e.From}, e.Recipients...) {
		if a == "" || strings.HasPrefix(a, "-") || strings.ContainsAny(a, "\r\n\x00 ") {
			return fmt.Errorf("sendmail: refusing address %q", a)
		}
	}
	args := append([]string{"-i", "-f", e.From}, e.Recipients...)
	cmd := exec.CommandContext(ctx, t.path, args...) // #nosec G204 -- sendmail path from the instance settings, arguments checked above
	cmd.Stdin = bytes.NewReader(bytes.ReplaceAll(e.Data, []byte("\r\n"), []byte("\n")))
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := fmt.Sprintf("%s: %v %s", t.path, err, strings.TrimSpace(string(out)))
		var ee *exec.ExitError
		// sysexits: EX_USAGE, EX_DATAERR, EX_NOUSER, EX_NOHOST are permanent
		if errors.As(err, &ee) && (ee.ExitCode() == 64 || ee.ExitCode() == 65 || ee.ExitCode() == 67 || ee.ExitCode() == 68) {
			return &PermanentError{Code: 550, Msg: msg}
		}
		return errors.New(msg)
	}
	return nil
}

// ---------------------------------------------------------------- log

type logTransport struct{ s *Service }

func (t logTransport) Name() string { return "log" }

func (t logTransport) Send(_ context.Context, e Envelope) error {
	t.s.log.Info("e-mail (log transport, not sent)", "from", e.From, "to", strings.Join(e.Recipients, ","), "size", len(e.Data))
	return nil
}

// DefaultSendmail is the usual location of the sendmail binary.
const DefaultSendmail = "/usr/sbin/sendmail"

func executable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}
