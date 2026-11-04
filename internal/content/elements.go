package content

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Element is an element without content.
type Element struct {
	ID           int64             `json:"id"`
	CategoryID   int64             `json:"category_id"`
	Type         string            `json:"type"`
	Position     int               `json:"position"`
	OriginalLang string            `json:"original_lang"`
	Titles       map[string]string `json:"titles"`
	Tags         []string          `json:"tags"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	UpdatedBy    string            `json:"updated_by"`
	DeletedAt    *time.Time        `json:"deleted_at,omitempty"`
	IsDefault    bool              `json:"is_default"`
	ShortURL     string            `json:"short_url"`
	// PreviewImage is shown in link previews (Open Graph); "" = first image of the page.
	PreviewImage string `json:"preview_image"`
	CreatedByID  *int64 `json:"created_by_id"`
}

// LangStatus is the translation state of one language.
type LangStatus struct {
	Lang      string     `json:"lang"`
	Rev       int        `json:"rev"`
	SourceRev *int       `json:"source_rev"`
	Status    string     `json:"status"` // original | current | outdated | missing
	UpdatedAt *time.Time `json:"updated_at"`
}

// ElementView is an element rendered in one language.
type ElementView struct {
	Element
	Lang         string          `json:"lang"`       // language requested
	ShownLang    string          `json:"shown_lang"` // language actually shown
	Fallback     bool            `json:"fallback"`
	Rev          int             `json:"rev"`
	Title        string          `json:"title"`
	Body         json.RawMessage `json:"body,omitempty"` // only for editors
	HTML         string          `json:"html"`
	Translations []LangStatus    `json:"translations"`
	Translated   int             `json:"translated_percent"`
	UpToDate     bool            `json:"up_to_date"`
	Permissions  []string        `json:"permissions"`
	TypeActive   bool            `json:"type_active"`
}

const elemCols = `e.id, e.category_id, e.type, e.position, e.original_lang, e.titles, e.tags, e.created_at, e.updated_at,
	COALESCE((SELECT u.display_name || ' (' || u.username || '@' || u.provider || ')' FROM users u WHERE u.id = e.updated_by), ''),
	e.deleted_at, EXISTS (SELECT 1 FROM categories c WHERE c.id = e.category_id AND c.default_element_id = e.id), COALESCE(e.short_url, ''), e.preview_image, e.created_by`

func scanElement(r pgx.CollectableRow) (Element, error) {
	var e Element
	err := r.Scan(&e.ID, &e.CategoryID, &e.Type, &e.Position, &e.OriginalLang, &e.Titles, &e.Tags, &e.CreatedAt, &e.UpdatedAt,
		&e.UpdatedBy, &e.DeletedAt, &e.IsDefault, &e.ShortURL, &e.PreviewImage, &e.CreatedByID)
	return e, err
}

func (s *Service) loadElement(ctx context.Context, q db.Querier, id int64, includeDeleted bool) (*Element, error) {
	rows, err := q.Query(ctx, `SELECT `+elemCols+` FROM elements e WHERE e.id=$1 AND ($2 OR e.deleted_at IS NULL)`, id, includeDeleted)
	if err != nil {
		return nil, err
	}
	e, err := pgx.CollectExactlyOneRow(rows, scanElement)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("element")
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListElements lists the elements of a category.
func (s *Service) ListElements(ctx context.Context, categoryID int64) ([]Element, error) {
	if err := s.require(ctx, categoryID, acl.View); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+elemCols+` FROM elements e WHERE e.category_id=$1 AND e.deleted_at IS NULL ORDER BY e.position, e.id`, categoryID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanElement)
}

// translations computes the translation state of every configured language.
func (s *Service) translations(ctx context.Context, e *Element) ([]LangStatus, int, bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (lang) lang, rev, source_rev, created_at FROM element_revisions
		WHERE element_id=$1 ORDER BY lang, rev DESC`, e.ID)
	if err != nil {
		return nil, 0, false, err
	}
	latest := map[string]LangStatus{}
	for rows.Next() {
		var l LangStatus
		var at time.Time
		if err := rows.Scan(&l.Lang, &l.Rev, &l.SourceRev, &at); err != nil {
			rows.Close()
			return nil, 0, false, err
		}
		l.UpdatedAt = &at
		latest[l.Lang] = l
	}
	rows.Close()
	orig := latest[e.OriginalLang].Rev
	langs := s.Languages()
	out := make([]LangStatus, 0, len(langs))
	done := 0
	seen := map[string]bool{}
	add := func(lang string) {
		if seen[lang] {
			return
		}
		seen[lang] = true
		l, ok := latest[lang]
		switch {
		case lang == e.OriginalLang:
			l.Status = "original"
		case !ok:
			l = LangStatus{Lang: lang, Status: "missing"}
		case l.SourceRev != nil && *l.SourceRev >= orig:
			l.Status = "current"
		default:
			l.Status = "outdated"
		}
		if l.Status == "original" || l.Status == "current" {
			done++
		}
		out = append(out, l)
	}
	add(e.OriginalLang)
	for _, l := range langs {
		add(l)
	}
	pct := 100
	if len(out) > 0 {
		pct = done * 100 / len(out)
	}
	return out, pct, done == len(out), rows.Err()
}

