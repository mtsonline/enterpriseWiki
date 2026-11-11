package core_properties

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

// The macro "properties" (property report). Parameters are short texts, so
// the Markdown stays readable, e.g.
//
//	::properties{source="category:12+sub" filter="status in open,review; due <= today+30d; owner = me"
//	  columns="title,status,owner,due" sort="due" view="table"}
//
// The server renders an accessible table (also without JavaScript, with the
// rights of the reader); the browser turns it into the chosen view.

// Views of a report.
var Views = []string{"table", "list", "cards", "kanban", "calendar", "timeline", "chart", "kpi", "pivot", "tagcloud", "completeness", "overdue", "map"}

// Charts of the view "chart".
var Charts = []string{"bar", "hbar", "stacked", "line", "area", "pie", "donut", "heatmap"}

type report struct{ p *Plugin }

func (report) Name() string { return "properties" }
func (report) Labels() map[string]string {
	return lm("Property report", "Eigenschaften-Bericht")
}
func (report) Description() map[string]string {
	return lm("Elements with their properties from several sources: filtered, sorted and grouped as a table, cards, board, calendar, timeline, chart, key figures or pivot table.",
		"Elemente mit ihren Eigenschaften aus mehreren Quellen: gefiltert, sortiert und gruppiert als Tabelle, Karten, Board, Kalender, Zeitleiste, Diagramm, Kennzahlen oder Pivot-Tabelle.")
}
func (report) HasBody() bool   { return false }
func (report) Toolbar() string { return "▦" }
func (report) Params() []content.MacroParam {
	opt := func(list []string) []string { return append([]string{}, list...) }
	return []content.MacroParam{
		{Name: "view", Labels: lm("View", "Darstellung"), Type: "select", Options: opt(Views), Default: "table"},
		{Name: "source", Labels: lm("Sources (empty: this category with subcategories)", "Quellen (leer: diese Kategorie mit Unterkategorien)"), Type: "custom:properties.source"},
		{Name: "filter", Labels: lm("Filters", "Filter"), Type: "custom:properties.filter"},
		{Name: "columns", Labels: lm("Columns", "Spalten"), Type: "custom:properties.columns"},
		{Name: "sort", Labels: lm("Sort by (- before a key: descending)", "Sortieren nach (- vor dem Schlüssel: absteigend)"), Type: "custom:properties.sort"},
		{Name: "group", Labels: lm("Group by (board columns, chart categories, pivot rows)", "Gruppieren nach (Board-Spalten, Diagramm-Kategorien, Pivot-Zeilen)"), Type: "custom:properties.key"},
		{Name: "group2", Labels: lm("Second grouping (series of stacked charts, pivot columns)", "Zweite Gruppierung (Reihen gestapelter Diagramme, Pivot-Spalten)"), Type: "custom:properties.key"},
		{Name: "bucket", Labels: lm("Dates grouped by", "Datumswerte zusammenfassen nach"), Type: "select", Options: []string{"day", "week", "month", "quarter", "year"}, Default: "month"},
		{Name: "agg", Labels: lm("Value", "Wert"), Type: "select", Options: []string{"count", "sum", "avg", "min", "max"}, Default: "count"},
		{Name: "value", Labels: lm("Property for sum/average (number)", "Eigenschaft für Summe/Mittelwert (Zahl)"), Type: "custom:properties.key"},
		{Name: "chart", Labels: lm("Chart", "Diagramm"), Type: "select", Options: opt(Charts), Default: "bar"},
		{Name: "date", Labels: lm("Date property (calendar, timeline, overdue)", "Datums-Eigenschaft (Kalender, Zeitleiste, überfällig)"), Type: "custom:properties.key"},
		{Name: "live_filters", Labels: lm("Filters for readers (properties)", "Filter für Lesende (Eigenschaften)"), Type: "custom:properties.columns"},
		{Name: "totals", Labels: lm("Show totals", "Summen anzeigen"), Type: "bool"},
		{Name: "export", Labels: lm("Offer CSV download", "CSV-Download anbieten"), Type: "bool", Default: "true"},
		{Name: "limit", Labels: lm("Maximum number of elements", "Höchstzahl der Elemente"), Type: "number", Default: "100"},
		{Name: "title", Labels: lm("Heading (also the name for screen readers)", "Überschrift (auch der Name für Screenreader)"), Type: "text"},
	}
}

