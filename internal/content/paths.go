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

// Readable paths: every category and element has a segment per language
// (from its title in that language; the original language where a
// translation is missing), unique among the children of its parent category
// (subcategories and elements share the namespace, across all languages).
// The path of an element is /wiki/<category segments>/<element segment>, of
// a category /wiki/<category segments>. Paths in every language resolve.
//
// When a slug changes (new title) or something moves, the old segment is
// kept as an alias (url_aliases, scope "path": parent + slug), so old links
// keep working and are redirected. The same applies to short URLs (scope
// "short").

// PathPrefix is the first segment of readable paths.
const PathPrefix = "/wiki/"

// slugFor is the segment of a title (fallback: type and id).
func slugFor(title, fallback string) string {
	if s := Slugify(title); s != "" {
		return s
	}
	return fallback
}

// slugTaken reports whether slug is used by another target among the
// children of parent (in any language; deleted targets free their segments).
func slugTaken(ctx context.Context, q db.Querier, parent int64, slug, selfType string, selfID int64) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM path_slugs p
		LEFT JOIN elements e ON p.target_type='element' AND e.id=p.target_id
		LEFT JOIN categories c ON p.target_type='category' AND c.id=p.target_id
		WHERE p.parent_id=$1 AND p.slug=$2 AND NOT (p.target_type=$3 AND p.target_id=$4)
		  AND COALESCE(e.deleted_at, c.deleted_at) IS NULL AND (e.id IS NOT NULL OR c.id IS NOT NULL)`,
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
	titles  map[string]string
	orig    string
	deleted bool
}

func (s *Service) slugState(ctx context.Context, q db.Querier, etype string, id int64) (*slugState, error) {
	st := &slugState{}
	var err error
	if etype == "element" {
		err = q.QueryRow(ctx, `SELECT category_id, titles, original_lang, deleted_at IS NOT NULL FROM elements WHERE id=$1`, id).
			Scan(&st.parent, &st.titles, &st.orig, &st.deleted)
	} else {
		var parent *int64
		err = q.QueryRow(ctx, `SELECT parent_id, titles, original_lang, deleted_at IS NOT NULL FROM categories WHERE id=$1`, id).
			Scan(&parent, &st.titles, &st.orig, &st.deleted)
		if parent != nil {
			st.parent = *parent
		}
	}
	if err != nil {
		return nil, err
	}
	if st.orig == "" || st.titles[st.orig] == "" {
		st.orig = s.originalOf(st.titles, st.orig)
	}
	return st, nil
}

// syncSlug updates the path segments (one per language) of an element or
// category after a change. Former segments (old parent and slug) become
// aliases. oldParent is the parent before a move (-1 = unchanged).
func (s *Service) syncSlug(ctx context.Context, etype string, id int64, oldParent int64) error {
	return db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('ewiki.slugs'))`); err != nil {
			return err
		}
		st, err := s.slugState(ctx, tx, etype, id)
		if err != nil || st.deleted {
			return err
		}
		cur := map[string]string{}
		curParent := map[string]int64{}
		rows, err := tx.Query(ctx, `SELECT lang, slug, parent_id FROM path_slugs WHERE target_type=$1 AND target_id=$2`, etype, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var l, sl string
			var par int64
			if err := rows.Scan(&l, &sl, &par); err != nil {
				rows.Close()
				return err
			}
			cur[l], curParent[l] = sl, par
		}
		rows.Close()
		fallback := fmt.Sprintf("%s-%d", etype[:1], id)
		want := map[string]string{} // lang -> base slug
		for _, l := range s.Languages() {
			if t := strings.TrimSpace(st.titles[l]); t != "" {
				want[l] = slugFor(t, fallback)
			}
		}
		if _, ok := want[st.orig]; !ok {
			want[st.orig] = slugFor(firstText(st.titles, st.orig, s.DefaultLanguage()), fallback)
		}
		for l, base := range want {
			old, had := cur[l]
			moved := had && curParent[l] != st.parent
			slug := old
			if !had || !sameBase(old, base) || moved {
				slug = base
			}
			// the same segment as another language of this target is fine
			if slug, err = uniqueSlug(ctx, tx, st.parent, slug, etype, id); err != nil {
				return err
			}
			if had && slug == old && !moved {
				continue
			}
			if had && old != "" {
				if err := addAlias(ctx, tx, "path", curParent[l], old, etype, id); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM url_aliases WHERE scope='path' AND parent_id=$1 AND slug=$2`, st.parent, slug); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO path_slugs(target_type, target_id, lang, parent_id, slug) VALUES ($1,$2,$3,$4,$5)
				ON CONFLICT (target_type, target_id, lang) DO UPDATE SET parent_id=EXCLUDED.parent_id, slug=EXCLUDED.slug`, etype, id, l, st.parent, slug); err != nil {
				return err
			}
		}
		// languages without a title any more: their segment becomes an alias
		for l, old := range cur {
			if _, ok := want[l]; ok {
				continue
			}
			if err := addAlias(ctx, tx, "path", curParent[l], old, etype, id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM path_slugs WHERE target_type=$1 AND target_id=$2 AND lang=$3`, etype, id, l); err != nil {
				return err
			}
		}
		_ = oldParent
		return nil
	})
}

func addAlias(ctx context.Context, tx pgx.Tx, scope string, parent int64, slug, etype string, id int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO url_aliases(scope, parent_id, slug, target_type, target_id) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (scope, parent_id, slug) DO UPDATE SET target_type=EXCLUDED.target_type, target_id=EXCLUDED.target_id, created_at=now()`,
		scope, parent, slug, etype, id)
	return err
}

