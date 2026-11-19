package content

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// Placement puts a building block of the interface (search, notifications,
// the category tree, a button of a plugin …) at a position of the template.
// An empty position hides the block at this level. The placements of a
// level replace those of the levels above for the same block: template,
// site, category (inherited by subcategories), category page (one category,
// only the category positions), instance (one element: only the element
// positions), user.
type Placement struct {
	Block    string `json:"block"`
	Position string `json:"position"`
	Sort     int    `json:"sort"`
	// Required: lower levels can neither move nor remove the block
	// (template and site only).
	Required bool `json:"required,omitempty"`
	// Options: how this placed block looks and when it is shown (checked
	// values of blockoptions.json; the CSS is built from them)
	Options map[string]any `json:"options,omitempty"`
}

// DashboardPositionPrefix starts the positions of the dashboard (dashboard-wide
// over the full width, dashboard-1 … the columns); everybody arranges the own
// dashboard, also when personal placement is switched off.
const DashboardPositionPrefix = "dashboard-"

// PlacementLevel is the placements of one level and target.
type PlacementLevel struct {
	Level      string      `json:"level"`
	Target     string      `json:"target"`
	Placements []Placement `json:"placements"`
	UpdatedAt  *time.Time  `json:"updated_at,omitempty"`
}

// Placement levels.
const (
	LevelTemplate = "template"
	LevelSite     = "site"
	LevelCategory = "category"
	LevelInstance = "instance"
	// LevelCategoryPage arranges the page of one category (not inherited by
	// the subcategories): its blocks and featured pages
	LevelCategoryPage = "category_page"
	LevelUser         = "user"
)

// CategoryPositionPrefix starts the positions of a category page
// (category-header, category-toolbar, category-content, category-aside,
// category-footer); the level of one category page places blocks only
// there.
const CategoryPositionPrefix = "category-"

// FeaturedBlockPrefix starts the block of a featured page of a category
// ("core.category.element/<element id>"): the page shown in full on the
// category page.
const FeaturedBlockPrefix = "core.category.element/"

// ElementPositionPrefix starts the positions of an element page
// (element-header, element-toolbar, element-aside, element-footer); the
// instance level places blocks only there.
const ElementPositionPrefix = "element-"