// ParseSources reads "category:12+sub; category:7; tag:hr; type:core.html; elements:1,2".
func ParseSources(s string) ([]Source, error) {
	var out []Source
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kind, val, ok := strings.Cut(part, ":")
		if !ok {
			return nil, apperr.BadRequest("source %q: kind:value", part)
		}
		kind, val = strings.TrimSpace(kind), strings.TrimSpace(val)
		switch kind {
		case "category":
			sub := strings.HasSuffix(val, "+sub")
			id, err := strconv.ParseInt(strings.TrimSuffix(val, "+sub"), 10, 64)
			if err != nil || id <= 0 {
				return nil, apperr.BadRequest("source %q: category id", part)
			}
			out = append(out, Source{Kind: "category", ID: id, Sub: sub})
		case "tag":
			out = append(out, Source{Kind: "tag", Tag: val})
		case "type":
			out = append(out, Source{Kind: "type", Type: val})
		case "elements":
			var ids []int64
			for _, x := range strings.Split(val, ",") {
				id, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
				if err != nil || id <= 0 {
					return nil, apperr.BadRequest("source %q: element ids", part)
				}
				ids = append(ids, id)
			}
			out = append(out, Source{Kind: "elements", IDs: ids})
		default:
			return nil, apperr.BadRequest("unknown source %q", kind)
		}
		if len(out) > 50 {
			return nil, apperr.BadRequest("at most 50 sources")
		}
	}
	return out, nil
}

// filter operators in the order they are looked for (longer first)
var filterOps = []struct{ tok, op string }{
	{" notempty ", "notempty"}, {" empty ", "empty"}, {" not in ", "nin"}, {" in ", "in"}, {" hasall ", "hasall"}, {" hasnot ", "hasnot"}, {" has ", "has"},
	{" contains ", "contains"}, {" starts ", "starts"}, {" between ", "between"},
	{"!=", "ne"}, {"<=", "le"}, {">=", "ge"}, {"=", "eq"}, {"<", "lt"}, {">", "gt"},
}

func literal(s string) json.RawMessage {
	s = strings.TrimSpace(s)
	switch s {
	case "true", "false":
		return json.RawMessage(s)
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return mustJSON(f)
	}
	return mustJSON(s)
}

// ParseFilters reads "status in open,review; due <= today+30d; owner = me;
// budget between 100..500; notes empty".
func ParseFilters(s string) ([]Filter, error) {
	var out []Filter
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// the operator that comes first (the longer one at the same place)
		padded := " " + part + " "
		best, at := -1, -1
		for i, o := range filterOps {
			j := strings.Index(padded, o.tok)
			if j > 0 && (at < 0 || j < at || j == at && len(o.tok) > len(filterOps[best].tok)) {
				best, at = i, j
			}
		}
		found := best >= 0
		if found {
			o := filterOps[best]
			key := strings.TrimSpace(padded[:at])
			val := strings.TrimSpace(padded[at+len(o.tok):])
			f := Filter{Key: strings.ToLower(key), Op: o.op}
			switch o.op {
			case "empty", "notempty":
			case "in", "nin", "has", "hasall", "hasnot":
				vals := []json.RawMessage{}
				for _, v := range strings.Split(val, ",") {
					if v = strings.TrimSpace(v); v != "" {
						vals = append(vals, literal(v))
					}
				}
				f.Value = mustJSON(vals)
			case "between":
				a, z, ok := strings.Cut(val, "..")
				if !ok {
					return nil, apperr.BadRequest("filter %q: between a..b", part)
				}
				f.Value = mustJSON([]json.RawMessage{literal(a), literal(z)})
			default:
				if strings.EqualFold(val, "me") && o.op == "eq" {
					f.Op = "me"
				} else {
					f.Value = literal(val)
				}
			}
			out = append(out, f)
		}
		if !found {
			return nil, apperr.BadRequest("filter %q: use key = value, key in a,b, key empty …", part)
		}
		if len(out) > 30 {
			return nil, apperr.BadRequest("at most 30 filters")
		}
	}
	return out, nil
}

