package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
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
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
	// text | textarea | number | select | bool | url, or custom:<name> for
	// a control a plugin registers in the browser (registerMacroParamControl)
	Type    string   `json:"type"`
	Options []string `json:"options,omitempty"`
	// OptionLabels: labels of the options by language
	OptionLabels map[string]map[string]string `json:"option_labels,omitempty"`
	Default      string                       `json:"default,omitempty"`
	Required     bool                         `json:"required,omitempty"`
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
	// Index is the number of the macro on its element (1 …) and Page the
	// page of its list a reader asked for (see Paging).
	Index int
	Page  int
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

// InlineMacro is implemented by macros that are used inside a line of text
// (:name[label]{...}); the label is the parameter "text".
type InlineMacro interface {
	Inline() bool
}

// ToolbarMacro is implemented by macros that get an own button in the
// toolbar of the editor; Toolbar returns its icon (a short text or emoji).
type ToolbarMacro interface {
	Toolbar() string
}

// GroupedMacro is implemented by macros that name their group in the menu
// of the editor (one of MacroGroups); others are in "other".
type GroupedMacro interface {
	Group() string
}

// MacroGroups are the groups of blocks in the menu of the editor, in their
// order there.
var MacroGroups = []string{"text", "layout", "navigation", "media", "lists", "data", "forms", "automation", "other"}

// PartsMacro is implemented by macros whose body is split into parts at
// horizontal rules (tabs, accordion, columns, cards …). The editor shows the
// parts and a button to add one.
type PartsMacro interface {
	Parts() MacroParts
}

// MacroParts describes the parts of a macro for the editor.
type MacroParts struct {
	// Labels name one part ("Tab", "Column" …) by language.
	Labels map[string]string `json:"labels"`
	// Titled: the first heading of a part is its title (a new part starts
	// with a heading).
	Titled bool `json:"titled"`
}

// MacroInfo describes a macro for the editor.
type MacroInfo struct {
	Name        string            `json:"name"`
	Plugin      string            `json:"plugin"`
	Labels      map[string]string `json:"labels"`
	Description map[string]string `json:"description"`
	Params      []MacroParam      `json:"params"`
	HasBody     bool              `json:"has_body"`
	Inline      bool              `json:"inline"`
	// Group of the block in the menu of the editor (MacroGroups).
	Group string `json:"group"`
	// Toolbar is the icon of the own toolbar button ("" = none).
	Toolbar string `json:"toolbar,omitempty"`
	// Parts: the body is split into parts at horizontal rules (nil = not).
	Parts *MacroParts `json:"parts,omitempty"`
	// Picture: the visual editor shows the result of the macro (one
	// without a body) while editing.
	Picture bool `json:"picture,omitempty"`
}

type macroRegistry struct {
	mu     sync.RWMutex
	macros map[string]registeredMacro
}

type registeredMacro struct {
	pluginID string
	m        Macro
	opts     MacroOptions
}

// MacroOptions are set by the plugin when it registers a macro.
type MacroOptions struct {
	// Picture: the visual editor shows the result while editing (cheap,
	// without side effects; see coreplugin.Host.RegisterPictureMacro).
	Picture bool
}

var macroNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// RegisterMacro registers a macro owned by pluginID.
func (s *Service) RegisterMacro(pluginID string, m Macro) {
	s.RegisterMacroWith(pluginID, m, MacroOptions{})
}

