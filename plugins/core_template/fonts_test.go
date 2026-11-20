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
	bad("TrueType", "not a WOFF2", func(b []byte) []byte { copy(b, "\x00\x01\x00\x00"); return b })
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
