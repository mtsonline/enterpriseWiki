package content

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Readable paths: every category and element has a slug (from its title in
// the original or default language), unique among the children of its
// parent category (subcategories and elements share the namespace). The
// path of an element is /wiki/<category slugs>/<element slug>, of a
// category /wiki/<category slugs>.
//
// When a slug changes (new title) or something moves, the old segment is
// kept as an alias (url_aliases, scope "path": parent + slug), so old links
// keep working and are redirected. The same applies to short URLs (scope
// "short").

// PathPrefix is the first segment of readable paths.
const PathPrefix = "/wiki/"

// baseSlug is the slug wanted for a title (fallback: type and id).
func baseSlug(titles map[string]string, orig, def, fallback string) string {
	s := Slugify(firstText(titles, orig, def))
	if s == "" {
		s = fallback
	}
	return s
}

// slugTaken reports whether slug is used by another child of parent.
func slugTaken(ctx context.Context, q db.Querier, parent int64, slug, selfType string, selfID int64) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT 1 FROM categories WHERE parent_id IS NOT DISTINCT FROM NULLIF($1, 0) AND slug=$2 AND deleted_at IS NULL AND NOT ($3 = 'category' AND id = $4)
		UNION ALL SELECT 1 FROM elements WHERE $1 <> 0 AND category_id=$1 AND slug=$2 AND deleted_at IS NULL AND NOT ($3 = 'element' AND id = $4)) x`,
		parent, slug, selfType, selfID).Scan(&n)
	return n > 0, err
}

// uniqueSlug returns base or base-2, base-3, … (free among the children of parent).
func uniqueSlug(ctx context.Context, q db.Querier, parent int64, base, selfType string, selfID int64) (string, error) {
	cand := base
	for i := 2; i < 1000; i++ {
		taken, err := slugTaken(ctx, q, parent, cand, selfType, selfID)
		if err != nil {
			return "", err
		}
		if !taken {
			return cand, nil
		}
		cand = fmt.Sprintf("%s-%d", base, i)
	}
	return fmt.Sprintf("%s-%s%d", base, selfType[:1], selfID), nil
}

// sameBase reports whether slug is base or base-N.
func sameBase(slug, base string) bool {
	if slug == base {
		return true
	}
	rest, ok := strings.CutPrefix(slug, base+"-")
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

type slugState struct {
	parent  int64 // parent category (0 = top level)
	slug    string
	titles  map[string]string
	orig    string
	deleted bool
}

func (s *Service) slugState(ctx context.Context, q db.Querier, etype string, id int64) (*slugState, error) {
	st := &slugState{}
	var err error
	if etype == "element" {
		err = q.QueryRow(ctx, `SELECT category_id, slug, titles, original_lang, deleted_at IS NOT NULL FROM elements WHERE id=$1`, id).
			Scan(&st.parent, &st.slug, &st.titles, &st.orig, &st.deleted)
	} else {
		var parent *int64
		err = q.QueryRow(ctx, `SELECT parent_id, slug, titles, original_lang, deleted_at IS NOT NULL FROM categories WHERE id=$1`, id).
			Scan(&parent, &st.slug, &st.titles, &st.orig, &st.deleted)
		if parent != nil {
			st.parent = *parent
		}
	}
	if err != nil {
		return nil, err
	}
	return st, nil
}

// syncSlug updates the slug of an element or category after a change. The
// former segment (old parent and slug) becomes an alias. oldParent is the
// parent before a move (-1 = unchanged).
func (s *Service) syncSlug(ctx context.Context, etype string, id int64, oldParent int64) error {
	return db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('ewiki.slugs'))`); err != nil {
			return err
		}
		st, err := s.slugState(ctx, tx, etype, id)
		if err != nil || st.deleted {
			return err
		}
		base := baseSlug(st.titles, st.orig, s.DefaultLanguage(), fmt.Sprintf("%s-%d", etype[:1], id))
		moved := oldParent >= 0 && oldParent != st.parent
		if st.slug != "" && sameBase(st.slug, base) && !moved {
			if taken, err := slugTaken(ctx, tx, st.parent, st.slug, etype, id); err != nil || !taken {
				return err
			}
		}
		slug := st.slug
		if slug == "" || !sameBase(slug, base) {
			slug = base
		}
		if slug, err = uniqueSlug(ctx, tx, st.parent, slug, etype, id); err != nil {
			return err
		}
		if slug == st.slug && !moved {
			return nil
		}
		if st.slug != "" {
			from := st.parent
			if moved {
				from = oldParent
			}
			if err := addAlias(ctx, tx, "path", from, st.slug, etype, id); err != nil {
				return err
			}
		}
		// the new address replaces an alias pointing elsewhere
		if _, err := tx.Exec(ctx, `DELETE FROM url_aliases WHERE scope='path' AND parent_id=$1 AND slug=$2`, st.parent, slug); err != nil {
			return err
		}
		table := "elements"
		if etype == "category" {
			table = "categories"
		}
		_, err = tx.Exec(ctx, `UPDATE `+table+` SET slug=$2 WHERE id=$1`, id, slug)
		return err
	})
}

