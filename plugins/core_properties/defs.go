package core_properties

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Def is the definition of a property.
type Def struct {
	ID           int64             `json:"id"`
	CategoryID   *int64            `json:"category_id"` // nil: global
	Key          string            `json:"key"`
	Labels       map[string]string `json:"labels"`
	Help         map[string]string `json:"help"`
	Type         string            `json:"type"`
	Options      Options           `json:"options"`
	Required     bool              `json:"required"`
	Default      json.RawMessage   `json:"default"`
	ElementTypes []string          `json:"element_types"`
	Group        string            `json:"group"`
	Position     int               `json:"position"`
	ShowOnPage   bool              `json:"show_on_page"`
	ViewSubjects []acl.Subject     `json:"view_subjects"`
	EditSubjects []acl.Subject     `json:"edit_subjects"`
	// Depth of the category of the definition (0: global), for inheritance.
	depth int
}

const defCols = `d.id, d.category_id, d.key, d.labels, d.help, d.type, d.options, d.required, d.default_value, d.element_types, d.grp, d.position,
	d.show_on_page, d.view_subjects, d.edit_subjects`

func scanDef(r pgx.CollectableRow) (Def, error) {
	var d Def
	var opts []byte
	err := r.Scan(&d.ID, &d.CategoryID, &d.Key, &d.Labels, &d.Help, &d.Type, &opts, &d.Required, &d.Default, &d.ElementTypes, &d.Group, &d.Position,
		&d.ShowOnPage, &d.ViewSubjects, &d.EditSubjects)
	if err == nil {
		_ = json.Unmarshal(opts, &d.Options)
	}
	return d, err
}

// Built-in properties: not stored here but shown, filtered and sorted
// like the others.
var Builtins = []string{"tags", "title", "type", "category", "created_at", "updated_at", "created_by", "updated_by"}

var keyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Label of a definition (or its key).
func (d *Def) Label(lang string) string {
	if v := pick(d.Labels, lang); v != "" {
		return v
	}
	return d.Key
}

// applies reports whether a definition applies to an element type.
func (d *Def) applies(elementType string) bool {
	return len(d.ElementTypes) == 0 || elementType == "" || slices.Contains(d.ElementTypes, elementType)
}

// canView: everyone who sees the element, unless restricted.
func (d *Def) canView(p *identity.Principal) bool {
	return len(d.ViewSubjects) == 0 || acl.MatchesAny(d.ViewSubjects, p)
}

// canEdit: everyone who may edit the element (and see the property), unless restricted.
func (d *Def) canEdit(p *identity.Principal) bool {
	return d.canView(p) && (len(d.EditSubjects) == 0 || acl.MatchesAny(d.EditSubjects, p))
}

// ancestors returns the ids of a category and its ancestors, nearest last.
func ancestors(ctx context.Context, q db.Querier, categoryID int64) ([]int64, error) {
	var path string
	if err := q.QueryRow(ctx, `SELECT path::text FROM categories WHERE id=$1`, categoryID).Scan(&path); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.NotFound("category")
		}
		return nil, err
	}
	var out []int64
	for _, s := range strings.Split(path, ".") {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out, nil
}

