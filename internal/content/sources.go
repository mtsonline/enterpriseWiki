package content

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Sources: where reading plugins and macros (blog, FAQ, news, carousel,
// lists, property reports …) take their elements from. Several sources are
// combined; every element is shown once and only if the reader may view
// its category. The short text form keeps Markdown readable:
//
//	category:12+sub; category:7; tag:hr; type:core.html; elements:1,2
//
// Any of them is enough ("include"); a leading "-" excludes (-tag:intern)
// and a leading "+" requires a source as well (+props:status = open).
//
// Plugins add own kinds (e.g. other calendars, external feeds) and read
// them with ParseSourcesKinds.

// Source is one source of elements.
type Source struct {
	Kind string  `json:"kind"` // category | tag | type | elements (or a kind of a plugin)
	ID   int64   `json:"id,omitempty"`
	Sub  bool    `json:"sub,omitempty"` // category: with subcategories
	Tag  string  `json:"tag,omitempty"`
	Type string  `json:"type,omitempty"`
	IDs  []int64 `json:"ids,omitempty"`
	// Value is the text of a plugin's kind (e.g. a feed address).
	Value string `json:"value,omitempty"`
	// Mode: "" (include: any source is enough), "exclude" (never these)
	// or "require" (only elements that also match this source).
	Mode string `json:"mode,omitempty"`
}

// Source modes.
const (
	ModeExclude = "exclude"
	ModeRequire = "require"
)

// HasInclude reports whether a list has a source to include (otherwise
// callers use their default, e.g. the current category).
func HasInclude(list []Source) bool {
	for _, s := range list {
		if s.Mode == "" {
			return true
		}
	}
	return false
}

// WithDefault adds the category cat with subcategories when the list has
// no source to include (e.g. only exclusions).
func WithDefault(list []Source, cat int64) []Source {
	if HasInclude(list) || cat <= 0 {
		return list
	}
	return append([]Source{{Kind: "category", ID: cat, Sub: true}}, list...)
}

// MaxSources is the most sources of one element or macro.
const MaxSources = 50

// ElementKinds are the kinds the core resolves to elements.
var ElementKinds = []string{"category", "tag", "type", "elements", "search"}

// resolvedKinds are kinds whose text value is turned into elements by a
// resolver: "search" (full-text search) and those plugins register
// (e.g. "props" for property filters).
var (
	resolvedMu    sync.RWMutex
	resolvedKinds = map[string]bool{"search": true}
)

// RegisterElementSourceKind declares a kind resolved to elements by a
// plugin (call in init; the plugin registers the resolver per site with
// Service.RegisterSourceResolver).
func RegisterElementSourceKind(kind string) {
	resolvedMu.Lock()
	defer resolvedMu.Unlock()
	resolvedKinds[kind] = true
}

func isResolvedKind(kind string) bool {
	resolvedMu.RLock()
	defer resolvedMu.RUnlock()
	return resolvedKinds[kind]
}

// SourceResolver returns the elements of a source value (with the rights
// of the caller; at most limit).
type SourceResolver func(ctx context.Context, value, lang string, limit int) ([]int64, error)

// MaxListItems is the upper bound of entries of one list and of elements
// a resolved source (search, filter) contributes (setting lists.max_items).
func (s *Service) MaxListItems() int { return s.cfg.Get().Lists.ListMax() }

// RegisterSourceResolver sets the resolver of a kind on this site.
func (s *Service) RegisterSourceResolver(kind string, r SourceResolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resolvers == nil {
		s.resolvers = map[string]SourceResolver{}
	}
	s.resolvers[kind] = r
}

func (s *Service) resolver(kind string) SourceResolver {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resolvers[kind]
}

// ParseSources reads the text form of element sources.
func ParseSources(s string) ([]Source, error) {
	return ParseSourcesKinds(s, nil)
}

// ParseSourcesKinds reads the text form with additional kinds of a plugin
// (their value is kept as text in Value, checked by the plugin).
func ParseSourcesKinds(s string, extra []string) ([]Source, error) {
	var out []Source
	if len(s) > 8000 {
		return nil, apperr.BadRequest("sources too long")
	}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kind, val, ok := strings.Cut(part, ":")
		if !ok {
			return nil, apperr.BadRequest("source %q: kind:value", part)
		}
		kind, val = strings.TrimSpace(kind), strings.TrimSpace(val)
		mode := ""
		switch {
		case strings.HasPrefix(kind, "-"):
			mode, kind = ModeExclude, strings.TrimSpace(kind[1:])
		case strings.HasPrefix(kind, "+"):
			mode, kind = ModeRequire, strings.TrimSpace(kind[1:])
		}
		n := len(out)
		switch kind {
		case "category":
			sub := strings.HasSuffix(val, "+sub")
			id, err := strconv.ParseInt(strings.TrimSuffix(val, "+sub"), 10, 64)
			if err != nil || id <= 0 {
				return nil, apperr.BadRequest("source %q: category id", part)
			}
			out = append(out, Source{Kind: "category", ID: id, Sub: sub})
		case "tag":
			if val == "" || utf8.RuneCountInString(val) > 100 {
				return nil, apperr.BadRequest("source %q: tag", part)
			}
			out = append(out, Source{Kind: "tag", Tag: strings.ToLower(val)})
		case "type":
			if val == "" || len(val) > 100 {
				return nil, apperr.BadRequest("source %q: element type", part)
			}
			out = append(out, Source{Kind: "type", Type: val})
		case "elements":
			var ids []int64
			for _, x := range strings.Split(val, ",") {
				id, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
				if err != nil || id <= 0 {
					return nil, apperr.BadRequest("source %q: element ids", part)
				}
				ids = append(ids, id)
			}
			if len(ids) > 1000 {
				return nil, apperr.BadRequest("at most 1000 elements")
			}
			out = append(out, Source{Kind: "elements", IDs: ids})
		default:
			found := isResolvedKind(kind)
			for _, k := range extra {
				found = found || k == kind
			}
			if !found {
				return nil, apperr.BadRequest("unknown source %q", kind)
			}
			if len(val) > 2000 || val == "" && isResolvedKind(kind) {
				return nil, apperr.BadRequest("source %q: value", kind)
			}
			out = append(out, Source{Kind: kind, Value: val})
		}
		if len(out) > n {
			out[n].Mode = mode
		}
		if len(out) > MaxSources {
			return nil, apperr.BadRequest("at most %d sources", MaxSources)
		}
	}
	return out, nil
}

