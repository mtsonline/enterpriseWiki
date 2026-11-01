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
