package i18n

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

// Formats for import and export.
const (
	FormatJSON  = "json"  // flat {"key": "text"} like the locale files
	FormatXLIFF = "xliff" // XLIFF 2.0 (translation tools, agencies)
	FormatPO    = "po"    // gettext PO (Poedit, Weblate, …)
)

// ExportOptions selects what is exported.
type ExportOptions struct {
	Format string // json, xliff or po
	Source string // only the texts of this component (core or plugin id); "" = all
	// Filter: all, missing (missing and outdated) or custom (translated in the administration)
	Filter string
}

// entry is one exported text (a single string or one plural form).
type entry struct {
	key, source, ref, value, form string
	status                        string
}

func (s *Service) entries(list []String, o ExportOptions) []entry {
	var out []entry
	for _, x := range list {
		if o.Source != "" && x.Source != o.Source {
			continue
		}
		switch o.Filter {
		case "missing":
			if x.Status != "missing" && x.Status != "outdated" {
				continue
			}
		case "custom":
			if len(x.Custom) == 0 {
				continue
			}
		}
		forms := []string{""}
		if x.Plural {
			forms = x.Forms
		}
		for _, f := range forms {
			k := x.Key
			if f != "" {
				k += "_" + f
			}
			out = append(out, entry{key: k, source: x.Source, ref: x.Reference[f], value: x.Value[f], form: f, status: x.Status})
		}
	}
	return out
}

// Export returns the texts of a language as a file.
func (s *Service) Export(ctx context.Context, lang string, o ExportOptions) (data []byte, contentType, filename string, err error) {
	if err := s.requireTranslate(ctx, lang); err != nil {
		return nil, "", "", err
	}
	list, err := s.loadStrings(ctx, lang)
	if err != nil {
		return nil, "", "", err
	}
	es := s.entries(list, o)
	name := "ewiki-" + lang
	if o.Source != "" {
		name += "-" + o.Source
	}
	switch o.Format {
	case "", FormatJSON:
		m := map[string]string{}
		for _, e := range es {
			m[e.key] = e.value
		}
		b, err := json.MarshalIndent(m, "", " ")
		return append(b, '\n'), "application/json; charset=utf-8", name + ".json", err
	case FormatXLIFF:
		b, err := exportXLIFF(lang, es)
		return b, "application/xliff+xml; charset=utf-8", name + ".xlf", err
	case FormatPO:
		return exportPO(lang, es), "text/x-gettext-translation; charset=utf-8", name + ".po", nil
	}
	return nil, "", "", apperr.BadRequest("unknown format %q (json, xliff, po)", o.Format)
}

// Import stores the translations of a file (as translations made in the
// administration). Unknown keys and empty texts are skipped; PO entries
// marked fuzzy and XLIFF segments in state "initial" too.
func (s *Service) Import(ctx context.Context, lang, format string, data []byte) (SetResult, error) {
	if err := s.requireTranslate(ctx, lang); err != nil {
		return SetResult{}, err
	}
	if format == "" {
		format = DetectFormat(data)
	}
	var values map[string]string
	var err error
	switch format {
	case FormatJSON:
		err = json.Unmarshal(data, &values)
	case FormatXLIFF:
		values, err = parseXLIFF(lang, data)
	case FormatPO:
		values, err = parsePO(data)
	default:
		return SetResult{}, apperr.BadRequest("unknown format %q (json, xliff, po)", format)
	}
	if err != nil {
		return SetResult{}, apperr.BadRequest("%s: %v", format, err)
	}
	return s.set(ctx, lang, values, true)
}

// DetectFormat guesses the format of a file.
func DetectFormat(data []byte) string {
	t := bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	switch {
	case bytes.HasPrefix(t, []byte("{")):
		return FormatJSON
	case bytes.HasPrefix(t, []byte("<")):
		return FormatXLIFF
	}
	return FormatPO
}

// ---------------------------------------------------------------- XLIFF 2.0

type xliffDoc struct {
	XMLName xml.Name    `xml:"urn:oasis:names:tc:xliff:document:2.0 xliff"`
	Version string      `xml:"version,attr"`
	SrcLang string      `xml:"srcLang,attr"`
	TrgLang string      `xml:"trgLang,attr"`
	Files   []xliffFile `xml:"file"`
}

type xliffFile struct {
	ID    string      `xml:"id,attr"`
	Units []xliffUnit `xml:"unit"`
}

type xliffUnit struct {
	ID      string       `xml:"id,attr"`
	Notes   *xliffNotes  `xml:"notes,omitempty"`
	Segment xliffSegment `xml:"segment"`
}

type xliffNotes struct {
	Notes []xliffNote `xml:"note"`
}

type xliffNote struct {
	Category string `xml:"category,attr,omitempty"`
	Text     string `xml:",chardata"`
}

type xliffSegment struct {
	State  string `xml:"state,attr,omitempty"`
	Source string `xml:"source"`
	Target string `xml:"target,omitempty"`
}

