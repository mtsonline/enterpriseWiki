package storage

import (
	"bytes"
	"compress/zlib"
	"strings"
	"testing"
)

func TestPDFText(t *testing.T) {
	var cs bytes.Buffer
	zw := zlib.NewWriter(&cs)
	zw.Write([]byte(`BT /F1 12 Tf (Hello \(PDF\) world) Tj [(Split) -200 (Text)] TJ ET`))
	zw.Close()
	pdf := []byte("%PDF-1.4\n4 0 obj << /Length 10 /Filter /FlateDecode >> stream\n")
	pdf = append(pdf, cs.Bytes()...)
	pdf = append(pdf, []byte("\nendstream endobj\n")...)
	got := PDFText(pdf)
	if !strings.Contains(got, "Hello (PDF) world") || !strings.Contains(got, "SplitText") {
		t.Fatalf("%q", got)
	}
}

func TestDetectMime(t *testing.T) {
	cases := map[string]string{
		"a.pdf":  DetectMime([]byte("%PDF-1.7"), "a.pdf"),
		"a.svg":  DetectMime([]byte("<svg xmlns=\"http://www.w3.org/2000/svg\">"), "a.svg"),
		"a.html": DetectMime([]byte("hello"), "a.html"),
		"a.csv":  DetectMime([]byte("a,b\n1,2"), "a.csv"),
	}
	if cases["a.pdf"] != "application/pdf" || strings.Contains(cases["a.svg"], "svg") || strings.Contains(cases["a.html"], "html") || cases["a.csv"] != "text/csv; charset=utf-8" {
		t.Fatalf("%v", cases)
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{"../../etc/passwd": "passwd", `C:\x\y.doc`: "y.doc", "a\x00b": "a_b", "..": "file", "": "file"} {
		if got := CleanName(in); got != want {
			t.Errorf("%q -> %q", in, got)
		}
	}
}
