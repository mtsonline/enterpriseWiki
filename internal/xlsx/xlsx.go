// Package xlsx writes simple spreadsheets (one sheet, a bold header row,
// text and number cells) in the Office Open XML format – without
// dependencies. Text is always written as text (no formulas), so cells
// starting with "=" are never evaluated.
package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Cell is a text or a number.
type Cell struct {
	Text   string
	Number *float64
}

// Text returns a text cell.
func Text(s string) Cell { return Cell{Text: s} }

// Num returns a number cell.
func Num(f float64) Cell { return Cell{Number: &f} }

// MaxCellText is the limit of Excel for one cell.
const MaxCellText = 32767

// Write writes a workbook with one sheet: header in bold, then the rows.
func Write(w io.Writer, sheet string, header []string, rows [][]Cell) error {
	z := zip.NewWriter(w)
	files := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="` + esc(sheetName(sheet)) + `" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf/></cellStyleXfs><cellXfs count="2"><xf fontId="0"/><xf fontId="1" applyFont="1"/></cellXfs></styleSheet>`},
	}
	for _, f := range files {
		fw, err := z.Create(f.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(fw, f.body); err != nil {
			return err
		}
	}
	fw, err := z.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><sheetData>`)
	b.WriteString(`<row r="1">`)
	for i, hd := range header {
		fmt.Fprintf(&b, `<c r="%s1" t="inlineStr" s="1"><is><t xml:space="preserve">%s</t></is></c>`, col(i), esc(clip(hd)))
	}
	b.WriteString(`</row>`)
	if _, err := io.WriteString(fw, b.String()); err != nil {
		return err
	}
	for r, row := range rows {
		b.Reset()
		fmt.Fprintf(&b, `<row r="%d">`, r+2)
		for i, c := range row {
			ref := col(i) + strconv.Itoa(r+2)
			if c.Number != nil {
				fmt.Fprintf(&b, `<c r="%s"><v>%s</v></c>`, ref, strconv.FormatFloat(*c.Number, 'g', -1, 64))
			} else if c.Text != "" {
				fmt.Fprintf(&b, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, esc(clip(c.Text)))
			}
		}
		b.WriteString(`</row>`)
		if _, err := io.WriteString(fw, b.String()); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(fw, `</sheetData></worksheet>`); err != nil {
		return err
	}
	return z.Close()
}

// col returns the column letters (0 = A).
func col(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= MaxCellText {
		return s
	}
	return string([]rune(s)[:MaxCellText])
}

// esc escapes XML and drops characters XML 1.0 does not allow.
func esc(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r != 0xFFFE && r != 0xFFFF) {
			return r
		}
		return -1
	}, s)
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func sheetName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return ' '
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" {
		s = "Sheet1"
	}
	if utf8.RuneCountInString(s) > 31 {
		s = string([]rune(s)[:31])
	}
	return s
}
