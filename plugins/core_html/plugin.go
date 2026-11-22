// Package core_html provides the "core.html" element type: rich text pages
// stored as sanitized HTML or as Markdown.
//
// Other plugins can build on it by embedding Type (see plugins/example_notice).
package core_html

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"html"
	"io/fs"
	"net/http"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	"github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/markdown"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Plugin is the core.html plugin.
type Plugin struct{}

func init() { coreplugin.Register(&Plugin{}) }

// Manifest implements coreplugin.Plugin.
func (p *Plugin) Manifest() coreplugin.Manifest { return coreplugin.ParseManifest(manifest) }

// Locales implements coreplugin.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { sub, _ := fs.Sub(locales, "locales"); return sub }

// Setup registers the element type.
func (p *Plugin) Setup(h *coreplugin.Host) error {
	h.RegisterElementType(&Type{})
	// Conversions for the editors (no macro is executed): the simple editor
	// edits HTML with macro placeholders, both save Markdown.
	h.BodyLimit("POST", "/edit-html", limits.RequestContentKB)
	h.BodyLimit("POST", "/to-markdown", limits.RequestContentKB)
	// action: core.html.to_html
	h.Handle("POST", "/edit-html", func(w http.ResponseWriter, r *http.Request) (any, error) {
		src, err := convertInput(r)
		if err != nil {
			return nil, err
		}
		return map[string]string{"html": ToHTML(&Body{Format: "markdown", Source: src})}, nil
	})
	// action: core.html.to_markdown
	h.Handle("POST", "/to-markdown", func(w http.ResponseWriter, r *http.Request) (any, error) {
		src, err := convertInput(r)
		if err != nil {
			return nil, err
		}
		return map[string]string{"markdown": markdown.FromHTML(policy.Sanitize(src))}, nil
	})
	if h.DB != nil && h.Content != nil && h.Workers != nil {
		registerConversion(h)
	}
	return nil
}

// convertInput reads {"source": "..."} of a conversion request (signed-in
// users only, at most MaxSize).
func convertInput(r *http.Request) (string, error) {
	if p := identity.From(r.Context()); p == nil || p.UserID == 0 && !p.InProcess() {
		return "", apperr.ErrUnauthorized
	}
	r.Body = http.MaxBytesReader(nil, r.Body, MaxSize+4096)
	var in struct {
		Source string `json:"source"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return "", err
	}
	return in.Source, nil
}

// Body is the stored content of a core.html element.
type Body struct {
	Format string `json:"format"` // html | markdown
	Source string `json:"source"`
}

// MaxSize is the maximum size of a page source.
const MaxSize = 2 << 20

// Policy returns the sanitizer used for all rich text.
func Policy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").Matching(bluemonday.SpaceSeparatedTokens).Globally()
	p.AllowAttrs("id").Matching(bluemonday.Paragraph).OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	p.AllowAttrs("data-type", "data-checked").OnElements("ul", "li")
	// code blocks: the info of the editor ("go lines") and line numbers
	p.AllowAttrs("data-lang").Matching(regexp.MustCompile(`^[A-Za-z0-9+#._ -]{0,40}$`)).OnElements("pre")
	p.AllowAttrs("data-lines").Matching(regexp.MustCompile(`^$`)).OnElements("pre")
	p.AllowAttrs("type", "checked", "disabled").OnElements("input")
	p.AllowElements("input", "mark", "figure", "figcaption", "u", "s")
	// macros: <div data-macro="name" data-params='{"k":"v"}'>body</div>
	p.AllowAttrs("data-macro").Matching(regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)).OnElements("div", "span")
	p.AllowAttrs("data-params").Matching(regexp.MustCompile(`^[\s\S]*$`)).OnElements("div", "span")
	p.AllowAttrs("colspan", "rowspan", "scope").OnElements("td", "th")
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}

var policy = Policy()

// Type implements content.ElementType for rich text.
type Type struct{}

// TypeID implements content.ElementType.
func (t *Type) TypeID() string { return "core.html" }

// Labels implements content.ElementType.
func (t *Type) Labels() map[string]string {
	return map[string]string{"en": "Page", "de": "Seite"}
}

// EmptyBody implements content.ElementType.
func (t *Type) EmptyBody() json.RawMessage {
	return json.RawMessage(`{"format":"markdown","source":""}`)
}

// Decode parses and validates a body.
func Decode(raw json.RawMessage) (*Body, error) {
	var b Body
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&b); err != nil {
		return nil, errors.New("body must be {\"format\": \"html\"|\"markdown\", \"source\": \"...\"}")
	}
	if b.Format == "" {
		b.Format = "html"
	}
	if b.Format != "html" && b.Format != "markdown" {
		return nil, errors.New("format must be html or markdown")
	}
	if len(b.Source) > MaxSize {
		return nil, errors.New("content too large (max 2 MB)")
	}
	return &b, nil
}

