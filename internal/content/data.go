package content

// Data of blocks and plugins for reports (concept "Bausteindaten",
// 22.11.2026): kinds of data are registered with a schema; blocks in the
// text (DataMacro) give their data when a revision is saved, plugins write
// their own; the core keeps them in element_data in the same transaction as
// the revision and answers queries with the permissions of the reader.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Field types of data.
const (
	DataText   = "text"   // a line of text (Max characters, default 500)
	DataLong   = "long"   // a longer text (Max characters, default 10000)
	DataChoice = "choice" // one of Options
	DataDay    = "day"    // a day, YYYY-MM-DD
	DataTime   = "time"   // a point in time, RFC 3339
	DataNumber = "number" // a number
	DataBool   = "bool"   // true or false
	DataPerson = "person" // the id of a user
	DataPage   = "page"   // the id of an element
	DataList   = "list"   // a list of short texts (at most Max entries, default 50)
)

// Columns a field can be kept in for filtering and sorting (indexed).
const (
	ColState  = "state"  // choice or text: status and the like
	ColDay    = "day"    // day: due date, date of a decision …
	ColAt     = "at"     // time
	ColPerson = "person" // person: owner, assignee …
	ColNum    = "num"    // number
	ColRef    = "ref"    // page: a page the entry is about
	ColLabel  = "label"  // text: the name shown in lists
)

var dataColumns = map[string]string{ColState: DataChoice, ColDay: DataDay, ColAt: DataTime, ColPerson: DataPerson, ColNum: DataNumber, ColRef: DataPage, ColLabel: DataText}

// DataField is a field of a kind of data.
type DataField struct {
	Key     string            `json:"key"`
	Type    string            `json:"type"`
	Labels  map[string]string `json:"labels"`
	Options []string          `json:"options,omitempty"`
	// OptionLabels: labels of the options by language
	OptionLabels map[string]map[string]string `json:"option_labels,omitempty"`
	// Column: kept also in an indexed column (one field per column)
	Column   string `json:"column,omitempty"`
	Required bool   `json:"required,omitempty"`
	Max      int    `json:"max,omitempty"`
}

// DataKind is a registered kind of data.
type DataKind struct {
	// Kind: plugin id + "." + name, e.g. core.macros.decision
	Kind    string            `json:"kind"`
	Version int               `json:"version"`
	Labels  map[string]string `json:"labels"`
	Fields  []DataField       `json:"fields"`
	// MaxPerElement: entries of one element and language (default 500)
	MaxPerElement int `json:"max_per_element"`
	// Plugin owns the kind (set when registered)
	Plugin string `json:"plugin"`
}

// DataUse is one use of a block in the text (for DataMacro.Data).
type DataUse struct {
	Lang   string
	Params map[string]string
	// Text is the plain text inside the block, HTML its sanitized HTML
	Text, HTML string
	// Index is the number of the block on the element (as data-macro-index)
	Index int
}

// DataMacro is implemented by blocks that keep data of each use for
// reports. Data returns the data of one use (nil: nothing kept); invalid
// data is not kept (the block shows its own error).
type DataMacro interface {
	DataKind() string
	Data(ctx context.Context, u DataUse) (map[string]any, error)
}

// ConditionalMacro is implemented by blocks that show their content only to
// some readers (or only under a condition). Blocks inside them keep no
// data: a report would show it to everyone who may see the page.
type ConditionalMacro interface {
	Conditional() bool
}

// MacroHTMLer is implemented by element types whose body has blocks in
// HTML (core.html and the types built on it): it gives the HTML in which
// the core finds the blocks with data.
type MacroHTMLer interface {
	MacroHTML(body json.RawMessage) (string, bool)
}

// SourceMacro: entries written from the blocks of the text.
const SourceMacro = "macro"

var dataKindRe = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
var dataKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

type dataRegistry struct {
	mu    sync.RWMutex
	kinds map[string]DataKind
}

// RegisterDataKind registers a kind of data owned by pluginID; the kind
// must start with the plugin id. It panics on an invalid schema (a
// programming error, found at start).
func (s *Service) RegisterDataKind(pluginID string, k DataKind) {
	if err := checkDataKind(pluginID, &k); err != nil {
		panic(err.Error())
	}
	s.data.mu.Lock()
	defer s.data.mu.Unlock()
	if s.data.kinds == nil {
		s.data.kinds = map[string]DataKind{}
	}
	s.data.kinds[k.Kind] = k
}

