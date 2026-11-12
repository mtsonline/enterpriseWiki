package core_properties

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/db"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/webhook"
)

// Value is a stored value as shown.
type Value struct {
	Key   string          `json:"key"`
	Type  string          `json:"type"`
	DefID *int64          `json:"def_id"`
	Label string          `json:"label"` // free properties: their name
	Value json.RawMessage `json:"value"`
}

// Input is the field "core.properties" of an editor or PUT /elements/{id}:
// the values to set (null removes one) and, for new free properties, their
// type and name. Keys that are not sent stay as they are (also those the
// caller cannot see).
type Input struct {
	Values map[string]json.RawMessage `json:"values"`
	Free   map[string]FreeMeta        `json:"free"`
}

// FreeMeta describes a new free property.
type FreeMeta struct {
	Type  string `json:"type"`
	Label string `json:"label"`
}

// change is a checked change, stored by Store.
type change struct {
	Set []setValue `json:"set"`
	Del []string   `json:"del"`
	// definitions computed on store (numbering, formulas) and defaults of a new element
	Auto []int64 `json:"auto"`
	New  bool    `json:"new"`
}

// keys returns the changed properties.
func (c *change) keys() []string {
	out := []string{}
	for _, v := range c.Set {
		out = append(out, v.Key)
	}
	return append(out, c.Del...)
}

type setValue struct {
	Key   string          `json:"key"`
	Type  string          `json:"type"`
	DefID *int64          `json:"def_id"`
	Label string          `json:"label"`
	Raw   json.RawMessage `json:"raw"`
}