// defsFor returns the definitions that apply in a category (global and
// inherited; a nearer one wins) for an element type ("" = all), by key.
func (p *Plugin) defsFor(ctx context.Context, q db.Querier, categoryID int64, elementType string) (map[string]*Def, error) {
	anc, err := ancestors(ctx, q, categoryID)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT `+defCols+` FROM plugin_prop_defs d WHERE d.category_id IS NULL OR d.category_id = ANY($1)`, anc)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, scanDef)
	if err != nil {
		return nil, err
	}
	out := map[string]*Def{}
	for i := range list {
		d := &list[i]
		if d.CategoryID != nil {
			d.depth = slices.Index(anc, *d.CategoryID) + 1
		}
		if !d.applies(elementType) {
			continue
		}
		if cur, ok := out[d.Key]; !ok || d.depth > cur.depth {
			out[d.Key] = d
		}
	}
	return out, nil
}

// sortedDefs orders definitions by group, position and label.
func sortedDefs(m map[string]*Def, lang string) []*Def {
	out := make([]*Def, 0, len(m))
	for _, d := range m {
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b *Def) int {
		if a.Group != b.Group {
			return strings.Compare(a.Group, b.Group)
		}
		if a.Position != b.Position {
			return a.Position - b.Position
		}
		return strings.Compare(a.Label(lang), b.Label(lang))
	})
	return out
}

// mayDefine: global definitions need core.properties.admin, those of a
// category core.properties.define there (or the global one).
func (p *Plugin) mayDefine(ctx context.Context, categoryID *int64) error {
	if perm.Has(ctx, "core.properties.admin") {
		if categoryID != nil {
			// the category must exist and be visible
			return p.host.Content.Require(ctx, *categoryID, acl.View)
		}
		return nil
	}
	if categoryID == nil {
		return apperr.Forbidden("defining global properties needs the permission %q", "core.properties.admin")
	}
	if err := p.host.Content.Require(ctx, *categoryID, acl.View); err != nil {
		return err
	}
	if !p.host.Can(ctx, *categoryID, "core.properties.define") {
		return apperr.Forbidden("defining properties of this category needs the permission %q", "core.properties.define")
	}
	return nil
}

func checkSubjects(list []acl.Subject) ([]acl.Subject, error) {
	if len(list) > 100 {
		return nil, apperr.BadRequest("at most 100 people, groups and roles")
	}
	out := []acl.Subject{}
	for _, s := range list {
		if !s.Valid() {
			return nil, apperr.BadRequest("invalid subject %q", s.Type)
		}
		out = append(out, s)
	}
	return out, nil
}

func checkTexts(m map[string]string, max int) (map[string]string, error) {
	out := map[string]string{}
	if len(m) > 30 {
		return nil, apperr.BadRequest("too many languages")
	}
	for l, v := range m {
		v = strings.TrimSpace(v)
		if !langRe.MatchString(l) {
			return nil, apperr.BadRequest("invalid language %q", l)
		}
		if utf8.RuneCountInString(v) > max {
			return nil, apperr.BadRequest("text too long (max. %d characters)", max)
		}
		if v != "" {
			out[l] = v
		}
	}
	return out, nil
}

// check validates a definition (before saving).
func (p *Plugin) check(d *Def) error {
	d.Key = strings.ToLower(strings.TrimSpace(d.Key))
	if !keyRe.MatchString(d.Key) {
		return apperr.BadRequest("key: lower-case letters, digits and _, starting with a letter (max. 63)")
	}
	if slices.Contains(Builtins, d.Key) {
		return apperr.BadRequest("%q is a built-in property", d.Key)
	}
	t, ok := TypeByID(d.Type)
	if !ok {
		return apperr.BadRequest("unknown type %q", d.Type)
	}
	if err := CheckOptions(t, &d.Options); err != nil {
		return err
	}
	var err error
	if d.Labels, err = checkTexts(d.Labels, MaxLabelLen); err != nil {
		return err
	}
	if d.Help, err = checkTexts(d.Help, 500); err != nil {
		return err
	}
	if utf8.RuneCountInString(d.Group) > 60 {
		return apperr.BadRequest("group name too long")
	}
	d.Group = strings.TrimSpace(d.Group)
	if len(d.ElementTypes) > 50 {
		return apperr.BadRequest("too many element types")
	}
	for _, et := range d.ElementTypes {
		if p.host.Content.Type(et) == nil {
			return apperr.BadRequest("unknown element type %q", et)
		}
	}
	if d.ElementTypes == nil {
		d.ElementTypes = []string{}
	}
	if d.ViewSubjects, err = checkSubjects(d.ViewSubjects); err != nil {
		return err
	}
	if d.EditSubjects, err = checkSubjects(d.EditSubjects); err != nil {
		return err
	}
	if t.Computed {
		d.Required, d.Default = false, nil
	}
	if len(d.Default) > 0 && string(d.Default) != "null" {
		v, err := CheckValue(d.Key, t, d.Options, d.Default)
		if err != nil {
			return apperr.BadRequest("default: %v", err)
		}
		if v == nil {
			d.Default = nil
		} else {
			d.Default = v.Raw
		}
	} else {
		d.Default = nil
	}
	return nil
}

func (p *Plugin) routesDefs() {
	host := p.host
	// GET /types – the types with their options
	host.Handle("GET", "/types", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]any{"types": Types, "palette": Palette, "builtins": Builtins}, nil
	})
	// GET /defs?category=ID – definitions that apply there (with own=true for
	// those of the category itself); without category: the global ones.
	host.Handle("GET", "/defs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		cat, _ := strconv.ParseInt(r.URL.Query().Get("category"), 10, 64)
		type out struct {
			Def
			Own       bool   `json:"own"`
			Inherited string `json:"inherited_from,omitempty"` // "global" or the category id
		}
		res := []out{}
		if cat <= 0 {
			rows, err := host.DB.Query(ctx, `SELECT `+defCols+` FROM plugin_prop_defs d WHERE d.category_id IS NULL ORDER BY d.grp, d.position, d.key`)
			if err != nil {
				return nil, err
			}
			list, err := pgx.CollectRows(rows, scanDef)
			if err != nil {
				return nil, err
			}
			for _, d := range list {
				res = append(res, out{Def: d, Own: true})
			}
		} else {
			if err := host.Content.Require(ctx, cat, acl.View); err != nil {
				return nil, err
			}
			m, err := p.defsFor(ctx, host.DB, cat, "")
			if err != nil {
				return nil, err
			}
			for _, d := range sortedDefs(m, r.URL.Query().Get("lang")) {
				o := out{Def: *d, Own: d.CategoryID != nil && *d.CategoryID == cat}
				switch {
				case d.CategoryID == nil:
					o.Inherited = "global"
				case !o.Own:
					o.Inherited = strconv.FormatInt(*d.CategoryID, 10)
				}
				res = append(res, o)
			}
		}
		canDefine := p.mayDefine(ctx, nilIfZero(cat)) == nil
		free, _ := p.freeSetting(ctx, host.DB, cat)
		own := "inherit"
		if cat > 0 {
			_ = host.DB.QueryRow(ctx, `SELECT free FROM plugin_prop_settings WHERE category_id=$1`, cat).Scan(&own)
		}
		return map[string]any{"defs": res, "can_define": canDefine, "free": free, "free_setting": own}, nil
	})
	// POST /defs – a new definition (category_id null: global)
	host.Handle("POST", "/defs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		var d Def
		if err := h.Decode(r, &d); err != nil {
			return nil, err
		}
		if d.CategoryID != nil && *d.CategoryID <= 0 {
			d.CategoryID = nil
		}
		if err := p.mayDefine(ctx, d.CategoryID); err != nil {
			return nil, err
		}
		if err := p.check(&d); err != nil {
			return nil, err
		}
		var n int
		if err := host.DB.QueryRow(ctx, `SELECT count(*) FROM plugin_prop_defs WHERE category_id IS NOT DISTINCT FROM $1`, d.CategoryID).Scan(&n); err != nil {
			return nil, err
		}
		if n >= MaxDefs {
			return nil, apperr.BadRequest("at most %d definitions", MaxDefs)
		}
		opts, _ := json.Marshal(d.Options)
		err := db.Tx(ctx, host.DB, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `INSERT INTO plugin_prop_defs(category_id, key, labels, help, type, options, required, default_value, element_types, grp, position, show_on_page, view_subjects, edit_subjects)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`,
				d.CategoryID, d.Key, d.Labels, d.Help, d.Type, opts, d.Required, nullJSON(d.Default), d.ElementTypes, d.Group, d.Position, d.ShowOnPage, d.ViewSubjects, d.EditSubjects).Scan(&d.ID); err != nil {
				if db.IsUniqueViolation(err) {
					return apperr.Conflict("property %q is already defined here", d.Key)
				}
				return err
			}
			// values of this key below the category become values of the definition
			if err := p.reassign(ctx, tx, d.CategoryID, d.Key, d.Label("en")); err != nil {
				return err
			}
			return audit.Log(ctx, tx, "properties.define", "property", d.ID, nil, d)
		})
		if err != nil {
			return nil, err
		}
		return h.Created{V: d}, nil
	})
	// PUT /defs/{id} – change a definition (key and category stay)
	host.Handle("PUT", "/defs/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		old, err := p.loadDef(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := p.mayDefine(ctx, old.CategoryID); err != nil {
			return nil, err
		}
		var d Def
		if err := h.Decode(r, &d); err != nil {
			return nil, err
		}
		d.ID, d.CategoryID, d.Key = old.ID, old.CategoryID, old.Key
		if d.Type != old.Type {
			// stored values keep their meaning only within the same storage
			ot, _ := TypeByID(old.Type)
			nt, ok := TypeByID(d.Type)
			if !ok || ot.Storage != nt.Storage || nt.Computed != ot.Computed {
				return nil, apperr.BadRequest("the type can only change to one stored the same way (e.g. text to e-mail)")
			}
		}
		if err := p.check(&d); err != nil {
			return nil, err
		}
		opts, _ := json.Marshal(d.Options)
		err = db.Tx(ctx, host.DB, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE plugin_prop_defs SET labels=$2, help=$3, type=$4, options=$5, required=$6, default_value=$7, element_types=$8, grp=$9,
				position=$10, show_on_page=$11, view_subjects=$12, edit_subjects=$13, updated_at=now() WHERE id=$1`,
				id, d.Labels, d.Help, d.Type, opts, d.Required, nullJSON(d.Default), d.ElementTypes, d.Group, d.Position, d.ShowOnPage, d.ViewSubjects, d.EditSubjects); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE plugin_prop_values SET type=$2 WHERE def_id=$1`, id, d.Type); err != nil {
				return err
			}
			// a change of the element types moves values to other definitions
			if !slices.Equal(old.ElementTypes, d.ElementTypes) {
				if err := p.reassign(ctx, tx, d.CategoryID, d.Key, d.Label("en")); err != nil {
					return err
				}
			}
			return audit.Log(ctx, tx, "properties.define", "property", id, old, d)
		})
		if err != nil {
			return nil, err
		}
		return d, nil
	})
	// DELETE /defs/{id}?values=keep|delete – the values become free
	// properties (keep, default) or are deleted
	host.Handle("DELETE", "/defs/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		old, err := p.loadDef(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := p.mayDefine(ctx, old.CategoryID); err != nil {
			return nil, err
		}
		del := r.URL.Query().Get("values") == "delete"
		err = db.Tx(ctx, host.DB, func(tx pgx.Tx) error {
			if del {
				if _, err := tx.Exec(ctx, `DELETE FROM plugin_prop_values WHERE def_id=$1`, id); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE plugin_prop_values SET def_id=NULL, label=$2 WHERE def_id=$1`, id, old.Label("en")); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM plugin_prop_defs WHERE id=$1`, id); err != nil {
				return err
			}
			// a definition further up takes over the values
			if err := p.reassign(ctx, tx, old.CategoryID, old.Key, old.Label("en")); err != nil {
				return err
			}
			return audit.Log(ctx, tx, "properties.undefine", "property", id, old, map[string]any{"values_deleted": del})
		})
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, nil
	})
	// PUT /settings/{category} {"free": "inherit|allow|deny"}
	host.Handle("PUT", "/settings/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		cat, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if err := p.mayDefine(ctx, &cat); err != nil {
			return nil, err
		}
		var in struct {
			Free string `json:"free"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if in.Free != "inherit" && in.Free != "allow" && in.Free != "deny" {
			return nil, apperr.BadRequest("free: inherit, allow or deny")
		}
		if _, err := host.DB.Exec(ctx, `INSERT INTO plugin_prop_settings(category_id, free) VALUES ($1,$2) ON CONFLICT (category_id) DO UPDATE SET free=EXCLUDED.free`, cat, in.Free); err != nil {
			return nil, err
		}
		_ = audit.Log(ctx, host.DB, "properties.settings", "category", cat, nil, in)
		free, err := p.freeSetting(ctx, host.DB, cat)
		return map[string]any{"free": free, "free_setting": in.Free}, err
	})
}