func splitKeys(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.ToLower(strings.TrimSpace(x)); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// QueryOf builds the query of a report from its parameters.
func QueryOf(params map[string]string, categoryID, elementID int64, lang string) (*Query, error) {
	q := &Query{Lang: lang, Exclude: elementID}
	var err error
	if q.Sources, err = ParseSources(params["source"]); err != nil {
		return nil, err
	}
	if len(q.Sources) == 0 && categoryID > 0 {
		q.Sources = []Source{{Kind: "category", ID: categoryID, Sub: true}}
	}
	if q.Filters, err = ParseFilters(params["filter"]); err != nil {
		return nil, err
	}
	q.Columns = splitKeys(params["columns"])
	for _, k := range splitKeys(params["sort"]) {
		q.Sort = append(q.Sort, SortKey{Key: strings.TrimPrefix(k, "-"), Desc: strings.HasPrefix(k, "-")})
	}
	q.Limit, _ = strconv.Atoi(params["limit"])
	if q.Limit <= 0 || q.Limit > maxRows {
		q.Limit = 100
	}
	switch params["view"] {
	case "chart", "kpi", "pivot", "tagcloud":
		q.Group, q.Group2 = strings.ToLower(params["group"]), strings.ToLower(params["group2"])
		if params["view"] == "tagcloud" && q.Group == "" {
			q.Group = "tags"
		}
		if params["view"] != "pivot" && params["chart"] != "stacked" && params["chart"] != "heatmap" {
			q.Group2 = ""
		}
		q.Bucket, q.Agg, q.AggKey = params["bucket"], params["agg"], strings.ToLower(params["value"])
		if q.Group == "" && params["view"] == "kpi" {
			q.Group = "type" // a single figure: everything in groups of the type, summed up by the browser
		}
		if q.Group == "" {
			return nil, apperr.BadRequest("choose a property to group by")
		}
	}
	return q, nil
}

func (r report) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	cfg, _ := json.Marshal(mc.Params)
	lang := mc.Lang
	title := strings.TrimSpace(mc.Params["title"])
	if title == "" {
		title = pick(lm("Property report", "Eigenschaften-Bericht"), lang)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<section class="ewprop-report" data-ew-widget="core.properties.report" data-config="%s" data-category="%d" data-element="%d" aria-label="%s">`,
		html.EscapeString(string(cfg)), mc.CategoryID, mc.ElementID, html.EscapeString(title))
	if t := strings.TrimSpace(mc.Params["title"]); t != "" {
		fmt.Fprintf(&b, `<h2 class="ewprop-report-title">%s</h2>`, html.EscapeString(t))
	}
	q, err := QueryOf(mc.Params, mc.CategoryID, mc.ElementID, lang)
	if err != nil {
		fmt.Fprintf(&b, `<p class="ew-notice ew-notice--warning" role="note">%s</p></section>`, html.EscapeString(err.Error()))
		return b.String(), nil
	}
	res, err := r.p.Run(ctx, q)
	if err != nil {
		var ae *apperr.Error
		msg := pick(lm("The report could not be created.", "Der Bericht konnte nicht erstellt werden."), lang)
		if asAppErr(err, &ae) {
			msg = ae.Msg
		}
		fmt.Fprintf(&b, `<p class="ew-notice ew-notice--warning" role="note">%s</p></section>`, html.EscapeString(msg))
		return b.String(), nil
	}
	b.WriteString(`<div class="ewprop-fallback">`)
	if q.Group != "" {
		r.groupTable(&b, q, res, lang)
	} else {
		r.rowTable(&b, q, res, lang)
	}
	b.WriteString(`</div></section>`)
	return b.String(), nil
}

func asAppErr(err error, target **apperr.Error) bool {
	e, ok := err.(*apperr.Error)
	if ok {
		*target = e
	}
	return ok
}

func colLabel(c Column, lang string) string {
	if c.Builtin || c.Label == "" {
		switch c.Key {
		case "title":
			return pick(lm("Title", "Titel"), lang)
		case "tags":
			return pick(lm("Tags", "Schlagwörter"), lang)
		case "type":
			return pick(lm("Type", "Typ"), lang)
		case "category":
			return pick(lm("Category", "Kategorie"), lang)
		case "created_at":
			return pick(lm("Created", "Erstellt"), lang)
		case "updated_at":
			return pick(lm("Changed", "Geändert"), lang)
		case "created_by":
			return pick(lm("Created by", "Erstellt von"), lang)
		case "updated_by":
			return pick(lm("Changed by", "Geändert von"), lang)
		}
		return c.Key
	}
	return c.Label
}

func (r report) rowTable(b *strings.Builder, q *Query, res *Result, lang string) {
	if len(res.Rows) == 0 {
		fmt.Fprintf(b, `<p class="ew-muted">%s</p>`, html.EscapeString(pick(lm("No elements match.", "Keine passenden Elemente."), lang)))
		return
	}
	cols := []Column{}
	for _, c := range res.Columns {
		if c.Key != "title" {
			cols = append(cols, c)
		}
	}
	fmt.Fprintf(b, `<table class="ew-table"><thead><tr><th scope="col">%s</th>`, html.EscapeString(pick(lm("Element", "Element"), lang)))
	for _, c := range cols {
		fmt.Fprintf(b, `<th scope="col">%s</th>`, html.EscapeString(colLabel(c, lang)))
	}
	b.WriteString(`</tr></thead><tbody>`)
	for _, row := range res.Rows {
		fmt.Fprintf(b, `<tr><th scope="row"><a href="%s">%s</a></th>`, html.EscapeString(row.Link), html.EscapeString(row.Title))
		for _, c := range cols {
			fmt.Fprintf(b, `<td>%s</td>`, html.EscapeString(plain(builtinValue(row, c.Key), c.Type, res.Refs)))
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table>`)
	if res.Total > len(res.Rows) {
		fmt.Fprintf(b, `<p class="ew-muted">%s</p>`, html.EscapeString(fmt.Sprintf(pick(lm("%d of %d elements shown.", "%d von %d Elementen angezeigt."), lang), len(res.Rows), res.Total)))
	}
}

