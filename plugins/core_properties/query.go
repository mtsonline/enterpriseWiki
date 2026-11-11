package core_properties

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/xlsx"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Query selects elements by their properties. Everything is checked
// against the rights of the caller: only elements of categories they may
// view, only values of properties they may see (also in filters, sorting
// and sums, so nothing hidden can be inferred).
type Query struct {
	Sources []Source  `json:"sources"` // empty: all elements the caller may view
	Filters []Filter  `json:"filters"`
	Search  string    `json:"q"` // part of the title
	Sort    []SortKey `json:"sort"`
	Columns []string  `json:"columns"` // keys of the rows (empty: all visible)
	// Groups: group by a key (Group2: a second key for pivot tables),
	// dates in buckets, counting or summing a numeric key.
	Group  string `json:"group"`
	Group2 string `json:"group2"`
	Bucket string `json:"bucket"` // day | week | month | quarter | year
	Agg    string `json:"agg"`    // count | sum | avg | min | max
	AggKey string `json:"agg_key"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
	Lang   string `json:"lang"`
	// Exclude is an element left out (e.g. the page with the report).
	Exclude int64 `json:"exclude"`
}

// Source is where elements come from (several are combined; see content.Source).
type Source = content.Source

// Filter is a condition on a property.
type Filter struct {
	Key   string          `json:"key"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}

// SortKey orders by a property.
type SortKey struct {
	Key  string `json:"key"`
	Desc bool   `json:"desc,omitempty"`
}

// Row is an element with its (visible) values.
type Row struct {
	ID         int64                      `json:"id"`
	Title      string                     `json:"title"`
	Link       string                     `json:"link"`
	Type       string                     `json:"type"`
	CategoryID int64                      `json:"category_id"`
	Tags       []string                   `json:"tags"`
	CreatedAt  time.Time                  `json:"created_at"`
	UpdatedAt  time.Time                  `json:"updated_at"`
	CreatedBy  *int64                     `json:"created_by"`
	UpdatedBy  *int64                     `json:"updated_by"`
	Values     map[string]json.RawMessage `json:"values"`
	// CanEdit: the caller may edit the element (changing values in place)
	CanEdit bool `json:"can_edit"`
}

// Column describes a key of the result.
type Column struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Options *Options `json:"options,omitempty"`
	Builtin bool     `json:"builtin,omitempty"`
}

// Group is one group of a grouped query.
type Group struct {
	Key   json.RawMessage `json:"key"`
	Key2  json.RawMessage `json:"key2,omitempty"`
	Value float64         `json:"value"`
	Count int             `json:"count"`
}

// Result of a query.
type Result struct {
	Rows    []Row          `json:"rows,omitempty"`
	Groups  []Group        `json:"groups,omitempty"`
	Total   int            `json:"total"`
	Columns []Column       `json:"columns"`
	Refs    map[string]Ref `json:"refs"`
}

const (
	maxRows   = 1000
	maxGroups = 500
)

var builtinType = map[string]string{
	"tags": "multiselect", "title": "text", "type": "text", "category": "category",
	"created_at": "datetime", "updated_at": "datetime", "created_by": "user", "updated_by": "user",
}

// qb builds the SQL of a query with numbered arguments.
type qb struct {
	args []any
}

func (b *qb) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

// scope is what a caller may see for a query.
type scope struct {
	cats   []int64           // categories of the sources the caller may view
	defs   map[string][]*Def // definitions by key (all categories)
	vis    []int64           // definitions the caller may see
	hidden map[string]bool   // keys whose every definition is hidden
}

func (p *Plugin) scope(ctx context.Context, q *Query) (*scope, error) {
	pr := identity.From(ctx)
	sc := &scope{defs: map[string][]*Def{}, hidden: map[string]bool{}}
	// categories of the sources (all when none names categories)
	var roots []int64
	sub := map[int64]bool{}
	onlyCats := len(q.Sources) > 0
	for _, s := range q.Sources {
		if s.Kind == "category" {
			roots = append(roots, s.ID)
			sub[s.ID] = s.Sub
		} else {
			onlyCats = false
		}
	}
	var rows pgx.Rows
	var err error
	if onlyCats {
		var subRoots, flat []int64
		for _, r := range roots {
			if sub[r] {
				subRoots = append(subRoots, r)
			} else {
				flat = append(flat, r)
			}
		}
		rows, err = p.host.DB.Query(ctx, `SELECT c.id FROM categories c WHERE c.deleted_at IS NULL AND (c.id = ANY($1)
			OR EXISTS (SELECT 1 FROM categories t WHERE t.id = ANY($2) AND c.path <@ t.path))`, flat, subRoots)
	} else {
		rows, err = p.host.DB.Query(ctx, `SELECT id FROM categories WHERE deleted_at IS NULL`)
	}
	if err != nil {
		return nil, err
	}
	all, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	if sc.cats, err = p.host.ACL.Filter(ctx, pr, all, acl.View); err != nil {
		return nil, err
	}
	// definitions
	drows, err := p.host.DB.Query(ctx, `SELECT `+defCols+` FROM plugin_prop_defs d`)
	if err != nil {
		return nil, err
	}
	defs, err := pgx.CollectRows(drows, scanDef)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range defs {
		d := &defs[i]
		sc.defs[d.Key] = append(sc.defs[d.Key], d)
		if d.canView(pr) {
			sc.vis = append(sc.vis, d.ID)
			seen[d.Key] = true
		}
	}
	for k := range sc.defs {
		if !seen[k] {
			sc.hidden[k] = true
		}
	}
	return sc, nil
}

