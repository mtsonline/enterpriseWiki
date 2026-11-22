package content

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"net/http"
	"regexp"
	"slices"
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
	// Feed: off | on (elements of this category) | recursive (with subcategories)
	Feed      string `json:"feed"`
	ShortURL  string `json:"short_url"`
	ShortOnly bool   `json:"short_only"`
	// Hidden: not in navigation, menus, listings and search (folders for media)
	Hidden bool `json:"hidden"`
	// Template: website template of this category and its subcategories
	// ("" = inherited); EffectiveTemplate is the one that applies ("" = site template)
	Template          string `json:"template"`
	EffectiveTemplate string `json:"effective_template"`
	// EditorTools: buttons of the formatting toolbar here and below (nil =
	// inherited, ["*"] = all); EffectiveEditorTools is the choice that
	// applies (nil = the site's)
	EditorTools          []string `json:"editor_tools"`
	EffectiveEditorTools []string `json:"effective_editor_tools"`
	// SpaceKey: key of a space (top level only, address /s/KEY)
	SpaceKey string `json:"space_key,omitempty"`
	// ArchivedAt: archived itself; Archived: it or a category above (read-only)
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	Archived   bool       `json:"archived"`
	// Classification set here (nil = as above); EffectiveClassification is
	// the strictest on it or above: 0 public, 1 internal, 2 confidential,
	// 3 strictly confidential
	Classification          *int `json:"classification"`
	EffectiveClassification int  `json:"effective_classification"`
	// Watermark (auto|off) and Downloads (auto|allow) of files set here (nil = as above)
	Watermark    *string    `json:"watermark"`
	Downloads    *string    `json:"downloads"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
	HasChildren  bool       `json:"has_children"`
	ElementCount int        `json:"element_count"`
	Permissions  []string   `json:"permissions"`
	// translations of title and description (see cattrans.go)
	OriginalLang         string            `json:"original_lang"`
	TextRev              int               `json:"text_rev"`
	TranslationRevs      map[string]int    `json:"-"`
	Translations         []CatTranslation  `json:"translations"`
	TranslatedPercent    int               `json:"translated_percent"`
	TranslationsUpToDate bool              `json:"translations_up_to_date"`
	URL                  string            `json:"url,omitempty"`  // readable path (/wiki/…) in the default language, single categories only
	URLs                 map[string]string `json:"urls,omitempty"` // readable path per language
}

// acl: the element count is internal – a number only (no titles); the trash and delete dialogs need all elements
const catCols = `c.id, c.parent_id, c.path::text, c.position, c.slug, c.titles, c.descriptions, c.tags, c.default_element_id, c.feed, COALESCE(c.short_url, ''),
	c.created_at, c.updated_at, c.deleted_at,
	EXISTS (SELECT 1 FROM categories x WHERE x.parent_id=c.id AND x.deleted_at IS NULL),
	(SELECT count(*) FROM elements e WHERE e.category_id=c.id AND e.deleted_at IS NULL),
	c.original_lang, c.text_rev, c.translation_revs, c.short_only, c.hidden, c.template, COALESCE(c.space_key, ''), c.archived_at,
	EXISTS (SELECT 1 FROM categories a WHERE a.path @> c.path AND a.archived_at IS NOT NULL),
	c.classification, (SELECT COALESCE(max(a.classification), 0) FROM categories a WHERE a.path @> c.path), c.watermark, c.downloads,
	COALESCE((SELECT t.template FROM categories t WHERE t.path @> c.path AND t.template <> '' ORDER BY nlevel(t.path) DESC LIMIT 1), ''),
	c.editor_tools, (SELECT t.editor_tools FROM categories t WHERE t.path @> c.path AND t.editor_tools IS NOT NULL ORDER BY nlevel(t.path) DESC LIMIT 1)`

func scanCategory(r pgx.CollectableRow) (Category, error) {
	var c Category
	var path string
	err := r.Scan(&c.ID, &c.ParentID, &path, &c.Position, &c.Slug, &c.Titles, &c.Descriptions, &c.Tags, &c.DefaultElementID, &c.Feed, &c.ShortURL,
		&c.CreatedAt, &c.UpdatedAt, &c.DeletedAt, &c.HasChildren, &c.ElementCount, &c.OriginalLang, &c.TextRev, &c.TranslationRevs, &c.ShortOnly, &c.Hidden, &c.Template, &c.SpaceKey, &c.ArchivedAt, &c.Archived, &c.Classification, &c.EffectiveClassification, &c.Watermark, &c.Downloads, &c.EffectiveTemplate,
		&c.EditorTools, &c.EffectiveEditorTools)
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
		if p&(acl.View|acl.Manage) == 0 { // category administrators see the structure
			continue
		}
		c.Permissions = p.Strings()
		s.catTranslations(&c)
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
	if err := s.requireCategory(ctx, id); err != nil {
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
	cs[0].URLs = s.Paths(ctx, "category", id)
	cs[0].URL = cs[0].URLs[s.DefaultLanguage()]
	return &cs[0], s.visibleChildren(ctx, cs)
}

// Children lists visible subcategories of parent (0 = root level).
func (s *Service) Children(ctx context.Context, parent int64) ([]Category, error) {
	return s.ChildrenOpt(ctx, parent, false)
}

// ChildrenOpt lists the subcategories; hidden ones only with withHidden.
func (s *Service) ChildrenOpt(ctx context.Context, parent int64, withHidden bool) ([]Category, error) {
	if parent != 0 {
		if err := s.requireCategory(ctx, parent); err != nil {
			return nil, err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT `+catCols+` FROM categories c
		WHERE c.deleted_at IS NULL AND (($1 = 0 AND c.parent_id IS NULL) OR c.parent_id = $1) AND ($2 OR (NOT c.hidden AND c.archived_at IS NULL))
		ORDER BY c.position, c.id`, parent, withHidden)
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
		if perms[k.id]&(acl.View|acl.Manage) != 0 {
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
	// Feed switches the Atom/RSS feed of the category: off, on, recursive.
	// ShortURL is an additional address such as /kalender ("" removes it).
	ShortURL *string `json:"short_url"`
	// ShortOnly: only the short URL reaches the category (no readable path)
	ShortOnly *bool `json:"short_only"`
	// Hidden: not in navigation, menus, listings and search
	Hidden *bool   `json:"hidden"`
	Feed   *string `json:"feed"`
	// Template: id of an active template plugin ("" = inherited)
	Template *string `json:"template"`
	// EditorTools: buttons of the formatting toolbar here and below; [] =
	// inherited, ["*"] = all, otherwise the names (content.EditorTools)
	EditorTools *[]string `json:"editor_tools"`
	// Classification 0–3; -1 = as above (only a stricter one than above has an effect)
	Classification *int `json:"classification"`
	// Watermark: auto (viewer's name on files of confidential content) or off
	// (original); Downloads: auto (none for strictly confidential) or allow;
	// "" = as above
	Watermark *string `json:"watermark"`
	Downloads *string `json:"downloads"`
	// GuestVisible applies to new top-level categories: true makes them
	// visible to guests (default: only the creator has permissions).
	GuestVisible *bool `json:"guest_visible"`
	// OriginalLang is the language the texts were written in (default: the
	// default language if it has a title); translations are compared with it.
	OriginalLang *string `json:"original_lang"`
	// Via: a section of an instance (a blog, a FAQ …), created with its
	// manage permission (see requireViaCategory); only texts, tags, slug and
	// position are taken then.
	Via int64 `json:"via,omitempty"`
}

