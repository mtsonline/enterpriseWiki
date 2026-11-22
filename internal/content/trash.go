package content

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// TrashItem is a deleted category or element.
type TrashItem struct {
	Type      string            `json:"type"` // category | element
	ID        int64             `json:"id"`
	Titles    map[string]string `json:"titles"`
	ParentID  *int64            `json:"parent_id"` // category: parent, element: category
	DeletedAt time.Time         `json:"deleted_at"`
	DeletedBy string            `json:"deleted_by"`
	Contains  int               `json:"contains"` // number of objects deleted with it
}

// Trash lists the objects the caller deleted or may restore. Admins see all.
func (s *Service) Trash(ctx context.Context) ([]TrashItem, error) {
	p := identity.From(ctx)
	if p == nil {
		return nil, apperr.ErrUnauthorized
	}
	// acl: filtered per item below (CanElement for pages; administrators see all)
	rows, err := s.pool.Query(ctx, `
		SELECT 'category', c.id, c.titles, c.parent_id, c.deleted_at, COALESCE(u.username || '@' || u.provider, ''),
			(SELECT count(*) FROM categories x WHERE x.path <@ c.path AND x.deleted_at = c.deleted_at) - 1 +
			(SELECT count(*) FROM elements e WHERE e.deleted_at = c.deleted_at AND e.category_id IN (SELECT id FROM categories y WHERE y.path <@ c.path))
		FROM categories c LEFT JOIN users u ON u.id=c.deleted_by WHERE c.trash_root AND c.deleted_at IS NOT NULL
		UNION ALL
		SELECT 'element', e.id, e.titles, e.category_id, e.deleted_at, COALESCE(u.username || '@' || u.provider, ''), 0
		FROM elements e LEFT JOIN users u ON u.id=e.deleted_by WHERE e.trash_root AND e.deleted_at IS NOT NULL
		ORDER BY 5 DESC LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TrashItem, error) {
		var t TrashItem
		return t, r.Scan(&t.Type, &t.ID, &t.Titles, &t.ParentID, &t.DeletedAt, &t.DeletedBy, &t.Contains)
	})
	if err != nil {
		return nil, err
	}
	if p.Admin() {
		return items, nil
	}
	out := []TrashItem{}
	for _, it := range items {
		switch {
		case it.ParentID == nil:
		case it.Type == "category":
			if s.acl.Can(ctx, p, *it.ParentID, acl.Manage) { // categories: manage the parent
				out = append(out, it)
			}
		case s.acl.CanElement(ctx, p, it.ID, *it.ParentID, acl.Delete): // elements: delete permission on them
			out = append(out, it)
		}
	}
	return out, nil
}

// Restore restores a trashed category (with everything deleted together with
// it) or element. The parent must not be in the trash.
func (s *Service) Restore(ctx context.Context, typ string, id int64) error {
	switch typ {
	case "category":
		c, err := s.loadCategory(ctx, s.pool, id, true)
		if err != nil {
			return err
		}
		if c.DeletedAt == nil {
			return apperr.BadRequest("category is not in the trash")
		}
		if c.ParentID == nil {
			if err := perm.Require(ctx, "core.categories.admin"); err != nil {
				return err
			}
		} else {
			var parentDeleted bool
			if err := s.pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM categories WHERE id=$1`, *c.ParentID).Scan(&parentDeleted); err != nil {
				return err
			}
			if parentDeleted {
				return apperr.Conflict("the parent category is in the trash; restore it first")
			}
			if !s.acl.Can(ctx, identity.From(ctx), *c.ParentID, acl.Manage) {
				return apperr.ErrForbidden
			}
		}
		var noted int64
		err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
			p := pathString(c.Path)
			if _, err := tx.Exec(ctx, `UPDATE elements SET deleted_at=NULL, deleted_by=NULL WHERE deleted_at=$2
				AND category_id IN (SELECT id FROM categories WHERE path <@ $1::ltree)`, p, *c.DeletedAt); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE categories SET deleted_at=NULL, deleted_by=NULL, trash_root=false WHERE path <@ $1::ltree AND deleted_at=$2`, p, *c.DeletedAt); err != nil {
				return err
			}
			return s.andNote(ctx, tx, &noted, "restore", "category", id, audit.Log(ctx, tx, "category.restore", "category", id, nil, map[string]any{"titles": c.Titles}))
		})
		if err == nil {
			s.aclChanged(ctx)
			s.fireNoted(ctx, noted, "restore", "category", id)
		}
		return err
	case "element":
		e, err := s.loadElement(ctx, s.pool, id, true)
		if err != nil {
			return err
		}
		if e.DeletedAt == nil {
			return apperr.BadRequest("element is not in the trash")
		}
		var catDeleted bool
		if err := s.pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM categories WHERE id=$1`, e.CategoryID).Scan(&catDeleted); err != nil {
			return err
		}
		if catDeleted {
			return apperr.Conflict("the category of this element is in the trash; restore it first")
		}
		if !s.acl.CanElement(ctx, identity.From(ctx), e.ID, e.CategoryID, acl.Delete) {
			return apperr.ErrForbidden
		}
		var noted int64
		err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE elements SET deleted_at=NULL, deleted_by=NULL, trash_root=false WHERE id=$1`, id); err != nil {
				return err
			}
			return s.andNote(ctx, tx, &noted, "restore", "element", id, audit.Log(ctx, tx, "element.restore_trash", "element", id, nil, map[string]any{"titles": e.Titles}))
		})
		if err == nil {
			s.fireNoted(ctx, noted, "restore", "element", id)
		}
		return err
	}
	return apperr.BadRequest("type must be category or element")
}

// Purge permanently deletes a trashed object (admin only).
func (s *Service) Purge(ctx context.Context, typ string, id int64) error {
	if err := perm.Require(ctx, "core.trash.manage"); err != nil {
		return err
	}
	return s.purge(ctx, typ, id)
}

func (s *Service) purge(ctx context.Context, typ string, id int64) error {
	return db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		switch typ {
		case "element":
			if err := RemoveRefs(ctx, tx, []int64{id}, nil); err != nil {
				return err
			}
			// acl: internal – purge (core.trash.manage)
			tag, err := tx.Exec(ctx, `DELETE FROM elements WHERE id=$1 AND deleted_at IS NOT NULL`, id)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return apperr.NotFound("trashed element")
			}
		case "category":
			var path string
			if err := tx.QueryRow(ctx, `SELECT path::text FROM categories WHERE id=$1 AND deleted_at IS NOT NULL`, id).Scan(&path); err != nil {
				return apperr.NotFound("trashed category")
			}
			var alive int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM categories WHERE path <@ $1::ltree AND deleted_at IS NULL`, path).Scan(&alive); err != nil {
				return err
			}
			if alive > 0 {
				return apperr.Conflict("category still contains restored subcategories")
			}
			var liveElems int
			// acl: internal – purge
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM elements WHERE deleted_at IS NULL AND category_id IN (SELECT id FROM categories WHERE path <@ $1::ltree)`, path).Scan(&liveElems); err != nil {
				return err
			}
			if liveElems > 0 {
				return apperr.Conflict("category still contains restored elements")
			}
			if _, err := tx.Exec(ctx, `UPDATE categories SET default_element_id=NULL WHERE path <@ $1::ltree`, path); err != nil {
				return err
			}
			var cats, elems []int64
			rows, err := tx.Query(ctx, `SELECT id FROM categories WHERE path <@ $1::ltree`, path)
			if err != nil {
				return err
			}
			if cats, err = pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
				return err
			}
			// acl: internal – purge
			rows, err = tx.Query(ctx, `SELECT id FROM elements WHERE category_id = ANY($1)`, cats)
			if err != nil {
				return err
			}
			if elems, err = pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
				return err
			}
			if err := RemoveRefs(ctx, tx, elems, cats); err != nil {
				return err
			}
			// acl: internal – purge
			if _, err := tx.Exec(ctx, `DELETE FROM elements WHERE category_id IN (SELECT id FROM categories WHERE path <@ $1::ltree)`, path); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM api_keys WHERE category_id IN (SELECT id FROM categories WHERE path <@ $1::ltree)`, path); err != nil {
				return err
			}
			// delete deepest first because of the parent foreign key
			if _, err := tx.Exec(ctx, `WITH doomed AS (SELECT id FROM categories WHERE path <@ $1::ltree ORDER BY nlevel(path) DESC)
				DELETE FROM categories WHERE id IN (SELECT id FROM doomed)`, path); err != nil {
				return err
			}
		default:
			return apperr.BadRequest("type must be category or element")
		}
		return audit.Log(ctx, tx, typ+".purge", typ, id, nil, nil)
	})
}

