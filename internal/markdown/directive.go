// Package markdown renders the Markdown of pages (GitHub Flavored Markdown
// plus macro directives) and converts stored HTML to Markdown.
//
// Macros are written as directives (as in the generic directives proposal
// used by remark-directive):
//
//	::toc{levels=3}                       a macro without content
//	:::panel{type=info title="Note"}      a macro with Markdown content,
//	Text **inside** the panel.            closed by the same number of colons
//	:::
//	::::columns                           nesting: the outer one has more colons
//	:::panel{type=info}
//	...
//	:::
//	::::
//	Status :status[DONE]{color=green}     inline macro with a label
//
// They are rendered as <div|span data-macro="name" data-params='{...}'>,
// which the macro engine of the wiki renders with the viewer's rights.
package markdown

import (
	"encoding/json"
	"html"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer"
	gmhtml "github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}`)

// ParseAttrs parses {key=value key2="quoted value"} (without braces).
func ParseAttrs(s string) (map[string]string, bool) {
	out := map[string]string{}
	i := 0
	for {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			return out, true
		}
		j := i
		for j < len(s) && (s[j] == '_' || s[j] == '-' || s[j] == '.' || s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= '0' && s[j] <= '9') {
			j++
		}
		if j == i || j >= len(s) || s[j] != '=' {
			return nil, false
		}
		key := s[i:j]
		j++
		var val strings.Builder
		if j < len(s) && s[j] == '"' {
			j++
			closed := false
			for j < len(s) {
				c := s[j]
				if c == '\\' && j+1 < len(s) {
					val.WriteByte(s[j+1])
					j += 2
					continue
				}
				if c == '"' {
					closed = true
					j++
					break
				}
				val.WriteByte(c)
				j++
			}
			if !closed {
				return nil, false
			}
		} else {
			for j < len(s) && s[j] != ' ' && s[j] != '\t' {
				if s[j] == '"' || s[j] == '}' {
					return nil, false
				}
				val.WriteByte(s[j])
				j++
			}
		}
		out[key] = val.String()
		i = j
	}
}

// FormatAttrs writes params as {key=value ...} ("" for none), sorted.
func FormatAttrs(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		v := params[k]
		b.WriteString(k + "=")
		if v != "" && !strings.ContainsAny(v, " \t\"}\\\n") {
			b.WriteString(v)
		} else {
			b.WriteString(`"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(v) + `"`)
		}
	}
	b.WriteByte('}')
	return b.String()
}

// header parses "name{attrs}" at the start of s; rest is what follows.
func header(s string) (name string, params map[string]string, rest string, ok bool) {
	name = nameRe.FindString(s)
	if name == "" {
		return "", nil, "", false
	}
	s = s[len(name):]
	params = map[string]string{}
	if strings.HasPrefix(s, "{") {
		end := attrEnd(s)
		if end < 0 {
			return "", nil, "", false
		}
		if params, ok = ParseAttrs(s[1:end]); !ok {
			return "", nil, "", false
		}
		s = s[end+1:]
	}
	return name, params, s, true
}