// visCond restricts values of alias v to those the caller may see.
func (sc *scope) visCond(b *qb, v string) string {
	return "(" + v + ".def_id IS NULL OR " + v + ".def_id = ANY(" + b.arg(sc.vis) + "))"
}

// typeOf returns the type of a key (definitions first, else free values).
func (p *Plugin) typeOf(ctx context.Context, sc *scope, key string) string {
	if t, ok := builtinType[key]; ok {
		return t
	}
	if ds := sc.defs[key]; len(ds) > 0 {
		return ds[0].Type
	}
	var t string
	_ = p.host.DB.QueryRow(ctx, `SELECT type FROM plugin_prop_values WHERE key=$1 AND def_id IS NULL LIMIT 1`, key).Scan(&t)
	if t == "" {
		t = "text"
	}
	return t
}

var relRe = regexp.MustCompile(`^today(?:([+-])(\d{1,4})([dwmy]))?$`)

// relDate reads "today", "today+30d", "today-1m", or a date.
func relDate(s string, now time.Time) (time.Time, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if m := relRe.FindStringSubmatch(s); m != nil {
		d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		if m[1] != "" {
			n, _ := strconv.Atoi(m[2])
			if m[1] == "-" {
				n = -n
			}
			switch m[3] {
			case "d":
				d = d.AddDate(0, 0, n)
			case "w":
				d = d.AddDate(0, 0, 7*n)
			case "m":
				d = d.AddDate(0, n, 0)
			case "y":
				d = d.AddDate(n, 0, 0)
			}
		}
		return d, true
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// cond builds the condition of a filter on a value (alias v) of type t;
// col is the typed column expression.
func cond(b *qb, f Filter, t TypeDef, v string, me int64) (string, error) {
	var s string
	var f64 float64
	var list []string
	// texts may arrive as numbers or yes/no from the short filter syntax
	toStr := func(raw json.RawMessage) (string, bool) {
		var x string
		if json.Unmarshal(raw, &x) == nil {
			return x, true
		}
		var n float64
		if json.Unmarshal(raw, &n) == nil {
			return strconv.FormatFloat(n, 'f', -1, 64), true
		}
		var bv bool
		if json.Unmarshal(raw, &bv) == nil {
			return strconv.FormatBool(bv), true
		}
		return "", false
	}
	str := func() bool {
		x, ok := toStr(f.Value)
		s = x
		return ok
	}
	num := func() bool {
		x, ok := asNumber(f.Value)
		f64 = x
		return ok
	}
	strs := func() bool {
		var raws []json.RawMessage
		if json.Unmarshal(f.Value, &raws) == nil {
			if len(raws) > 100 {
				return false
			}
			for _, r := range raws {
				x, ok := toStr(r)
				if !ok {
					return false
				}
				list = append(list, x)
			}
			return true
		}
		if str() {
			list = []string{s}
			return true
		}
		return false
	}
	cmp := map[string]string{"eq": "=", "ne": "<>", "lt": "<", "le": "<=", "gt": ">", "ge": ">="}
	switch t.Storage {
	case StoreText:
		switch f.Op {
		case "eq", "ne", "lt", "le", "gt", "ge":
			if !str() {
				return "", apperr.BadRequest("%s: text expected", f.Key)
			}
			return "lower(" + v + ".txt) " + cmp[f.Op] + " lower(" + b.arg(s) + ")", nil
		case "contains", "starts":
			if !str() || len(s) > 200 {
				return "", apperr.BadRequest("%s: text expected", f.Key)
			}
			pat := likeEscape(s) + "%"
			if f.Op == "contains" {
				pat = "%" + pat
			}
			return v + ".txt ILIKE " + b.arg(pat), nil
		case "in", "nin":
			if !strs() {
				return "", apperr.BadRequest("%s: list expected", f.Key)
			}
			low := make([]string, len(list))
			for i, x := range list {
				low[i] = strings.ToLower(x)
			}
			c := "lower(" + v + ".txt) = ANY(" + b.arg(low) + ")"
			if f.Op == "nin" {
				c = "NOT " + c
			}
			return c, nil
		}
	case StoreNum:
		switch f.Op {
		case "eq", "ne", "lt", "le", "gt", "ge":
			if t.ID == "boolean" {
				var bv bool
				if json.Unmarshal(f.Value, &bv) != nil {
					return "", apperr.BadRequest("%s: yes or no expected", f.Key)
				}
				f64 = 0
				if bv {
					f64 = 1
				}
			} else if !num() {
				return "", apperr.BadRequest("%s: number expected", f.Key)
			}
			return v + ".num " + cmp[f.Op] + " " + b.arg(f64), nil
		case "between":
			var r [2]float64
			if json.Unmarshal(f.Value, &r) != nil {
				return "", apperr.BadRequest("%s: [from, to] expected", f.Key)
			}
			return v + ".num BETWEEN " + b.arg(r[0]) + " AND " + b.arg(r[1]), nil
		}
	case StoreTime, StoreRange:
		now := time.Now().UTC()
		day := func(raw json.RawMessage) (time.Time, error) {
			var x string
			if json.Unmarshal(raw, &x) != nil {
				return time.Time{}, apperr.BadRequest("%s: date expected", f.Key)
			}
			d, ok := relDate(x, now)
			if !ok {
				return time.Time{}, apperr.BadRequest("%s: date such as 2026-12-31 or today+30d", f.Key)
			}
			return d, nil
		}
		end := v + ".ts"
		if t.Storage == StoreRange {
			end = "COALESCE(" + v + ".ts_end, " + v + ".ts)"
		}
		switch f.Op {
		case "eq", "ne":
			d, err := day(f.Value)
			if err != nil {
				return "", err
			}
			c := "(" + v + ".ts < " + b.arg(d.AddDate(0, 0, 1)) + " AND " + end + " >= " + b.arg(d) + ")"
			if f.Op == "ne" {
				c = "NOT " + c
			}
			return c, nil
		case "lt", "le", "gt", "ge":
			d, err := day(f.Value)
			if err != nil {
				return "", err
			}
			switch f.Op {
			case "lt":
				return v + ".ts < " + b.arg(d), nil
			case "le":
				return v + ".ts < " + b.arg(d.AddDate(0, 0, 1)), nil
			case "gt":
				return end + " >= " + b.arg(d.AddDate(0, 0, 1)), nil
			default:
				return end + " >= " + b.arg(d), nil
			}
		case "between":
			var r [2]json.RawMessage
			if json.Unmarshal(f.Value, &r) != nil {
				return "", apperr.BadRequest("%s: [from, to] expected", f.Key)
			}
			a, err := day(r[0])
			if err != nil {
				return "", err
			}
			z, err := day(r[1])
			if err != nil {
				return "", err
			}
			return "(" + v + ".ts < " + b.arg(z.AddDate(0, 0, 1)) + " AND " + end + " >= " + b.arg(a) + ")", nil
		}
	case StoreArr:
		switch f.Op {
		case "has", "hasall", "hasnot", "eq":
			if !strs() {
				return "", apperr.BadRequest("%s: list expected", f.Key)
			}
			op := "&&"
			if f.Op == "hasall" {
				op = "@>"
			}
			c := v + ".arr " + op + " " + b.arg(list)
			if f.Op == "hasnot" {
				c = "NOT " + c
			}
			return c, nil
		}
	case StoreRefs:
		switch f.Op {
		case "has", "hasnot", "eq", "me":
			var ids []int64
			if f.Op == "me" {
				if me == 0 {
					return "false", nil
				}
				ids = []int64{me}
			} else if x, ok := asIDs(f.Value); ok && len(x) <= 100 {
				ids = x
			} else {
				return "", apperr.BadRequest("%s: ids expected", f.Key)
			}
			c := v + ".refs && " + b.arg(ids)
			if f.Op == "hasnot" {
				c = "NOT " + c
			}
			return c, nil
		}
	}
	return "", apperr.BadRequest("%s: the operator %q does not fit the type %s", f.Key, f.Op, t.ID)
}

// builtinCol is the column of a built-in key on elements e.
func builtinCol(key, lang string, b *qb) (string, TypeDef) {
	t, _ := TypeByID(builtinType[key])
	switch key {
	case "title":
		return "COALESCE(e.titles->>" + b.arg(lang) + ", e.titles->>e.original_lang)", t
	case "type":
		return "e.type", t
	case "category":
		return "ARRAY[e.category_id]", t
	case "tags":
		return "e.tags", t
	case "created_by":
		return "ARRAY[e.created_by]", t
	case "updated_by":
		return "ARRAY[e.updated_by]", t
	}
	return "e." + key, t // created_at, updated_at
}

// builtinCond is a filter on a built-in key.
func builtinCond(b *qb, f Filter, lang string, me int64) (string, error) {
	col, t := builtinCol(f.Key, lang, b)
	// rewrite to the alias form: a fake value table x with one column
	var alias string
	switch t.Storage {
	case StoreText:
		alias = "(SELECT " + col + " AS txt)"
	case StoreArr:
		alias = "(SELECT " + col + " AS arr)"
	case StoreRefs:
		alias = "(SELECT " + col + " AS refs)"
	default:
		alias = "(SELECT " + col + " AS ts, NULL::timestamptz AS ts_end)"
	}
	switch f.Op {
	case "empty", "notempty":
		c := "(" + col + ") IS NULL"
		if t.Storage == StoreArr {
			c = "cardinality(" + col + ") = 0"
		}
		if f.Op == "notempty" {
			c = "NOT (" + c + ")"
		}
		return c, nil
	}
	c, err := cond(b, f, t, "x", me)
	if err != nil {
		return "", err
	}
	return "EXISTS (SELECT 1 FROM " + alias + " x WHERE " + c + ")", nil
}

// where builds the conditions of a query on elements e.
func (p *Plugin) where(ctx context.Context, q *Query, sc *scope, b *qb) (string, error) {
	me := identity.From(ctx).ID()
	parts := []string{"e.deleted_at IS NULL", "e.category_id = ANY(" + b.arg(sc.cats) + ")"}
	if q.Exclude > 0 {
		parts = append(parts, "e.id <> "+b.arg(q.Exclude))
	}
	// sources: categories (already the scope), tags, types, elements; combined with OR
	var or []string
	var hasCat bool
	for _, s := range q.Sources {
		switch s.Kind {
		case "category":
			hasCat = true
		case "tag":
			or = append(or, b.arg(strings.ToLower(s.Tag))+" = ANY(e.tags)")
		case "type":
			or = append(or, "e.type = "+b.arg(s.Type))
		case "elements":
			if len(s.IDs) > 1000 {
				return "", apperr.BadRequest("at most 1000 elements")
			}
			or = append(or, "e.id = ANY("+b.arg(s.IDs)+")")
		default:
			return "", apperr.BadRequest("unknown source %q", s.Kind)
		}
	}
	if hasCat && len(or) > 0 {
		// categories are in the scope; other sources may reach elsewhere: scope then is all viewable
		return "", apperr.BadRequest("combine categories with tags or types as filters, not as sources")
	}
	if len(or) > 0 {
		parts = append(parts, "("+strings.Join(or, " OR ")+")")
	}
	if s := strings.TrimSpace(q.Search); s != "" {
		if len(s) > 200 {
			return "", apperr.BadRequest("search too long")
		}
		parts = append(parts, "COALESCE(e.titles->>"+b.arg(q.Lang)+", e.titles->>e.original_lang) ILIKE "+b.arg("%"+likeEscape(s)+"%"))
	}
	if len(q.Filters) > 30 {
		return "", apperr.BadRequest("at most 30 filters")
	}
	for _, f := range q.Filters {
		if _, ok := builtinType[f.Key]; ok {
			c, err := builtinCond(b, f, q.Lang, me)
			if err != nil {
				return "", err
			}
			parts = append(parts, c)
			continue
		}
		if !keyRe.MatchString(f.Key) || sc.hidden[f.Key] {
			return "", apperr.BadRequest("unknown property %q", f.Key)
		}
		vis := sc.visCond(b, "v")
		key := b.arg(f.Key)
		switch f.Op {
		case "empty":
			parts = append(parts, "NOT EXISTS (SELECT 1 FROM plugin_prop_values v WHERE v.element_id = e.id AND v.key = "+key+" AND "+vis+")")
			continue
		case "notempty":
			parts = append(parts, "EXISTS (SELECT 1 FROM plugin_prop_values v WHERE v.element_id = e.id AND v.key = "+key+" AND "+vis+")")
			continue
		}
		t, _ := TypeByID(p.typeOf(ctx, sc, f.Key))
		c, err := cond(b, f, t, "v", me)
		if err != nil {
			return "", err
		}
		neg := strings.HasPrefix(c, "NOT ")
		if neg {
			// "not equal" also matches elements without a value
			parts = append(parts, "NOT EXISTS (SELECT 1 FROM plugin_prop_values v WHERE v.element_id = e.id AND v.key = "+key+" AND "+vis+" AND "+strings.TrimPrefix(c, "NOT ")+")")
		} else {
			parts = append(parts, "EXISTS (SELECT 1 FROM plugin_prop_values v WHERE v.element_id = e.id AND v.key = "+key+" AND "+vis+" AND "+c+")")
		}
	}
	return strings.Join(parts, " AND "), nil
}

// Run executes a query.
func (p *Plugin) Run(ctx context.Context, q *Query) (*Result, error) {
	if len(q.Sources) > 50 {
		return nil, apperr.BadRequest("at most 50 sources")
	}
	q.Lang = safeLang(q.Lang)
	sc, err := p.scope(ctx, q)
	if err != nil {
		return nil, err
	}
	res := &Result{Refs: map[string]Ref{}}
	if q.Group != "" {
		return p.groups(ctx, q, sc, res)
	}
	b := &qb{}
	where, err := p.where(ctx, q, sc, b)
	if err != nil {
		return nil, err
	}
	if q.Limit <= 0 || q.Limit > maxRows {
		q.Limit = 100
	}
	if q.Offset < 0 || q.Offset > 100000 {
		q.Offset = 0
	}
	// sorting (by properties: the visible value, empty ones last)
	var joins, order []string
	if len(q.Sort) > 5 {
		return nil, apperr.BadRequest("at most 5 sort keys")
	}
	for i, s := range q.Sort {
		dir := " ASC"
		if s.Desc {
			dir = " DESC"
		}
		if _, ok := builtinType[s.Key]; ok {
			col, _ := builtinCol(s.Key, q.Lang, b)
			if s.Key == "title" {
				col = "lower(" + col + ")"
			}
			order = append(order, col+dir+" NULLS LAST")
			continue
		}
		if !keyRe.MatchString(s.Key) || sc.hidden[s.Key] {
			return nil, apperr.BadRequest("unknown property %q", s.Key)
		}
		a := "s" + strconv.Itoa(i)
		joins = append(joins, "LEFT JOIN plugin_prop_values "+a+" ON "+a+".element_id = e.id AND "+a+".key = "+b.arg(s.Key)+" AND "+sc.visCond(b, a))
		order = append(order, a+".num"+dir+" NULLS LAST", a+".ts"+dir+" NULLS LAST", "lower("+a+".txt)"+dir+" NULLS LAST")
	}
	order = append(order, "lower(COALESCE(e.titles->>"+b.arg(q.Lang)+", e.titles->>e.original_lang))", "e.id")
	sql := `SELECT e.id, COALESCE(e.titles->>` + b.arg(q.Lang) + `, e.titles->>e.original_lang, ''), e.type, e.category_id, e.tags, e.created_at, e.updated_at,
		e.created_by, e.updated_by, count(*) OVER () FROM elements e ` + strings.Join(joins, " ") + ` WHERE ` + where +
		` ORDER BY ` + strings.Join(order, ", ") + ` LIMIT ` + b.arg(q.Limit) + ` OFFSET ` + b.arg(q.Offset)
	rows, err := p.host.DB.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, err
	}
	res.Rows, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Row, error) {
		var x Row
		err := r.Scan(&x.ID, &x.Title, &x.Type, &x.CategoryID, &x.Tags, &x.CreatedAt, &x.UpdatedAt, &x.CreatedBy, &x.UpdatedBy, &res.Total)
		x.Link = "/e/" + strconv.FormatInt(x.ID, 10)
		x.Values = map[string]json.RawMessage{}
		return x, err
	})
	if err != nil {
		return nil, err
	}
	if res.Rows == nil {
		res.Rows = []Row{}
	}
	ids := make([]int64, len(res.Rows))
	cats := make([]int64, 0, len(res.Rows))
	for i, r := range res.Rows {
		ids[i] = r.ID
		cats = append(cats, r.CategoryID)
	}
	editable, err := p.host.ACL.Filter(ctx, identity.From(ctx), cats, acl.Edit)
	if err != nil {
		return nil, err
	}
	for i := range res.Rows {
		res.Rows[i].CanEdit = slices.Contains(editable, res.Rows[i].CategoryID)
	}
	vals, err := p.load(ctx, p.host.DB, ids)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, c := range q.Columns {
		want[c] = true
	}
	refIDs := map[string][]int64{}
	cols := map[string]Column{}
	for i := range res.Rows {
		r := &res.Rows[i]
		for _, v := range vals[r.ID] {
			if v.DefID != nil && !slices.Contains(sc.vis, *v.DefID) {
				continue
			}
			if len(want) > 0 && !want[v.Key] {
				continue
			}
			r.Values[v.Key] = v.Value
			if _, ok := cols[v.Key]; !ok {
				cols[v.Key] = p.column(sc, v)
			}
			switch v.Type {
			case "user", "group", "element", "category":
				if x, ok := asIDs(v.Value); ok {
					refIDs[v.Type] = append(refIDs[v.Type], x...)
				}
			}
		}
		if want["created_by"] && r.CreatedBy != nil {
			refIDs["user"] = append(refIDs["user"], *r.CreatedBy)
		}
		if want["updated_by"] && r.UpdatedBy != nil {
			refIDs["user"] = append(refIDs["user"], *r.UpdatedBy)
		}
		if want["category"] {
			refIDs["category"] = append(refIDs["category"], r.CategoryID)
		}
	}
	if res.Refs, err = p.refNames(ctx, refIDs, q.Lang); err != nil {
		return nil, err
	}
	res.Columns = p.columns(sc, q, cols)
	return res, nil
}

