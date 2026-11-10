package markdown

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// FromHTML converts (sanitized) HTML of the old editor to Markdown with
// macro directives. What Markdown cannot express (merged table cells,
// underline, figures with captions ...) stays as inline HTML, which GFM
// allows and the renderer sanitizes.
func FromHTML(src string) string {
	nodes, err := html.ParseFragment(strings.NewReader(src), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		return src
	}
	c := &conv{}
	root := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	out := c.blocks(root, 3)
	return strings.TrimSpace(collapseBlank.ReplaceAllString(out, "\n\n")) + "\n"
}

var collapseBlank = regexp.MustCompile(`\n{3,}`)

type conv struct{}

func attr(n *html.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}

func isBlock(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.DataAtom {
	case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Ul, atom.Ol, atom.Blockquote,
		atom.Pre, atom.Hr, atom.Table, atom.Div, atom.Figure, atom.Section, atom.Article, atom.Details, atom.Dl:
		return true
	}
	return false
}

// blocks converts the children of n to block Markdown. colons is the fence
// length for macros at this level (outer macros get more).
func (c *conv) blocks(n *html.Node, colons int) string {
	var b strings.Builder
	var inline []*html.Node
	flush := func() {
		if len(inline) == 0 {
			return
		}
		txt := strings.TrimSpace(c.inlines(inline))
		if txt != "" {
			b.WriteString(txt + "\n\n")
		}
		inline = nil
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if !isBlock(ch) {
			inline = append(inline, ch)
			continue
		}
		flush()
		b.WriteString(c.block(ch, colons))
	}
	flush()
	return b.String()
}

func (c *conv) block(n *html.Node, colons int) string {
	switch n.DataAtom {
	case atom.P:
		t := strings.TrimSpace(c.inlines(children(n)))
		if t == "" {
			return ""
		}
		return t + "\n\n"
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		lvl := int(n.Data[1] - '0')
		return strings.Repeat("#", lvl) + " " + strings.TrimSpace(strings.ReplaceAll(c.inlines(children(n)), "\n", " ")) + "\n\n"
	case atom.Hr:
		return "---\n\n"
	case atom.Blockquote:
		inner := strings.TrimSpace(c.blocks(n, colons))
		return prefixLines(inner, "> ") + "\n\n"
	case atom.Pre:
		code, lang := textContent(n), ""
		if ch := firstElement(n); ch != nil && ch.DataAtom == atom.Code {
			lang = strings.TrimPrefix(attr(ch, "class"), "language-")
			if strings.ContainsAny(lang, " `") {
				lang = ""
			}
		}
		fence := "```"
		for strings.Contains(code, fence) {
			fence += "`"
		}
		return fence + lang + "\n" + strings.TrimRight(code, "\n") + "\n" + fence + "\n\n"
	case atom.Ul, atom.Ol:
		return c.list(n, colons) + "\n"
	case atom.Table:
		if t, ok := c.table(n); ok {
			return t + "\n"
		}
		return rawHTML(n) + "\n\n"
	case atom.Div:
		if name := attr(n, "data-macro"); name != "" {
			return c.directive(n, name, colons)
		}
		return c.blocks(n, colons)
	}
	// figure, details, dl, section ...: keep as HTML
	return rawHTML(n) + "\n\n"
}

func (c *conv) directive(n *html.Node, name string, colons int) string {
	params := map[string]string{}
	if p := attr(n, "data-params"); p != "" {
		_ = json.Unmarshal([]byte(p), &params)
	}
	if !nameRe.MatchString(name) || len(nameRe.FindString(name)) != len(name) {
		return ""
	}
	body := strings.TrimSpace(c.blocks(n, 3))
	if body == "" {
		return "::" + name + FormatAttrs(params) + "\n\n"
	}
	// the fence must be longer than every fence inside
	need := colons
	for _, line := range strings.Split(body, "\n") {
		if k := len(line) - len(strings.TrimLeft(line, ":")); k >= need && k >= 3 {
			need = k + 1
		}
	}
	fence := strings.Repeat(":", need)
	return fence + name + FormatAttrs(params) + "\n" + body + "\n" + fence + "\n\n"
}