// FormatSources writes sources in the text form.
func FormatSources(list []Source) string {
	parts := make([]string, 0, len(list))
	for _, s := range list {
		pre := ""
		switch s.Mode {
		case ModeExclude:
			pre = "-"
		case ModeRequire:
			pre = "+"
		}
		switch s.Kind {
		case "category":
			p := "category:" + strconv.FormatInt(s.ID, 10)
			if s.Sub {
				p += "+sub"
			}
			parts = append(parts, pre+p)
		case "tag":
			parts = append(parts, pre+"tag:"+s.Tag)
		case "type":
			parts = append(parts, pre+"type:"+s.Type)
		case "elements":
			ids := make([]string, len(s.IDs))
			for i, id := range s.IDs {
				ids[i] = strconv.FormatInt(id, 10)
			}
			parts = append(parts, pre+"elements:"+strings.Join(ids, ","))
		default:
			parts = append(parts, pre+s.Kind+":"+s.Value)
		}
	}
	return strings.Join(parts, "; ")
}

// SourceSet is a resolved set of element sources for one reader.
type SourceSet struct {
	// Categories of the category sources the reader may view.
	Categories []int64
	Tags       []string
	Types      []string
	IDs        []int64
	// Viewable are all categories the reader may view (tags, types and
	// single elements are only shown from there).
	Viewable []int64
	// Resolved: some IDs come from searches or filters (not listed by hand)
	Resolved bool
	// Not: elements matching it are left out; And: each must match too.
	Not *SourceSet
	And []*SourceSet
}

// Empty reports whether nothing can come from the set.
func (s *SourceSet) Empty() bool {
	return s == nil || len(s.Categories) == 0 && len(s.Tags) == 0 && len(s.Types) == 0 && len(s.IDs) == 0
}

// ResolveSources checks element sources with the rights of the caller;
// other kinds are ignored (they belong to plugins). Categories that do not
// exist or that the caller may not view are left out silently, so a page
// shows what its reader may see.
func (s *Service) ResolveSources(ctx context.Context, list []Source) (*SourceSet, error) {
	var inc, exc, req []Source
	for _, src := range list {
		switch src.Mode {
		case ModeExclude:
			exc = append(exc, src)
		case ModeRequire:
			req = append(req, src)
		default:
			inc = append(inc, src)
		}
	}
	if len(inc) == 0 && len(exc)+len(req) > 0 {
		return nil, apperr.BadRequest("sources: at least one source to include (not only - or +)")
	}
	set, err := s.resolveSet(ctx, inc, true)
	if err != nil {
		return nil, err
	}
	if len(exc) > 0 {
		if set.Not, err = s.resolveSet(ctx, exc, false); err != nil {
			return nil, err
		}
	}
	for _, r := range req {
		x, err := s.resolveSet(ctx, []Source{r}, false)
		if err != nil {
			return nil, err
		}
		set.And = append(set.And, x)
	}
	return set, nil
}

// ResolveNarrowing resolves one source that only narrows a list
// (exclusion or requirement).
func (s *Service) ResolveNarrowing(ctx context.Context, src Source) (*SourceSet, error) {
	return s.resolveSet(ctx, []Source{src}, false)
}

