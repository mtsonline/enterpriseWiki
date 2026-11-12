package content

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Scope returns root and (with recursive) its subcategories that the caller
// may view, in tree order. Hidden subcategories hide their children too.
func (s *Service) Scope(ctx context.Context, root int64, recursive bool) ([]int64, error) {
	if err := s.require(ctx, root, acl.View); err != nil {
		return nil, err
	}
	if !recursive {
		return []int64{root}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.parent_id FROM categories c, categories r
		WHERE r.id=$1 AND c.path <@ r.path AND c.deleted_at IS NULL ORDER BY nlevel(c.path), c.position`, root)
	if err != nil {
		return nil, err
	}
	type node struct {
		id     int64
		parent *int64
	}
	nodes, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (node, error) {
		var n node
		return n, r.Scan(&n.id, &n.parent)
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(nodes))
	for i, n := range nodes {
		ids[i] = n.id
	}
	perms, err := s.acl.PermsMap(ctx, identity.From(ctx), ids)
	if err != nil {
		return nil, err
	}
	ok := map[int64]bool{root: true}
	out := []int64{root}
	for _, n := range nodes {
		if n.id == root || n.parent == nil || !ok[*n.parent] || perms[n.id]&acl.View == 0 {
			continue
		}
		ok[n.id] = true
		out = append(out, n.id)
	}
	return out, nil
}

// ListedElement is an element in a listing (blog, FAQ, feed).
type ListedElement struct {
	ID         int64     `json:"id"`
	CategoryID int64     `json:"category_id"`
	Type       string    `json:"type"`
	Lang       string    `json:"lang"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary"` // plain text, shortened
	Tags       []string  `json:"tags"`
	Position   int       `json:"position"`
	Author     string    `json:"author"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	HTML       string    `json:"html,omitempty"` // rendered content (WithContent)
}

// ListOptions selects elements of categories.
type ListOptions struct {
	Categories  []int64 // from Scope (already permission checked)
	Lang        string
	Order       string // "created" (newest first, default), "updated", "position"
	Tag         string
	Limit       int // default 20, max 200
	Offset      int
	SummaryLen  int // characters of the summary (default 300)
	WithContent bool
	Exclude     int64 // e.g. the blog element itself
	// Sources replaces Categories: elements of several sources (ResolveSources)
	Sources *SourceSet
	// NoConfidential leaves out confidential content (for feeds that leave the wiki)
	NoConfidential bool
	// SkipListings leaves out elements whose type lists other elements
	// (blog, FAQ, overviews …) unless their type or id is a source itself
	SkipListings bool
}

// ListIn returns elements of the given categories in the caller's
// language (falling back to the original language) and the total count.
func (s *Service) ListIn(ctx context.Context, o ListOptions) ([]ListedElement, int, error) {
	if o.Limit <= 0 {
		o.Limit = 20
	}
	o.Limit = min(o.Limit, s.MaxListItems()) // the bound of the setting lists.max_items
	if o.SummaryLen <= 0 {
		o.SummaryLen = 300
	}
	if o.Lang == "" || !s.validLang(o.Lang) {
		o.Lang = s.DefaultLanguage()
	}
	order := "e.created_at DESC, e.id DESC"
	switch o.Order {
	case "updated":
		order = "e.updated_at DESC, e.id DESC"
	case "position":
		order = "c.path, e.position, e.id"
	}
	// where the elements come from: categories (permission checked by the caller) or sources
	where, wargs := "e.category_id = ANY($1)", []any{o.Categories}
	if o.Sources != nil {
		where, wargs = sourceCond(o.Sources, 1)
	}
	if o.NoConfidential {
		where += " AND NOT EXISTS (SELECT 1 FROM categories cc JOIN categories cx ON cx.id = e.category_id WHERE cc.classification >= 2 AND cx.path <@ cc.path)"
	}
	if o.SkipListings {
		types, ids := []string{}, []int64{}
		if o.Sources != nil {
			types, ids = o.Sources.Types, o.Sources.IDs
		}
		k := len(wargs)
		where += fmt.Sprintf(" AND (e.type <> ALL($%d) OR e.type = ANY($%d) OR e.id = ANY($%d))", k+1, k+2, k+3)
		wargs = append(wargs, s.ListingTypes(), types, ids)
	}
	nw := len(wargs)
	arg := func(i int) string { return "$" + strconv.Itoa(nw+i) }
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM elements e WHERE e.deleted_at IS NULL AND `+where+`
		AND (`+arg(1)+` = '' OR `+arg(1)+` = ANY(e.tags)) AND e.id <> `+arg(2), append(wargs, o.Tag, o.Exclude)...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id, e.category_id, e.type, d.lang, d.title, left(d.body_text, `+arg(2)+`), e.tags, e.position,
			COALESCE(NULLIF(u.display_name, ''), u.username, ''), e.created_at, e.updated_at
		FROM elements e JOIN categories c ON c.id = e.category_id
		JOIN LATERAL (SELECT lang, title, body_text FROM search_docs WHERE element_id = e.id AND lang IN (`+arg(1)+`, e.original_lang)
			ORDER BY (lang = `+arg(1)+`) DESC LIMIT 1) d ON true
		LEFT JOIN users u ON u.id = e.created_by
		WHERE e.deleted_at IS NULL AND `+where+` AND (`+arg(3)+` = '' OR `+arg(3)+` = ANY(e.tags)) AND e.id <> `+arg(6)+`
		ORDER BY `+order+` LIMIT `+arg(4)+` OFFSET `+arg(5), append(wargs, o.Lang, o.SummaryLen+1, o.Tag, o.Limit, o.Offset, o.Exclude)...)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ListedElement, error) {
		var e ListedElement
		err := r.Scan(&e.ID, &e.CategoryID, &e.Type, &e.Lang, &e.Title, &e.Summary, &e.Tags, &e.Position, &e.Author, &e.CreatedAt, &e.UpdatedAt)
		if len([]rune(e.Summary)) > o.SummaryLen {
			rs := []rune(e.Summary)[:o.SummaryLen]
			e.Summary = string(rs) + " …"
		}
		return e, err
	})
	if err != nil {
		return nil, 0, err
	}
	if o.WithContent {
		for i := range list {
			if v, err := s.GetElement(ctx, list[i].ID, o.Lang, false); err == nil {
				list[i].HTML = v.HTML
			}
		}
	}
	return list, total, nil
}

// ElementBody returns type, stored body and category of an element in lang
// (falling back to the original language) for callers who may view it.
// Element type plugins use it to read their configuration.
func (s *Service) ElementBody(ctx context.Context, id int64, lang string) (typ string, body []byte, shown string, cat int64, err error) {
	if cat, err = s.ElementCategory(ctx, id, acl.View); err != nil {
		return
	}
	err = s.pool.QueryRow(ctx, `SELECT e.type, r.body, r.lang FROM element_revisions r JOIN elements e ON e.id = r.element_id
		WHERE r.element_id = $1 AND e.deleted_at IS NULL ORDER BY (r.lang = $2) DESC, (r.lang = e.original_lang) DESC, r.rev DESC LIMIT 1`, id, lang).
		Scan(&typ, &body, &shown)
	return
}

// CategoriesByID loads categories in tree order (no permission check:
// pass ids from Scope).
func (s *Service) CategoriesByID(ctx context.Context, ids []int64) ([]Category, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+catCols+` FROM categories c WHERE c.id = ANY($1) AND c.deleted_at IS NULL ORDER BY c.path`, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanCategory)
}
