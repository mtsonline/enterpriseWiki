package macro_http

import (
	"net"
	"net/url"
	"strings"
	"testing"
)

func TestAllowed(t *testing.T) {
	allow := []string{"https://status.example.com/api/", "http://intranet.local"}
	for raw, want := range map[string]bool{
		"https://status.example.com/api/v1":          true,
		"https://status.example.com/api":             false,
		"https://status.example.com/apix":            false,
		"https://status.example.com.evil.net/api/":   false,
		"http://status.example.com/api/":             false,
		"https://user:pw@status.example.com/api/x":   false,
		"http://intranet.local/anything":             true,
		"http://INTRANET.local/":                     true,
		"ftp://intranet.local/":                      false,
		"https://status.example.com/api/../secret":   true, // path is checked as sent; the server normalizes
		"https://status.example.com/api/%2e%2e/next": true,
	} {
		u, _ := url.Parse(raw)
		if got := Allowed(u, allow); got != want {
			t.Errorf("%s: %v, want %v", raw, got, want)
		}
	}
}

func TestInternal(t *testing.T) {
	for ip, want := range map[string]bool{
		"127.0.0.1": true, "10.1.2.3": true, "192.168.0.1": true, "172.16.5.4": true, "169.254.169.254": true,
		"100.64.0.1": true, "::1": true, "fd00::1": true, "fe80::1": true, "0.0.0.0": true,
		"8.8.8.8": false, "2a00:1450:4001::1": false,
	} {
		if got := Internal(net.ParseIP(ip)); got != want {
			t.Errorf("%s: %v", ip, got)
		}
	}
}

func TestJSON(t *testing.T) {
	v := map[string]any{"a": []any{map[string]any{"x": 1.0, "y": "<b>"}, map[string]any{"x": 2.5}}}
	out := JSONToHTML(JSONPath(v, "a"))
	if !strings.Contains(out, `<th scope="col">x</th><th scope="col">y</th>`) || !strings.Contains(out, "&lt;b&gt;") || !strings.Contains(out, "<td>2.5</td>") {
		t.Fatal(out)
	}
	if JSONToHTML(JSONPath(v, "a.1.x")) != `<span class="ew-http-value">2.5</span>` {
		t.Fatal(JSONToHTML(JSONPath(v, "a.1.x")))
	}
	if JSONPath(v, "a.9") != nil || JSONPath(v, "b.c") != nil {
		t.Fatal("missing path")
	}
}
