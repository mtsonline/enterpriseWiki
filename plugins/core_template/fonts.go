package core_template

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Uploaded fonts: a font has one or more files (faces: regular, bold,
// italic … or one variable font with all weights), in the usual formats
// WOFF2, WOFF, TrueType or OpenType. They are stored in the plugin's own
// tables and served unchanged by the wiki itself (never from elsewhere).
// The person uploading confirms the right to use the font on the web and
// names its license. The files are not converted or subset, so the rule of
// the SIL Open Font License on reserved font names (only for modified
// versions) does not apply.

// FontLicenses are the licenses to choose from; "other" needs a note.
var FontLicenses = []string{"OFL-1.1", "Apache-2.0", "MIT", "other"}

// FontFormats: the formats of font files and their media types.
var FontFormats = map[string]string{"woff2": "font/woff2", "woff": "font/woff", "truetype": "font/ttf", "opentype": "font/otf"}

const (
	limitFontKB    = "core.template.font_kb"
	limitFontCount = "core.template.fonts"
	limitFontBody  = "core.template.font_upload_kb"
	// MaxFontFaces: files of one font at most (four weights, each upright and italic)
	MaxFontFaces = 8
)

// Font is an uploaded font (without its files).
type Font struct {
	ID          int64      `json:"id"`
	Token       string     `json:"token"` // own-ID, the value in the tokens of a design
	Name        string     `json:"name"`
	License     string     `json:"license"`
	LicenseNote string     `json:"license_note"`
	Faces       []FontFace `json:"faces"`
	Bytes       int        `json:"bytes"` // of all its files
	CreatedAt   time.Time  `json:"created_at"`
	CreatedBy   *int64     `json:"created_by"`
	// UsedBy: the stored templates (also in the trash) that use it
	UsedBy []string `json:"used_by"`
}

// FontFace is one file of a font.
type FontFace struct {
	N      int    `json:"n"`
	Weight string `json:"weight"` // "400" or a range of a variable font "100 900"
	Style  string `json:"style"`  // normal | italic
	Format string `json:"format"` // woff2 | woff | truetype | opentype
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type fontInput struct {
	Name        string `json:"name"`
	License     string `json:"license"`
	LicenseNote string `json:"license_note"`
	// Confirm: "I may use this font on this website" (needed)
	Confirm bool            `json:"confirm"`
	Files   []fontFileInput `json:"files"`
}

type fontFileInput struct {
	Data   string `json:"data"`   // the file, base64
	Weight string `json:"weight"` // empty: 400
	Style  string `json:"style"`  // empty: normal
}

var (
	fontNameRe   = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,59}$`)
	fontWeightRe = regexp.MustCompile(`^([1-9]00)(?: ([1-9]00))?$`)
)

// checkWeight: "100" … "900", or a range "lo hi" with lo < hi.
func checkWeight(w string) (string, error) {
	w = strings.TrimSpace(w)
	if w == "" {
		return "400", nil
	}
	m := fontWeightRe.FindStringSubmatch(w)
	if m == nil || (m[2] != "" && m[1] >= m[2]) {
		return "", fmt.Errorf("weight: 100 to 900 in hundreds, or a range like \"100 900\" (variable font)")
	}
	return w, nil
}

// fontFormat finds the format of a font file by its first bytes and checks
// its header; browsers check the font tables themselves before they use a
// font.
func fontFormat(b []byte) (string, error) {
	if len(b) < 12 {
		return "", fmt.Errorf("too short for a font file")
	}
	if len(b) > math.MaxUint32 {
		return "", fmt.Errorf("too large for a font file")
	}
	switch string(b[0:4]) {
	case "wOF2":
		return "woff2", checkWOFF2(b)
	case "wOFF":
		return "woff", checkWOFF(b)
	case "\x00\x01\x00\x00", "true":
		return "truetype", checkSFNT(b)
	case "OTTO":
		return "opentype", checkSFNT(b)
	case "ttcf":
		return "", fmt.Errorf("font collections are not supported; upload one font per file")
	}
	return "", fmt.Errorf("not a font file (WOFF2, WOFF, TTF or OTF)")
}

// fileLen is the length of a checked file as a header field (fontFormat
// refuses files over 4 GiB before).
func fileLen(b []byte) uint32 {
	return uint32(len(b)) // #nosec G115 -- fontFormat refuses files longer than MaxUint32
}

// within reports whether [off, off+length) lies in a file of n bytes, at or after min.
func within(off, length uint32, min uint64, n uint32) bool {
	return uint64(off) >= min && uint64(off)+uint64(length) <= uint64(n)
}

// checkWOFF2 checks a WOFF2 file (W3C recommendation, section 3): a single
// font of TrueType or CFF outlines (no collection), the length in the header
// is the length of the file, and every part named in the header lies inside it.
func checkWOFF2(b []byte) error {
	const header = 48
	if len(b) < header {
		return fmt.Errorf("too short for a WOFF2 file")
	}
	be := binary.BigEndian
	if string(b[0:4]) != "wOF2" {
		return fmt.Errorf("not a WOFF2 file")
	}
	switch string(b[4:8]) {
	case "\x00\x01\x00\x00", "OTTO", "true":
	case "ttcf":
		return fmt.Errorf("font collections are not supported; upload one font per file")
	default:
		return fmt.Errorf("unknown kind of font in the WOFF2 file")
	}
	if len(b) > math.MaxUint32 {
		return fmt.Errorf("too large for a WOFF2 file")
	}
	n := fileLen(b)
	if be.Uint32(b[8:12]) != n {
		return fmt.Errorf("the length in the WOFF2 header does not match the file")
	}
	if be.Uint16(b[12:14]) == 0 || be.Uint16(b[14:16]) != 0 {
		return fmt.Errorf("damaged WOFF2 header")
	}
	if compressed := be.Uint32(b[20:24]); compressed == 0 || compressed > n {
		return fmt.Errorf("damaged WOFF2 header")
	}
	inside := func(off, length uint32) bool { return off == 0 && length == 0 || within(off, length, header, n) }
	if !inside(be.Uint32(b[28:32]), be.Uint32(b[32:36])) || !inside(be.Uint32(b[40:44]), be.Uint32(b[44:48])) {
		return fmt.Errorf("damaged WOFF2 header")
	}
	return nil
}

// checkWOFF checks a WOFF 1.0 file (W3C recommendation, sections 3 and 4):
// header, the length of the file, and every table and the extra data inside
// the file after the table directory.
func checkWOFF(b []byte) error {
	const header, entry = 44, 20
	if len(b) < header {
		return fmt.Errorf("too short for a WOFF file")
	}
	be := binary.BigEndian
	switch string(b[4:8]) {
	case "\x00\x01\x00\x00", "OTTO", "true":
	default:
		return fmt.Errorf("unknown kind of font in the WOFF file")
	}
	n := fileLen(b)
	if be.Uint32(b[8:12]) != n {
		return fmt.Errorf("the length in the WOFF header does not match the file")
	}
	tables := uint32(be.Uint16(b[12:14]))
	if tables == 0 || tables > 200 || be.Uint16(b[14:16]) != 0 {
		return fmt.Errorf("damaged WOFF header")
	}
	dirEnd := uint64(header) + uint64(tables)*entry
	if dirEnd > uint64(n) {
		return fmt.Errorf("damaged WOFF header")
	}
	for i := uint64(0); i < uint64(tables); i++ {
		e := b[header+i*entry:]
		off, comp, orig := be.Uint32(e[4:8]), be.Uint32(e[8:12]), be.Uint32(e[12:16])
		if comp > orig || !within(off, comp, dirEnd, n) {
			return fmt.Errorf("damaged WOFF table directory")
		}
	}
	inside := func(off, length uint32) bool { return off == 0 && length == 0 || within(off, length, dirEnd, n) }
	if !inside(be.Uint32(b[24:28]), be.Uint32(b[28:32])) || !inside(be.Uint32(b[36:40]), be.Uint32(b[40:44])) {
		return fmt.Errorf("damaged WOFF header")
	}
	return nil
}

// checkSFNT checks a TrueType or OpenType file: the table directory, and
// every table inside the file after it.
func checkSFNT(b []byte) error {
	const header, entry = 12, 16
	be := binary.BigEndian
	n := fileLen(b)
	tables := uint32(be.Uint16(b[4:6]))
	if tables == 0 || tables > 200 {
		return fmt.Errorf("damaged font file (table directory)")
	}
	dirEnd := uint64(header) + uint64(tables)*entry
	if dirEnd > uint64(n) {
		return fmt.Errorf("damaged font file (table directory)")
	}
	for i := uint64(0); i < uint64(tables); i++ {
		e := b[header+i*entry:]
		if !within(be.Uint32(e[8:12]), be.Uint32(e[12:16]), dirEnd, n) {
			return fmt.Errorf("damaged font file (a table lies outside the file)")
		}
	}
	return nil
}

// fontProblems: the uploaded fonts a design uses must exist.
func (p *Plugin) fontProblems(ctx context.Context, d *Design) ([]Problem, error) {
	ids := d.OwnFonts()
	if len(ids) == 0 {
		return nil, nil
	}
	var have []int64
	rows, err := p.host.DB.Query(ctx, `SELECT id FROM plugin_template_fonts WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	if have, err = pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
		return nil, err
	}
	var out []Problem
	for _, id := range ids {
		if !slices.Contains(have, id) {
			out = append(out, Problem{Where: "tokens.font", Text: fmt.Sprintf("the uploaded font own-%d does not exist (any more)", id)})
		}
	}
	return out, nil
}

