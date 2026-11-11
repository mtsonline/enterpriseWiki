// Package content_faq provides the "FAQ" element type: the subcategories of
// a category become collapsible sections, its elements collapsible questions
// (title = question, content = answer). Everything stays normal categories
// and elements, so permissions, history, translations and search apply.
package content_faq

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/pkg/catlist"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// TypeID is the element type.
const TypeID = "content.faq"

// MaxQuestions limits the size of one FAQ.
const MaxQuestions = 1000

// Plugin is the FAQ plugin.
type Plugin struct{ host *sdk.Host }

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Config is the body of a FAQ element.
type Config struct {
	catlist.Source
	OpenSections bool `json:"open_sections"` // sections expanded initially
	Numbering    bool `json:"numbering"`     // "1.2" before questions
	Search       bool `json:"search"`        // search field above the FAQ
}

func decode(raw []byte) (*Config, error) {
	c := &Config{}
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, apperr.BadRequest("invalid FAQ settings: %v", err)
	}
	if err := c.Check(); err != nil {
		return nil, err
	}
	return c, nil
}

// Question is a question with its answer.
type Question struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Lang      string    `json:"lang"`
	HTML      string    `json:"html"`
	Tags      []string  `json:"tags"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Section is a category of the FAQ (depth 0 = the FAQ's category).
type Section struct {
	ID          int64      `json:"id"`
	ParentID    *int64     `json:"parent_id"`
	Depth       int        `json:"depth"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	CanCreate   bool       `json:"can_create"`
	Questions   []Question `json:"questions"`
	Number      string     `json:"number"`
}

func pick(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	for _, v := range m {
		if v != "" {
			return v
		}
	}
	return ""
}

// sections loads sections and questions (only ids in only, if not nil).
func sections(ctx context.Context, svc *content.Service, info *catlist.Info, src *catlist.Source, lang string, exclude int64, only map[int64]bool) ([]Section, error) {
	// further sources: their questions come in sections of their categories
	set, err := src.Sources(ctx, svc)
	if err != nil {
		return nil, err
	}
	var list []content.ListedElement
	for off := 0; off < MaxQuestions; off += 200 {
		part, total, err := svc.ListIn(ctx, content.ListOptions{Categories: info.Scope, Sources: set, Lang: lang, Order: "position", Limit: 200, Offset: off, Exclude: exclude})
		if err != nil {
			return nil, err
		}
		list = append(list, part...)
		if off+200 >= total {
			break
		}
	}
	byCat := map[int64][]Question{}
	for _, e := range list {
		if only != nil && !only[e.ID] {
			continue
		}
		q := Question{ID: e.ID, Title: e.Title, Lang: e.Lang, Tags: e.Tags, UpdatedAt: e.UpdatedAt}
		if v, err := svc.GetElement(ctx, e.ID, lang, false); err == nil {
			q.HTML = v.HTML
		}
		byCat[e.CategoryID] = append(byCat[e.CategoryID], q)
	}
	out := make([]Section, 0, len(info.Categories))
	counter := map[int64]int{}
	numbers := map[int64]string{}
	for _, c := range info.Categories {
		s := Section{ID: c.ID, ParentID: c.ParentID, Depth: c.Depth, Title: pick(c.Titles, lang), Description: pick(c.Descriptions, lang),
			CanCreate: c.CanCreate, Questions: byCat[c.ID]}
		if s.Questions == nil {
			s.Questions = []Question{}
		}
		if c.Depth > 0 && c.ParentID != nil {
			counter[*c.ParentID]++
			n := fmt.Sprint(counter[*c.ParentID])
			if p := numbers[*c.ParentID]; p != "" {
				n = p + "." + n
			}
			numbers[c.ID], s.Number = n, n
		}
		out = append(out, s)
	}
	// sections of the categories of further sources (in the order they appear)
	own := map[int64]bool{}
	for _, c := range info.Categories {
		own[c.ID] = true
	}
	var foreign []int64
	for _, e := range list {
		if !own[e.CategoryID] && len(byCat[e.CategoryID]) > 0 && !slices.Contains(foreign, e.CategoryID) {
			foreign = append(foreign, e.CategoryID)
		}
	}
	if len(foreign) > 0 {
		cats, err := svc.CategoriesByID(ctx, foreign)
		if err != nil {
			return nil, err
		}
		// below the FAQ's own section (a second depth-0 section would replace it)
		var root *int64
		if len(info.Categories) > 0 {
			id := info.Categories[0].ID
			root = &id
		}
		for _, c := range cats {
			out = append(out, Section{ID: c.ID, ParentID: root, Depth: 1, Title: pick(c.Titles, lang), Description: pick(c.Descriptions, lang), Questions: byCat[c.ID]})
		}
	}
	if only != nil {
		// keep sections with hits and their ancestors
		keep := map[int64]bool{}
		parent := map[int64]*int64{}
		for _, s := range out {
			parent[s.ID] = s.ParentID
		}
		for _, s := range out {
			if len(s.Questions) > 0 {
				for id := &s.ID; id != nil && !keep[*id]; id = parent[*id] {
					keep[*id] = true
				}
			}
		}
		filtered := out[:0]
		for _, s := range out {
			if keep[s.ID] {
				filtered = append(filtered, s)
			}
		}
		out = filtered
	}
	return out, nil
}

