package content

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
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
	ShortOnly    bool              `json:"short_only"` // only the short URL (no readable paths)
	// PreviewImage is shown in link previews (Open Graph); "" = first image of the page.
	PreviewImage string `json:"preview_image"`
	CreatedByID  *int64 `json:"created_by_id"`
	// OwnerInstance is the instance (a blog …) the element was created
	// through; its instance permissions apply to the element too.
	OwnerInstance *int64 `json:"owner_instance,omitempty"`
	// Hidden: not in listings, sources, feeds and the search (reachable by
	// its link with view permission).
	Hidden bool `json:"hidden"`
	// OwnRightsOnly: only the element's own rules apply, not those of its
	// category (managers of the category by a rule and administrators keep
	// access).
	OwnRightsOnly bool `json:"own_rights_only"`
	// Classification set on the element (0–3); the strictest of the element
	// and its categories applies.
	Classification int `json:"classification"`
	// OwnRules: the element has own permission rules.
	OwnRules bool `json:"own_rules"`
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
	Lang         string            `json:"lang"`       // language requested
	ShownLang    string            `json:"shown_lang"` // language actually shown
	Fallback     bool              `json:"fallback"`
	Rev          int               `json:"rev"`
	Title        string            `json:"title"`
	URL          string            `json:"url"`            // readable path (/wiki/…) in the requested language
	URLs         map[string]string `json:"urls"`           // readable path per language
	Body         json.RawMessage   `json:"body,omitempty"` // only for editors
	HTML         string            `json:"html"`
	Translations []LangStatus      `json:"translations"`
	Translated   int               `json:"translated_percent"`
	UpToDate     bool              `json:"up_to_date"`
	Permissions  []string          `json:"permissions"`
	TypeActive   bool              `json:"type_active"`
}

const elemCols = `e.id, e.category_id, e.type, e.position, e.original_lang, e.titles, e.tags, e.created_at, e.updated_at,
	COALESCE((SELECT u.display_name || ' (' || u.username || '@' || u.provider || ')' FROM users u WHERE u.id = e.updated_by), ''),
	e.deleted_at, EXISTS (SELECT 1 FROM categories c WHERE c.id = e.category_id AND c.default_element_id = e.id), COALESCE(e.short_url, ''), e.preview_image, e.created_by, e.short_only, e.owner_instance, e.hidden,
	e.own_rights_only, e.classification, EXISTS (SELECT 1 FROM element_rules er WHERE er.element_id = e.id)`

func scanElement(r pgx.CollectableRow) (Element, error) {
	var e Element
	err := r.Scan(&e.ID, &e.CategoryID, &e.Type, &e.Position, &e.OriginalLang, &e.Titles, &e.Tags, &e.CreatedAt, &e.UpdatedAt,
		&e.UpdatedBy, &e.DeletedAt, &e.IsDefault, &e.ShortURL, &e.PreviewImage, &e.CreatedByID, &e.ShortOnly, &e.OwnerInstance, &e.Hidden, &e.OwnRightsOnly, &e.Classification, &e.OwnRules)
	return e, err
}

func (s *Service) loadElement(ctx context.Context, q db.Querier, id int64, includeDeleted bool) (*Element, error) {
	// acl: loader without checks; every caller checks with requireElement/elementPerms
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

// loadElements loads (not deleted) elements by id, without checks.
func (s *Service) loadElements(ctx context.Context, ids []int64) (map[int64]*Element, error) {
	out := map[int64]*Element{}
	if len(ids) == 0 {
		return out, nil
	}
	// acl: checked by the callers
	rows, err := s.pool.Query(ctx, `SELECT `+elemCols+` FROM elements e WHERE e.id = ANY($1) AND e.deleted_at IS NULL`, ids)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, scanElement)
	if err != nil {
		return nil, err
	}
	for i := range list {
		out[list[i].ID] = &list[i]
	}
	return out, nil
}

