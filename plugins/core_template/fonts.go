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

// Uploaded fonts: one WOFF2 file per font (best a variable font), stored in
// the plugin's own table and served unchanged by the wiki itself (never from
// elsewhere). The person uploading it confirms the right to use it on the
// web and names its license. The file is not converted or subset, so the
// rule of the SIL Open Font License on reserved font names (only for
// modified versions) does not apply.

// FontLicenses are the licenses to choose from; "other" needs a note.
var FontLicenses = []string{"OFL-1.1", "Apache-2.0", "MIT", "other"}

const (
	limitFontKB    = "core.template.font_kb"
	limitFontCount = "core.template.fonts"
	limitFontBody  = "core.template.font_upload_kb"
)

// Font is an uploaded font (without its file).
type Font struct {
	ID          int64     `json:"id"`
	Token       string    `json:"token"` // own-ID, the value in the tokens of a design
	Name        string    `json:"name"`
	License     string    `json:"license"`
	LicenseNote string    `json:"license_note"`
	Bytes       int       `json:"bytes"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   *int64    `json:"created_by"`
	// UsedBy: the stored templates (also in the trash) that use it
	UsedBy []string `json:"used_by"`
}

type fontInput struct {
	Name        string `json:"name"`
	License     string `json:"license"`
	LicenseNote string `json:"license_note"`
	// Confirm: "I may use this font on this website" (needed)
	Confirm bool   `json:"confirm"`
	Data    string `json:"data"` // the WOFF2 file, base64
}

var fontNameRe = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,59}$`)

// checkWOFF2 checks that b is a WOFF2 file (header of the W3C
// recommendation, section 3): signature, a single font of TrueType or CFF
// outlines (no collection), the length in the header is the length of the
// file, and every part named in the header lies inside it. Browsers check
// the font tables themselves before they use a font.
func checkWOFF2(b []byte) error {
	const header = 48
	if len(b) < header {
		return fmt.Errorf("too short for a WOFF2 file")
	}
	be := binary.BigEndian
	if string(b[0:4]) != "wOF2" {
		return fmt.Errorf("not a WOFF2 file (convert the font to WOFF2 first)")
	}
	switch string(b[4:8]) {
	case "\x00\x01\x00\x00", "OTTO", "true":
	case "ttcf":
		return fmt.Errorf("font collections are not supported; upload one font")
	default:
		return fmt.Errorf("unknown kind of font in the WOFF2 file")
	}
	if len(b) > math.MaxUint32 {
		return fmt.Errorf("too large for a WOFF2 file")
	}
	n := uint32(len(b)) // #nosec G115 -- checked just above: at most MaxUint32
	if be.Uint32(b[8:12]) != n {
		return fmt.Errorf("the length in the WOFF2 header does not match the file")
	}
	if be.Uint16(b[12:14]) == 0 || be.Uint16(b[14:16]) != 0 {
		return fmt.Errorf("damaged WOFF2 header")
	}
	if compressed := be.Uint32(b[20:24]); compressed == 0 || compressed > n {
		return fmt.Errorf("damaged WOFF2 header")
	}
	inside := func(off, length uint32) bool {
		return off == 0 && length == 0 || uint64(off) >= header && uint64(off)+uint64(length) <= uint64(n)
	}
	if !inside(be.Uint32(b[28:32]), be.Uint32(b[32:36])) || !inside(be.Uint32(b[40:44]), be.Uint32(b[44:48])) {
		return fmt.Errorf("damaged WOFF2 header")
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

func (p *Plugin) fonts(ctx context.Context) ([]Font, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT id, name, license, license_note, octet_length(data), sha256, created_at, created_by FROM plugin_template_fonts ORDER BY lower(name), id`)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Font, error) {
		var f Font
		err := r.Scan(&f.ID, &f.Name, &f.License, &f.LicenseNote, &f.Bytes, &f.SHA256, &f.CreatedAt, &f.CreatedBy)
		f.Token, f.UsedBy = "own-"+strconv.FormatInt(f.ID, 10), []string{}
		return f, err
	})
	if err != nil {
		return nil, err
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
	return out, used.Err()
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
	max := p.host.Limit(limitFontKB) << 10
	if int64(base64.StdEncoding.DecodedLen(len(in.Data))) > max+3 {
		return nil, apperr.BadRequest("a font file has at most %d KB (expert setting %s)", max>>10, limitFontKB)
	}
	data, err := base64.StdEncoding.DecodeString(in.Data)
	if err != nil {
		return nil, apperr.BadRequest("data: the file as base64")
	}
	if int64(len(data)) > max {
		return nil, apperr.BadRequest("a font file has at most %d KB (expert setting %s)", max>>10, limitFontKB)
	}
	if err := checkWOFF2(data); err != nil {
		return nil, apperr.BadRequest("%v", err)
	}
	sum := sha256.Sum256(data)
	me := identity.From(ctx).ID()
	var id int64
	err = pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
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
		if err := tx.QueryRow(ctx, `INSERT INTO plugin_template_fonts(name, license, license_note, data, sha256, created_by) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			name, in.License, note, data, hex.EncodeToString(sum[:]), me).Scan(&id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "core.template.font.add", "template_font", id, nil, map[string]any{"name": name, "license": in.License, "license_note": note, "bytes": len(data), "sha256": hex.EncodeToString(sum[:]), "confirmed": true})
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
	// POST /fonts {name, license, license_note, confirm, data} → upload a WOFF2 font
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
			if len(used) > 0 {
				names := make([]string, len(used))
				for i, u := range used {
					names[i] = fmt.Sprintf("%s/%d", ID, u)
				}
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
	// GET /fonts/{id}/file → the WOFF2 file (public while a template outside the trash has
	// it, like the designs; for designers always)
	// action: core.template.font.file
	host.Handle("GET", "/fonts/{id}/file", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var data []byte
		var sum string
		// public only while a template that can be used has it; designers see every font
		token := "own-" + strconv.FormatInt(id, 10)
		public := perm.Has(r.Context(), permDesign)
		if !public {
			if err := host.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM plugin_templates WHERE deleted_at IS NULL AND (design->'tokens'->>'font' = $1 OR design->'tokens'->>'heading_font' = $1))`, token).Scan(&public); err != nil {
				return nil, err
			}
		}
		if !public {
			return nil, apperr.NotFound("font")
		}
		if err := host.DB.QueryRow(r.Context(), `SELECT data, sha256 FROM plugin_template_fonts WHERE id=$1`, id).Scan(&data, &sum); err != nil {
			if err == pgx.ErrNoRows {
				return nil, apperr.NotFound("font")
			}
			return nil, err
		}
		// the file of an id never changes (a new upload gets a new id); cached a
		// day, as it may stop being public
		w.Header().Set("Content-Type", "font/woff2")
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
