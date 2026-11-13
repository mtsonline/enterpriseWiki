// Package ooxml writes simple Office Open XML documents without any
// dependency: a Word document (headings and paragraphs) and an Excel
// workbook (one sheet of text cells). Cells are always inline strings, so a
// value can never become a formula (no formula injection).
package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// Paragraph of a Word document.
type Paragraph struct {
	Text    string
	Heading int // 0: normal text, 1–3: heading level
	Bold    bool
}

func esc(s string) string {
	var b bytes.Buffer
	// drop characters XML 1.0 does not allow
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 && r != 0xfffe && r != 0xffff {
			return r
		}
		return -1
	}, s)
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

type file struct{ name, body string }

func pack(files []file) ([]byte, error) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := z.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const xmlHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

func core(title, lang string) string {
	return xmlHead + `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" ` +
		`xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:title>` + esc(title) + `</dc:title><dc:language>` + esc(lang) + `</dc:language>` +
		`<dcterms:created xsi:type="dcterms:W3CDTF">` + time.Now().UTC().Format(time.RFC3339) + `</dcterms:created></cp:coreProperties>`
}

// Docx writes a Word document with the title and language in its
// properties (accessibility: headings are real heading styles).
func Docx(title, lang string, paras []Paragraph) ([]byte, error) {
	var body strings.Builder
	for _, p := range paras {
		body.WriteString(`<w:p>`)
		if p.Heading > 0 {
			fmt.Fprintf(&body, `<w:pPr><w:pStyle w:val="Heading%d"/></w:pPr>`, min(p.Heading, 3))
		}
		lines := strings.Split(p.Text, "\n")
		for i, l := range lines {
			body.WriteString(`<w:r>`)
			if p.Bold {
				body.WriteString(`<w:rPr><w:b/></w:rPr>`)
			}
			if i > 0 {
				body.WriteString(`<w:br/>`)
			}
			body.WriteString(`<w:t xml:space="preserve">` + esc(l) + `</w:t></w:r>`)
		}
		body.WriteString(`</w:p>`)
	}
	heading := func(n, size int) string {
		return fmt.Sprintf(`<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/>`+
			`<w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="120"/><w:outlineLvl w:val="%d"/></w:pPr><w:rPr><w:b/><w:sz w:val="%d"/></w:rPr></w:style>`, n, n, n-1, size)
	}
	return pack([]file{
		{"[Content_Types].xml", xmlHead + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
			`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/></Types>`},
		{"_rels/.rels", xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/></Relationships>`},
		{"word/_rels/document.xml.rels", xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"word/styles.xml", xmlHead + `<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:sz w:val="22"/>` +
			`<w:lang w:val="` + esc(lang) + `"/></w:rPr></w:rPrDefault></w:docDefaults>` +
			`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="120"/></w:pPr></w:style>` +
			heading(1, 32) + heading(2, 28) + heading(3, 24) + `</w:styles>`},
		{"word/document.xml", xmlHead + `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() +
			`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="709" w:footer="709" w:gutter="0"/></w:sectPr></w:body></w:document>`},
		{"docProps/core.xml", core(title, lang)},
	})
}

// col is the column name of a 0-based index (A, B, … Z, AA …).
func col(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// Xlsx writes a workbook with one sheet; the first row is a header row
// (bold, frozen). All cells are text.
func Xlsx(title, lang, sheet string, rows [][]string) ([]byte, error) {
	if sheet = strings.TrimSpace(sheet); sheet == "" {
		sheet = "Sheet1"
	}
	// sheet names: at most 31 characters, none of []:*?/\
	sheet = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '_'
		}
		return r
	}, sheet)
	if r := []rune(sheet); len(r) > 31 {
		sheet = string(r[:31])
	}
	var data strings.Builder
	for i, row := range rows {
		fmt.Fprintf(&data, `<row r="%d">`, i+1)
		for j, v := range row {
			style := ""
			if i == 0 {
				style = ` s="1"`
			}
			fmt.Fprintf(&data, `<c r="%s%d" t="inlineStr"%s><is><t xml:space="preserve">%s</t></is></c>`, col(j), i+1, style, esc(v))
		}
		data.WriteString(`</row>`)
	}
	return pack([]file{
		{"[Content_Types].xml", xmlHead + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
			`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
			`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/></Types>`},
		{"_rels/.rels", xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/></Relationships>`},
		{"xl/_rels/workbook.xml.rels", xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"xl/workbook.xml", xmlHead + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets><sheet name="` + esc(sheet) + `" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/styles.xml", xmlHead + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
			`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
			`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
			`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
			`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
			`<cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs>` +
			`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`},
		{"xl/worksheets/sheet1.xml", xmlHead + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
			`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>` +
			`<sheetData>` + data.String() + `</sheetData></worksheet>`},
		{"docProps/core.xml", core(title, lang)},
	})
}
