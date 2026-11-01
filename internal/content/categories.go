package content

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Category is a node of the content tree.
type Category struct {
	ID               int64             `json:"id"`
	ParentID         *int64            `json:"parent_id"`
	Path             []int64           `json:"path"`   // ancestor ids incl. self
	Number           string            `json:"number"` // human readable position, e.g. "1.2.3"
	Position         int               `json:"position"`
	Slug             string            `json:"slug"`
	Titles           map[string]string `json:"titles"`
	Descriptions     map[string]string `json:"descriptions"`
	Tags             []string          `json:"tags"`
	DefaultElementID *int64            `json:"default_element_id"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	DeletedAt        *time.Time        `json:"deleted_at,omitempty"`
	HasChildren      bool              `json:"has_children"`
	ElementCount     int               `json:"element_count"`
	Permissions      []string          `json:"permissions"`
}

const catCols = `c.id, c.parent_id, c.path::text, c.position, c.slug, c.titles, c.descriptions, c.tags, c.default_element_id,
	c.created_at, c.updated_at, c.deleted_at,
	EXISTS (SELECT 1 FROM categories x WHERE x.parent_id=c.id AND x.deleted_at IS NULL),
	(SELECT count(*) FROM elements e WHERE e.category_id=c.id AND e.deleted_at IS NULL)`

func scanCategory(r pgx.CollectableRow) (Category, error) {
	var c Category
	var path string
	err := r.Scan(&c.ID, &c.ParentID, &path, &c.Position, &c.Slug, &c.Titles, &c.Descriptions, &c.Tags, &c.DefaultElementID,
		&c.CreatedAt, &c.UpdatedAt, &c.DeletedAt, &c.HasChildren, &c.ElementCount)
	c.Path = parsePath(path)
	return c, err
}

func parsePath(p string) []int64 {
	parts := strings.Split(p, ".")
	out := make([]int64, 0, len(parts))
	for _, s := range parts {
		v, _ := strconv.ParseInt(s, 10, 64)
		out = append(out, v)
	}
	return out
}

func pathString(ids []int64) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(s, ".")
}

func (s *Service) loadCategory(ctx context.Context, q db.Querier, id int64, includeDeleted bool) (*Category, error) {
	rows, err := q.Query(ctx, `SELECT `+catCols+` FROM categories c WHERE c.id=$1 AND ($2 OR c.deleted_at IS NULL)`, id, includeDeleted)
	if err != nil {
		return nil, err
	}
	c, err := pgx.CollectExactlyOneRow(rows, scanCategory)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("category")
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Service) decorate(ctx context.Context, cats []Category) ([]Category, error) {
	ids := make([]int64, len(cats))
	for i, c := range cats {
		ids[i] = c.ID
	}
	pm, err := s.acl.PermsMap(ctx, identity.From(ctx), ids)
	if err != nil {
		return nil, err
	}
	out := cats[:0]
	for _, c := range cats {
		p := pm[c.ID]
		if p&acl.View == 0 {
			continue
		}
		c.Permissions = p.Strings()
		out = append(out, c)
	}
	return out, nil
}

// numbers computes the human readable outline numbers ("1.2.3") of path ids.
func (s *Service) numbers(ctx context.Context, path []int64) (string, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id, (SELECT count(*) FROM categories s
			WHERE s.parent_id IS NOT DISTINCT FROM c.parent_id AND s.deleted_at IS NULL
			AND (s.position < c.position OR (s.position = c.position AND s.id <= c.id)))
		FROM categories c WHERE c.id = ANY($1)`, path)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	n := map[int64]int{}
	for rows.Next() {
		var id int64
		var rank int
		if err := rows.Scan(&id, &rank); err != nil {
			return "", err
		}
		n[id] = rank
	}
	parts := make([]string, len(path))
	for i, id := range path {
		parts[i] = strconv.Itoa(n[id])
	}
	return strings.Join(parts, "."), rows.Err()
}