// ListElements lists the elements of a category.
func (s *Service) ListElements(ctx context.Context, categoryID int64) ([]Element, error) {
	if err := s.require(ctx, categoryID, acl.View); err != nil {
		return nil, err
	}
	// hidden elements: only for those who may edit them (marked as hidden)
	p := identity.From(ctx)
	editors := s.acl.Can(ctx, p, categoryID, acl.Edit)
	f, err := s.acl.ElementFilter(ctx, p, acl.View)
	if err != nil {
		return nil, err
	}
	args := []any{categoryID, editors}
	// acl: filtered (ElementFilter)
	rows, err := s.pool.Query(ctx, `SELECT `+elemCols+` FROM elements e WHERE e.category_id=$1 AND e.deleted_at IS NULL AND ($2 OR NOT e.hidden) AND `+f.SQL("e", &args)+` ORDER BY e.position, e.id`, args...)
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
		if l.Status == "current" {
			done++
		}
		out = append(out, l)
	}
	add(e.OriginalLang)
	for _, l := range langs {
		add(l)
	}
	// the original language is not counted: with only the original the
	// element is 0 % translated, with one of two translations 50 %
	pct := 100
	if n := len(out) - 1; n > 0 {
		pct = done * 100 / n
	}
	return out, pct, done == len(out)-1, rows.Err()
}

// GetElement returns an element rendered in lang. If the language has no
// translation, the original language is shown (fallback).
func (s *Service) GetElement(ctx context.Context, id int64, lang string, withBody bool) (*ElementView, error) {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	if err := s.requireElement(ctx, e, acl.View); err != nil {
		if errors.Is(err, apperr.ErrForbidden) {
			return nil, apperr.NotFound("element")
		}
		return nil, err
	}
	if lang == "" || !s.validLang(lang) {
		lang = s.DefaultLanguage()
	}
	v := &ElementView{Element: *e, Lang: lang}
	perms := s.elementPerms(ctx, e)
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
	depth, _ := ctx.Value(renderDepthKey{}).(int)
	ctx = s.withIncludeBudget(ctx)
	switch {
	case t != nil && depth >= MaxRenderDepth:
		// elements shown inside elements (FAQ answers, full blog posts,
		// previews) that again show elements: stop before it loops
		v.HTML = `<p role="note">This content is nested too deeply to be shown here.</p>`
	case t != nil:
		ctx := context.WithValue(ctx, renderDepthKey{}, depth+1)
		html, err := t.Render(ctx, RenderContext{ElementID: id, CategoryID: e.CategoryID, Lang: v.ShownLang, Principal: identity.From(ctx), Service: s}, body)
		if err != nil {
			s.log.Error("render failed", "element", id, "err", err)
			html = `<p role="alert">This element could not be rendered.</p>`
		}
		v.HTML = html
	default:
		v.HTML = `<p role="note">The element type "` + htmlEscape(e.Type) + `" is not available (plugin disabled or not installed).</p>`
	}
	if withBody && perms&acl.Edit != 0 {
		v.Body = body
	}
	v.URLs = s.Paths(ctx, "element", e.ID)
	v.URL = v.URLs[v.Lang]
	if v.URL == "" {
		v.URL = v.URLs[s.DefaultLanguage()]
	}
	return v, nil
}

// LatestRevision returns element id with its latest revision in lang
// (no other language), for the caller with the permission to view it. The
// body is for checks in the server only: it may hold parts the caller
// does not see.
func (s *Service) LatestRevision(ctx context.Context, id int64, lang string) (*Element, int, string, json.RawMessage, error) {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return nil, 0, "", nil, err
	}
	if err := s.requireElement(ctx, e, acl.View); err != nil {
		if errors.Is(err, apperr.ErrForbidden) {
			return nil, 0, "", nil, apperr.NotFound("element")
		}
		return nil, 0, "", nil, err
	}
	var rev int
	var title string
	var body json.RawMessage
	err = s.pool.QueryRow(ctx, `SELECT rev, title, body FROM element_revisions WHERE element_id=$1 AND lang=$2 ORDER BY rev DESC LIMIT 1`, id, lang).Scan(&rev, &title, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, "", nil, apperr.NotFound("element")
	}
	if err != nil {
		return nil, 0, "", nil, err
	}
	return e, rev, title, body, nil
}

// MaxRenderDepth limits elements rendered inside the rendering of elements.
const MaxRenderDepth = 4

type includeBudgetKey struct{}

type renderIDsKey struct{}

// renderIDs are the anchor ids handed out on the page being rendered.
type renderIDs struct {
	mu  sync.Mutex
	ids map[string]bool
}

