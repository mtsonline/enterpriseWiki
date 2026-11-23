package server_test

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestConsent: the privacy policy is accepted before the wiki is used in
// the browser; the policy itself, the site and signing out stay open;
// administrators and API keys are never locked out; a new version asks
// again; the proof lists who accepted what when (CSV without formulas).
func TestConsent(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Legal"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": false}, nil, 201)
	var policy, other elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Privacy", "body": htmlBody("<p>We keep okapi data.</p>")}, &policy, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Other", "body": htmlBody("<p>Lynx</p>")}, &other, 201)
	pw := "Passw0rd!Passw0rd"
	newUser := func(name, display string) *client {
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": pw, "display_name": display}, nil, 201)
		cl := e.client()
		cl.login(name, pw)
		return cl
	}
	bob := newUser("bob", "Bob")
	eve := newUser("eve", "=HYPERLINK(\"https://evil.example.com\")")
	carol := newUser("carol", "Carol")
	var key struct {
		Token string `json:"token"`
	}
	carol.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "k", "permissions": 1, "expires_at": time.Now().Add(time.Hour)}, &key, 201)
	keyed := e.client()
	keyed.token = key.Token
	read := func(cl *client, id int64) (int, string) {
		st, body, _ := cl.raw503("GET", fmt.Sprintf("/api/v1/elements/%d", id), nil)
		return st, body
	}

	// only the settings permission changes it; values are checked
	bob.must("PUT", "/api/v1/admin/consent", map[string]any{"privacy": map[string]any{"enabled": false}}, nil, 403)
	admin.must("PUT", "/api/v1/admin/consent", map[string]any{"privacy": map[string]any{"enabled": true, "version": 1}}, nil, 400)
	admin.must("PUT", "/api/v1/admin/consent", map[string]any{"privacy": map[string]any{"enabled": true, "version": 1, "page": 999999}}, nil, 400)
	admin.must("PUT", "/api/v1/admin/consent", map[string]any{"privacy": map[string]any{"enabled": true, "version": 1, "page": policy.ID, "guests": "always"}}, nil, 400)
	bob.must("POST", "/api/v1/me/consent", map[string]any{"kind": "privacy", "version": 1}, nil, 400) // not asked for yet
	admin.must("PUT", "/api/v1/admin/consent", map[string]any{
		"cookies": map[string]any{"enabled": true, "required": true, "version": 1, "text": map[string]string{"en": "Cookies here."}},
		"privacy": map[string]any{"enabled": true, "version": 1, "page": policy.ID, "guests": "required"}}, nil, 200)
	var site struct {
		Consent struct {
			Cookies struct {
				Required bool `json:"required"`
				Version  int  `json:"version"`
			} `json:"cookies"`
			Privacy struct {
				Page    int64  `json:"page"`
				Version int    `json:"version"`
				Guests  string `json:"guests"`
			} `json:"privacy"`
		} `json:"consent"`
	}
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if !site.Consent.Cookies.Required || site.Consent.Privacy.Page != policy.ID || site.Consent.Privacy.Guests != "required" || site.Consent.Privacy.Version != 1 {
		t.Fatalf("site: %+v", site.Consent)
	}

	// before accepting: only the policy, the site and signing out
	if st, body := read(bob, other.ID); st != 403 || !strings.Contains(body, "consent_required") {
		t.Fatalf("reading before accepting: %d %s", st, body)
	}
	if st, _ := read(bob, policy.ID); st != 200 {
		t.Fatalf("reading the policy: %d", st)
	}
	bob.must("GET", "/api/v1/site", nil, nil, 200)
	var mine struct {
		Enabled         bool `json:"enabled"`
		Version         int  `json:"version"`
		AcceptedVersion int  `json:"accepted_version"`
	}
	bob.must("GET", "/api/v1/me/consent", nil, &mine, 200)
	if !mine.Enabled || mine.Version != 1 || mine.AcceptedVersion != 0 {
		t.Fatalf("own consent: %+v", mine)
	}
	// administrators, API keys and guests are not locked out
	if st, _ := read(admin, other.ID); st != 200 {
		t.Fatalf("administrator: %d", st)
	}
	if st, _ := read(keyed, other.ID); st != 200 {
		t.Fatalf("API key: %d", st)
	}
	if st, _ := read(e.client(), other.ID); st != 200 {
		t.Fatalf("guest: %d", st)
	}
	// accepting: only the version in force
	bob.must("POST", "/api/v1/me/consent", map[string]any{"kind": "privacy", "version": 2}, nil, 409)
	bob.must("POST", "/api/v1/me/consent", map[string]any{"kind": "cookies", "version": 1}, nil, 400)
	bob.must("POST", "/api/v1/me/consent", map[string]any{"kind": "privacy", "version": 1}, nil, 200)
	bob.must("POST", "/api/v1/me/consent", map[string]any{"kind": "privacy", "version": 1}, nil, 200) // again: no second entry
	eve.must("POST", "/api/v1/me/consent", map[string]any{"kind": "privacy", "version": 1}, nil, 200)
	if st, _ := read(bob, other.ID); st != 200 {
		t.Fatalf("reading after accepting: %d", st)
	}
	if st, _ := read(carol, other.ID); st != 403 {
		t.Fatalf("another person in the browser: %d", st)
	}
	bob.must("POST", "/api/v1/auth/logout", nil, nil, 204)

	// the proof
	var list struct {
		Items []struct {
			Username string `json:"username"`
			Version  int    `json:"version"`
		} `json:"items"`
		Total int `json:"total"`
	}
	admin.must("GET", "/api/v1/admin/consents", nil, &list, 200)
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("proof: %+v", list)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/admin/consents?format=csv", nil)
	req.Header.Set("X-Requested-With", "ewiki")
	res, err := admin.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\xef\xbb\xbf"))).ReadAll()
	if err != nil || len(rows) != 3 || !strings.Contains(string(data), "bob") {
		t.Fatalf("CSV: %v\n%s", err, data)
	}
	for _, row := range rows {
		for _, v := range row {
			if v != "" && strings.ContainsRune("=+-@", rune(v[0])) {
				t.Fatalf("a cell starts like a formula: %q", v)
			}
		}
	}
	var log struct {
		Total int `json:"total"`
	}
	admin.must("GET", "/api/v1/admin/audit/search?action=consent.privacy.accept", nil, &log, 200)
	if log.Total != 2 {
		t.Fatalf("audit: %d entries", log.Total)
	}

	// a new version asks everybody again; versions never go down
	admin.must("PUT", "/api/v1/admin/consent", map[string]any{"privacy": map[string]any{"enabled": true, "version": 2, "page": policy.ID}}, nil, 200)
	if st, _ := read(eve, other.ID); st != 403 {
		t.Fatalf("after a new version: %d", st)
	}
	admin.must("PUT", "/api/v1/admin/consent", map[string]any{"privacy": map[string]any{"enabled": true, "version": 1, "page": policy.ID}}, nil, 400)
	// switched off: open again
	admin.must("PUT", "/api/v1/admin/consent", map[string]any{"privacy": map[string]any{"enabled": false, "version": 2}}, nil, 200)
	if st, _ := read(eve, other.ID); st != 200 {
		t.Fatalf("switched off: %d", st)
	}
}