// GetCategory returns a category the caller may view, with outline number.
func (s *Service) GetCategory(ctx context.Context, id int64) (*Category, error) {
	if err := s.require(ctx, id, acl.View); err != nil {
		return nil, err
	}
	c, err := s.loadCategory(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	cs, err := s.decorate(ctx, []Category{*c})
	if err != nil || len(cs) == 0 {
		return nil, apperr.NotFound("category")
	}
	cs[0].Number, err = s.numbers(ctx, cs[0].Path)
	if err != nil {
		return nil, err
	}
	return &cs[0], s.visibleChildren(ctx, cs)
}

// Children lists visible subcategories of parent (0 = root level).
func (s *Service) Children(ctx context.Context, parent int64) ([]Category, error) {
	if parent != 0 {
		if err := s.require(ctx, parent, acl.View); err != nil {
			return nil, err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT `+catCols+` FROM categories c
		WHERE c.deleted_at IS NULL AND (($1 = 0 AND c.parent_id IS NULL) OR c.parent_id = $1)
		ORDER BY c.position, c.id`, parent)
	if err != nil {
		return nil, err
	}
	cats, err := pgx.CollectRows(rows, scanCategory)
	if err != nil {
		return nil, err
	}
	// outline numbers relative to all siblings (stable for every viewer)
	prefix := ""
	if parent != 0 {
		pc, err := s.loadCategory(ctx, s.pool, parent, false)
		if err != nil {
			return nil, err
		}
		if prefix, err = s.numbers(ctx, pc.Path); err != nil {
			return nil, err
		}
		prefix += "."
	}
	for i := range cats {
		cats[i].Number = prefix + strconv.Itoa(i+1)
	}
	cats, err = s.decorate(ctx, cats)
	if err != nil {
		return nil, err
	}
	return cats, s.visibleChildren(ctx, cats)
}

// visibleChildren sets HasChildren only if the caller can see a child, so the
// existence of hidden subcategories is not revealed.
func (s *Service) visibleChildren(ctx context.Context, cats []Category) error {
	ids := make([]int64, 0, len(cats))
	for _, c := range cats {
		if c.HasChildren {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id, parent_id FROM categories WHERE parent_id = ANY($1) AND deleted_at IS NULL`, ids)
	if err != nil {
		return err
	}
	type pc struct{ id, parent int64 }
	kids, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pc, error) {
		var x pc
		return x, r.Scan(&x.id, &x.parent)
	})
	if err != nil {
		return err
	}
	kidIDs := make([]int64, len(kids))
	for i, k := range kids {
		kidIDs[i] = k.id
	}
	perms, err := s.acl.PermsMap(ctx, identity.From(ctx), kidIDs)
	if err != nil {
		return err
	}
	visible := map[int64]bool{}
	for _, k := range kids {
		if perms[k.id]&acl.View != 0 {
			visible[k.parent] = true
		}
	}
	for i := range cats {
		cats[i].HasChildren = visible[cats[i].ID]
	}
	return nil
}

