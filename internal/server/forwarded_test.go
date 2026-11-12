package server

import (
	"net/http"
	"testing"
	"time"
)

func TestForwardedClient(t *testing.T) {
	trusted := []string{"10.0.0.0/8", "192.0.2.1"}
	for _, c := range []struct{ kind, header, value, want string }{
		{"", "X-Forwarded-For", "203.0.113.7", "203.0.113.7"},
		{"x-forwarded-for", "X-Forwarded-For", "6.6.6.6, 203.0.113.7, 10.1.2.3", "203.0.113.7"}, // left of the client: forged
		{"", "X-Forwarded-For", "garbage", "10.0.0.1"},
		{"", "X-Forwarded-For", "10.1.1.1, 10.1.1.2", "10.1.1.1"}, // only proxies: the outermost
		{"forwarded", "Forwarded", `for=6.6.6.6, for="203.0.113.7:4711";proto=https, for=10.1.2.3`, "203.0.113.7"},
		{"forwarded", "Forwarded", `For="[2001:db8::7]:4711"`, "2001:db8::7"},
		{"forwarded", "Forwarded", `for=unknown`, "10.0.0.1"},
		{"forwarded", "Forwarded", `for=203.0.113.7, proto=https`, "10.0.0.1"}, // the nearest element names nobody
		{"forwarded", "X-Forwarded-For", "203.0.113.7", "10.0.0.1"},            // not the configured header
		{"X-Real-IP", "X-Real-Ip", "203.0.113.7", "203.0.113.7"},
		{"cf-connecting-ip", "CF-Connecting-IP", "2001:db8::7", "2001:db8::7"},
		{"x-real-ip", "X-Real-Ip", "203.0.113.7, 1.1.1.1", "10.0.0.1"},
	} {
		h := http.Header{}
		h.Set(c.header, c.value)
		if got := forwardedClient(h, c.kind, trusted, "10.0.0.1"); got != c.want {
			t.Errorf("%s %q: %s, want %s", c.kind, c.value, got, c.want)
		}
	}
}

// Anybody can send forwarding headers: few entries, one audit record per
// peer and day.
func TestUntrustedForwardsAreBounded(t *testing.T) {
	var u untrustedForwards
	now := time.Now()
	audits := 0
	for i := range 1000 {
		for _, peer := range []string{"198.51.100.1", "198.51.100.2"} {
			if u.note(peer, "X-Forwarded-For", now.Add(time.Duration(i)*time.Second)) {
				audits++
			}
		}
	}
	if audits != 2 {
		t.Fatalf("audit records: %d", audits)
	}
	for i := range 500 {
		if u.note("203.0.113."+string(rune('0'+i%10))+string(rune('0'+i/10%10))+string(rune('0'+i/100)), "X-Forwarded-For", now) {
			audits++
		}
	}
	if len(u.list()) != untrustedForwardPeers || audits != untrustedForwardPeers {
		t.Fatalf("peers %d, audit records %d", len(u.list()), audits)
	}
	if !u.note("198.51.100.1", "X-Forwarded-For", now.Add(25*time.Hour)) {
		t.Fatal("a known peer is recorded again after a day")
	}
}
