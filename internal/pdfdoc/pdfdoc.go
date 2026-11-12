// Package pdfdoc writes simple text documents as PDF (A4, Helvetica,
// Windows-1252 characters, several pages, title and language in the
// document information). It is meant for records such as a form
// submission, not for layout.
package pdfdoc

import (
	"bytes"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Line is a paragraph of the document.
type Line struct {
	Text  string
	Bold  bool
	Size  float64 // points (default 11)
	Space float64 // extra space above (points)
}

const (
	pageW, pageH = 595.0, 842.0
	margin       = 56.0
)

// cp1252 maps the characters outside Latin-1 that Windows-1252 has.
var cp1252 = map[rune]byte{'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87, 'ˆ': 0x88, '‰': 0x89, 'Š': 0x8a,
	'‹': 0x8b, 'Œ': 0x8c, 'Ž': 0x8e, '‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97, '˜': 0x98, '™': 0x99,
	'š': 0x9a, '›': 0x9b, 'œ': 0x9c, 'ž': 0x9e, 'Ÿ': 0x9f, '✓': 'x'}

func encode(s string) []byte {
	var b []byte
	for _, r := range s {
		switch {
		case r == '\t':
			b = append(b, ' ')
		case r < 32:
		case r < 128 || r >= 0xa0 && r <= 0xff:
			b = append(b, byte(r)) // #nosec G115 -- checked range
		default:
			if c, ok := cp1252[r]; ok {
				b = append(b, c)
			} else {
				b = append(b, '?')
			}
		}
	}
	return b
}

// width estimates the width of s in em (Helvetica).
func width(s string, bold bool) float64 {
	w := 0.0
	for _, r := range s {
		switch {
		case strings.ContainsRune("iljI.,;:'|!()[]ft ", r):
			w += 0.28
		case strings.ContainsRune("mwMW", r):
			w += 0.85
		case r >= 'A' && r <= 'Z', strings.ContainsRune("ÄÖÜ", r):
			w += 0.68
		default:
			w += 0.56
		}
	}
	if bold {
		w *= 1.07
	}
	return w
}

// wrap breaks a paragraph into lines of at most maxW points.
func wrap(text string, size float64, bold bool, maxW float64) []string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, w := range words {
			for width(w, bold)*size > maxW && utf8.RuneCountInString(w) > 1 { // very long words are broken
				r := []rune(w)
				n := len(r)
				for n > 1 && width(string(r[:n]), bold)*size > maxW {
					n--
				}
				if line != "" {
					out = append(out, line)
					line = ""
				}
				out = append(out, string(r[:n]))
				w = string(r[n:])
			}
			cand := w
			if line != "" {
				cand = line + " " + w
			}
			if width(cand, bold)*size > maxW && line != "" {
				out = append(out, line)
				line = w
			} else {
				line = cand
			}
		}
		out = append(out, line)
	}
	return out
}

func esc(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte(`\`), []byte(`\\`))
	b = bytes.ReplaceAll(b, []byte("("), []byte(`\(`))
	return bytes.ReplaceAll(b, []byte(")"), []byte(`\)`))
}

// Write returns the PDF of the lines.
func Write(title, lang string, lines []Line) []byte {
	type row struct {
		text    []byte
		bold    bool
		size, y float64
	}
	var pages [][]row
	var cur []row
	y := pageH - margin
	for _, l := range lines {
		size := l.Size
		if size <= 0 {
			size = 11
		}
		lead := size * 1.35
		y -= l.Space
		for _, t := range wrap(l.Text, size, l.Bold, pageW-2*margin) {
			if y-lead < margin {
				pages = append(pages, cur)
				cur, y = nil, pageH-margin
			}
			y -= lead
			cur = append(cur, row{encode(t), l.Bold, size, y})
		}
	}
	pages = append(pages, cur)

	var objs [][]byte
	add := func(s []byte) int { objs = append(objs, s); return len(objs) }
	catalog := add(nil) // filled at the end
	pagesObj := add(nil)
	font := add([]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"))
	bold := add([]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>"))
	var kids []string
	for _, p := range pages {
		var s bytes.Buffer
		for _, r := range p {
			f := "F1"
			if r.bold {
				f = "F2"
			}
			fmt.Fprintf(&s, "BT /%s %.1f Tf %.1f %.1f Td (", f, r.size, margin, r.y)
			s.Write(esc(r.text))
			s.WriteString(") Tj ET\n")
		}
		content := add([]byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", s.Len(), s.String())))
		page := add([]byte(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 %d 0 R /F2 %d 0 R >> >> /Contents %d 0 R >>",
			pagesObj, pageW, pageH, font, bold, content)))
		kids = append(kids, fmt.Sprintf("%d 0 R", page))
	}
	objs[pagesObj-1] = []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(kids)))
	objs[catalog-1] = []byte(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R /Lang (%s) /ViewerPreferences << /DisplayDocTitle true >> >>", pagesObj, esc(encode(lang))))
	info := add([]byte(fmt.Sprintf("<< /Title (%s) /Producer (Enterprise Wiki) /CreationDate (D:%s) >>", esc(encode(title)), time.Now().UTC().Format("20060102150405Z"))))

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, catalog, info, xref)
	return b.Bytes()
}