// Breadcrumbs returns the visible ancestors of a category (root first, without self).
func (s *Service) Breadcrumbs(ctx context.Context, c *Category) ([]Category, error) {
	if len(c.Path) <= 1 {
		return []Category{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+catCols+` FROM categories c WHERE c.id = ANY($1)`, c.Path[:len(c.Path)-1])
	if err != nil {
		return nil, err
	}
	cats, err := pgx.CollectRows(rows, scanCategory)
	if err != nil {
		return nil, err
	}
	byID := map[int64]Category{}
	for _, x := range cats {
		byID[x.ID] = x
	}
	ordered := []Category{}
	for _, id := range c.Path[:len(c.Path)-1] {
		if x, ok := byID[id]; ok {
			ordered = append(ordered, x)
		}
	}
	return s.decorate(ctx, ordered)
}

// CategoryInput is used to create or update a category.
type CategoryInput struct {
	ParentID         *int64            `json:"parent_id"`
	Titles           map[string]string `json:"titles"`
	Descriptions     map[string]string `json:"descriptions"`
	Tags             []string          `json:"tags"`
	Slug             *string           `json:"slug"`
	Position         *int              `json:"position"`
	DefaultElementID *int64            `json:"default_element_id"`
	// GuestVisible applies to new root categories (default true: the first
	// level is visible to guests unless disabled).
	GuestVisible *bool `json:"guest_visible"`
}

// CreateCategory creates a category below in.ParentID (nil = root, admin only).
func (s *Service) CreateCategory(ctx context.Context, in CategoryInput) (*Category, error) {
	if in.ParentID == nil || *in.ParentID == 0 {
		in.ParentID = nil
		if err := requireAdmin(ctx); err != nil {
			return nil, err
		}
	} else if err := s.require(ctx, *in.ParentID, acl.Create); err != nil {
		return nil, err
	}
	titles, err := s.normalizeTexts(in.Titles, 200, true)
	if err != nil {
		return nil, err
	}
	descs, err := s.normalizeTexts(in.Descriptions, 2000, false)
	if err != nil {
		return nil, err
	}
	tags, err := NormalizeTags(in.Tags)
	if err != nil {
		return nil, err
	}
	slug := Slugify(firstText(titles, s.DefaultLanguage(), s.DefaultLanguage()))
	if in.Slug != nil && Slugify(*in.Slug) != "" {
		slug = Slugify(*in.Slug)
	}
	var id int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var parentPath string
		if in.ParentID != nil {
			if err := tx.QueryRow(ctx, `SELECT path::text FROM categories WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, *in.ParentID).Scan(&parentPath); err != nil {
				return apperr.NotFound("parent category")
			}
		}
		if err := tx.QueryRow(ctx, `SELECT nextval('categories_id_seq')`).Scan(&id); err != nil {
			return err
		}
		path := strconv.FormatInt(id, 10)
		if parentPath != "" {
			path = parentPath + "." + path
		}
		pos := 0
		if in.Position != nil {
			pos = *in.Position
		} else if err := tx.QueryRow(ctx, `SELECT COALESCE(max(position)+1, 0) FROM categories WHERE parent_id IS NOT DISTINCT FROM $1`, in.ParentID).Scan(&pos); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO categories(id, parent_id, path, position, slug, titles, descriptions, tags, created_by)
			VALUES ($1,$2,$3::ltree,$4,$5,$6,$7,$8,$9)`, id, in.ParentID, path, pos, slug, titles, descs, tags, nullID(ctx)); err != nil {
			return err
		}
		if in.ParentID == nil && (in.GuestVisible == nil || *in.GuestVisible) {
			if _, err := tx.Exec(ctx, `INSERT INTO acl_rules(category_id, subject_type, permissions, inherit, created_by)
				VALUES ($1,'guest',$2,false,$3)`, id, acl.View, nullID(ctx)); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, "category.create", "category", id, nil, snapshotOf(titles, descs, tags, slug, in.ParentID, pos, nil))
	})
	if err != nil {
		return nil, err
	}
	s.aclChanged(ctx)
	s.fire(ctx, "create", "category", id)
	return s.GetCategory(ctx, id)
}

func nullID(ctx context.Context) *int64 {
	if id := identity.From(ctx).ID(); id != 0 {
		return &id
	}
	return nil
}

func snapshotOf(titles, descs map[string]string, tags []string, slug string, parent *int64, pos int, def *int64) map[string]any {
	return map[string]any{"titles": titles, "descriptions": descs, "tags": tags, "slug": slug, "parent_id": parent, "position": pos, "default_element_id": def}
}