func (c *conv) list(n *html.Node, colons int) string {
	var b strings.Builder
	i := 1
	if s, err := strconv.Atoi(attr(n, "start")); err == nil && s > 0 {
		i = s
	}
	for li := n.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != html.ElementNode || li.DataAtom != atom.Li {
			continue
		}
		marker := "- "
		if n.DataAtom == atom.Ol {
			marker = strconv.Itoa(i) + ". "
			i++
		}
		task := taskMarker(li)
		body := strings.TrimSpace(c.blocks(li, colons))
		body = strings.ReplaceAll(body, "\n\n", "\n")
		b.WriteString(marker + task + indentRest(body, strings.Repeat(" ", len(marker))) + "\n")
	}
	return b.String()
}

// taskMarker returns "[ ] " or "[x] " for items of task lists: data-checked
// (editor) or a leading checkbox (rendered Markdown, which is removed).
func taskMarker(li *html.Node) string {
	mark := func(on bool) string {
		if on {
			return "[x] "
		}
		return "[ ] "
	}
	switch attr(li, "data-checked") {
	case "true":
		return mark(true)
	case "false":
		return mark(false)
	}
	in := li.FirstChild
	for in != nil && in.Type == html.TextNode && strings.TrimSpace(in.Data) == "" {
		in = in.NextSibling
	}
	if in != nil && in.Type == html.ElementNode && in.DataAtom == atom.P {
		// loose lists: <li><p><input ...> text</p></li>
		in = in.FirstChild
	}
	if in == nil || in.Type != html.ElementNode || in.DataAtom != atom.Input || attr(in, "type") != "checkbox" {
		return ""
	}
	_, checked := attrOK(in, "checked")
	in.Parent.RemoveChild(in)
	return mark(checked)
}

func attrOK(n *html.Node, k string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val, true
		}
	}
	return "", false
}

// table converts a simple table to GFM (no merged cells, no blocks inside).
func (c *conv) table(n *html.Node) (string, bool) {
	var rows [][]string
	header := false
	var walk func(*html.Node) bool
	walk = func(x *html.Node) bool {
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type != html.ElementNode {
				continue
			}
			switch ch.DataAtom {
			case atom.Thead, atom.Tbody, atom.Tfoot:
				if !walk(ch) {
					return false
				}
			case atom.Tr:
				var row []string
				for td := ch.FirstChild; td != nil; td = td.NextSibling {
					if td.Type != html.ElementNode {
						continue
					}
					if td.DataAtom != atom.Td && td.DataAtom != atom.Th {
						return false
					}
					if attr(td, "colspan") != "" || attr(td, "rowspan") != "" {
						return false
					}
					for k := td.FirstChild; k != nil; k = k.NextSibling {
						if isBlock(k) && k.DataAtom != atom.P {
							return false
						}
					}
					if len(rows) == 0 && td.DataAtom == atom.Th {
						header = true
					}
					cell := strings.TrimSpace(c.inlines(children(td)))
					cell = strings.ReplaceAll(cell, "\n", " ") // "|" is escaped in the text already
					row = append(row, cell)
				}
				rows = append(rows, row)
			case atom.Caption, atom.Colgroup:
				return false
			}
		}
		return true
	}
	if !walk(n) || len(rows) == 0 {
		return "", false
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	if cols == 0 {
		return "", false
	}
	line := func(r []string) string {
		for len(r) < cols {
			r = append(r, "")
		}
		return "| " + strings.Join(r, " | ") + " |\n"
	}
	var b strings.Builder
	start := 0
	if header {
		b.WriteString(line(rows[0]))
		start = 1
	} else {
		b.WriteString(line(make([]string, cols)))
	}
	b.WriteString("|" + strings.Repeat(" --- |", cols) + "\n")
	for _, r := range rows[start:] {
		b.WriteString(line(r))
	}
	return b.String(), true
}

