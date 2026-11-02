package content

import (
	"context"
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
}

// ListIn returns elements of the given categories in the caller's
// language (falling back to the original language) and the total count.
func (s *Service) ListIn(ctx context.Context, o ListOptions) ([]ListedElement, int, error) {
	if o.Limit <= 0 || o.Limit > 200 {
		o.Limit = 20
	}
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
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM elements e WHERE e.deleted_at IS NULL AND e.category_id = ANY($1)
		AND ($2 = '' OR $2 = ANY(e.tags))`, o.Categories, o.Tag).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id, e.category_id, e.type, d.lang, d.title, left(d.body_text, $3), e.tags, e.position,
			COALESCE(NULLIF(u.display_name, ''), u.username, ''), e.created_at, e.updated_at
		FROM elements e JOIN categories c ON c.id = e.category_id
		JOIN LATERAL (SELECT lang, title, body_text FROM search_docs WHERE element_id = e.id AND lang IN ($2, e.original_lang)
			ORDER BY (lang = $2) DESC LIMIT 1) d ON true
		LEFT JOIN users u ON u.id = e.created_by
		WHERE e.deleted_at IS NULL AND e.category_id = ANY($1) AND ($4 = '' OR $4 = ANY(e.tags))
		ORDER BY `+order+` LIMIT $5 OFFSET $6`, o.Categories, o.Lang, o.SummaryLen+1, o.Tag, o.Limit, o.Offset)
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