// GetElement returns an element rendered in lang. If the language has no
// translation, the original language is shown (fallback).
func (s *Service) GetElement(ctx context.Context, id int64, lang string, withBody bool) (*ElementView, error) {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, e.CategoryID, acl.View); err != nil {
		if errors.Is(err, apperr.ErrForbidden) {
			return nil, apperr.NotFound("element")
		}
		return nil, err
	}
	if lang == "" || !s.validLang(lang) {
		lang = s.DefaultLanguage()
	}
	v := &ElementView{Element: *e, Lang: lang}
	perms, _ := s.acl.Perms(ctx, identity.From(ctx), e.CategoryID)
	v.Permissions = perms.Strings()
	var body json.RawMessage
	err = s.pool.QueryRow(ctx, `SELECT lang, rev, title, body FROM element_revisions WHERE element_id=$1 AND lang = ANY($2)
		ORDER BY (lang=$3) DESC, (lang=$4) DESC, rev DESC LIMIT 1`, id, []string{lang, e.OriginalLang}, lang, e.OriginalLang).
		Scan(&v.ShownLang, &v.Rev, &v.Title, &body)
	if err != nil {
		return nil, err
	}
	v.Fallback = v.ShownLang != lang
	if v.Translations, v.Translated, v.UpToDate, err = s.translations(ctx, e); err != nil {
		return nil, err
	}
	t := s.Type(e.Type)
	v.TypeActive = t != nil
	if t != nil {
		html, err := t.Render(ctx, RenderContext{ElementID: id, CategoryID: e.CategoryID, Lang: v.ShownLang, Principal: identity.From(ctx), Service: s}, body)
		if err != nil {
			s.log.Error("render failed", "element", id, "err", err)
			html = `<p role="alert">This element could not be rendered.</p>`
		}
		v.HTML = html
	} else {
		v.HTML = `<p role="note">The element type "` + htmlEscape(e.Type) + `" is not available (plugin disabled or not installed).</p>`
	}
	if withBody && perms&acl.Edit != 0 {
		v.Body = body
	}
	return v, nil
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(s)
}

// ElementInput creates an element.
type ElementInput struct {
	CategoryID int64           `json:"category_id"`
	Type       string          `json:"type"`
	Lang       string          `json:"lang"`
	Title      string          `json:"title"`
	Body       json.RawMessage `json:"body"`
	Tags       []string        `json:"tags"`
	Position   *int            `json:"position"`
	Comment    string          `json:"comment"`
	ShortURL   string          `json:"short_url"`
}

