package content

import (
	"context"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// SearchHit is an element found by the full text search.
type SearchHit struct {
	ElementID  int64    `json:"element_id"`
	CategoryID int64    `json:"category_id"`
	Lang       string   `json:"lang"`
	Type       string   `json:"type"`
	Title      string   `json:"title"`
	Snippet    string   `json:"snippet"` // HTML-escaped, matches wrapped in <mark>
	Tags       []string `json:"tags"`
	Rank       float32  `json:"rank"`
}

// CategoryHit is a category found by title or tag.
type CategoryHit struct {
	ID     int64             `json:"id"`
	Titles map[string]string `json:"titles"`
	Tags   []string          `json:"tags"`
}

// SearchResult is the result of Search.
type SearchResult struct {
	Elements   []SearchHit   `json:"elements"`
	Categories []CategoryHit `json:"categories"`
	// Engine that found the elements: postgres or the name of a search backend.
	Engine string `json:"engine"`
}

// buildTSQuery turns user input into a safe prefix tsquery: "foo ba" -> "foo:* & ba:*".
func buildTSQuery(q string) string {
	var terms []string
	for _, f := range strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		if len(terms) >= 10 {
			break
		}
		terms = append(terms, strings.ToLower(f)+":*")
	}
	return strings.Join(terms, " & ")
}

// Search runs a full text search over all element languages plus category
// titles, filtered by tags and by the caller's view permission.
func (s *Service) Search(ctx context.Context, q string, tags []string, lang string, limit int) (*SearchResult, error) {
	return s.SearchIn(ctx, q, tags, lang, limit, nil)
}

// SearchIn is Search limited to the given categories (nil = everywhere).
// Scoped searches always use PostgreSQL.
func (s *Service) SearchIn(ctx context.Context, q string, tags []string, lang string, limit int, scope []int64) (*SearchResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	tags, err := NormalizeTags(tags)
	if err != nil {
		return nil, err
	}
	tsq := buildTSQuery(q)
	if tsq == "" && len(tags) == 0 {
		return nil, apperr.BadRequest("enter a search term or a tag")
	}
	p := identity.From(ctx)
	res := &SearchResult{Elements: []SearchHit{}, Categories: []CategoryHit{}, Engine: "postgres"}
	var rows pgx.Rows
	if b := s.searchBackend(ctx); b != nil && strings.TrimSpace(q) != "" && scope == nil {
		if err := s.searchExternal(ctx, b, q, tags, lang, limit, res); err == nil {
			res.Engine = b.Name()
			return s.searchCategories(ctx, q, tags, res, nil)
		} else {
			s.log.Warn("external search failed, using PostgreSQL", "engine", b.Name(), "err", err)
			res.Elements = []SearchHit{}
		}
	}
	if tsq != "" {
		rows, err = s.pool.Query(ctx, `SELECT d.element_id, d.category_id, d.lang, e.type, d.title,
				ts_headline('simple', left(d.body_text, 20000), q, 'StartSel=⟦, StopSel=⟧, MaxFragments=2, MaxWords=24, MinWords=8, FragmentDelimiter=" … "'),
				e.tags, ts_rank(d.tsv, q)
			FROM search_docs d JOIN elements e ON e.id = d.element_id, to_tsquery('simple', $1) q
			WHERE d.tsv @@ q AND e.deleted_at IS NULL AND (cardinality($2::text[]) = 0 OR e.tags @> $2)
				AND ($4::bigint[] IS NULL OR d.category_id = ANY($4))
				AND ($4::bigint[] IS NOT NULL OR NOT EXISTS (SELECT 1 FROM categories hc JOIN categories hx ON hx.id = d.category_id WHERE (hc.hidden OR hc.archived_at IS NOT NULL OR hc.classification >= 3) AND hx.path <@ hc.path)) -- hidden categories only when searched explicitly
			ORDER BY ts_rank(d.tsv, q) DESC, (d.lang = $3) DESC LIMIT 1000`, tsq, tags, lang, scope)
	} else {
		rows, err = s.pool.Query(ctx, `SELECT d.element_id, d.category_id, d.lang, e.type, d.title, left(d.body_text, 200), e.tags, 1::real
			FROM search_docs d JOIN elements e ON e.id = d.element_id
			WHERE e.deleted_at IS NULL AND e.tags @> $1 AND ($3::bigint[] IS NULL OR d.category_id = ANY($3))
				AND ($3::bigint[] IS NOT NULL OR NOT EXISTS (SELECT 1 FROM categories hc JOIN categories hx ON hx.id = d.category_id WHERE (hc.hidden OR hc.archived_at IS NOT NULL OR hc.classification >= 3) AND hx.path <@ hc.path))
			ORDER BY e.updated_at DESC, (d.lang = $2) DESC LIMIT 1000`, tags, lang, scope)
	}
	if err != nil {
		return nil, err
	}
	hits, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SearchHit, error) {
		var h SearchHit
		return h, r.Scan(&h.ElementID, &h.CategoryID, &h.Lang, &h.Type, &h.Title, &h.Snippet, &h.Tags, &h.Rank)
	})
	if err != nil {
		return nil, err
	}
	cats := make([]int64, 0, len(hits))
	for _, h := range hits {
		cats = append(cats, h.CategoryID)
	}
	perms, err := s.acl.PermsMap(ctx, p, cats)
	if err != nil {
		return nil, err
	}
	best := map[int64]int{} // element -> index in res
	for _, h := range hits {
		if perms[h.CategoryID]&acl.View == 0 {
			continue
		}
		h.Snippet = strings.NewReplacer("⟦", "<mark>", "⟧", "</mark>").Replace(htmlEscape(h.Snippet))
		if i, ok := best[h.ElementID]; ok {
			if h.Lang == lang && res.Elements[i].Lang != lang {
				res.Elements[i] = h
			}
			continue
		}
		if len(res.Elements) >= limit {
			continue
		}
		best[h.ElementID] = len(res.Elements)
		res.Elements = append(res.Elements, h)
	}
	return s.searchCategories(ctx, q, tags, res, scope)
}