// faces returns the files of fonts (without their data), by font.
func (p *Plugin) faces(ctx context.Context, ids []int64) (map[int64][]FontFace, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT font_id, n, weight, style, format, octet_length(data), sha256 FROM plugin_template_font_files
		WHERE font_id = ANY($1) ORDER BY font_id, n`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]FontFace{}
	for rows.Next() {
		var id int64
		var f FontFace
		if err := rows.Scan(&id, &f.N, &f.Weight, &f.Style, &f.Format, &f.Bytes, &f.SHA256); err != nil {
			return nil, err
		}
		out[id] = append(out[id], f)
	}
	return out, rows.Err()
}

func (p *Plugin) fonts(ctx context.Context) ([]Font, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT id, name, license, license_note, created_at, created_by FROM plugin_template_fonts ORDER BY lower(name), id`)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Font, error) {
		var f Font
		err := r.Scan(&f.ID, &f.Name, &f.License, &f.LicenseNote, &f.CreatedAt, &f.CreatedBy)
		f.Token, f.UsedBy, f.Faces = "own-"+strconv.FormatInt(f.ID, 10), []string{}, []FontFace{}
		return f, err
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(out))
	for i := range out {
		ids[i] = out[i].ID
	}
	faces, err := p.faces(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if fs := faces[out[i].ID]; fs != nil {
			out[i].Faces = fs
		}
		for _, f := range out[i].Faces {
			out[i].Bytes += f.Bytes
		}
	}
	// where they are used (stored templates, also those in the trash)
	used, err := p.host.DB.Query(ctx, `SELECT id, design->'tokens'->>'font', design->'tokens'->>'heading_font' FROM plugin_templates WHERE design->'tokens'->>'font' LIKE 'own-%' OR design->'tokens'->>'heading_font' LIKE 'own-%'`)
	if err != nil {
		return nil, err
	}
	defer used.Close()
	for used.Next() {
		var id int64
		var a, b string
		if err := used.Scan(&id, &a, &b); err != nil {
			return nil, err
		}
		for i := range out {
			if (out[i].Token == a || out[i].Token == b) && !slices.Contains(out[i].UsedBy, fmt.Sprintf("%s/%d", ID, id)) {
				out[i].UsedBy = append(out[i].UsedBy, fmt.Sprintf("%s/%d", ID, id))
			}
		}
	}
	if err := used.Err(); err != nil {
		return nil, err
	}
	// and the settings of templates that choose them (Templates → Settings)
	for tpl, m := range p.host.Config.Get().TemplateSettings {
		for _, v := range m {
			for i := range out {
				if v == out[i].Token && !slices.Contains(out[i].UsedBy, tpl) {
					out[i].UsedBy = append(out[i].UsedBy, tpl)
				}
			}
		}
	}
	for i := range out {
		slices.Sort(out[i].UsedBy)
	}
	return out, nil
}

