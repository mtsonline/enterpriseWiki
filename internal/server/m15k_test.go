package server_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// Each API key can have its own limit of requests: the owner limits a key
// below the limit of the account, an administrator grants a key a budget
// of its own.
func TestAPIKeyRateLimit(t *testing.T) {
	e := setupWith(t, func(c *config.Config) {
		c.Server.RateLimit.Disabled = false
		c.Limits = map[string]int64{"core.rate.user_per_second": 5, "core.rate.user_burst": 40}
	})
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "robot", "password": "Robot-Passw0rd!", "display_name": "Robot", "roles": []int64{}}, nil, 201)
	owner := e.client()
	owner.login("robot", "Robot-Passw0rd!")
	type key struct {
		ID            int64  `json:"id"`
		Token         string `json:"token"`
		RatePerSecond *int   `json:"rate_per_second"`
		RateOwn       bool   `json:"rate_own"`
		Owner         string `json:"owner"`
	}
	// the owner cannot go above the limit of the account
	owner.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "fast", "permissions": 1, "rate_per_second": 6}, nil, 400)
	var slow, plain key
	owner.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "slow", "permissions": 1, "rate_per_second": 1}, &slow, 201)
	owner.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "plain", "permissions": 1}, &plain, 201)
	burst := func(token string, n int) map[int]int {
		c := e.client()
		c.token = token
		codes := map[int]int{}
		for range n {
			codes[c.do("GET", "/api/v1/auth/me", nil, nil)]++
		}
		return codes
	}
	// 1 per second with the reserve in the proportion of accounts (8)
	if codes := burst(slow.Token, 14); codes[200] < 8 || codes[200] > 9 || codes[429] == 0 {
		t.Fatalf("limited key: %v", codes)
	}
	// the key without a limit still has the budget of the account
	if codes := burst(plain.Token, 14); codes[429] != 0 {
		t.Fatalf("key without own limit: %v", codes)
	}
	// a key changes no limits, the owner none above the account, others none at all
	k := e.client()
	k.token = plain.Token
	k.must("PUT", fmt.Sprintf("/api/v1/me/api-keys/%d/rate", plain.ID), map[string]any{"rate_per_second": 1}, nil, 403)
	owner.must("PUT", fmt.Sprintf("/api/v1/me/api-keys/%d/rate", plain.ID), map[string]any{"rate_per_second": 99}, nil, 400)
	owner.must("PUT", fmt.Sprintf("/api/v1/admin/api-keys/%d/rate", plain.ID), map[string]any{"rate_per_second": 99}, nil, 403)
	owner.must("GET", "/api/v1/admin/api-keys", nil, nil, 403)
	admin.must("PUT", fmt.Sprintf("/api/v1/me/api-keys/%d/rate", plain.ID), map[string]any{"rate_per_second": 2}, nil, 404)
	// the administrator grants a budget of its own, above the account
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/api-keys/%d/rate", plain.ID), map[string]any{"rate_per_second": 100}, nil, 204)
	var all []key
	admin.must("GET", "/api/v1/admin/api-keys", nil, &all, 200)
	found := false
	for _, x := range all {
		if x.ID == plain.ID {
			found = x.RateOwn && x.RatePerSecond != nil && *x.RatePerSecond == 100 && x.Owner == "robot@local" && x.Token == ""
		}
	}
	if !found {
		t.Fatalf("list of the administrator: %+v", all)
	}
	if codes := burst(plain.Token, 120); codes[429] != 0 {
		t.Fatalf("key with own budget: %v", codes)
	}
	// ... which the owner cannot change or take over
	owner.must("PUT", fmt.Sprintf("/api/v1/me/api-keys/%d/rate", plain.ID), map[string]any{"rate_per_second": 5}, nil, 404)
	var n int
	if err := e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action='apikey.rate'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit entries: %d %v", n, err)
	}
}

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
