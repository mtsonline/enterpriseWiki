package server_test

// Tests for gaps the trap run (tools/traps) found on 18.11.2026: the code
// was right, but no test would have noticed if it broke.

import (
	"fmt"
	"testing"
)

// TestDuplicateSkipsStrictlyConfidentialPages: duplicating a category does
// not copy a page that is strictly confidential itself (its own
// classification), even when the category is not.
func TestDuplicateSkipsStrictlyConfidentialPages(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID     int64             `json:"id"`
		Titles map[string]string `json:"titles"`
	}
	var cat, target, open, secret, cp obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &cat, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Copies"}}, &target, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Open", "body": map[string]any{"format": "html", "source": "<p>x</p>"}}, &open, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Salaries", "body": map[string]any{"format": "html", "source": "<p>x</p>"}}, &secret, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d/acl", secret.ID), map[string]any{"classification": 3}, nil, 200)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/duplicate", cat.ID), map[string]any{"parent_id": target.ID}, &cp, 201)
	var got struct {
		Elements []obj `json:"elements"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/categories/%d", cp.ID), nil, &got, 200)
	if len(got.Elements) != 1 {
		t.Fatalf("copied pages: %+v (want only the open one)", got.Elements)
	}
	for _, el := range got.Elements {
		if el.ID == secret.ID || el.Titles["en"] != "Open" { // (copies of whole categories keep their titles)
			t.Fatalf("a strictly confidential page was copied: %+v", got.Elements)
		}
	}
}

// TestDisabledAccountIsLockedOut: a disabled account can do nothing any
// more – not with a session and not with an API key (sessions are deleted
// when an account is disabled, keys are not).
func TestDisabledAccountIsLockedOut(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "leaver", "password": "Passw0rd!Passw0rd"}, &u, 201)
	c := e.client()
	c.login("leaver", "Passw0rd!Passw0rd")
	var key struct {
		Token string `json:"token"`
	}
	c.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "script", "permissions": 1}, &key, 201)
	k := e.client()
	k.token = key.Token
	k.must("GET", "/api/v1/auth/me", nil, nil, 200)

	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), map[string]any{"disabled": true}, nil, 200)
	if code := k.do("GET", "/api/v1/auth/me", nil, nil); code != 401 {
		t.Fatalf("API key of a disabled account still works: %d", code)
	}
	if code := k.do("GET", "/api/v1/categories", nil, nil); code != 401 {
		t.Fatalf("API key of a disabled account lists categories: %d", code)
	}
	// the session was ended: the browser is a guest again
	var me struct {
		User any `json:"user"`
	}
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User != nil {
		t.Fatalf("session of a disabled account still signed in: %+v", me)
	}
	if code := c.do("GET", "/api/v1/me/api-keys", nil, nil); code != 401 {
		t.Fatalf("session of a disabled account still works: %d", code)
	}
	if code := e.client().do("POST", "/api/v1/auth/login", map[string]string{"username": "leaver", "password": "Passw0rd!Passw0rd"}, nil); code == 200 {
		t.Fatal("a disabled account signed in")
	}
	// enabled again: the key works again (it was not deleted)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), map[string]any{"disabled": false}, nil, 200)
	k.must("GET", "/api/v1/auth/me", nil, nil, 200)
}