func exportXLIFF(lang string, es []entry) ([]byte, error) {
	doc := xliffDoc{Version: "2.0", SrcLang: Reference, TrgLang: lang}
	files := map[string]*xliffFile{}
	var order []string
	for _, e := range es {
		f := files[e.source]
		if f == nil {
			f = &xliffFile{ID: e.source}
			files[e.source] = f
			order = append(order, e.source)
		}
		u := xliffUnit{ID: e.key, Segment: xliffSegment{Source: e.ref, Target: e.value, State: "translated"}}
		switch {
		case e.value == "":
			u.Segment.State = "initial"
		case e.status == "outdated":
			u.Segment.State = "initial" // needs review: the English text changed
			u.Notes = &xliffNotes{Notes: []xliffNote{{Category: "status", Text: "outdated: the English text changed"}}}
		}
		if e.form != "" {
			n := xliffNote{Category: "plural", Text: "plural form: " + e.form + pluralExample(lang, e.form)}
			if u.Notes == nil {
				u.Notes = &xliffNotes{}
			}
			u.Notes.Notes = append(u.Notes.Notes, n)
		}
		f.Units = append(f.Units, u)
	}
	sort.Strings(order)
	for _, id := range order {
		doc.Files = append(doc.Files, *files[id])
	}
	b, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(b, '\n')...), nil
}

func parseXLIFF(lang string, data []byte) (map[string]string, error) {
	var doc xliffDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.TrgLang != "" && !strings.EqualFold(doc.TrgLang, lang) {
		return nil, fmt.Errorf("the file is for the language %s, not %s", doc.TrgLang, lang)
	}
	out := map[string]string{}
	for _, f := range doc.Files {
		for _, u := range f.Units {
			if u.Segment.State == "initial" || u.Segment.Target == "" {
				continue
			}
			out[u.ID] = u.Segment.Target
		}
	}
	return out, nil
}

// pluralExample gives numbers that use a plural form (for translators).
func pluralExample(lang, form string) string {
	var ex []string
	for i := 0; i <= 200 && len(ex) < 4; i++ {
		if PluralForm(lang, strconv.Itoa(i)) == form {
			ex = append(ex, strconv.Itoa(i))
		}
	}
	if len(ex) == 0 && form == "other" {
		return " (e.g. 1.5)"
	}
	if len(ex) == 0 {
		return ""
	}
	return " (e.g. " + strings.Join(ex, ", ") + ")"
}

// ---------------------------------------------------------------- PO

// exportPO writes one entry per key with msgctxt = key, so every tool can
// handle plural forms as separate entries (no Plural-Forms formula needed).
func exportPO(lang string, es []entry) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# Enterprise Wiki user interface: %s\n", lang)
	b.WriteString("msgid \"\"\nmsgstr \"\"\n")
	for _, h := range []string{
		"Project-Id-Version: Enterprise Wiki", "Language: " + lang, "MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8", "Content-Transfer-Encoding: 8bit",
		"X-Generator: ewiki", "PO-Revision-Date: " + time.Now().UTC().Format("2006-01-02 15:04-0700"),
	} {
		fmt.Fprintf(&b, "%s\n", poQuote(h+"\n"))
	}
	for _, e := range es {
		b.WriteString("\n")
		if e.form != "" {
			fmt.Fprintf(&b, "#. plural form: %s%s\n", e.form, pluralExample(lang, e.form))
		}
		fmt.Fprintf(&b, "#: %s\n", e.source)
		if e.status == "outdated" && e.value != "" {
			b.WriteString("#, fuzzy\n")
		}
		fmt.Fprintf(&b, "msgctxt %s\nmsgid %s\nmsgstr %s\n", poQuote(e.key), poQuote(e.ref), poQuote(e.value))
	}
	return b.Bytes()
}

func poQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
	return `"` + r.Replace(s) + `"`
}

func poUnquote(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("invalid string %s", s)
	}
	return strconv.Unquote(s)
}

// parsePO reads entries with msgctxt (the key); entries without context,
// plural entries (msgid_plural) and fuzzy entries are skipped.
func parsePO(data []byte) (map[string]string, error) {
	out := map[string]string{}
	type po struct {
		ctx, id, str  string
		fuzzy, plural bool
		hasCtx        bool
	}
	var cur po
	field := ""
	flush := func() {
		if cur.hasCtx && !cur.fuzzy && !cur.plural && cur.str != "" {
			out[cur.ctx] = cur.str
		}
		cur, field = po{}, ""
	}
	sc := bufio.NewScanner(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		l := strings.TrimSpace(sc.Text())
		switch {
		case l == "":
			flush()
		case strings.HasPrefix(l, "#,"):
			if field != "" {
				flush()
			}
			cur.fuzzy = strings.Contains(l, "fuzzy")
		case strings.HasPrefix(l, "#"):
			if field == "msgstr" {
				flush()
			}
		case strings.HasPrefix(l, `"`):
			v, err := poUnquote(l)
			if err != nil {
				return nil, fmt.Errorf("line %d: %v", line, err)
			}
			switch field {
			case "msgctxt":
				cur.ctx += v
			case "msgid":
				cur.id += v
			case "msgstr":
				cur.str += v
			}
		default:
			kw, rest, _ := strings.Cut(l, " ")
			if (kw == "msgctxt" || kw == "msgid") && (field == "msgstr" || field == "pluralstr") {
				flush() // next entry without a blank line in between
			}
			v, err := poUnquote(rest)
			if err != nil {
				return nil, fmt.Errorf("line %d: %v", line, err)
			}
			switch {
			case kw == "msgctxt":
				field, cur.ctx, cur.hasCtx = "msgctxt", v, true
			case kw == "msgid":
				field, cur.id = "msgid", v
			case kw == "msgid_plural":
				field, cur.plural = "plural", true
			case kw == "msgstr":
				field, cur.str = "msgstr", v
			case strings.HasPrefix(kw, "msgstr["):
				field = "pluralstr"
			default:
				return nil, fmt.Errorf("line %d: unexpected %q", line, kw)
			}
		}
	}
	flush()
	return out, sc.Err()
}
