package content

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
)

// Settings of every category, inherited by its subcategories (decision of
// 17.11.2026): language of new elements, logo, accent color, the own
// navigation (with shortcuts) and the display of the category page – the
// nearest setting applies; allowed element
// types and macros – each level can only narrow them (intersection), so
// managing a subcategory never widens a limit of the space. The parts that
// make a space (key, directory, template for new spaces, personal space)
// stay with top-level categories (SetSpace).

// noneAllowed marks an intersection without any type or macro left: nothing
// is allowed (an empty list would mean "all").
const noneAllowed = "\x00none"

// CategorySettings are the settings of a category as stored (own) and as
// they apply (effective, with the category each value comes from).
type CategorySettings struct {
	Own       Space            `json:"own"`
	Effective Space            `json:"effective"`
	From      map[string]int64 `json:"from"`
	// TreeRoot is the category the navigation starts from (own navigation),
	// 0 for the normal tree.
	TreeRoot int64 `json:"tree_root"`
}

// general keeps the fields every category may set.
func general(sp Space) Space {
	return Space{OwnTree: sp.OwnTree, Shortcuts: sp.Shortcuts, AllowedTypes: sp.AllowedTypes, AllowedMacros: sp.AllowedMacros,
		Accent: sp.Accent, Logo: sp.Logo, Language: sp.Language, Display: sp.Display}
}

func intersect(acc, level []string) []string {
	if len(level) == 0 {
		return acc
	}
	if len(acc) == 0 {
		return slices.Clone(level)
	}
	out := []string{}
	for _, x := range acc {
		if slices.Contains(level, x) {
			out = append(out, x)
		}
	}
	if len(out) == 0 {
		return []string{noneAllowed}
	}
	return out
}

// effectiveSettings merges the settings of a category and its ancestors.
func (s *Service) effectiveSettings(ctx context.Context, q db.Querier, categoryID int64) (*CategorySettings, error) {
	rows, err := q.Query(ctx, `SELECT c.id, c.space FROM categories c, categories x
		WHERE x.id=$1 AND c.path @> x.path AND c.deleted_at IS NULL ORDER BY nlevel(c.path)`, categoryID)
	if err != nil {
		return nil, err
	}
	type lvl struct {
		id  int64
		raw []byte
	}
	levels, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (lvl, error) {
		var l lvl
		return l, r.Scan(&l.id, &l.raw)
	})
	if err != nil {
		return nil, err
	}
	out := &CategorySettings{From: map[string]int64{}, Effective: Space{Shortcuts: []SpaceLink{}, AllowedTypes: []string{}, AllowedMacros: []string{}}}
	for i, l := range levels {
		var sp Space
		_ = json.Unmarshal(l.raw, &sp)
		if i == len(levels)-1 {
			out.Own = general(sp)
		}
		e := &out.Effective
		if sp.Language != "" {
			e.Language, out.From["language"] = sp.Language, l.id
		}
		if sp.Accent != "" {
			e.Accent, out.From["accent"] = sp.Accent, l.id
		}
		if sp.Logo != "" {
			e.Logo, out.From["logo"] = sp.Logo, l.id
		}
		if sp.Display != nil {
			d := *sp.Display
			e.Display, out.From["display"] = &d, l.id
		}
		if sp.OwnTree {
			e.OwnTree, e.Shortcuts, out.TreeRoot, out.From["own_tree"] = true, sp.Shortcuts, l.id, l.id
		}
		if len(sp.AllowedTypes) > 0 {
			e.AllowedTypes, out.From["allowed_types"] = intersect(e.AllowedTypes, sp.AllowedTypes), l.id
		}
		if len(sp.AllowedMacros) > 0 {
			e.AllowedMacros, out.From["allowed_macros"] = intersect(e.AllowedMacros, sp.AllowedMacros), l.id
		}
	}
	if out.Own.Shortcuts == nil {
		out.Own.Shortcuts = []SpaceLink{}
	}
	if out.Own.AllowedTypes == nil {
		out.Own.AllowedTypes = []string{}
	}
	if out.Own.AllowedMacros == nil {
		out.Own.AllowedMacros = []string{}
	}
	if e := &out.Effective; e.Shortcuts == nil {
		e.Shortcuts = []SpaceLink{}
	}
	if out.Effective.Display == nil {
		d := DefaultDisplay()
		out.Effective.Display = &d
	}
	return out, nil
}

// GetCategorySettings returns the settings of a category (view permission).
func (s *Service) GetCategorySettings(ctx context.Context, id int64) (*CategorySettings, error) {
	if err := s.require(ctx, id, acl.View); err != nil {
		return nil, err
	}
	return s.effectiveSettings(ctx, s.pool, id)
}

// SetCategorySettings changes the general settings of a category (Manage).
// The parts of a space stay as they are.
func (s *Service) SetCategorySettings(ctx context.Context, id int64, in Space) (*CategorySettings, error) {
	if err := s.require(ctx, id, acl.Manage); err != nil {
		return nil, err
	}
	if err := s.notArchived(ctx, id); err != nil {
		return nil, err
	}
	clean, err := s.cleanSpace(general(in))
	if err != nil {
		return nil, err
	}
	var noted int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT space FROM categories WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&raw); err != nil {
			return err
		}
		var cur Space
		_ = json.Unmarshal(raw, &cur)
		next := general(clean)
		next.IsTemplate, next.Personal = cur.IsTemplate, cur.Personal // parts of a space
		b, _ := json.Marshal(next)
		if _, err := tx.Exec(ctx, `UPDATE categories SET space=$2, updated_at=now() WHERE id=$1`, id, b); err != nil {
			return err
		}
		return s.andNote(ctx, tx, &noted, "update", "category", id, audit.Log(ctx, tx, "category.settings", "category", id, general(cur), general(next)))
	})
	if err != nil {
		return nil, err
	}
	s.fireNoted(ctx, noted, "update", "category", id)
	return s.effectiveSettings(ctx, s.pool, id)
}