type checkedFace struct {
	FontFace
	data []byte
}

func (p *Plugin) addFont(ctx context.Context, in fontInput) (*Font, error) {
	name := strings.TrimSpace(in.Name)
	if !fontNameRe.MatchString(name) {
		return nil, apperr.BadRequest("a font needs a name (letters, digits, space, . _ -; at most 60)")
	}
	if !slices.Contains(FontLicenses, in.License) {
		return nil, apperr.BadRequest("license: one of %v", FontLicenses)
	}
	note := strings.TrimSpace(in.LicenseNote)
	if utf8.RuneCountInString(note) > 500 || strings.ContainsAny(note, "<>") {
		return nil, apperr.BadRequest("the note on the license has at most 500 characters, without < and >")
	}
	if in.License == "other" && note == "" {
		return nil, apperr.BadRequest("for another license, name it and where the right to use the font on the web comes from (license_note)")
	}
	if !in.Confirm {
		return nil, apperr.BadRequest("confirm that this font may be used on this website (confirm)")
	}
	if len(in.Files) == 0 || len(in.Files) > MaxFontFaces {
		return nil, apperr.BadRequest("a font has 1 to %d files (files)", MaxFontFaces)
	}
	max := p.host.Limit(limitFontKB) << 10
	var faces []checkedFace
	for i, f := range in.Files {
		where := fmt.Sprintf("file %d", i+1)
		if int64(base64.StdEncoding.DecodedLen(len(f.Data))) > max+3 {
			return nil, apperr.BadRequest("%s: a font file has at most %d KB (expert setting %s)", where, max>>10, limitFontKB)
		}
		data, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			return nil, apperr.BadRequest("%s: the file as base64 (data)", where)
		}
		if int64(len(data)) > max {
			return nil, apperr.BadRequest("%s: a font file has at most %d KB (expert setting %s)", where, max>>10, limitFontKB)
		}
		format, err := fontFormat(data)
		if err != nil {
			return nil, apperr.BadRequest("%s: %v", where, err)
		}
		weight, err := checkWeight(f.Weight)
		if err != nil {
			return nil, apperr.BadRequest("%s: %v", where, err)
		}
		style := strings.TrimSpace(f.Style)
		if style == "" {
			style = "normal"
		}
		if style != "normal" && style != "italic" {
			return nil, apperr.BadRequest("%s: style normal or italic", where)
		}
		for _, o := range faces {
			if o.Weight == weight && o.Style == style {
				return nil, apperr.BadRequest("%s: two files for the same weight and style (%s %s)", where, weight, style)
			}
		}
		sum := sha256.Sum256(data)
		faces = append(faces, checkedFace{FontFace{N: i + 1, Weight: weight, Style: style, Format: format, Bytes: len(data), SHA256: hex.EncodeToString(sum[:])}, data})
	}
	me := identity.From(ctx).ID()
	var id int64
	err := pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		// one at a time, so the count holds
		if _, err := tx.Exec(ctx, `LOCK TABLE plugin_template_fonts IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return err
		}
		var n int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM plugin_template_fonts`).Scan(&n); err != nil {
			return err
		}
		if lim := p.host.Limit(limitFontCount); n >= lim {
			return apperr.BadRequest("at most %d uploaded fonts (expert setting %s)", lim, limitFontCount)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO plugin_template_fonts(name, license, license_note, created_by) VALUES ($1,$2,$3,$4) RETURNING id`,
			name, in.License, note, me).Scan(&id); err != nil {
			return err
		}
		logged := make([]FontFace, 0, len(faces))
		for _, f := range faces {
			if _, err := tx.Exec(ctx, `INSERT INTO plugin_template_font_files(font_id, n, weight, style, format, data, sha256) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
				id, f.N, f.Weight, f.Style, f.Format, f.data, f.SHA256); err != nil {
				return err
			}
			logged = append(logged, f.FontFace)
		}
		return audit.Log(ctx, tx, "core.template.font.add", "template_font", id, nil, map[string]any{"name": name, "license": in.License, "license_note": note, "files": logged, "confirmed": true})
	})
	if err != nil {
		return nil, err
	}
	all, err := p.fonts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, apperr.NotFound("font")
}

