package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// Envelope is a rendered message with its SMTP envelope.
type Envelope struct {
	From       string
	Recipients []string
	Data       []byte
}

// Transport delivers messages.
type Transport interface {
	Name() string
	Send(ctx context.Context, e Envelope) error
}

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

func (t smtpTransport) Send(ctx context.Context, e Envelope) error {
	port := t.c.Port
	sec := t.c.Security
	if sec == "" {
		sec = "starttls"
	}
	if port == 0 {
		port = map[string]int{"starttls": 587, "tls": 465, "none": 25}[sec]
	}
	addr := net.JoinHostPort(t.c.Host, strconv.Itoa(port))
	deadline := time.Now().Add(60 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	tlsCfg := &tls.Config{ServerName: t.c.Host, InsecureSkipVerify: t.c.SkipVerify, MinVersion: tls.VersionTLS12} //nolint:gosec // opt-in for self-signed relays
	var conn net.Conn
	var err error
	if sec == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, t.c.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer c.Close()
	helo := t.c.HeloName
	if helo == "" {
		if h, err := os.Hostname(); err == nil && strings.Contains(h, ".") {
			helo = h
		} else {
			helo = "localhost"
		}
	}
	if err := c.Hello(helo); err != nil {
		return fmt.Errorf("HELO: %w", err)
	}
	if sec == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("the server does not offer STARTTLS (set mail.smtp.security to tls or none)")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if t.c.Username != "" {
		ok, mechs := c.Extension("AUTH")
		if !ok {
			return errors.New("the server does not offer authentication")
		}
		var auth smtp.Auth = smtp.PlainAuth("", t.c.Username, t.c.Password, t.c.Host)
		if !strings.Contains(" "+strings.ToUpper(mechs)+" ", " PLAIN ") && strings.Contains(strings.ToUpper(mechs), "LOGIN") {
			auth = loginAuth{t.c.Username, t.c.Password}
		}
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("authentication: %w", err)
		}
	}
	if err := c.Mail(e.From); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, r := range e.Recipients {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(e.Data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	return c.Quit()
}

// ---------------------------------------------------------------- sendmail

type sendmailTransport struct{ path string }

func (t sendmailTransport) Name() string { return "sendmail" }

func (t sendmailTransport) Send(ctx context.Context, e Envelope) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	args := append([]string{"-i", "-f", e.From}, e.Recipients...) // addresses are validated and never start with "-"
	cmd := exec.CommandContext(ctx, t.path, args...)
	cmd.Stdin = bytes.NewReader(bytes.ReplaceAll(e.Data, []byte("\r\n"), []byte("\n")))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v %s", t.path, err, strings.TrimSpace(string(out)))
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