// RollbackInput rolls content back to a point in time.
type RollbackInput struct {
	At        time.Time `json:"at"`
	Recursive bool      `json:"recursive"`
	// Structure also restores the structure below the category (needs
	// Recursive): categories and pages moved since go back to their place
	// and order, categories created since go to the trash with their pages.
	Structure bool `json:"structure"`
}

// RollbackResult reports what a rollback changed.
type RollbackResult struct {
	Categories int `json:"categories"`
	Revisions  int `json:"revisions"`
	Trashed    int `json:"trashed"`
	// with Structure: moves and orders undone, categories moved to the trash
	Moved             int `json:"moved"`
	CategoriesTrashed int `json:"categories_trashed"`
	// Skipped: subcategories and pages left as they are, because the caller
	// may not edit and delete there (own rights of a page or subcategory)
	Skipped int `json:"skipped"`
}

// RollbackCategory restores a category (and optionally its whole subtree) to
// the state at in.At: category texts are restored from the audit trail, every
// element language gets a new revision with its content at that time, and
// elements created later are moved to the trash. Nothing is deleted, so a
// rollback can itself be undone.
func (s *Service) RollbackCategory(ctx context.Context, id int64, in RollbackInput) (*RollbackResult, error) {
	if err := s.require(ctx, id, acl.Edit|acl.Delete); err != nil {
		return nil, err
	}
	if in.At.IsZero() || in.At.After(time.Now()) {
		return nil, apperr.BadRequest("'at' must be a time in the past")
	}
	if in.Structure && !in.Recursive {
		return nil, apperr.BadRequest("'structure' needs 'recursive': the structure is the subcategories")
	}
	root, err := s.loadCategory(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	res := &RollbackResult{}
	var noted int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if in.Structure {
			if err := s.rollbackStructure(ctx, tx, root, in.At, res); err != nil {
				return err
			}
		}
		q := `SELECT id FROM categories WHERE id=$1 AND deleted_at IS NULL`
		arg := any(id)
		if in.Recursive {
			q = `SELECT id FROM categories WHERE path <@ $1::ltree AND deleted_at IS NULL`
			arg = pathString(root.Path)
		}
		rows, err := tx.Query(ctx, q, arg)
		if err != nil {
			return err
		}
		cats, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		p := identity.From(ctx)
		for _, cid := range cats {
			if !s.acl.Can(ctx, p, cid, acl.Edit|acl.Delete) {
				res.Skipped++
				continue
			}
			// category texts
			var snap json.RawMessage
			err := tx.QueryRow(ctx, `SELECT after FROM audit_log WHERE entity_type='category' AND entity_id=$1 AND at <= $2
				AND action IN ('category.create','category.update') AND after IS NOT NULL ORDER BY at DESC, id DESC LIMIT 1`, cid, in.At).Scan(&snap)
			if err == nil {
				var st struct {
					Titles       map[string]string `json:"titles"`
					Descriptions map[string]string `json:"descriptions"`
					Tags         []string          `json:"tags"`
					Slug         string            `json:"slug"`
				}
				if json.Unmarshal(snap, &st) == nil && st.Titles != nil {
					if st.Descriptions == nil {
						st.Descriptions = map[string]string{}
					}
					if st.Tags == nil {
						st.Tags = []string{}
					}
					tag, err := tx.Exec(ctx, `UPDATE categories SET titles=$2, descriptions=$3, tags=$4, slug=$5, updated_at=now()
						WHERE id=$1 AND (titles<>$2 OR descriptions<>$3 OR tags<>$4 OR slug<>$5)`, cid, st.Titles, st.Descriptions, st.Tags, st.Slug)
					if err != nil {
						return err
					}
					res.Categories += int(tag.RowsAffected())
				}
			}
			// elements
			// acl: checked per element (CanElement) below
			erows, err := tx.Query(ctx, `SELECT `+elemCols+` FROM elements e WHERE e.category_id=$1 AND e.deleted_at IS NULL`, cid)
			if err != nil {
				return err
			}
			elems, err := pgx.CollectRows(erows, scanElement)
			if err != nil {
				return err
			}
			for i := range elems {
				e := &elems[i]
				if !s.acl.CanElement(ctx, p, e.ID, e.CategoryID, acl.Edit|acl.Delete) {
					res.Skipped++
					continue
				}
				if e.CreatedAt.After(in.At) {
					// moving to the trash is a deletion: a type with own
					// permissions decides (Guard); refused elements stay
					if err := s.guardChange(ctx, e); err != nil {
						if st, _, _ := apperr.Status(err); st == http.StatusForbidden {
							res.Skipped++
							continue
						}
						return err
					}
					if _, err := tx.Exec(ctx, `UPDATE elements SET deleted_at=now(), deleted_by=$2, trash_root=true WHERE id=$1`, e.ID, nullID(ctx)); err != nil {
						return err
					}
					if _, err := tx.Exec(ctx, `UPDATE categories SET default_element_id=NULL WHERE default_element_id=$1`, e.ID); err != nil {
						return err
					}
					res.Trashed++
					continue
				}
				lrows, err := tx.Query(ctx, `SELECT lang, max(rev) FROM element_revisions WHERE element_id=$1 AND created_at <= $2 GROUP BY lang`, e.ID, in.At)
				if err != nil {
					return err
				}
				type lr struct {
					lang string
					rev  int
				}
				targets, err := pgx.CollectRows(lrows, func(r pgx.CollectableRow) (lr, error) {
					var x lr
					return x, r.Scan(&x.lang, &x.rev)
				})
				if err != nil {
					return err
				}
				refused := false
				for _, t := range targets {
					changed, err := s.restoreRev(ctx, tx, e, t.lang, t.rev, "Rollback to "+in.At.UTC().Format(time.RFC3339))
					if err != nil {
						// a type with own permissions refused the change
						// (Guard): this element keeps its content
						if st, _, _ := apperr.Status(err); st == http.StatusForbidden {
							refused = true
							break
						}
						return err
					}
					if changed {
						res.Revisions++
					}
				}
				if refused {
					res.Skipped++
				}
			}
		}
		return s.andNote(ctx, tx, &noted, "rollback", "category", id, audit.Log(ctx, tx, "category.rollback", "category", id, nil, map[string]any{"at": in.At, "recursive": in.Recursive, "result": res}))
	})
	if err != nil {
		return nil, err
	}
	if in.Structure {
		s.aclChanged(ctx)
	}
	s.fireNoted(ctx, noted, "rollback", "category", id)
	return res, nil
}