func checkDataKind(pluginID string, k *DataKind) error {
	if !dataKindRe.MatchString(k.Kind) || !strings.HasPrefix(k.Kind, pluginID+".") {
		return fmt.Errorf("data kind %q: must be %s.<name>", k.Kind, pluginID)
	}
	if k.Version < 1 {
		k.Version = 1
	}
	if k.MaxPerElement <= 0 {
		k.MaxPerElement = 500
	}
	k.Plugin = pluginID
	seen, cols := map[string]bool{}, map[string]bool{}
	for i := range k.Fields {
		f := &k.Fields[i]
		if !dataKeyRe.MatchString(f.Key) || seen[f.Key] {
			return fmt.Errorf("data kind %s: invalid or repeated field %q", k.Kind, f.Key)
		}
		seen[f.Key] = true
		switch f.Type {
		case DataText, DataLong, DataChoice, DataDay, DataTime, DataNumber, DataBool, DataPerson, DataPage, DataList:
		default:
			return fmt.Errorf("data kind %s: field %s has the unknown type %q", k.Kind, f.Key, f.Type)
		}
		if f.Type == DataChoice && len(f.Options) == 0 {
			return fmt.Errorf("data kind %s: field %s needs options", k.Kind, f.Key)
		}
		if f.Column != "" {
			want, ok := dataColumns[f.Column]
			if !ok || cols[f.Column] || !(want == f.Type || (f.Column == ColState && f.Type == DataText)) {
				return fmt.Errorf("data kind %s: field %s cannot be kept in column %q", k.Kind, f.Key, f.Column)
			}
			cols[f.Column] = true
		}
	}
	return nil
}

// DataKinds lists the registered kinds of active plugins.
func (s *Service) DataKinds() []DataKind {
	s.data.mu.RLock()
	defer s.data.mu.RUnlock()
	out := []DataKind{}
	for _, k := range s.data.kinds {
		if s.pluginOn(k.Plugin) {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Kind < out[b].Kind })
	return out
}

// DataKindOf returns an active kind.
func (s *Service) DataKindOf(kind string) (DataKind, bool) {
	s.data.mu.RLock()
	defer s.data.mu.RUnlock()
	k, ok := s.data.kinds[kind]
	if !ok || !s.pluginOn(k.Plugin) {
		return DataKind{}, false
	}
	return k, true
}

// dataRow is a checked entry with its indexed columns.
type dataRow struct {
	idx    int
	data   []byte
	state  *string
	day    *time.Time
	at     *time.Time
	person *int64
	num    *float64
	ref    *int64
	label  *string
}