// UpdateCategory changes texts, tags, slug, position and default element.
func (s *Service) UpdateCategory(ctx context.Context, id int64, in CategoryInput) (*Category, error) {
	if err := s.require(ctx, id, acl.Edit); err != nil {
		return nil, err
	}
	before, err := s.loadCategory(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	titles, descs, tags, slug, pos, def := before.Titles, before.Descriptions, before.Tags, before.Slug, before.Position, before.DefaultElementID
	if in.Titles != nil {
		if titles, err = s.normalizeTexts(in.Titles, 200, true); err != nil {
			return nil, err
		}
	}
	if in.Descriptions != nil {
		if descs, err = s.normalizeTexts(in.Descriptions, 2000, false); err != nil {
			return nil, err
		}
	}
	if in.Tags != nil {
		if tags, err = NormalizeTags(in.Tags); err != nil {
			return nil, err
		}
	}
	if in.Slug != nil {
		slug = Slugify(*in.Slug)
	}
	if in.Position != nil {
		pos = *in.Position
	}
	if in.DefaultElementID != nil {
		if *in.DefaultElementID == 0 {
			def = nil
		} else {
			var cat int64
			if err := s.pool.QueryRow(ctx, `SELECT category_id FROM elements WHERE id=$1 AND deleted_at IS NULL`, *in.DefaultElementID).Scan(&cat); err != nil || cat != id {
				return nil, apperr.BadRequest("default element must be an element of this category")
			}
			def = in.DefaultElementID
		}
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE categories SET titles=$2, descriptions=$3, tags=$4, slug=$5, position=$6, default_element_id=$7, updated_at=now()
			WHERE id=$1`, id, titles, descs, tags, slug, pos, def); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "category.update", "category", id,
			snapshotOf(before.Titles, before.Descriptions, before.Tags, before.Slug, before.ParentID, before.Position, before.DefaultElementID),
			snapshotOf(titles, descs, tags, slug, before.ParentID, pos, def))
	})
	if err != nil {
		return nil, err
	}
	s.fire(ctx, "update", "category", id)
	return s.GetCategory(ctx, id)
}

// MoveCategory moves a category (with its subtree) below newParent (0 = root).
func (s *Service) MoveCategory(ctx context.Context, id, newParent int64, position *int) (*Category, error) {
	if err := s.require(ctx, id, acl.Delete); err != nil {
		return nil, err
	}
	if newParent == 0 {
		if err := requireAdmin(ctx); err != nil {
			return nil, err
		}
	} else if err := s.require(ctx, newParent, acl.Create); err != nil {
		return nil, err
	}
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var oldPath string
		var oldParent *int64
		var oldPos int
		if err := tx.QueryRow(ctx, `SELECT path::text, parent_id, position FROM categories WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&oldPath, &oldParent, &oldPos); err != nil {
			return apperr.NotFound("category")
		}
		newPrefix := ""
		var parentPtr *int64
		if newParent != 0 {
			if err := tx.QueryRow(ctx, `SELECT path::text FROM categories WHERE id=$1 AND deleted_at IS NULL`, newParent).Scan(&newPrefix); err != nil {
				return apperr.NotFound("target category")
			}
			for _, a := range parsePath(newPrefix) {
				if a == id {
					return apperr.BadRequest("a category cannot be moved into itself or its subcategories")
				}
			}
			parentPtr = &newParent
		}
		pos := 0
		if position != nil {
			pos = *position
		} else if err := tx.QueryRow(ctx, `SELECT COALESCE(max(position)+1,0) FROM categories WHERE parent_id IS NOT DISTINCT FROM $1`, parentPtr).Scan(&pos); err != nil {
			return err
		}
		depth := len(parsePath(oldPath))
		var err error
		if newPrefix == "" {
			_, err = tx.Exec(ctx, `UPDATE categories SET path = subpath(path, $2 - 1) WHERE path <@ $1::ltree`, oldPath, depth)
		} else {
			_, err = tx.Exec(ctx, `UPDATE categories SET path = $3::ltree || subpath(path, $2 - 1) WHERE path <@ $1::ltree`, oldPath, depth, newPrefix)
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE categories SET parent_id=$2, position=$3, updated_at=now() WHERE id=$1`, id, parentPtr, pos); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "category.move", "category", id, map[string]any{"parent_id": oldParent, "position": oldPos}, map[string]any{"parent_id": parentPtr, "position": pos})
	})
	if err != nil {
		return nil, err
	}
	s.aclChanged(ctx)
	s.fire(ctx, "move", "category", id)
	return s.GetCategory(ctx, id)
}

// Reorder sets the order of the given sibling categories.
func (s *Service) Reorder(ctx context.Context, parent int64, ids []int64) error {
	if parent == 0 {
		if err := requireAdmin(ctx); err != nil {
			return err
		}
	} else if err := s.require(ctx, parent, acl.Edit); err != nil {
		return err
	}
	return db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		for i, id := range ids {
			if _, err := tx.Exec(ctx, `UPDATE categories SET position=$2 WHERE id=$1 AND parent_id IS NOT DISTINCT FROM $3`, id, i, nilIfZero(parent)); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, "category.reorder", "category", parent, nil, map[string]any{"order": ids})
	})
}

func nilIfZero(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// DuplicateCategory copies a category with all visible subcategories, the
// latest revisions of their elements and their own permission rules.
func (s *Service) DuplicateCategory(ctx context.Context, id, targetParent int64) (*Category, error) {
	if err := s.require(ctx, id, acl.View); err != nil {
		return nil, err
	}
	if targetParent == 0 {
		if err := requireAdmin(ctx); err != nil {
			return nil, err
		}
	} else if err := s.require(ctx, targetParent, acl.Create); err != nil {
		return nil, err
	}
	src, err := s.loadCategory(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+catCols+` FROM categories c WHERE c.path <@ $1::ltree AND c.deleted_at IS NULL ORDER BY nlevel(c.path), c.position`, pathString(src.Path))
	if err != nil {
		return nil, err
	}
	tree, err := pgx.CollectRows(rows, scanCategory)
	if err != nil {
		return nil, err
	}
	tree, err = s.decorate(ctx, tree) // only what the caller can see
	if err != nil {
		return nil, err
	}
	p := identity.From(ctx)
	var newRoot int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		mapping := map[int64]int64{}
		paths := map[int64]string{}
		var targetPath string
		if targetParent != 0 {
			if err := tx.QueryRow(ctx, `SELECT path::text FROM categories WHERE id=$1`, targetParent).Scan(&targetPath); err != nil {
				return err
			}
			for _, a := range parsePath(targetPath) {
				if a == id {
					return apperr.BadRequest("cannot duplicate a category into itself")
				}
			}
		}
		for _, c := range tree {
			var parent *int64
			var parentPath string
			if c.ID == src.ID {
				if targetParent != 0 {
					parent, parentPath = &targetParent, targetPath
				}
			} else {
				np, ok := mapping[*c.ParentID]
				if !ok {
					continue // parent not visible -> skip subtree
				}
				parent, parentPath = &np, paths[np]
			}
			var nid int64
			if err := tx.QueryRow(ctx, `SELECT nextval('categories_id_seq')`).Scan(&nid); err != nil {
				return err
			}
			path := strconv.FormatInt(nid, 10)
			if parentPath != "" {
				path = parentPath + "." + path
			}
			titles := c.Titles
			pos := c.Position
			if c.ID == src.ID {
				titles = map[string]string{}
				for k, v := range c.Titles {
					titles[k] = v + " (copy)"
				}
				if err := tx.QueryRow(ctx, `SELECT COALESCE(max(position)+1,0) FROM categories WHERE parent_id IS NOT DISTINCT FROM $1`, parent).Scan(&pos); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `INSERT INTO categories(id, parent_id, path, position, slug, titles, descriptions, tags, created_by)
				VALUES ($1,$2,$3::ltree,$4,$5,$6,$7,$8,$9)`, nid, parent, path, pos, c.Slug, titles, c.Descriptions, c.Tags, nullID(ctx)); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO acl_rules(category_id, subject_type, subject_id, permissions, inherit, created_by)
				SELECT $2, subject_type, subject_id, permissions, inherit, $3 FROM acl_rules WHERE category_id=$1`, c.ID, nid, nullID(ctx)); err != nil {
				return err
			}
			mapping[c.ID], paths[nid] = nid, path
			if err := copyElements(ctx, tx, c.ID, nid, p); err != nil {
				return err
			}
		}
		newRoot = mapping[src.ID]
		return audit.Log(ctx, tx, "category.duplicate", "category", newRoot, map[string]any{"source": id}, map[string]any{"categories": len(mapping)})
	})
	if err != nil {
		return nil, err
	}
	s.aclChanged(ctx)
	return s.GetCategory(ctx, newRoot)
}

