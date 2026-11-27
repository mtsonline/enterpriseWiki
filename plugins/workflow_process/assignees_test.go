package workflow_process

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

// TestAssignees: a step has a list of assignees (people, groups, roles and
// the starter together); the list is checked and cleaned, definitions with
// the older single "assignee" are read as a list of one, and a step is
// still read strictly (unknown fields are refused).
func TestAssignees(t *testing.T) {
	step := func(assignees string) string {
		return `{"steps":[{"id":"a","name":"A",` + assignees + `,"actions":[{"id":"go","label":"Go","next":"e"}]},{"id":"e","name":"E","type":"end"}]}`
	}
	norm := func(def string) (*Definition, error) {
		out, err := Type{}.Normalize(json.RawMessage(def))
		if err != nil {
			return nil, err
		}
		return Decode(out)
	}
	// several kinds together; the same entry twice (any case) counts once; the starter has no value
	d, err := norm(step(`"assignees":[{"type":"user","value":"ann"},{"type":"role","value":"Managers"},{"type":"group","value":"hr"},{"type":"group","value":"HR"},{"type":"starter","value":"x"}]`))
	if err != nil {
		t.Fatal(err)
	}
	got := d.Steps[0].Assignees
	if len(got) != 4 || got[0] != (Assignee{"user", "ann"}) || got[1] != (Assignee{"role", "Managers"}) || got[2] != (Assignee{"group", "hr"}) || got[3] != (Assignee{"starter", ""}) {
		t.Fatalf("assignees: %+v", got)
	}
	// the older form: one assignee
	d, err = norm(step(`"assignee":{"type":"group","value":"hr"}`))
	if err != nil || len(d.Steps[0].Assignees) != 1 || d.Steps[0].Assignees[0] != (Assignee{"group", "hr"}) {
		t.Fatalf("older form: %+v %v", d, err)
	}
	many := make([]string, MaxAssignees+1)
	for i := range many {
		many[i] = fmt.Sprintf(`{"type":"user","value":"u%d"}`, i)
	}
	for name, def := range map[string]string{
		"none":          step(`"assignees":[]`),
		"missing":       step(`"description":"x"`),
		"unknown type":  step(`"assignees":[{"type":"nobody","value":"x"}]`),
		"empty value":   step(`"assignees":[{"type":"group","value":" "}]`),
		"long value":    step(`"assignees":[{"type":"group","value":"` + strings.Repeat("g", 201) + `"}]`),
		"too many":      step(`"assignees":[` + strings.Join(many, ",") + `]`),
		"unknown field": step(`"assignees":[{"type":"starter"}],"assigne":{"type":"starter"}`),
	} {
		if _, err := norm(def); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// the overview of the process names every assignee
	d, err = norm(step(`"assignees":[{"type":"group","value":"hr"},{"type":"role","value":"Managers"}]`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(d)
	html, err := Type{}.Render(t.Context(), content.RenderContext{Lang: "en"}, raw)
	if err != nil || !strings.Contains(html, "hr") || !strings.Contains(html, "Managers") {
		t.Fatalf("overview: %v %s", err, html)
	}
}
