package content

// Drafts, scheduled publication and templates.
//
// A draft holds unpublished changes of one language of an element, or a new
// element that does not exist yet. Readers always see the published
// revisions; a draft is visible to its author and to everybody who may edit
// in the category (so it can be worked on together). Publishing turns the
// draft into a revision (or a new element) – immediately or at a set time.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Draft is an unpublished state of an element.
type Draft struct {
	ID         int64           `json:"id"`
	ElementID  *int64          `json:"element_id"` // nil: a new element
	CategoryID int64           `json:"category_id"`
	Type       string          `json:"type"`
	Lang       string          `json:"lang"`
	Title      string          `json:"title"`
	Body       json.RawMessage `json:"body,omitempty"`
	Tags       []string        `json:"tags"`
	Comment    string          `json:"comment"`
	BaseRev    int             `json:"base_rev"`
	CreatedBy  string          `json:"created_by"`
	UpdatedBy  string          `json:"updated_by"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	PublishAt  *time.Time      `json:"publish_at"`
	Options    json.RawMessage `json:"publish_options"`
	createdBy  *int64
	publishBy  *int64
}

const draftCols = `d.id, d.element_id, d.category_id, d.type, d.lang, d.title, d.body, d.tags, d.comment, d.base_rev,
	COALESCE((SELECT u.display_name FROM users u WHERE u.id = d.created_by), ''), COALESCE((SELECT u.display_name FROM users u WHERE u.id = d.updated_by), ''),
	d.created_at, d.updated_at, d.publish_at, d.publish_options, d.created_by, d.publish_by`

func scanDraft(r pgx.CollectableRow) (Draft, error) {
	var d Draft
	err := r.Scan(&d.ID, &d.ElementID, &d.CategoryID, &d.Type, &d.Lang, &d.Title, &d.Body, &d.Tags, &d.Comment, &d.BaseRev,
		&d.CreatedBy, &d.UpdatedBy, &d.CreatedAt, &d.UpdatedAt, &d.PublishAt, &d.Options, &d.createdBy, &d.publishBy)
	return d, err
}

// DraftInput saves a draft.
type DraftInput struct {
	ElementID  *int64          `json:"element_id"`  // changes of an existing element
	CategoryID int64           `json:"category_id"` // new element: its category
	Type       string          `json:"type"`        // new element: its type
	Lang       string          `json:"lang"`
	Title      string          `json:"title"`
	Body       json.RawMessage `json:"body"`
	Tags       []string        `json:"tags"`
	Comment    string          `json:"comment"`
	BaseRev    int             `json:"base_rev"`
}

// canSeeDraft: the author, or edit permission in the category.
func (s *Service) canSeeDraft(ctx context.Context, d *Draft) bool {
	p := identity.From(ctx)
	if p == nil {
		return false
	}
	if d.createdBy != nil && *d.createdBy == p.ID() && (s.acl.Can(ctx, p, d.CategoryID, acl.Create) || s.acl.Can(ctx, p, d.CategoryID, acl.Edit)) {
		return true
	}
	return s.acl.Can(ctx, p, d.CategoryID, acl.Edit)
}

// SaveDraft creates or updates a draft (one per element and language;
// new elements: in.ID via UpdateDraft).
func (s *Service) SaveDraft(ctx context.Context, id int64, in DraftInput) (*Draft, error) {
	p := identity.From(ctx)
	if p == nil {
		return nil, apperr.ErrUnauthorized
	}
	if id != 0 {
		d, err := s.GetDraft(ctx, id)
		if err != nil {
			return nil, err
		}
		in.ElementID, in.CategoryID, in.Type = d.ElementID, d.CategoryID, d.Type
		if in.Lang == "" {
			in.Lang = d.Lang
		}
	}
	if in.ElementID != nil {
		e, err := s.loadElement(ctx, s.pool, *in.ElementID, false)
		if err != nil {
			return nil, err
		}
		if err := s.require(ctx, e.CategoryID, acl.Edit); err != nil {
			return nil, err
		}
		in.CategoryID, in.Type = e.CategoryID, e.Type
		if in.Lang == "" {
			in.Lang = e.OriginalLang
		}
	} else {
		if err := s.require(ctx, in.CategoryID, acl.Create); err != nil {
			return nil, err
		}
		if err := s.guardCreate(ctx, in.Type, in.CategoryID); err != nil {
			return nil, err
		}
		if in.Lang == "" {
			in.Lang = s.DefaultLanguage()
		}
	}
	if !s.validLang(in.Lang) {
		return nil, apperr.BadRequest("language %q is not enabled", in.Lang)
	}
	t := s.Type(in.Type)
	if t == nil {
		return nil, apperr.BadRequest("unknown or disabled element type %q", in.Type)
	}
	if len(in.Body) == 0 {
		in.Body = t.EmptyBody()
	}
	body, err := t.Normalize(in.Body)
	if err != nil {
		return nil, apperr.BadRequest("invalid body: %v", err)
	}
	tags, err := NormalizeTags(in.Tags)
	if err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []string{}
	}
	uid := nullID(ctx)
	switch {
	case id != 0:
		_, err = s.pool.Exec(ctx, `UPDATE element_drafts SET title=$2, body=$3, tags=$4, comment=$5, base_rev=GREATEST(base_rev, $6), lang=$7, updated_by=$8, updated_at=now() WHERE id=$1`,
			id, in.Title, body, tags, in.Comment, in.BaseRev, in.Lang, uid)
	case in.ElementID != nil:
		err = s.pool.QueryRow(ctx, `INSERT INTO element_drafts(element_id, category_id, type, lang, title, body, tags, comment, base_rev, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)
			ON CONFLICT (element_id, lang) WHERE element_id IS NOT NULL DO UPDATE SET title=EXCLUDED.title, body=EXCLUDED.body, tags=EXCLUDED.tags,
				comment=EXCLUDED.comment, updated_by=EXCLUDED.updated_by, updated_at=now()
			RETURNING id`, *in.ElementID, in.CategoryID, in.Type, in.Lang, in.Title, body, tags, in.Comment, in.BaseRev, uid).Scan(&id)
	default:
		err = s.pool.QueryRow(ctx, `INSERT INTO element_drafts(category_id, type, lang, title, body, tags, comment, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8) RETURNING id`, in.CategoryID, in.Type, in.Lang, in.Title, body, tags, in.Comment, uid).Scan(&id)
	}
	if err != nil {
		return nil, err
	}
	_ = audit.Log(ctx, s.pool, "draft.save", "draft", id, nil, map[string]any{"element_id": in.ElementID, "category_id": in.CategoryID, "lang": in.Lang, "title": in.Title})
	return s.GetDraft(ctx, id)
}

// GetDraft returns a draft the caller may see.
func (s *Service) GetDraft(ctx context.Context, id int64) (*Draft, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+draftCols+` FROM element_drafts d WHERE d.id=$1`, id)
	if err != nil {
		return nil, err
	}
	d, err := pgx.CollectExactlyOneRow(rows, scanDraft)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("draft")
	}
	if err != nil {
		return nil, err
	}
	if !s.canSeeDraft(ctx, &d) {
		return nil, apperr.NotFound("draft")
	}
	return &d, nil
}

// Drafts lists the drafts of a category (new elements and changes) or of
// an element that the caller may see; bodies are left out.
func (s *Service) Drafts(ctx context.Context, categoryID, elementID int64) ([]Draft, error) {
	if identity.From(ctx) == nil {
		return []Draft{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+draftCols+` FROM element_drafts d
		WHERE ($1 = 0 OR d.category_id = $1) AND ($2 = 0 OR d.element_id = $2) AND ($1 <> 0 OR $2 <> 0 OR d.created_by = $3 OR d.updated_by = $3)
		ORDER BY d.updated_at DESC LIMIT 200`, categoryID, elementID, identity.From(ctx).ID())
	if err != nil {
		return nil, err
	}
	all, err := pgx.CollectRows(rows, scanDraft)
	if err != nil {
		return nil, err
	}
	out := []Draft{}
	for i := range all {
		if s.canSeeDraft(ctx, &all[i]) {
			all[i].Body = nil
			out = append(out, all[i])
		}
	}
	return out, nil
}

// DeleteDraft discards a draft.
func (s *Service) DeleteDraft(ctx context.Context, id int64) error {
	d, err := s.GetDraft(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM element_drafts WHERE id=$1`, id); err != nil {
		return err
	}
	_ = audit.Log(ctx, s.pool, "draft.delete", "draft", id, map[string]any{"element_id": d.ElementID, "title": d.Title}, nil)
	return nil
}

