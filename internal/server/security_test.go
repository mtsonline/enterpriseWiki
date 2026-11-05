package server_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestSecurityBaseline probes the HTTP layer the way an attacker would:
// headers, cookies, access control, injection, traversal, brute force,
// Host header and oversized requests. Every new release must pass it.
func TestSecurityBaseline(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "mallory", "password": "Mallory-Passw0rd!", "display_name": "Mallory"}, nil, 201)
	mal := e.client()
	mal.login("mallory", "Mallory-Passw0rd!")
	guest := e.client()

	raw := func(method, path string, hdr map[string]string, body io.Reader) (*http.Response, string) {
		t.Helper()
		req, err := http.NewRequest(method, e.srv.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range hdr {
			if k == "Host" {
				req.Host = v
			} else {
				req.Header.Set(k, v)
			}
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}

	t.Run("security headers", func(t *testing.T) {
		for _, p := range []string{"/", "/api/v1/site", "/wiki/x"} {
			res, _ := raw("GET", p, nil, nil)
			for h, want := range map[string]string{
				"X-Content-Type-Options": "nosniff", "X-Frame-Options": "SAMEORIGIN",
				"Referrer-Policy": "strict-origin-when-cross-origin", "Cross-Origin-Opener-Policy": "same-origin",
			} {
				if got := res.Header.Get(h); got != want {
					t.Errorf("%s: %s = %q, want %q", p, h, got, want)
				}
			}
			csp := res.Header.Get("Content-Security-Policy")
			for _, d := range []string{"default-src 'self'", "script-src 'self'", "object-src 'none'", "frame-ancestors 'self'", "base-uri 'self'"} {
				if !strings.Contains(csp, d) {
					t.Errorf("%s: CSP lacks %q: %s", p, d, csp)
				}
			}
			if strings.Contains(csp, "unsafe-eval") || strings.Contains(strings.SplitN(csp, "style-src", 2)[0], "unsafe-inline") {
				t.Errorf("%s: CSP allows unsafe scripts: %s", p, csp)
			}
		}
		res, _ := raw("GET", "/api/v1/site", nil, nil)
		if res.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("API responses must not be cached: %q", res.Header.Get("Cache-Control"))
		}
	})

	t.Run("session cookie", func(t *testing.T) {
		b := strings.NewReader(`{"username":"mallory","password":"Mallory-Passw0rd!"}`)
		res, _ := raw("POST", "/api/v1/auth/login", map[string]string{"Content-Type": "application/json", "X-Requested-With": "ewiki"}, b)
		var found bool
		for _, c := range res.Cookies() {
			if c.Value == "" {
				continue
			}
			found = true
			if !c.HttpOnly || c.SameSite == http.SameSiteNoneMode || c.SameSite == http.SameSiteDefaultMode || c.Path != "/" {
				t.Errorf("cookie %s: HttpOnly=%v SameSite=%v Path=%q", c.Name, c.HttpOnly, c.SameSite, c.Path)
			}
			if len(c.Value) < 32 {
				t.Errorf("session token too short: %d", len(c.Value))
			}
		}
		if !found {
			t.Fatal("no session cookie")
		}
	})

	t.Run("access control", func(t *testing.T) {
		for _, p := range []string{"/api/v1/admin/users", "/api/v1/admin/settings", "/api/v1/admin/audit", "/api/v1/admin/sites", "/api/v1/admin/plugins"} {
			if c := guest.do("GET", p, nil, nil); c != 401 && c != 404 {
				t.Errorf("guest GET %s: %d", p, c)
			}
			if c := mal.do("GET", p, nil, nil); c != 403 && c != 404 {
				t.Errorf("user GET %s: %d", p, c)
			}
		}
		// privilege escalation: change own roles, another user, settings
		if c := mal.do("PUT", "/api/v1/admin/users/1", map[string]any{"password": "Pwned-Passw0rd!!"}, nil); c != 403 {
			t.Errorf("user changed admin: %d", c)
		}
		if c := mal.do("POST", "/api/v1/admin/roles", map[string]any{"name": "x", "is_admin": true}, nil); c != 403 && c != 404 && c != 405 {
			t.Errorf("user created admin role: %d", c)
		}
		if c := mal.do("PATCH", "/api/v1/me", map[string]any{"is_admin": true, "roles": []int{1}}, nil); c == 200 {
			var me struct {
				IsAdmin bool `json:"is_admin"`
			}
			mal.must("GET", "/api/v1/me", nil, &me, 200)
			if me.IsAdmin {
				t.Fatal("mass assignment made the user an administrator")
			}
		}
		// API keys cannot exceed the owner's rights
		var key struct {
			Token string `json:"token"`
		}
		if c := mal.do("POST", "/api/v1/me/api-keys", map[string]any{"name": "k", "allow_admin": true, "permissions": 31}, &key); c < 300 && key.Token != "" {
			kc := e.client()
			kc.token = key.Token
			if c := kc.do("GET", "/api/v1/admin/users", nil, nil); c != 403 {
				t.Errorf("API key of a normal user reached the administration: %d", c)
			}
		}
	})

	t.Run("injection probes never cause server errors", func(t *testing.T) {
		probes := []string{`' OR '1'='1`, `1; DROP TABLE users;--`, `") OR 1=1--`, `%' AND pg_sleep(5)--`, `{{7*7}}`, `${jndi:ldap://x}`, `<script>alert(1)</script>`, "\x00", `../../../../etc/passwd`, `\u0000`}
		for _, pr := range probes {
			q := url.QueryEscape(pr)
			for _, p := range []string{"/api/v1/search?q=" + q, "/api/v1/admin/users?q=" + q, "/api/v1/elements?tag=" + q, "/api/v1/resolve?path=" + q, "/api/v1/not-found?path=" + q, "/api/v1/users/search?q=" + q} {
				if c := admin.do("GET", p, nil, nil); c >= 500 {
					t.Errorf("GET %s: %d", p, c)
				}
			}
			if c := admin.do("POST", "/api/v1/auth/login", map[string]string{"username": pr, "password": pr}, nil); c >= 500 {
				t.Errorf("login probe %q: %d", pr, c)
			}
		}
		var n struct {
			Total int `json:"total"`
		}
		_ = n
		// the users table is still there
		admin.must("GET", "/api/v1/admin/users", nil, nil, 200)
	})

	t.Run("path traversal", func(t *testing.T) {
		for _, p := range []string{"/assets/../../../../etc/passwd", "/..%2f..%2f..%2fetc%2fpasswd", "/%2e%2e/%2e%2e/config.yaml", "/assets/..%5c..%5cconfig.yaml",
			"/api/v1/files/..%2f..%2fconfig.yaml", "/config.yaml", "/.env", "/data/config.yaml", "/.git/config"} {
			res, body := raw("GET", p, nil, nil)
			if strings.Contains(body, "root:x:") || strings.Contains(body, "database:") || strings.Contains(body, "POSTGRES_PASSWORD") || strings.Contains(body, "[core]") {
				t.Errorf("%s leaked a file (%d)", p, res.StatusCode)
			}
		}
	})

	t.Run("XSS in stored content and names", func(t *testing.T) {
		payload := `<img src=x onerror=alert(1)><svg onload=alert(2)><a href="javascript:alert(3)">x</a><iframe src="https://evil.example"></iframe>`
		var cat struct {
			ID int64 `json:"id"`
		}
		admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": `Cat<script>alert(4)</script>`}}, &cat, 201)
		var el struct {
			ID   int64  `json:"id"`
			HTML string `json:"html"`
		}
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "lang": "en", "title": `T"><script>alert(5)</script>`, "body": htmlBody(payload)}, &el, 201)
		for _, bad := range []string{"onerror", "onload", "javascript:", "<iframe", "<script"} {
			if strings.Contains(strings.ToLower(el.HTML), bad) {
				t.Errorf("stored XSS: %q survived: %s", bad, el.HTML)
			}
		}
		// prerendered pages for guests escape titles
		_, body := raw("GET", fmt.Sprintf("/e/%d", el.ID), nil, nil)
		if strings.Contains(body, "<script>alert(5)") || strings.Contains(body, "<script>alert(4)") {
			t.Error("title not escaped in the prerendered page")
		}
	})

	t.Run("brute force lockout", func(t *testing.T) {
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "victim", "password": "Victim-Passw0rd!"}, nil, 201)
		c := e.client()
		for i := 0; i < 12; i++ {
			c.do("POST", "/api/v1/auth/login", map[string]string{"username": "victim", "password": fmt.Sprintf("wrong-%d", i)}, nil)
		}
		if code := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "victim", "password": "Victim-Passw0rd!"}, nil); code == 200 {
			t.Fatal("no lockout after repeated failures")
		}
		// a forged X-Forwarded-For from an untrusted peer does not reset the limits
		res, _ := raw("POST", "/api/v1/auth/login", map[string]string{"Content-Type": "application/json", "X-Requested-With": "ewiki", "X-Forwarded-For": "198.51.100.1"},
			strings.NewReader(`{"username":"victim","password":"Victim-Passw0rd!"}`))
		if res.StatusCode == 200 {
			t.Fatal("lockout bypassed with X-Forwarded-For")
		}
		// unknown and known users get the same answer (no user enumeration)
		u1 := e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "nobody-here", "password": "x"}, nil)
		u2 := e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "mallory", "password": "x"}, nil)
		if u1 != u2 {
			t.Errorf("user enumeration: unknown %d, known %d", u1, u2)
		}
	})

	t.Run("Host header does not change links", func(t *testing.T) {
		for _, p := range []string{"/", "/sitemap.xml", "/api/v1/feeds/changes.atom"} {
			_, body := raw("GET", p, map[string]string{"Host": "evil.example", "X-Forwarded-Host": "evil.example"}, nil)
			if strings.Contains(body, "evil.example") {
				t.Errorf("%s reflects a forged Host header", p)
			}
		}
	})

	t.Run("CSRF and content types", func(t *testing.T) {
		// cookie-authenticated writes need the custom header (cross-site forms cannot set it)
		jar := admin.http.Jar
		req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/categories", strings.NewReader(`titles[en]=x`))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := (&http.Client{Jar: jar}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Errorf("form post without CSRF header: %d", res.StatusCode)
		}
	})

	t.Run("oversized requests", func(t *testing.T) {
		big := bytes.Repeat([]byte("a"), 70<<20)
		res, _ := raw("POST", "/api/v1/auth/login", map[string]string{"Content-Type": "application/json", "X-Requested-With": "ewiki"}, bytes.NewReader(big))
		if res.StatusCode < 400 || res.StatusCode >= 500 {
			t.Errorf("70 MB body: %d", res.StatusCode)
		}
	})
}