var (
	// a block, optionally one instance of it ("core.menu/12")
	blockRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}\.[a-z0-9_-]{1,40}(/[a-z0-9_-]{1,40})?$`)
	positionRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)
)

// pluginOf is the plugin of a block or template id ("workflow.process.tasks"
// → "workflow.process"; "core.search" → "core").
func pluginOf(id string) string {
	if i := strings.IndexByte(id, '/'); i > 0 {
		id = id[:i] // an instance of a block
	}
	if strings.HasPrefix(id, "core.") {
		return "core"
	}
	if i := strings.LastIndexByte(id, '.'); i > 0 {
		return id[:i]
	}
	return id
}

// RemovePlacementsTx removes the placements of a block (e.g. of a deleted
// instance "core.menu/12"), in the transaction that deletes it.
func RemovePlacementsTx(ctx context.Context, tx pgx.Tx, block string) error {
	_, err := tx.Exec(ctx, `DELETE FROM placements WHERE block=$1`, block)
	return err
}

// knownOwner: the core or an enabled plugin (templates are plugins too).
func (s *Service) knownOwner(plugin string) bool {
	return plugin == "core" || (s.enabled != nil && s.enabled(plugin))
}

// ResolvedPlacements returns the levels that apply to a page: the template
// (templateID, or the one of the site), the site, the category and its
// ancestors (from the top; only categories the caller may view), the
// element shown (if the caller may view it) and – if personal placement is
// switched on – the caller's own. The interface
// applies them in this order to the blocks it knows.
func (s *Service) ResolvedPlacements(ctx context.Context, templateID string, categoryID, elementID int64) ([]PlacementLevel, error) {
	type key struct{ level, target string }
	want := []key{}
	if templateID == "" {
		templateID = s.cfg.Get().UI.Template
	}
	if templateID != "" {
		want = append(want, key{LevelTemplate, templateID})
	}
	want = append(want, key{LevelSite, ""})
	if categoryID > 0 {
		if c, err := s.loadCategory(ctx, s.pool, categoryID, false); err == nil && s.acl.Can(ctx, identity.From(ctx), categoryID, acl.View) {
			for _, id := range c.Path {
				want = append(want, key{LevelCategory, strconv.FormatInt(id, 10)})
			}
			// the page of this category (on element pages its positions do not exist)
			want = append(want, key{LevelCategoryPage, strconv.FormatInt(categoryID, 10)})
		}
	}
	if elementID > 0 {
		if _, err := s.ElementCategory(ctx, elementID, acl.View); err == nil {
			want = append(want, key{LevelInstance, strconv.FormatInt(elementID, 10)})
		}
	}
	// the own level: always (the own dashboard); without personal placement
	// the interface applies only its tiles
	p := identity.From(ctx)
	if p != nil && p.UserID > 0 {
		want = append(want, key{LevelUser, strconv.FormatInt(p.UserID, 10)})
	}
	levels := make([]string, len(want))
	targets := make([]string, len(want))
	for i, k := range want {
		levels[i], targets[i] = k.level, k.target
	}
	rows, err := s.pool.Query(ctx, `SELECT p.level, p.target, p.block, p.position, p.sort, p.required, p.options
		FROM placements p JOIN unnest($1::text[], $2::text[]) AS w(level, target) ON w.level = p.level AND w.target = p.target
		ORDER BY p.sort, p.id`, levels, targets)
	if err != nil {
		return nil, err
	}
	byKey := map[key][]Placement{}
	for rows.Next() {
		var k key
		var pl Placement
		if err := rows.Scan(&k.level, &k.target, &pl.Block, &pl.Position, &pl.Sort, &pl.Required, &pl.Options); err != nil {
			rows.Close()
			return nil, err
		}
		byKey[k] = append(byKey[k], pl)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []PlacementLevel{}
	for _, k := range want {
		if pls := byKey[k]; len(pls) > 0 {
			out = append(out, PlacementLevel{Level: k.level, Target: k.target, Placements: pls})
		}
	}
	return out, nil
}

// requirePlacementLevel checks who may change a level: template and site
// need the permission to change settings, a category its Manage permission,
// a user level only the person and only with personal placement on.
func (s *Service) requirePlacementLevel(ctx context.Context, level, target string) error {
	p := identity.From(ctx)
	if p == nil {
		return apperr.ErrUnauthorized
	}
	switch level {
	case LevelTemplate:
		// templates are plugins (theme.wiki …) or offered by a plugin (core.template/12)
		known := target == "core.basic" || s.knownOwner(target) || s.TemplateOK(ctx, target)
		if !blockRe.MatchString(target) || !known {
			return apperr.BadRequest("unknown template %q", target)
		}
		// the default placements of a template of data belong to its design
		if strings.Contains(target, "/") && perm.Has(ctx, "core.template.design") {
			return nil
		}
		return perm.Require(ctx, "core.settings.manage")
	case LevelSite:
		if target != "" {
			return apperr.BadRequest("the site level has no target")
		}
		return perm.Require(ctx, "core.settings.manage")
	case LevelCategory:
		id, err := strconv.ParseInt(target, 10, 64)
		if err != nil || id <= 0 {
			return apperr.BadRequest("target must be a category id")
		}
		if _, err := s.loadCategory(ctx, s.pool, id, false); err != nil {
			return err
		}
		return s.require(ctx, id, acl.Manage)
	case LevelCategoryPage:
		id, err := strconv.ParseInt(target, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != target {
			return apperr.BadRequest("target must be a category id")
		}
		if _, err := s.loadCategory(ctx, s.pool, id, false); err != nil {
			return err
		}
		return s.require(ctx, id, acl.Manage)
	case LevelInstance:
		id, err := strconv.ParseInt(target, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != target {
			return apperr.BadRequest("target must be an element id")
		}
		return s.requireArrange(ctx, id)
	case LevelUser:
		if target != strconv.FormatInt(p.UserID, 10) || p.UserID <= 0 {
			return apperr.Forbidden("only the own placements can be changed")
		}
		return nil // without personal placement only the dashboard (SetPlacements)
	}
	return apperr.BadRequest("level must be template, site, category, category_page, instance or user")
}

// requireArrange checks who may arrange one element page: for elements of a
// type with permissions of its own (a blog, a FAQ …) the permission to
// manage the instance, for all others the permission to edit the element.
func (s *Service) requireArrange(ctx context.Context, id int64) error {
	cat, err := s.ElementCategory(ctx, id, acl.View)
	if err != nil {
		return err
	}
	var typ string
	// acl: checked above (ElementCategory)
	if err := s.pool.QueryRow(ctx, `SELECT type FROM elements WHERE id=$1`, id).Scan(&typ); err != nil {
		return apperr.NotFound("element")
	}
	if len(perm.InstanceKeys(typ)) == 0 {
		return s.requireOn(ctx, id, cat, acl.Edit)
	}
	mk := manageKey(typ)
	mine, err := s.acl.InstanceActions(ctx, identity.From(ctx), acl.Instance{ID: id, Type: typ, CategoryID: cat})
	if err != nil {
		return err
	}
	if mk == "" || !slices.Contains(mine, mk) {
		return apperr.Forbidden("arranging this page needs the permission to manage it")
	}
	return nil
}

// GetPlacements returns the placements of one level (for editing).
func (s *Service) GetPlacements(ctx context.Context, level, target string) (*PlacementLevel, error) {
	if err := s.requirePlacementLevel(ctx, level, target); err != nil {
		return nil, err
	}
	out := &PlacementLevel{Level: level, Target: target, Placements: []Placement{}}
	rows, err := s.pool.Query(ctx, `SELECT block, position, sort, required, options, updated_at FROM placements WHERE level=$1 AND target=$2 ORDER BY sort, id`, level, target)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pl Placement
		var at time.Time
		if err := rows.Scan(&pl.Block, &pl.Position, &pl.Sort, &pl.Required, &pl.Options, &at); err != nil {
			return nil, err
		}
		if out.UpdatedAt == nil || at.After(*out.UpdatedAt) {
			out.UpdatedAt = &at
		}
		out.Placements = append(out.Placements, pl)
	}
	return out, rows.Err()
}

// requiredBlocks are the blocks that the template of the site and the site
// itself made required: lower levels cannot change them.
func (s *Service) requiredBlocks(ctx context.Context) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT block FROM placements WHERE required AND (level='site' OR (level='template' AND target=$1))`, s.cfg.Get().UI.Template)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			rows.Close()
			return nil, err
		}
		out[b] = true
	}
	rows.Close()
	return out, rows.Err()
}

