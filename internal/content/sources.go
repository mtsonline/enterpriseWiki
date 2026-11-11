package content

import (
	"context"
	"strconv"
	"strings"
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
}

// MaxSources is the most sources of one element or macro.
const MaxSources = 50

// ElementKinds are the kinds the core resolves to elements.
var ElementKinds = []string{"category", "tag", "type", "elements"}

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
			found := false
			for _, k := range extra {
				found = found || k == kind
			}
			if !found {
				return nil, apperr.BadRequest("unknown source %q", kind)
			}
			if len(val) > 2000 {
				return nil, apperr.BadRequest("source %q too long", kind)
			}
			out = append(out, Source{Kind: kind, Value: val})
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
		switch s.Kind {
		case "category":
			p := "category:" + strconv.FormatInt(s.ID, 10)
			if s.Sub {
				p += "+sub"
			}
			parts = append(parts, p)
		case "tag":
			parts = append(parts, "tag:"+s.Tag)
		case "type":
			parts = append(parts, "type:"+s.Type)
		case "elements":
			ids := make([]string, len(s.IDs))
			for i, id := range s.IDs {
				ids[i] = strconv.FormatInt(id, 10)
			}
			parts = append(parts, "elements:"+strings.Join(ids, ","))
		default:
			parts = append(parts, s.Kind+":"+s.Value)
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
	set := &SourceSet{}
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
		if set.Categories, err = s.acl.Filter(ctx, p, cats, acl.View); err != nil {
			return nil, err
		}
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
	a := func(i int) string { return "$" + strconv.Itoa(n+i) }
	return "(e.category_id = ANY(" + a(0) + ") OR e.tags && " + a(1) + " OR e.type = ANY(" + a(2) + ") OR e.id = ANY(" + a(3) + ")) AND e.category_id = ANY(" + a(4) + ")",
		[]any{set.Categories, set.Tags, set.Types, set.IDs, set.Viewable}
}