// Type implements content.ElementType.
type Type struct{}

// TypeID implements content.ElementType.
func (Type) TypeID() string { return TypeID }

// ListsElements implements content.Listing (left out of lists of several sources).
func (Type) ListsElements() bool { return true }

// Labels implements content.ElementType.
func (Type) Labels() map[string]string { return map[string]string{"en": "FAQ", "de": "FAQ"} }

// EmptyBody implements content.ElementType.
func (Type) EmptyBody() json.RawMessage {
	return json.RawMessage(`{"category_id":0,"recursive":true,"intro":{"format":"markdown","source":""},"feed":true,"open_sections":false,"numbering":false,"search":true}`)
}

// Normalize implements content.ElementType.
func (Type) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	c, err := decode(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

// Text implements content.ElementType.
func (Type) Text(raw json.RawMessage) string {
	var c Config
	_ = json.Unmarshal(raw, &c)
	return c.Intro.Source
}

var msgs = map[string]map[string]string{
	"en": {"empty": "No questions yet.", "missing": "The category of this FAQ no longer exists or you may not see it."},
	"de": {"empty": "Noch keine Fragen.", "missing": "Die Kategorie dieser FAQ existiert nicht mehr oder ist für Sie nicht sichtbar."},
}

func msg(lang, key string) string {
	if m, ok := msgs[lang]; ok {
		return m[key]
	}
	return msgs["en"][key]
}

// RenderSections writes the sections as nested <details> (no JavaScript needed).
func RenderSections(b *strings.Builder, secs []Section, c *Config) {
	esc := html.EscapeString
	children := map[int64][]Section{}
	var root *Section
	for i := range secs {
		if secs[i].Depth == 0 {
			root = &secs[i]
		} else if secs[i].ParentID != nil {
			children[*secs[i].ParentID] = append(children[*secs[i].ParentID], secs[i])
		}
	}
	questions := func(s *Section) {
		if len(s.Questions) == 0 {
			return
		}
		b.WriteString(`<div class="ewq-questions">`)
		for i, q := range s.Questions {
			num := ""
			if c.Numbering {
				if s.Number != "" {
					num = fmt.Sprintf(`<span class="ewq-num">%s.%d</span> `, s.Number, i+1)
				} else {
					num = fmt.Sprintf(`<span class="ewq-num">%d</span> `, i+1)
				}
			}
			fmt.Fprintf(b, `<details class="ewq-q" id="q-%d" data-id="%d" lang="%s"><summary class="ewq-question">%s%s</summary><div class="ewq-answer">%s</div></details>`,
				q.ID, q.ID, esc(q.Lang), num, esc(q.Title), q.HTML)
		}
		b.WriteString(`</div>`)
	}
	var section func(s *Section, level int)
	section = func(s *Section, level int) {
		open := ""
		if c.OpenSections {
			open = " open"
		}
		hl := level + 1
		if hl > 6 {
			hl = 6
		}
		num := ""
		if c.Numbering && s.Number != "" {
			num = `<span class="ewq-num">` + esc(s.Number) + `</span> `
		}
		fmt.Fprintf(b, `<details class="ewq-section ewq-depth-%d" id="s-%d" data-id="%d"%s><summary class="ewq-section-title"><h%d>%s%s</h%d></summary><div class="ewq-section-body">`,
			s.Depth, s.ID, s.ID, open, hl, num, esc(s.Title), hl)
		if s.Description != "" {
			b.WriteString(`<p class="ewq-section-desc">` + esc(s.Description) + `</p>`)
		}
		questions(s)
		for i := range children[s.ID] {
			section(&children[s.ID][i], level+1)
		}
		b.WriteString(`</div></details>`)
	}
	if root != nil {
		questions(root)
		for i := range children[root.ID] {
			section(&children[root.ID][i], 1)
		}
	}
}

// Render implements content.ElementType.
func (Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	c, err := decode(raw)
	if err != nil {
		return "", err
	}
	esc := html.EscapeString
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewq" data-ew-widget="faq" data-element="%d">`, rc.ElementID)
	if intro := c.IntroHTML(); intro != "" {
		b.WriteString(`<div class="ewq-intro">` + intro + `</div>`)
	}
	scope, err := rc.Service.Scope(ctx, c.CategoryID, c.Recursive)
	if err != nil {
		b.WriteString(`<p role="note">` + esc(msg(rc.Lang, "missing")) + `</p></div>`)
		return b.String(), nil
	}
	info := &catlist.Info{Scope: scope}
	// titles of the sections (Describe needs the host; the renderer only has the service)
	cats, err := rc.Service.CategoriesByID(ctx, scope)
	if err != nil {
		return "", err
	}
	for _, x := range cats {
		depth := len(x.Path) - len(cats[0].Path)
		info.Categories = append(info.Categories, catlist.Cat{ID: x.ID, ParentID: x.ParentID, Depth: depth, Titles: x.Titles, Descriptions: x.Descriptions})
	}
	secs, err := sections(ctx, rc.Service, info, &c.Source, rc.Lang, rc.ElementID, nil)
	if err != nil {
		return "", err
	}
	b.WriteString(`<div class="ewq-body">`)
	n := 0
	for _, s := range secs {
		n += len(s.Questions)
	}
	if n == 0 {
		b.WriteString(`<p class="ewq-empty">` + esc(msg(rc.Lang, "empty")) + `</p>`)
	}
	RenderSections(&b, secs, c)
	b.WriteString(`</div></div>`)
	return b.String(), nil
}

func (p *Plugin) load(ctx context.Context, id int64, lang string) (*Config, string, error) {
	var raw json.RawMessage
	shown, err := catlist.Load(ctx, p.host, id, TypeID, lang, &raw)
	if err != nil {
		return nil, "", err
	}
	c, err := decode(raw)
	return c, shown, err
}

// Setup registers the element type and the API.
func (p *Plugin) Setup(hs *sdk.Host) error {
	p.host = hs
	hs.RegisterElementType(Type{})
	hs.RegisterMacro(faqMacro{})
	catlist.Routes(hs, "FAQ", "questions", func(ctx context.Context, id int64, lang string) (*catlist.Source, string, error) {
		c, shown, err := p.load(ctx, id, lang)
		if err != nil {
			return nil, "", err
		}
		return &c.Source, shown, nil
	})

	// GET /elements/{id}/questions?q=&lang= returns sections with questions
	// and answers; q searches within the FAQ.
	hs.Handle("GET", "/elements/{id}/questions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		q := r.URL.Query()
		c, shown, err := p.load(ctx, id, q.Get("lang"))
		if err != nil {
			return nil, err
		}
		lang := q.Get("lang")
		if lang == "" {
			lang = shown
		}
		info, err := catlist.Describe(ctx, hs, &c.Source)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"config": c, "info": info, "sections": []Section{}}
		if info.Missing {
			return out, nil
		}
		var only map[int64]bool
		if term := strings.TrimSpace(q.Get("q")); term != "" {
			scope := info.Scope
			if set, err := c.Sources(ctx, hs.Content); err == nil && set != nil {
				scope = set.Categories
			}
			res, err := hs.Content.SearchIn(ctx, term, nil, lang, 100, scope)
			if err != nil {
				return nil, err
			}
			only = map[int64]bool{}
			for _, x := range res.Elements {
				only[x.ElementID] = true
			}
		}
		secs, err := sections(ctx, hs.Content, info, &c.Source, lang, id, only)
		if err != nil {
			return nil, err
		}
		out["sections"] = secs
		return out, nil
	})

	hs.Handle("GET", "/elements/{id}/feed", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if r, err = catlist.FeedContext(hs, r); err != nil {
			return nil, err
		}
		c, _, err := p.load(r.Context(), id, r.URL.Query().Get("lang"))
		if err != nil {
			return nil, err
		}
		v, err := hs.Content.GetElement(r.Context(), id, r.URL.Query().Get("lang"), false)
		if err != nil {
			return nil, err
		}
		// newest and recently answered questions first
		return catlist.WriteFeed(w, r, hs, id, &c.Source, catlist.FeedOptions{Title: v.Title, Order: "updated", WithContent: true, Limit: 50, Exclude: id})
	})
	return nil
}

// Prepare implements content.Preparer: without a chosen category a new one
// (named like the element) is created for the entries.
func (Type) Prepare(ctx context.Context, svc *content.Service, categoryID int64, lang, title string, body json.RawMessage) (json.RawMessage, error) {
	return catlist.PrepareSource(ctx, svc, categoryID, lang, title, body)
}