// CheckData checks data against its kind and gives the indexed columns.
func checkData(k DataKind, in map[string]any) (dataRow, error) {
	var r dataRow
	out := map[string]any{}
	for _, f := range k.Fields {
		v, ok := in[f.Key]
		if !ok || v == nil || v == "" {
			if f.Required {
				return r, apperr.BadRequest("%s: %s is required", k.Kind, f.Key)
			}
			continue
		}
		cv, err := checkDataValue(k.Kind, f, v)
		if err != nil {
			return r, err
		}
		out[f.Key] = cv
		switch f.Column {
		case ColState:
			s := cv.(string)
			r.state = &s
		case ColLabel:
			s := cv.(string)
			if utf8.RuneCountInString(s) > 300 {
				s = string([]rune(s)[:300])
			}
			r.label = &s
		case ColDay:
			d, _ := time.Parse("2006-01-02", cv.(string))
			r.day = &d
		case ColAt:
			t, _ := time.Parse(time.RFC3339, cv.(string))
			r.at = &t
		case ColNum:
			n := cv.(float64)
			r.num = &n
		case ColPerson:
			n := cv.(int64)
			r.person = &n
		case ColRef:
			n := cv.(int64)
			r.ref = &n
		}
	}
	for key := range in {
		if !slices.ContainsFunc(k.Fields, func(f DataField) bool { return f.Key == key }) {
			return r, apperr.BadRequest("%s: unknown field %q", k.Kind, key)
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return r, err
	}
	if len(b) > 32<<10 {
		return r, apperr.BadRequest("%s: an entry has at most 32 KB", k.Kind)
	}
	r.data = b
	return r, nil
}

func checkDataValue(kind string, f DataField, v any) (any, error) {
	str := func(max int) (string, bool) {
		s, ok := v.(string)
		if !ok {
			return "", false
		}
		s = strings.TrimSpace(s)
		if f.Max > 0 {
			max = f.Max
		}
		if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max || strings.ContainsRune(s, 0) {
			return "", false
		}
		return s, true
	}
	textMax := func(def int) int {
		if f.Max > 0 {
			return f.Max
		}
		return def
	}
	switch f.Type {
	case DataText, DataLong:
		def := 500
		if f.Type == DataLong {
			def = 10000
		}
		if s, ok := str(def); ok {
			return s, nil
		}
		return nil, apperr.BadRequest("%s: %s: a text of at most %d characters", kind, f.Key, textMax(def))
	case DataChoice:
		if s, ok := str(100); ok && slices.Contains(f.Options, s) {
			return s, nil
		}
		return nil, apperr.BadRequest("%s: %s: one of %s", kind, f.Key, strings.Join(f.Options, ", "))
	case DataDay:
		if s, ok := str(10); ok {
			if _, err := time.Parse("2006-01-02", s); err == nil {
				return s, nil
			}
		}
		return nil, apperr.BadRequest("%s: %s: a day as YYYY-MM-DD", kind, f.Key)
	case DataTime:
		if s, ok := str(40); ok {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t.UTC().Format(time.RFC3339), nil
			}
		}
		return nil, apperr.BadRequest("%s: %s: a time as RFC 3339", kind, f.Key)
	case DataNumber:
		switch x := v.(type) {
		case float64:
			return x, nil
		case int:
			return float64(x), nil
		case int64:
			return float64(x), nil
		case string:
			if n, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(x, ",", ".")), 64); err == nil && !math.IsInf(n, 0) && !math.IsNaN(n) {
				return n, nil
			}
		}
		return nil, apperr.BadRequest("%s: %s: a number", kind, f.Key)
	case DataBool:
		switch x := v.(type) {
		case bool:
			return x, nil
		case string:
			if x == "true" || x == "false" {
				return x == "true", nil
			}
		}
		return nil, apperr.BadRequest("%s: %s: true or false", kind, f.Key)
	case DataPerson, DataPage:
		switch x := v.(type) {
		case float64:
			if x >= 1 && x <= 1<<53 && x == math.Trunc(x) {
				return int64(x), nil
			}
		case int64:
			if x >= 1 {
				return x, nil
			}
		case int:
			if x >= 1 {
				return int64(x), nil
			}
		case string:
			if n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil && n >= 1 {
				return n, nil
			}
		}
		return nil, apperr.BadRequest("%s: %s: a number of 1 or more", kind, f.Key)
	case DataList:
		max := textMax(50)
		var list []string
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				s, ok := e.(string)
				if !ok {
					return nil, apperr.BadRequest("%s: %s: a list of texts", kind, f.Key)
				}
				list = append(list, s)
			}
		case []string:
			list = x
		case string:
			list = strings.Split(x, ",")
		default:
			return nil, apperr.BadRequest("%s: %s: a list of texts", kind, f.Key)
		}
		out := []string{}
		for _, s := range list {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if !utf8.ValidString(s) || utf8.RuneCountInString(s) > 200 || strings.ContainsRune(s, 0) {
				return nil, apperr.BadRequest("%s: %s: entries of at most %d characters", kind, f.Key, 200)
			}
			out = append(out, s)
		}
		if len(out) > max {
			return nil, apperr.BadRequest("%s: %s: at most %d entries", kind, f.Key, max)
		}
		return out, nil
	}
	return nil, apperr.BadRequest("%s: %s: a number", kind, f.Key)
}