// PublishInput publishes a draft.
type PublishInput struct {
	// CategoryID is the target category of a new element (default: the
	// category of the draft); the caller needs create permission there.
	CategoryID int64 `json:"category_id"`
	// At schedules the publication (nil or past: now).
	At *time.Time `json:"at"`
	// Notify tells the watchers (default true).
	Notify *bool `json:"notify"`
	// Options are passed to publish options of plugins (e.g. news carousels).
	Options map[string]json.RawMessage `json:"options"`
}

// PublishResult is the result of publishing.
type PublishResult struct {
	Element   *ElementView `json:"element,omitempty"`
	Scheduled *time.Time   `json:"scheduled,omitempty"`
}

// PublishOption is an extension of the publish dialog by a plugin (e.g.
// "show as news"). Apply runs after the element was published.
type PublishOption interface {
	ID() string
	Apply(ctx context.Context, svc *Service, el *ElementView, value json.RawMessage) error
}

// RegisterPublishOption adds a publish option of a plugin.
func (s *Service) RegisterPublishOption(pluginID string, o PublishOption) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.publishOptions == nil {
		s.publishOptions = map[string]publishOption{}
	}
	s.publishOptions[o.ID()] = publishOption{pluginID, o}
}

type publishOption struct {
	plugin string
	opt    PublishOption
}