// searchCategories adds categories matching by title or tag.
func (s *Service) searchCategories(ctx context.Context, q string, tags []string, res *SearchResult, scope []int64) (*SearchResult, error) {
	p := identity.From(ctx)
	like := "%" + strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(q), "%", ""), "_", "") + "%"
	crow, err := s.pool.Query(ctx, `SELECT id, titles, tags FROM categories WHERE deleted_at IS NULL AND (
			($1 <> '%%' AND EXISTS (SELECT 1 FROM jsonb_each_text(titles) t WHERE t.value ILIKE $1))
			OR (cardinality($2::text[]) > 0 AND tags @> $2)) AND ($3::bigint[] IS NULL OR id = ANY($3))
			AND ($3::bigint[] IS NOT NULL OR NOT EXISTS (SELECT 1 FROM categories hc WHERE (hc.hidden OR hc.archived_at IS NOT NULL OR hc.classification >= 3) AND categories.path <@ hc.path))
			ORDER BY nlevel(path), position LIMIT 200`, like, tags, scope)
	if err != nil {
		return nil, err
	}
	chits, err := pgx.CollectRows(crow, func(r pgx.CollectableRow) (CategoryHit, error) {
		var c CategoryHit
		return c, r.Scan(&c.ID, &c.Titles, &c.Tags)
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(chits))
	for i, c := range chits {
		ids[i] = c.ID
	}
	cperm, err := s.acl.PermsMap(ctx, p, ids)
	if err != nil {
		return nil, err
	}
	for _, c := range chits {
		if cperm[c.ID]&acl.View != 0 && len(res.Categories) < 20 {
			res.Categories = append(res.Categories, c)
		}
	}
	return res, nil
}