// column describes a stored value's key (the first definition the caller sees).
func (p *Plugin) column(sc *scope, v Value) Column {
	c := Column{Key: v.Key, Label: v.Label, Type: v.Type}
	for _, d := range sc.defs[v.Key] {
		if slices.Contains(sc.vis, d.ID) {
			o := d.Options
			c.Label, c.Type, c.Options = d.Label("en"), d.Type, &o
			break
		}
	}
	if c.Label == "" {
		c.Label = v.Key
	}
	return c
}

// columns in the order asked (or by label), with labels in the language.
func (p *Plugin) columns(sc *scope, q *Query, found map[string]Column) []Column {
	label := func(c Column) Column {
		for _, d := range sc.defs[c.Key] {
			if slices.Contains(sc.vis, d.ID) {
				c.Label = d.Label(q.Lang)
				break
			}
		}
		return c
	}
	out := []Column{}
	if len(q.Columns) > 0 {
		for _, k := range q.Columns {
			if t, ok := builtinType[k]; ok {
				out = append(out, Column{Key: k, Type: t, Builtin: true})
				continue
			}
			if c, ok := found[k]; ok {
				out = append(out, label(c))
				continue
			}
			if ds := sc.defs[k]; len(ds) > 0 && !sc.hidden[k] {
				for _, d := range ds {
					if slices.Contains(sc.vis, d.ID) {
						o := d.Options
						out = append(out, Column{Key: k, Label: d.Label(q.Lang), Type: d.Type, Options: &o})
						break
					}
				}
			}
		}
		return out
	}
	for _, c := range found {
		out = append(out, label(c))
	}
	slices.SortFunc(out, func(a, b Column) int { return strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label)) })
	return out
}

