// Package catlist contains the parts shared by element types that present
// the elements of a category in a special way (blog, FAQ): the common
// configuration, loading it with permission checks, the categories of the
// scope and API handlers to add entries and subcategories and to serve the
// Atom/RSS feed. The entries stay normal elements of normal categories.
package catlist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/feed"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
	"github.com/mtsonline/enterprisewiki/plugins/core_html"
)

// Source is the common configuration stored in the element body.
type Source struct {
	CategoryID int64          `json:"category_id"`
	Recursive  bool           `json:"recursive"`
	Intro      core_html.Body `json:"intro"`
	Feed       bool           `json:"feed"`
	// More are further sources whose elements are shown too (text form of
	// content.Source, e.g. "category:7+sub; tag:hr"); new entries still go
	// into the own category.
	More string `json:"more_sources"`
}

// Check validates the source and sanitizes the intro text.
func (s *Source) Check() error {
	if s.CategoryID <= 0 {
		return apperr.BadRequest("choose the category whose elements are shown (category_id)")
	}
	if _, err := content.ParseSources(s.More); err != nil {
		return err
	}
	if s.Intro.Format == "" {
		s.Intro.Format = "markdown"
	}
	raw, _ := json.Marshal(s.Intro)
	clean, err := (&core_html.Type{}).Normalize(raw)
	if err != nil {
		return apperr.BadRequest("intro: %v", err)
	}
	return json.Unmarshal(clean, &s.Intro)
}

// IntroHTML renders the intro (sanitized; empty if there is none).
func (s *Source) IntroHTML() string {
	if strings.TrimSpace(s.Intro.Source) == "" {
		return ""
	}
	return core_html.ToHTML(&s.Intro)
}

// Sources resolves the own category (scope) and the further sources for
// the caller; nil when there are no further sources (list by Categories).
func (s *Source) Sources(ctx context.Context, svc *content.Service) (*content.SourceSet, error) {
	if strings.TrimSpace(s.More) == "" {
		return nil, nil
	}
	more, err := content.ParseSources(s.More)
	if err != nil {
		return nil, err
	}
	return svc.ResolveSources(ctx, append([]content.Source{{Kind: "category", ID: s.CategoryID, Sub: s.Recursive}}, more...))
}

// Load reads the configuration of element id (of type typeID) into dst for
// a caller who may view the element. dst must embed Source.
func Load(ctx context.Context, hs *sdk.Host, id int64, typeID, lang string, dst any) (shown string, err error) {
	typ, body, shown, _, err := hs.Content.ElementBody(ctx, id, lang)
	if err != nil {
		return "", err
	}
	if typ != typeID {
		return "", apperr.NotFound("element of type " + typeID)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return "", apperr.BadRequest("invalid configuration: %v", err)
	}
	return shown, nil
}

// Cat is a category of the scope.
type Cat struct {
	ID           int64             `json:"id"`
	ParentID     *int64            `json:"parent_id"`
	Depth        int               `json:"depth"` // 0 = the source category
	Titles       map[string]string `json:"titles"`
	Descriptions map[string]string `json:"descriptions"`
	Position     int               `json:"position"`
	CanCreate    bool              `json:"can_create"`
}

// Info describes the scope for the caller.
type Info struct {
	Scope      []int64 `json:"-"`
	Categories []Cat   `json:"categories"`
	// CanCreate: the caller may add entries somewhere in the scope.
	CanCreate bool `json:"can_create"`
	// CanManage: the caller may add subcategories below the source category.
	CanManage bool `json:"can_manage"`
	// Missing: the source category does not exist or is hidden.
	Missing bool `json:"missing"`
}

// Describe returns the categories of the scope with the caller's rights.
func Describe(ctx context.Context, hs *sdk.Host, src *Source) (*Info, error) {
	info := &Info{Categories: []Cat{}}
	scope, err := hs.Content.Scope(ctx, src.CategoryID, src.Recursive)
	if err != nil {
		info.Missing = true
		return info, nil
	}
	info.Scope = scope
	p := identity.From(ctx)
	perms, err := hs.ACL.PermsMap(ctx, p, scope)
	if err != nil {
		return nil, err
	}
	rows, err := hs.DB.Query(ctx, `SELECT c.id, c.parent_id, nlevel(c.path) - nlevel(r.path), c.titles, c.descriptions, c.position
		FROM categories c, categories r WHERE r.id = $1 AND c.id = ANY($2) ORDER BY c.path`, src.CategoryID, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Cat
		if err := rows.Scan(&c.ID, &c.ParentID, &c.Depth, &c.Titles, &c.Descriptions, &c.Position); err != nil {
			return nil, err
		}
		c.CanCreate = perms[c.ID]&acl.Create != 0
		info.CanCreate = info.CanCreate || c.CanCreate
		info.Categories = append(info.Categories, c)
	}
	info.CanManage = perms[src.CategoryID]&acl.Create != 0
	return info, rows.Err()
}

func (i *Info) contains(id int64) bool {
	for _, c := range i.Scope {
		if c == id {
			return true
		}
	}
	return false
}

