package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"sync"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Macros are dynamic blocks inside rich text, written in Markdown as
// directives (::toc{levels=3}, :::panel{...} ... :::, :status[OK]{...}) and
// rendered to
//
//	<div data-macro="toc" data-params='{"levels":"3"}'>optional body</div>
//	<span data-macro="status" ...>label</span>   (inline)
//
// They are provided by plugins and rendered on every view with the
// permissions of the viewer. Macros run after sanitizing; a macro must
// escape everything it outputs that is not already sanitized (the body is).

// MacroParam describes a parameter for the macro dialog in the editor.
type MacroParam struct {
	Name     string            `json:"name"`
	Labels   map[string]string `json:"labels"`
	Type     string            `json:"type"` // text | textarea | number | select | bool | url
	Options  []string          `json:"options,omitempty"`
	Default  string            `json:"default,omitempty"`
	Required bool              `json:"required,omitempty"`
}

// Heading is a heading of the page (used by the table of contents).
type Heading struct {
	Level int
	ID    string
	Text  string
}

// MacroContext is passed to Macro.Render.
type MacroContext struct {
	RenderContext
	Params   map[string]string
	Body     string // sanitized inner HTML (macros inside are already rendered)
	Headings []Heading
}

// Macro is implemented by macro plugins.
type Macro interface {
	Name() string // e.g. "toc"
	Labels() map[string]string
	Description() map[string]string
	Params() []MacroParam
	HasBody() bool // the macro wraps content (panels, sections, ...)
	Render(ctx context.Context, mc MacroContext) (string, error)
}

// MacroInfo describes a macro for the editor.
type MacroInfo struct {
	Name        string            `json:"name"`
	Plugin      string            `json:"plugin"`
	Labels      map[string]string `json:"labels"`
	Description map[string]string `json:"description"`
	Params      []MacroParam      `json:"params"`
	HasBody     bool              `json:"has_body"`
}

type macroRegistry struct {
	mu     sync.RWMutex
	macros map[string]registeredMacro
}

type registeredMacro struct {
	pluginID string
	m        Macro
}

var macroNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// RegisterMacro registers a macro owned by pluginID.
func (s *Service) RegisterMacro(pluginID string, m Macro) {
	if !macroNameRe.MatchString(m.Name()) {
		panic("invalid macro name " + m.Name())
	}
	s.macros.mu.Lock()
	defer s.macros.mu.Unlock()
	if s.macros.macros == nil {
		s.macros.macros = map[string]registeredMacro{}
	}
	s.macros.macros[m.Name()] = registeredMacro{pluginID, m}
}

func (s *Service) macro(name string) Macro {
	s.macros.mu.RLock()
	defer s.macros.mu.RUnlock()
	r, ok := s.macros.macros[name]
	if !ok || !s.enabled(r.pluginID) {
		return nil
	}
	return r.m
}

// Macros lists the active macros.
func (s *Service) Macros() []MacroInfo {
	s.macros.mu.RLock()
	defer s.macros.mu.RUnlock()
	out := []MacroInfo{}
	for name, r := range s.macros.macros {
		if s.enabled(r.pluginID) {
			p := r.m.Params()
			if p == nil {
				p = []MacroParam{}
			}
			out = append(out, MacroInfo{Name: name, Plugin: r.pluginID, Labels: r.m.Labels(), Description: r.m.Description(), Params: p, HasBody: r.m.HasBody()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ParseMacroParams decodes the data-params attribute.
func ParseMacroParams(s string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(s) == "" || len(s) > 8000 {
		return out
	}
	var raw map[string]any
	if json.Unmarshal([]byte(s), &raw) != nil {
		return out
	}
	for k, v := range raw {
		switch x := v.(type) {
		case string:
			out[k] = x
		case nil:
		default:
			out[k] = fmt.Sprint(x)
		}
	}
	return out
}

func attr(n *nethtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func setAttr(n *nethtml.Node, key, val string) {
	for i, a := range n.Attr {
		if a.Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, nethtml.Attribute{Key: key, Val: val})
}

func textOf(n *nethtml.Node) string {
	var b strings.Builder
	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

var nonSlug = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// RenderMacros renders all macros in sanitized HTML and gives headings
// stable ids (for anchors and the table of contents).
func (s *Service) RenderMacros(ctx context.Context, rc RenderContext, src string) string {
	ctxNode := &nethtml.Node{Type: nethtml.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := nethtml.ParseFragment(strings.NewReader(src), ctxNode)
	if err != nil {
		return src
	}
	root := &nethtml.Node{Type: nethtml.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	// headings: ensure unique ids
	var headings []Heading
	used := map[string]int{}
	var walkH func(*nethtml.Node)
	walkH = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode && len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6' {
			txt := textOf(n)
			id := attr(n, "id")
			if id == "" {
				id = strings.Trim(strings.ToLower(nonSlug.ReplaceAllString(txt, "-")), "-")
				if id == "" {
					id = "section"
				}
			}
			if c := used[id]; c > 0 {
				used[id] = c + 1
				id = fmt.Sprintf("%s-%d", id, c+1)
			} else {
				used[id] = 1
			}
			setAttr(n, "id", id)
			headings = append(headings, Heading{Level: int(n.Data[1] - '0'), ID: id, Text: txt})
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkH(c)
		}
	}
	walkH(root)
	if !strings.Contains(src, "data-macro") {
		return renderChildren(root)
	}
	count := 0
	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			walk(c) // inner macros first
			c = next
		}
		if n.Type != nethtml.ElementNode || (n.DataAtom != atom.Div && n.DataAtom != atom.Span) {
			return
		}
		name := attr(n, "data-macro")
		if name == "" {
			return
		}
		count++
		var out string
		m := s.macro(name)
		switch {
		case count > 200:
			out = macroNote("too many macros on this page")
		case m == nil:
			out = macroNote(fmt.Sprintf("macro %q is not available", name))
		default:
			body := renderChildren(n)
			params := ParseMacroParams(attr(n, "data-params"))
			if _, ok := params["text"]; !ok && n.DataAtom == atom.Span {
				params["text"] = textOf(n) // :status[DONE]: the label is the text
			}
			res, err := m.Render(ctx, MacroContext{RenderContext: rc, Params: params, Body: body, Headings: headings})
			if err != nil {
				out = macroNote(fmt.Sprintf("macro %s: %s", name, err.Error()))
			} else {
				tag := "div"
				if n.DataAtom == atom.Span {
					tag = "span" // inline macro
				}
				out = `<` + tag + ` class="ew-macro ew-macro--` + html.EscapeString(name) + `">` + res + `</` + tag + `>`
			}
		}
		repl, err := nethtml.ParseFragment(strings.NewReader(out), ctxNode)
		if err != nil {
			return
		}
		for _, r := range repl {
			n.Parent.InsertBefore(r, n)
		}
		n.Parent.RemoveChild(n)
	}
	walk(root)
	return renderChildren(root)
}

func macroNote(msg string) string {
	return `<p class="ew-macro-error" role="note">` + html.EscapeString(msg) + `</p>`
}

func renderChildren(n *nethtml.Node) string {
	var b bytes.Buffer
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		_ = nethtml.Render(&b, c)
	}
	return b.String()
}
