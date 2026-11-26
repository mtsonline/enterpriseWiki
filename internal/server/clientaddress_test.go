package server_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// The address of visitors behind reverse proxies: only trusted proxies
// are believed, the header is selectable, and a proxy that is not
// configured is reported (expert settings, audit log - once).
func TestClientAddressAndUntrustedProxy(t *testing.T) {
	e := setupWith(t, func(c *config.Config) { c.Server.ClientIPHeader = "forwarded" })
	admin := e.client()
	admin.login("admin", adminPW)
	get := func(header, value string) {
		req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/site", nil)
		req.Header.Set(header, value)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	// the test client is not a trusted proxy: its header is ignored and reported
	for range 5 {
		get("Forwarded", `for=203.0.113.9;proto=https`)
	}
	get("X-Forwarded-For", "203.0.113.9") // not the configured header: nothing to report
	var res struct {
		Proxy struct {
			Header    string `json:"header"`
			Trusted   int    `json:"trusted"`
			Untrusted []struct {
				Peer     string `json:"peer"`
				Requests int64  `json:"requests"`
			} `json:"untrusted"`
		} `json:"proxy"`
	}
	admin.must("GET", "/api/v1/admin/limits", nil, &res, 200)
	if res.Proxy.Header != "Forwarded" || res.Proxy.Trusted != 0 || len(res.Proxy.Untrusted) != 1 || res.Proxy.Untrusted[0].Requests != 5 {
		t.Fatalf("proxy: %+v", res.Proxy)
	}
	var n int
	for range 50 { // the audit record is written in the background
		if err := e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action='server.proxy_untrusted' AND after->>'peer'=$1`, res.Proxy.Untrusted[0].Peer).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n != 1 {
		t.Fatalf("audit records for 5 requests: %d", n)
	}
	// the forged address never reached the audit log
	if err := e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE ip='203.0.113.9'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("forged address in the audit log: %d %v", n, err)
	}
}