func cleanTitle(t string) (string, error) {
	t = strings.TrimSpace(t)
	if t == "" {
		return "", apperr.BadRequest("title is required")
	}
	if utf8.RuneCountInString(t) > 250 {
		return "", apperr.BadRequest("title too long (max 250 characters)")
	}
	return t, nil
}

// CreateElement creates a new element with revision 1 in in.Lang.
func (s *Service) CreateElement(ctx context.Context, in ElementInput) (*ElementView, error) {
	if err := s.require(ctx, in.CategoryID, acl.Create); err != nil {
		return nil, err
	}
	t := s.Type(in.Type)
	if t == nil {
		return nil, apperr.BadRequest("unknown or disabled element type %q", in.Type)
	}
	if in.Lang == "" {
		in.Lang = s.DefaultLanguage()
	}
	if !s.validLang(in.Lang) {
		return nil, apperr.BadRequest("language %q is not enabled", in.Lang)
	}
	title, err := cleanTitle(in.Title)
	if err != nil {
		return nil, err
	}
	if len(in.Body) == 0 {
		in.Body = t.EmptyBody()
	}
	if g, ok := t.(Guard); ok {
		if err := g.CheckCreate(ctx, s, in.CategoryID); err != nil {
			return nil, err
		}
	}
	if p, ok := t.(Preparer); ok {
		if in.Body, err = p.Prepare(ctx, s, in.CategoryID, in.Lang, title, in.Body); err != nil {
			return nil, err
		}
	}
	body, err := t.Normalize(in.Body)
	if err != nil {
		return nil, apperr.BadRequest("invalid body: %v", err)
	}
	tags, err := NormalizeTags(in.Tags)
	if err != nil {
		return nil, err
	}
	short, err := NormalizeShortURL(in.ShortURL)
	if err != nil {
		return nil, err
	}
	var id int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var catPath string
		if err := tx.QueryRow(ctx, `SELECT path::text FROM categories WHERE id=$1 AND deleted_at IS NULL`, in.CategoryID).Scan(&catPath); err != nil {
			return apperr.NotFound("category")
		}
		pos := 0
		if in.Position != nil {
			pos = *in.Position
		} else if err := tx.QueryRow(ctx, `SELECT COALESCE(max(position)+1,0) FROM elements WHERE category_id=$1`, in.CategoryID).Scan(&pos); err != nil {
			return err
		}
		uid := nullID(ctx)
		if err := tx.QueryRow(ctx, `INSERT INTO elements(category_id, type, position, original_lang, titles, tags, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$7) RETURNING id`, in.CategoryID, in.Type, pos, in.Lang, map[string]string{in.Lang: title}, tags, uid).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO element_revisions(element_id, lang, rev, title, body, comment, created_by) VALUES ($1,$2,1,$3,$4,$5,$6)`,
			id, in.Lang, title, body, in.Comment, uid); err != nil {
			return err
		}
		if err := s.index(ctx, tx, id, in.CategoryID, in.Lang, title, t.Text(body), tags); err != nil {
			return err
		}
		if short != "" {
			if err := s.setShortURL(ctx, tx, "element", id, short); err != nil {
				return err
			}
		}
		// Make the first element the category default.
		if _, err := tx.Exec(ctx, `UPDATE categories SET default_element_id=$2 WHERE id=$1 AND default_element_id IS NULL`, in.CategoryID, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "element.create", "element", id, nil, map[string]any{"category_id": in.CategoryID, "type": in.Type, "lang": in.Lang, "title": title})
	})
	if err != nil {
		return nil, err
	}
	s.fire(ctx, "create", "element", id)
	return s.GetElement(ctx, id, in.Lang, true)
}

// tsvExpr builds the search vector from parameters $4 (title), $6 (tags) and $5 (body).
const tsvExpr = `setweight(to_tsvector('simple', $4::text), 'A') || setweight(to_tsvector('simple', array_to_string($6::text[], ' ')), 'B') || setweight(to_tsvector('simple', $5::text), 'C')`

func (s *Service) index(ctx context.Context, q db.Querier, id, cat int64, lang, title, text string, tags []string) error {
	if len(text) > 1<<20 {
		text = text[:1<<20]
	}
	_, err := q.Exec(ctx, `INSERT INTO search_docs(element_id, lang, category_id, title, body_text, tags, tsv) VALUES ($1,$2,$3,$4,$5,$6,`+tsvExpr+`)
		ON CONFLICT (element_id, lang) DO UPDATE SET category_id=EXCLUDED.category_id, title=EXCLUDED.title, body_text=EXCLUDED.body_text,
			tags=EXCLUDED.tags, tsv=EXCLUDED.tsv`,
		id, lang, cat, title, text, tags)
	return err
}

// SaveInput saves a new revision of one language.
type SaveInput struct {
	Lang    string          `json:"lang"`
	Title   string          `json:"title"`
	Body    json.RawMessage `json:"body"`
	Comment string          `json:"comment"`
	// BaseRev is the revision the editor started from (0 for a new
	// translation). If another revision was saved meanwhile, a conflict is returned.
	BaseRev *int `json:"base_rev"`
	// SourceRev marks which revision of the original language a translation is
	// based on; defaults to the current original revision.
	SourceRev *int `json:"source_rev"`
}

// SaveElement stores a new revision. Nothing is ever overwritten.
func (s *Service) SaveElement(ctx context.Context, id int64, in SaveInput) (*ElementView, error) {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, e.CategoryID, acl.Edit); err != nil {
		return nil, err
	}
	t := s.Type(e.Type)
	if t == nil {
		return nil, apperr.BadRequest("element type %q is disabled", e.Type)
	}
	if in.Lang == "" {
		in.Lang = e.OriginalLang
	}
	if !s.validLang(in.Lang) {
		return nil, apperr.BadRequest("language %q is not enabled", in.Lang)
	}
	title, err := cleanTitle(in.Title)
	if err != nil {
		return nil, err
	}
	if p, ok := t.(Preparer); ok {
		if in.Body, err = p.Prepare(ctx, s, e.CategoryID, in.Lang, title, in.Body); err != nil {
			return nil, err
		}
	}
	body, err := t.Normalize(in.Body)
	if err != nil {
		return nil, apperr.BadRequest("invalid body: %v", err)
	}
	if g, ok := t.(Guard); ok {
		var old json.RawMessage
		_ = s.pool.QueryRow(ctx, `SELECT body FROM element_revisions WHERE element_id=$1 AND lang=$2 ORDER BY rev DESC LIMIT 1`, id, in.Lang).Scan(&old)
		if err := g.CheckChange(ctx, s, e, old, body); err != nil {
			return nil, err
		}
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM elements WHERE id=$1 FOR UPDATE`, id); err != nil {
			return err
		}
		var cur, origRev int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(rev),0) FROM element_revisions WHERE element_id=$1 AND lang=$2`, id, in.Lang).Scan(&cur); err != nil {
			return err
		}
		if in.BaseRev != nil && *in.BaseRev != cur {
			return apperr.Conflict("this element was changed by someone else (revision %d); reload and merge your changes", cur)
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(rev),0) FROM element_revisions WHERE element_id=$1 AND lang=$2`, id, e.OriginalLang).Scan(&origRev); err != nil {
			return err
		}
		var src *int
		if in.Lang != e.OriginalLang {
			v := origRev
			if in.SourceRev != nil && *in.SourceRev >= 1 && *in.SourceRev <= origRev {
				v = *in.SourceRev
			}
			src = &v
		}
		uid := nullID(ctx)
		if _, err := tx.Exec(ctx, `INSERT INTO element_revisions(element_id, lang, rev, source_rev, title, body, comment, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, id, in.Lang, cur+1, src, title, body, in.Comment, uid); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE elements SET titles = titles || jsonb_build_object($2::text, $3::text), updated_at=now(), updated_by=$4 WHERE id=$1`,
			id, in.Lang, title, uid); err != nil {
			return err
		}
		if err := s.index(ctx, tx, id, e.CategoryID, in.Lang, title, t.Text(body), e.Tags); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "element.save", "element", id, map[string]any{"lang": in.Lang, "rev": cur}, map[string]any{"lang": in.Lang, "rev": cur + 1, "title": title})
	})
	if err != nil {
		return nil, err
	}
	s.fire(ctx, "update", "element", id)
	return s.GetElement(ctx, id, in.Lang, true)
}

