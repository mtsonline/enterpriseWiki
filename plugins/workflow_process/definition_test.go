package workflow_process

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := `{"description":"d","fields":[{"id":"n","label":"Name","type":"text","options":["x"]}],"steps":[
		{"id":"a","name":"A","assignee":{"type":"group","value":"hr"},"actions":[{"id":"go","label":"Go","next":"b","note":"weird"}]},
		{"id":"b","name":"B","assignee":{"type":"group","value":"x"},"actions":[],"outcome":"?"}]}`
	out, err := Type{}.Normalize(json.RawMessage(ok))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, `"options"`) || !strings.Contains(s, `"outcome":"neutral"`) || strings.Contains(s, `"weird"`) || strings.Contains(s, `"value":"x"`) {
		t.Fatalf("not normalized: %s", s)
	}
	for _, bad := range []string{
		`{"steps":[]}`,
		`{"steps":[{"id":"A","name":"x","actions":[]}]}`,
		`{"steps":[{"id":"a","name":"x","assignee":{"type":"nobody"},"actions":[{"id":"x","label":"X","next":"a"}]}]}`,
		`{"steps":[{"id":"a","name":"x","assignee":{"type":"group","value":""},"actions":[{"id":"x","label":"X","next":"a"}]}]}`,
		`{"steps":[{"id":"a","name":"x","actions":[]},{"id":"a","name":"y","actions":[]}]}`,
		`{"fields":[{"id":"s","label":"S","type":"select"}],"steps":[{"id":"a","name":"x","actions":[]}]}`,
		`{"unknown":1,"steps":[{"id":"a","name":"x","actions":[]}]}`,
	} {
		if _, err := (Type{}).Normalize(json.RawMessage(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestValidateData(t *testing.T) {
	fields := []Field{{ID: "d", Label: "D", Type: "date", Required: true}, {ID: "n", Label: "N", Type: "number"}, {ID: "f", Label: "F", Type: "file"}}
	if _, err := validateData(fields, map[string]any{"d": "2026-13-01"}); err == nil {
		t.Fatal("bad date")
	}
	out, err := validateData(fields, map[string]any{"d": "2026-11-01", "n": 4.5, "f": map[string]any{"hash": strings.Repeat("a", 64), "name": "cv.pdf", "evil": 1}})
	if err != nil || out["n"] != "4.5" || out["f"].(map[string]any)["evil"] != nil {
		t.Fatalf("%v %v", out, err)
	}
	if _, err := validateData(fields, map[string]any{"d": "2026-11-01", "f": map[string]any{"hash": "../x"}}); err == nil {
		t.Fatal("bad file hash")
	}
}

func TestStepTypesAndConditions(t *testing.T) {
	def := `{"fields":[{"id":"amount","label":"A","type":"number"},{"id":"start","label":"S","type":"date"}],"steps":[
		{"id":"c","name":"C","type":"condition","match":"any","rules":[{"field":"amount","op":"gt","value":"100"},{"field":"start","op":"lt","value":"2026-01-01"}],"next":"e","next_else":"e"},
		{"id":"d","name":"D","type":"delay","delay":{"until":"start","days":1},"next":"e","assignee":{"type":"group","value":"x"}},
		{"id":"m","name":"M","type":"email","email":{"to":"{{field.x}}","subject":"s","body":"b"},"next":"e"},
		{"id":"e","name":"E","type":"end","outcome":"success"}]}`
	out, err := Type{}.Normalize(json.RawMessage(def))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := Decode(out)
	if d.Steps[1].Assignee.Type != "" {
		t.Fatal("assignee kept on a delay step")
	}
	x := &Instance{Data: map[string]any{"amount": "99,5", "start": "2025-12-31"}, Definition: *d}
	if !evaluate(x, &d.Steps[0]) {
		t.Fatal("any: date rule should match")
	}
	d.Steps[0].Match = "all"
	if evaluate(x, &d.Steps[0]) {
		t.Fatal("all: amount rule should fail")
	}
	x.Data["amount"] = 150.0
	if !evaluate(x, &d.Steps[0]) {
		t.Fatal("all should match")
	}
	if got := render("Hi {{ field.name }} {{unknown}}!", map[string]string{"field.name": "Ann"}); got != "Hi Ann !" {
		t.Fatal(got)
	}
	for _, bad := range []string{
		`{"steps":[{"id":"a","name":"A","type":"delay","delay":{},"next":"a"}]}`,
		`{"steps":[{"id":"a","name":"A","type":"webhook","webhook":{"url":"ftp://x"},"next":"a"}]}`,
		`{"steps":[{"id":"a","name":"A","type":"email","email":{"to":"","subject":"s","body":"b"},"next":"a"}]}`,
		`{"steps":[{"id":"a","name":"A","type":"approval","assignee":{"type":"group","value":"g"},"next":"a"}]}`,
		`{"steps":[{"id":"a","name":"A","type":"magic"}]}`,
	} {
		if _, err := (Type{}).Normalize(json.RawMessage(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
