package mail

import (
	"bytes"
	"io"
	"mime"
	netmail "net/mail"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

func TestBuild(t *testing.T) {
	data := Build("wiki@example.com", "Wiki \"Bot\"", "hr@example.com", []string{"a@example.com"}, []string{"b@example.com"},
		"Grüße\r\nBcc: x@evil", "Hallo Ä\n\nsee https://example.com/x?a=1&b=2", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	msg, err := netmail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	subj, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subj != "Grüße Bcc: x@evil" || msg.Header.Get("Bcc") != "" {
		t.Fatalf("subject %q", subj)
	}
	if msg.Header.Get("Reply-To") != "<hr@example.com>" || msg.Header.Get("Cc") != "<b@example.com>" || msg.Header.Get("Auto-Submitted") != "auto-generated" {
		t.Fatalf("headers %v", msg.Header)
	}
	from, err := netmail.ParseAddress(msg.Header.Get("From"))
	if err != nil || from.Name != `Wiki "Bot"` || from.Address != "wiki@example.com" {
		t.Fatalf("from %v %v", from, err)
	}
	body, _ := io.ReadAll(msg.Body)
	if !strings.Contains(string(body), "Content-Type: text/html") || !strings.Contains(string(body), "Hallo =C3=84") {
		t.Fatalf("body %s", body)
	}
	if !strings.Contains(TextToHTML("a <b>\nhttps://x.org/y"), `a &lt;b&gt;<br><a href="https://x.org/y">`) {
		t.Fatal(TextToHTML("a <b>\nhttps://x.org/y"))
	}
}

func TestAddresses(t *testing.T) {
	for in, ok := range map[string]bool{"a@b.de": true, "Ann <a@b.de>": true, "a@b.de, c@d.de": false, "-x@y.de": false, "nobody": false, "a@b.de\r\nBcc: c@d": false} {
		if _, err := ParseAddress(in); (err == nil) != ok {
			t.Errorf("%q: %v", in, err)
		}
	}
	if got := SplitAddresses("a@b.de; c@d.de,\n e@f.de ,"); len(got) != 3 {
		t.Fatal(got)
	}
	st, err := config.Open(t.TempDir() + "/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(func(c *config.Config) error {
		c.Mail.From, c.Mail.AllowedFrom = "wiki@example.com", []string{"*@hr.example.com", "boss@example.com"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := &Service{cfg: st}
	for addr, ok := range map[string]bool{"wiki@example.com": true, "jobs@hr.example.com": true, "BOSS@example.com": true, "x@example.com": false, "a@evilhr.example.com": false, "x@y@hr.example.com": false} {
		if s.SenderAllowed(addr) != ok {
			t.Errorf("%s: %v", addr, !ok)
		}
	}
}

func TestParseBounce(t *testing.T) {
	// no VERP: the X-Ewiki-Message header of the returned message and the DSN recipient
	raw := strings.ReplaceAll(`From: postmaster@example.net
To: bounces@example.com
Subject: Delivery Status Notification (Failure)
Content-Type: multipart/report; report-type=delivery-status; boundary="X"

--X
Content-Type: message/delivery-status

Reporting-MTA: dns; mx

Final-Recipient: rfc822;<a@example.com>
Action: failed
Status: 5.2.2
Diagnostic-Code: smtp; 552 mailbox full,
  please try again

Final-Recipient: rfc822; b@example.com
Action: delivered
Status: 2.0.0

--X
Content-Type: message/rfc822

X-Ewiki-Message: 77
Subject: Hi

body
--X--
`, "\n", "\r\n")
	bs := ParseBounce([]byte(raw))
	if len(bs) != 1 || bs[0].Addr != "a@example.com" || bs[0].MessageID != 77 || bs[0].Status != "5.2.2" || !strings.Contains(bs[0].Diagnosis, "please try again") {
		t.Fatalf("%+v", bs)
	}
	// VERP only, plain text bounce
	raw = "From: x@y\r\nTo: bounces+m5r9@example.com\r\nSubject: Undeliverable: Hi\r\n\r\nsorry\r\n"
	bs = ParseBounce([]byte(raw))
	if len(bs) != 1 || bs[0].MessageID != 5 || bs[0].RcptID != 9 {
		t.Fatalf("%+v", bs)
	}
	if len(ParseBounce([]byte("From: a@b\r\nSubject: Hello\r\n\r\nnot a bounce"))) != 0 {
		t.Fatal("normal mail treated as bounce")
	}
}
