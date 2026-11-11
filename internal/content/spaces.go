package content

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Spaces: every top-level category is a space (like a Confluence space).
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
	// Language of new elements ("" = the default language of the site)
	Language string `json:"language"`
}

// SpaceInfo is a space with its category texts.
type SpaceInfo struct {
	ID           int64             `json:"id"`
	Key          string            `json:"key"`
	Titles       map[string]string `json:"titles"`
	Descriptions map[string]string `json:"descriptions"`
	Space
}

// SpaceInput changes a space (nil fields stay).
type SpaceInput struct {
	Key *string `json:"key"`
	Space
}

var spaceKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,15}$`)

const maxShortcuts = 30

func (s *Service) loadSpace(ctx context.Context, top int64) (*SpaceInfo, error) {
	info := &SpaceInfo{ID: top}
	var key *string
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT space_key, titles, descriptions, space FROM categories WHERE id=$1 AND parent_id IS NULL AND deleted_at IS NULL`, top).
		Scan(&key, &info.Titles, &info.Descriptions, &raw)
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
	if info.Shortcuts == nil {
		info.Shortcuts = []SpaceLink{}
	}
	if info.AllowedTypes == nil {
		info.AllowedTypes = []string{}
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
	if _, err := s.pool.Exec(ctx, `UPDATE categories SET space_key=$2, space=$3, updated_at=now() WHERE id=$1`, id, keyArg, raw); err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return nil, apperr.Conflict("the key %s is already used by another space", key)
		}
		return nil, err
	}
	_ = audit.Log(ctx, s.pool, "space.update", "category", id, map[string]any{"key": before.Key, "space": before.Space}, map[string]any{"key": key, "space": sp})
	s.fire(ctx, "update", "category", id)
	return s.loadSpace(ctx, id)
}

func (s *Service) cleanSpace(in Space) (Space, error) {
	out := Space{OwnTree: in.OwnTree, Shortcuts: []SpaceLink{}, AllowedTypes: []string{}}
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
