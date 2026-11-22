package server_test

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestGuardOnCreate: what an element type checks for a change of its body
// is also checked when an element is created with that body, duplicated or
// copied with its category.
func TestGuardOnCreate(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var top, c, mine, other, role, u obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Top"}}, &top, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": top.ID, "titles": map[string]string{"en": "Forms"}}, &c, 201)
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Mine"}, &mine, 201)
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Other"}, &other, 201)
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "All lists", "permissions": []string{"core.groups.list_all", "core.users.list_all"}}, &role, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "fay", "password": "Pw-fay-Passw0rd!x", "display_name": "Fay", "groups": []int64{mine.ID}}, &u, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", top.ID), map[string]any{"subject_type": "user", "subject_id": u.ID, "permissions": 1 | 2 | 4 | 16, "actions": []string{"content.forms.create"}, "inherit": true}, nil, 201)
	fay := e.client()
	fay.login("fay", "Pw-fay-Passw0rd!x")

	// forms: sending submissions outside needs its permission, also for a new form
	form := func(targets string) map[string]any {
		return map[string]any{"category_id": c.ID, "type": "content.forms", "title": "Contact", "body": map[string]any{
			"intro": "", "fields": []map[string]any{{"id": "message", "type": "textarea", "label": "Message"}}, "targets": json.RawMessage(targets)}}
	}
	fay.must("POST", "/api/v1/elements", form(`{"store":true}`), nil, 201)
	for name, targets := range map[string]string{
		"e-mail outside": `{"store":true,"email":[{"to":"someone@example.com"}]}`,
		"webhook":        `{"store":true,"webhook":{"url":"https://hooks.example.com/x"}}`,
	} {
		if code := fay.do("POST", "/api/v1/elements", form(targets), nil); code != 403 {
			t.Errorf("a form with %s created without the permission to send outside: %d", name, code)
		}
	}
	// a copy of a form of someone else with a webhook: refused
	var hook obj
	admin.must("POST", "/api/v1/elements", form(`{"store":true,"webhook":{"url":"https://hooks.example.com/x"}}`), &hook, 201)
	if code := fay.do("POST", fmt.Sprintf("/api/v1/elements/%d/duplicate", hook.ID), map[string]any{"category_id": c.ID}, nil); code != 403 {
		t.Errorf("a form with a webhook duplicated without the permission to send outside: %d", code)
	}
	// a copy of the category: without what the person may not create
	var cp obj
	fay.must("POST", fmt.Sprintf("/api/v1/categories/%d/duplicate", c.ID), map[string]any{"parent_id": top.ID}, &cp, 201)
	var hooks int
	var forms int
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*), count(*) FILTER (WHERE r.body->'targets' ? 'webhook') FROM elements el JOIN element_revisions r ON r.element_id = el.id
		WHERE el.category_id=$1 AND el.type='content.forms'`, cp.ID).Scan(&forms, &hooks); err != nil || forms != 1 || hooks != 0 {
		t.Errorf("forms in the copy of the category: %d, with a webhook %d (%v)", forms, hooks, err)
	}

	// team: the members of a group are shown only by who may list them
	team := func(groups ...int64) map[string]any {
		return map[string]any{"category_id": c.ID, "type": "content.team", "title": "Team", "body": map[string]any{"groups": groups, "fields": []string{"email"}}}
	}
	// signed-in people may list their own groups and their members (default)
	fay.must("POST", "/api/v1/elements", team(mine.ID), nil, 201)
	if code := fay.do("POST", "/api/v1/elements", team(mine.ID, other.ID), nil); code != 403 {
		t.Errorf("a team of a group of others with only the own lists: %d", code)
	}
	var tm obj
	admin.must("POST", "/api/v1/elements", team(other.ID), &tm, 201)
	// groups that are there stay for every editor
	fay.must("PUT", fmt.Sprintf("/api/v1/elements/%d", tm.ID), map[string]any{"lang": "en", "title": "Team renamed", "body": map[string]any{"groups": []int64{other.ID}, "fields": []string{"email"}}}, nil, 200)
	if code := fay.do("POST", fmt.Sprintf("/api/v1/elements/%d/duplicate", tm.ID), map[string]any{"category_id": c.ID}, nil); code != 403 {
		t.Errorf("a team of a group of others duplicated: %d", code)
	}
	// with the permissions to list every group and person
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), map[string]any{"roles": []int64{role.ID}}, nil, 200)
	fay = e.client()
	fay.login("fay", "Pw-fay-Passw0rd!x")
	fay.must("POST", "/api/v1/elements", team(mine.ID, other.ID), nil, 201)
}
