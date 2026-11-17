package perm

import (
	"encoding/json"
	"testing"
)

// A preset without named actions is an empty list in JSON, never null: the
// permission dialog iterates over it (regression: "add permission" did not
// open when no plugin declared an action for a preset).
func TestPresetIsAList(t *testing.T) {
	for _, name := range []string{Reader, Editor, Manager, "unknown"} {
		b, err := json.Marshal(Preset(name))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) == "null" {
			t.Errorf("Preset(%q) is null in JSON", name)
		}
	}
}