// onChangeSlugs is a content hook keeping path segments current.
func (s *Service) onChangeSlugs(ctx context.Context, event, etype string, id int64) {
	if etype != "element" && etype != "category" {
		return
	}
	switch event {
	case "create", "update", "restore", "rollback":
		_ = s.syncSlug(context.WithoutCancel(ctx), etype, id, -1)
	}
}

// slugsOf returns the segments of a target per language (assigned when missing).
func (s *Service) slugsOf(ctx context.Context, etype string, id int64) (map[string]string, error) {
	load := func() (map[string]string, error) {
		rows, err := s.pool.Query(ctx, `SELECT lang, slug FROM path_slugs WHERE target_type=$1 AND target_id=$2`, etype, id)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		m := map[string]string{}
		for rows.Next() {
			var l, sl string
			if err := rows.Scan(&l, &sl); err != nil {
				return nil, err
			}
			m[l] = sl
		}
		return m, rows.Err()
	}
	m, err := load()
	if err != nil || len(m) > 0 {
		return m, err
	}
	if err := s.syncSlug(ctx, etype, id, -1); err != nil {
		return nil, err
	}
	return load()
}

// shortOnly returns the short URL of a target that may only be reached by it.
func (s *Service) shortOnly(ctx context.Context, etype string, id int64) string {
	table := "elements"
	if etype == "category" {
		table = "categories"
	}
	var short *string
	var only bool
	if s.pool.QueryRow(ctx, `SELECT short_url, short_only FROM `+table+` WHERE id=$1`, id).Scan(&short, &only) != nil || !only || short == nil || *short == "" {
		return ""
	}
	return "/" + *short
}

// Path returns the readable path of an element or category in lang (the
// original language where a translation is missing). "" if readable paths
// are switched off; the short URL if the target only allows that.
func (s *Service) Path(ctx context.Context, etype string, id int64, lang string) string {
	return s.Paths(ctx, etype, id)[lang]
}

