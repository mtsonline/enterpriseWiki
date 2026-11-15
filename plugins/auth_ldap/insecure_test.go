package auth_ldap

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestInsecureSkipVerify: a server with a certificate nobody vouches for is
// refused by default; with insecure_skip_verify the connection is made, and
// every such connection is logged as a warning.
func TestInsecureSkipVerify(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	srv.StartTLS() // self-signed certificate
	defer srv.Close()
	url := "ldaps://" + srv.Listener.Addr().String()
	var logs syncBuf
	p := &Provider{id: "ldap", log: slog.New(slog.NewTextHandler(&logs, nil))}

	if c, err := p.dial(map[string]any{"url": url, "timeout_seconds": "5"}); err == nil {
		c.Close()
		t.Fatal("connected to a server with an unverifiable certificate")
	}
	if strings.Contains(logs.String(), "insecure_skip_verify") {
		t.Fatalf("warning without the setting: %s", logs.String())
	}

	c, err := p.dial(map[string]any{"url": url, "timeout_seconds": "5", "insecure_skip_verify": true})
	if err != nil {
		t.Fatalf("insecure_skip_verify did not connect: %v", err)
	}
	c.Close()
	if out := logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "insecure_skip_verify") || !strings.Contains(out, "provider=ldap") {
		t.Fatalf("connection without verification was not logged: %s", out)
	}
}
