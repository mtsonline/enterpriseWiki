package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestWorkflowStartLabel: a workflow names its start button itself, per
// language version ("New holiday request"); the page gets the label of the
// version it shows; a label longer than a button can hold is refused.
func TestWorkflowStartLabel(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var w cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Requests"}}, &w, 201)
	def := func(label string) map[string]any {
		return map[string]any{"start_label": label, "fields": []map[string]any{{"id": "from", "label": "From", "type": "date"}},
			"steps": []map[string]any{{"id": "done", "name": "Done", "type": "end", "outcome": "success"}}}
	}
	if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Too long",
		"body": def(strings.Repeat("x", 61))}, nil); code != 400 {
		t.Fatalf("a label of 61 characters: %d", code)
	}
	var wf elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "lang": "en", "title": "Holiday",
		"body": def("  New holiday request ")}, &wf, 201)
	zero := 0
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", wf.ID), map[string]any{"lang": "de", "title": "Urlaub", "base_rev": zero,
		"body": def("Neuer Urlaubsantrag")}, nil, 200)
	for lang, want := range map[string]string{"en": "New holiday request", "de": "Neuer Urlaubsantrag"} {
		var list struct {
			StartLabel string `json:"start_label"`
		}
		admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances?lang=%s", wf.ID, lang), nil, &list, 200)
		if list.StartLabel != want {
			t.Fatalf("%s: %q", lang, list.StartLabel)
		}
	}
	// without a label the page uses its own text ("Start new case")
	var plain elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": w.ID, "type": "workflow.process", "title": "Plain", "body": def("")}, &plain, 201)
	var list map[string]any
	admin.must("GET", fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", plain.ID), nil, &list, 200)
	if v, _ := list["start_label"].(string); v != "" {
		t.Fatalf("no label: %q", v)
	}
}
