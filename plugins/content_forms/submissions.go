package content_forms

import (
	"bytes"
	"context"
	"encoding/csv"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/xlsx"
)

// Submission is a stored submission.
type Submission struct {
	ID        int64          `json:"id"`
	ElementID int64          `json:"element_id"`
	Lang      string         `json:"lang"`
	Data      map[string]any `json:"data"`
	Files     []StoredFile   `json:"files"`
	Author    string         `json:"author"`
	UserID    *int64         `json:"user_id"`
	CreatedAt time.Time      `json:"created_at"`
	Seen      bool           `json:"seen"`
	Pending   []string       `json:"pending"`
	LastError string         `json:"last_error,omitempty"`
}

const subCols = `id, element_id, lang, data, files, author, user_id, created_at, seen, pending, last_error`

func scanSubmission(r pgx.CollectableRow) (*Submission, error) {
	s := &Submission{}
	err := r.Scan(&s.ID, &s.ElementID, &s.Lang, &s.Data, &s.Files, &s.Author, &s.UserID, &s.CreatedAt, &s.Seen, &s.Pending, &s.LastError)
	for i := range s.Files {
		s.Files[i].Hash = "" // downloads only through the submission (rights)
	}
	return s, err
}

// canSee reports whether the caller sees the submissions of form element id
// (creator of the form, "See submissions" or forms administrator).
func (p *Plugin) canSee(ctx context.Context, id int64) (int64, error) {
	cat, err := p.host.Content.ElementCategory(ctx, id, acl.View)
	if err != nil {
		return 0, err
	}
	me := identity.From(ctx)
	if me.ID() <= 0 {
		return 0, apperr.ErrUnauthorized
	}
	var creator *int64
	var typ string
	if err := p.host.DB.QueryRow(ctx, `SELECT created_by, type FROM elements WHERE id=$1`, id).Scan(&creator, &typ); err != nil || typ != TypeID {
		return 0, apperr.NotFound("form")
	}
	if creator != nil && *creator == me.ID() || p.host.Can(ctx, cat, permSubmissions) || p.host.Can(ctx, cat, permAdmin) {
		return cat, nil
	}
	return 0, apperr.Forbidden("seeing the submissions needs the permission %q", "See submissions")
}