// PublishDraft publishes a draft now or schedules it.
func (s *Service) PublishDraft(ctx context.Context, id int64, in PublishInput) (*PublishResult, error) {
	d, err := s.GetDraft(ctx, id)
	if err != nil {
		return nil, err
	}
	target := d.CategoryID
	if d.ElementID == nil && in.CategoryID != 0 {
		target = in.CategoryID
	}
	need := acl.Edit
	if d.ElementID == nil {
		need = acl.Create
	}
	if err := s.require(ctx, target, need); err != nil {
		return nil, err
	}
	if in.At != nil && in.At.After(time.Now().Add(30*time.Second)) {
		opts, _ := json.Marshal(in)
		if _, err := s.pool.Exec(ctx, `UPDATE element_drafts SET publish_at=$2, publish_by=$3, publish_options=$4, category_id=CASE WHEN element_id IS NULL THEN $5 ELSE category_id END WHERE id=$1`,
			id, *in.At, nullID(ctx), opts, target); err != nil {
			return nil, err
		}
		_ = audit.Log(ctx, s.pool, "draft.schedule", "draft", id, nil, map[string]any{"at": in.At, "category_id": target, "title": d.Title})
		return &PublishResult{Scheduled: in.At}, nil
	}
	return s.publish(ctx, d, target, in)
}

// Unschedule cancels a scheduled publication.
func (s *Service) Unschedule(ctx context.Context, id int64) error {
	if _, err := s.GetDraft(ctx, id); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE element_drafts SET publish_at=NULL, publish_by=NULL WHERE id=$1`, id)
	return err
}

func (s *Service) publish(ctx context.Context, d *Draft, target int64, in PublishInput) (*PublishResult, error) {
	if in.Notify != nil && !*in.Notify {
		ctx = WithSilent(ctx)
	}
	var el *ElementView
	var err error
	if d.ElementID != nil {
		base := d.BaseRev
		el, err = s.SaveElement(ctx, *d.ElementID, SaveInput{Lang: d.Lang, Title: d.Title, Body: d.Body, Comment: d.Comment, BaseRev: baseRev(base)})
		if err == nil && len(d.Tags) > 0 {
			_, err = s.UpdateElementMeta(ctx, *d.ElementID, MetaInput{Tags: d.Tags})
		}
	} else {
		el, err = s.CreateElement(ctx, ElementInput{CategoryID: target, Type: d.Type, Lang: d.Lang, Title: d.Title, Body: d.Body, Tags: d.Tags, Comment: d.Comment})
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM element_drafts WHERE id=$1`, d.ID); err != nil {
		return nil, err
	}
	_ = audit.Log(ctx, s.pool, "draft.publish", "element", el.ID, map[string]any{"draft": d.ID}, map[string]any{"lang": d.Lang, "title": d.Title})
	s.mu.Lock()
	opts := s.publishOptions
	s.mu.Unlock()
	for key, v := range in.Options {
		if o, ok := opts[key]; ok && (s.enabled == nil || s.enabled(o.plugin)) {
			if err := o.opt.Apply(ctx, s, el, v); err != nil {
				s.log.Error("publish option failed", "option", key, "element", el.ID, "err", err)
			}
		}
	}
	return &PublishResult{Element: el}, nil
}

