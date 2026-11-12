package content

import (
	"context"
	"encoding/json"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
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
		need := acl.Delete // elements: delete permission in their category
		if it.Type == "category" {
			need = acl.Manage // categories: manage the parent
		}
		if it.ParentID != nil && s.acl.Can(ctx, p, *it.ParentID, need) {
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
		if !s.acl.Can(ctx, identity.From(ctx), e.CategoryID, acl.Delete) {
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
}

// RollbackResult reports what a rollback changed.
type RollbackResult struct {
	Categories int `json:"categories"`
	Revisions  int `json:"revisions"`
	Trashed    int `json:"trashed"`
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
	root, err := s.loadCategory(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	res := &RollbackResult{}
	var noted int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
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
		for _, cid := range cats {
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
				if e.CreatedAt.After(in.At) {
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
				for _, t := range targets {
					changed, err := s.restoreRev(ctx, tx, e, t.lang, t.rev, "Rollback to "+in.At.UTC().Format(time.RFC3339))
					if err != nil {
						return err
					}
					if changed {
						res.Revisions++
					}
				}
			}
		}
		return s.andNote(ctx, tx, &noted, "rollback", "category", id, audit.Log(ctx, tx, "category.rollback", "category", id, nil, map[string]any{"at": in.At, "recursive": in.Recursive, "result": res}))
	})
	if err != nil {
		return nil, err
	}
	s.fireNoted(ctx, noted, "rollback", "category", id)
	return res, nil
}

// CategoryHistory lists audit entries of a category and its elements.
func (s *Service) CategoryHistory(ctx context.Context, id int64, limit int) ([]audit.Entry, error) {
	if err := s.require(ctx, id, acl.View); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `SELECT id, at, user_id, actor, action, entity_type, entity_id, before, after FROM audit_log
		WHERE (entity_type='category' AND entity_id=$1)
		   OR (entity_type='element' AND entity_id IN (SELECT id FROM elements WHERE category_id=$1))
		ORDER BY at DESC, id DESC LIMIT $2`, id, limit)
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
