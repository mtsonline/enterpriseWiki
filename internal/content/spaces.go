package content

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Spaces: every top-level category is a space with its own template, key,
// sidebar, managers and features.
// Its settings apply to the whole subtree: key with the address /s/KEY,
// own navigation with shortcuts, allowed element types, default language.
// Managing a space needs the permission Manage on its top-level category.

// SpaceLink is a shortcut in the navigation of a space.
type SpaceLink struct {
	Label map[string]string `json:"label"`
	URL   string            `json:"url"`
}

// Space holds the settings of a space.
type Space struct {
	// OwnTree: the navigation shows only this space (with its shortcuts)
	OwnTree   bool        `json:"own_tree"`
	Shortcuts []SpaceLink `json:"shortcuts"`
	// AllowedTypes limits the element types (empty: all active types)
	AllowedTypes []string `json:"allowed_types"`
	// AllowedMacros limits the macros (empty: all active macros)
	AllowedMacros []string `json:"allowed_macros"`
	// Accent: color of links and buttons in the space (#rrggbb, contrast
	// at least 4.5:1 with white); Logo: picture of the wiki (files or media)
	Accent string `json:"accent"`
	Logo   string `json:"logo"`
	// Language of new elements ("" = the default language of the site)
	Language string `json:"language"`
	// IsTemplate: offered as template for new spaces
	IsTemplate bool `json:"is_template"`
	// Personal: the owner of a personal space (set when it is created)
	Personal int64 `json:"personal,omitempty"`
}

// SpaceInfo is a space with its category texts.
type SpaceInfo struct {
	ID           int64             `json:"id"`
	Key          string            `json:"key"`
	Titles       map[string]string `json:"titles"`
	Descriptions map[string]string `json:"descriptions"`
	// Archived: read-only, out of navigation and search (directory: separately)
	Archived bool `json:"archived"`
	// CanManage: the caller may change the space (settings, templates)
	CanManage bool `json:"can_manage"`
	// Member: the caller has own permissions here (as user, group or role)
	Member bool `json:"member"`
	Space
}

// SpaceInput changes a space (nil fields stay).
type SpaceInput struct {
	Key *string `json:"key"`
	Space
}

var (
	spaceKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,15}$`)
	accentRe   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	logoRe     = regexp.MustCompile(`^/api/v1/(files|media)/[A-Za-z0-9._/-]{1,200}$`)
)

// contrastWithWhite is the WCAG contrast ratio of a #rrggbb color with white.
func contrastWithWhite(hex string) float64 {
	lin := func(i int) float64 {
		v, _ := strconv.ParseUint(hex[i:i+2], 16, 8)
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	l := 0.2126*lin(1) + 0.7152*lin(3) + 0.0722*lin(5)
	return 1.05 / (l + 0.05)
}

const maxShortcuts = 30

func (s *Service) loadSpace(ctx context.Context, top int64) (*SpaceInfo, error) {
	info := &SpaceInfo{ID: top}
	var key *string
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT space_key, titles, descriptions, space, archived_at IS NOT NULL FROM categories WHERE id=$1 AND parent_id IS NULL AND deleted_at IS NULL`, top).
		Scan(&key, &info.Titles, &info.Descriptions, &raw, &info.Archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("space")
	}
	if err != nil {
		return nil, err
	}
	if key != nil {
		info.Key = *key
	}
	_ = json.Unmarshal(raw, &info.Space)
	info.CanManage = s.require(ctx, top, acl.Manage) == nil
	info.Member = s.acl.Member(ctx, identity.From(ctx), top)
	if info.Shortcuts == nil {
		info.Shortcuts = []SpaceLink{}
	}
	if info.AllowedTypes == nil {
		info.AllowedTypes = []string{}
	}
	if info.AllowedMacros == nil {
		info.AllowedMacros = []string{}
	}
	return info, nil
}

// topOf returns the top-level category of a category.
func (s *Service) topOf(ctx context.Context, categoryID int64) (int64, error) {
	var top int64
	err := s.pool.QueryRow(ctx, `SELECT split_part(path::text, '.', 1)::bigint FROM categories WHERE id=$1`, categoryID).Scan(&top)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, apperr.NotFound("category")
	}
	return top, err
}

// SpaceOf returns the space of a category, nil when the caller may not
// view the space's top-level category (its name and links stay hidden).
func (s *Service) SpaceOf(ctx context.Context, categoryID int64) (*SpaceInfo, error) {
	top, err := s.topOf(ctx, categoryID)
	if err != nil {
		return nil, err
	}
	if s.require(ctx, top, acl.View) != nil {
		return nil, nil
	}
	return s.loadSpace(ctx, top)
}

