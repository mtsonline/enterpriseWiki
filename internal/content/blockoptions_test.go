package content

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestBlockOptionCases: the shared cases (testdata/blockoptions_cases.json);
// the unit test of the browser checks the same file, so the form and the
// server always decide the same way.
func TestBlockOptionCases(t *testing.T) {
	raw, err := os.ReadFile("testdata/blockoptions_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name    string         `json:"name"`
		OK      bool           `json:"ok"`
		Options map[string]any `json:"options"`
		// Problem: the reason it is refused (a word of the message)
		Problem string `json:"problem"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 20 {
		t.Fatalf("only %d cases", len(cases))
	}
	for _, c := range cases {
		err := CheckBlockOptions(c.Options)
		if c.OK && err != nil {
			t.Errorf("%s: refused: %v", c.Name, err)
		}
		if !c.OK && err == nil {
			t.Errorf("%s: accepted", c.Name)
		}
		words := map[string]string{"text_color": "needs its own text color", "contrast": "has a contrast of", "overlay": "needs an overlay", "columns": "columns:", "until": "until must be after", "between": "lies within the brightness"}
		if c.Problem != "" && (err == nil || !strings.Contains(err.Error(), words[c.Problem])) {
			t.Errorf("%s: want the reason %q, got %v", c.Name, c.Problem, err)
		}
	}
}

func TestBlockOptionsSchema(t *testing.T) {
	if len(blockOptions.byID) < 60 {
		t.Fatalf("options in the schema: %d", len(blockOptions.byID))
	}
	if MaxColumns("mobile") != 1 || MaxColumns("tablet") != 3 || MaxColumns("desktop") != 5 {
		t.Fatalf("columns: %d %d %d", MaxColumns("mobile"), MaxColumns("tablet"), MaxColumns("desktop"))
	}
	big := map[string]any{"langs": []any{}}
	for i := 0; i < 600; i++ {
		big["langs"] = append(big["langs"].([]any), "de")
	}
	if err := CheckBlockOptions(big); err == nil {
		t.Fatal("too large options accepted")
	}
}
