package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestTrustedProxiesAndTLSAllowed(t *testing.T) {
	t.Setenv("EWIKI_TRUSTED_PROXIES", "127.0.0.1, 10.9.0.0/16, junk")
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	ip := func(xff ...string) string {
		t.Helper()
		req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/x/mfa.network/me", nil)
		for _, x := range xff {
			req.Header.Add("X-Forwarded-For", x)
		}
		res, err := admin.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out struct {
			IP string `json:"current_ip"`
		}
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out.IP
	}
	if got := ip(); got != "127.0.0.1" {
		t.Fatalf("no header: %q", got)
	}
	// the client forged the left entry; the proxy appended the real address
	if got := ip("1.1.1.1, 203.0.113.5"); got != "203.0.113.5" {
		t.Fatalf("forged entry used: %q", got)
	}
	// a chain of trusted proxies is skipped
	if got := ip("198.51.100.7", "10.9.1.1, 10.9.2.2"); got != "198.51.100.7" {
		t.Fatalf("proxy chain: %q", got)
	}
	if got := ip("garbage"); got != "127.0.0.1" {
		t.Fatalf("garbage: %q", got)
	}

	// on-demand TLS: only the wiki's own host names
	status := func(domain string) int {
		res, err := http.Get(e.srv.URL + "/tls-allowed?domain=" + domain)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if s := status("localhost"); s != 200 { // host of the base URL http://localhost:8080
		t.Fatalf("base URL host: %d", s)
	}
	if s := status("evil.example.com"); s != 404 {
		t.Fatalf("foreign host: %d", s)
	}
	if s := status(""); s != 404 {
		t.Fatalf("empty: %d", s)
	}
}