// groupExpr returns the expression of a grouping key, joining its values (alias a).
func (p *Plugin) groupExpr(ctx context.Context, q *Query, sc *scope, b *qb, key, a string, joins *[]string) (string, TypeDef, error) {
	bucket := "month"
	switch q.Bucket {
	case "day", "week", "month", "quarter", "year":
		bucket = q.Bucket
	}
	if _, ok := builtinType[key]; ok {
		col, t := builtinCol(key, q.Lang, b)
		switch t.Storage {
		case StoreArr, StoreRefs:
			*joins = append(*joins, "LEFT JOIN LATERAL unnest("+col+") "+a+"(g) ON true")
			return a + ".g::text", t, nil
		case StoreTime:
			return "to_char(date_trunc('" + bucket + "', " + col + "), 'YYYY-MM-DD')", t, nil
		}
		return col, t, nil
	}
	if !keyRe.MatchString(key) || sc.hidden[key] {
		return "", TypeDef{}, apperr.BadRequest("unknown property %q", key)
	}
	t, _ := TypeByID(p.typeOf(ctx, sc, key))
	*joins = append(*joins, "LEFT JOIN plugin_prop_values "+a+" ON "+a+".element_id = e.id AND "+a+".key = "+b.arg(key)+" AND "+sc.visCond(b, a))
	switch t.Storage {
	case StoreArr:
		*joins = append(*joins, "LEFT JOIN LATERAL unnest("+a+".arr) "+a+"u(g) ON true")
		return a + "u.g", t, nil
	case StoreRefs:
		*joins = append(*joins, "LEFT JOIN LATERAL unnest("+a+".refs) "+a+"u(g) ON true")
		return a + "u.g::text", t, nil
	case StoreTime, StoreRange:
		return "to_char(date_trunc('" + bucket + "', " + a + ".ts), 'YYYY-MM-DD')", t, nil
	case StoreNum:
		if t.ID == "boolean" {
			return "(" + a + ".num = 1)::text", t, nil
		}
		return a + ".num::text", t, nil
	}
	return a + ".txt", t, nil
}