func (r report) groupTable(b *strings.Builder, q *Query, res *Result, lang string) {
	if len(res.Groups) == 0 {
		fmt.Fprintf(b, `<p class="ew-muted">%s</p>`, html.EscapeString(pick(lm("No elements match.", "Keine passenden Elemente."), lang)))
		return
	}
	label := func(k string) string {
		for _, c := range res.Columns {
			if c.Key == k {
				return colLabel(c, lang)
			}
		}
		return k
	}
	typeOf := func(k string) string {
		for _, c := range res.Columns {
			if c.Key == k {
				return c.Type
			}
		}
		return "text"
	}
	val := pick(lm("Number of elements", "Anzahl Elemente"), lang)
	if q.Agg != "" && q.Agg != "count" {
		val = q.Agg + "(" + label(q.AggKey) + ")"
	}
	fmt.Fprintf(b, `<table class="ew-table"><thead><tr><th scope="col">%s</th>`, html.EscapeString(label(q.Group)))
	if q.Group2 != "" {
		fmt.Fprintf(b, `<th scope="col">%s</th>`, html.EscapeString(label(q.Group2)))
	}
	fmt.Fprintf(b, `<th scope="col">%s</th></tr></thead><tbody>`, html.EscapeString(val))
	empty := pick(lm("(empty)", "(leer)"), lang)
	show := func(raw json.RawMessage, typ string) string {
		if t, ok := TypeByID(typ); ok && t.Storage == StoreRefs {
			if x, err := strconv.ParseInt(string(raw), 10, 64); err == nil {
				raw = mustJSON([]int64{x})
			}
		}
		if s := plain(raw, typ, res.Refs); s != "" {
			return s
		}
		return empty
	}
	for _, g := range res.Groups {
		fmt.Fprintf(b, `<tr><th scope="row">%s</th>`, html.EscapeString(show(g.Key, typeOf(q.Group))))
		if q.Group2 != "" {
			fmt.Fprintf(b, `<td>%s</td>`, html.EscapeString(show(g.Key2, typeOf(q.Group2))))
		}
		fmt.Fprintf(b, `<td>%s</td></tr>`, html.EscapeString(strconv.FormatFloat(g.Value, 'f', -1, 64)))
	}
	b.WriteString(`</tbody></table>`)
}