// Paths returns the readable path of a target for every configured language.
func (s *Service) Paths(ctx context.Context, etype string, id int64) map[string]string {
	out := map[string]string{}
	if !s.cfg.Get().UI.ReadableURLs {
		if short := s.shortOnly(ctx, etype, id); short != "" {
			for _, l := range s.Languages() {
				out[l] = short
			}
		}
		return out
	}
	if short := s.shortOnly(ctx, etype, id); short != "" {
		for _, l := range s.Languages() {
			out[l] = short
		}
		return out
	}
	var cat int64
	if etype == "element" {
		if s.pool.QueryRow(ctx, `SELECT category_id FROM elements WHERE id=$1`, id).Scan(&cat) != nil {
			return out
		}
	} else {
		cat = id
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.original_lang FROM categories c, categories t WHERE t.id=$1 AND c.path @> t.path ORDER BY nlevel(c.path)`, cat)
	if err != nil {
		return out
	}
	type node struct {
		id   int64
		orig string
	}
	var chain []node
	for rows.Next() {
		var n node
		if rows.Scan(&n.id, &n.orig) != nil {
			rows.Close()
			return out
		}
		chain = append(chain, n)
	}
	rows.Close()
	segs := make([]map[string]string, 0, len(chain)+1)
	origs := make([]string, 0, len(chain)+1)
	for _, n := range chain {
		m, err := s.slugsOf(ctx, "category", n.id)
		if err != nil {
			return out
		}
		segs, origs = append(segs, m), append(origs, n.orig)
	}
	if etype == "element" {
		m, err := s.slugsOf(ctx, "element", id)
		if err != nil {
			return out
		}
		var orig string
		_ = s.pool.QueryRow(ctx, `SELECT original_lang FROM elements WHERE id=$1`, id).Scan(&orig)
		segs, origs = append(segs, m), append(origs, orig)
	}
	for _, l := range s.Languages() {
		parts := make([]string, 0, len(segs))
		for i, m := range segs {
			sl := m[l]
			if sl == "" {
				sl = m[origs[i]]
			}
			if sl == "" {
				for _, v := range m { // any language
					sl = v
					break
				}
			}
			if sl == "" {
				parts = nil
				break
			}
			parts = append(parts, sl)
		}
		if len(parts) > 0 {
			out[l] = PathPrefix + strings.Join(parts, "/")
		}
	}
	return out
}

// resolvePath finds the target of /wiki/a/b/c (segments in any language,
// former segments too). lang is the language of the last matched segment.
func (s *Service) resolvePath(ctx context.Context, segs []string) (*Resolved, bool, string, error) {
	parent := int64(0)
	var res *Resolved
	aliased := false
	lang := ""
	for i, seg := range segs {
		if res != nil && res.Type == "element" {
			return nil, false, "", apperr.NotFound("page")
		}
		last := i == len(segs)-1
		var t, l string
		var id int64
		err := s.pool.QueryRow(ctx, `SELECT p.target_type, p.target_id, p.lang FROM path_slugs p
			LEFT JOIN elements e ON p.target_type='element' AND e.id=p.target_id
			LEFT JOIN categories c ON p.target_type='category' AND c.id=p.target_id
			WHERE p.parent_id=$1 AND p.slug=$2 AND ($3 OR p.target_type='category')
			  AND COALESCE(e.deleted_at, c.deleted_at) IS NULL AND (e.id IS NOT NULL OR c.id IS NOT NULL)
			  AND NOT COALESCE(e.short_only AND e.short_url IS NOT NULL, c.short_only AND c.short_url IS NOT NULL, false)
			ORDER BY (p.target_type='category') DESC, p.target_id LIMIT 1`, parent, seg, last && parent != 0).Scan(&t, &id, &l)
		if err == nil {
			res, lang = &Resolved{Type: t, ID: id}, l
			if t == "category" {
				parent = id
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, false, "", err
		}
		err = s.pool.QueryRow(ctx, `SELECT target_type, target_id FROM url_aliases WHERE scope='path' AND parent_id=$1 AND slug=$2`, parent, seg).Scan(&t, &id)
		if err != nil {
			return nil, false, "", apperr.NotFound("page")
		}
		aliased = true
		res = &Resolved{Type: t, ID: id}
		if t == "category" {
			parent = id
		}
	}
	if res == nil {
		return nil, false, "", apperr.NotFound("page")
	}
	if aliased && s.shortOnly(ctx, res.Type, res.ID) != "" {
		return nil, false, "", apperr.NotFound("page")
	}
	return res, aliased, lang, nil
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
			SELECT p.target_type AS t, p.target_id AS id, COALESCE(e.deleted_at, c.deleted_at) AS at FROM path_slugs p
				LEFT JOIN elements e ON p.target_type='element' AND e.id=p.target_id
				LEFT JOIN categories c ON p.target_type='category' AND c.id=p.target_id
				WHERE p.slug=$1 AND COALESCE(e.deleted_at, c.deleted_at) IS NOT NULL
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
			out.Suggestions = append(out.Suggestions, Suggestion{Type: "category", ID: c.ID, Title: firstText(c.Titles, lang, s.DefaultLanguage()), Path: s.Path(ctx, "category", c.ID, lang)})
		}
	}
	for _, h := range res.Elements {
		if len(out.Suggestions) >= 5 {
			break
		}
		key := fmt.Sprintf("e%d", h.ElementID)
		if !seen[key] && !(etype == "element" && h.ElementID == id) {
			seen[key] = true
			out.Suggestions = append(out.Suggestions, Suggestion{Type: "element", ID: h.ElementID, Title: h.Title, Path: s.Path(ctx, "element", h.ElementID, lang)})
		}
	}
	return out, nil
}
