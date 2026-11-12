package content_forms

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

// resultsMacro (::form-results{form=ID}) shows the submissions of a form:
// "counts" – how often each choice was taken, as a table with bars (for
// everyone who sees the form when the form allows public results, else
// for those who see the submissions); "table" – the submissions themselves
// (only for those who see the submissions).
type resultsMacro struct{ p *Plugin }

func lm(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }

func (resultsMacro) Name() string { return "form-results" }
func (resultsMacro) Labels() map[string]string {
	return lm("Form results", "Formular-Ergebnisse")
}
func (resultsMacro) Description() map[string]string {
	return lm("Counts of the choices of a form (e.g. a poll) or its submissions as a table – only for readers with the right to see them.",
		"Häufigkeit der Antworten eines Formulars (z. B. einer Umfrage) oder seine Einsendungen als Tabelle – nur für Lesende mit dem Recht dazu.")
}
func (resultsMacro) HasBody() bool   { return false }
func (resultsMacro) Toolbar() string { return "📊" }

func (resultsMacro) Params() []content.MacroParam {
	return append([]content.MacroParam{
		{Name: "form", Labels: lm("Form", "Formular"), Type: "custom:forms.form", Required: true},
		{Name: "show", Labels: lm("Show", "Zeigen"), Type: "select", Options: []string{"counts", "table"}, Default: "counts",
			OptionLabels: map[string]map[string]string{"counts": lm("Counts of the answers", "Häufigkeit der Antworten"), "table": lm("Submissions as table", "Einsendungen als Tabelle")}},
		{Name: "fields", Labels: lm("Fields (IDs, comma separated; empty: all)", "Felder (IDs durch Komma getrennt; leer: alle)"), Type: "text"},
		{Name: "title", Labels: lm("Heading", "Überschrift"), Type: "text"},
	}, content.ListParams(50)...)
}

func (m resultsMacro) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	p := m.p
	id, _ := strconv.ParseInt(strings.TrimSpace(mc.Params["form"]), 10, 64)
	tr := func(k string) string { return p.host.T(ctx, mc.Lang, k, nil) }
	note := func(k string) string { return `<p class="ew-muted" role="note">` + esc(tr(k)) + `</p>` }
	if id <= 0 {
		return note("forms.results.choose"), nil
	}
	f, _, _, title, err := p.load(ctx, id, mc.Lang)
	if err != nil {
		return note("forms.results.none"), nil // not visible: nothing is told
	}
	_, seeErr := p.canSee(ctx, id)
	var want []string
	for _, s := range strings.Split(mc.Params["fields"], ",") {
		if s = strings.TrimSpace(s); s != "" {
			want = append(want, s)
		}
	}
	var b strings.Builder
	heading := mc.Params["title"]
	if heading == "" {
		heading = title
	}
	b.WriteString(`<section class="ewfm-results">`)
	fmt.Fprintf(&b, `<h2>%s</h2>`, esc(heading))
	if mc.Params["show"] == "table" {
		if seeErr != nil {
			return note("forms.results.no_right"), nil
		}
		return p.resultsTable(ctx, mc, f, id, want, &b)
	}
	if seeErr != nil && !f.PublicResults {
		return note("forms.results.no_right"), nil
	}
	var total int
	if err := p.host.DB.QueryRow(ctx, `SELECT count(*) FROM plugin_form_submissions WHERE element_id=$1`, id).Scan(&total); err != nil {
		return "", err
	}
	fmt.Fprintf(&b, `<p>%s</p>`, esc(strings.ReplaceAll(tr("forms.results.total"), "{n}", strconv.Itoa(total))))
	for _, x := range f.Fields {
		countable := slices.Contains([]string{"select", "multiselect", "radio", "checkboxes", "rating", "checkbox", "consent"}, x.Type)
		if !countable || len(want) > 0 && !slices.Contains(want, x.ID) {
			continue
		}
		counts := map[string]int{}
		rows, err := p.host.DB.Query(ctx, `SELECT v, count(*) FROM plugin_form_submissions s,
			LATERAL jsonb_array_elements_text(CASE WHEN jsonb_typeof(s.data->$2) = 'array' THEN s.data->$2 WHEN s.data ? $2 THEN jsonb_build_array(s.data->$2) ELSE '[]'::jsonb END) v
			WHERE s.element_id=$1 GROUP BY v`, id, x.ID)
		if err != nil {
			return "", err
		}
		err = func() error {
			defer rows.Close()
			for rows.Next() {
				var v string
				var n int
				if err := rows.Scan(&v, &n); err != nil {
					return err
				}
				counts[v] = n
			}
			return rows.Err()
		}()
		if err != nil {
			return "", err
		}
		type opt struct{ value, label string }
		var opts []opt
		switch x.Type {
		case "rating":
			n := 5
			if x.Max != nil && *x.Max >= 2 && *x.Max <= 10 {
				n = int(*x.Max)
			}
			for i := n; i >= 1; i-- {
				opts = append(opts, opt{strconv.Itoa(i), strings.Repeat("★", i)})
			}
		case "checkbox", "consent":
			opts = []opt{{"true", tr("forms.results.yes")}, {"false", tr("forms.results.no")}}
		default:
			for _, o := range x.Options {
				opts = append(opts, opt{o.Value, o.Label})
			}
		}
		fmt.Fprintf(&b, `<table class="ew-table ewfm-counts"><caption>%s</caption><thead><tr><th scope="col">%s</th><th scope="col">%s</th><th scope="col">%s</th></tr></thead><tbody>`,
			esc(x.Label), esc(tr("forms.results.answer")), esc(tr("forms.results.count")), esc(tr("forms.results.share")))
		for _, o := range opts {
			n := counts[o.value]
			pct := 0
			if total > 0 {
				pct = n * 100 / total
			}
			fmt.Fprintf(&b, `<tr><th scope="row">%s</th><td>%d</td><td><span class="ewfm-bar" aria-hidden="true" style="--ewfm-pct:%d%%"></span> %d %%</td></tr>`,
				esc(o.label), n, pct, pct)
		}
		b.WriteString(`</tbody></table>`)
	}
	b.WriteString(`</section>`)
	return b.String(), nil
}

