package content_forms

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func form(t *testing.T, js string) *Form {
	t.Helper()
	f, err := Decode([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestValidate(t *testing.T) {
	f := form(t, `{"fields":[{"id":"name","type":"text","label":"Name","required":true},
		{"id":"kind","type":"radio","label":"Kind","options":[{"label":"A"},{"value":"b","label":"B"}]},
		{"id":"why","type":"textarea","label":"Why","show_if":{"field":"kind","op":"eq","value":"b"}},
		{"id":"up","type":"file","label":"Files"}]}`)
	if f.Layout != "grid" || f.Fields[1].Options[0].Value != "A" || f.Fields[0].MaxLen != 500 || f.Fields[3].MaxFiles != 1 || f.Fields[3].MaxSizeMB != 10 {
		t.Fatalf("normalized: %+v", f)
	}
	for _, bad := range []string{
		`{"fields":[{"id":"1x","type":"text","label":"X"}]}`,
		`{"fields":[{"id":"a","type":"text","label":"A"},{"id":"a","type":"text","label":"B"}]}`,
		`{"fields":[{"id":"a","type":"script","label":"A"}]}`,
		`{"fields":[{"id":"a","type":"text","label":""}]}`,
		`{"fields":[{"id":"a","type":"select","label":"A","options":[]}]}`,
		`{"fields":[{"id":"a","type":"text","label":"A","pattern":"(("}]}`,
		`{"fields":[{"id":"a","type":"text","label":"A","show_if":{"field":"b","op":"eq"}},{"id":"b","type":"text","label":"B"}]}`,
		`{"fields":[{"id":"a","type":"file","label":"A","accept":[".exe"]}]}`,
		`{"fields":[{"id":"a","type":"file","label":"A","accept":[".svg"]}]}`,
		`{"fields":[{"id":"a","type":"text","label":"A","autocomplete":"javascript"}]}`,
		`{"fields":[],"targets":{"email":[{"to":"not an address"}]}}`,
		`{"fields":[],"targets":{"webhook":{"url":"ftp://x"}}}`,
		`{"fields":[{"id":"a","type":"text","label":"A"}],"targets":{"email":[{"to":"field:a"}]}}`,
	} {
		f, err := Decode([]byte(bad))
		if err == nil {
			err = f.Validate()
		}
		if err == nil {
			t.Errorf("accepted: %s", bad)
		}
	}
}

func TestCheckValues(t *testing.T) {
	f := form(t, `{"fields":[{"id":"name","type":"text","label":"Name","required":true,"max_length":10},
		{"id":"mail","type":"email","label":"Mail"},
		{"id":"n","type":"number","label":"N","min":1,"max":5},
		{"id":"kind","type":"radio","label":"Kind","options":[{"value":"a","label":"A"},{"value":"b","label":"B"}]},
		{"id":"why","type":"textarea","label":"Why","required":true,"show_if":{"field":"kind","op":"eq","value":"b"}},
		{"id":"tags","type":"checkboxes","label":"Tags","options":[{"value":"x","label":"X"},{"value":"y","label":"Y"}]},
		{"id":"ok","type":"consent","label":"I agree","required":true},
		{"id":"zip","type":"text","label":"ZIP","pattern":"[0-9]{4}"},
		{"id":"up","type":"file","label":"Up","required":true,"max_files":2}]}`)
	in := func(js string) map[string]json.RawMessage {
		m := map[string]json.RawMessage{}
		_ = json.Unmarshal([]byte(js), &m)
		return m
	}
	vals, errs := f.CheckValues(in(`{"name":"  Ann   Lee ","mail":"a@b.at","n":"3","kind":"a","why":"","tags":["y","x","y"],"ok":true,"zip":"1010"}`), map[string]int{"up": 1})
	if len(errs) != 0 {
		t.Fatalf("errors: %+v", errs)
	}
	if vals["name"] != "Ann Lee" || vals["n"] != 3.0 || len(vals["tags"].([]string)) != 2 {
		t.Fatalf("values: %+v", vals)
	}
	if _, ok := vals["why"]; ok {
		t.Fatal("hidden field kept")
	}
	_, errs = f.CheckValues(in(`{"name":"much too long name","mail":"a@","n":9,"kind":"b","tags":["z"],"ok":false,"zip":"12"}`), map[string]int{"up": 3})
	codes := map[string]string{}
	for _, e := range errs {
		codes[e.Field] = e.Code
	}
	want := map[string]string{"name": "too_long", "mail": "email", "n": "max", "why": "required", "tags": "option", "ok": "consent", "zip": "pattern", "up": "files"}
	for k, v := range want {
		if codes[k] != v {
			t.Errorf("%s: %q, want %q (%+v)", k, codes[k], v, errs)
		}
	}
	if f.Text("kind", "b") != "B" || f.Text("tags", []string{"x", "y"}) != "X, Y" {
		t.Fatal("text of options")
	}
}

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func zipWith(t *testing.T, names ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, n := range names {
		w, _ := z.Create(n)
		_, _ = w.Write([]byte("x"))
	}
	_ = z.Close()
	return b.Bytes()
}

func TestCheckFile(t *testing.T) {
	p := &Plugin{}
	x := &Field{ID: "up", Type: "file", Accept: []string{"pdf", "office", "image", ".csv"}}
	cases := []struct {
		name string
		data []byte
		code string
	}{
		{"a.pdf", []byte("%PDF-1.7\n1 0 obj << /Type /Catalog >>"), ""},
		{"a.pdf", []byte("%PDF-1.7\n1 0 obj << /OpenAction << /S /JavaScript /JS (app.alert(1)) >> >>"), "pdf_active"},
		{"a.pdf", []byte("\x89PNG\r\n\x1a\nxxxx"), "file_content"},
		{"a.png", []byte("\x89PNG\r\n\x1a\nxxxx"), ""},
		{"a.exe", []byte("MZ"), "file_type"},
		{"a.docx", zipWith(t, "[Content_Types].xml", "word/document.xml"), ""},
		{"a.docx", zipWith(t, "[Content_Types].xml", "word/vbaProject.bin"), "macros"},
		{"a.xls", append([]byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"), []byte("_\x00V\x00B\x00A\x00_\x00P\x00R\x00O\x00J\x00E\x00C\x00T\x00")...), "macros"},
		{"a.csv", []byte("a;b\n1;2\n"), ""},
		{"a.csv", []byte("\x00\x01\x02binary"), "file_content"},
	}
	for _, c := range cases {
		err := p.checkFile(t.Context(), x, c.name, writeFile(t, c.name, c.data))
		got := ""
		if r, ok := err.(*ErrRejected); ok {
			got = r.Code
		} else if err != nil && !strings.Contains(err.Error(), "settings") {
			got = err.Error()
		}
		if got != c.code {
			t.Errorf("%s %q: %q, want %q", c.name, c.data[:min(12, len(c.data))], got, c.code)
		}
	}
	x.AllowMacros, x.AllowActive = true, true
	if err := p.checkFile(t.Context(), x, "a.docx", writeFile(t, "a.docx", zipWith(t, "word/vbaProject.bin"))); err != nil && err.(*ErrRejected) != nil && err.(*ErrRejected).Code == "macros" {
		t.Error("macros refused although allowed")
	}
}
