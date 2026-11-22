package content

import (
	"strings"
	"testing"
)

// TestMacroUsesHidden: blocks inside blocks that show their content only to
// some readers, or inside unknown blocks, keep no data; the numbering stays
// that of the rendering (inner blocks first).
func TestMacroUsesHidden(t *testing.T) {
	src := `<div data-macro="decision" data-params='{"title":"a"}'></div>` +
		`<div data-macro="only_for"><div data-macro="decision" data-params='{"title":"b"}'></div></div>` +
		`<div data-macro="gone"><span data-macro="decision" data-params='{"title":"c"}'></span></div>` +
		`<div data-macro="panel"><div data-macro="decision" data-params='{"title":"d"}'>Text <b>here</b></div></div>`
	open := map[string]bool{"decision": true, "panel": true}
	var got []string
	for _, u := range macroUses(src, func(n string) bool { return !open[n] }) {
		got = append(got, u.Name+":"+u.Params["title"]+":"+string(rune('0'+u.Index)))
	}
	if strings.Join(got, "|") != "decision:a:1|only_for::3|gone::5|decision:d:6|panel::7" {
		t.Fatalf("uses: %v", got)
	}
	for _, u := range macroUses(src, func(string) bool { return false }) {
		if u.Params["title"] == "d" && u.Text != "Text here" {
			t.Fatalf("text: %q", u.Text)
		}
	}
}

// TestCheckDataKind: invalid schemas are refused when registered.
func TestCheckDataKind(t *testing.T) {
	for name, k := range map[string]DataKind{
		"other plugin":  {Kind: "other.x", Fields: []DataField{{Key: "a", Type: DataText}}},
		"bad name":      {Kind: "core.macros.X", Fields: []DataField{{Key: "a", Type: DataText}}},
		"repeated key":  {Kind: "core.macros.x", Fields: []DataField{{Key: "a", Type: DataText}, {Key: "a", Type: DataText}}},
		"unknown type":  {Kind: "core.macros.x", Fields: []DataField{{Key: "a", Type: "blob"}}},
		"no options":    {Kind: "core.macros.x", Fields: []DataField{{Key: "a", Type: DataChoice}}},
		"wrong column":  {Kind: "core.macros.x", Fields: []DataField{{Key: "a", Type: DataText, Column: ColDay}}},
		"column twice":  {Kind: "core.macros.x", Fields: []DataField{{Key: "a", Type: DataDay, Column: ColDay}, {Key: "b", Type: DataDay, Column: ColDay}}},
		"bad field key": {Kind: "core.macros.x", Fields: []DataField{{Key: "A-b", Type: DataText}}},
	} {
		if err := checkDataKind("core.macros", &k); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	k := DataKind{Kind: "core.macros.x", Fields: []DataField{{Key: "n", Type: DataNumber, Column: ColNum}, {Key: "tags", Type: DataList, Max: 2}, {Key: "who", Type: DataPerson, Column: ColPerson}}}
	if err := checkDataKind("core.macros", &k); err != nil || k.Version != 1 || k.MaxPerElement != 500 || k.Plugin != "core.macros" {
		t.Fatalf("valid kind: %v %+v", err, k)
	}
	r, err := checkData(k, map[string]any{"n": "2,5", "tags": "a, b", "who": float64(7)})
	if err != nil || *r.num != 2.5 || *r.person != 7 || string(r.data) != `{"n":2.5,"tags":["a","b"],"who":7}` {
		t.Fatalf("data: %v %s", err, r.data)
	}
	for _, bad := range []map[string]any{{"tags": "a,b,c"}, {"who": 1.5}, {"who": "-1"}, {"n": "NaN"}, {"n": "1e999"}, {"tags": []any{1}}, {"zzz": "1"}} {
		if _, err := checkData(k, bad); err == nil {
			t.Errorf("accepted: %v", bad)
		}
	}
}
