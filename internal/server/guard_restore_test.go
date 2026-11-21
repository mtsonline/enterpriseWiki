package server_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestGuardOnRestoreAndRollback: element types with own permissions (here
// forms: only their creator or a forms administrator may change them)
// decide also when an old revision is restored or a category is rolled
// back – both change the content. A person with only the permissions for
// pages can neither restore an old version of a form nor roll it back or
// move it to the trash that way; pages are rolled back as before.
func TestGuardOnRestoreAndRollback(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Office"}}, &c, 201)
	var ed idResp
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ed", "password": "Ed-Passw0rd!xyz", "display_name": "Ed"}, &ed, 201)
	// view, create, edit, delete for pages – no permissions of forms
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": ed.ID, "permissions": 1 | 2 | 4 | 8, "inherit": true}, nil, 201)
	edc := e.client()
	edc.login("ed", "Ed-Passw0rd!xyz")

	form := func(intro string) map[string]any {
		return map[string]any{"intro": intro, "submit_label": "Send", "success_message": "Thanks",
			"fields": []map[string]any{{"id": "name", "type": "text", "label": "Name"}}, "targets": map[string]any{"store": true}}
	}
	var f, page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.forms", "title": "Order", "body": form("first")}, &f, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Notes", "body": htmlBody("<p>first</p>")}, &page, 201)
	time.Sleep(20 * time.Millisecond)
	at := time.Now()
	time.Sleep(20 * time.Millisecond)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", f.ID), map[string]any{"lang": "en", "title": "Order", "body": form("second")}, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Notes", "body": htmlBody("<p>second</p>")}, nil, 200)
	var late elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.forms", "title": "Late", "body": form("late")}, &late, 201)

	intro := func(id int64) string {
		var x map[string]any
		admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en&edit=1", id), nil, &x, 200)
		return fmt.Sprint(x["body"])
	}
	// changing the form directly is refused (the guard)…
	if code := edc.do("PUT", fmt.Sprintf("/api/v1/elements/%d", f.ID), map[string]any{"lang": "en", "title": "Order", "body": form("mine")}, nil); code != 403 {
		t.Fatalf("change of the form by a page editor: %d", code)
	}
	// …and so is restoring an old revision
	if code := edc.do("POST", fmt.Sprintf("/api/v1/elements/%d/restore", f.ID), map[string]any{"lang": "en", "rev": 1}, nil); code != 403 {
		t.Fatalf("restore of an old form revision by a page editor: %d", code)
	}
	if got := intro(f.ID); !strings.Contains(got, "second") {
		t.Fatalf("form after a refused restore: %q", got)
	}
	// the rollback of the category restores the page, but not the form, and
	// does not move the newer form to the trash
	var rb struct {
		Revisions int `json:"revisions"`
		Trashed   int `json:"trashed"`
		Skipped   int `json:"skipped"`
	}
	edc.must("POST", fmt.Sprintf("/api/v1/categories/%d/rollback", c.ID), map[string]any{"at": at}, &rb, 200)
	if rb.Revisions != 1 || rb.Trashed != 0 || rb.Skipped != 2 {
		t.Fatalf("rollback by a page editor: %+v", rb)
	}
	if got := intro(f.ID); !strings.Contains(got, "second") {
		t.Fatalf("form after the rollback: %q", got)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", late.ID), nil, nil, 200)
	var p struct {
		HTML string `json:"html"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", page.ID), nil, &p, 200)
	if !strings.Contains(p.HTML, "first") {
		t.Fatalf("page after the rollback: %s", p.HTML)
	}
	// the administrator (forms administrator too) may do both
	admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/restore", f.ID), map[string]any{"lang": "en", "rev": 1}, nil, 200)
	if got := intro(f.ID); !strings.Contains(got, "first") {
		t.Fatalf("form after the restore by the administrator: %q", got)
	}
}