// load returns the stored values of elements.
func (p *Plugin) load(ctx context.Context, q db.Querier, ids []int64) (map[int64][]Value, error) {
	rows, err := q.Query(ctx, `SELECT element_id, key, type, def_id, label, raw FROM plugin_prop_values WHERE element_id = ANY($1) ORDER BY element_id, position, key`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Value{}
	for rows.Next() {
		var id int64
		var v Value
		if err := rows.Scan(&id, &v.Key, &v.Type, &v.DefID, &v.Label, &v.Value); err != nil {
			return nil, err
		}
		out[id] = append(out[id], v)
	}
	return out, rows.Err()
}

// field is the ElementField of the editors.
type field struct{ p *Plugin }

func (field) ID() string { return "core.properties" }

// Always: defaults and computed properties also apply to elements created
// without the field (API, other plugins).
func (field) Always() bool { return true }

func (f field) Check(ctx context.Context, svc *content.Service, elementID, categoryID int64, typ string, raw json.RawMessage) (json.RawMessage, error) {
	ch, err := f.p.checkInput(ctx, svc, elementID, categoryID, typ, raw)
	if err != nil || ch == nil {
		return nil, err
	}
	return json.Marshal(ch)
}

func (f field) Store(ctx context.Context, tx pgx.Tx, elementID int64, raw json.RawMessage) error {
	var ch change
	if err := json.Unmarshal(raw, &ch); err != nil {
		return err
	}
	return f.p.store(ctx, tx, elementID, &ch)
}

// checkInput validates an Input (raw nil: nothing sent) with the rights of the caller.
func (p *Plugin) checkInput(ctx context.Context, svc *content.Service, elementID, categoryID int64, typ string, raw json.RawMessage) (*change, error) {
	var in Input
	sent := len(raw) > 0 && string(raw) != "null"
	if sent {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			return nil, apperr.BadRequest("properties: %v", err)
		}
	}
	defs, err := p.defsFor(ctx, p.host.DB, categoryID, typ)
	if err != nil {
		return nil, err
	}
	pr := identity.From(ctx)
	ch := &change{New: elementID == 0}
	existing := map[string]Value{}
	if elementID != 0 {
		m, err := p.load(ctx, p.host.DB, []int64{elementID})
		if err != nil {
			return nil, err
		}
		for _, v := range m[elementID] {
			existing[v.Key] = v
		}
	}
	free, err := p.freeSetting(ctx, p.host.DB, categoryID)
	if err != nil {
		return nil, err
	}
	mayFree := free && p.host.Can(ctx, categoryID, "core.properties.free")
	keys := make([]string, 0, len(in.Values))
	for k := range in.Values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if len(keys) > MaxProps {
		return nil, apperr.BadRequest("at most %d properties", MaxProps)
	}
	for _, k := range keys {
		v := in.Values[k]
		if !keyRe.MatchString(k) {
			return nil, apperr.BadRequest("invalid property key %q", k)
		}
		if slices.Contains(Builtins, k) {
			return nil, apperr.BadRequest("%q is a built-in property (saved with the element)", k)
		}
		remove := len(v) == 0 || string(v) == "null"
		if d, ok := defs[k]; ok {
			if !d.canEdit(pr) {
				return nil, apperr.Forbidden("you may not change the property %q", d.Label("en"))
			}
			t, _ := TypeByID(d.Type)
			if t.Computed {
				if !remove {
					return nil, apperr.BadRequest("%s is computed", d.Label("en"))
				}
				continue
			}
			tv, err := CheckValue(d.Label("en"), t, d.Options, v)
			if err != nil {
				return nil, err
			}
			if tv == nil {
				if d.Required {
					return nil, apperr.BadRequest("%s is required", d.Label("en"))
				}
				if _, had := existing[k]; had {
					ch.Del = append(ch.Del, k)
				}
				continue
			}
			ch.Set = append(ch.Set, setValue{Key: k, Type: d.Type, DefID: &d.ID, Raw: tv.Raw})
			continue
		}
		// a free property
		old, had := existing[k]
		if remove {
			if had {
				if old.DefID == nil && !p.host.Can(ctx, categoryID, "core.properties.free") {
					return nil, apperr.Forbidden("you may not change free properties here")
				}
				ch.Del = append(ch.Del, k)
			}
			continue
		}
		if !mayFree {
			return nil, apperr.Forbidden("only defined properties can be used here; %q is not defined", k)
		}
		meta := in.Free[k]
		tname, label := meta.Type, strings.TrimSpace(meta.Label)
		if had {
			if tname == "" {
				tname = old.Type
			}
			if label == "" {
				label = old.Label
			}
		}
		t, ok := TypeByID(tname)
		if !ok || t.Computed {
			return nil, apperr.BadRequest("%s: choose a type", k)
		}
		if label == "" {
			label = k
		}
		if utf8.RuneCountInString(label) > MaxLabelLen || strings.ContainsAny(label, "\r\n\x00") {
			return nil, apperr.BadRequest("%s: name too long", k)
		}
		tv, err := CheckValue(label, t, Options{Multiple: true}, v)
		if err != nil {
			return nil, err
		}
		if tv == nil {
			if had {
				ch.Del = append(ch.Del, k)
			}
			continue
		}
		ch.Set = append(ch.Set, setValue{Key: k, Type: tname, Label: label, Raw: tv.Raw})
	}
	for k := range in.Free {
		if _, ok := in.Values[k]; !ok {
			return nil, apperr.BadRequest("free property %q without value", k)
		}
	}
	setKeys := map[string]bool{}
	for _, s := range ch.Set {
		setKeys[s.Key] = true
	}
	for _, d := range sortedDefs(defs, "en") {
		t, _ := TypeByID(d.Type)
		switch {
		case t.Computed:
			ch.Auto = append(ch.Auto, d.ID)
		case setKeys[d.Key]:
		case ch.New && len(d.Default) > 0:
			ch.Set = append(ch.Set, setValue{Key: d.Key, Type: d.Type, DefID: &d.ID, Raw: d.Default})
		case sent && d.Required && d.canEdit(pr) && !slices.Contains(ch.Del, d.Key):
			if _, had := existing[d.Key]; !had {
				return nil, apperr.BadRequest("%s is required", d.Label("en"))
			}
		}
	}
	if len(existing)+len(ch.Set) > MaxProps*2 {
		return nil, apperr.BadRequest("too many properties")
	}
	if len(ch.Set) == 0 && len(ch.Del) == 0 && len(ch.Auto) == 0 {
		return nil, nil
	}
	return ch, nil
}