func addAlias(ctx context.Context, tx pgx.Tx, scope string, parent int64, slug, etype string, id int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO url_aliases(scope, parent_id, slug, target_type, target_id) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (scope, parent_id, slug) DO UPDATE SET target_type=EXCLUDED.target_type, target_id=EXCLUDED.target_id, created_at=now()`,
		scope, parent, slug, etype, id)
	return err
}

// onChangeSlugs is a content hook keeping slugs current.
func (s *Service) onChangeSlugs(ctx context.Context, event, etype string, id int64) {
	if etype != "element" && etype != "category" {
		return
	}
	switch event {
	case "create", "update", "restore", "rollback":
		_ = s.syncSlug(context.WithoutCancel(ctx), etype, id, -1)
	}
}

// Path returns the readable path of an element or category ("" if it has
// no slug yet; assigned on the next change).
func (s *Service) Path(ctx context.Context, etype string, id int64) string {
	var cat int64
	var last string
	if etype == "element" {
		if err := s.pool.QueryRow(ctx, `SELECT category_id, slug FROM elements WHERE id=$1`, id).Scan(&cat, &last); err != nil {
			return ""
		}
		if last == "" {
			if s.syncSlug(ctx, etype, id, -1) != nil {
				return ""
			}
			_ = s.pool.QueryRow(ctx, `SELECT slug FROM elements WHERE id=$1`, id).Scan(&last)
		}
	} else {
		cat = id
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.slug FROM categories c, categories t
		WHERE t.id=$1 AND c.path @> t.path ORDER BY nlevel(c.path)`, cat)
	if err != nil {
		return ""
	}
	var segs []string
	var missing []int64
	for rows.Next() {
		var cid int64
		var slug string
		if rows.Scan(&cid, &slug) != nil {
			rows.Close()
			return ""
		}
		if slug == "" {
			missing = append(missing, cid)
		}
		segs = append(segs, slug)
	}
	rows.Close()
	if len(missing) > 0 {
		for _, cid := range missing {
			if s.syncSlug(ctx, "category", cid, -1) != nil {
				return ""
			}
		}
		return s.Path(ctx, etype, id)
	}
	if etype == "element" {
		segs = append(segs, last)
	}
	if len(segs) == 0 {
		return ""
	}
	return PathPrefix + strings.Join(segs, "/")
}

// resolvePath finds the target of /wiki/a/b/c (with former segments).
func (s *Service) resolvePath(ctx context.Context, segs []string) (*Resolved, bool, error) {
	parent := int64(0)
	var res *Resolved
	aliased := false
	for i, seg := range segs {
		if res != nil && res.Type == "element" {
			return nil, false, apperr.NotFound("page")
		}
		last := i == len(segs)-1
		var id int64
		err := s.pool.QueryRow(ctx, `SELECT id FROM categories WHERE parent_id IS NOT DISTINCT FROM NULLIF($1, 0) AND slug=$2 AND deleted_at IS NULL ORDER BY id LIMIT 1`, parent, seg).Scan(&id)
		if err == nil {
			res, parent = &Resolved{Type: "category", ID: id}, id
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, false, err
		}
		if last && parent != 0 {
			err = s.pool.QueryRow(ctx, `SELECT id FROM elements WHERE category_id=$1 AND slug=$2 AND deleted_at IS NULL ORDER BY id LIMIT 1`, parent, seg).Scan(&id)
			if err == nil {
				res = &Resolved{Type: "element", ID: id}
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, false, err
			}
		}
		var t string
		err = s.pool.QueryRow(ctx, `SELECT target_type, target_id FROM url_aliases WHERE scope='path' AND parent_id=$1 AND slug=$2`, parent, seg).Scan(&t, &id)
		if err != nil {
			return nil, false, apperr.NotFound("page")
		}
		aliased = true
		res = &Resolved{Type: t, ID: id}
		if t == "category" {
			parent = id
		}
	}
	if res == nil {
		return nil, false, apperr.NotFound("page")
	}
	return res, aliased, nil
}