// ---------------------------------------------------------------- inline

func children(n *html.Node) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out = append(out, c)
	}
	return out
}

func (c *conv) inlines(ns []*html.Node) string {
	var b strings.Builder
	for _, n := range ns {
		b.WriteString(c.inline(n))
	}
	return b.String()
}

var spaceRe = regexp.MustCompile(`[ \t\r\n]+`)

func (c *conv) inline(n *html.Node) string {
	switch n.Type {
	case html.TextNode:
		return escapeText(spaceRe.ReplaceAllString(n.Data, " "))
	case html.ElementNode:
	default:
		return ""
	}
	in := func() string { return c.inlines(children(n)) }
	wrap := func(m string) string {
		t := in()
		trim := strings.TrimSpace(t)
		if trim == "" {
			return t
		}
		lead, trail := t[:len(t)-len(strings.TrimLeft(t, " "))], t[len(strings.TrimRight(t, " ")):]
		return lead + m + trim + m + trail
	}
	switch n.DataAtom {
	case atom.Strong, atom.B:
		return wrap("**")
	case atom.Em, atom.I:
		return wrap("*")
	case atom.S, atom.Del, atom.Strike:
		return wrap("~~")
	case atom.Code:
		t := textContent(n)
		fence := "`"
		for strings.Contains(t, fence) {
			fence += "`"
		}
		if strings.HasPrefix(t, "`") || strings.HasSuffix(t, "`") {
			return fence + " " + t + " " + fence
		}
		return fence + t + fence
	case atom.Br:
		return "\\\n"
	case atom.A:
		href := attr(n, "href")
		label := in()
		if href == "" {
			return label
		}
		title := attr(n, "title")
		if title != "" {
			return "[" + label + "](" + linkDest(href) + ` "` + strings.ReplaceAll(title, `"`, `\"`) + `")`
		}
		return "[" + label + "](" + linkDest(href) + ")"
	case atom.Img:
		return "![" + escapeText(attr(n, "alt")) + "](" + linkDest(attr(n, "src")) + ")"
	case atom.Span:
		if name := attr(n, "data-macro"); name != "" && nameRe.FindString(name) == name {
			params := map[string]string{}
			_ = json.Unmarshal([]byte(attr(n, "data-params")), &params)
			label := textContent(n)
			if t, ok := params["text"]; ok && strings.TrimSpace(label) == "" {
				label = t
				delete(params, "text")
			}
			label = strings.ReplaceAll(label, "]", `\]`)
			return ":" + name + "[" + label + "]" + FormatAttrs(params)
		}
		return in()
	}
	if isBlock(n) {
		return rawHTML(n)
	}
	// u, sub, sup, mark, kbd, abbr ...: inline HTML
	return rawHTML(n)
}

var mdSpecial = strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`, "~", `\~`, "|", `\|`)

// leading markers that would start a block (#, -, +, 1., :::)
var blockStart = regexp.MustCompile(`^(\s*)([#+-]|\d+\.|::)`)

// inline directives (:name[ or :name{) must not appear by accident
var inlineDir = regexp.MustCompile(`(^|[^\pL\pN_:/]):([a-z])`)

func escapeText(s string) string {
	s = mdSpecial.Replace(s)
	s = inlineDir.ReplaceAllString(s, `$1\:$2`)
	return blockStart.ReplaceAllString(s, `$1\$2`)
}

func linkDest(u string) string {
	if strings.ContainsAny(u, " ()<>") {
		return "<" + strings.NewReplacer("<", "%3C", ">", "%3E").Replace(u) + ">"
	}
	return u
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func firstElement(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			return c
		}
	}
	return nil
}

func rawHTML(n *html.Node) string {
	var b strings.Builder
	_ = html.Render(&b, n)
	return b.String()
}

func prefixLines(s, p string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = strings.TrimRight(p, " ")
		} else {
			lines[i] = p + l
		}
	}
	return strings.Join(lines, "\n")
}

func indentRest(s, pad string) string {
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			lines[i] = pad + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
