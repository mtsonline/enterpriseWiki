package content

import "testing"

// TestWithOptionLabels: options without labels get the common ones, own
// labels win, and the parameters of the macro are not changed.
func TestWithOptionLabels(t *testing.T) {
	own := map[string]map[string]string{"primary": {"en": "Main color", "de": "Hauptfarbe"}}
	in := []MacroParam{
		{Name: "color", Type: "select", Options: []string{"primary", "start", "2", "weird"}, OptionLabels: own},
		{Name: "title", Type: "text"},
	}
	out := withOptionLabels(in)
	if got := out[0].OptionLabels["primary"]["de"]; got != "Hauptfarbe" {
		t.Errorf("own label: %q", got)
	}
	if got := out[0].OptionLabels["start"]["de"]; got != "Am Anfang (links)" {
		t.Errorf("common label: %q", got)
	}
	if _, ok := out[0].OptionLabels["2"]; ok {
		t.Error("a number needs no label")
	}
	if _, ok := out[0].OptionLabels["weird"]; ok {
		t.Error("an unknown option gets no invented label")
	}
	if len(in[0].OptionLabels) != 1 || in[0].OptionLabels["start"] != nil {
		t.Error("the parameters of the macro were changed")
	}
	if out[1].OptionLabels != nil {
		t.Error("a text parameter has no options")
	}
	// every common label has both languages
	for o, l := range commonOptionLabels {
		if l["en"] == "" || l["de"] == "" {
			t.Errorf("common label of %q lacks a language: %v", o, l)
		}
	}
}