// groups runs a grouped query (counts or sums per value).
func (p *Plugin) groups(ctx context.Context, q *Query, sc *scope, res *Result) (*Result, error) {
	b := &qb{}
	where, err := p.where(ctx, q, sc, b)
	if err != nil {
		return nil, err
	}
	var joins []string
	g1, t1, err := p.groupExpr(ctx, q, sc, b, q.Group, "g1", &joins)
	if err != nil {
		return nil, err
	}
	g2 := "NULL::text"
	var t2 TypeDef
	if q.Group2 != "" {
		if g2, t2, err = p.groupExpr(ctx, q, sc, b, q.Group2, "g2", &joins); err != nil {
			return nil, err
		}
	}
	num := "NULL::numeric"
	agg := "count(DISTINCT eid)"
	switch q.Agg {
	case "", "count":
	case "sum", "avg", "min", "max":
		if !keyRe.MatchString(q.AggKey) || sc.hidden[q.AggKey] {
			return nil, apperr.BadRequest("unknown property %q", q.AggKey)
		}
		at, _ := TypeByID(p.typeOf(ctx, sc, q.AggKey))
		if at.Storage != StoreNum {
			return nil, apperr.BadRequest("%s is not a number", q.AggKey)
		}
		joins = append(joins, "LEFT JOIN plugin_prop_values av ON av.element_id = e.id AND av.key = "+b.arg(q.AggKey)+" AND "+sc.visCond(b, "av"))
		num = "av.num"
		agg = "COALESCE(" + q.Agg + "(n), 0)"
	default:
		return nil, apperr.BadRequest("agg: count, sum, avg, min or max")
	}
	// one row per group and element (unnested keys must not count an element twice)
	sql := `SELECT k1, k2, ` + agg + `, count(DISTINCT eid) FROM (SELECT DISTINCT ` + g1 + ` AS k1, ` + g2 + ` AS k2, e.id AS eid, ` + num + ` AS n
		FROM elements e ` + strings.Join(joins, " ") + ` WHERE ` + where + `) t GROUP BY 1, 2 ORDER BY 1 NULLS LAST, 2 NULLS LAST LIMIT ` + strconv.Itoa(maxGroups+1)
	rows, err := p.host.DB.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, err
	}
	type g struct {
		k1, k2 *string
		v      float64
		n      int
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (g, error) {
		var x g
		return x, r.Scan(&x.k1, &x.k2, &x.v, &x.n)
	})
	if err != nil {
		return nil, err
	}
	if len(list) > maxGroups {
		return nil, apperr.BadRequest("more than %d groups: choose a coarser grouping or filter", maxGroups)
	}
	refIDs := map[string][]int64{}
	keyJSON := func(k *string, t TypeDef) json.RawMessage {
		if k == nil {
			return json.RawMessage("null")
		}
		switch t.Storage {
		case StoreRefs:
			if id, err := strconv.ParseInt(*k, 10, 64); err == nil {
				refIDs[t.ID] = append(refIDs[t.ID], id)
				return mustJSON(id)
			}
		case StoreNum:
			if t.ID == "boolean" {
				return mustJSON(*k == "true")
			}
			if f, err := strconv.ParseFloat(*k, 64); err == nil {
				return mustJSON(f)
			}
		}
		return mustJSON(*k)
	}
	res.Groups = make([]Group, 0, len(list))
	for _, x := range list {
		gr := Group{Key: keyJSON(x.k1, t1), Value: x.v, Count: x.n}
		if q.Group2 != "" {
			gr.Key2 = keyJSON(x.k2, t2)
		}
		res.Groups = append(res.Groups, gr)
		res.Total += x.n
	}
	if res.Refs, err = p.refNames(ctx, refIDs, q.Lang); err != nil {
		return nil, err
	}
	// keys without a visible definition (free properties): their type
	found := map[string]Column{}
	for _, k := range []string{q.Group, q.Group2, q.AggKey} {
		if _, builtin := builtinType[k]; k != "" && !builtin && len(sc.defs[k]) == 0 {
			found[k] = Column{Key: k, Label: k, Type: p.typeOf(ctx, sc, k)}
		}
	}
	q.Columns = slices.DeleteFunc([]string{q.Group, q.Group2, q.AggKey}, func(s string) bool { return s == "" })
	res.Columns = p.columns(sc, q, found)
	return res, nil
}

