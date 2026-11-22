package content

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// PlacedSiteWide reports whether a block is shown beyond one category: placed
// at a position on the level template, site or element type (the layout of
// all pages of a type).
func (s *Service) PlacedSiteWide(ctx context.Context, block string) (bool, error) {
	var yes bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM placements WHERE block=$1 AND level IN ('template','site','type') AND position<>'')`, block).Scan(&yes)
	return yes, err
}

// BlockPlacement is one placement of a block (where it is shown).
type BlockPlacement struct {
	Level            string `json:"level"`
	Target           string `json:"target"`
	Position         string `json:"position"`
	OnlyCategoryPage bool   `json:"only_category_page"`
	NoSubcategories  bool   `json:"no_subcategories"`
}

// PlacementsOfBlock lists where a block is placed (not the personal
// arrangements of people). The caller checks that the block may be edited.
func (s *Service) PlacementsOfBlock(ctx context.Context, block string) ([]BlockPlacement, error) {
	rows, err := s.pool.Query(ctx, `SELECT level, target, position, only_category_page, no_subcategories FROM placements
		WHERE block=$1 AND level<>'user' ORDER BY level, target, position`, block)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (BlockPlacement, error) {
		var x BlockPlacement
		return x, r.Scan(&x.Level, &x.Target, &x.Position, &x.OnlyCategoryPage, &x.NoSubcategories)
	})
}

// TypedElement is an element of a type in a list (id, title, category).
type TypedElement struct {
	ID         int64
	Title      string
	CategoryID int64
}

// ElementsOfType lists the elements of a type that the caller may view, in
// all categories (also hidden ones: unlisted types are hidden), at most max,
// with their title in lang (else the original language).
func (s *Service) ElementsOfType(ctx context.Context, typ, lang string, max int) ([]TypedElement, error) {
	f, err := s.acl.ElementFilter(ctx, identity.From(ctx), acl.View)
	if err != nil {
		return nil, err
	}
	args := []any{typ, lang, max}
	// acl: filtered (ElementFilter: categories, own rights, classification)
	rows, err := s.pool.Query(ctx, `SELECT e.id, COALESCE(NULLIF(e.titles->>$2, ''), e.titles->>e.original_lang, ''), e.category_id FROM elements e
		WHERE e.type=$1 AND e.deleted_at IS NULL AND `+f.SQL("e", &args)+`
		ORDER BY e.id LIMIT $3`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (TypedElement, error) {
		var x TypedElement
		return x, r.Scan(&x.ID, &x.Title, &x.CategoryID)
	})
}