// resultsTable lists the submissions (newest first) with paging.
func (p *Plugin) resultsTable(ctx context.Context, mc content.MacroContext, f *Form, id int64, want []string, b *strings.Builder) (string, error) {
	pg := mc.Paging(50)
	var total int
	if err := p.host.DB.QueryRow(ctx, `SELECT count(*) FROM plugin_form_submissions WHERE element_id=$1`, id).Scan(&total); err != nil {
		return "", err
	}
	rows, err := p.host.DB.Query(ctx, `SELECT `+subCols+` FROM plugin_form_submissions WHERE element_id=$1 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		id, pg.Size(), pg.Offset())
	if err != nil {
		return "", err
	}
	list, err := pgx.CollectRows(rows, scanSubmission)
	if err != nil {
		return "", err
	}
	var cols []*Field
	for i := range f.Fields {
		x := &f.Fields[i]
		if x.Input() && x.Type != "hidden" && (len(want) == 0 || slices.Contains(want, x.ID)) {
			cols = append(cols, x)
		}
	}
	tr := func(k string) string { return p.host.T(ctx, mc.Lang, k, nil) }
	fmt.Fprintf(b, `<div class="ew-table-wrap" tabindex="0" role="region" aria-label="%s"><table class="ew-table"><thead><tr><th scope="col">%s</th>`,
		esc(tr("forms.submissions")), esc(tr("forms.submitted_at")))
	for _, c := range cols {
		fmt.Fprintf(b, `<th scope="col">%s</th>`, esc(c.Label))
	}
	b.WriteString(`</tr></thead><tbody>`)
	for _, s := range list {
		fmt.Fprintf(b, `<tr><th scope="row"><time datetime="%s">%s</time></th>`, s.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), s.CreatedAt.Format("02.01.2006 15:04"))
		for _, c := range cols {
			text := f.Text(c.ID, s.Data[c.ID])
			if c.Type == "file" || c.Type == "signature" {
				var names []string
				if text != "" {
					names = append(names, text)
				}
				for _, fl := range s.Files {
					if fl.Field == c.ID {
						names = append(names, fl.Name)
					}
				}
				text = strings.Join(names, ", ")
			}
			fmt.Fprintf(b, `<td>%s</td>`, esc(text))
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table></div>`)
	b.WriteString(mc.Pager(pg, total))
	b.WriteString(`</section>`)
	return b.String(), nil
}