// TagCount is a tag with its usage count.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// Tags lists tags (optionally by prefix) used in visible content.
func (s *Service) Tags(ctx context.Context, prefix string, limit int) ([]TagCount, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT t.tag, t.cat FROM (
			SELECT unnest(tags) AS tag, category_id AS cat FROM elements WHERE deleted_at IS NULL
			UNION ALL SELECT unnest(tags), id FROM categories WHERE deleted_at IS NULL) t
		WHERE t.tag LIKE $1 || '%'`, strings.ToLower(strings.TrimSpace(prefix)))
	if err != nil {
		return nil, err
	}
	type tc struct {
		tag string
		cat int64
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (tc, error) {
		var x tc
		return x, r.Scan(&x.tag, &x.cat)
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(all))
	for i, x := range all {
		ids[i] = x.cat
	}
	perms, err := s.acl.PermsMap(ctx, identity.From(ctx), ids)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, x := range all {
		if perms[x.cat]&acl.View != 0 {
			counts[x.tag]++
		}
	}
	out := make([]TagCount, 0, len(counts))
	for t, c := range counts {
		out = append(out, TagCount{t, c})
	}
	// sort by count desc, then name
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && (out[j].Count > out[j-1].Count || out[j].Count == out[j-1].Count && out[j].Tag < out[j-1].Tag); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------------------------------------------------------------- ACL management

// ACLView is the permission configuration of a category.
type ACLView struct {
	Own       []acl.Rule `json:"own"`
	Inherited []acl.Rule `json:"inherited"`
}

// GetACL returns the rules of a category (requires manage).
func (s *Service) GetACL(ctx context.Context, id int64) (*ACLView, error) {
	if err := s.require(ctx, id, acl.Manage); err != nil {
		return nil, err
	}
	own, inh, err := s.acl.Rules(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ACLView{own, inh}, nil
}

// RuleInput adds or changes a rule.
type RuleInput struct {
	SubjectType string   `json:"subject_type"`
	SubjectID   *int64   `json:"subject_id"`
	Permissions acl.Perm `json:"permissions"`
	// Actions are named category permissions of plugins.
	Actions []string `json:"actions"`
	// Preset (reader, editor, manager) sets permissions and actions.
	Preset  string `json:"preset"`
	Inherit *bool  `json:"inherit"`
}

// PresetPerms returns the core bits and named actions of a preset.
func PresetPerms(name string) (acl.Perm, []string, bool) {
	switch name {
	case perm.Reader:
		return acl.View, perm.Preset(perm.Reader), true
	case perm.Editor:
		return acl.View | acl.Edit | acl.Create | acl.Delete, perm.Preset(perm.Editor), true
	case perm.Manager:
		return acl.All, perm.Preset(perm.Manager), true
	}
	return 0, nil, false
}

// checkGrant validates permissions a caller wants to grant on a category.
// Who is neither administrator nor category administrator cannot grant
// more than they have themselves.
func (s *Service) checkGrant(ctx context.Context, categoryID int64, in *RuleInput) error {
	if in.Preset != "" {
		bits, acts, ok := PresetPerms(in.Preset)
		if !ok {
			return apperr.BadRequest("preset must be reader, editor or manager")
		}
		in.Permissions, in.Actions = bits, acts
	}
	if in.Actions == nil {
		in.Actions = []string{}
	}
	if in.Permissions < 0 || in.Permissions > acl.All || (in.Permissions == 0 && len(in.Actions) == 0) {
		return apperr.BadRequest("permissions must be between 1 and %d", acl.All)
	}
	if err := perm.Validate(perm.Category, in.Actions); err != nil {
		return err
	}
	p := identity.From(ctx)
	if p.Admin() || perm.HasP(p, "core.categories.admin") {
		return nil
	}
	mine, _ := s.acl.Perms(ctx, p, categoryID)
	if in.Permissions&^mine != 0 {
		return apperr.BadRequest("you cannot grant permissions you do not have")
	}
	acts, _ := s.acl.Actions(ctx, p, categoryID)
	for _, a := range in.Actions {
		if !slices.Contains(acts, a) {
			return apperr.BadRequest("you cannot grant the permission %s, which you do not have", a)
		}
	}
	return nil
}

// AddRule adds a permission rule to a category (requires manage). Users
// cannot grant more than they have themselves (except admins).
func (s *Service) AddRule(ctx context.Context, categoryID int64, in RuleInput) (*acl.Rule, error) {
	if err := s.require(ctx, categoryID, acl.Manage); err != nil {
		return nil, err
	}
	if err := s.checkGrant(ctx, categoryID, &in); err != nil {
		return nil, err
	}
	switch in.SubjectType {
	case "guest", "authenticated":
		in.SubjectID = nil
	case "user", "group", "role":
		if in.SubjectID == nil {
			return nil, apperr.BadRequest("subject_id is required")
		}
		table := map[string]string{"user": "users", "group": "groups", "role": "roles"}[in.SubjectType]
		var ok bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=$1)`, *in.SubjectID).Scan(&ok); err != nil || !ok {
			return nil, apperr.BadRequest("%s %d does not exist", in.SubjectType, *in.SubjectID)
		}
	default:
		return nil, apperr.BadRequest("subject_type must be user, group, role, guest or authenticated")
	}
	if in.SubjectType == "guest" && (in.Permissions&^acl.View != 0 || len(in.Actions) > 0) {
		return nil, apperr.BadRequest("guests can only be granted view permission")
	}
	inherit := true
	if in.Inherit != nil {
		inherit = *in.Inherit
	}
	r := &acl.Rule{CategoryID: categoryID, SubjectType: in.SubjectType, SubjectID: in.SubjectID, Permissions: in.Permissions, Actions: in.Actions, Inherit: inherit}
	err := s.pool.QueryRow(ctx, `INSERT INTO acl_rules(category_id, subject_type, subject_id, permissions, actions, inherit, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, categoryID, r.SubjectType, r.SubjectID, r.Permissions, r.Actions, r.Inherit, nullID(ctx)).Scan(&r.ID)
	if err != nil {
		return nil, err
	}
	_ = auditLog(ctx, s, "acl.add", "category", categoryID, nil, r)
	s.aclChanged(ctx)
	return r, nil
}

// UpdateRule changes the permissions of a rule (requires manage).
func (s *Service) UpdateRule(ctx context.Context, ruleID int64, in RuleInput) (*acl.Rule, error) {
	var r acl.Rule
	err := s.pool.QueryRow(ctx, `SELECT id, category_id, subject_type, subject_id, permissions, actions, inherit FROM acl_rules WHERE id=$1`, ruleID).
		Scan(&r.ID, &r.CategoryID, &r.SubjectType, &r.SubjectID, &r.Permissions, &r.Actions, &r.Inherit)
	if err != nil {
		return nil, apperr.NotFound("rule")
	}
	if err := s.require(ctx, r.CategoryID, acl.Manage); err != nil {
		return nil, err
	}
	if err := s.checkGrant(ctx, r.CategoryID, &in); err != nil {
		return nil, err
	}
	if r.SubjectType == "guest" && (in.Permissions&^acl.View != 0 || len(in.Actions) > 0) {
		return nil, apperr.BadRequest("guests can only be granted view permission")
	}
	before := r
	r.Permissions, r.Actions = in.Permissions, in.Actions
	if in.Inherit != nil {
		r.Inherit = *in.Inherit
	}
	if _, err := s.pool.Exec(ctx, `UPDATE acl_rules SET permissions=$2, actions=$3, inherit=$4 WHERE id=$1`, r.ID, r.Permissions, r.Actions, r.Inherit); err != nil {
		return nil, err
	}
	_ = auditLog(ctx, s, "acl.update", "category", r.CategoryID, before, r)
	s.aclChanged(ctx)
	return &r, nil
}

// DeleteRule removes a rule (requires manage on its category).
func (s *Service) DeleteRule(ctx context.Context, ruleID int64) error {
	var r acl.Rule
	err := s.pool.QueryRow(ctx, `SELECT id, category_id, subject_type, subject_id, permissions, actions, inherit FROM acl_rules WHERE id=$1`, ruleID).
		Scan(&r.ID, &r.CategoryID, &r.SubjectType, &r.SubjectID, &r.Permissions, &r.Actions, &r.Inherit)
	if err != nil {
		return apperr.NotFound("rule")
	}
	if err := s.require(ctx, r.CategoryID, acl.Manage); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM acl_rules WHERE id=$1`, ruleID); err != nil {
		return err
	}
	_ = auditLog(ctx, s, "acl.delete", "category", r.CategoryID, r, nil)
	s.aclChanged(ctx)
	return nil
}