// SpaceByKey finds a space the caller may view by its key.
func (s *Service) SpaceByKey(ctx context.Context, key string) (*SpaceInfo, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM categories WHERE space_key=$1 AND parent_id IS NULL AND deleted_at IS NULL`, strings.ToUpper(strings.TrimSpace(key))).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("space")
	}
	if err != nil {
		return nil, err
	}
	if s.require(ctx, id, acl.View) != nil {
		return nil, apperr.NotFound("space")
	}
	return s.loadSpace(ctx, id)
}

// Spaces lists the spaces the caller may view (not hidden ones).
func (s *Service) Spaces(ctx context.Context) ([]SpaceInfo, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM categories WHERE parent_id IS NULL AND deleted_at IS NULL AND NOT hidden ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	if ids, err = s.acl.Filter(ctx, identity.From(ctx), ids, acl.View); err != nil {
		return nil, err
	}
	out := []SpaceInfo{}
	for _, id := range ids {
		if info, err := s.loadSpace(ctx, id); err == nil {
			out = append(out, *info)
		}
	}
	return out, nil
}

// SetSpace changes the settings of a space (Manage on its top-level category).
func (s *Service) SetSpace(ctx context.Context, id int64, in SpaceInput) (*SpaceInfo, error) {
	if err := s.require(ctx, id, acl.Manage); err != nil {
		return nil, err
	}
	before, err := s.loadSpace(ctx, id)
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return nil, apperr.BadRequest("only top-level categories are spaces")
	}
	if err != nil {
		return nil, err
	}
	sp, err := s.cleanSpace(in.Space)
	if err != nil {
		return nil, err
	}
	sp.Personal = before.Personal // fixed when created
	if sp.IsTemplate != before.IsTemplate && !perm.HasP(identity.From(ctx), "core.categories.admin") {
		return nil, apperr.Forbidden("only category administrators offer spaces as templates")
	}
	key := before.Key
	if in.Key != nil {
		key = strings.ToUpper(strings.TrimSpace(*in.Key))
		if key != "" && !spaceKeyRe.MatchString(key) {
			return nil, apperr.BadRequest("the key has 2–16 letters and digits and starts with a letter (e.g. HR)")
		}
	}
	raw, _ := json.Marshal(sp)
	var keyArg *string
	if key != "" {
		keyArg = &key
	}
	var noted int64
	err = db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE categories SET space_key=$2, space=$3, updated_at=now() WHERE id=$1`, id, keyArg, raw); err != nil {
			return err
		}
		return s.andNote(ctx, tx, &noted, "update", "category", id, audit.Log(ctx, tx, "space.update", "category", id, map[string]any{"key": before.Key, "space": before.Space}, map[string]any{"key": key, "space": sp}))
	})
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return nil, apperr.Conflict("the key %s is already used by another space", key)
		}
		return nil, err
	}
	s.fireNoted(ctx, noted, "update", "category", id)
	return s.loadSpace(ctx, id)
}

func (s *Service) cleanSpace(in Space) (Space, error) {
	out := Space{OwnTree: in.OwnTree, IsTemplate: in.IsTemplate, Shortcuts: []SpaceLink{}, AllowedTypes: []string{}, AllowedMacros: []string{}}
	if len(in.Shortcuts) > maxShortcuts {
		return out, apperr.BadRequest("at most %d shortcuts", maxShortcuts)
	}
	for _, l := range in.Shortcuts {
		u := strings.TrimSpace(l.URL)
		if !safeLink(u) {
			return out, apperr.BadRequest("shortcut %q: use a path of the wiki (/…) or an http(s) address", u)
		}
		label := map[string]string{}
		for lang, v := range l.Label {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if !s.validLang(lang) || utf8.RuneCountInString(v) > 100 {
				return out, apperr.BadRequest("shortcut label: unknown language or longer than 100 characters")
			}
			label[lang] = v
		}
		if len(label) == 0 {
			return out, apperr.BadRequest("shortcut %q needs a label", u)
		}
		out.Shortcuts = append(out.Shortcuts, SpaceLink{Label: label, URL: u})
	}
	for _, t := range in.AllowedTypes {
		if s.Type(t) == nil {
			return out, apperr.BadRequest("unknown or disabled element type %q", t)
		}
		if !slices.Contains(out.AllowedTypes, t) {
			out.AllowedTypes = append(out.AllowedTypes, t)
		}
	}
	if in.Accent != "" {
		if !accentRe.MatchString(in.Accent) {
			return out, apperr.BadRequest("accent: a color like #0b5cad")
		}
		if contrastWithWhite(in.Accent) < 4.5 {
			return out, apperr.BadRequest("accent %s is too light: it needs a contrast of at least 4.5:1 with white (WCAG)", in.Accent)
		}
		out.Accent = strings.ToLower(in.Accent)
	}
	if in.Logo != "" {
		if !logoRe.MatchString(in.Logo) || strings.Contains(in.Logo, "..") {
			return out, apperr.BadRequest("logo: choose a picture of the wiki (files or media library)")
		}
		out.Logo = in.Logo
	}
	for _, m := range in.AllowedMacros {
		if s.macro(m) == nil {
			return out, apperr.BadRequest("unknown or disabled macro %q", m)
		}
		if !slices.Contains(out.AllowedMacros, m) {
			out.AllowedMacros = append(out.AllowedMacros, m)
		}
	}
	if in.Language != "" && !s.validLang(in.Language) {
		return out, apperr.BadRequest("language %q is not enabled", in.Language)
	}
	out.Language = in.Language
	return out, nil
}

