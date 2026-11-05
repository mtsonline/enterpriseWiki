package storage

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxText is the maximum amount of extracted text kept for the search index.
const MaxText = 1 << 20

// ExtractText returns searchable plain text of a stored file (PDF and text
// formats). For PDFs, poppler's pdftotext is used when installed (best
// quality); otherwise a built-in extractor handles common text-based PDFs.
func (s *Store) ExtractText(ctx context.Context, f *File) string {
	switch {
	case f.Mime == "application/pdf":
		if t := pdftotext(ctx, s.Path(f.Hash)); t != "" {
			return clip(t)
		}
		fh, err := s.Open(f.Hash)
		if err != nil {
			return ""
		}
		defer func() { _ = fh.Close() }()
		b, err := io.ReadAll(io.LimitReader(fh, 64<<20))
		if err != nil {
			return ""
		}
		return clip(PDFText(b))
	case strings.HasPrefix(f.Mime, "text/") && !strings.Contains(f.Mime, "html"), f.Mime == "application/json":
		fh, err := s.Open(f.Hash)
		if err != nil {
			return ""
		}
		defer func() { _ = fh.Close() }()
		b, _ := io.ReadAll(io.LimitReader(fh, MaxText))
		if !utf8.Valid(b) {
			b = bytes.ToValidUTF8(b, []byte(" "))
		}
		return clip(string(b))
	}
	return ""
}

func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > MaxText {
		s = s[:MaxText]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

func pdftotext(ctx context.Context, path string) string {
	bin, err := exec.LookPath("pdftotext")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-q", "-enc", "UTF-8", path, "-").Output() // #nosec G204 -- path of a stored file (generated, never starts with "-")
	if err != nil {
		return ""
	}
	return string(out)
}

var (
	streamRe = regexp.MustCompile(`(?s)<<(.*?)>>\s*stream\r?\n`)
	textOpRe = regexp.MustCompile(`(?s)(\((?:\\.|[^\\)])*\)|\[(?:[^\]])*\])\s*(Tj|TJ|'|")`)
	strRe    = regexp.MustCompile(`\((?:\\.|[^\\)])*\)`)
)

// PDFText is a small fallback extractor for PDFs with simple (WinAnsi /
// standard encoded) fonts: it inflates FlateDecode streams and collects the
// strings of the text-showing operators Tj, TJ, ' and ".
func PDFText(b []byte) string {
	var out strings.Builder
	idx := streamRe.FindAllSubmatchIndex(b, -1)
	for _, m := range idx {
		dict := string(b[m[2]:m[3]])
		start := m[1]
		end := bytes.Index(b[start:], []byte("endstream"))
		if end < 0 {
			continue
		}
		data := b[start : start+end]
		if strings.Contains(dict, "/FlateDecode") {
			zr, err := zlib.NewReader(bytes.NewReader(data))
			if err != nil {
				continue
			}
			dec, _ := io.ReadAll(io.LimitReader(zr, 32<<20))
			_ = zr.Close()
			data = dec
		} else if strings.Contains(dict, "/Filter") {
			continue // other filters (images etc.) are not text
		}
		if !bytes.Contains(data, []byte("BT")) {
			continue
		}
		for _, op := range textOpRe.FindAllSubmatch(data, -1) {
			for _, sm := range strRe.FindAll(op[1], -1) {
				out.WriteString(unescapePDF(sm[1 : len(sm)-1]))
			}
			out.WriteByte(' ')
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func unescapePDF(s []byte) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteRune(rune(c)) // Latin-1 / WinAnsi approximation
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r', 't', 'b', 'f':
			b.WriteByte(' ')
		case '(', ')', '\\':
			b.WriteByte(s[i])
		default:
			if s[i] >= '0' && s[i] <= '7' {
				v, n := 0, 0
				for n < 3 && i < len(s) && s[i] >= '0' && s[i] <= '7' {
					v = v*8 + int(s[i]-'0')
					i++
					n++
				}
				i--
				b.WriteRune(rune(v))
			}
		}
	}
	return b.String()
}