// copyElements copies the non-deleted elements of category from into to.
func copyElements(ctx context.Context, tx pgx.Tx, from, to int64, p *identity.Principal) error {
	rows, err := tx.Query(ctx, `SELECT id FROM elements WHERE category_id=$1 AND deleted_at IS NULL ORDER BY position, id`, from)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, eid := range ids {
		if _, err := copyElement(ctx, tx, eid, to, ""); err != nil {
			return err
		}
	}
	return nil
}

// copyElement copies the latest revision of every language of element eid.
func copyElement(ctx context.Context, tx pgx.Tx, eid, to int64, titleSuffix string) (int64, error) {
	var nid int64
	uid := nullID(ctx)
	if err := tx.QueryRow(ctx, `INSERT INTO elements(category_id, type, position, original_lang, titles, tags, created_by, updated_by)
		SELECT $2, type, position, original_lang, titles, tags, $3, $3 FROM elements WHERE id=$1 RETURNING id`, eid, to, uid).Scan(&nid); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO element_revisions(element_id, lang, rev, source_rev, title, body, comment, created_by)
		SELECT $2, r.lang, 1, CASE WHEN r.source_rev IS NULL THEN NULL ELSE 1 END, r.title || $4::text, r.body, 'Copied from element #' || $1::bigint::text, $3
		FROM element_revisions r WHERE r.element_id=$1::bigint AND r.rev = (SELECT max(rev) FROM element_revisions x WHERE x.element_id=$1::bigint AND x.lang=r.lang)`,
		eid, nid, uid, titleSuffix); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE elements e SET titles = (SELECT jsonb_object_agg(lang, title) FROM element_revisions WHERE element_id=$1 AND rev=1) WHERE id=$1`, nid); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO search_docs(element_id, lang, category_id, title, body_text, tags, tsv)
		SELECT $2, d.lang, $3, d.title || $4::text, d.body_text, d.tags, d.tsv FROM search_docs d WHERE d.element_id=$1`, eid, nid, to, titleSuffix); err != nil {
		return 0, err
	}
	return nid, nil
}

// DeleteCategory moves a category, its subcategories and their elements to the trash.
func (s *Service) DeleteCategory(ctx context.Context, id int64) error {
	if err := s.require(ctx, id, acl.Delete); err != nil {
		return err
	}
	c, err := s.loadCategory(ctx, s.pool, id, false)
	if err != nil {
		return err
	}
	if c.ParentID == nil {
		if err := requireAdmin(ctx); err != nil {
			return err
		}
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		now := time.Now()
		uid := nullID(ctx)
		p := pathString(c.Path)
		if _, err := tx.Exec(ctx, `UPDATE elements SET deleted_at=$2, deleted_by=$3, trash_root=false
			WHERE deleted_at IS NULL AND category_id IN (SELECT id FROM categories WHERE path <@ $1::ltree)`, p, now, uid); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE categories SET deleted_at=$2, deleted_by=$3, trash_root=(id=$4)
			WHERE deleted_at IS NULL AND path <@ $1::ltree`, p, now, uid, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "category.trash", "category", id, map[string]any{"titles": c.Titles}, nil)
	})
	if err != nil {
		return err
	}
	s.aclChanged(ctx)
	s.fire(ctx, "delete", "category", id)
	return nil
}