// Normalize implements content.ElementType: pages are always stored as
// Markdown; HTML (old editor, API clients) is sanitized and converted.
func (t *Type) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	b, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	if b.Format == "html" {
		b.Source, b.Format = markdown.FromHTML(policy.Sanitize(b.Source)), "markdown"
		if len(b.Source) > MaxSize {
			return nil, errors.New("content too large (max 2 MB)")
		}
	}
	return json.Marshal(b)
}

// ToHTML converts a body to sanitized HTML.
func ToHTML(b *Body) string {
	if b.Format == "markdown" {
		out, err := markdown.ToHTML(b.Source)
		if err != nil {
			return "<pre>" + html.EscapeString(b.Source) + "</pre>"
		}
		return policy.Sanitize(out)
	}
	return policy.Sanitize(b.Source)
}

// MacroHTML implements content.MacroHTMLer: the HTML in which the core
// finds the blocks with data (reports).
func (t *Type) MacroHTML(raw json.RawMessage) (string, bool) {
	b, err := Decode(raw)
	if err != nil {
		return "", false
	}
	return ToHTML(b), true
}

// Render implements content.ElementType. Macros are rendered with the
// permissions of the viewer.
func (t *Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	b, err := Decode(raw)
	if err != nil {
		return "", err
	}
	out := labelTasks(ToHTML(b), rc.Lang)
	if rc.Service != nil {
		out = rc.Service.RenderMacros(ctx, rc, out)
	}
	return out, nil
}

// checkboxes of task lists as rendered and sanitized (GFM: "- [x] done")
var taskBox = regexp.MustCompile(`<input( checked="")? disabled="" type="checkbox"/?>`)

var taskLabels = map[string][2]string{"en": {"Not done", "Done"}, "de": {"Offen", "Erledigt"}}

// labelTasks gives the checkboxes of task lists an accessible name (WCAG
// 4.1.2); they are read-only.
func labelTasks(s, lang string) string {
	l, ok := taskLabels[lang]
	if !ok {
		l = taskLabels["en"]
	}
	return taskBox.ReplaceAllStringFunc(s, func(m string) string {
		if strings.Contains(m, "checked") {
			return `<input checked="" disabled="" type="checkbox" aria-label="` + l[1] + `">`
		}
		return `<input disabled="" type="checkbox" aria-label="` + l[0] + `">`
	})
}

var strict = bluemonday.StrictPolicy()

// Text implements content.ElementType.
func (t *Type) Text(raw json.RawMessage) string {
	b, err := Decode(raw)
	if err != nil {
		return ""
	}
	h := ToHTML(b)
	// keep word boundaries between blocks
	h = strings.NewReplacer("</p>", "</p> ", "<br>", " ", "</li>", "</li> ", "</h1>", "</h1> ", "</h2>", "</h2> ", "</h3>", "</h3> ", "</td>", "</td> ").Replace(h)
	return strings.Join(strings.Fields(html.UnescapeString(strict.Sanitize(h))), " ")
}