// fontPublic: a font is public while a template outside the trash uses it
// (like the designs) or the settings of a template choose it; designers
// see every font.
func (p *Plugin) fontPublic(ctx context.Context, id int64) (bool, error) {
	if perm.Has(ctx, permDesign) {
		return true, nil
	}
	token := "own-" + strconv.FormatInt(id, 10)
	// a font chosen in the settings of a template (Administration →
	// Templates → Settings) is shown to every visitor too
	if p.host.Config != nil {
		for _, m := range p.host.Config.Get().TemplateSettings {
			for _, v := range m {
				if v == token {
					return true, nil
				}
			}
		}
	}
	var public bool
	err := p.host.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM plugin_templates WHERE deleted_at IS NULL AND (design->'tokens'->>'font' = $1 OR design->'tokens'->>'heading_font' = $1))`, token).Scan(&public)
	return public, err
}

// fontCSS writes the @font-face rules of a font (from checked values only).
func fontCSS(id int64, faces []FontFace) string {
	var b strings.Builder
	for _, f := range faces {
		fmt.Fprintf(&b, "@font-face { font-family: \"ewd-own-%d\"; src: url(\"/api/v1/x/core.template/fonts/%d/files/%d\") format(\"%s\"); font-weight: %s; font-style: %s; font-display: swap; }\n",
			id, id, f.N, f.Format, f.Weight, f.Style)
	}
	return b.String()
}

func (p *Plugin) setupFonts(host *coreplugin.Host) {
	design := func(r *http.Request) error { return perm.Require(r.Context(), permDesign) }
	host.BodyLimit("POST", "/fonts", limitFontBody)

	// GET /fonts → the uploaded fonts (without files) and where they are used
	// action: core.template.fonts
	host.Handle("GET", "/fonts", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		return p.fonts(r.Context())
	})
	// POST /fonts {name, license, license_note, confirm, files: [{data, weight, style}]} → upload a font
	// action: core.template.font.add
	host.Handle("POST", "/fonts", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		var in fontInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		f, err := p.addFont(r.Context(), in)
		if err != nil {
			return nil, err
		}
		return h.Created{V: f}, nil
	})
	// DELETE /fonts/{id} → remove a font no template uses
	// action: core.template.font.delete
	host.Handle("DELETE", "/fonts/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		token := "own-" + strconv.FormatInt(id, 10)
		err = pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
			var name string
			if err := tx.QueryRow(ctx, `SELECT name FROM plugin_template_fonts WHERE id=$1 FOR UPDATE`, id).Scan(&name); err != nil {
				if err == pgx.ErrNoRows {
					return apperr.NotFound("font")
				}
				return err
			}
			// templates are locked against a parallel save that starts to use it
			rows, err := tx.Query(ctx, `SELECT id FROM plugin_templates WHERE design->'tokens'->>'font' = $1 OR design->'tokens'->>'heading_font' = $1 FOR SHARE`, token)
			if err != nil {
				return err
			}
			used, err := pgx.CollectRows(rows, pgx.RowTo[int64])
			if err != nil {
				return err
			}
			names := make([]string, 0, len(used))
			for _, u := range used {
				names = append(names, fmt.Sprintf("%s/%d", ID, u))
			}
			// the settings of a template may choose it too (Templates → Settings)
			for tpl, m := range host.Config.Get().TemplateSettings {
				for _, v := range m {
					if v == token && !slices.Contains(names, tpl) {
						names = append(names, tpl)
					}
				}
			}
			if len(names) > 0 {
				slices.Sort(names)
				return apperr.Conflict("the font is used by %s (also templates in the trash); choose another font there first", strings.Join(names, ", "))
			}
			if _, err := tx.Exec(ctx, `DELETE FROM plugin_template_fonts WHERE id=$1`, id); err != nil {
				return err
			}
			return audit.Log(ctx, tx, "core.template.font.delete", "template_font", id, map[string]any{"name": name}, nil)
		})
		if err != nil {
			return nil, err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil, nil
	})
	// GET /fonts/{id}/css → the @font-face rules of a font (the engine imports
	// them; public while a template outside the trash has it, like the designs;
	// for designers always)
	// action: core.template.font.css
	host.Handle("GET", "/fonts/{id}/css", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		if ok, err := p.fontPublic(ctx, id); err != nil {
			return nil, err
		} else if !ok {
			return nil, apperr.NotFound("font")
		}
		faces, err := p.faces(ctx, []int64{id})
		if err != nil {
			return nil, err
		}
		if len(faces[id]) == 0 {
			return nil, apperr.NotFound("font")
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write([]byte(fontCSS(id, faces[id])))
		return nil, nil
	})
	// GET /fonts/{id}/files/{n} → one file of a font, unchanged (public like its CSS)
	// action: core.template.font.file
	host.Handle("GET", "/fonts/{id}/files/{n}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil || n < 1 || n > MaxFontFaces {
			return nil, apperr.NotFound("font file")
		}
		ctx := r.Context()
		if ok, err := p.fontPublic(ctx, id); err != nil {
			return nil, err
		} else if !ok {
			return nil, apperr.NotFound("font")
		}
		var data []byte
		var sum, format string
		if err := host.DB.QueryRow(ctx, `SELECT data, sha256, format FROM plugin_template_font_files WHERE font_id=$1 AND n=$2`, id, n).Scan(&data, &sum, &format); err != nil {
			if err == pgx.ErrNoRows {
				return nil, apperr.NotFound("font file")
			}
			return nil, err
		}
		// a file never changes (a new upload is a new font); cached a day, as it
		// may stop being public
		w.Header().Set("Content-Type", FontFormats[format])
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("ETag", `"`+sum+`"`)
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		if r.Header.Get("If-None-Match") == `"`+sum+`"` {
			w.WriteHeader(http.StatusNotModified)
			return nil, nil
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
		return nil, nil
	})
}
