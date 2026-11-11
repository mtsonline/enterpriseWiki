// Package content_catlist provides the "Category overview" element type: the
// subcategories of a category (or the top level) as cards or a list,
// optionally with their elements – for start pages and section overviews.
package content_catlist

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
	"github.com/mtsonline/enterprisewiki/plugins/core_html"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// TypeID is the element type.
const TypeID = "content.catlist"

// Plugin is the plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Setup registers the element type.
func (p *Plugin) Setup(hs *sdk.Host) error {
	hs.RegisterElementType(Type{})
	return nil
}

// Config is the body.
type Config struct {
	Intro        core_html.Body `json:"intro"`
	CategoryID   int64          `json:"category_id"` // 0 = top level
	Depth        int            `json:"depth"`       // levels of subcategories (1–5)
	Layout       string         `json:"layout"`      // cards | list
	Descriptions bool           `json:"descriptions"`
	Elements     bool           `json:"elements"` // list the elements of each category
	MaxElements  int            `json:"max_elements"`
}

func decode(raw []byte) (*Config, error) {
	c := &Config{}
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, apperr.BadRequest("invalid settings: %v", err)
	}
	if c.CategoryID < 0 {
		c.CategoryID = 0
	}
	if c.Depth < 1 || c.Depth > 5 {
		c.Depth = 1
	}
	if c.Layout != "list" {
		c.Layout = "cards"
	}
	if c.MaxElements <= 0 || c.MaxElements > 50 {
		c.MaxElements = 5
	}
	if c.Intro.Format == "" {
		c.Intro.Format = "markdown"
	}
	d, _ := json.Marshal(c.Intro)
	clean, err := (&core_html.Type{}).Normalize(d)
	if err != nil {
		return nil, apperr.BadRequest("intro: %v", err)
	}
	_ = json.Unmarshal(clean, &c.Intro)
	return c, nil
}

// Type implements content.ElementType.
type Type struct{}

// TypeID implements content.ElementType.
func (Type) TypeID() string { return TypeID }

// ListsElements implements content.Listing (left out of lists of several sources).
func (Type) ListsElements() bool { return true }

// Labels implements content.ElementType.
func (Type) Labels() map[string]string {
	return map[string]string{"en": "Category overview", "de": "Kategorieübersicht"}
}

// EmptyBody implements content.ElementType.
func (Type) EmptyBody() json.RawMessage {
	return json.RawMessage(`{"intro":{"format":"markdown","source":""},"category_id":0,"depth":1,"layout":"cards","descriptions":true,"elements":false,"max_elements":5}`)
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

func href(kind string, id int64, short string) string {
	if short != "" {
		return "/" + short
	}
	return fmt.Sprintf("/%s/%d", kind, id)
}

var empty = map[string]string{"en": "No categories.", "de": "Keine Kategorien."}

// Render implements content.ElementType (permissions of the viewer apply).
func (Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	c, err := decode(raw)
	if err != nil {
		return "", err
	}
	svc := rc.Service
	esc := html.EscapeString
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewo ewo--%s">`, c.Layout)
	if strings.TrimSpace(c.Intro.Source) != "" {
		b.WriteString(`<div class="ewo-intro">` + core_html.ToHTML(&c.Intro) + `</div>`)
	}
	nodes := 0
	var level func(parent int64, depth int) error
	level = func(parent int64, depth int) error {
		cats, err := svc.Children(ctx, parent)
		if err != nil {
			return err
		}
		if len(cats) == 0 {
			if depth == 1 {
				m := empty["en"]
				if v, ok := empty[rc.Lang]; ok {
					m = v
				}
				b.WriteString(`<p class="ewo-empty">` + esc(m) + `</p>`)
			}
			return nil
		}
		cls := "ewo-list"
		if depth == 1 && c.Layout == "cards" {
			cls = "ew-cards ewo-cards"
		}
		fmt.Fprintf(&b, `<ul class="%s ewo-level-%d">`, cls, depth)
		for _, x := range cats {
			if nodes++; nodes > 500 {
				break
			}
			item := "ewo-item"
			if depth == 1 && c.Layout == "cards" {
				item = "ew-card ewo-item"
			}
			tag := "h2"
			if depth > 1 || c.Layout == "list" {
				tag = "h3"
			}
			fmt.Fprintf(&b, `<li class="%s"><%s class="ewo-title"><a href="%s">%s</a></%s>`, item, tag, esc(href("c", x.ID, x.ShortURL)), esc(pick(x.Titles, rc.Lang)), tag)
			if d := pick(x.Descriptions, rc.Lang); c.Descriptions && d != "" {
				b.WriteString(`<p class="ewo-desc">` + esc(d) + `</p>`)
			}
			if c.Elements {
				els, err := svc.ListElements(ctx, x.ID)
				if err == nil && len(els) > 0 {
					b.WriteString(`<ul class="ewo-elements">`)
					for i, e := range els {
						if i >= c.MaxElements {
							fmt.Fprintf(&b, `<li class="ewo-more"><a href="%s">… (%d)</a></li>`, esc(href("c", x.ID, x.ShortURL)), len(els))
							break
						}
						fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`, esc(href("e", e.ID, e.ShortURL)), esc(pick(e.Titles, rc.Lang)))
					}
					b.WriteString(`</ul>`)
				}
			}
			if depth < c.Depth && x.HasChildren {
				if err := level(x.ID, depth+1); err != nil {
					return err
				}
			}
			b.WriteString(`</li>`)
		}
		b.WriteString(`</ul>`)
		return nil
	}
	if err := level(c.CategoryID, 1); err != nil {
		return "", err
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}