// MetaInput changes element metadata.
type MetaInput struct {
	Tags     []string `json:"tags"`
	Position *int     `json:"position"`
	ShortURL *string  `json:"short_url"` // "" removes it
	// PreviewImage: image for link previews (path like /api/v1/files/… or https URL; "" = automatic)
	PreviewImage *string `json:"preview_image"`
}

// UpdateElementMeta changes tags and position.
func (s *Service) UpdateElementMeta(ctx context.Context, id int64, in MetaInput) (*Element, error) {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, e.CategoryID, acl.Edit); err != nil {
		return nil, err
	}
	tags, pos, preview := e.Tags, e.Position, e.PreviewImage
	if in.PreviewImage != nil {
		if preview, err = NormalizeImageURL(*in.PreviewImage); err != nil {
			return nil, err
		}
	}
	var short *string
	if in.ShortURL != nil {
		v, err := NormalizeShortURL(*in.ShortURL)
		if err != nil {
			return nil, err
		}
		short = &v
	}
	if in.Tags != nil {
		if tags, err = NormalizeTags(in.Tags); err != nil {
			return nil, err
		}
	}
	if in.Position != nil {
		pos = *in.Position
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE elements SET tags=$2, position=$3, preview_image=$5, updated_at=now(), updated_by=$4 WHERE id=$1`, id, tags, pos, nullID(ctx), preview); err != nil {
			return err
		}
		if short != nil {
			if err := s.setShortURL(ctx, tx, "element", id, *short); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE search_docs SET tags=$2, tsv=setweight(to_tsvector('simple', title), 'A') ||
			setweight(to_tsvector('simple', array_to_string($2::text[], ' ')), 'B') || setweight(to_tsvector('simple', body_text), 'C')
			WHERE element_id=$1`, id, tags); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "element.meta", "element", id, map[string]any{"tags": e.Tags, "position": e.Position, "preview_image": e.PreviewImage},
			map[string]any{"tags": tags, "position": pos, "preview_image": preview})
	})
	if err != nil {
		return nil, err
	}
	return s.loadElement(ctx, s.pool, id, false)
}