// ClaimID reserves an id on the page being rendered (with the pages it
// includes); false when it is taken already. Outside a rendered page true.
func ClaimID(ctx context.Context, id string) bool {
	r, ok := ctx.Value(renderIDsKey{}).(*renderIDs)
	if !ok {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ids[id] {
		return false
	}
	r.ids[id] = true
	return true
}

// SpendInclude takes one page from the budget of included pages of the page
// being rendered (limit core.content.includes_per_page, over all levels: a
// page with many includes that again include many stays cheap). False when
// the budget is used up.
func SpendInclude(ctx context.Context) bool {
	b, ok := ctx.Value(includeBudgetKey{}).(*atomic.Int64)
	if !ok {
		return true // not inside a rendered page (tests of a macro alone)
	}
	return b.Add(-1) >= 0
}

// withIncludeBudget gives ctx one budget for the whole tree of included
// pages and one set of anchor ids, unless it has them (an outer page being
// rendered).
func (s *Service) withIncludeBudget(ctx context.Context) context.Context {
	if _, ok := ctx.Value(includeBudgetKey{}).(*atomic.Int64); ok {
		return ctx
	}
	b := &atomic.Int64{}
	b.Store(limits.Value(s.cfg.Get(), limits.IncludesPerPage))
	ctx = context.WithValue(ctx, renderIDsKey{}, &renderIDs{ids: map[string]bool{}})
	return context.WithValue(ctx, includeBudgetKey{}, b)
}

// Classification is the effective classification of an element (the
// strictest of its own and its categories); elementID 0: of the category.
func (s *Service) Classification(ctx context.Context, elementID, categoryID int64) int {
	return s.acl.ElementClassification(ctx, elementID, categoryID)
}

type renderDepthKey struct{}

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
	// Fields of plugins (e.g. properties), by field id
	Fields map[string]json.RawMessage `json:"fields"`
	// Via is an instance (a blog, a FAQ …) the element is created on behalf
	// of: its instance permissions apply instead of the page permissions of
	// the category (see requireVia).
	Via int64 `json:"via,omitempty"`
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
	var owner *int64
	if in.Via != 0 {
		inst, err := s.requireVia(ctx, in.Via, in.CategoryID, in.Type)
		if err != nil {
			return nil, err
		}
		owner = &inst.ID
	} else if err := s.require(ctx, in.CategoryID, acl.Create); err != nil {
		return nil, err
	}
	t := s.Type(in.Type)
	if t == nil {
		return nil, apperr.BadRequest("unknown or disabled element type %q", in.Type)
	}
	if in.Lang == "" {
		in.Lang = s.spaceLanguage(ctx, in.CategoryID)
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
	if err := s.guardCreate(ctx, in.Type, in.CategoryID); err != nil {
		return nil, err
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
	if err := s.guardNew(ctx, in.Type, in.CategoryID, body); err != nil {
		return nil, err
	}
	tags, err := NormalizeTags(in.Tags)
	if err != nil {
		return nil, err
	}
	short, err := NormalizeShortURL(in.ShortURL)
	if err != nil {
		return nil, err
	}
	fields, err := s.checkFields(ctx, 0, in.CategoryID, in.Type, in.Fields, true)
	if err != nil {
		return nil, err
	}
	var id, noted int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var catPath string
		if err := tx.QueryRow(ctx, `SELECT path::text FROM categories WHERE id=$1 AND deleted_at IS NULL`, in.CategoryID).Scan(&catPath); err != nil {
			return apperr.NotFound("category")
		}
		pos := 0
		if in.Position != nil {
			pos = *in.Position
			// acl: internal – next position only
		} else if err := tx.QueryRow(ctx, `SELECT COALESCE(max(position)+1,0) FROM elements WHERE category_id=$1`, in.CategoryID).Scan(&pos); err != nil {
			return err
		}
		uid := nullID(ctx)
		if err := tx.QueryRow(ctx, `INSERT INTO elements(category_id, type, position, original_lang, titles, tags, created_by, updated_by, owner_instance, hidden)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$7,$8,$9) RETURNING id`, in.CategoryID, in.Type, pos, in.Lang, map[string]string{in.Lang: title}, tags, uid, owner, s.unlisted(in.Type)).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO element_revisions(element_id, lang, rev, title, body, comment, created_by) VALUES ($1,$2,1,$3,$4,$5,$6)`,
			id, in.Lang, title, body, in.Comment, uid); err != nil {
			return err
		}
		// the data of the blocks (reports), with the revision
		if err := s.writeMacroData(ctx, tx, id, in.Type, in.Lang, 1, body); err != nil {
			return err
		}
		if err := bindSecrets(ctx, tx, t, id, body); err != nil {
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
		if err := s.storeFields(ctx, tx, id, fields); err != nil {
			return err
		}
		// Make the first element the category default.
		if _, err := tx.Exec(ctx, `UPDATE categories SET default_element_id=$2 WHERE id=$1 AND default_element_id IS NULL`, in.CategoryID, id); err != nil {
			return err
		}
		if err := audit.Log(ctx, tx, "element.create", "element", id, nil, map[string]any{"category_id": in.CategoryID, "type": in.Type, "lang": in.Lang, "title": title, "via": owner}); err != nil {
			return err
		}
		noted, err = s.note(ctx, tx, "create", "element", id) // what follows (notifications) commits with the page
		return err
	})
	if err != nil {
		return nil, err
	}
	s.fireNoted(ctx, noted, "create", "element", id)
	return s.GetElement(ctx, id, in.Lang, true)
}

// tsvExpr builds the search vector from parameters $4 (title), $6 (tags) and $5 (body).
const tsvExpr = `setweight(to_tsvector('simple', $4::text), 'A') || setweight(to_tsvector('simple', array_to_string($6::text[], ' ')), 'B') || setweight(to_tsvector('simple', $5::text), 'C')`

// searchMarks replaces the characters the search uses to mark hits in
// snippets (⟦ ⟧ in the database, \x01 \x02 from external engines): text
// on a page must not become markup in the result list.
var searchMarks = strings.NewReplacer("⟦", "[", "⟧", "]", "\x01", " ", "\x02", " ")

func (s *Service) index(ctx context.Context, q db.Querier, id, cat int64, lang, title, text string, tags []string) error {
	if len(text) > 1<<20 {
		// cut at the limit without splitting a character
		text = strings.ToValidUTF8(text[:1<<20], "")
	}
	title, text = searchMarks.Replace(title), searchMarks.Replace(text)
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
	// Tags of the element (all languages); null or missing keeps them.
	Tags []string `json:"tags"`
	// Fields of plugins (e.g. properties); missing ones are kept.
	Fields map[string]json.RawMessage `json:"fields"`
}

// SaveElement stores a new revision. Nothing is ever overwritten.
func (s *Service) SaveElement(ctx context.Context, id int64, in SaveInput) (*ElementView, error) {
	return s.saveElement(ctx, id, in, nil)
}

// SaveElementChecked stores a new revision like SaveElement, but check
// decides instead of the permission to edit whether the caller may (for
// small changes with own rules, such as ticking an own task). The caller
// must be able to view the element; check gets it and the caller's
// permissions on it. in.BaseRev is required (the revision the caller
// checked the change against must still be the latest); tags and fields
// are not changed this way.
func (s *Service) SaveElementChecked(ctx context.Context, id int64, in SaveInput, check func(e *Element, perms acl.Perm) error) (*ElementView, error) {
	if check == nil || in.BaseRev == nil || in.Tags != nil || in.Fields != nil {
		return nil, apperr.BadRequest("a check and the base revision are required; tags and fields cannot be changed this way")
	}
	return s.saveElement(ctx, id, in, check)
}

func (s *Service) saveElement(ctx context.Context, id int64, in SaveInput, check func(e *Element, perms acl.Perm) error) (*ElementView, error) {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	if check != nil {
		if err := s.requireElement(ctx, e, acl.View); err != nil {
			return nil, err
		}
		if err := check(e, s.elementPerms(ctx, e)); err != nil {
			return nil, err
		}
	} else if err := s.requireElement(ctx, e, acl.Edit); err != nil {
		return nil, err
	}
	if in.Tags != nil {
		if _, err := NormalizeTags(in.Tags); err != nil {
			return nil, err
		}
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
	fields, err := s.checkFields(ctx, id, e.CategoryID, e.Type, in.Fields, true)
	if err != nil {
		return nil, err
	}
	var noted int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		// acl: internal – row lock after requireElement
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
		if err := s.writeMacroData(ctx, tx, id, e.Type, in.Lang, cur+1, body); err != nil {
			return err
		}
		if err := bindSecrets(ctx, tx, t, id, body); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE elements SET titles = titles || jsonb_build_object($2::text, $3::text), updated_at=now(), updated_by=$4 WHERE id=$1`,
			id, in.Lang, title, uid); err != nil {
			return err
		}
		if err := s.index(ctx, tx, id, e.CategoryID, in.Lang, title, t.Text(body), e.Tags); err != nil {
			return err
		}
		if err := s.storeFields(ctx, tx, id, fields); err != nil {
			return err
		}
		if err := audit.Log(ctx, tx, "element.save", "element", id, map[string]any{"lang": in.Lang, "rev": cur}, map[string]any{"lang": in.Lang, "rev": cur + 1, "title": title}); err != nil {
			return err
		}
		noted, err = s.note(ctx, tx, "update", "element", id) // what follows (notifications) commits with the page
		return err
	})
	if err != nil {
		return nil, err
	}
	if in.Tags != nil {
		if _, err := s.UpdateElementMeta(ctx, id, MetaInput{Tags: in.Tags}); err != nil {
			s.fireNoted(ctx, noted, "update", "element", id) // the page is saved
			return nil, err
		}
	}
	s.fireNoted(ctx, noted, "update", "element", id)
	return s.GetElement(ctx, id, in.Lang, true)
}

