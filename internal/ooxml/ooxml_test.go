package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

// every part is well-formed XML; values are escaped and never formulas
func parts(t *testing.T, b []byte) map[string]string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		_ = r.Close()
		d := xml.NewDecoder(bytes.NewReader(data))
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", f.Name, err)
			}
		}
		out[f.Name] = string(data)
	}
	return out
}

func TestDocx(t *testing.T) {
	b, err := Docx("Report <1>", "de", []Paragraph{{Text: "Title & more", Heading: 1}, {Text: "line 1\nline 2 <script>\x01"}, {Text: "Who", Bold: true}})
	if err != nil {
		t.Fatal(err)
	}
	p := parts(t, b)
	doc := p["word/document.xml"]
	if !strings.Contains(doc, `<w:pStyle w:val="Heading1"/>`) || !strings.Contains(doc, "&lt;script&gt;") || !strings.Contains(doc, "<w:br/>") {
		t.Fatal(doc)
	}
	if !strings.Contains(p["docProps/core.xml"], "Report &lt;1&gt;") {
		t.Fatal(p["docProps/core.xml"])
	}
}

func TestXlsx(t *testing.T) {
	rows := [][]string{{"Field", "Value"}, {"Sum", "=1+1"}, {"Long", strings.Repeat("x", 30)}}
	b, err := Xlsx("T", "en", "Data [1]/x", rows)
	if err != nil {
		t.Fatal(err)
	}
	p := parts(t, b)
	sh := p["xl/worksheets/sheet1.xml"]
	if strings.Contains(sh, "<f>") || !strings.Contains(sh, `t="inlineStr"`) || !strings.Contains(sh, "=1+1") || !strings.Contains(sh, `r="B3"`) {
		t.Fatal(sh)
	}
	if !strings.Contains(p["xl/workbook.xml"], `name="Data _1__x"`) {
		t.Fatal(p["xl/workbook.xml"])
	}
	if col(0) != "A" || col(25) != "Z" || col(26) != "AA" || col(701) != "ZZ" || col(702) != "AAA" {
		t.Fatal(col(26), col(701), col(702))
	}
}