// MoveElement moves an element to another category.
func (s *Service) MoveElement(ctx context.Context, id, categoryID int64) (*Element, error) {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, e.CategoryID, acl.Delete); err != nil {
		return nil, err
	}
	if err := s.require(ctx, categoryID, acl.Create); err != nil {
		return nil, err
	}
	if err := s.guardChange(ctx, e); err != nil {
		return nil, err
	}
	if err := s.guardCreate(ctx, e.Type, categoryID); err != nil {
		return nil, err
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM categories WHERE id=$1 AND deleted_at IS NULL)`, categoryID).Scan(&ok); err != nil || !ok {
			return apperr.NotFound("target category")
		}
		if _, err := tx.Exec(ctx, `UPDATE elements SET category_id=$2, position=(SELECT COALESCE(max(position)+1,0) FROM elements WHERE category_id=$2),
			updated_at=now() WHERE id=$1`, id, categoryID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE categories SET default_element_id=NULL WHERE default_element_id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE search_docs SET category_id=$2 WHERE element_id=$1`, id, categoryID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "element.move", "element", id, map[string]any{"category_id": e.CategoryID}, map[string]any{"category_id": categoryID})
	})
	if err != nil {
		return nil, err
	}
	s.fire(ctx, "move", "element", id)
	return s.loadElement(ctx, s.pool, id, false)
}

