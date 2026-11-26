package server_test

import (
	"fmt"
	"testing"
)

// TestWorkflowDateOrder: a date field with "not before" (after) is never
// before its other field – checked when a case starts and when the form of
// a step changes one of the two; the rule only ties two date fields.
func TestWorkflowDateOrder(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var w cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Leave"}}, &w, 201)
	fields := func(after string) []map[string]any {
		return []map[string]any{{"id": "from", "label": "First day", "type": "date", "required": true},
			{"id": "to", "label": "Last day", "type": "date", "after": after}, {"id": "note", "label": "Note", "type": "text"}}
	}
	steps := []map[string]any{
		{"id": "fix", "name": "Fix the dates", "assignee": map[string]any{"type": "starter"}, "form": []map[string]any{{"field": "to", "mode": "edit"}},
			"actions": []map[string]any{{"id": "ok", "label": "OK", "next": "done"}}},
		{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}
	for _, bad := range []string{"note", "to", "nope"} {
		if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Bad",
			"body": map[string]any{"fields": fields(bad), "steps": steps}}, nil); code != 400 {
			t.Fatalf("not before %q: %d", bad, code)
		}
	}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Leave",
		"body": map[string]any{"fields": fields("from"), "steps": steps}}, &wf, 201)
	start := fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", wf.ID)
	if code := admin.do("POST", start, map[string]any{"data": map[string]any{"from": "2026-12-10", "to": "2026-12-09"}}, nil); code != 400 {
		t.Fatalf("last day before the first: %d", code)
	}
	var c wfCase
	admin.must("POST", start, map[string]any{"data": map[string]any{"from": "2026-12-10", "to": "2026-12-10"}}, &c, 201)
	act := fmt.Sprintf("/api/v1/x/workflow.process/instances/%d/actions", c.ID)
	// the form of the step changes "to": checked against "from" of the case
	if code := admin.do("POST", act, map[string]any{"action": "ok", "data": map[string]any{"to": "2026-12-01"}}, nil); code != 400 {
		t.Fatalf("step form with a date before: %d", code)
	}
	var x wfCase
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/instances/%d", c.ID), nil, &x, 200)
	if x.Data["to"] != "2026-12-10" || x.Status != "open" {
		t.Fatalf("after the refused step: %+v", x)
	}
	admin.must("POST", act, map[string]any{"action": "ok", "data": map[string]any{"to": "2026-12-12"}}, nil, 200)
}
