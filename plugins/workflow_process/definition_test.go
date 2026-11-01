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