// RegisterMacroWith registers a macro owned by pluginID with options.
func (s *Service) RegisterMacroWith(pluginID string, m Macro, opts MacroOptions) {
	if !macroNameRe.MatchString(m.Name()) {
		panic("invalid macro name " + m.Name())
	}
	s.macros.mu.Lock()
	defer s.macros.mu.Unlock()
	if s.macros.macros == nil {
		s.macros.macros = map[string]registeredMacro{}
	}
	s.macros.macros[m.Name()] = registeredMacro{pluginID, m, opts}
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
			p = withOptionLabels(p) // a copy: the macro's own parameters stay as they are
			info := MacroInfo{Name: name, Plugin: r.pluginID, Labels: r.m.Labels(), Description: r.m.Description(), Params: p, HasBody: r.m.HasBody()}
			if im, ok := r.m.(InlineMacro); ok {
				info.Inline = im.Inline() && !info.HasBody
			}
			// every block in the content has the options of blocks
			if !info.Inline && !slices.ContainsFunc(p, func(x MacroParam) bool { return x.Name == OptionsParam }) {
				info.Params = append(slices.Clip(p), MacroParam{Name: OptionsParam, Labels: map[string]string{"en": "Options", "de": "Optionen"}, Type: "custom:blockoptions"})
			}
			if tm, ok := r.m.(ToolbarMacro); ok {
				info.Toolbar = tm.Toolbar()
			}
			info.Group = "other"
			if gm, ok := r.m.(GroupedMacro); ok && slices.Contains(MacroGroups, gm.Group()) {
				info.Group = gm.Group()
			}
			info.Picture = r.opts.Picture && !info.HasBody && !info.Inline
			if pm, ok := r.m.(PartsMacro); ok && info.HasBody {
				parts := pm.Parts()
				info.Parts = &parts
			}
			out = append(out, info)
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
	// one budget of included pages and one set of ids for everything shown
	// together (content blocks, previews … render without a page around them)
	ctx = s.withIncludeBudget(ctx)
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
	taken := map[string]bool{} // ids of this body (without a page being rendered, ClaimID knows none)
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
			// never an id of the interface (the skip link jumps to #main,
			// fields and dialogs have ids starting with "ew", see interfaceID)
			if interfaceID(id) {
				id = "h-" + id
			}
			// unique on the whole page shown, also with included pages and
			// anchors (one set of ids per page, see ClaimID)
			base := id
			for c := used[base]; ; c++ {
				if c > 0 {
					id = fmt.Sprintf("%s-%d", base, c+1)
				}
				if !taken[id] && ClaimID(ctx, id) {
					used[base] = c + 1
					taken[id] = true
					break
				}
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
	allowed := s.spaceMacros(ctx, rc.CategoryID) // the space may limit the macros
	mp, _ := ctx.Value(macroPageKey{}).(*macroPage)
	if mp != nil && (rc.ElementID <= 0 || mp.element != rc.ElementID) {
		mp = nil // a page of another element (e.g. one shown inside this one)
	}
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
		// inside a line (an inline macro) the note is inline too: a <p>
		// inside a paragraph would break the paragraph
		note := macroNote
		if n.DataAtom == atom.Span {
			note = func(msg string) string {
				return `<span class="ew-macro-error" role="note">` + html.EscapeString(msg) + `</span>`
			}
		}
		m := s.macro(name)
		page := 0
		if mp != nil && mp.index == count {
			page = mp.page
		}
		switch {
		case mp != nil && mp.only && mp.index != count:
			// only one macro is asked for: leave the others out
		case count > 200:
			out = note(apperr.Localized(apperr.BadRequest("too many blocks on this page"), rc.Lang))
		case m == nil:
			out = note(apperr.Localized(apperr.BadRequest("block %q is not available", name), rc.Lang))
		case len(allowed) > 0 && !slices.Contains(allowed, name):
			out = note(apperr.Localized(apperr.BadRequest("block %q is not available in this space", name), rc.Lang))
		default:
			body := renderChildren(n)
			params := ParseMacroParams(attr(n, "data-params"))
			if _, ok := params["text"]; !ok && n.DataAtom == atom.Span {
				params["text"] = textOf(n) // :status[DONE]: the label is the text
			}
			res, err := m.Render(ctx, MacroContext{RenderContext: rc, Params: params, Body: body, Headings: headings, Index: count, Page: page})
			if err != nil {
				// in the language of the page: "Makro Akkordeon: …"
				label := m.Labels()[rc.Lang]
				if label == "" {
					label = name
				}
				out = note(apperr.Localized(apperr.BadRequest("block %s: %s", label, apperr.Localized(err, rc.Lang)), rc.Lang))
			} else {
				tag := "div"
				if n.DataAtom == atom.Span {
					tag = "span" // inline macro
				}
				if tag == "div" {
					res = withBlockOptions(res, params[OptionsParam], rc.Lang)
				}
				out = `<` + tag + ` class="ew-macro ew-macro--` + html.EscapeString(name) + `" data-macro-index="` + strconv.Itoa(count) + `">` + res + `</` + tag + `>`
			}
			if mp != nil && mp.only && mp.out != nil {
				*mp.out = out
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

// OptionsParam is the parameter of every block in the content that holds
// its options (JSON, blockoptions.json).
const OptionsParam = "options"

// withBlockOptions puts a rendered block into a frame with its options; the
// browser builds the look from them (CSS only from values of a valid form).
// Options the server does not accept are not used, and the block says so.
func withBlockOptions(res, raw, lang string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return res
	}
	o, err := DecodeBlockOptions(raw)
	if err != nil {
		return macroNote(apperr.Localized(err, lang)) + res
	}
	if err := CheckBlockOptions(o); err != nil {
		return macroNote(apperr.Localized(err, lang)) + res
	}
	if len(o) == 0 {
		return res
	}
	canon, _ := json.Marshal(o)
	return `<div class="ew-bo" data-ewbo="` + html.EscapeString(string(canon)) + `"><div class="ew-bo-body">` + res + `</div></div>`
}