func (p *Plugin) routesQuery() {
	host := p.host
	// POST /query – rows or groups (see Query)
	host.Handle("POST", "/query", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var q Query
		if err := h.Decode(r, &q); err != nil {
			return nil, err
		}
		return p.Run(r.Context(), &q)
	})
	// POST /report – a report from the parameters of the macro (as in the
	// page), with filters of the reader added, another sorting or page:
	// {"params": {...}, "category_id": 1, "element_id": 2, "filters": [...], "sort": [...], "offset": 0, "lang": "de", "rows": true}
	host.Handle("POST", "/report", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in reportInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		q, err := in.query()
		if err != nil {
			return nil, err
		}
		q.Offset = in.Offset
		if in.Rows {
			q.Group, q.Group2 = "", ""
		}
		return p.Run(r.Context(), q)
	})
	// POST /query.csv and /query.xlsx – the rows as file (strictly
	// confidential content is never exported)
	export := func(format string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !host.IsEnabled(host.Manifest.ID) {
				h.WriteError(w, r, apperr.NotFound("plugin"))
				return
			}
			// a query or (with "params") the body of a report
			var raw json.RawMessage
			if err := h.Decode(r, &raw); err != nil {
				h.WriteError(w, r, err)
				return
			}
			q := &Query{}
			var probe struct {
				Params map[string]string `json:"params"`
			}
			_ = json.Unmarshal(raw, &probe)
			if probe.Params != nil {
				if probe.Params["export"] == "false" {
					h.WriteError(w, r, apperr.Forbidden("this report offers no download"))
					return
				}
				var in reportInput
				if err := json.Unmarshal(raw, &in); err != nil {
					h.WriteError(w, r, apperr.BadRequest("invalid report"))
					return
				}
				var err error
				if q, err = in.query(); err != nil {
					h.WriteError(w, r, err)
					return
				}
			} else if err := json.Unmarshal(raw, q); err != nil {
				h.WriteError(w, r, apperr.BadRequest("invalid query"))
				return
			}
			q.Group, q.Group2, q.Limit, q.Offset = "", "", maxRows, 0
			res, err := p.Run(r.Context(), q)
			if err != nil {
				h.WriteError(w, r, err)
				return
			}
			rows := res.Rows[:0:0]
			for _, row := range res.Rows {
				if host.ACL.Classification(r.Context(), row.CategoryID) < acl.StrictConfidential {
					rows = append(rows, row)
				}
			}
			head := []string{"id", "title", "link"}
			for _, c := range res.Columns {
				head = append(head, c.Label)
			}
			if format == "xlsx" {
				out := make([][]xlsx.Cell, 0, len(rows))
				for _, row := range rows {
					rec := []xlsx.Cell{xlsx.Num(float64(row.ID)), xlsx.Text(row.Title), xlsx.Text(row.Link)}
					for _, c := range res.Columns {
						raw := builtinValue(row, c.Key)
						var f float64
						if t, ok := TypeByID(c.Type); ok && t.Group == "number" && json.Unmarshal(raw, &f) == nil {
							rec = append(rec, xlsx.Num(f))
						} else {
							rec = append(rec, xlsx.Text(plain(raw, c.Type, res.Refs)))
						}
					}
					out = append(out, rec)
				}
				w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
				w.Header().Set("Content-Disposition", `attachment; filename="properties.xlsx"`)
				_ = xlsx.Write(w, "Properties", head, out)
				return
			}
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="properties.csv"`)
			_, _ = w.Write([]byte("\xef\xbb\xbf"))
			cw := csv.NewWriter(w)
			for i := range head {
				head[i] = csvSafe(head[i])
			}
			_ = cw.Write(head)
			for _, row := range rows {
				rec := []string{strconv.FormatInt(row.ID, 10), csvSafe(row.Title), row.Link}
				for _, c := range res.Columns {
					rec = append(rec, csvSafe(plain(builtinValue(row, c.Key), c.Type, res.Refs)))
				}
				_ = cw.Write(rec)
			}
			cw.Flush()
		}
	}
	host.Mux.HandleFunc("POST /api/v1/x/core.properties/query.csv", export("csv"))
	host.Mux.HandleFunc("POST /api/v1/x/core.properties/query.xlsx", export("xlsx"))
	// GET /values?key=K&category=ID – values in use (for filter forms)
	host.Handle("GET", "/values", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		key := r.URL.Query().Get("key")
		cat, _ := strconv.ParseInt(r.URL.Query().Get("category"), 10, 64)
		q := &Query{Group: key, Lang: r.URL.Query().Get("lang")}
		if cat > 0 {
			q.Sources = []Source{{Kind: "category", ID: cat, Sub: true}}
		}
		if key == "" {
			return nil, apperr.BadRequest("key is required")
		}
		return p.Run(ctx, q)
	})
	// GET /keys?category=ID – keys in use there (suggestions), with type and label
	host.Handle("GET", "/keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		cat, _ := strconv.ParseInt(r.URL.Query().Get("category"), 10, 64)
		q := &Query{}
		if cat > 0 {
			q.Sources = []Source{{Kind: "category", ID: cat, Sub: true}}
		}
		sc, err := p.scope(ctx, q)
		if err != nil {
			return nil, err
		}
		b := &qb{}
		rows, err := host.DB.Query(ctx, `SELECT v.key, min(v.type), max(v.label), count(*) FROM plugin_prop_values v JOIN elements e ON e.id = v.element_id
			WHERE e.deleted_at IS NULL AND e.category_id = ANY(`+b.arg(sc.cats)+`) AND `+sc.visCond(b, "v")+`
			GROUP BY v.key ORDER BY count(*) DESC, v.key LIMIT 200`, b.args...)
		if err != nil {
			return nil, err
		}
		type key struct {
			Key   string `json:"key"`
			Type  string `json:"type"`
			Label string `json:"label"`
			Count int    `json:"count"`
		}
		list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[key])
		if err != nil {
			return nil, err
		}
		lang := r.URL.Query().Get("lang")
		for i := range list {
			for _, d := range sc.defs[list[i].Key] {
				if slices.Contains(sc.vis, d.ID) {
					list[i].Label, list[i].Type = d.Label(lang), d.Type
					break
				}
			}
			if list[i].Label == "" {
				list[i].Label = list[i].Key
			}
		}
		return list, nil
	})
}

// builtinValue is the value of a key in a row (built-in or property).
func builtinValue(r Row, key string) json.RawMessage {
	switch key {
	case "title":
		return mustJSON(r.Title)
	case "type":
		return mustJSON(r.Type)
	case "category":
		return mustJSON([]int64{r.CategoryID})
	case "tags":
		return mustJSON(r.Tags)
	case "created_at":
		return mustJSON(r.CreatedAt.UTC().Format(time.RFC3339))
	case "updated_at":
		return mustJSON(r.UpdatedAt.UTC().Format(time.RFC3339))
	case "created_by":
		if r.CreatedBy == nil {
			return nil
		}
		return mustJSON([]int64{*r.CreatedBy})
	case "updated_by":
		if r.UpdatedBy == nil {
			return nil
		}
		return mustJSON([]int64{*r.UpdatedBy})
	}
	return r.Values[key]
}

// plain renders a value as text (CSV, accessible summaries).
func plain(raw json.RawMessage, typ string, refs map[string]Ref) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	switch typ {
	case "user", "group", "element", "category":
		ids, _ := asIDs(raw)
		names := []string{}
		for _, id := range ids {
			if r, ok := refs[typ+":"+strconv.FormatInt(id, 10)]; ok {
				names = append(names, r.Name)
			}
		}
		return strings.Join(names, ", ")
	case "multiselect":
		var l []string
		_ = json.Unmarshal(raw, &l)
		return strings.Join(l, ", ")
	case "boolean":
		if string(raw) == "true" {
			return "yes"
		}
		return "no"
	case "daterange":
		var d DateRange
		_ = json.Unmarshal(raw, &d)
		return d.From + " – " + d.To
	case "location":
		var l Location
		_ = json.Unmarshal(raw, &l)
		if l.Lat != nil {
			return fmt.Sprintf("%s (%.5f, %.5f)", l.Address, *l.Lat, *l.Lng)
		}
		return l.Address
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

// csvSafe keeps spreadsheets from running cell text as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// reportInput is the body of POST /report (a report macro with the
// filters and sorting of its reader).
type reportInput struct {
	Params     map[string]string `json:"params"`
	CategoryID int64             `json:"category_id"`
	ElementID  int64             `json:"element_id"`
	Filters    []Filter          `json:"filters"`
	Sort       []SortKey         `json:"sort"`
	Offset     int               `json:"offset"`
	Lang       string            `json:"lang"`
	// Rows: the elements instead of the groups (e.g. a chart's data per element)
	Rows bool `json:"rows"`
}

func (in *reportInput) query() (*Query, error) {
	if len(in.Params) > 40 {
		return nil, apperr.BadRequest("too many parameters")
	}
	q, err := QueryOf(in.Params, in.CategoryID, in.ElementID, in.Lang)
	if err != nil {
		return nil, err
	}
	if len(in.Filters) > 10 {
		return nil, apperr.BadRequest("at most 10 filters of readers")
	}
	q.Filters = append(q.Filters, in.Filters...)
	if len(in.Sort) > 0 {
		q.Sort = in.Sort
	}
	return q, nil
}
