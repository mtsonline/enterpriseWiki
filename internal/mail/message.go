// Package mail sends e-mail: an outbox in PostgreSQL (written in the same
// transaction as the change that causes a message), a background worker with
// retries and the transports SMTP, sendmail and log.
package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// Message is an e-mail to send.
type Message struct {
	To       []string // addresses
	Cc       []string
	From     string // empty = configured sender
	FromName string
	ReplyTo  string
	Subject  string
	Text     string // plain text body; an HTML alternative is generated
	Source   string // who created it (plugin id, "test", ...)
	Ref      string // reference, e.g. "case:42"
}

// MaxRecipients limits To + Cc of one message.
const MaxRecipients = 50

// CleanHeader removes line breaks and control characters (header injection).
func CleanHeader(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// ParseAddress validates a single plain address ("a@b.c" or "Name <a@b.c>")
// and returns the bare address.
func ParseAddress(s string) (string, error) {
	s = CleanHeader(s)
	a, err := mail.ParseAddress(s)
	if err != nil || !strings.Contains(a.Address, "@") || strings.ContainsAny(a.Address, " ,;<>\"") || strings.HasPrefix(a.Address, "-") {
		return "", fmt.Errorf("invalid e-mail address %q", s)
	}
	return a.Address, nil
}

// SplitAddresses splits a list separated by commas, semicolons or line breaks.
func SplitAddresses(s string) []string {
	out := []string{}
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func encodeAddress(name, addr string) string {
	if name == "" {
		return "<" + addr + ">"
	}
	return (&mail.Address{Name: CleanHeader(name), Address: addr}).String()
}

var linkRe = regexp.MustCompile(`https?://[^\s<>"']+[^\s<>"'.,;:!?)\]]`)

// TextToHTML renders plain text as simple HTML (paragraphs, line breaks, links).
func TextToHTML(text string) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><body style="font-family:Arial,Helvetica,sans-serif;font-size:15px;line-height:1.5;color:#172b4d">`)
	for _, para := range regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(text), -1) {
		esc := html.EscapeString(strings.TrimSpace(para))
		esc = linkRe.ReplaceAllStringFunc(esc, func(u string) string { return `<a href="` + u + `">` + u + `</a>` })
		b.WriteString("<p>" + strings.ReplaceAll(esc, "\n", "<br>") + "</p>")
	}
	b.WriteString("</body></html>")
	return b.String()
}

func qp(s string) string {
	var buf bytes.Buffer
	w := quotedprintable.NewWriter(&buf)
	_, _ = w.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")))
	_ = w.Close()
	return buf.String()
}

// Build renders the RFC 5322 message (CRLF line endings).
func Build(from, fromName, replyTo string, to, cc []string, subject, text string, now time.Time) []byte {
	rnd := make([]byte, 12)
	_, _ = rand.Read(rnd)
	boundary := "ewiki-" + hex.EncodeToString(rnd)
	domain := "localhost"
	if i := strings.LastIndex(from, "@"); i >= 0 {
		domain = from[i+1:]
	}
	enc := func(list []string) string {
		parts := make([]string, len(list))
		for i, a := range list {
			parts[i] = "<" + a + ">"
		}
		return strings.Join(parts, ", ")
	}
	var b strings.Builder
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", encodeAddress(fromName, from))
	h("To", enc(to))
	if len(cc) > 0 {
		h("Cc", enc(cc))
	}
	if replyTo != "" {
		h("Reply-To", "<"+replyTo+">")
	}
	h("Subject", mime.QEncoding.Encode("utf-8", CleanHeader(subject)))
	h("Date", now.Format(time.RFC1123Z))
	h("Message-ID", "<"+hex.EncodeToString(rnd)+"@"+domain+">")
	h("MIME-Version", "1.0")
	h("Auto-Submitted", "auto-generated")
	h("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	b.WriteString("--" + boundary + "\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	b.WriteString(qp(text) + "\r\n")
	b.WriteString("--" + boundary + "\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	b.WriteString(qp(TextToHTML(text)) + "\r\n")
	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}