// store writes a checked change (in the transaction of the element).
func (p *Plugin) store(ctx context.Context, tx pgx.Tx, elementID int64, ch *change) error {
	uid := identity.From(ctx).ID()
	var by *int64
	if uid != 0 {
		by = &uid
	}
	for _, k := range ch.Del {
		tag, err := tx.Exec(ctx, `DELETE FROM plugin_prop_values WHERE element_id=$1 AND key=$2`, elementID, k)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO plugin_prop_history(element_id, key, raw, changed_by) VALUES ($1,$2,NULL,$3)`, elementID, k, by); err != nil {
				return err
			}
		}
	}
	for i, s := range ch.Set {
		if err := p.put(ctx, tx, elementID, s, i, by); err != nil {
			return err
		}
	}
	if len(ch.Auto) > 0 {
		if err := p.compute(ctx, tx, elementID, ch.Auto, by); err != nil {
			return err
		}
	}
	return nil
}

// put writes one value (typed columns from its type) and its history.
func (p *Plugin) put(ctx context.Context, tx pgx.Tx, elementID int64, s setValue, pos int, by *int64) error {
	t, _ := TypeByID(s.Type)
	var tv *Typed
	if t.Computed {
		tv = computedTyped(t, s.Raw)
	} else {
		var err error
		if tv, err = CheckValue(s.Key, t, Options{Multiple: true}, s.Raw); err != nil || tv == nil {
			return apperr.BadRequest("%s: invalid stored value", s.Key)
		}
	}
	var same bool
	_ = tx.QueryRow(ctx, `SELECT raw = $3::jsonb FROM plugin_prop_values WHERE element_id=$1 AND key=$2`, elementID, s.Key, []byte(tv.Raw)).Scan(&same)
	if _, err := tx.Exec(ctx, `INSERT INTO plugin_prop_values(element_id, key, type, def_id, label, position, raw, txt, num, ts, ts_end, arr, refs, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (element_id, key) DO UPDATE SET type=EXCLUDED.type, def_id=EXCLUDED.def_id, label=EXCLUDED.label, raw=EXCLUDED.raw, txt=EXCLUDED.txt,
			num=EXCLUDED.num, ts=EXCLUDED.ts, ts_end=EXCLUDED.ts_end, arr=EXCLUDED.arr, refs=EXCLUDED.refs,
			updated_at = CASE WHEN plugin_prop_values.raw = EXCLUDED.raw THEN plugin_prop_values.updated_at ELSE now() END,
			updated_by = CASE WHEN plugin_prop_values.raw = EXCLUDED.raw THEN plugin_prop_values.updated_by ELSE EXCLUDED.updated_by END`,
		elementID, s.Key, s.Type, s.DefID, s.Label, pos, []byte(tv.Raw), tv.Txt, tv.Num, tv.TS, tv.TSEnd, tv.Arr, tv.Refs, by); err != nil {
		return err
	}
	if !same {
		if _, err := tx.Exec(ctx, `INSERT INTO plugin_prop_history(element_id, key, raw, changed_by) VALUES ($1,$2,$3,$4)`, elementID, s.Key, []byte(tv.Raw), by); err != nil {
			return err
		}
	}
	return nil
}

func computedTyped(t TypeDef, raw json.RawMessage) *Typed {
	tv := &Typed{Raw: raw}
	if t.ID == "autonumber" {
		var s string
		_ = json.Unmarshal(raw, &s)
		tv.Txt = &s
		digits := strings.TrimLeftFunc(s, func(r rune) bool { return r < '0' || r > '9' })
		if n, err := strconv.ParseFloat(digits, 64); err == nil {
			tv.Num = &n
		}
	} else {
		var f float64
		_ = json.Unmarshal(raw, &f)
		tv.Num = &f
	}
	return tv
}

// compute sets automatic numbers (once) and formulas of an element.
func (p *Plugin) compute(ctx context.Context, tx pgx.Tx, elementID int64, defIDs []int64, by *int64) error {
	rows, err := tx.Query(ctx, `SELECT `+defCols+` FROM plugin_prop_defs d WHERE d.id = ANY($1)`, defIDs)
	if err != nil {
		return err
	}
	defs, err := pgx.CollectRows(rows, scanDef)
	if err != nil {
		return err
	}
	var nums map[string]float64
	for _, d := range defs {
		switch d.Type {
		case "autonumber":
			var has bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM plugin_prop_values WHERE element_id=$1 AND key=$2)`, elementID, d.Key).Scan(&has); err != nil {
				return err
			}
			if has {
				continue
			}
			var n int64
			if err := tx.QueryRow(ctx, `INSERT INTO plugin_prop_seq(def_id, next) VALUES ($1, 2) ON CONFLICT (def_id) DO UPDATE SET next = plugin_prop_seq.next + 1
				RETURNING next - 1`, d.ID).Scan(&n); err != nil {
				return err
			}
			s := strconv.FormatInt(n, 10)
			for len(s) < d.Options.Digits {
				s = "0" + s
			}
			id := d.ID
			if err := p.put(ctx, tx, elementID, setValue{Key: d.Key, Type: d.Type, DefID: &id, Raw: mustJSON(d.Options.Prefix + s)}, 1000, by); err != nil {
				return err
			}
		case "formula":
			if nums == nil {
				if nums, err = numbers(ctx, tx, elementID); err != nil {
					return err
				}
			}
			f, err := p.formula(d.Options.Formula)
			if err != nil {
				continue
			}
			x, err := f.Eval(nums)
			id := d.ID
			if err != nil {
				if _, err := tx.Exec(ctx, `DELETE FROM plugin_prop_values WHERE element_id=$1 AND key=$2`, elementID, d.Key); err != nil {
					return err
				}
				continue
			}
			if d.Options.Decimals != nil {
				pw := math.Pow(10, float64(*d.Options.Decimals))
				x = math.Round(x*pw) / pw
			}
			if err := p.put(ctx, tx, elementID, setValue{Key: d.Key, Type: d.Type, DefID: &id, Raw: mustJSON(x)}, 1000, by); err != nil {
				return err
			}
			nums[d.Key] = x
		}
	}
	return nil
}