// MetaInput changes element metadata.
type MetaInput struct {
	Tags     []string `json:"tags"`
	Position *int     `json:"position"`
	ShortURL *string  `json:"short_url"` // "" removes it
	// ShortOnly: only the short URL reaches the element (no readable paths)
	ShortOnly *bool `json:"short_only"`
	// PreviewImage: image for link previews (path like /api/v1/files/… or https URL; "" = automatic)
	PreviewImage *string `json:"preview_image"`
	// Hidden: not in listings, sources, feeds and the search.
	Hidden *bool `json:"hidden"`
}

// UpdateElementMeta changes tags and position.
func (s *Service) UpdateElementMeta(ctx context.Context, id int64, in MetaInput) (*Element, error) {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	if err := s.requireElement(ctx, e, acl.Edit); err != nil {
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
		if in.ShortOnly != nil {
			if _, err := tx.Exec(ctx, `UPDATE elements SET short_only=$2 WHERE id=$1`, id, *in.ShortOnly); err != nil {
				return err
			}
		}
		if in.Hidden != nil {
			if !*in.Hidden && s.unlisted(e.Type) {
				return apperr.BadRequest("elements of the type %s are never listed", e.Type)
			}
			if _, err := tx.Exec(ctx, `UPDATE elements SET hidden=$2 WHERE id=$1`, id, *in.Hidden); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE search_docs SET tags=$2, tsv=setweight(to_tsvector('simple', title), 'A') ||
			setweight(to_tsvector('simple', array_to_string($2::text[], ' ')), 'B') || setweight(to_tsvector('simple', body_text), 'C')
			WHERE element_id=$1`, id, tags); err != nil {
			return err
		}
		hidden := e.Hidden
		if in.Hidden != nil {
			hidden = *in.Hidden
		}
		return audit.Log(ctx, tx, "element.meta", "element", id, map[string]any{"tags": e.Tags, "position": e.Position, "preview_image": e.PreviewImage, "hidden": e.Hidden},
			map[string]any{"tags": tags, "position": pos, "preview_image": preview, "hidden": hidden})
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
	if err := s.requireMove(ctx, e, categoryID); err != nil {
		return nil, err
	}
	if err := s.guardChange(ctx, e); err != nil {
		return nil, err
	}
	if err := s.guardCreate(ctx, e.Type, categoryID); err != nil {
		return nil, err
	}
	var noted int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.andNote(ctx, tx, &noted, "move", "element", id, s.moveElementTx(ctx, tx, id, categoryID, nil))
	})
	if err != nil {
		return nil, err
	}
	if e.OwnRules || e.OwnRightsOnly || e.Classification > 0 {
		s.aclChanged(ctx) // the snapshot knows the category of elements with own settings
	}
	_ = s.syncSlug(ctx, "element", id, e.CategoryID)
	s.fireNoted(ctx, noted, "move", "element", id)
	return s.loadElement(ctx, s.pool, id, false)
}

// moveElementTx moves an element inside a transaction (the checks of the
// caller apply) and writes the audit entry; position nil puts it last.
func (s *Service) moveElementTx(ctx context.Context, tx pgx.Tx, id, categoryID int64, position *int) error {
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM categories WHERE id=$1 AND deleted_at IS NULL)`, categoryID).Scan(&ok); err != nil || !ok {
		return apperr.NotFound("target category")
	}
	var oldCat int64
	var oldPos int
	// acl: internal – row lock; callers check requireMove
	if err := tx.QueryRow(ctx, `SELECT category_id, position FROM elements WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&oldCat, &oldPos); err != nil {
		return apperr.NotFound("element")
	}
	// acl: internal – next position only
	if _, err := tx.Exec(ctx, `UPDATE elements SET category_id=$2,
		position=COALESCE($3, (SELECT COALESCE(max(position)+1,0) FROM elements WHERE category_id=$2)), updated_at=now() WHERE id=$1`, id, categoryID, position); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE categories SET default_element_id=NULL WHERE default_element_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE search_docs SET category_id=$2 WHERE element_id=$1`, id, categoryID); err != nil {
		return err
	}
	return audit.Log(ctx, tx, "element.move", "element", id, map[string]any{"category_id": oldCat, "position": oldPos}, map[string]any{"category_id": categoryID})
}

// DuplicateElement copies an element (latest revision of each language).
// The copy keeps the classification and hidden flag; its own rules and
// "own rights only" only with withRights. Without them, a copy of an
// element with "own rights only" needs the permission to give rights on it
// (the copy is visible to everybody in the target category).
func (s *Service) DuplicateElement(ctx context.Context, id, categoryID int64, withRights bool) (*Element, error) {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return nil, err
	}
	if err := s.requireElement(ctx, e, acl.View); err != nil {
		return nil, err
	}
	if s.acl.ElementClassification(ctx, e.ID, e.CategoryID) >= acl.StrictConfidential {
		return nil, apperr.Forbidden("strictly confidential content is not copied")
	}
	if e.OwnRightsOnly && !withRights {
		if err := s.requireElement(ctx, e, acl.Grant); err != nil {
			return nil, apperr.Forbidden("this page has own rights only: copy it with its rights, or ask someone who may give rights on it")
		}
	}
	if categoryID == 0 {
		categoryID = e.CategoryID
	}
	if err := s.require(ctx, categoryID, acl.Create); err != nil {
		return nil, err
	}
	if err := s.guardCopy(ctx, s.pool, id, e.Type, categoryID); err != nil {
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
		if nid, err = copyElement(ctx, tx, id, categoryID, suffix, withRights); err != nil {
			return err
		}
		if err := s.writeMacroDataAll(ctx, tx, nid); err != nil {
			return err
		}
		// acl: internal – next position only
		if _, err := tx.Exec(ctx, `UPDATE elements SET position=(SELECT COALESCE(max(position)+1,0) FROM elements WHERE category_id=$2) WHERE id=$1`, nid, categoryID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "element.duplicate", "element", nid, map[string]any{"source": id, "with_rights": withRights}, nil)
	})
	if err != nil {
		return nil, err
	}
	if e.Classification > 0 || (withRights && (e.OwnRules || e.OwnRightsOnly)) {
		s.aclChanged(ctx) // the copy has own settings
	}
	s.bus.Publish(ctx, "i18n", "") // the labels of the copy
	return s.loadElement(ctx, s.pool, nid, false)
}

