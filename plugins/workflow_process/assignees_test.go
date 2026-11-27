package workflow_process

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

// TestAssignees: a step has a list of assignees (people, groups, roles and
// the starter together); the list is checked and cleaned, its length is an
// expert setting, and a step is read strictly (unknown fields, also the
// single "assignee" of earlier versions, are refused).
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
	many := make([]string, DefaultAssignees+1)
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
		"single field":  step(`"assignee":{"type":"group","value":"hr"}`),
	} {
		if _, err := norm(def); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// the expert setting: a smaller number applies
	three := Type{maxAssignees: func() int64 { return 3 }}
	if _, err := three.Normalize(json.RawMessage(step(`"assignees":[` + strings.Join(many[:3], ",") + `]`))); err != nil {
		t.Errorf("3 of 3: %v", err)
	}
	if _, err := three.Normalize(json.RawMessage(step(`"assignees":[` + strings.Join(many[:4], ",") + `]`))); err == nil {
		t.Error("4 of 3: accepted")
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
