package core_template

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

// TestCheckWOFF2: a real font passes; files that are no WOFF2, collections,
// a wrong length or parts outside the file are refused.
func TestCheckWOFF2(t *testing.T) {
	good, err := os.ReadFile("testdata/inter-sample.woff2") // Inter, SIL OFL 1.1 (see the LICENSE file beside it)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkWOFF2(good); err != nil {
		t.Fatalf("a real WOFF2 font: %v", err)
	}
	bad := func(name, want string, change func(b []byte) []byte) {
		t.Helper()
		b := change(append([]byte(nil), good...))
		err := checkWOFF2(b)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v (want %q)", name, err, want)
		}
	}
	be := binary.BigEndian
	bad("empty", "too short", func(b []byte) []byte { return nil })
	bad("WOFF 1", "not a WOFF2", func(b []byte) []byte { copy(b, "wOFF"); return b })
	bad("collection", "collections", func(b []byte) []byte { copy(b[4:], "ttcf"); return b })
	bad("unknown flavor", "unknown kind", func(b []byte) []byte { copy(b[4:], "abcd"); return b })
	bad("cut off", "length", func(b []byte) []byte { return b[:len(b)-10] })
	bad("longer", "length", func(b []byte) []byte { return append(b, 0) })
	bad("no tables", "damaged", func(b []byte) []byte { be.PutUint16(b[12:], 0); return b })
	bad("reserved", "damaged", func(b []byte) []byte { be.PutUint16(b[14:], 1); return b })
	bad("compressed too long", "damaged", func(b []byte) []byte { be.PutUint32(b[20:], uint32(len(b)+1)); return b })
	bad("metadata outside", "damaged", func(b []byte) []byte { be.PutUint32(b[28:], uint32(len(b)-4)); be.PutUint32(b[32:], 100); return b })
	bad("private data in the header", "damaged", func(b []byte) []byte { be.PutUint32(b[40:], 8); be.PutUint32(b[44:], 4); return b })
	bad("offset overflows", "damaged", func(b []byte) []byte { be.PutUint32(b[28:], 0xFFFFFFF0); be.PutUint32(b[32:], 0x20); return b })
}

// TestOwnFontsInDesign: own fonts in the tokens by their form only (whether
// they exist is checked by the store).
func TestOwnFontsInDesign(t *testing.T) {
	d := example(t, "wiki")
	d.Tokens.Font, d.Tokens.HeadingFont = "own-12", "own-12"
	if ps := d.Problems(); len(ps) != 0 {
		t.Fatalf("own font: %+v", ps)
	}
	if got := d.OwnFonts(); len(got) != 1 || got[0] != 12 {
		t.Fatalf("own fonts: %v", got)
	}
	for _, bad := range []string{"own-0", "own-", "own-012", "own-1x", "own-1;}", "Arial", "url(x)"} {
		d.Tokens.Font = bad
		if ps := d.Problems(); len(ps) == 0 {
			t.Errorf("font %q accepted", bad)
		}
	}
}

// TestFontFormats: real fonts in the four formats are recognised; damaged
// directories, collections and other files are refused.
func TestFontFormats(t *testing.T) {
	for file, want := range map[string]string{"inter-sample.woff2": "woff2", "caladea-italic.woff": "woff", "caladea-bold.ttf": "truetype", "gfs-porson.otf": "opentype"} {
		b, err := os.ReadFile("testdata/" + file) // fonts under the SIL OFL 1.1, see testdata/README.md
		if err != nil {
			t.Fatal(err)
		}
		got, err := fontFormat(b)
		if err != nil || got != want {
			t.Errorf("%s: %q %v", file, got, err)
		}
		// cut off: a table or the length no longer fits
		if _, err := fontFormat(b[:len(b)-200]); err == nil {
			t.Errorf("%s cut off accepted", file)
		}
	}
	be := binary.BigEndian
	ttf, _ := os.ReadFile("testdata/caladea-bold.ttf")
	woff, _ := os.ReadFile("testdata/caladea-italic.woff")
	for name, c := range map[string]struct {
		file   []byte
		change func(b []byte)
	}{
		"ttf: no tables":             {ttf, func(b []byte) { be.PutUint16(b[4:], 0) }},
		"ttf: too many tables":       {ttf, func(b []byte) { be.PutUint16(b[4:], 1000) }},
		"ttf: table outside":         {ttf, func(b []byte) { be.PutUint32(b[12+8:], uint32(len(b)-4)); be.PutUint32(b[12+12:], 100) }},
		"ttf: table in the header":   {ttf, func(b []byte) { be.PutUint32(b[12+8:], 4) }},
		"woff: wrong length":         {woff, func(b []byte) { be.PutUint32(b[8:], 7) }},
		"woff: reserved":             {woff, func(b []byte) { be.PutUint16(b[14:], 1) }},
		"woff: table outside":        {woff, func(b []byte) { be.PutUint32(b[44+4:], uint32(len(b)-1)) }}, // only the place: its lengths stay valid
		"woff: compressed > orig":    {woff, func(b []byte) { be.PutUint32(b[44+8:], be.Uint32(b[44+12:])+1) }},
		"woff: metadata outside":     {woff, func(b []byte) { be.PutUint32(b[24:], uint32(len(b)-2)); be.PutUint32(b[28:], 10) }},
		"collection":                 {ttf, func(b []byte) { copy(b, "ttcf") }},
		"svg":                        {ttf, func(b []byte) { copy(b, "<svg") }},
		"woff: unknown kind of font": {woff, func(b []byte) { copy(b[4:], "abcd") }},
	} {
		b := append([]byte(nil), c.file...)
		c.change(b)
		if _, err := fontFormat(b); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	for in, want := range map[string]string{"": "400", "700": "700", "100 900": "100 900"} {
		if got, err := checkWeight(in); err != nil || got != want {
			t.Errorf("weight %q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"450", "1000", "0", "900 100", "400 400", "bold", "400;", "100  900"} {
		if _, err := checkWeight(bad); err == nil {
			t.Errorf("weight %q accepted", bad)
		}
	}
	css := fontCSS(12, []FontFace{{N: 1, Weight: "400", Style: "normal", Format: "truetype"}, {N: 2, Weight: "100 900", Style: "italic", Format: "woff2"}})
	for _, want := range []string{`url("/api/v1/x/core.template/fonts/12/files/1") format("truetype"); font-weight: 400; font-style: normal;`, `files/2") format("woff2"); font-weight: 100 900; font-style: italic;`} {
		if !strings.Contains(css, want) {
			t.Errorf("css: %s", css)
		}
	}
}