func nilIfZero(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func nullJSON(v json.RawMessage) any {
	if len(v) == 0 {
		return nil
	}
	return []byte(v)
}

func (p *Plugin) loadDef(ctx context.Context, id int64) (*Def, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT `+defCols+` FROM plugin_prop_defs d WHERE d.id=$1`, id)
	if err != nil {
		return nil, err
	}
	d, err := pgx.CollectExactlyOneRow(rows, scanDef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("property definition")
	}
	return &d, err
}

// freeSetting: may free properties be added in a category (nearest
// allow/deny, else the site setting)?
func (p *Plugin) freeSetting(ctx context.Context, q db.Querier, categoryID int64) (bool, error) {
	if categoryID > 0 {
		anc, err := ancestors(ctx, q, categoryID)
		if err != nil {
			return false, err
		}
		var v string
		err = q.QueryRow(ctx, `SELECT s.free FROM plugin_prop_settings s WHERE s.category_id = ANY($1) AND s.free <> 'inherit'
			ORDER BY array_position($1, s.category_id) DESC LIMIT 1`, anc).Scan(&v)
		if err == nil {
			return v == "allow", nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}
	}
	return p.freeDefault(), nil
}

// reassign gives the values of a key in a category and below (nil: all)
// to the definition that applies to each element now (the nearest one of
// a type stored the same way); values without one become free properties
// named label.
func (p *Plugin) reassign(ctx context.Context, tx pgx.Tx, categoryID *int64, key, label string) error {
	rows, err := tx.Query(ctx, `SELECT v.element_id, e.category_id, e.type, v.type, v.def_id FROM plugin_prop_values v JOIN elements e ON e.id = v.element_id
		WHERE v.key = $1 AND ($2::bigint IS NULL OR e.category_id IN (SELECT c.id FROM categories c, categories t WHERE t.id=$2 AND c.path <@ t.path))`, key, categoryID)
	if err != nil {
		return err
	}
	type row struct {
		el, cat     int64
		etype, vtyp string
		def         *int64
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		return x, r.Scan(&x.el, &x.cat, &x.etype, &x.vtyp, &x.def)
	})
	if err != nil {
		return err
	}
	cache := map[string]map[string]*Def{}
	for _, x := range list {
		ck := strconv.FormatInt(x.cat, 10) + "/" + x.etype
		m, ok := cache[ck]
		if !ok {
			if m, err = p.defsFor(ctx, tx, x.cat, x.etype); err != nil {
				return err
			}
			cache[ck] = m
		}
		d := m[key]
		vt, _ := TypeByID(x.vtyp)
		if d != nil {
			dt, _ := TypeByID(d.Type)
			if dt.Storage == vt.Storage && !dt.Computed {
				if x.def == nil || *x.def != d.ID {
					if _, err := tx.Exec(ctx, `UPDATE plugin_prop_values SET def_id=$3, type=$4, label='' WHERE element_id=$1 AND key=$2`, x.el, key, d.ID, d.Type); err != nil {
						return err
					}
				}
				continue
			}
		}
		if x.def != nil {
			if _, err := tx.Exec(ctx, `UPDATE plugin_prop_values SET def_id=NULL, label=$3 WHERE element_id=$1 AND key=$2`, x.el, key, label); err != nil {
				return err
			}
		}
	}
	return nil
}