// resolveSet resolves sources; with reader it also limits the categories to
// those the caller may view (exclusions and requirements only narrow a
// set that is limited already).
func (s *Service) resolveSet(ctx context.Context, list []Source, reader bool) (*SourceSet, error) {
	set := &SourceSet{}
	limit := s.MaxListItems()
	var flat, sub []int64
	needAll := false
	for _, src := range list {
		switch src.Kind {
		case "category":
			if src.Sub {
				sub = append(sub, src.ID)
			} else {
				flat = append(flat, src.ID)
			}
		case "tag":
			set.Tags = append(set.Tags, strings.ToLower(src.Tag))
			needAll = true
		case "type":
			set.Types = append(set.Types, src.Type)
			needAll = true
		case "elements":
			set.IDs = append(set.IDs, src.IDs...)
			needAll = true
		case "search":
			ids, err := s.searchIDs(ctx, src.Value, limit)
			if err != nil {
				return nil, err
			}
			set.IDs = append(set.IDs, ids...)
			set.Resolved = true
			needAll = true
		default:
			if r := s.resolver(src.Kind); r != nil && isResolvedKind(src.Kind) {
				ids, err := r(ctx, src.Value, "", limit)
				if err != nil {
					return nil, err
				}
				if len(ids) > limit {
					ids = ids[:limit]
				}
				set.IDs = append(set.IDs, ids...)
				set.Resolved = true
				needAll = true
			}
		}
	}
	p := identity.From(ctx)
	var rows pgx.Rows
	var err error
	if len(flat)+len(sub) > 0 {
		rows, err = s.pool.Query(ctx, `SELECT c.id FROM categories c WHERE c.deleted_at IS NULL AND (c.id = ANY($1)
			OR EXISTS (SELECT 1 FROM categories t WHERE t.id = ANY($2) AND t.deleted_at IS NULL AND c.path <@ t.path))`, flat, sub)
		if err != nil {
			return nil, err
		}
		cats, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return nil, err
		}
		if !reader {
			set.Categories = cats
		} else if set.Categories, err = s.acl.Filter(ctx, p, cats, acl.View); err != nil {
			return nil, err
		}
	}
	if !reader {
		needAll = false
	}
	if needAll {
		rows, err = s.pool.Query(ctx, `SELECT id FROM categories WHERE deleted_at IS NULL`)
		if err != nil {
			return nil, err
		}
		all, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return nil, err
		}
		if set.Viewable, err = s.acl.Filter(ctx, p, all, acl.View); err != nil {
			return nil, err
		}
	} else {
		set.Viewable = set.Categories
	}
	if set.Categories == nil {
		set.Categories = []int64{}
	}
	return set, nil
}

// sourceCond returns the SQL condition for elements e of a source set with
// the arguments starting at number n.
func sourceCond(set *SourceSet, n int) (string, []any) {
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(n+len(args)-1)
	}
	return SourceSQL(set, arg), args
}

// SourceSQL returns the SQL condition for elements e of a resolved source
// set; arg adds an argument and returns its placeholder.
func SourceSQL(set *SourceSet, arg func(any) string) string {
	cond := matchSQL(set, arg) + " AND e.category_id = ANY(" + arg(set.Viewable) + ")"
	if set.Not != nil {
		cond += " AND NOT " + matchSQL(set.Not, arg)
	}
	for _, x := range set.And {
		cond += " AND " + matchSQL(x, arg)
	}
	return cond
}

// MatchSQL is the condition for elements e matching a set (without the
// reader's categories; for narrowing an already limited query).
func MatchSQL(set *SourceSet, arg func(any) string) string { return matchSQL(set, arg) }

func matchSQL(set *SourceSet, arg func(any) string) string {
	cats, tags, types, ids := set.Categories, set.Tags, set.Types, set.IDs
	if cats == nil {
		cats = []int64{}
	}
	if tags == nil {
		tags = []string{}
	}
	if types == nil {
		types = []string{}
	}
	if ids == nil {
		ids = []int64{}
	}
	return "(e.category_id = ANY(" + arg(cats) + ") OR e.tags && " + arg(tags) + " OR e.type = ANY(" + arg(types) + ") OR e.id = ANY(" + arg(ids) + "))"
}

// searchIDs returns the elements matching a full-text search (PostgreSQL,
// with the rules of the search: hidden, archived and strictly confidential
// categories only when searched there; the viewer's rights are applied
// by the source set).
func (s *Service) searchIDs(ctx context.Context, q string, limit int) ([]int64, error) {
	tsq := buildTSQuery(q)
	if tsq == "" {
		return nil, apperr.BadRequest("search source: enter a search term")
	}
	rows, err := s.pool.Query(ctx, `SELECT x.element_id FROM (
			SELECT d.element_id, max(ts_rank(d.tsv, q)) AS r FROM search_docs d JOIN elements e ON e.id = d.element_id, to_tsquery('simple', $1) q
			WHERE d.tsv @@ q AND e.deleted_at IS NULL
				AND NOT EXISTS (SELECT 1 FROM categories hc JOIN categories hx ON hx.id = d.category_id WHERE (hc.hidden OR hc.archived_at IS NOT NULL OR hc.classification >= 3) AND hx.path <@ hc.path)
			GROUP BY d.element_id) x ORDER BY x.r DESC, x.element_id LIMIT $2`, tsq, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}