// Routes registers the shared API of a category-backed element type below
// /elements/{id}: POST entries (a core.html element in the scope), POST
// categories (a subcategory) and GET feed. load returns the source of an
// element (permission checked).
func Routes(hs *sdk.Host, typeID, entries string, load func(ctx context.Context, id int64, lang string) (*Source, string, error)) {
	// POST /elements/{id}/<entries> {"title", "body": {...}, "category_id", "lang", "tags", "type"}
	hs.Handle("POST", "/elements/{id}/"+entries, func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.ElementInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		ctx := r.Context()
		src, _, err := load(ctx, id, in.Lang)
		if err != nil {
			return nil, err
		}
		info, err := Describe(ctx, hs, src)
		if err != nil {
			return nil, err
		}
		if in.CategoryID == 0 {
			in.CategoryID = src.CategoryID
		}
		if info.Missing || !info.contains(in.CategoryID) {
			return nil, apperr.BadRequest("category %d is not part of this %s", in.CategoryID, typeID)
		}
		if in.Type == "" {
			in.Type = "core.html"
		}
		el, err := hs.Content.CreateElement(ctx, in)
		if err != nil {
			return nil, err
		}
		return h.Created{V: el}, nil
	})
	// POST /elements/{id}/categories {"parent_id" (default: source), "titles", "descriptions"}
	hs.Handle("POST", "/elements/{id}/categories", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in content.CategoryInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		ctx := r.Context()
		src, _, err := load(ctx, id, "")
		if err != nil {
			return nil, err
		}
		if in.ParentID == nil || *in.ParentID == 0 {
			in.ParentID = &src.CategoryID
		}
		info, err := Describe(ctx, hs, src)
		if err != nil {
			return nil, err
		}
		if info.Missing || !info.contains(*in.ParentID) {
			return nil, apperr.BadRequest("category %d is not part of this %s", *in.ParentID, typeID)
		}
		c, err := hs.Content.CreateCategory(ctx, in)
		if err != nil {
			return nil, err
		}
		return h.Created{V: c}, nil
	})
}

// FeedOptions configures WriteFeed.
type FeedOptions struct {
	Title, Subtitle string
	Order           string // created | updated | position
	WithContent     bool
	Limit           int
	Exclude         int64
}

// WriteFeed answers GET /elements/{id}/feed (format, lang, token).
func WriteFeed(w http.ResponseWriter, r *http.Request, hs *sdk.Host, id int64, src *Source, o FeedOptions) (any, error) {
	if !src.Feed {
		return nil, apperr.NotFound("feed (switch it on in the element settings)")
	}
	ctx := r.Context()
	q := r.URL.Query()
	lang := q.Get("lang")
	if lang == "" {
		lang = hs.Content.DefaultLanguage()
	}
	scope, err := hs.Content.Scope(ctx, src.CategoryID, src.Recursive)
	if err != nil {
		return nil, err
	}
	if o.Limit == 0 {
		o.Limit = 30
	}
	set, err := src.Sources(ctx, hs.Content)
	if err != nil {
		return nil, err
	}
	list, _, err := hs.Content.ListIn(ctx, content.ListOptions{Categories: scope, Sources: set, Lang: lang, Order: o.Order, Limit: o.Limit, WithContent: o.WithContent, Exclude: o.Exclude, NoConfidential: true})
	if err != nil {
		return nil, err
	}
	base := feed.BaseURL(hs.Config.Get().Server.BaseURL, r)
	f := feed.Feed{ID: fmt.Sprintf("%s/e/%d", base, id), Title: o.Title, Subtitle: o.Subtitle, Link: fmt.Sprintf("%s/e/%d", base, id),
		Self: feed.SelfURL(base, r), Lang: lang, Entries: feed.FromListing(base, list)}
	private := identity.From(ctx) != nil
	if err := feed.Write(w, r, f, q.Get("format"), private); err != nil {
		return nil, apperr.BadRequest("%v", err)
	}
	return h.Handled{}, nil
}

// FeedContext applies a personal feed token (?token=) to the request.
func FeedContext(hs *sdk.Host, r *http.Request) (*http.Request, error) {
	ctx, _, err := hs.Auth.FeedContext(r)
	if err != nil {
		return nil, err
	}
	return r.WithContext(ctx), nil
}

// PrepareSource completes a body without a source category: it creates a
// subcategory (named like the element) below the element's category, with
// the caller's permissions. Used by the content.Preparer of blog and FAQ.
func PrepareSource(ctx context.Context, svc *content.Service, parent int64, lang, title string, body json.RawMessage) (json.RawMessage, error) {
	m := map[string]any{}
	if err := json.Unmarshal(body, &m); err != nil {
		return body, nil // Normalize reports the error
	}
	if id, ok := m["category_id"].(float64); ok && id > 0 {
		return body, nil
	}
	c, err := svc.CreateCategory(ctx, content.CategoryInput{ParentID: &parent, Titles: map[string]string{lang: title}})
	if err != nil {
		return nil, err
	}
	m["category_id"] = c.ID
	return json.Marshal(m)
}