// macroUses finds the blocks of html in the order of data-macro-index
// (inner blocks first, as RenderMacros counts them). Blocks inside a block
// for which skip is true are counted but not given.
func macroUses(src string, skip func(string) bool) []struct {
	Name   string
	Params map[string]string
	Text   string
	HTML   string
	Index  int
} {
	type use = struct {
		Name   string
		Params map[string]string
		Text   string
		HTML   string
		Index  int
	}
	if !strings.Contains(src, "data-macro") {
		return nil
	}
	ctxNode := &nethtml.Node{Type: nethtml.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := nethtml.ParseFragment(strings.NewReader(src), ctxNode)
	if err != nil {
		return nil
	}
	root := &nethtml.Node{Type: nethtml.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	var out []use
	count := 0
	var walk func(n *nethtml.Node, hidden bool)
	walk = func(n *nethtml.Node, hidden bool) {
		name := ""
		if n.Type == nethtml.ElementNode && (n.DataAtom == atom.Div || n.DataAtom == atom.Span) {
			name = attr(n, "data-macro")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, hidden || (name != "" && skip(name)))
		}
		if name == "" {
			return
		}
		count++
		if hidden {
			return
		}
		params := ParseMacroParams(attr(n, "data-params"))
		if _, ok := params["text"]; !ok && n.DataAtom == atom.Span {
			params["text"] = textOf(n)
		}
		out = append(out, use{Name: name, Params: params, Text: textOf(n), HTML: renderChildren(n), Index: count})
	}
	walk(root, false)
	return out
}

// dataMacros: the active blocks with data, by name.
func (s *Service) dataMacros() map[string]DataMacro {
	s.macros.mu.RLock()
	defer s.macros.mu.RUnlock()
	out := map[string]DataMacro{}
	for name, r := range s.macros.macros {
		if dm, ok := r.m.(DataMacro); ok && s.pluginOn(r.pluginID) {
			if _, ok := s.DataKindOf(dm.DataKind()); ok {
				out[name] = dm
			}
		}
	}
	return out
}

// conditionalMacros: the names of blocks that show their content only to
// some readers (ConditionalMacro); unknown blocks count as such, as their
// meaning is not known (a plugin switched off).
func (s *Service) conditionalMacros() func(string) bool {
	s.macros.mu.RLock()
	defer s.macros.mu.RUnlock()
	open := map[string]bool{}
	for name, r := range s.macros.macros {
		if c, ok := r.m.(ConditionalMacro); (!ok || !c.Conditional()) && s.pluginOn(r.pluginID) {
			open[name] = true
		}
	}
	return func(name string) bool { return !open[name] }
}

// writeMacroData replaces the entries of the blocks of element id in lang
// with those of body (in the transaction tx of the revision rev). Only
// kinds of active blocks are replaced; entries of plugins stay.
func (s *Service) writeMacroData(ctx context.Context, tx pgx.Tx, id int64, typ, lang string, rev int, body json.RawMessage) error {
	dms := s.dataMacros()
	if len(dms) == 0 {
		return nil
	}
	kinds := map[string]bool{}
	for _, dm := range dms {
		kinds[dm.DataKind()] = true
	}
	kindList := make([]string, 0, len(kinds))
	for k := range kinds {
		kindList = append(kindList, k)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM element_data WHERE element_id=$1 AND lang=$2 AND source=$3 AND kind = ANY($4)`, id, lang, SourceMacro, kindList); err != nil {
		return err
	}
	mh, ok := s.Type(typ).(MacroHTMLer)
	if !ok {
		return nil
	}
	src, ok := mh.MacroHTML(body)
	if !ok {
		return nil
	}
	perKind := map[string]int{}
	batch := &pgx.Batch{}
	for _, u := range macroUses(src, s.conditionalMacros()) {
		dm, ok := dms[u.Name]
		if !ok {
			continue
		}
		k, _ := s.DataKindOf(dm.DataKind())
		if perKind[k.Kind] >= k.MaxPerElement {
			continue
		}
		data, err := dm.Data(ctx, DataUse{Lang: lang, Params: u.Params, Text: u.Text, HTML: u.HTML, Index: u.Index})
		if err != nil || data == nil {
			continue // the block shows its error where it is
		}
		r, err := checkData(k, data)
		if err != nil {
			continue
		}
		perKind[k.Kind]++
		batch.Queue(`INSERT INTO element_data(element_id, lang, kind, source, idx, rev, data, state, day, at, person, num, ref, label)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, id, lang, k.Kind, SourceMacro, u.Index, rev, r.data, r.state, r.day, r.at, r.person, r.num, r.ref, r.label)
	}
	if batch.Len() == 0 {
		return nil
	}
	return tx.SendBatch(ctx, batch).Close()
}