// DuplicateElement copies an element (latest revision of each language).
func (s *Service) DuplicateElement(ctx context.Context, id, categoryID int64) (*Element, error) {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, e.CategoryID, acl.View); err != nil {
		return nil, err
	}
	if categoryID == 0 {
		categoryID = e.CategoryID
	}
	if err := s.require(ctx, categoryID, acl.Create); err != nil {
		return nil, err
	}
	if err := s.guardCreate(ctx, e.Type, categoryID); err != nil {
		return nil, err
	}
	var nid int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		suffix := ""
		if categoryID == e.CategoryID {
			suffix = " (copy)"
		}
		var err error
		if nid, err = copyElement(ctx, tx, id, categoryID, suffix); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE elements SET position=(SELECT COALESCE(max(position)+1,0) FROM elements WHERE category_id=$2) WHERE id=$1`, nid, categoryID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "element.duplicate", "element", nid, map[string]any{"source": id}, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.loadElement(ctx, s.pool, nid, false)
}

// DeleteElement moves an element to the trash.
func (s *Service) DeleteElement(ctx context.Context, id int64) error {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return err
	}
	if err := s.require(ctx, e.CategoryID, acl.Delete); err != nil {
		return err
	}
	if err := s.guardChange(ctx, e); err != nil {
		return err
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE elements SET deleted_at=now(), deleted_by=$2, trash_root=true WHERE id=$1`, id, nullID(ctx)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE categories SET default_element_id=NULL WHERE default_element_id=$1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "element.trash", "element", id, map[string]any{"titles": e.Titles}, nil)
	})
	if err == nil {
		s.fire(ctx, "delete", "element", id)
	}
	return err
}

// Revision is an entry of the element history.
type Revision struct {
	Lang         string          `json:"lang"`
	Rev          int             `json:"rev"`
	SourceRev    *int            `json:"source_rev"`
	Title        string          `json:"title"`
	Comment      string          `json:"comment"`
	RestoredFrom *int            `json:"restored_from"`
	Author       string          `json:"author"`
	CreatedAt    time.Time       `json:"created_at"`
	Body         json.RawMessage `json:"body,omitempty"`
	HTML         string          `json:"html,omitempty"`
}