// rollbackStructure restores the structure below root to the point in time
// at, inside the transaction of the rollback (all or nothing):
//   - moves of categories and pages and orders of subcategories since then
//     are undone, the newest first, so every step is a valid move with the
//     checks of a normal move (permissions, no move into itself);
//   - categories created since then go to the trash with their pages.
//
// The category itself keeps its place. A former place that no longer exists
// stops the rollback with a clear message (restore it from the trash first).
func (s *Service) rollbackStructure(ctx context.Context, tx pgx.Tx, root *Category, at time.Time, res *RollbackResult) error {
	rootPath := pathString(root.Path)
	// what is in the subtree now; a change is part of the rollback when it
	// concerns this subtree (moved within, out of or into it)
	rows, err := tx.Query(ctx, `SELECT id FROM categories WHERE path <@ $1::ltree AND deleted_at IS NULL`, rootPath)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	inside := make(map[int64]bool, len(ids))
	for _, id := range ids {
		inside[id] = true
	}
	type change struct {
		Action   string
		Entity   int64
		Before   map[string]any
		After    map[string]any
		Category int64 // pages: their category now
	}
	maxChanges := limits.Value(s.cfg.Get(), limits.RollbackChanges)
	// acl: internal – structure rollback; moved pages are checked with CanElement
	rows, err = tx.Query(ctx, `SELECT a.action, a.entity_id, COALESCE(a.before, '{}'), COALESCE(a.after, '{}'), COALESCE(e.category_id, 0)
		FROM audit_log a LEFT JOIN elements e ON a.entity_type='element' AND e.id=a.entity_id AND e.deleted_at IS NULL
		WHERE a.at > $1 AND (
			(a.action IN ('category.move', 'category.reorder') AND (a.entity_id = ANY($2)
				OR (a.before->>'parent_id')::bigint = ANY($2) OR (a.after->>'parent_id')::bigint = ANY($2)))
			OR (a.action = 'element.move' AND (e.category_id = ANY($2)
				OR (a.before->>'category_id')::bigint = ANY($2) OR (a.after->>'category_id')::bigint = ANY($2))))
		ORDER BY a.at DESC, a.id DESC LIMIT $3`, at, ids, maxChanges+1)
	if err != nil {
		return err
	}
	changes, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (change, error) {
		var c change
		return c, r.Scan(&c.Action, &c.Entity, &c.Before, &c.After, &c.Category)
	})
	if err != nil {
		return err
	}
	if int64(len(changes)) > maxChanges {
		return apperr.BadRequest("more than %d moves and orders since this point in time: choose a later one (expert setting %s)", maxChanges, limits.RollbackChanges)
	}
	// a check of a normal move: a place that is gone is a conflict (restore it
	// first); a missing permission is refused without naming categories the
	// person may not see
	refused := apperr.Forbidden("the rollback would change categories or pages you may not manage here")
	check := func(id int64, p acl.Perm, what string) error {
		if id == 0 {
			if err := perm.Require(ctx, "core.categories.admin"); err != nil {
				return refused
			}
			return nil
		}
		if err := s.require(ctx, id, p); err != nil {
			var alive bool
			if qerr := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM categories WHERE id=$1 AND deleted_at IS NULL)`, id).Scan(&alive); qerr != nil {
				return qerr
			}
			if !alive {
				return apperr.Conflict("%s no longer exists: restore it from the trash first", what)
			}
			return refused
		}
		return nil
	}
	num := func(m map[string]any, k string) (int64, bool) {
		v, ok := m[k].(float64)
		return int64(v), ok
	}
	for _, c := range changes {
		switch c.Action {
		case "category.move":
			if c.Entity == root.ID {
				continue // the category itself keeps its place
			}
			before, _ := num(c.Before, "parent_id") // nil: top level (0)
			after, _ := num(c.After, "parent_id")
			if !inside[c.Entity] && !inside[before] && !inside[after] {
				continue
			}
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM categories WHERE id=$1 AND deleted_at IS NULL)`, c.Entity).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				continue // in the trash now: not part of the structure
			}
			// the same checks as a move by hand
			if err := check(c.Entity, acl.Manage, "a moved category"); err != nil {
				return err
			}
			if err := check(before, acl.Manage, fmt.Sprintf("the former place (category %d) of category %d", before, c.Entity)); err != nil {
				return err
			}
			var pos *int
			if p, ok := num(c.Before, "position"); ok {
				v := int(p)
				pos = &v
			}
			if err := s.moveCategoryTx(ctx, tx, c.Entity, before, pos); err != nil {
				if isNotFound(err) {
					return apperr.Conflict("the former place (category %d) of category %d no longer exists: restore it from the trash first", before, c.Entity)
				}
				return err
			}
			inside[c.Entity] = true
			res.Moved++
		case "category.reorder":
			if !inside[c.Entity] {
				continue // top level or another subtree
			}
			raw, ok := c.Before["order"].([]any)
			if !ok {
				continue // logged before the order was kept
			}
			order := make([]int64, 0, len(raw))
			for _, x := range raw {
				if f, ok := x.(float64); ok {
					order = append(order, int64(f))
				}
			}
			if err := setOrder(ctx, tx, c.Entity, order); err != nil {
				return err
			}
			res.Moved++
		case "element.move":
			before, _ := num(c.Before, "category_id")
			after, _ := num(c.After, "category_id")
			if c.Category == 0 || (!inside[before] && !inside[after] && !inside[c.Category]) {
				continue // in the trash, or not about this subtree
			}
			if err := check(c.Category, acl.Delete, "the category of a moved page"); err != nil {
				return err
			}
			// the page's own rights
			if !s.acl.CanElement(ctx, identity.From(ctx), c.Entity, c.Category, acl.Delete) {
				return refused
			}
			if err := check(before, acl.Create, fmt.Sprintf("the former category %d of page %d", before, c.Entity)); err != nil {
				return err
			}
			var pos *int
			if p, ok := num(c.Before, "position"); ok {
				v := int(p)
				pos = &v
			}
			if err := s.moveElementTx(ctx, tx, c.Entity, before, pos); err != nil {
				if isNotFound(err) {
					return apperr.Conflict("the former category %d of page %d no longer exists: restore it from the trash first", before, c.Entity)
				}
				return err
			}
			res.Moved++
		}
	}
	// categories created since then (also copies) go to the trash with their pages
	rows, err = tx.Query(ctx, `SELECT id FROM categories n WHERE path <@ $1::ltree AND id <> $2 AND deleted_at IS NULL AND created_at > $3
		AND NOT EXISTS (SELECT 1 FROM categories p WHERE p.id = n.parent_id AND p.created_at > $3 AND p.id <> $2)`, rootPath, root.ID, at)
	if err != nil {
		return err
	}
	tops, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range tops {
		if err := check(id, acl.Delete, "a new category"); err != nil {
			return err
		}
	}
	if len(tops) == 0 {
		return nil
	}
	now, uid := time.Now(), nullID(ctx)
	sub := `SELECT c.id FROM categories c JOIN categories t ON c.path <@ t.path WHERE t.id = ANY($1) AND c.deleted_at IS NULL`
	tag, err := tx.Exec(ctx, `UPDATE elements SET deleted_at=$2, deleted_by=$3, trash_root=false WHERE deleted_at IS NULL AND category_id IN (`+sub+`)`, tops, now, uid)
	if err != nil {
		return err
	}
	res.Trashed += int(tag.RowsAffected()) // pages that go with the new categories
	if _, err := tx.Exec(ctx, `UPDATE categories SET deleted_at=$2, deleted_by=$3, trash_root=(id = ANY($1)) WHERE id IN (`+sub+`)`, tops, now, uid); err != nil {
		return err
	}
	for _, id := range tops {
		if err := audit.Log(ctx, tx, "category.trash", "category", id, map[string]any{"rollback_to": at}, nil); err != nil {
			return err
		}
	}
	res.CategoriesTrashed = len(tops)
	return nil
}