// SetPlacements replaces the placements of one level.
func (s *Service) SetPlacements(ctx context.Context, level, target string, in []Placement) (*PlacementLevel, error) {
	if err := s.requirePlacementLevel(ctx, level, target); err != nil {
		return nil, err
	}
	if max := limits.Value(s.cfg.Get(), limits.PlacementRows); int64(len(in)) > max {
		return nil, apperr.BadRequest("at most %d placements per level (expert setting %s)", max, limits.PlacementRows)
	}
	var required map[string]bool
	if level == LevelCategory || level == LevelCategoryPage || level == LevelInstance || level == LevelUser {
		var err error
		if required, err = s.requiredBlocks(ctx); err != nil {
			return nil, err
		}
	}
	ui := s.cfg.Get().UI
	seen := map[string]bool{}
	anchors := map[string]bool{}
	for i, pl := range in {
		if !blockRe.MatchString(pl.Block) || !s.knownOwner(pluginOf(pl.Block)) {
			return nil, apperr.BadRequest("placement %d: unknown block %q", i+1, pl.Block)
		}
		if pl.Position != "" && !positionRe.MatchString(pl.Position) {
			return nil, apperr.BadRequest("placement %d: invalid position %q", i+1, pl.Position)
		}
		if level == LevelInstance && pl.Position != "" && !strings.HasPrefix(pl.Position, ElementPositionPrefix) {
			return nil, apperr.BadRequest("placement %d: one page places blocks only at the positions of the page (%s…)", i+1, ElementPositionPrefix)
		}
		if level == LevelCategoryPage && pl.Position != "" && !strings.HasPrefix(pl.Position, CategoryPositionPrefix) {
			return nil, apperr.BadRequest("placement %d: one category page places blocks only at the positions of the page (%s…)", i+1, CategoryPositionPrefix)
		}
		if strings.HasPrefix(pl.Block, FeaturedBlockPrefix) {
			if level != LevelCategoryPage {
				return nil, apperr.BadRequest("placement %d: featured pages belong to the page of one category (level %s)", i+1, LevelCategoryPage)
			}
			if _, err := strconv.ParseInt(strings.TrimPrefix(pl.Block, FeaturedBlockPrefix), 10, 64); err != nil {
				return nil, apperr.BadRequest("placement %d: %q names no page", i+1, pl.Block)
			}
		}
		dash := strings.HasPrefix(pl.Position, DashboardPositionPrefix)
		if level == LevelUser && !ui.PersonalPlacement && pl.Position != "" && !dash {
			return nil, apperr.BadRequest("placement %d: personal placement is switched off (ui.personal_placement): only the own dashboard (%s…)", i+1, DashboardPositionPrefix)
		}
		if level == LevelUser && dash && len(ui.DashboardTiles) > 0 && !slices.Contains(ui.DashboardTiles, pl.Block) {
			return nil, apperr.BadRequest("placement %d: the tile %q is not offered (ui.dashboard_tiles)", i+1, pl.Block)
		}
		if pl.Sort < 0 || pl.Sort > 10000 {
			return nil, apperr.BadRequest("placement %d: sort must be between 0 and 10000", i+1)
		}
		if pl.Required && level != LevelTemplate && level != LevelSite {
			return nil, apperr.BadRequest("placement %d: only the template and the site make blocks required", i+1)
		}
		if err := CheckBlockOptions(pl.Options); err != nil {
			return nil, apperr.BadRequest("placement %d (%s): %s", i+1, pl.Block, err.Error())
		}
		if pl.Position == "" && len(pl.Options) > 0 {
			return nil, apperr.BadRequest("placement %d: a hidden block has no options", i+1)
		}
		if a, _ := pl.Options["anchor"].(string); a != "" {
			if anchors[a] {
				return nil, apperr.BadRequest("placement %d: the anchor %q is used twice", i+1, a)
			}
			anchors[a] = true
		}
		if required[pl.Block] {
			return nil, apperr.BadRequest("placement %d: the block %q is required by the administration and cannot be changed here", i+1, pl.Block)
		}
		k := pl.Block + "@" + pl.Position
		if seen[k] {
			return nil, apperr.BadRequest("placement %d: %q is placed twice at %q", i+1, pl.Block, pl.Position)
		}
		seen[k] = true
		if pl.Position == "" && slices.ContainsFunc(in, func(x Placement) bool { return x.Block == pl.Block && x.Position != "" }) {
			return nil, apperr.BadRequest("placement %d: %q is hidden and placed at the same time", i+1, pl.Block)
		}
	}
	if err := s.checkFeatured(ctx, in); err != nil {
		return nil, err
	}
	before, err := s.GetPlacements(ctx, level, target)
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// one change of a level at a time
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('placements:' || $1 || ':' || $2))`, level, target); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM placements WHERE level=$1 AND target=$2`, level, target); err != nil {
			return err
		}
		for _, pl := range in {
			if _, err := tx.Exec(ctx, `INSERT INTO placements(level, target, block, position, sort, required, options, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
				level, target, pl.Block, pl.Position, pl.Sort, pl.Required, optionsJSON(pl.Options), nullID(ctx)); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, "placements.set", "placements", 0, before, PlacementLevel{Level: level, Target: target, Placements: in})
	})
	if err != nil {
		return nil, err
	}
	return s.GetPlacements(ctx, level, target)
}

// ValidBlockID reports whether id has the form of a block id
// (plugin-id.name, optionally /instance).
func ValidBlockID(id string) bool { return blockRe.MatchString(id) }

// checkFeatured checks the featured pages of a category page: each must be
// a page the caller may view (all at once). Readers see a featured page only
// when they may view it too (it is loaded with their permissions).
func (s *Service) checkFeatured(ctx context.Context, in []Placement) error {
	ids := []int64{}
	for _, pl := range in {
		if strings.HasPrefix(pl.Block, FeaturedBlockPrefix) && pl.Position != "" {
			id, _ := strconv.ParseInt(strings.TrimPrefix(pl.Block, FeaturedBlockPrefix), 10, 64)
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if max := limits.Value(s.cfg.Get(), limits.CategoryFeatured); int64(len(ids)) > max {
		return apperr.BadRequest("at most %d featured pages (expert setting %s)", max, limits.CategoryFeatured)
	}
	vis, err := s.VisibleElements(ctx, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !vis[id] {
			return apperr.BadRequest("featured page %d: not found", id)
		}
	}
	return nil
}

// optionsJSON is the options of a placement as stored ({} when none).
func optionsJSON(o map[string]any) []byte {
	if len(o) == 0 {
		return []byte("{}")
	}
	b, _ := json.Marshal(o)
	return b
}