// RecentItem is a recently changed element.
type RecentItem struct {
	ID         int64             `json:"id"`
	CategoryID int64             `json:"category_id"`
	Titles     map[string]string `json:"titles"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// Recent lists recently changed elements the caller can see (optionally
// within the subtree of category).
func (s *Service) Recent(ctx context.Context, category int64, limit int) ([]RecentItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id, e.category_id, e.titles, e.updated_at FROM elements e JOIN categories c ON c.id=e.category_id
		WHERE e.deleted_at IS NULL AND c.deleted_at IS NULL
		AND ($1 = 0 OR c.path <@ (SELECT path FROM categories WHERE id=$1))
		ORDER BY e.updated_at DESC LIMIT 500`, category)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (RecentItem, error) {
		var x RecentItem
		return x, r.Scan(&x.ID, &x.CategoryID, &x.Titles, &x.UpdatedAt)
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(items))
	for i, x := range items {
		ids[i] = x.CategoryID
	}
	perms, err := s.acl.PermsMap(ctx, identity.From(ctx), ids)
	if err != nil {
		return nil, err
	}
	out := []RecentItem{}
	for _, x := range items {
		if perms[x.CategoryID]&acl.View != 0 {
			out = append(out, x)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// searchExternal queries a search backend and applies tags, trash and
// permissions like the PostgreSQL search.
func (s *Service) searchExternal(ctx context.Context, b SearchBackend, q string, tags []string, lang string, limit int, res *SearchResult) error {
	p := identity.From(ctx)
	best := map[int64]int{}
	const page = 100
	for off := 0; off < 5*page && len(res.Elements) < limit; off += page {
		hits, err := b.Search(ctx, SearchQuery{Q: q, Tags: tags, Lang: lang, Limit: page, Offset: off})
		if err != nil {
			return err
		}
		if len(hits) == 0 {
			break
		}
		ids := make([]int64, 0, len(hits))
		for _, h := range hits {
			ids = append(ids, h.ElementID)
		}
		rows, err := s.pool.Query(ctx, `SELECT e.id, e.category_id, e.type, e.tags FROM elements e JOIN categories c ON c.id=e.category_id
			WHERE e.id = ANY($1) AND e.deleted_at IS NULL AND c.deleted_at IS NULL AND (cardinality($2::text[]) = 0 OR e.tags @> $2)
				AND NOT EXISTS (SELECT 1 FROM categories hc WHERE (hc.hidden OR hc.archived_at IS NOT NULL OR hc.classification >= 3) AND c.path <@ hc.path)`, ids, tags)
		if err != nil {
			return err
		}
		type info struct {
			cat  int64
			typ  string
			tags []string
		}
		known := map[int64]info{}
		cats := []int64{}
		for rows.Next() {
			var id int64
			var in info
			if err := rows.Scan(&id, &in.cat, &in.typ, &in.tags); err != nil {
				rows.Close()
				return err
			}
			known[id] = in
			cats = append(cats, in.cat)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		perms, err := s.acl.PermsMap(ctx, p, cats)
		if err != nil {
			return err
		}
		mark := strings.NewReplacer("\x01", "<mark>", "\x02", "</mark>")
		strip := strings.NewReplacer("\x01", "", "\x02", "")
		for _, h := range hits {
			in, ok := known[h.ElementID]
			if !ok || perms[in.cat]&acl.View == 0 {
				continue
			}
			hit := SearchHit{ElementID: h.ElementID, CategoryID: in.cat, Lang: h.Lang, Type: in.typ, Title: strip.Replace(h.Title),
				Snippet: mark.Replace(htmlEscape(h.Snippet)), Tags: in.tags, Rank: float32(h.Score)}
			if i, ok := best[h.ElementID]; ok {
				if h.Lang == lang && res.Elements[i].Lang != lang {
					res.Elements[i] = hit
				}
				continue
			}
			if len(res.Elements) >= limit {
				continue
			}
			best[h.ElementID] = len(res.Elements)
			res.Elements = append(res.Elements, hit)
		}
		if len(hits) < page {
			break
		}
	}
	return nil
}
