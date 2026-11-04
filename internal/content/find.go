package content

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// FindOptions filters FindElements.
type FindOptions struct {
	Q         string // part of a title or short URL
	Type      string
	Category  int64 // 0 = all
	Recursive bool
	Sort      string // title | updated (default) | created | type
	Lang      string
	Limit     int // default 50, max 500
	Offset    int
}

// FoundElement is an element with the path of its category.
type FoundElement struct {
	Element
	Title        string   `json:"title"`         // in the requested language (or another)
	CategoryPath []string `json:"category_path"` // titles from the top
}

// FindElements lists the elements the caller may view (all of them for
// administrators), with paging and the total count.
func (s *Service) FindElements(ctx context.Context, o FindOptions) ([]FoundElement, int, error) {
	if o.Limit <= 0 || o.Limit > 500 {
		o.Limit = 50
	}
	if o.Lang == "" || !s.validLang(o.Lang) {
		o.Lang = s.DefaultLanguage()
	}
	var cats []int64 // nil = all
	if o.Category != 0 {
		scope, err := s.Scope(ctx, o.Category, o.Recursive)
		if err != nil {
			return nil, 0, err
		}
		cats = scope
	} else if p := identity.From(ctx); !p.Admin() {
		rows, err := s.pool.Query(ctx, `SELECT id FROM categories WHERE deleted_at IS NULL`)
		if err != nil {
			return nil, 0, err
		}
		all, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return nil, 0, err
		}
		perms, err := s.acl.PermsMap(ctx, p, all)
		if err != nil {
			return nil, 0, err
		}
		cats = []int64{}
		for _, id := range all {
			if perms[id]&acl.View != 0 {
				cats = append(cats, id)
			}
		}
	}
	order := "e.updated_at DESC, e.id DESC"
	switch o.Sort {
	case "title":
		order = "lower(COALESCE(e.titles->>$5, e.titles->>e.original_lang)), e.id"
	case "created":
		order = "e.created_at DESC, e.id DESC"
	case "type":
		order = "e.type, lower(COALESCE(e.titles->>$5, e.titles->>e.original_lang)), e.id"
	}
	q := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(o.Q), "%", ""), "_", "")
	where := `e.deleted_at IS NULL AND ($1::bigint[] IS NULL OR e.category_id = ANY($1)) AND ($2 = '' OR e.type = $2)
		AND ($3 = '' OR e.short_url ILIKE '%' || $3 || '%' OR EXISTS (SELECT 1 FROM jsonb_each_text(e.titles) t WHERE t.value ILIKE '%' || $3 || '%'))`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM elements e WHERE `+where, cats, o.Type, q).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+elemCols+`, COALESCE(NULLIF(e.titles->>$5, ''), e.titles->>e.original_lang, '')
		FROM elements e WHERE `+where+` ORDER BY `+order+` LIMIT $4 OFFSET $6`, cats, o.Type, q, o.Limit, o.Lang, o.Offset)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (FoundElement, error) {
		var f FoundElement
		e := &f.Element
		err := r.Scan(&e.ID, &e.CategoryID, &e.Type, &e.Position, &e.OriginalLang, &e.Titles, &e.Tags, &e.CreatedAt, &e.UpdatedAt,
			&e.UpdatedBy, &e.DeletedAt, &e.IsDefault, &e.ShortURL, &e.PreviewImage, &e.CreatedByID, &f.Title)
		return f, err
	})
	if err != nil {
		return nil, 0, err
	}
	// category paths
	ids := map[int64]bool{}
	for _, f := range list {
		ids[f.CategoryID] = true
	}
	if len(ids) > 0 {
		arr := make([]int64, 0, len(ids))
		for id := range ids {
			arr = append(arr, id)
		}
		prow, err := s.pool.Query(ctx, `SELECT c.id, ARRAY(SELECT a.titles FROM categories a WHERE a.path @> c.path ORDER BY nlevel(a.path))
			FROM categories c WHERE c.id = ANY($1)`, arr)
		if err != nil {
			return nil, 0, err
		}
		paths := map[int64][]string{}
		for prow.Next() {
			var id int64
			var ts []map[string]string
			if err := prow.Scan(&id, &ts); err != nil {
				prow.Close()
				return nil, 0, err
			}
			for _, t := range ts {
				paths[id] = append(paths[id], firstText(t, o.Lang, s.DefaultLanguage()))
			}
		}
		prow.Close()
		for i := range list {
			list[i].CategoryPath = paths[list[i].CategoryID]
		}
	}
	return list, total, nil
}