// numbers are the values of an element for formulas: numbers, dates as
// days since 1970-01-01, yes/no as 1/0.
func numbers(ctx context.Context, q db.Querier, elementID int64) (map[string]float64, error) {
	rows, err := q.Query(ctx, `SELECT key, num, ts FROM plugin_prop_values WHERE element_id=$1`, elementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var k string
		var n *float64
		var ts *time.Time
		if err := rows.Scan(&k, &n, &ts); err != nil {
			return nil, err
		}
		switch {
		case n != nil:
			out[k] = *n
		case ts != nil:
			out[k] = math.Floor(float64(ts.Unix()) / 86400)
		}
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- shown values

// Shown is a property as shown for an element.
type Shown struct {
	Key        string          `json:"key"`
	Label      string          `json:"label"`
	Type       string          `json:"type"`
	DefID      *int64          `json:"def_id"`
	Value      json.RawMessage `json:"value"`
	Options    *Options        `json:"options,omitempty"`
	Group      string          `json:"group,omitempty"`
	Help       string          `json:"help,omitempty"`
	Required   bool            `json:"required,omitempty"`
	ShowOnPage bool            `json:"show_on_page"`
	CanEdit    bool            `json:"can_edit"`
}

// Ref is the name of a referenced user, group, element or category.
type Ref struct {
	Name string `json:"name"`
	Link string `json:"link,omitempty"`
}

// visible returns the properties of an element the caller may see (with
// the definitions without a value, for editors).
func (p *Plugin) visible(ctx context.Context, elementID, categoryID int64, typ, lang string, canEditElement bool) ([]Shown, error) {
	defs, err := p.defsFor(ctx, p.host.DB, categoryID, typ)
	if err != nil {
		return nil, err
	}
	vals, err := p.load(ctx, p.host.DB, []int64{elementID})
	if err != nil {
		return nil, err
	}
	pr := identity.From(ctx)
	out := []Shown{}
	seen := map[string]bool{}
	for _, d := range sortedDefs(defs, lang) {
		if !d.canView(pr) {
			continue
		}
		seen[d.Key] = true
		o := d.Options
		s := Shown{Key: d.Key, Label: d.Label(lang), Type: d.Type, DefID: &d.ID, Options: &o, Group: d.Group, Help: pick(d.Help, lang),
			Required: d.Required, ShowOnPage: d.ShowOnPage, CanEdit: canEditElement && d.canEdit(pr)}
		for _, v := range vals[elementID] {
			if v.Key == d.Key {
				s.Value = v.Value
			}
		}
		out = append(out, s)
	}
	for _, v := range vals[elementID] {
		if seen[v.Key] {
			continue
		}
		if v.DefID != nil {
			continue // a definition that does not apply any more or is hidden
		}
		out = append(out, Shown{Key: v.Key, Label: v.Label, Type: v.Type, Value: v.Value, ShowOnPage: true, CanEdit: canEditElement})
	}
	return out, nil
}

// refs returns the names of referenced users, groups, elements and
// categories; elements and categories only if the caller may see them.
func (p *Plugin) refs(ctx context.Context, list []Shown, lang string) (map[string]Ref, error) {
	ids := map[string][]int64{}
	for _, s := range list {
		switch s.Type {
		case "user", "group", "element", "category":
			if x, ok := asIDs(s.Value); ok {
				ids[s.Type] = append(ids[s.Type], x...)
			}
		}
	}
	return p.refNames(ctx, ids, lang)
}

func (p *Plugin) refNames(ctx context.Context, ids map[string][]int64, lang string) (map[string]Ref, error) {
	out := map[string]Ref{}
	pr := identity.From(ctx)
	q := func(kind, sql string, list []int64, link func(int64) string) error {
		if len(list) == 0 {
			return nil
		}
		rows, err := p.host.DB.Query(ctx, sql, list)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				return err
			}
			r := Ref{Name: name}
			if link != nil {
				r.Link = link(id)
			}
			out[kind+":"+strconv.FormatInt(id, 10)] = r
		}
		return rows.Err()
	}
	// names of people only for signed-in readers
	if pr != nil && pr.UserID != 0 {
		if err := q("user", `SELECT id, display_name FROM users WHERE id = ANY($1)`, ids["user"], func(id int64) string { return "/u/" + strconv.FormatInt(id, 10) }); err != nil {
			return nil, err
		}
		if err := q("group", `SELECT id, name FROM groups WHERE id = ANY($1)`, ids["group"], nil); err != nil {
			return nil, err
		}
	}
	if len(ids["element"]) > 0 {
		rows, err := p.host.DB.Query(ctx, `SELECT e.id, e.category_id, COALESCE(e.titles->>$2, e.titles->>e.original_lang, '') FROM elements e
			JOIN categories c ON c.id = e.category_id WHERE e.id = ANY($1) AND e.deleted_at IS NULL AND c.deleted_at IS NULL`, ids["element"], lang)
		if err != nil {
			return nil, err
		}
		type el struct {
			id, cat int64
			title   string
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (el, error) {
			var x el
			return x, r.Scan(&x.id, &x.cat, &x.title)
		})
		if err != nil {
			return nil, err
		}
		cats := []int64{}
		for _, x := range list {
			cats = append(cats, x.cat)
		}
		ok, err := p.host.ACL.Filter(ctx, pr, cats, acl.View)
		if err != nil {
			return nil, err
		}
		for _, x := range list {
			if slices.Contains(ok, x.cat) {
				out["element:"+strconv.FormatInt(x.id, 10)] = Ref{Name: x.title, Link: "/e/" + strconv.FormatInt(x.id, 10)}
			}
		}
	}
	if len(ids["category"]) > 0 {
		ok, err := p.host.ACL.Filter(ctx, pr, ids["category"], acl.View)
		if err != nil {
			return nil, err
		}
		if err := q("category", `SELECT id, COALESCE(titles->>'`+safeLang(lang)+`', titles->>'en', '') FROM categories WHERE id = ANY($1) AND deleted_at IS NULL`, ok,
			func(id int64) string { return "/c/" + strconv.FormatInt(id, 10) }); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// safeLang returns lang if it is a plain language code (it is put into SQL as a literal key).
func safeLang(lang string) string {
	if langRe.MatchString(lang) && !strings.ContainsAny(lang, "'\\") {
		return lang
	}
	return "en"
}

func (p *Plugin) routesValues() {
	host := p.host
	// GET /elements/{id}?lang= – the properties of an element (and
	// definitions without value), names of references, whether the caller
	// may change them and add free properties
	host.Handle("GET", "/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		lang := r.URL.Query().Get("lang")
		cat, err := host.Content.ElementCategory(ctx, id, acl.View)
		if err != nil {
			return nil, err
		}
		var typ string
		if err := host.DB.QueryRow(ctx, `SELECT type FROM elements WHERE id=$1`, id).Scan(&typ); err != nil {
			return nil, err
		}
		perms, _ := host.ACL.Perms(ctx, identity.From(ctx), cat)
		canEdit := perms&acl.Edit != 0
		items, err := p.visible(ctx, id, cat, typ, lang, canEdit)
		if err != nil {
			return nil, err
		}
		names, err := p.refs(ctx, items, lang)
		if err != nil {
			return nil, err
		}
		free, err := p.freeSetting(ctx, host.DB, cat)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items, "refs": names, "can_edit": canEdit,
			"can_add_free": canEdit && free && host.Can(ctx, cat, "core.properties.free"), "category_id": cat}, nil
	})
	// GET /new?category=ID&type=T&lang= – the definitions for a new element
	host.Handle("GET", "/new", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		cat, _ := strconv.ParseInt(r.URL.Query().Get("category"), 10, 64)
		typ := r.URL.Query().Get("type")
		lang := r.URL.Query().Get("lang")
		if err := host.Content.Require(ctx, cat, acl.Create); err != nil {
			return nil, err
		}
		defs, err := p.defsFor(ctx, host.DB, cat, typ)
		if err != nil {
			return nil, err
		}
		pr := identity.From(ctx)
		items := []Shown{}
		for _, d := range sortedDefs(defs, lang) {
			if !d.canView(pr) {
				continue
			}
			o := d.Options
			items = append(items, Shown{Key: d.Key, Label: d.Label(lang), Type: d.Type, DefID: &d.ID, Options: &o, Group: d.Group, Help: pick(d.Help, lang),
				Required: d.Required, ShowOnPage: d.ShowOnPage, CanEdit: d.canEdit(pr), Value: d.Default})
		}
		names, err := p.refs(ctx, items, lang)
		if err != nil {
			return nil, err
		}
		free, err := p.freeSetting(ctx, host.DB, cat)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items, "refs": names, "can_edit": true, "can_add_free": free && host.Can(ctx, cat, "core.properties.free"), "category_id": cat}, nil
	})
	// PUT /elements/{id} – change properties without a new version of the
	// element (Input: values, free)
	host.Handle("PUT", "/elements/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		cat, err := host.Content.ElementCategory(ctx, id, acl.Edit)
		if err != nil {
			return nil, err
		}
		var typ string
		if err := host.DB.QueryRow(ctx, `SELECT type FROM elements WHERE id=$1`, id).Scan(&typ); err != nil {
			return nil, err
		}
		raw, err := readBody(r)
		if err != nil {
			return nil, err
		}
		ch, err := p.checkInput(ctx, host.Content, id, cat, typ, raw)
		if err != nil {
			return nil, err
		}
		if ch != nil {
			before, _ := p.load(ctx, host.DB, []int64{id})
			err = db.Tx(ctx, host.DB, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `SELECT 1 FROM elements WHERE id=$1 FOR UPDATE`, id); err != nil {
					return err
				}
				if err := p.store(ctx, tx, id, ch); err != nil {
					return err
				}
				if host.Webhooks != nil { // with the change: webhooks and workflows ("status becomes …")
					host.Webhooks.EmitTx(ctx, tx, webhook.Event{Name: "element.properties", CategoryID: cat,
						Object: webhook.Object{Type: "element", ID: id, URL: fmt.Sprintf("/e/%d", id)}, Data: map[string]any{"keys": ch.keys()}})
				}
				return audit.Log(ctx, tx, "element.properties", "element", id, map[string]any{"properties": before[id]}, ch)
			})
			if err != nil {
				return nil, err
			}
		}
		lang := r.URL.Query().Get("lang")
		items, err := p.visible(ctx, id, cat, typ, lang, true)
		if err != nil {
			return nil, err
		}
		names, err := p.refs(ctx, items, lang)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items, "refs": names, "can_edit": true}, nil
	})
	// GET /refs?user=1,2&group=…&element=…&category=… – names for pickers
	host.Handle("GET", "/refs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ids := map[string][]int64{}
		n := 0
		for _, kind := range []string{"user", "group", "element", "category"} {
			for _, s := range strings.Split(r.URL.Query().Get(kind), ",") {
				if id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && id > 0 {
					ids[kind] = append(ids[kind], id)
					n++
				}
			}
		}
		if n > 500 {
			return nil, apperr.BadRequest("too many ids")
		}
		return p.refNames(r.Context(), ids, r.URL.Query().Get("lang"))
	})
	// GET /history/{id} – changes of the visible properties of an element
	host.Handle("GET", "/history/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		cat, err := host.Content.ElementCategory(ctx, id, acl.View)
		if err != nil {
			return nil, err
		}
		var typ string
		if err := host.DB.QueryRow(ctx, `SELECT type FROM elements WHERE id=$1`, id).Scan(&typ); err != nil {
			return nil, err
		}
		hidden, err := p.hiddenKeys(ctx, cat, typ)
		if err != nil {
			return nil, err
		}
		rows, err := host.DB.Query(ctx, `SELECT h.key, h.raw, h.changed_at, COALESCE(u.display_name, '') FROM plugin_prop_history h
			LEFT JOIN users u ON u.id = h.changed_by WHERE h.element_id=$1 AND NOT (h.key = ANY($2)) ORDER BY h.changed_at DESC, h.id DESC LIMIT 200`, id, hidden)
		if err != nil {
			return nil, err
		}
		type change struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
			At    time.Time       `json:"at"`
			By    string          `json:"by"`
		}
		return pgx.CollectRows(rows, pgx.RowToStructByPos[change])
	})
}

// hiddenKeys are the keys of definitions the caller may not see there.
func (p *Plugin) hiddenKeys(ctx context.Context, categoryID int64, typ string) ([]string, error) {
	defs, err := p.defsFor(ctx, p.host.DB, categoryID, typ)
	if err != nil {
		return nil, err
	}
	pr := identity.From(ctx)
	out := []string{}
	for _, d := range defs {
		if !d.canView(pr) {
			out = append(out, d.Key)
		}
	}
	return out, nil
}

func readBody(r *http.Request) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := h.Decode(r, &raw); err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return nil, err
		}
		return nil, apperr.BadRequest("invalid JSON")
	}
	return raw, nil
}
