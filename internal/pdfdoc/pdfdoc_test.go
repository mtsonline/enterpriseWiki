package pdfdoc

import (
	"bytes"
	"strings"
	"testing"
)

func TestWrite(t *testing.T) {
	var lines []Line
	for range 120 {
		lines = append(lines, Line{Text: "Größe: (über) 10 € – " + strings.Repeat("Wort ", 30)})
	}
	b := Write("Einsendung (1)", "de", append([]Line{{Text: "Titel", Bold: true, Size: 16}}, lines...))
	if !bytes.HasPrefix(b, []byte("%PDF-1.4")) || !bytes.HasSuffix(b, []byte("%%EOF\n")) {
		t.Fatal("not a PDF")
	}
	if bytes.Count(b, []byte("/Type /Page ")) < 3 {
		t.Fatal("expected several pages")
	}
	if !bytes.Contains(b, []byte("Gr\xf6\xdfe: \\(\xfcber\\) 10 \x80")) {
		t.Fatal("encoding or escaping")
	}
	if !bytes.Contains(b, []byte("/Title (Einsendung \\(1\\))")) || !bytes.Contains(b, []byte("/Lang (de)")) {
		t.Fatal("info")
	}
}
