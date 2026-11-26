package content

// Data for the wiki blocks about pages: the tags of an
// area as a cloud, the latest changes of a page and the people who wrote
// it. Everything with the permissions of the reader and with fixed limits;
// people are named only by their display name (no login names).

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

const (
	// tagCloudScan: at most so many (tag, page) pairs are read for a cloud;
	// a cloud is an overview, the search lists everything
	tagCloudScan = 20000
	// PageChangesMax and ContributorsMax: what a block shows at most
	PageChangesMax  = 50
	ContributorsMax = 50
)

// TagCloud returns the tags of the pages (and categories) the reader may
// see, in a category with its subcategories (root 0: the whole wiki), most
// used first, at most limit.
func (s *Service) TagCloud(ctx context.Context, root int64, limit int) ([]TagCount, error) {
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	var scope []int64
	if root > 0 {
		var err error
		if scope, err = s.Scope(ctx, root, true); err != nil {
			return nil, err
		}
	}
	// acl: checked below (ElementFilter for pages, category permissions for categories)
	rows, err := s.pool.Query(ctx, `SELECT t.tag, t.cat, t.el FROM (
			SELECT unnest(tags) AS tag, category_id AS cat, id AS el FROM elements
				WHERE deleted_at IS NULL AND NOT hidden AND ($1::bigint[] IS NULL OR category_id = ANY($1))
			UNION ALL SELECT unnest(tags), id, 0 FROM categories
				WHERE deleted_at IS NULL AND ($1::bigint[] IS NULL OR id = ANY($1))) t
		LIMIT $2`, scope, tagCloudScan)
	if err != nil {
		return nil, err
	}
	type tc struct {
		tag     string
		cat, el int64
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (tc, error) {
		var x tc
		return x, r.Scan(&x.tag, &x.cat, &x.el)
	})
	if err != nil {
		return nil, err
	}
	cats := make([]int64, 0, len(all))
	for _, x := range all {
		cats = append(cats, x.cat)
	}
	p := identity.From(ctx)
	perms, err := s.acl.PermsMap(ctx, p, cats)
	if err != nil {
		return nil, err
	}
	f, err := s.acl.ElementFilter(ctx, p, acl.View)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, x := range all {
		if (x.el == 0 && perms[x.cat]&acl.View != 0) || (x.el != 0 && f.Allows(x.el, x.cat)) {
			counts[x.tag]++
		}
	}
	out := make([]TagCount, 0, len(counts))
	for t, c := range counts {
		out = append(out, TagCount{t, c})
	}
	slices.SortFunc(out, func(a, b TagCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Tag, b.Tag)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// PageChange is one saved version of a page, for readers.
type PageChange struct {
	Lang    string    `json:"lang"`
	Rev     int       `json:"rev"`
	Author  string    `json:"author"` // display name only
	Comment string    `json:"comment"`
	At      time.Time `json:"at"`
}

// PageChanges returns the latest saved versions of a page the reader may
// see (all languages), newest first.
func (s *Service) PageChanges(ctx context.Context, elementID int64, limit int) ([]PageChange, error) {
	if _, err := s.ElementCategory(ctx, elementID, acl.View); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > PageChangesMax {
		limit = 10
	}
	// acl: checked above (ElementCategory)
	rows, err := s.pool.Query(ctx, `SELECT r.lang, r.rev, COALESCE(NULLIF(u.display_name, ''), ''), r.comment, r.created_at
		FROM element_revisions r LEFT JOIN users u ON u.id = r.created_by
		WHERE r.element_id=$1 ORDER BY r.created_at DESC, r.rev DESC LIMIT $2`, elementID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[PageChange])
}

// PageContributor is a person who saved versions of a page.
type PageContributor struct {
	Name     string    `json:"name"` // display name only
	Versions int       `json:"versions"`
	Last     time.Time `json:"last"`
}

// Contributors returns the people who saved versions of a page the reader
// may see, the most recent first (people without a display name are left
// out: the block names nobody by a login name).
func (s *Service) Contributors(ctx context.Context, elementID int64, limit int) ([]PageContributor, error) {
	if _, err := s.ElementCategory(ctx, elementID, acl.View); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > ContributorsMax {
		limit = 20
	}
	// acl: checked above (ElementCategory)
	rows, err := s.pool.Query(ctx, `SELECT u.display_name, count(*)::int, max(r.created_at)
		FROM element_revisions r JOIN users u ON u.id = r.created_by
		WHERE r.element_id=$1 AND u.display_name <> ''
		GROUP BY u.id, u.display_name ORDER BY max(r.created_at) DESC LIMIT $2`, elementID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[PageContributor])
}