// DeleteElement moves an element to the trash.
func (s *Service) DeleteElement(ctx context.Context, id int64) error {
	e, err := s.loadElement(ctx, s.pool, id, false)
	if err != nil {
		return err
	}
	if err := s.requireElement(ctx, e, acl.Delete); err != nil {
		return err
	}
	if err := s.guardChange(ctx, e); err != nil {
		return err
	}
	var noted int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE elements SET deleted_at=now(), deleted_by=$2, trash_root=true WHERE id=$1`, id, nullID(ctx)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE categories SET default_element_id=NULL WHERE default_element_id=$1`, id); err != nil {
			return err
		}
		return s.andNote(ctx, tx, &noted, "delete", "element", id, audit.Log(ctx, tx, "element.trash", "element", id, map[string]any{"titles": e.Titles}, nil))
	})
	if err == nil {
		s.fireNoted(ctx, noted, "delete", "element", id)
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
	if err := s.requireElement(ctx, e, acl.View); err != nil {
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
	if err := s.requireElement(ctx, e, acl.View); err != nil {
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
		x.HTML, _ = t.Render(s.withIncludeBudget(ctx), RenderContext{ElementID: id, CategoryID: e.CategoryID, Lang: lang, Principal: identity.From(ctx), Service: s}, x.Body)
	}
	if s.elementPerms(ctx, e)&acl.Edit == 0 {
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
	if err := s.requireElement(ctx, e, acl.Edit); err != nil {
		return nil, err
	}
	var noted int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.restoreRev(ctx, tx, e, lang, rev, "Restored revision"); err != nil {
			return err
		}
		var err error
		noted, err = s.note(ctx, tx, "update", "element", id)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.fireNoted(ctx, noted, "update", "element", id)
	return s.GetElement(ctx, id, lang, true)
}

// restoreRev copies revision rev of lang to a new latest revision. It returns
// false if rev already is the latest revision.
func (s *Service) restoreRev(ctx context.Context, tx pgx.Tx, e *Element, lang string, rev int, comment string) (bool, error) {
	// acl: internal – row lock after requireElement
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
	// an old revision becomes the content again: a change like any other,
	// so a type with own permissions decides (Guard), with the bodies
	if g, ok := s.Type(e.Type).(Guard); ok {
		var curBody json.RawMessage
		if err := tx.QueryRow(ctx, `SELECT body FROM element_revisions WHERE element_id=$1 AND lang=$2 AND rev=$3`, e.ID, lang, cur).Scan(&curBody); err != nil {
			return false, err
		}
		if err := g.CheckChange(ctx, s, e, curBody, body); err != nil {
			return false, err
		}
	}
	uid := nullID(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO element_revisions(element_id, lang, rev, source_rev, title, body, comment, restored_from, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, e.ID, lang, cur+1, src, title, body, comment, rev, uid); err != nil {
		return false, err
	}
	if err := s.writeMacroData(ctx, tx, e.ID, e.Type, lang, cur+1, body); err != nil {
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
	return t.Render(s.withIncludeBudget(ctx), RenderContext{Lang: lang, Principal: identity.From(ctx), Service: s}, norm)
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
// The space of the category may also limit the element types.
func (s *Service) guardCreate(ctx context.Context, typ string, categoryID int64) error {
	if err := s.spaceAllows(ctx, typ, categoryID); err != nil {
		return err
	}
	if g, ok := s.Type(typ).(Guard); ok {
		return g.CheckCreate(ctx, s, categoryID)
	}
	return nil
}

// guardNew asks the element type whether a new element with body may be
// created in categoryID by the caller (as for a change of its body: a body
// refused for a change is refused for a new element, a copy too).
func (s *Service) guardNew(ctx context.Context, typ string, categoryID int64, body json.RawMessage) error {
	g, ok := s.Type(typ).(Guard)
	if !ok {
		return nil
	}
	e := &Element{CategoryID: categoryID, Type: typ}
	if uid := identity.From(ctx).ID(); uid > 0 {
		e.CreatedByID = &uid
	}
	return g.CheckChange(ctx, s, e, nil, body)
}

// guardCopy checks the latest body of every language of element id for a
// copy into categoryID (guardCreate and guardNew).
func (s *Service) guardCopy(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, id int64, typ string, categoryID int64) error {
	if _, ok := s.Type(typ).(Guard); !ok {
		return nil
	}
	if err := s.guardCreate(ctx, typ, categoryID); err != nil {
		return err
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT ON (lang) body FROM element_revisions WHERE element_id=$1 ORDER BY lang, rev DESC`, id)
	if err != nil {
		return err
	}
	bodies, err := pgx.CollectRows(rows, pgx.RowTo[json.RawMessage])
	if err != nil {
		return err
	}
	for _, b := range bodies {
		if err := s.guardNew(ctx, typ, categoryID, b); err != nil {
			return err
		}
	}
	return nil
}

// bindSecrets binds the secret references of a saved body to the element.
func bindSecrets(ctx context.Context, tx pgx.Tx, t ElementType, id int64, body json.RawMessage) error {
	h, ok := t.(SecretHolder)
	if !ok {
		return nil
	}
	return secrets.BindRefs(ctx, tx, id, identity.From(ctx).ID(), h.SecretRefs(body))
}

// VisibleElements reports which of the (not deleted) elements ids the caller
// may view (their own rights apply).
func (s *Service) VisibleElements(ctx context.Context, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	f, err := s.acl.ElementFilter(ctx, identity.From(ctx), acl.View)
	if err != nil {
		return nil, err
	}
	// acl: checked below (ElementFilter)
	rows, err := s.pool.Query(ctx, `SELECT id, category_id FROM elements WHERE id = ANY($1) AND deleted_at IS NULL`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, cat int64
		if err := rows.Scan(&id, &cat); err != nil {
			return nil, err
		}
		if f.Allows(id, cat) {
			out[id] = true
		}
	}
	return out, rows.Err()
}