// safeLink accepts paths of the wiki and http(s) addresses.
func safeLink(u string) bool {
	if u == "" || len(u) > 2000 || strings.ContainsAny(u, "\x00\r\n\t\\") {
		return false
	}
	if strings.HasPrefix(u, "/") {
		return !strings.HasPrefix(u, "//")
	}
	p, err := url.Parse(u)
	return err == nil && (p.Scheme == "https" || p.Scheme == "http") && p.Host != ""
}

// spaceAllows checks the element types allowed in the space of a category.
func (s *Service) spaceAllows(ctx context.Context, typ string, categoryID int64) error {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT t.space FROM categories c JOIN categories t ON t.id = split_part(c.path::text, '.', 1)::bigint WHERE c.id=$1`, categoryID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // the caller reports the missing category
	}
	if err != nil {
		return err
	}
	var sp Space
	_ = json.Unmarshal(raw, &sp)
	if len(sp.AllowedTypes) > 0 && !slices.Contains(sp.AllowedTypes, typ) {
		return apperr.BadRequest("element type %q is not allowed in this space", typ)
	}
	return nil
}

// spaceLanguage is the language of new elements in a category.
func (s *Service) spaceLanguage(ctx context.Context, categoryID int64) string {
	var lang string
	_ = s.pool.QueryRow(ctx, `SELECT COALESCE(t.space->>'language', '') FROM categories c JOIN categories t ON t.id = split_part(c.path::text, '.', 1)::bigint WHERE c.id=$1`, categoryID).Scan(&lang)
	if lang == "" || !s.validLang(lang) {
		return s.DefaultLanguage()
	}
	return lang
}

// SetArchived archives a category with everything below it (read-only for
// everybody, out of navigation and search) or restores it; needs Manage.
func (s *Service) SetArchived(ctx context.Context, id int64, on bool) (*Category, error) {
	if err := s.require(ctx, id, acl.Manage); err != nil {
		return nil, err
	}
	q := `UPDATE categories SET archived_at = now(), updated_at = now() WHERE id=$1 AND deleted_at IS NULL AND archived_at IS NULL`
	if !on {
		q = `UPDATE categories SET archived_at = NULL, updated_at = now() WHERE id=$1 AND deleted_at IS NULL`
	}
	var noted int64
	changed := false
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, q, id)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		changed = true
		event := "category.archive"
		if !on {
			event = "category.unarchive"
		}
		return s.andNote(ctx, tx, &noted, "update", "category", id, audit.Log(ctx, tx, event, "category", id, nil, nil))
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.aclChanged(ctx)
		s.fireNoted(ctx, noted, "update", "category", id)
	}
	return s.GetCategory(ctx, id)
}

// notArchived refuses changes of the structure below archived categories.
func (s *Service) notArchived(ctx context.Context, categoryID int64) error {
	if categoryID != 0 && s.acl.Archived(ctx, categoryID) {
		return apperr.BadRequest("the category is archived (read-only); restore it first")
	}
	return nil
}

// NewSpaceInput creates a space from a space template.
type NewSpaceInput struct {
	TemplateID int64             `json:"template_id"`
	Titles     map[string]string `json:"titles"`
	Key        string            `json:"key"`
	// WithPages copies the pages too (else only the structure: categories,
	// permissions, settings and the page templates of the space)
	WithPages bool `json:"with_pages"`
}

// CreateSpaceFrom creates a new space from a space template (category
// administrators): structure, permissions, website template, settings and
// page templates of the space; optionally its pages.
func (s *Service) CreateSpaceFrom(ctx context.Context, in NewSpaceInput) (*SpaceInfo, error) {
	tpl, err := s.loadSpace(ctx, in.TemplateID)
	if err != nil || s.require(ctx, in.TemplateID, acl.View) != nil || !tpl.IsTemplate {
		return nil, apperr.NotFound("space template")
	}
	titles, err := s.normalizeTexts(in.Titles, 200, true)
	if err != nil {
		return nil, err
	}
	key := strings.ToUpper(strings.TrimSpace(in.Key))
	if key != "" && !spaceKeyRe.MatchString(key) {
		return nil, apperr.BadRequest("the key has 2–16 letters and digits and starts with a letter (e.g. HR)")
	}
	if key != "" {
		var used bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM categories WHERE space_key=$1 AND deleted_at IS NULL)`, key).Scan(&used); err != nil {
			return nil, err
		}
		if used {
			return nil, apperr.Conflict("the key %s is already used by another space", key)
		}
	}
	c, err := s.duplicateCategory(ctx, in.TemplateID, 0, in.WithPages) // checks core.categories.admin
	if err != nil {
		return nil, err
	}
	sp := tpl.Space
	sp.IsTemplate = false
	_, err = s.UpdateCategory(ctx, c.ID, CategoryInput{Titles: titles, Descriptions: tpl.Descriptions})
	if err == nil {
		_, err = s.SetSpace(ctx, c.ID, SpaceInput{Key: &key, Space: sp})
	}
	if err != nil {
		// do not leave a half-made space behind
		_, _ = s.pool.Exec(ctx, `DELETE FROM categories WHERE path <@ (SELECT path FROM categories WHERE id=$1)`, c.ID)
		s.aclChanged(ctx)
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO element_templates(name, description, type, lang, title, body, tags, space_id, created_by)
		SELECT name, description, type, lang, title, body, tags, $2, $3 FROM element_templates WHERE space_id=$1`, in.TemplateID, c.ID, nullID(ctx)); err != nil {
		return nil, err
	}
	_ = audit.Log(ctx, s.pool, "space.create", "category", c.ID, nil, map[string]any{"template": in.TemplateID, "key": key, "pages": in.WithPages})
	return s.loadSpace(ctx, c.ID)
}

type personalSpaceKey struct{}

// CreatePersonalSpace creates the personal space of the caller (permission
// core.spaces.personal; one per person) or returns the existing one. Only
// its owner has permissions until they share it.
func (s *Service) CreatePersonalSpace(ctx context.Context) (*SpaceInfo, error) {
	p := identity.From(ctx)
	if p == nil || p.ID() <= 0 {
		return nil, apperr.ErrUnauthorized
	}
	if err := perm.Require(ctx, "core.spaces.personal"); err != nil {
		return nil, err
	}
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM categories WHERE parent_id IS NULL AND deleted_at IS NULL AND space->>'personal' = $1`, strconv.FormatInt(p.ID(), 10)).Scan(&id)
	if err == nil {
		return s.loadSpace(ctx, id)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	name := p.DisplayName
	if name == "" {
		name = p.Username
	}
	titles := map[string]string{}
	for _, l := range s.Languages() {
		titles[l] = name
	}
	c, err := s.CreateCategory(context.WithValue(ctx, personalSpaceKey{}, true), CategoryInput{Titles: titles})
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(Space{OwnTree: true, Personal: p.ID(), Shortcuts: []SpaceLink{}, AllowedTypes: []string{}})
	if _, err := s.pool.Exec(ctx, `UPDATE categories SET space=$2 WHERE id=$1`, c.ID, raw); err != nil {
		_, _ = s.pool.Exec(ctx, `DELETE FROM categories WHERE id=$1`, c.ID)
		return nil, err
	}
	_ = audit.Log(ctx, s.pool, "space.personal", "category", c.ID, nil, nil)
	return s.loadSpace(ctx, c.ID)
}

// spaceMacros returns the macros allowed in the space of a category (nil: all).
func (s *Service) spaceMacros(ctx context.Context, categoryID int64) []string {
	if categoryID == 0 {
		return nil
	}
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT t.space->'allowed_macros' FROM categories c JOIN categories t ON t.id = split_part(c.path::text, '.', 1)::bigint WHERE c.id=$1`, categoryID).Scan(&raw); err != nil {
		return nil
	}
	var list []string
	_ = json.Unmarshal(raw, &list)
	return list
}
