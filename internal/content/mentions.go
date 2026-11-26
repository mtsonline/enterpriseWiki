package content

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// mentionRe finds @name and @name@provider (after a space, a bracket, a
// quote or punctuation, so e-mail addresses do not count).
var mentionRe = regexp.MustCompile(`(?:^|[\s(\[{>"'„“,;:])@([\p{L}\p{N}][\p{L}\p{N}._-]{0,63}(?:@[\p{L}\p{N}._-]{1,64})?)`)

// Mentions returns the people mentioned in a text (@name, @name@provider;
// lower case, without duplicates, at most max).
func Mentions(text string, max int) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		h := strings.TrimRight(strings.ToLower(m[1]), ".-_")
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
		if len(out) >= max {
			break
		}
	}
	return out
}

// mentionSkip are the elements whose text never holds a mention to show as
// a person: code (an "@" there is code), links, scripts and fields.
var mentionSkip = map[string]bool{"code": true, "pre": true, "kbd": true, "samp": true, "a": true,
	"script": true, "style": true, "textarea": true, "button": true, "select": true, "option": true}

// mentionMask is what readers see of a mention of a person they may not
// find (setting people.mention_display "masked"): the first two characters
// and a fixed run of stars, which tells neither the rest nor the length.
func mentionMask(handle string) string {
	r := []rune(handle)
	if len(r) > 2 {
		r = r[:2]
	}
	return "@" + string(r) + "****"
}

// renderMentions shows the mentions in the text of rendered content as the
// reader may see them (decision of the project owner, 26.11.2026):
//
//   - a reader who may find the person (the rules of PeopleRefs) sees the
//     display name with the menu on names (span[data-person], turned into
//     the menu by the interface);
//   - every other reader – guests, people outside the person's groups –
//     sees "@na****" (default) or, with people.mention_display "name",
//     the display name as plain text without the menu;
//   - a name that is no person is masked the same way, so the text does
//     not tell who exists.
//
// Without this, a writer could put "@someone" into a page and read the
// display name in the preview, bypassing the rules for finding people; and
// guests would see handles, which are often e-mail addresses. Two queries
// for all mentions of all roots together.
func (s *Service) renderMentions(ctx context.Context, roots ...*nethtml.Node) {
	type hit struct {
		node       *nethtml.Node
		start, end int // the mention in node.Data, "@" included
		handle     string
	}
	var hits []hit
	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode && (mentionSkip[n.Data] || attr(n, "data-person") != "") {
			return
		}
		if n.Type == nethtml.TextNode && strings.Contains(n.Data, "@") {
			for _, m := range mentionRe.FindAllStringSubmatchIndex(n.Data, -1) {
				handle := strings.TrimRight(n.Data[m[2]:m[3]], ".-_")
				if handle == "" {
					continue
				}
				hits = append(hits, hit{node: n, start: m[2] - 1, end: m[2] + len(handle), handle: handle})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, root := range roots {
		walk(root)
	}
	if len(hits) == 0 {
		return
	}
	var names []string
	for _, h := range hits {
		names = append(names, h.handle)
	}
	if len(names) > PeopleNamesMax {
		names = names[:PeopleNamesMax]
	}
	ids, err := UserIDsByName(ctx, s.pool, names)
	if err != nil {
		s.log.Warn("mentions: looking up people failed", "err", err)
		ids = map[string]int64{}
	}
	var list []int64
	for _, id := range ids {
		list = append(list, id)
	}
	refs, err := s.PeopleRefs(ctx, list)
	if err != nil {
		s.log.Warn("mentions: names of people failed", "err", err)
		refs = map[int64]PersonRef{}
	}
	showNames := s.cfg.Get().People.Mentions() == "name"
	// replace from the end of each text node, so earlier offsets stay valid
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		ref, known := refs[ids[strings.ToLower(h.handle)]]
		var repl *nethtml.Node
		switch {
		case known && ref.ID != nil && ref.Name != "":
			repl = &nethtml.Node{Type: nethtml.ElementNode, Data: "span", DataAtom: atom.Span,
				Attr: []nethtml.Attribute{{Key: "class", Val: "ew-mention"}, {Key: "data-person", Val: strconv.FormatInt(*ref.ID, 10)}}}
			repl.AppendChild(&nethtml.Node{Type: nethtml.TextNode, Data: ref.Name})
		case known && showNames && ref.Name != "":
			repl = &nethtml.Node{Type: nethtml.ElementNode, Data: "span", DataAtom: atom.Span,
				Attr: []nethtml.Attribute{{Key: "class", Val: "ew-mention"}}}
			repl.AppendChild(&nethtml.Node{Type: nethtml.TextNode, Data: ref.Name})
		default:
			repl = &nethtml.Node{Type: nethtml.TextNode, Data: mentionMask(h.handle)}
		}
		n := h.node
		after := &nethtml.Node{Type: nethtml.TextNode, Data: n.Data[h.end:]}
		n.Data = n.Data[:h.start]
		if next := n.NextSibling; next != nil {
			n.Parent.InsertBefore(repl, next)
			n.Parent.InsertBefore(after, next)
		} else {
			n.Parent.AppendChild(repl)
			n.Parent.AppendChild(after)
		}
	}
}

// MentionsHTML shows the mentions in pieces of sanitized HTML (for example
// the comments of a page) as the reader may see them, like the content of
// pages (see renderMentions); all pieces with the same two queries.
func (s *Service) MentionsHTML(ctx context.Context, pieces []string) []string {
	ctxNode := &nethtml.Node{Type: nethtml.ElementNode, Data: "div", DataAtom: atom.Div}
	roots := make([]*nethtml.Node, len(pieces))
	for i, src := range pieces {
		root := &nethtml.Node{Type: nethtml.ElementNode, Data: "div", DataAtom: atom.Div}
		if strings.Contains(src, "@") {
			nodes, err := nethtml.ParseFragment(strings.NewReader(src), ctxNode)
			if err != nil {
				continue
			}
			for _, n := range nodes {
				root.AppendChild(n)
			}
		}
		roots[i] = root
	}
	var parsed []*nethtml.Node
	for _, r := range roots {
		if r.FirstChild != nil {
			parsed = append(parsed, r)
		}
	}
	s.renderMentions(ctx, parsed...)
	out := make([]string, len(pieces))
	for i, r := range roots {
		if r.FirstChild == nil {
			out[i] = pieces[i] // no mention, unchanged
			continue
		}
		out[i] = renderChildren(r)
	}
	return out
}

// MaskMentions masks every mention in plain text ("@bob" becomes
// "@bo****"), for text that goes to people one by one without knowing whom
// each of them may find: the excerpt in the notification about a mention.
func MaskMentions(text string) string {
	return mentionRe.ReplaceAllStringFunc(text, func(m string) string {
		at := strings.IndexByte(m, '@')
		handle := strings.TrimRight(m[at+1:], ".-_")
		return m[:at] + mentionMask(handle) + m[at+1+len(handle):]
	})
}