// CreateCategory creates a category below in.ParentID (nil = root, admin only).
func (s *Service) CreateCategory(ctx context.Context, in CategoryInput) (*Category, error) {
	var owner *int64
	if in.Via != 0 {
		// a section of an instance (e.g. of a FAQ): its manage permission
		// instead of managing the parent; only texts, tags and position
		if in.ParentID == nil || *in.ParentID == 0 {
			return nil, apperr.BadRequest("parent_id is required with via")
		}
		inst, err := s.requireViaCategory(ctx, in.Via, *in.ParentID)
		if err != nil {
			return nil, err
		}
		owner = &inst.ID
		in = CategoryInput{ParentID: in.ParentID, Titles: in.Titles, Descriptions: in.Descriptions, Tags: in.Tags, Position: in.Position, Slug: in.Slug}
	} else if in.ParentID == nil || *in.ParentID == 0 {
		in.ParentID = nil
		if ok, _ := ctx.Value(personalSpaceKey{}).(bool); !ok { // see CreatePersonalSpace
			if err := perm.Require(ctx, "core.categories.admin"); err != nil {
				return nil, err
			}
		}
	} else if err := s.require(ctx, *in.ParentID, acl.Manage); err != nil {
		return nil, err
	} else if err := s.notArchived(ctx, *in.ParentID); err != nil {
		return nil, err
	}
	if err := s.checkTemplate(ctx, in.Template); err != nil {
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
	var id, noted int64
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
		want := ""
		if in.OriginalLang != nil {
			want = *in.OriginalLang
		}
		orig := s.originalOf(titles, want)
		revs := map[string]int{}
		for l, v := range titles {
			if l != orig && v != "" {
				revs[l] = 1 // written together with the original
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO categories(id, parent_id, path, position, slug, titles, descriptions, tags, created_by, original_lang, translation_revs, owner_instance)
			VALUES ($1,$2,$3::ltree,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, id, in.ParentID, path, pos, slug, titles, descs, tags, nullID(ctx), orig, revs, owner); err != nil {
			return err
		}
		if in.ParentID == nil {
			// a new top-level category belongs to its creator (all permissions,
			// inherited by the subcategories); guests only if asked for
			if uid := identity.From(ctx).ID(); uid > 0 {
				if _, err := tx.Exec(ctx, `INSERT INTO acl_rules(category_id, subject_type, subject_id, permissions, actions, inherit, created_by)
					VALUES ($1,'user',$2,$3,$4,true,$2)`, id, uid, acl.All, perm.Preset(perm.Manager)); err != nil {
					return err
				}
			}
			if in.GuestVisible != nil && *in.GuestVisible {
				if _, err := tx.Exec(ctx, `INSERT INTO acl_rules(category_id, subject_type, permissions, inherit, created_by)
					VALUES ($1,'guest',$2,false,$3)`, id, acl.View, nullID(ctx)); err != nil {
					return err
				}
			}
		}
		if in.ShortURL != nil {
			short, err := NormalizeShortURL(*in.ShortURL)
			if err != nil {
				return err
			}
			if err := s.setShortURL(ctx, tx, "category", id, short); err != nil {
				return err
			}
		}
		if in.Hidden != nil && *in.Hidden {
			if _, err := tx.Exec(ctx, `UPDATE categories SET hidden=true WHERE id=$1`, id); err != nil {
				return err
			}
		}
		if in.Template != nil && *in.Template != "" {
			if _, err := tx.Exec(ctx, `UPDATE categories SET template=$2 WHERE id=$1`, id, *in.Template); err != nil {
				return err
			}
		}
		if in.ShortOnly != nil && *in.ShortOnly {
			if _, err := tx.Exec(ctx, `UPDATE categories SET short_only=true WHERE id=$1`, id); err != nil {
				return err
			}
		}
		if err := audit.Log(ctx, tx, "category.create", "category", id, nil, snapshotOf(titles, descs, tags, slug, in.ParentID, pos, nil)); err != nil {
			return err
		}
		var err error
		noted, err = s.note(ctx, tx, "create", "category", id)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.aclChanged(ctx)
	s.fireNoted(ctx, noted, "create", "category", id)
	return s.GetCategory(ctx, id)
}

func nullID(ctx context.Context) *int64 {
	if id := identity.From(ctx).ID(); id != 0 {
		return &id
	}
	return nil
}

func intPtrEqual(a, b *int) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// checkTemplate accepts "" (inherited) or an active template plugin.
func (s *Service) checkTemplate(ctx context.Context, t *string) error {
	if t == nil || *t == "" {
		return nil
	}
	if !s.TemplateOK(ctx, *t) {
		return apperr.BadRequest("template %q is not an active website template", *t)
	}
	return nil
}

func snapshotOf(titles, descs map[string]string, tags []string, slug string, parent *int64, pos int, def *int64) map[string]any {
	return map[string]any{"titles": titles, "descriptions": descs, "tags": tags, "slug": slug, "parent_id": parent, "position": pos, "default_element_id": def}
}

// UpdateCategory changes texts, tags, slug, position and default element.
func (s *Service) UpdateCategory(ctx context.Context, id int64, in CategoryInput) (*Category, error) {
	if err := s.require(ctx, id, acl.Manage); err != nil {
		return nil, err
	}
	if err := s.checkTemplate(ctx, in.Template); err != nil {
		return nil, err
	}
	if err := s.notArchived(ctx, id); err != nil {
		return nil, err
	}
	if in.Classification != nil && (*in.Classification < -1 || *in.Classification > 3) {
		return nil, apperr.BadRequest("classification: -1 (as above) or 0–3")
	}
	if in.EditorTools != nil && len(*in.EditorTools) > 0 {
		if err := CheckEditorTools(*in.EditorTools); err != nil {
			return nil, err
		}
	}
	if in.Watermark != nil && *in.Watermark != "" && *in.Watermark != "auto" && *in.Watermark != "off" {
		return nil, apperr.BadRequest("watermark: auto, off or empty (as above)")
	}
	if in.Downloads != nil && *in.Downloads != "" && *in.Downloads != "auto" && *in.Downloads != "allow" {
		return nil, apperr.BadRequest("downloads: auto, allow or empty (as above)")
	}
	before, err := s.loadCategory(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	titles, descs, tags, slug, pos, def, feed := before.Titles, before.Descriptions, before.Tags, before.Slug, before.Position, before.DefaultElementID, before.Feed
	if in.Feed != nil {
		switch *in.Feed {
		case "off", "on", "recursive":
			feed = *in.Feed
		default:
			return nil, apperr.BadRequest("feed must be off, on or recursive")
		}
	}
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
			// acl: internal – checks the element belongs to the category; showing it checks the element
			if err := s.pool.QueryRow(ctx, `SELECT category_id FROM elements WHERE id=$1 AND deleted_at IS NULL`, *in.DefaultElementID).Scan(&cat); err != nil || cat != id {
				return nil, apperr.BadRequest("default element must be an element of this category")
			}
			def = in.DefaultElementID
		}
	}
	orig := before.OriginalLang
	if orig == "" {
		orig = s.originalOf(before.Titles, "")
	}
	if in.OriginalLang != nil && *in.OriginalLang != orig {
		if titles[*in.OriginalLang] == "" {
			return nil, apperr.BadRequest("the original language needs a title")
		}
		orig = *in.OriginalLang
	}
	if titles[orig] == "" {
		return nil, apperr.BadRequest("the original language (%s) needs a title", orig)
	}
	rev, revs := nextRevs(orig, before.TextRev, before.TranslationRevs, before.Titles, before.Descriptions, titles, descs)
	if orig != before.OriginalLang && before.OriginalLang != "" {
		// a new original: the other languages count as based on it
		rev++
		revs = map[string]int{}
		for l, v := range titles {
			if l != orig && v != "" {
				revs[l] = rev
			}
		}
	}
	classChanged := false
	var noted int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE categories SET titles=$2, descriptions=$3, tags=$4, slug=$5, position=$6, default_element_id=$7, feed=$8,
			original_lang=$9, text_rev=$10, translation_revs=$11, updated_at=now()
			WHERE id=$1`, id, titles, descs, tags, slug, pos, def, feed, orig, rev, revs); err != nil {
			return err
		}
		if in.ShortURL != nil {
			short, err := NormalizeShortURL(*in.ShortURL)
			if err != nil {
				return err
			}
			if err := s.setShortURL(ctx, tx, "category", id, short); err != nil {
				return err
			}
		}
		if in.Hidden != nil {
			if _, err := tx.Exec(ctx, `UPDATE categories SET hidden=$2 WHERE id=$1`, id, *in.Hidden); err != nil {
				return err
			}
		}
		for _, x := range []struct {
			col string
			in  *string
			old *string
		}{{"watermark", in.Watermark, before.Watermark}, {"downloads", in.Downloads, before.Downloads}} {
			if x.in == nil {
				continue
			}
			var v *string
			if *x.in != "" {
				v = x.in
			}
			if (v == nil) == (x.old == nil) && (v == nil || *v == *x.old) {
				continue
			}
			if _, err := tx.Exec(ctx, `UPDATE categories SET `+x.col+`=$2 WHERE id=$1`, id, v); err != nil {
				return err
			}
			if err := audit.Log(ctx, tx, "category."+x.col, "category", id, map[string]any{x.col: x.old}, map[string]any{x.col: v}); err != nil {
				return err
			}
			classChanged = true
		}
		if in.Classification != nil {
			var v *int
			if *in.Classification >= 0 {
				v = in.Classification
			}
			if !intPtrEqual(v, before.Classification) {
				if _, err := tx.Exec(ctx, `UPDATE categories SET classification=$2 WHERE id=$1`, id, v); err != nil {
					return err
				}
				if err := audit.Log(ctx, tx, "category.classification", "category", id, map[string]any{"classification": before.Classification}, map[string]any{"classification": v}); err != nil {
					return err
				}
				classChanged = true
				// external search indexes take the elements again (or drop them)
				// acl: internal – queue for the search index
				if _, err := tx.Exec(ctx, `INSERT INTO search_changes(element_id) SELECT e.id FROM elements e JOIN categories c ON c.id = e.category_id
					WHERE c.path <@ (SELECT path FROM categories WHERE id=$1)`, id); err != nil {
					return err
				}
			}
		}
		if in.Template != nil && *in.Template != before.Template {
			if _, err := tx.Exec(ctx, `UPDATE categories SET template=$2 WHERE id=$1`, id, *in.Template); err != nil {
				return err
			}
			if err := audit.Log(ctx, tx, "category.template", "category", id, map[string]any{"template": before.Template}, map[string]any{"template": *in.Template}); err != nil {
				return err
			}
		}
		if in.EditorTools != nil {
			var v []string // nil: inherited
			if len(*in.EditorTools) > 0 {
				v = *in.EditorTools
			}
			if !slices.Equal(v, before.EditorTools) || (v == nil) != (before.EditorTools == nil) {
				if _, err := tx.Exec(ctx, `UPDATE categories SET editor_tools=$2 WHERE id=$1`, id, v); err != nil {
					return err
				}
				if err := audit.Log(ctx, tx, "category.editor_tools", "category", id, map[string]any{"editor_tools": before.EditorTools}, map[string]any{"editor_tools": v}); err != nil {
					return err
				}
			}
		}
		if in.ShortOnly != nil {
			if _, err := tx.Exec(ctx, `UPDATE categories SET short_only=$2 WHERE id=$1`, id, *in.ShortOnly); err != nil {
				return err
			}
		}
		if err := audit.Log(ctx, tx, "category.update", "category", id,
			snapshotOf(before.Titles, before.Descriptions, before.Tags, before.Slug, before.ParentID, before.Position, before.DefaultElementID),
			snapshotOf(titles, descs, tags, slug, before.ParentID, pos, def)); err != nil {
			return err
		}
		var err error
		noted, err = s.note(ctx, tx, "update", "category", id)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.fireNoted(ctx, noted, "update", "category", id)
	if classChanged {
		s.aclChanged(ctx)
	}
	return s.GetCategory(ctx, id)
}

// MoveCategory moves a category (with its subtree) below newParent (0 = root).
func (s *Service) MoveCategory(ctx context.Context, id, newParent int64, position *int) (*Category, error) {
	if err := s.require(ctx, id, acl.Manage); err != nil {
		return nil, err
	}
	if newParent == 0 {
		if err := perm.Require(ctx, "core.categories.admin"); err != nil {
			return nil, err
		}
	} else if err := s.require(ctx, newParent, acl.Manage); err != nil {
		return nil, err
	}
	if err := s.notArchived(ctx, id); err != nil {
		return nil, err
	}
	if err := s.notArchived(ctx, newParent); err != nil {
		return nil, err
	}
	var fromParent int64
	_ = s.pool.QueryRow(ctx, `SELECT COALESCE(parent_id, 0) FROM categories WHERE id=$1`, id).Scan(&fromParent)
	var noted int64
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.andNote(ctx, tx, &noted, "move", "category", id, s.moveCategoryTx(ctx, tx, id, newParent, position))
	})
	if err != nil {
		return nil, err
	}
	s.aclChanged(ctx)
	_ = s.syncSlug(ctx, "category", id, fromParent)
	s.fireNoted(ctx, noted, "move", "category", id)
	return s.GetCategory(ctx, id)
}

// moveCategoryTx moves a category inside a transaction (the checks of the
// caller apply) and writes the audit entry.
func (s *Service) moveCategoryTx(ctx context.Context, tx pgx.Tx, id, newParent int64, position *int) error {
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
		// the space it moves into may limit the element types
		if err := typesAllowedIn(ctx, tx, oldPath, newPrefix); err != nil {
			return err
		}
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
	// the classification may change with the new place: external indexes take the elements again
	// acl: internal – queue for the search index
	if _, err := tx.Exec(ctx, `INSERT INTO search_changes(element_id) SELECT e.id FROM elements e JOIN categories c ON c.id = e.category_id
			WHERE c.path <@ (SELECT path FROM categories WHERE id=$1)`, id); err != nil {
		return err
	}
	// a space that becomes a subcategory gives up its key (/s/KEY, free for
	// others); its other space settings and page templates rest and apply
	// again when it becomes a top-level category (a space) again
	var key *string
	if oldParent == nil && parentPtr != nil {
		if err := tx.QueryRow(ctx, `UPDATE categories c SET space_key = NULL FROM categories o WHERE c.id=$1 AND o.id=c.id RETURNING o.space_key`, id).Scan(&key); err != nil {
			return err
		}
	}
	return audit.Log(ctx, tx, "category.move", "category", id, map[string]any{"parent_id": oldParent, "position": oldPos, "space_key": key}, map[string]any{"parent_id": parentPtr, "position": pos})
}

// typesAllowedIn checks the elements below subtree against the element
// types allowed in the space of the category with path target.
func typesAllowedIn(ctx context.Context, q db.Querier, subtree, target string) error {
	var bad string
	// acl: internal – type check of the whole subtree
	if err := q.QueryRow(ctx, `SELECT COALESCE((SELECT e.type FROM elements e JOIN categories c ON c.id = e.category_id
		WHERE c.path <@ $1::ltree AND e.deleted_at IS NULL AND jsonb_array_length(COALESCE(t.space->'allowed_types', '[]')) > 0
			AND NOT (t.space->'allowed_types') ? e.type LIMIT 1), '')
		FROM categories t WHERE t.id = split_part($2, '.', 1)::bigint`, subtree, target).Scan(&bad); err != nil {
		return err
	}
	if bad != "" {
		return apperr.BadRequest("element type %q is not allowed in the target space", bad)
	}
	return nil
}

// Reorder sets the order of the given sibling categories.
func (s *Service) Reorder(ctx context.Context, parent int64, ids []int64) error {
	if parent == 0 {
		if err := perm.Require(ctx, "core.categories.admin"); err != nil {
			return err
		}
	} else if err := s.require(ctx, parent, acl.Manage); err != nil {
		return err
	}
	return db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		before, err := siblingOrder(ctx, tx, parent)
		if err != nil {
			return err
		}
		if err := setOrder(ctx, tx, parent, ids); err != nil {
			return err
		}
		// the order before, so a rollback can restore it
		return audit.Log(ctx, tx, "category.reorder", "category", parent, map[string]any{"order": before}, map[string]any{"order": ids})
	})
}

// siblingOrder lists the categories below parent (0: top level) in their order.
func siblingOrder(ctx context.Context, tx pgx.Tx, parent int64) ([]int64, error) {
	var ids []int64
	err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id ORDER BY position, id), '{}') FROM categories
		WHERE parent_id IS NOT DISTINCT FROM $1 AND deleted_at IS NULL`, nilIfZero(parent)).Scan(&ids)
	return ids, err
}

// setOrder gives the listed siblings of parent the positions of their order
// (one statement; ids below another parent are left alone).
func setOrder(ctx context.Context, tx pgx.Tx, parent int64, ids []int64) error {
	_, err := tx.Exec(ctx, `UPDATE categories c SET position = o.n - 1 FROM unnest($1::bigint[]) WITH ORDINALITY AS o(id, n)
		WHERE c.id = o.id AND c.parent_id IS NOT DISTINCT FROM $2`, ids, nilIfZero(parent))
	return err
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
	return s.duplicateCategory(ctx, id, targetParent, true)
}

func (s *Service) duplicateCategory(ctx context.Context, id, targetParent int64, withElements bool) (*Category, error) {
	if err := s.require(ctx, id, acl.View); err != nil {
		return nil, err
	}
	if s.acl.Classification(ctx, id) >= acl.StrictConfidential {
		return nil, apperr.Forbidden("strictly confidential content is not copied")
	}
	if targetParent == 0 {
		if err := perm.Require(ctx, "core.categories.admin"); err != nil {
			return nil, err
		}
	} else if err := s.require(ctx, targetParent, acl.Manage); err != nil {
		return nil, err
	} else if err := s.notArchived(ctx, targetParent); err != nil {
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
			if withElements {
				if err := typesAllowedIn(ctx, tx, pathString(src.Path), targetPath); err != nil {
					return err
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
			// the copy keeps the protection of the original (classification,
			// watermark, downloads) and its display settings
			if _, err := tx.Exec(ctx, `INSERT INTO categories(id, parent_id, path, position, slug, titles, descriptions, tags, created_by, original_lang, text_rev, translation_revs, template,
				classification, watermark, downloads, editor_tools)
				VALUES ($1,$2,$3::ltree,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, nid, parent, path, pos, c.Slug, titles, c.Descriptions, c.Tags, nullID(ctx),
				c.OriginalLang, c.TextRev, c.TranslationRevs, c.Template, c.Classification, c.Watermark, c.Downloads, c.EditorTools); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO acl_rules(category_id, subject_type, subject_id, permissions, inherit, created_by)
				SELECT $2, subject_type, subject_id, permissions, inherit, $3 FROM acl_rules WHERE category_id=$1`, c.ID, nid, nullID(ctx)); err != nil {
				return err
			}
			mapping[c.ID], paths[nid] = nid, path
			if withElements {
				if err := s.copyElements(ctx, tx, c.ID, nid, p); err != nil {
					return err
				}
			}
		}
		newRoot = mapping[src.ID]
		if withElements {
			// F-02: references to categories of the copied tree (blog, FAQ,
			// sources …) point to the copies
			if err := remapCategoryRefs(ctx, tx, mapping); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, "category.duplicate", "category", newRoot, map[string]any{"source": id}, map[string]any{"categories": len(mapping)})
	})
	if err != nil {
		return nil, err
	}
	s.aclChanged(ctx)
	s.bus.Publish(ctx, "i18n", "") // the labels of the copied elements
	return s.GetCategory(ctx, newRoot)
}

// remapCategoryRefs rewrites, in the copied elements, references to
// categories of the copied tree: "category_id" values in JSON bodies and
// "category:N" in source texts.
func remapCategoryRefs(ctx context.Context, tx pgx.Tx, mapping map[int64]int64) error {
	newCats := make([]int64, 0, len(mapping))
	for _, n := range mapping {
		newCats = append(newCats, n)
	}
	// acl: internal – references of the new copies
	rows, err := tx.Query(ctx, `SELECT r.element_id, r.lang, r.rev, r.body FROM element_revisions r JOIN elements e ON e.id = r.element_id
		WHERE e.category_id = ANY($1)`, newCats)
	if err != nil {
		return err
	}
	type rev struct {
		id   int64
		lang string
		rev  int
		body []byte
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (rev, error) {
		var x rev
		return x, r.Scan(&x.id, &x.lang, &x.rev, &x.body)
	})
	if err != nil {
		return err
	}
	for _, x := range list {
		var v any
		if json.Unmarshal(x.body, &v) != nil {
			continue
		}
		changed := false
		v = remapRefs(v, mapping, &changed)
		if !changed {
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE element_revisions SET body=$4 WHERE element_id=$1 AND lang=$2 AND rev=$3`, x.id, x.lang, x.rev, b); err != nil {
			return err
		}
	}
	return nil
}

var catRefText = regexp.MustCompile(`(^|[;\s+\-"'=])category:(\d+)`)

func remapRefs(v any, mapping map[int64]int64, changed *bool) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if f, ok := e.(float64); ok && k == "category_id" {
				if n, ok := mapping[int64(f)]; ok {
					x[k] = n
					*changed = true
				}
				continue
			}
			x[k] = remapRefs(e, mapping, changed)
		}
	case []any:
		for i, e := range x {
			x[i] = remapRefs(e, mapping, changed)
		}
	case string:
		if strings.Contains(x, "category:") {
			out := catRefText.ReplaceAllStringFunc(x, func(m string) string {
				sub := catRefText.FindStringSubmatch(m)
				id, _ := strconv.ParseInt(sub[2], 10, 64)
				if n, ok := mapping[id]; ok {
					*changed = true
					return sub[1] + "category:" + strconv.FormatInt(n, 10)
				}
				return m
			})
			return out
		}
	}
	return v
}

// copyElements copies the non-deleted elements of category from into to
// that p may view, with their own rights (so that a copy is protected as
// its original); strictly confidential elements are not copied.
func (s *Service) copyElements(ctx context.Context, tx pgx.Tx, from, to int64, p *identity.Principal) error {
	// acl: checked per element (ElementPerms) below
	type item struct {
		id  int64
		typ string
	}
	rows, err := tx.Query(ctx, `SELECT id, type FROM elements WHERE category_id=$1 AND deleted_at IS NULL ORDER BY position, id`, from)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (item, error) {
		var x item
		return x, r.Scan(&x.id, &x.typ)
	})
	if err != nil {
		return err
	}
	for _, it := range items {
		eid := it.id
		if !s.acl.CanElement(ctx, p, eid, from, acl.View) || s.acl.ElementClassification(ctx, eid, from) >= acl.StrictConfidential {
			continue
		}
		// what the person may not create (the element type says) is not
		// copied, like what they may not see. Checked in the category of
		// the original: the copy keeps its rules, and the rights in the new
		// category are not known before the copy is committed.
		if err := s.guardCopy(ctx, tx, eid, it.typ, from); err != nil {
			if st, _, _ := apperr.Status(err); st == http.StatusForbidden {
				continue
			}
			return err
		}
		if _, err := copyElement(ctx, tx, eid, to, "", true); err != nil {
			return err
		}
	}
	return nil
}

// copyElement copies the latest revision of every language of element eid
// (with its classification and hidden flag; with its own rules and "own
// rights only" if withRights).
func copyElement(ctx context.Context, tx pgx.Tx, eid, to int64, titleSuffix string, withRights bool) (int64, error) {
	var nid int64
	uid := nullID(ctx)
	// acl: internal – copy; callers check the element
	if err := tx.QueryRow(ctx, `INSERT INTO elements(category_id, type, position, original_lang, titles, tags, created_by, updated_by, hidden, classification, own_rights_only)
		SELECT $2, type, position, original_lang, titles, tags, $3, $3, hidden, classification, own_rights_only AND $4 FROM elements WHERE id=$1 RETURNING id`, eid, to, uid, withRights).Scan(&nid); err != nil {
		return 0, err
	}
	if withRights {
		if _, err := tx.Exec(ctx, `INSERT INTO element_rules(element_id, subject_type, subject_id, permissions, actions, created_by)
			SELECT $2, subject_type, subject_id, permissions, actions, $3 FROM element_rules WHERE element_id=$1 ORDER BY id`, eid, nid, uid); err != nil {
			return 0, err
		}
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
	// acl: internal – copy; callers check the element
	if _, err := tx.Exec(ctx, `INSERT INTO search_docs(element_id, lang, category_id, title, body_text, tags, tsv)
		SELECT $2, d.lang, $3, d.title || $4::text, d.body_text, d.tags, d.tsv FROM search_docs d WHERE d.element_id=$1`, eid, nid, to, titleSuffix); err != nil {
		return 0, err
	}
	// the display of the page: its arrangement and its own labels
	// ("<key>@<plugin>/<id>"; callers publish "i18n" afterwards)
	if _, err := tx.Exec(ctx, `INSERT INTO placements(level, target, block, position, sort, required, updated_by)
		SELECT level, $2::bigint::text, block, position, sort, false, $3 FROM placements WHERE level='instance' AND target=$1::bigint::text ORDER BY id`, eid, nid, uid); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ui_translations(lang, key, value, source_hash, updated_by)
		SELECT lang, left(key, length(key) - length($1::bigint::text)) || $2::bigint::text, value, source_hash, $3 FROM ui_translations
		WHERE key LIKE '%@%/%' AND substring(key from '@[a-z0-9._-]+/([0-9]{1,19})$') = $1::bigint::text`, eid, nid, uid); err != nil {
		return 0, err
	}
	return nid, nil
}

// DeleteCategory moves a category, its subcategories and their elements to the trash.
func (s *Service) DeleteCategory(ctx context.Context, id int64) error {
	if err := s.require(ctx, id, acl.Manage); err != nil {
		return err
	}
	c, err := s.loadCategory(ctx, s.pool, id, false)
	if err != nil {
		return err
	}
	if c.ParentID == nil {
		if err := perm.Require(ctx, "core.categories.admin"); err != nil {
			return err
		}
	}
	var noted int64
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
		return s.andNote(ctx, tx, &noted, "delete", "category", id, audit.Log(ctx, tx, "category.trash", "category", id, map[string]any{"titles": c.Titles}, nil))
	})
	if err != nil {
		return err
	}
	s.aclChanged(ctx)
	s.fireNoted(ctx, noted, "delete", "category", id)
	return nil
}

// TreeNode is a light category entry for pickers.
type TreeNode struct {
	ID       int64             `json:"id"`
	ParentID *int64            `json:"parent_id"`
	Depth    int               `json:"depth"`
	Titles   map[string]string `json:"titles"`
	Perms    []string          `json:"permissions"`
	Hidden   bool              `json:"hidden"`
}

// Tree returns all visible categories in tree order (depth first), at most limit.
func (s *Service) Tree(ctx context.Context, limit int) ([]TreeNode, error) {
	if limit <= 0 || limit > 20000 {
		limit = 5000
	}
	rows, err := s.pool.Query(ctx, `SELECT id, parent_id, nlevel(path), titles, path::text, hidden FROM categories WHERE deleted_at IS NULL`)
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
		err := r.Scan(&x.n.ID, &x.n.ParentID, &x.n.Depth, &x.n.Titles, &p, &x.n.Hidden)
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
			if perms[a]&(acl.View|acl.Manage) == 0 {
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

// requireCategory checks that the caller may see a category: view, or
// manage (category administrators see the structure, not the content).
func (s *Service) requireCategory(ctx context.Context, id int64) error {
	if s.acl.Can(ctx, identity.From(ctx), id, acl.Manage) {
		return nil
	}
	return s.require(ctx, id, acl.View)
}
