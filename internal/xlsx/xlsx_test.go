package xlsx

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestWrite(t *testing.T) {
	var buf bytes.Buffer
	rows := [][]Cell{{Text("=HYPERLINK(\"x\")"), Num(3.5)}, {Text("a<b>&\x01c"), Text("")}}
	if err := Write(&buf, "Report: [1]", []string{"Title", "Value"}, rows); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		files[f.Name] = string(b)
	}
	sheet := files["xl/worksheets/sheet1.xml"]
	for _, want := range []string{`t="inlineStr"><is><t xml:space="preserve">=HYPERLINK(&#34;x&#34;)</t>`, `<v>3.5</v>`, `a&lt;b&gt;&amp;c`, `r="B1"`} {
		if !strings.Contains(sheet, want) {
			t.Errorf("sheet lacks %q: %s", want, sheet)
		}
	}
	if strings.Contains(sheet, "<f>") {
		t.Error("formula written")
	}
	buf.Reset()
	if err := Write(&buf, "x", []string{"a"}, [][]Cell{{Formula("SUM(1,2)")}}); err != nil {
		t.Fatal(err)
	}
	z, _ = zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	for _, f := range z.File {
		if f.Name == "xl/worksheets/sheet1.xml" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			if !strings.Contains(string(b), "<f>SUM(1,2)</f>") {
				t.Errorf("formula: %s", b)
			}
		}
	}
	if !strings.Contains(files["xl/workbook.xml"], `name="Report   1 "`) {
		t.Errorf("sheet name: %s", files["xl/workbook.xml"])
	}
	if col(0) != "A" || col(25) != "Z" || col(26) != "AA" || col(701) != "ZZ" || col(702) != "AAA" {
		t.Error("column letters")
	}
}