// writeMacroDataAll writes the entries of every language of element id
// from its latest revisions (copies, imports, the rebuild).
func (s *Service) writeMacroDataAll(ctx context.Context, tx pgx.Tx, id int64) error {
	if len(s.dataMacros()) == 0 {
		return nil
	}
	var typ string
	// acl: internal – the element being saved (rights checked by the caller)
	if err := tx.QueryRow(ctx, `SELECT type FROM elements WHERE id=$1`, id).Scan(&typ); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (lang) lang, rev, body FROM element_revisions WHERE element_id=$1 ORDER BY lang, rev DESC`, id)
	if err != nil {
		return err
	}
	type rv struct {
		lang string
		rev  int
		body json.RawMessage
	}
	revs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (rv, error) {
		var x rv
		return x, r.Scan(&x.lang, &x.rev, &x.body)
	})
	if err != nil {
		return err
	}
	for _, r := range revs {
		if err := s.writeMacroData(ctx, tx, id, typ, r.lang, r.rev, r.body); err != nil {
			return err
		}
	}
	return nil
}

// PutData writes an entry of a plugin (kind of the plugin, source = the
// plugin id; idx numbers the entries of the element and language). tx may
// be the transaction of the change it follows from (nil: an own one).
func (s *Service) PutData(ctx context.Context, tx pgx.Tx, pluginID string, elementID int64, lang, kind string, idx int, data map[string]any) error {
	k, ok := s.DataKindOf(kind)
	if !ok || k.Plugin != pluginID {
		return apperr.BadRequest("data kind %q is not one of the plugin %s", kind, pluginID)
	}
	if !s.validLang(lang) || idx < 1 || idx > k.MaxPerElement {
		return apperr.BadRequest("data: invalid language or number")
	}
	r, err := checkData(k, data)
	if err != nil {
		return err
	}
	q := `INSERT INTO element_data(element_id, lang, kind, source, idx, rev, data, state, day, at, person, num, ref, label)
		VALUES ($1,$2,$3,$4,$5,0,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (element_id, lang, kind, source, idx) DO UPDATE SET data=EXCLUDED.data, state=EXCLUDED.state, day=EXCLUDED.day, at=EXCLUDED.at,
			person=EXCLUDED.person, num=EXCLUDED.num, ref=EXCLUDED.ref, label=EXCLUDED.label, updated_at=now()`
	args := []any{elementID, lang, kind, pluginID, idx, r.data, r.state, r.day, r.at, r.person, r.num, r.ref, r.label}
	if tx != nil {
		_, err = tx.Exec(ctx, q, args...)
	} else {
		_, err = s.pool.Exec(ctx, q, args...)
	}
	return err
}

// DeleteData removes entries of a plugin (idx 0: all of the element and
// language; lang "": all languages).
func (s *Service) DeleteData(ctx context.Context, tx pgx.Tx, pluginID string, elementID int64, lang, kind string, idx int) error {
	k, ok := s.DataKindOf(kind)
	if !ok || k.Plugin != pluginID {
		return apperr.BadRequest("data kind %q is not one of the plugin %s", kind, pluginID)
	}
	q := `DELETE FROM element_data WHERE element_id=$1 AND kind=$2 AND source=$3 AND ($4 = '' OR lang=$4) AND ($5 = 0 OR idx=$5)`
	var err error
	if tx != nil {
		_, err = tx.Exec(ctx, q, elementID, kind, pluginID, lang, idx)
	} else {
		_, err = s.pool.Exec(ctx, q, elementID, kind, pluginID, lang, idx)
	}
	return err
}

// DataQuery asks for entries of a kind.
type DataQuery struct {
	Kind string
	// Category: only elements in it (with subcategories unless Direct)
	Category int64
	Direct   bool
	Element  int64
	Lang     string
	States   []string
	// From and To limit the column day (inclusive)
	From, To string
	Person   int64
	Ref      int64
	// Q: words in the label or the data
	Q string
	// Sort: day, -day, at, -at, num, -num, label, state, page (default: -updated)
	Sort          string
	Limit, Offset int
	// Hidden: also entries of hidden elements (blocks)
	Hidden bool
}

// DataEntry is an entry as a query gives it.
type DataEntry struct {
	ElementID int64          `json:"element_id"`
	Title     string         `json:"title"`
	Path      string         `json:"path"`
	Lang      string         `json:"lang"`
	Source    string         `json:"source"`
	Index     int            `json:"index"`
	Data      map[string]any `json:"data"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// DataResult: the entries, their total and whether the kind is complete
// (false while it is being built after a new kind or version).
type DataResult struct {
	Kind     DataKind    `json:"kind"`
	Items    []DataEntry `json:"items"`
	Total    int         `json:"total"`
	Complete bool        `json:"complete"`
	// People are the names of the people in fields of the type person
	// that the reader may see (PeopleNames); the others are left out.
	People map[int64]string `json:"people"`
}

// DataQueryMax is the most entries of one answer.
const DataQueryMax = 500

var dataSorts = map[string]string{
	"day": "d.day ASC NULLS LAST", "-day": "d.day DESC NULLS LAST", "at": "d.at ASC NULLS LAST", "-at": "d.at DESC NULLS LAST",
	"num": "d.num ASC NULLS LAST", "-num": "d.num DESC NULLS LAST", "label": "lower(d.label) ASC NULLS LAST", "state": "d.state ASC NULLS LAST",
	"page": "e.id, d.idx", "": "d.updated_at DESC", "-updated": "d.updated_at DESC",
}

// QueryData answers a query with the permissions of the caller: only
// entries of elements the caller may see (category, own rights,
// classification), not in the trash, hidden ones only if asked.
func (s *Service) QueryData(ctx context.Context, q DataQuery) (*DataResult, error) {
	k, ok := s.DataKindOf(q.Kind)
	if !ok {
		return nil, apperr.NotFound("data kind")
	}
	order, ok := dataSorts[q.Sort]
	if !ok {
		return nil, apperr.BadRequest("sort: one of day, -day, at, -at, num, -num, label, state, page")
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	q.Limit = min(q.Limit, DataQueryMax)
	if q.Offset < 0 || q.Offset > 100000 {
		return nil, apperr.BadRequest("offset: 0 to 100000")
	}
	for _, d := range []string{q.From, q.To} {
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return nil, apperr.BadRequest("from and to: a day as YYYY-MM-DD")
			}
		}
	}
	if len(q.States) > 20 || len(q.Q) > 200 {
		return nil, apperr.BadRequest("too many states or a too long search")
	}
	f, err := s.acl.ElementFilter(ctx, identity.From(ctx), acl.View)
	if err != nil {
		return nil, err
	}
	q.Q = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(q.Q))
	lang := q.Lang
	if lang == "" {
		lang = s.DefaultLanguage()
	}
	args := []any{k.Kind, q.Category, q.Direct, q.Element, q.Lang, q.States, q.From, q.To, q.Person, q.Ref, q.Q, q.Hidden}
	where := `d.kind = $1::text AND e.deleted_at IS NULL
		AND ($2::bigint = 0 OR ($3::bool AND e.category_id = $2::bigint) OR (NOT $3::bool AND e.category_id IN (SELECT c.id FROM categories c WHERE c.path <@ (SELECT path FROM categories WHERE id = $2::bigint))))
		AND ($4::bigint = 0 OR e.id = $4::bigint) AND ($5::text = '' OR d.lang = $5::text)
		AND (COALESCE(cardinality($6::text[]), 0) = 0 OR d.state = ANY($6::text[]))
		AND ($7::text = '' OR d.day >= $7::text::date) AND ($8::text = '' OR d.day <= $8::text::date)
		AND ($9::bigint = 0 OR d.person = $9::bigint) AND ($10::bigint = 0 OR d.ref = $10::bigint)
		AND ($11::text = '' OR d.label ILIKE '%' || $11::text || '%' OR d.data::text ILIKE '%' || $11::text || '%')
		AND ($12::bool OR NOT e.hidden)
		AND ` + f.SQL("e", &args)
	var total int
	// acl: filtered (ElementFilter)
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM element_data d JOIN elements e ON e.id = d.element_id WHERE `+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, lang, q.Limit, q.Offset)
	n := len(args)
	// acl: filtered (ElementFilter)
	rows, err := s.pool.Query(ctx, `SELECT e.id, COALESCE(NULLIF(e.titles->>$`+strconv.Itoa(n-2)+`::text, ''), e.titles->>e.original_lang, ''), d.lang, d.source, d.idx, d.data, d.updated_at
		FROM element_data d JOIN elements e ON e.id = d.element_id WHERE `+where+`
		ORDER BY `+order+`, e.id, d.idx LIMIT $`+strconv.Itoa(n-1)+` OFFSET $`+strconv.Itoa(n), args...)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (DataEntry, error) {
		var x DataEntry
		var raw []byte
		if err := r.Scan(&x.ElementID, &x.Title, &x.Lang, &x.Source, &x.Index, &raw, &x.UpdatedAt); err != nil {
			return x, err
		}
		x.Path = fmt.Sprintf("/e/%d", x.ElementID)
		return x, json.Unmarshal(raw, &x.Data)
	})
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []DataEntry{}
	}
	var ver int
	var done bool
	_ = s.pool.QueryRow(ctx, `SELECT version, done FROM data_kinds WHERE kind=$1`, k.Kind).Scan(&ver, &done)
	var ids []int64
	for _, f := range k.Fields {
		if f.Type != DataPerson {
			continue
		}
		for _, it := range items {
			if n, ok := it.Data[f.Key].(float64); ok && n >= 1 && n <= 1<<53 {
				ids = append(ids, int64(n))
			}
		}
	}
	people, err := s.PeopleNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	return &DataResult{Kind: k, Items: items, Total: total, Complete: ver == k.Version && done, People: people}, nil
}

// RebuildData builds the entries of the blocks of kinds that are new or
// have a new version, in batches of at most batch elements per call (a
// job runs it again until everything is built). It reports whether there
// is more to do.
func (s *Service) RebuildData(ctx context.Context, batch int) (bool, error) {
	var todo []DataKind
	for _, k := range s.DataKinds() {
		var ver int
		var done bool
		err := s.pool.QueryRow(ctx, `SELECT version, done FROM data_kinds WHERE kind=$1`, k.Kind).Scan(&ver, &done)
		if errors.Is(err, pgx.ErrNoRows) || ver != k.Version {
			if _, err := s.pool.Exec(ctx, `INSERT INTO data_kinds(kind, version, cursor, done) VALUES ($1,$2,0,false)
				ON CONFLICT (kind) DO UPDATE SET version=EXCLUDED.version, cursor=0, done=false, started_at=now()`, k.Kind, k.Version); err != nil {
				return false, err
			}
			done = false
		} else if err != nil {
			return false, err
		}
		if !done {
			todo = append(todo, k)
		}
	}
	if len(todo) == 0 {
		return false, nil
	}
	// the elements of the types with blocks in HTML, after the lowest cursor
	var types []string
	s.types.mu.RLock()
	all := make([]string, 0, len(s.types.types))
	for id := range s.types.types {
		all = append(all, id)
	}
	s.types.mu.RUnlock()
	for _, id := range all {
		if _, ok := s.Type(id).(MacroHTMLer); ok {
			types = append(types, id)
		}
	}
	var cursor int64 = -1
	for _, k := range todo {
		var c int64
		if err := s.pool.QueryRow(ctx, `SELECT cursor FROM data_kinds WHERE kind=$1`, k.Kind).Scan(&c); err != nil {
			return false, err
		}
		if cursor < 0 || c < cursor {
			cursor = c
		}
	}
	// acl: internal – building the data (system job; readers are filtered in QueryData)
	rows, err := s.pool.Query(ctx, `SELECT id FROM elements WHERE id > $1 AND type = ANY($2) AND deleted_at IS NULL ORDER BY id LIMIT $3`, cursor, types, batch)
	if err != nil {
		return false, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			// the element locked: a change at the same time waits (and then writes its own)
			// acl: internal – building the data (system job)
			if _, err := tx.Exec(ctx, `SELECT 1 FROM elements WHERE id=$1 FOR UPDATE`, id); err != nil {
				return err
			}
			return s.writeMacroDataAll(ctx, tx, id)
		})
		if err != nil {
			return false, err
		}
	}
	last := cursor
	if len(ids) > 0 {
		last = ids[len(ids)-1]
	}
	finished := len(ids) < batch
	for _, k := range todo {
		if _, err := s.pool.Exec(ctx, `UPDATE data_kinds SET cursor=GREATEST(cursor, $2), done=$3, built_at=CASE WHEN $3 THEN now() ELSE built_at END WHERE kind=$1 AND version=$4`,
			k.Kind, last, finished, k.Version); err != nil {
			return false, err
		}
	}
	return !finished, nil
}

// ResetData marks a kind to be built again (all kinds when kind is "").
func (s *Service) ResetData(ctx context.Context, kind string) error {
	if kind != "" {
		if _, ok := s.DataKindOf(kind); !ok {
			return apperr.NotFound("data kind")
		}
	}
	_, err := s.pool.Exec(ctx, `UPDATE data_kinds SET cursor=0, done=false, started_at=now() WHERE $1 = '' OR kind=$1`, kind)
	return err
}

// pluginOn: whether the plugin is active (all are without a check).
func (s *Service) pluginOn(id string) bool { return s.enabled == nil || s.enabled(id) }
