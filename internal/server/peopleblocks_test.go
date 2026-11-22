package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestPeopleBlocks: the profile card and the people of a group show only
// people the reader could list; guests see none; nothing about people the
// reader may not see.
func TestPeopleBlocks(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var c, g1, g2, ann, bob, ved, gone obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "People"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "guest", "permissions": 1}, nil, 201)
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Sales"}, &g1, 201)
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Board"}, &g2, 201)
	user := func(name, display string, groups []int64, out *obj) {
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": "Passw0rd!Passw0rd", "display_name": display, "email": name + "@example.com", "groups": groups}, out, 201)
	}
	user("ann", "Ann Okapi", []int64{g1.ID}, &ann)
	user("bob", "Bob Walrus", []int64{g2.ID}, &bob)
	user("ved", "Ved Reader", []int64{g1.ID}, &ved)
	user("gone", "Gone Lynx", []int64{g1.ID}, &gone)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d/profile", ann.ID), map[string]string{"job_title": "Head of Sales", "phone": "+43 1 234"}, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", gone.ID), map[string]any{"disabled": true}, nil, 200)
	src := fmt.Sprintf("::profilecard{user=%d}\n\n::profilecard{user=%d}\n\n::people{group=%d layout=list}\n\n::people{group=%d}\n\n::profilecard{user=\"1 OR 1=1\"}\n", ann.ID, bob.ID, g1.ID, g2.ID)
	var el obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Contacts", "body": map[string]any{"format": "markdown", "source": src}}, &el, 201)
	view := func(cl *client) string {
		var v struct {
			HTML string `json:"html"`
		}
		cl.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", el.ID), nil, &v, 200)
		return v.HTML
	}
	h := view(e.client())
	if strings.Contains(h, "Okapi") || strings.Contains(h, "Walrus") || strings.Contains(h, "example.com") || !strings.Contains(h, "signed-in people only") {
		t.Fatalf("people blocks for guests: %s", h)
	}
	vc := e.client()
	vc.login("ved", "Passw0rd!Passw0rd")
	h = view(vc)
	if !strings.Contains(h, "Ann Okapi") || !strings.Contains(h, "Head of Sales") || !strings.Contains(h, `href="tel:+431234"`) || !strings.Contains(h, "Ved Reader") {
		t.Fatalf("people of the own group: %s", h)
	}
	if strings.Contains(h, "Walrus") || strings.Contains(h, "bob@example.com") || strings.Contains(h, "Gone Lynx") || !strings.Contains(h, "You may not see these people") {
		t.Fatalf("people the reader may not see: %s", h)
	}
	h = view(admin)
	if !strings.Contains(h, "Bob Walrus") || strings.Contains(h, "Gone Lynx") {
		t.Fatalf("people blocks for the administrator: %s", h)
	}
}