// baseRev: 0 (a draft of a new translation) means no conflict check.
func baseRev(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

// PublishDue publishes scheduled drafts (worker). The publication runs with
// the permissions of the person who scheduled it.
func (s *Service) PublishDue(ctx context.Context, load func(context.Context, int64) (*identity.Principal, error)) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+draftCols+` FROM element_drafts d WHERE d.publish_at <= now() ORDER BY d.publish_at LIMIT 50`)
	if err != nil {
		return 0, err
	}
	due, err := pgx.CollectRows(rows, scanDraft)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range due {
		d := &due[i]
		// claim it (cluster: only one node publishes)
		tag, err := s.pool.Exec(ctx, `UPDATE element_drafts SET publish_at=NULL WHERE id=$1 AND publish_at IS NOT NULL`, d.ID)
		if err != nil || tag.RowsAffected() == 0 || d.publishBy == nil {
			continue
		}
		p, err := load(ctx, *d.publishBy)
		if err != nil {
			s.log.Error("scheduled publication: publisher not found", "draft", d.ID, "err", err)
			continue
		}
		var in PublishInput
		_ = json.Unmarshal(d.Options, &in)
		pctx := identity.With(ctx, p)
		if _, err := s.publish(pctx, d, d.CategoryID, in); err != nil {
			s.log.Error("scheduled publication failed", "draft", d.ID, "err", err)
			_ = audit.Log(pctx, s.pool, "draft.publish_failed", "draft", d.ID, nil, map[string]string{"error": err.Error()})
			continue
		}
		n++
	}
	return n, nil
}

// ---------------------------------------------------------------- silent changes

type silentKey struct{}

// WithSilent marks changes that should not notify watchers.
func WithSilent(ctx context.Context) context.Context { return context.WithValue(ctx, silentKey{}, true) }

// IsSilent reports whether watchers should not be notified.
func IsSilent(ctx context.Context) bool { v, _ := ctx.Value(silentKey{}).(bool); return v }

// ---------------------------------------------------------------- templates