// TreeNode is a light category entry for pickers.
type TreeNode struct {
	ID       int64             `json:"id"`
	ParentID *int64            `json:"parent_id"`
	Depth    int               `json:"depth"`
	Titles   map[string]string `json:"titles"`
	Perms    []string          `json:"permissions"`
}

// Tree returns all visible categories in tree order (depth first), at most limit.
func (s *Service) Tree(ctx context.Context, limit int) ([]TreeNode, error) {
	if limit <= 0 || limit > 20000 {
		limit = 5000
	}
	rows, err := s.pool.Query(ctx, `SELECT id, parent_id, nlevel(path), titles, path::text FROM categories WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	type row struct {
		n    TreeNode
		path []int64
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		var p string
		err := r.Scan(&x.n.ID, &x.n.ParentID, &x.n.Depth, &x.n.Titles, &p)
		x.path = parsePath(p)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	pos := map[int64]int{}
	prow, err := s.pool.Query(ctx, `SELECT id, row_number() OVER (PARTITION BY parent_id ORDER BY position, id) FROM categories WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	for prow.Next() {
		var id int64
		var n int
		if err := prow.Scan(&id, &n); err != nil {
			prow.Close()
			return nil, err
		}
		pos[id] = n
	}
	prow.Close()
	ids := make([]int64, len(all))
	for i, x := range all {
		ids[i] = x.n.ID
	}
	perms, err := s.acl.PermsMap(ctx, identity.From(ctx), ids)
	if err != nil {
		return nil, err
	}
	// sort by the sequence of sibling positions along the path
	key := func(p []int64) []int {
		k := make([]int, len(p))
		for i, id := range p {
			k[i] = pos[id]
		}
		return k
	}
	less := func(a, b []int) bool {
		for i := 0; i < len(a) && i < len(b); i++ {
			if a[i] != b[i] {
				return a[i] < b[i]
			}
		}
		return len(a) < len(b)
	}
	keys := make([][]int, len(all))
	for i := range all {
		keys[i] = key(all[i].path)
	}
	idx := make([]int, len(all))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(i, j int) bool { return less(keys[idx[i]], keys[idx[j]]) })
	out := []TreeNode{}
	for _, i := range idx {
		x := all[i]
		visible := true
		for _, a := range x.path {
			if perms[a]&acl.View == 0 {
				visible = false
				break
			}
		}
		if !visible {
			continue
		}
		x.n.Perms = perms[x.n.ID].Strings()
		out = append(out, x.n)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