// Revisions lists the history of an element (all languages if lang is empty).
func (s *Service) Revisions(ctx context.Context, id int64, lang string) ([]Revision, error) {
	e, err := s.loadElement(ctx, s.pool, id, true)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, e.CategoryID, acl.View); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT r.lang, r.rev, r.source_rev, r.title, r.comment, r.restored_from,
		COALESCE(u.display_name || ' (' || u.username || '@' || u.provider || ')', ''), r.created_at
		FROM element_revisions r LEFT JOIN users u ON u.id = r.created_by
		WHERE r.element_id=$1 AND ($2 = '' OR r.lang=$2) ORDER BY r.created_at DESC, r.rev DESC LIMIT 1000`, id, lang)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Revision, error) {
		var x Revision
		return x, r.Scan(&x.Lang, &x.Rev, &x.SourceRev, &x.Title, &x.Comment, &x.RestoredFrom, &x.Author, &x.CreatedAt)
	})
}

// GetRevision returns one revision with body and rendered HTML.
func (s *Service) GetRevision(ctx context.Context, id int64, lang string, rev int) (*Revision, error) {
	e, err := s.loadElement(ctx, s.pool, id, true)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, e.CategoryID, acl.View); err != nil {
		return nil, err
	}
	var x Revision
	err = s.pool.QueryRow(ctx, `SELECT r.lang, r.rev, r.source_rev, r.title, r.comment, r.restored_from,
		COALESCE(u.display_name || ' (' || u.username || '@' || u.provider || ')', ''), r.created_at, r.body
		FROM element_revisions r LEFT JOIN users u ON u.id = r.created_by WHERE r.element_id=$1 AND r.lang=$2 AND r.rev=$3`, id, lang, rev).
		Scan(&x.Lang, &x.Rev, &x.SourceRev, &x.Title, &x.Comment, &x.RestoredFrom, &x.Author, &x.CreatedAt, &x.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("revision")
	}
	if err != nil {
		return nil, err
	}
	if t := s.Type(e.Type); t != nil {
		x.HTML, _ = t.Render(ctx, RenderContext{ElementID: id, CategoryID: e.CategoryID, Lang: lang, Principal: identity.From(ctx), Service: s}, x.Body)
	}
	if !s.acl.Can(ctx, identity.From(ctx), e.CategoryID, acl.Edit) {
		x.Body = nil
	}
	return &x, nil
}

// RestoreRevision creates a new revision with the content of an old one.
func (s *Service) RestoreRevision(ctx context.Context, id int64, lang string, rev int) (*ElementView, error) {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, e.CategoryID, acl.Edit); err != nil {
		return nil, err
	}
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := s.restoreRev(ctx, tx, e, lang, rev, "Restored revision")
		return err
	})
	if err != nil {
		return nil, err
	}
	s.fire(ctx, "update", "element", id)
	return s.GetElement(ctx, id, lang, true)
}

// restoreRev copies revision rev of lang to a new latest revision. It returns
// false if rev already is the latest revision.
func (s *Service) restoreRev(ctx context.Context, tx pgx.Tx, e *Element, lang string, rev int, comment string) (bool, error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM elements WHERE id=$1 FOR UPDATE`, e.ID); err != nil {
		return false, err
	}
	var cur int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(rev),0) FROM element_revisions WHERE element_id=$1 AND lang=$2`, e.ID, lang).Scan(&cur); err != nil {
		return false, err
	}
	if cur == rev {
		return false, nil
	}
	var title string
	var body json.RawMessage
	var src *int
	if err := tx.QueryRow(ctx, `SELECT title, body, source_rev FROM element_revisions WHERE element_id=$1 AND lang=$2 AND rev=$3`, e.ID, lang, rev).Scan(&title, &body, &src); err != nil {
		return false, apperr.NotFound("revision")
	}
	uid := nullID(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO element_revisions(element_id, lang, rev, source_rev, title, body, comment, restored_from, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, e.ID, lang, cur+1, src, title, body, comment, rev, uid); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE elements SET titles = titles || jsonb_build_object($2::text, $3::text), updated_at=now(), updated_by=$4 WHERE id=$1`, e.ID, lang, title, uid); err != nil {
		return false, err
	}
	text := ""
	if t := s.Type(e.Type); t != nil {
		text = t.Text(body)
	}
	if err := s.index(ctx, tx, e.ID, e.CategoryID, lang, title, text, e.Tags); err != nil {
		return false, err
	}
	return true, audit.Log(ctx, tx, "element.restore", "element", e.ID, map[string]any{"lang": lang, "rev": cur}, map[string]any{"lang": lang, "rev": cur + 1, "restored_from": rev})
}

// Preview renders a body of an element type without saving (editors only).
func (s *Service) Preview(ctx context.Context, typ, lang string, body json.RawMessage) (string, error) {
	if identity.From(ctx) == nil {
		return "", apperr.ErrUnauthorized
	}
	t := s.Type(typ)
	if t == nil {
		return "", apperr.BadRequest("unknown element type %q", typ)
	}
	norm, err := t.Normalize(body)
	if err != nil {
		return "", apperr.BadRequest("invalid body: %v", err)
	}
	return t.Render(ctx, RenderContext{Lang: lang, Principal: identity.From(ctx), Service: s}, norm)
}

// guardChange asks the element type whether the element may be deleted or
// moved away (Guard).
func (s *Service) guardChange(ctx context.Context, e *Element) error {
	if g, ok := s.Type(e.Type).(Guard); ok {
		return g.CheckChange(ctx, s, e, nil, nil)
	}
	return nil
}

// guardCreate asks the element type whether an element may be created in categoryID.
func (s *Service) guardCreate(ctx context.Context, typ string, categoryID int64) error {
	if g, ok := s.Type(typ).(Guard); ok {
		return g.CheckCreate(ctx, s, categoryID)
	}
	return nil
}