func (p *Plugin) routes() {
	host := p.host
	host.Handle("GET", "/meta", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]any{"input_types": InputTypes, "static_types": StaticTypes, "cond_ops": CondOps, "file_kinds": FileKinds,
			"autocomplete": AutocompleteValues, "http_allow": len(host.Config.Get().Plugins.Macros.HTTPAllow) > 0,
			"virus_scan": p.clamd() != ""}, nil
	})
	host.Handle("GET", "/elements/{id}/start", p.start)
	host.Handle("POST", "/elements/{id}/submit", p.submit)

	// GET /elements/{id}/submissions?limit=&offset=&q=
	host.Handle("GET", "/elements/{id}/submissions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		if _, err := p.canSee(ctx, id); err != nil {
			return nil, err
		}
		limit := min(max(h.QueryInt(r, "limit", 50), 1), 500)
		offset := max(h.QueryInt(r, "offset", 0), 0)
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
		var total int
		if err := host.DB.QueryRow(ctx, `SELECT count(*) FROM plugin_form_submissions WHERE element_id=$1 AND ($2='' OR data::text ILIKE $3 OR author ILIKE $3)`,
			id, q, like).Scan(&total); err != nil {
			return nil, err
		}
		rows, err := host.DB.Query(ctx, `SELECT `+subCols+` FROM plugin_form_submissions WHERE element_id=$1 AND ($2='' OR data::text ILIKE $3 OR author ILIKE $3)
			ORDER BY created_at DESC, id DESC LIMIT $4 OFFSET $5`, id, q, like, limit, offset)
		if err != nil {
			return nil, err
		}
		list, err := pgx.CollectRows(rows, scanSubmission)
		if err != nil {
			return nil, err
		}
		f, _, _, _, err := p.load(ctx, id, r.URL.Query().Get("lang"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"total": total, "items": list, "fields": f.Fields}, nil
	})

	// GET /submissions/{sid} (marks it as seen)
	host.Handle("GET", "/submissions/{sid}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s, err := p.submission(r)
		if err != nil {
			return nil, err
		}
		if !s.Seen {
			_, _ = host.DB.Exec(r.Context(), `UPDATE plugin_form_submissions SET seen=true WHERE id=$1`, s.ID)
		}
		return s, nil
	})

	// DELETE /submissions/{sid}
	host.Handle("DELETE", "/submissions/{sid}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s, err := p.submission(r)
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		err = pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `DELETE FROM plugin_form_submissions WHERE id=$1`, s.ID); err != nil {
				return err
			}
			return audit.Log(ctx, tx, "form.submission_delete", "form_submission", s.ID, map[string]any{"element_id": s.ElementID}, nil)
		})
		return h.NoContent{}, err
	})

	// GET /submissions/{sid}/files/{n}: an uploaded file (always as download)
	host.Handle("GET", "/submissions/{sid}/files/{n}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s, err := p.submission(r)
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(r.PathValue("n"))
		var files []StoredFile
		if err == nil {
			err = host.DB.QueryRow(r.Context(), `SELECT files FROM plugin_form_submissions WHERE id=$1`, s.ID).Scan(&files)
		}
		if err != nil || n < 0 || n >= len(files) {
			return nil, apperr.NotFound("file")
		}
		fl := files[n]
		f, err := host.Files.Get(r.Context(), fl.Hash)
		if err != nil {
			return nil, apperr.NotFound("file")
		}
		host.Files.Serve(w, r, f, fl.Name, false)
		return h.Handled{}, nil
	})

	// GET /elements/{id}/submissions.csv|.xlsx (values starting with = stay text)
	export := func(format string) func(w http.ResponseWriter, r *http.Request) (any, error) {
		return func(w http.ResponseWriter, r *http.Request) (any, error) {
			id, err := h.PathID(r, "id")
			if err != nil {
				return nil, err
			}
			ctx := r.Context()
			if _, err := p.canSee(ctx, id); err != nil {
				return nil, err
			}
			f, _, _, title, err := p.load(ctx, id, r.URL.Query().Get("lang"))
			if err != nil {
				return nil, err
			}
			rows, err := host.DB.Query(ctx, `SELECT `+subCols+` FROM plugin_form_submissions WHERE element_id=$1 ORDER BY created_at, id LIMIT 100000`, id)
			if err != nil {
				return nil, err
			}
			list, err := pgx.CollectRows(rows, scanSubmission)
			if err != nil {
				return nil, err
			}
			header := []string{"#", host.T(ctx, "", "forms.submitted_at", nil), host.T(ctx, "", "forms.submitted_by", nil)}
			var cols []*Field
			for i := range f.Fields {
				if f.Fields[i].Input() {
					cols = append(cols, &f.Fields[i])
					header = append(header, f.Fields[i].Label)
				}
			}
			var out [][]string
			for _, s := range list {
				row := []string{strconv.FormatInt(s.ID, 10), s.CreatedAt.Format(time.RFC3339), s.Author}
				for _, c := range cols {
					if c.Type == "file" {
						var names []string
						for _, fl := range s.Files {
							if fl.Field == c.ID {
								names = append(names, fl.Name)
							}
						}
						row = append(row, strings.Join(names, ", "))
						continue
					}
					row = append(row, f.Text(c.ID, s.Data[c.ID]))
				}
				out = append(out, row)
			}
			name := safeName(title)
			var buf bytes.Buffer
			if format == "csv" {
				cw := csv.NewWriter(&buf)
				_ = cw.Write(header)
				for _, row := range out {
					for i := range row {
						row[i] = csvSafe(row[i])
					}
					_ = cw.Write(row)
				}
				cw.Flush()
				w.Header().Set("Content-Type", "text/csv; charset=utf-8")
				w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".csv"}))
				_, _ = w.Write(append([]byte("\xef\xbb\xbf"), buf.Bytes()...)) // #nosec G705 -- download, not rendered
				return h.Handled{}, nil
			}
			cells := make([][]xlsx.Cell, len(out))
			for i, row := range out {
				for _, v := range row {
					cells[i] = append(cells[i], xlsx.Text(v)) // text cells: never formulas
				}
			}
			if err := xlsx.Write(&buf, title, header, cells); err != nil {
				return nil, err
			}
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".xlsx"}))
			_, _ = w.Write(buf.Bytes())
			return h.Handled{}, nil
		}
	}
	host.Handle("GET", "/elements/{id}/submissions.csv", export("csv"))
	host.Handle("GET", "/elements/{id}/submissions.xlsx", export("xlsx"))

}

// submission loads a submission the caller may see.
func (p *Plugin) submission(r *http.Request) (*Submission, error) {
	sid, err := h.PathID(r, "sid")
	if err != nil {
		return nil, err
	}
	ctx := r.Context()
	rows, err := p.host.DB.Query(ctx, `SELECT `+subCols+` FROM plugin_form_submissions WHERE id=$1`, sid)
	if err != nil {
		return nil, err
	}
	s, err := pgx.CollectExactlyOneRow(rows, scanSubmission)
	if err != nil {
		return nil, apperr.NotFound("submission")
	}
	if _, err := p.canSee(ctx, s.ElementID); err != nil {
		return nil, apperr.NotFound("submission")
	}
	return s, nil
}

// retention deletes submissions older than the retention of their form.
func (p *Plugin) retention(ctx context.Context) error {
	rows, err := p.host.DB.Query(ctx, `SELECT DISTINCT s.element_id FROM plugin_form_submissions s`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		var body []byte
		if err := p.host.DB.QueryRow(ctx, `SELECT r.body FROM elements e JOIN element_revisions r ON r.element_id=e.id AND r.lang=e.original_lang
			WHERE e.id=$1 ORDER BY r.rev DESC LIMIT 1`, id).Scan(&body); err != nil {
			continue
		}
		f, err := Decode(body)
		if err != nil || f.RetentionDays <= 0 {
			continue
		}
		if _, err := p.host.DB.Exec(ctx, `DELETE FROM plugin_form_submissions WHERE element_id=$1 AND created_at < now() - make_interval(days => $2)`,
			id, f.RetentionDays); err != nil {
			return err
		}
	}
	return nil
}

// csvSafe keeps spreadsheets from running cell text as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, s)
	if s = strings.TrimSpace(s); s == "" {
		s = "form"
	}
	return clean(s, 100)
}