// attrEnd finds the closing brace of {...} (quotes respected).
func attrEnd(s string) int {
	q := false
	for i := 1; i < len(s); i++ {
		switch {
		case q && s[i] == '\\':
			i++
		case s[i] == '"':
			q = !q
		case !q && s[i] == '}':
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------- AST

// KindDirective is the node kind of block macro directives.
var KindDirective = ast.NewNodeKind("Directive")

// Directive is a block macro (leaf or container) in the document.
type Directive struct {
	ast.BaseBlock
	Name   string
	Params map[string]string
	Colons int  // container: number of colons of the fence
	Leaf   bool // ::name{...}
}

// Kind implements ast.Node.
func (n *Directive) Kind() ast.NodeKind { return KindDirective }

// Dump implements ast.Node.
func (n *Directive) Dump(_ []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, map[string]any{"Name": n.Name})
}

func newDirective(name string, params map[string]string, colons int, leaf bool) *Directive {
	n := &Directive{Name: name, Params: params, Colons: colons, Leaf: leaf}
	n.Init(n)
	return n
}

// KindInlineDirective is the node kind of inline macro directives.
var KindInlineDirective = ast.NewNodeKind("InlineDirective")

// InlineDirective is :name[label]{...}.
type InlineDirective struct {
	ast.BaseInline
	Name   string
	Params map[string]string
	Label  string
}

// Kind implements ast.Node.
func (n *InlineDirective) Kind() ast.NodeKind { return KindInlineDirective }

// Dump implements ast.Node.
func (n *InlineDirective) Dump(_ []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, map[string]any{"Name": n.Name, "Label": n.Label})
}

// ---------------------------------------------------------------- block parser

type blockParser struct{}

func (blockParser) Trigger() []byte { return []byte{':'} }

func (blockParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	w, pos := util.IndentWidth(line, reader.LineOffset())
	if w > 3 {
		return nil, parser.NoChildren
	}
	s := string(util.TrimRightSpace(line[pos:]))
	colons := 0
	for colons < len(s) && s[colons] == ':' {
		colons++
	}
	if colons < 2 {
		return nil, parser.NoChildren
	}
	name, params, rest, ok := header(strings.TrimLeft(s[colons:], " "))
	if !ok || strings.TrimSpace(rest) != "" {
		return nil, parser.NoChildren
	}
	reader.AdvanceToEOL()
	if colons == 2 {
		return newDirective(name, params, 0, true), parser.NoChildren
	}
	return newDirective(name, params, colons, false), parser.HasChildren
}

func (blockParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	d := node.(*Directive)
	if d.Leaf {
		return parser.Close
	}
	line, _ := reader.PeekLine()
	w, pos := util.IndentWidth(line, reader.LineOffset())
	if w <= 3 {
		s := string(util.TrimRightSpace(line[pos:]))
		if len(s) == d.Colons && strings.Trim(s, ":") == "" {
			reader.AdvanceToEOL()
			return parser.Close
		}
	}
	return parser.Continue | parser.HasChildren
}

func (blockParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {}
func (blockParser) CanInterruptParagraph() bool                                { return true }
func (blockParser) CanAcceptIndentedLine() bool                                { return false }

// ---------------------------------------------------------------- inline parser

type inlineParser struct{}

func (inlineParser) Trigger() []byte { return []byte{':'} }

func (inlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 3 || line[0] != ':' {
		return nil
	}
	// not inside a word (10:30, https://, foo:bar)
	if prev := block.PrecedingCharacter(); prev == '_' || prev == ':' || prev == '/' || unicode.IsLetter(prev) || unicode.IsDigit(prev) {
		return nil
	}
	s := string(line[1:])
	name := nameRe.FindString(s)
	if name == "" {
		return nil
	}
	s = s[len(name):]
	consumed := 1 + len(name)
	label := ""
	hasLabel := false
	if strings.HasPrefix(s, "[") {
		depth, end := 0, -1
		for i := 0; i < len(s) && end < 0; i++ {
			switch s[i] {
			case '\\':
				i++
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					end = i
				}
			}
		}
		if end < 0 {
			return nil
		}
		label, hasLabel = s[1:end], true
		s = s[end+1:]
		consumed += end + 1
	}
	params := map[string]string{}
	if strings.HasPrefix(s, "{") {
		end := attrEnd(s)
		if end < 0 {
			return nil
		}
		p, ok := ParseAttrs(s[1:end])
		if !ok {
			return nil
		}
		params = p
		consumed += end + 1
	} else if !hasLabel {
		return nil // ":word" alone is text
	}
	block.Advance(consumed)
	n := &InlineDirective{Name: name, Params: params, Label: strings.ReplaceAll(label, `\]`, "]")}
	n.Init(n)
	return n
}

// ---------------------------------------------------------------- renderer

func paramsAttr(p map[string]string) string {
	if len(p) == 0 {
		return ""
	}
	b, _ := json.Marshal(p)
	return " data-params=\"" + html.EscapeString(string(b)) + "\""
}

func renderBlock(w io.Writer, _ []byte, n ast.Node, entering bool, _ renderer.Context) (ast.WalkStatus, error) {
	d := n.(*Directive)
	var err error
	if entering {
		_, err = io.WriteString(w, "<div data-macro=\""+html.EscapeString(d.Name)+"\""+paramsAttr(d.Params)+">")
	} else {
		_, err = io.WriteString(w, "</div>\n")
	}
	return ast.WalkContinue, err
}

func renderInline(w io.Writer, _ []byte, n ast.Node, entering bool, _ renderer.Context) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	d := n.(*InlineDirective)
	_, err := io.WriteString(w, "<span data-macro=\""+html.EscapeString(d.Name)+"\""+paramsAttr(d.Params)+">"+html.EscapeString(d.Label)+"</span>")
	return ast.WalkSkipChildren, err
}

// ParserExtension adds macro directives to the goldmark parser.
type ParserExtension struct{}

// ParserOptions implements parser.Extension.
func (ParserExtension) ParserOptions(*parser.Config) []parser.Option {
	return []parser.Option{
		parser.WithBlockParsers(util.Prioritized[parser.BlockParser](blockParser{}, 90)),
		parser.WithInlineParsers(util.Prioritized[parser.InlineParser](inlineParser{}, 90)),
	}
}

// RendererExtension renders macro directives as macro markup.
type RendererExtension struct{}

// RendererOptions implements html.Extension.
func (RendererExtension) RendererOptions(*gmhtml.Config) []gmhtml.Option {
	return []gmhtml.Option{gmhtml.WithNodeRenderers(map[ast.NodeKind]gmhtml.NodeRenderer{
		KindDirective:       gmhtml.NodeRendererFunc(renderBlock),
		KindInlineDirective: gmhtml.NodeRendererFunc(renderInline),
	})}
}