// checkVisible returns the target if the caller may view it.
func (s *Service) checkVisible(ctx context.Context, r *Resolved) error {
	cat := r.ID
	if r.Type == "element" {
		var deleted bool
		if err := s.pool.QueryRow(ctx, `SELECT category_id, deleted_at IS NOT NULL FROM elements WHERE id=$1`, r.ID).Scan(&cat, &deleted); err != nil || deleted {
			return apperr.NotFound("page")
		}
	} else {
		var deleted bool
		if err := s.pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM categories WHERE id=$1`, r.ID).Scan(&deleted); err != nil || deleted {
			return apperr.NotFound("page")
		}
	}
	if !s.acl.Can(ctx, identity.From(ctx), cat, acl.View) {
		if identity.From(ctx) == nil {
			return apperr.ErrUnauthorized
		}
		return apperr.NotFound("page")
	}
	return nil
}

// Suggestion is a probable target for an address that does not exist.
type Suggestion struct {
	Type  string `json:"type"`
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

// NotFoundInfo helps on "page not found": a deleted page (for those who
// could see it) and probable pages matching the address or former title.
type NotFoundInfo struct {
	Deleted *struct {
		Type       string `json:"type"`
		ID         int64  `json:"id"`
		Title      string `json:"title"`
		CanRestore bool   `json:"can_restore"`
	} `json:"deleted"`
	Suggestions []Suggestion `json:"suggestions"`
}

// NotFound explains a missing address: path (readable path or short URL),
// or the id of an element or category (etype).
func (s *Service) NotFound(ctx context.Context, path, etype string, id int64, lang string) (*NotFoundInfo, error) {
	out := &NotFoundInfo{Suggestions: []Suggestion{}}
	var words []string
	if etype == "element" || etype == "category" {
		var cat int64
		var deleted bool
		var title string
		if etype == "element" {
			err := s.pool.QueryRow(ctx, `SELECT e.category_id, e.deleted_at IS NOT NULL, COALESCE((SELECT title FROM element_revisions r WHERE r.element_id=e.id ORDER BY (r.lang=$2) DESC, r.created_at DESC LIMIT 1), '')
				FROM elements e WHERE e.id=$1`, id, lang).Scan(&cat, &deleted, &title)
			if err != nil {
				return out, nil
			}
		} else {
			var titles map[string]string
			if err := s.pool.QueryRow(ctx, `SELECT id, deleted_at IS NOT NULL, titles FROM categories WHERE id=$1`, id).Scan(&cat, &deleted, &titles); err != nil {
				return out, nil
			}
			title = firstText(titles, lang, s.DefaultLanguage())
		}
		// only who could see it learns that it was deleted
		perms, err := s.acl.Perms(ctx, identity.From(ctx), cat)
		if err != nil || perms&acl.View == 0 {
			return out, nil
		}
		if deleted {
			out.Deleted = &struct {
				Type       string `json:"type"`
				ID         int64  `json:"id"`
				Title      string `json:"title"`
				CanRestore bool   `json:"can_restore"`
			}{etype, id, title, perms&acl.Delete != 0}
		}
		words = append(words, title)
	}
	if path != "" {
		p := strings.TrimPrefix(strings.Trim(path, "/"), strings.Trim(PathPrefix, "/")+"/")
		segs := strings.Split(p, "/")
		words = append(words, strings.ReplaceAll(segs[len(segs)-1], "-", " "))
		// a deleted page with this address (its slug or a former one)
		var t string
		var tid int64
		last := segs[len(segs)-1]
		err := s.pool.QueryRow(ctx, `SELECT t, id FROM (
			SELECT 'element' AS t, id, deleted_at AS at FROM elements WHERE slug=$1 AND deleted_at IS NOT NULL
			UNION ALL SELECT 'category', id, deleted_at FROM categories WHERE slug=$1 AND deleted_at IS NOT NULL
			UNION ALL SELECT a.target_type, a.target_id, a.created_at FROM url_aliases a WHERE a.scope='path' AND a.slug=$1) x
			ORDER BY at DESC LIMIT 1`, last).Scan(&t, &tid)
		if err == nil {
			if info, _ := s.NotFound(ctx, "", t, tid, lang); info != nil && info.Deleted != nil {
				out.Deleted = info.Deleted
			}
		}
	}
	q := strings.TrimSpace(strings.Join(words, " "))
	if q == "" {
		return out, nil
	}
	res, err := s.Search(ctx, q, nil, lang, 5)
	if err != nil || res == nil {
		return out, nil
	}
	seen := map[string]bool{}
	for _, c := range res.Categories {
		if len(out.Suggestions) >= 5 {
			break
		}
		key := fmt.Sprintf("c%d", c.ID)
		if !seen[key] && !(etype == "category" && c.ID == id) {
			seen[key] = true
			out.Suggestions = append(out.Suggestions, Suggestion{Type: "category", ID: c.ID, Title: firstText(c.Titles, lang, s.DefaultLanguage()), Path: s.Path(ctx, "category", c.ID)})
		}
	}
	for _, h := range res.Elements {
		if len(out.Suggestions) >= 5 {
			break
		}
		key := fmt.Sprintf("e%d", h.ElementID)
		if !seen[key] && !(etype == "element" && h.ElementID == id) {
			seen[key] = true
			out.Suggestions = append(out.Suggestions, Suggestion{Type: "element", ID: h.ElementID, Title: h.Title, Path: s.Path(ctx, "element", h.ElementID)})
		}
	}
	return out, nil
}