// CategoryHistory lists audit entries of a category and its elements.
func (s *Service) CategoryHistory(ctx context.Context, id int64, limit int) ([]audit.Entry, error) {
	if err := s.require(ctx, id, acl.View); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	f, err := s.acl.ElementFilter(ctx, identity.From(ctx), acl.View)
	if err != nil {
		return nil, err
	}
	args := []any{id, limit}
	// acl: filtered (ElementFilter) – entries of pages the caller may not see are left out
	rows, err := s.pool.Query(ctx, `SELECT id, at, user_id, actor, action, entity_type, entity_id, before, after FROM audit_log
		WHERE (entity_type='category' AND entity_id=$1)
		   OR (entity_type='element' AND entity_id IN (SELECT e.id FROM elements e WHERE e.category_id=$1 AND `+f.SQL("e", &args)+`))
		ORDER BY at DESC, id DESC LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (audit.Entry, error) {
		var e audit.Entry
		return e, r.Scan(&e.ID, &e.At, &e.UserID, &e.Actor, &e.Action, &e.EntityType, &e.EntityID, &e.Before, &e.After)
	})
}

// RetentionResult reports what the retention job removed.
type RetentionResult struct {
	Revisions int64 `json:"revisions"`
	Trash     int   `json:"trash"`
	Audit     int64 `json:"audit"`
}

// RunRetention applies the configured retention rules. The latest revision
// of each element language and the newest revisions_keep_min revisions are
// always kept; only trashed objects are purged.
func (s *Service) RunRetention(ctx context.Context) (*RetentionResult, error) {
	cfg := s.cfg.Get().Retention
	res := &RetentionResult{}
	ctx = identity.With(ctx, &identity.Principal{Username: "retention", Provider: "system", IsAdmin: true, Via: "cli"})
	if cfg.RevisionsKeepDays > 0 {
		tag, err := s.pool.Exec(ctx, `DELETE FROM element_revisions r USING (
				SELECT id, row_number() OVER (PARTITION BY element_id, lang ORDER BY rev DESC) AS n FROM element_revisions) ranked
			WHERE r.id = ranked.id AND ranked.n > $1 AND r.created_at < now() - make_interval(days => $2)`, cfg.RevisionsKeepMin, cfg.RevisionsKeepDays)
		if err != nil {
			return res, err
		}
		res.Revisions = tag.RowsAffected()
	}
	if cfg.TrashDays > 0 {
		// acl: internal – retention (system)
		rows, err := s.pool.Query(ctx, `SELECT 'element', id FROM elements WHERE trash_root AND deleted_at < now() - make_interval(days => $1)
			UNION ALL SELECT 'category', id FROM categories WHERE trash_root AND deleted_at < now() - make_interval(days => $1)`, cfg.TrashDays)
		if err != nil {
			return res, err
		}
		type item struct {
			t  string
			id int64
		}
		items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (item, error) {
			var x item
			return x, r.Scan(&x.t, &x.id)
		})
		if err != nil {
			return res, err
		}
		for _, it := range items {
			if err := s.purge(ctx, it.t, it.id); err != nil {
				s.log.Warn("retention purge failed", "type", it.t, "id", it.id, "err", err)
				continue
			}
			res.Trash++
		}
	}
	if cfg.AuditDays > 0 {
		tag, err := s.pool.Exec(ctx, `DELETE FROM audit_log WHERE at < now() - make_interval(days => $1)`, cfg.AuditDays)
		if err != nil {
			return res, err
		}
		res.Audit = tag.RowsAffected()
	}
	if res.Trash > 0 {
		s.aclChanged(ctx)
	}
	return res, nil
}

// isNotFound reports whether err means that something does not exist.
func isNotFound(err error) bool {
	st, _, _ := apperr.Status(err)
	return st == http.StatusNotFound
}
