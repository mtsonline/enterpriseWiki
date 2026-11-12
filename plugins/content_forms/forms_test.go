package content_forms

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
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

func TestFormula(t *testing.T) {
	vars := map[string]float64{"a": 2, "b": 3, "price": 9.99}
	for src, want := range map[string]float64{
		"a + b * 2": 8, "(a + b) * 2": 10, "-a + 10 / 4": 0.5, "round(price * 3, 1)": 30, "max(a, b, 1)": 3, "min(a, b)": 2,
		"abs(a - b)": 1, "sum(a, b, 5)": 10, "a / 0": 0, "unknown_field": 0,
	} {
		n, _, err := parseFormula(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if got := n.eval(vars); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
	for _, bad := range []string{"a +", "(a", "exec(a)", "a $ b", "1..2", "abs(a, b)", strings.Repeat("(", 60) + "1" + strings.Repeat(")", 60)} {
		if _, _, err := parseFormula(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	f := form(t, `{"fields":[{"id":"n","type":"number","label":"N"},{"id":"x","type":"checkbox","label":"X"},{"id":"total","type":"calc","label":"Total","formula":"n * 2 + x","decimals":1}]}`)
	vals, errs := f.CheckValues(map[string]json.RawMessage{"n": json.RawMessage(`"2.25"`), "x": json.RawMessage(`true`), "total": json.RawMessage(`"999"`)}, nil)
	if len(errs) != 0 || vals["total"] != 5.5 {
		t.Fatalf("computed: %+v %+v", vals, errs)
	}
	if _, err := Decode([]byte(`{"fields":[{"id":"t","type":"calc","label":"T","formula":"later"},{"id":"later","type":"number","label":"L"}]}`)); err == nil {
		f, _ := Decode([]byte(`{"fields":[{"id":"t","type":"calc","label":"T","formula":"later"},{"id":"later","type":"number","label":"L"}]}`))
		if f.Validate() == nil {
			t.Error("formula with a field below accepted")
		}
	}
}

func TestProofOfWork(t *testing.T) {
	tok := "123.abc"
	n := 0
	for ; !powValid(tok, strconv.Itoa(n), 12); n++ {
	}
	if !powValid(tok, strconv.Itoa(n), 12) || powValid(tok, "", 12) || powValid("other", strconv.Itoa(n), 12) && powValid("other", strconv.Itoa(n+1), 12) {
		t.Fatal("proof of work")
	}
}

// fake ICAP and clamd servers: clean, infected, broken
func TestVirusScanners(t *testing.T) {
	serve := func(answer string) string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				go func() {
					defer c.Close()
					buf := make([]byte, 64<<10)
					got := []byte{}
					for !bytes.Contains(got, []byte("0\r\n\r\n")) && !bytes.HasSuffix(got, []byte{0, 0, 0, 0}) {
						n, err := c.Read(buf)
						if err != nil {
							return
						}
						got = append(got, buf[:n]...)
					}
					_, _ = c.Write([]byte(answer))
				}()
			}
		}()
		return l.Addr().String()
	}
	x := &Field{ID: "up", Type: "file", Accept: []string{"pdf"}}
	path := writeFile(t, "a.pdf", []byte("%PDF-1.4 hello"))
	for _, c := range []struct {
		setting, value, answer, code string
	}{
		{"icap", "icap://%s/avscan", "ICAP/1.0 204 No Content\r\n\r\n", ""},
		{"icap", "icap://%s/avscan", "ICAP/1.0 200 OK\r\nX-Infection-Found: Type=0; Threat=EICAR\r\n\r\n", "virus"},
		{"icap", "icap://%s/avscan", "ICAP/1.0 500 Server Error\r\n\r\n", "scan_unavailable"},
		{"clamd", "tcp://%s", "stream: OK\x00", ""},
		{"clamd", "tcp://%s", "stream: Eicar-Signature FOUND\x00", "virus"},
		{"clamd", "tcp://127.0.0.1:1", "", "scan_unavailable"},
	} {
		v := c.value
		if strings.Contains(v, "%s") {
			v = fmt.Sprintf(v, serve(c.answer))
		}
		p := &Plugin{host: testHost(map[string]any{c.setting: v})}
		err := p.checkFile(t.Context(), x, "a.pdf", path)
		got := ""
		if r, ok := err.(*ErrRejected); ok {
			got = r.Code
		} else if err != nil {
			got = err.Error()
		}
		if got != c.code {
			t.Errorf("%s %q: %q, want %q", c.setting, c.answer, got, c.code)
		}
	}
}

// testHost is a host with plugin settings (written to a temporary config).
func testHost(settings map[string]any) *sdk.Host {
	dir, _ := os.MkdirTemp("", "forms-test")
	path := filepath.Join(dir, "config.yaml")
	c := config.Defaults()
	c.Settings = map[string]map[string]any{"content.forms": settings}
	_ = config.WriteFile(path, c)
	st, err := config.Open(path)
	if err != nil {
		panic(err)
	}
	return &sdk.Host{Manifest: sdk.Manifest{ID: "content.forms"}, Config: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestRepeat(t *testing.T) {
	f := form(t, `{"fields":[{"id":"people","type":"repeat","label":"People","required":true,"max_rows":2,"fields":[
		{"id":"name","type":"text","label":"Name","required":true},{"id":"age","type":"number","label":"Age","max":120},
		{"id":"diet","type":"select","label":"Diet","options":[{"value":"veg","label":"Vegetarian"}]}]}]}`)
	raw := func(js string) map[string]json.RawMessage {
		m := map[string]json.RawMessage{}
		_ = json.Unmarshal([]byte(js), &m)
		return m
	}
	vals, errs := f.CheckValues(raw(`{"people":[{"name":"Ann","age":"30","diet":"veg"},{"name":"Bob","age":"","diet":""}]}`), nil)
	if len(errs) != 0 || len(vals["people"].([]map[string]any)) != 2 {
		t.Fatalf("rows: %+v %+v", vals, errs)
	}
	if got := f.Text("people", vals["people"]); got != "Name: Ann, Age: 30, Diet: Vegetarian | Name: Bob" {
		t.Fatalf("text: %q", got)
	}
	_, errs = f.CheckValues(raw(`{"people":[{"name":"","age":"200"},{"name":"x","diet":"meat"}]}`), nil)
	want := map[string]string{"people.0.name": "required", "people.0.age": "max", "people.1.diet": "option"}
	if len(errs) != 3 {
		t.Fatalf("errors: %+v", errs)
	}
	for _, e := range errs {
		if want[e.Field] != e.Code {
			t.Errorf("%s: %s", e.Field, e.Code)
		}
	}
	if _, errs = f.CheckValues(raw(`{"people":[{},{},{}]}`), nil); len(errs) == 0 || errs[0].Code != "rows_max" {
		t.Fatalf("too many rows: %+v", errs)
	}
	if _, errs = f.CheckValues(raw(`{"people":[]}`), nil); len(errs) != 1 || errs[0].Code != "rows_min" {
		t.Fatalf("no rows: %+v", errs)
	}
	for _, bad := range []string{
		`{"fields":[{"id":"g","type":"repeat","label":"G","fields":[]}]}`,
		`{"fields":[{"id":"g","type":"repeat","label":"G","fields":[{"id":"f","type":"file","label":"F"}]}]}`,
		`{"fields":[{"id":"g","type":"repeat","label":"G","fields":[{"id":"a","type":"text","label":"A"},{"id":"a","type":"text","label":"B"}]}]}`,
	} {
		f, _ := Decode([]byte(bad))
		if f.Validate() == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
