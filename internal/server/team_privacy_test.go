package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestTeamPrivacy: guests see the contact details of a team page only if
// the page is public, and only the permission content.team.public makes it
// public; attributes of the identity provider are chosen by who manages users.
func TestTeamPrivacy(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var c, g, zoe, ed obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "About us"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "guest", "permissions": 1}, nil, 201)
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Sales"}, &g, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "zoe", "password": "Passw0rd!Passw0rd", "display_name": "Zoe Zimmer", "email": "zoe@example.com", "groups": []int64{g.ID}}, &zoe, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d/profile", zoe.ID), map[string]string{"job_title": "Head of Sales", "phone": "+43 1 234"}, nil, 200)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ed", "password": "Passw0rd!Passw0rd", "display_name": "Ed", "groups": []int64{g.ID}}, &ed, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": ed.ID, "permissions": 1 | 2 | 4}, nil, 201)
	edc := e.client()
	edc.login("ed", "Passw0rd!Passw0rd")
	guest := e.client()
	body := func(public bool, fields ...string) map[string]any {
		return map[string]any{"groups": []int64{g.ID}, "fields": fields, "public": public, "description": map[string]string{"format": "markdown", "source": "We sell"}}
	}
	var team obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.team", "title": "Sales team", "body": body(false, "job_title", "email", "phone")}, &team, 201)
	view := func(cl *client) string {
		var v struct {
			HTML string `json:"html"`
		}
		cl.must("GET", fmt.Sprintf("/api/v1/elements/%d", team.ID), nil, &v, 200)
		var m struct {
			Sections []struct {
				Members []struct {
					Fields map[string]string `json:"fields"`
				} `json:"members"`
			} `json:"sections"`
		}
		cl.must("GET", fmt.Sprintf("/api/v1/x/content.team/elements/%d/members", team.ID), nil, &m, 200)
		out := v.HTML
		for _, s := range m.Sections {
			for _, x := range s.Members {
				out += fmt.Sprint(x.Fields)
			}
		}
		return out
	}
	h := view(guest)
	if !strings.Contains(h, "Zoe Zimmer") || !strings.Contains(h, "Head of Sales") || strings.Contains(h, "zoe@example.com") || strings.Contains(h, "234") {
		t.Fatalf("a team page for guests: %s", h)
	}
	if h := view(edc); !strings.Contains(h, "zoe@example.com") || !strings.Contains(h, "+43 1 234") {
		t.Fatalf("a team page for signed-in people: %s", h)
	}
	// only the permission makes it public
	save := func(cl *client, b map[string]any) int {
		return cl.do("PUT", fmt.Sprintf("/api/v1/elements/%d", team.ID), map[string]any{"lang": "en", "title": "Sales team", "body": b}, nil)
	}
	if code := save(edc, body(true, "job_title", "email", "phone")); code != 403 {
		t.Fatalf("made public without the permission: %d", code)
	}
	if code := edc.do("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.team", "title": "Mine", "body": body(true, "email")}, nil); code != 403 {
		t.Fatalf("a public team page created without the permission: %d", code)
	}
	if code := save(admin, body(true, "job_title", "email", "phone")); code != 200 {
		t.Fatalf("made public by the administrator: %d", code)
	}
	if h := view(guest); !strings.Contains(h, "zoe@example.com") {
		t.Fatalf("a public team page for guests: %s", h)
	}
	// a public page stays public for other editors, as long as groups and fields stay
	if code := save(edc, body(true, "email", "job_title", "phone")); code != 200 {
		t.Fatalf("a public page changed without changing groups or fields: %d", code)
	}
	if code := save(edc, body(true, "job_title", "email", "phone", "mobile")); code != 403 {
		t.Fatalf("a field added to a public page without the permission: %d", code)
	}
	if code := save(edc, body(false, "job_title", "email", "phone", "mobile")); code != 200 {
		t.Fatalf("a field added to a page that is not public: %d", code)
	}
	// attributes of the identity provider: only who manages users
	if code := save(edc, body(false, "job_title", "attr:employeeNumber")); code != 403 {
		t.Fatalf("an attribute chosen without managing users: %d", code)
	}
	if code := save(admin, body(false, "job_title", "attr:employeeNumber")); code != 200 {
		t.Fatalf("an attribute chosen by the administrator: %d", code)
	}
	if code := save(edc, body(false, "job_title", "attr:employeeNumber", "email")); code != 200 {
		t.Fatalf("an attribute that is there stays: %d", code)
	}
}