// Template is a template for new elements.
type Template struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	Lang        string          `json:"lang"`
	Title       string          `json:"title"`
	Body        json.RawMessage `json:"body,omitempty"`
	Tags        []string        `json:"tags"`
	Global      bool            `json:"global"`
	Mine        bool            `json:"mine"`
	CreatedBy   string          `json:"created_by"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// TemplateInput saves a template.
type TemplateInput struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	Lang        string          `json:"lang"`
	Title       string          `json:"title"`
	Body        json.RawMessage `json:"body"`
	Tags        []string        `json:"tags"`
	Global      bool            `json:"global"` // for everybody (core.templates.global)
}

// Templates lists the personal templates of the caller and the global ones
// (optionally of one element type); withBody includes the bodies.
func (s *Service) Templates(ctx context.Context, typ string, id int64) ([]Template, error) {
	p := identity.From(ctx)
	if p == nil {
		return nil, apperr.ErrUnauthorized
	}
	rows, err := s.pool.Query(ctx, `SELECT t.id, t.name, t.description, t.type, t.lang, t.title, CASE WHEN $3 <> 0 THEN t.body END, t.tags, t.owner_id IS NULL,
			COALESCE(t.owner_id = $1, false), COALESCE((SELECT u.display_name FROM users u WHERE u.id = t.created_by), ''), t.updated_at
		FROM element_templates t WHERE (t.owner_id IS NULL OR t.owner_id = $1) AND ($2 = '' OR t.type = $2) AND ($3 = 0 OR t.id = $3)
		ORDER BY t.owner_id IS NULL, t.name`, p.ID(), typ, id)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Template, error) {
		var t Template
		return t, r.Scan(&t.ID, &t.Name, &t.Description, &t.Type, &t.Lang, &t.Title, &t.Body, &t.Tags, &t.Global, &t.Mine, &t.CreatedBy, &t.UpdatedAt)
	})
	if err != nil {
		return nil, err
	}
	enabled := out[:0]
	for _, t := range out {
		if s.Type(t.Type) != nil {
			enabled = append(enabled, t)
		}
	}
	return enabled, nil
}

// SaveTemplate creates (id 0) or changes a template: own ones, global ones
// with core.templates.global.
func (s *Service) SaveTemplate(ctx context.Context, id int64, in TemplateInput) (*Template, error) {
	p := identity.From(ctx)
	if p == nil {
		return nil, apperr.ErrUnauthorized
	}
	if in.Name == "" {
		return nil, apperr.BadRequest("name is required")
	}
	if in.Global && !perm.HasP(p, "core.templates.global") {
		return nil, apperr.Forbidden("saving templates for everybody needs the permission %q", "Save templates for everybody")
	}
	t := s.Type(in.Type)
	if t == nil {
		return nil, apperr.BadRequest("unknown or disabled element type %q", in.Type)
	}
	body, err := t.Normalize(in.Body)
	if err != nil {
		return nil, apperr.BadRequest("invalid body: %v", err)
	}
	if in.Lang == "" || !s.validLang(in.Lang) {
		in.Lang = s.DefaultLanguage()
	}
	tags, err := NormalizeTags(in.Tags)
	if err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []string{}
	}
	var owner *int64
	if !in.Global {
		uid := p.ID()
		owner = &uid
	}
	if id == 0 {
		err = s.pool.QueryRow(ctx, `INSERT INTO element_templates(name, description, type, lang, title, body, tags, owner_id, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, in.Name, in.Description, in.Type, in.Lang, in.Title, body, tags, owner, nullID(ctx)).Scan(&id)
	} else {
		if err := s.canChangeTemplate(ctx, id); err != nil {
			return nil, err
		}
		_, err = s.pool.Exec(ctx, `UPDATE element_templates SET name=$2, description=$3, type=$4, lang=$5, title=$6, body=$7, tags=$8, owner_id=$9, updated_at=now() WHERE id=$1`,
			id, in.Name, in.Description, in.Type, in.Lang, in.Title, body, tags, owner)
	}
	if err != nil {
		return nil, err
	}
	_ = audit.Log(ctx, s.pool, "template.save", "template", id, nil, map[string]any{"name": in.Name, "type": in.Type, "global": in.Global})
	list, err := s.Templates(ctx, "", id)
	if err != nil || len(list) == 0 {
		return nil, apperr.NotFound("template")
	}
	return &list[0], nil
}

func (s *Service) canChangeTemplate(ctx context.Context, id int64) error {
	p := identity.From(ctx)
	var owner *int64
	if err := s.pool.QueryRow(ctx, `SELECT owner_id FROM element_templates WHERE id=$1`, id).Scan(&owner); err != nil {
		return apperr.NotFound("template")
	}
	if owner == nil {
		if !perm.HasP(p, "core.templates.global") {
			return apperr.ErrForbidden
		}
		return nil
	}
	if *owner != p.ID() {
		return apperr.NotFound("template")
	}
	return nil
}

// DeleteTemplate deletes a template (own ones, global ones with core.templates.global).
func (s *Service) DeleteTemplate(ctx context.Context, id int64) error {
	if identity.From(ctx) == nil {
		return apperr.ErrUnauthorized
	}
	if err := s.canChangeTemplate(ctx, id); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM element_templates WHERE id=$1`, id); err != nil {
		return err
	}
	return audit.Log(ctx, s.pool, "template.delete", "template", id, nil, nil)
}
