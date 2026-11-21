package content

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// TestBlockOptionsText: the shared cases of the text form, read and written
// (the browser runs the same file).
func TestBlockOptionsText(t *testing.T) {
	raw, err := os.ReadFile("testdata/blockoptions_text_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases struct {
		OK []struct {
			Name      string         `json:"name"`
			Text      string         `json:"text"`
			Options   map[string]any `json:"options"`
			Unchecked bool           `json:"unchecked"` // only the text form, not a valid option
		} `json:"ok"`
		Bad []struct {
			Name string `json:"name"`
			Text string `json:"text"`
		} `json:"bad"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases.OK {
		got, err := DecodeBlockOptions(c.Text)
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		if !reflect.DeepEqual(got, c.Options) && !(len(got) == 0 && len(c.Options) == 0) {
			t.Errorf("%s: read %#v, want %#v", c.Name, got, c.Options)
		}
		if enc := EncodeBlockOptions(c.Options); enc != c.Text {
			t.Errorf("%s: written %q, want %q", c.Name, enc, c.Text)
		}
		if err := CheckBlockOptions(got); err != nil && !c.Unchecked {
			t.Errorf("%s: not valid: %v", c.Name, err)
		}
	}
	for _, c := range cases.Bad {
		if o, err := DecodeBlockOptions(c.Text); err == nil {
			t.Errorf("%s: %q read as %v", c.Name, c.Text, o)
		}
	}
}
