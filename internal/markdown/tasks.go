package markdown

import (
	"bytes"
	"errors"
	"strconv"

	gast "github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/text"
)

// ErrNoTask: the text has no task item with this number.
var ErrNoTask = errors.New("no task item with this number")

// SetTask sets the task item n (1 = the first "- [ ]" of the text, in the
// order of the rendered page, nested items included) to done or not done.
// Only the one character between the brackets changes; everything else of
// the text stays as it is.
func SetTask(src string, n int, done bool) (string, error) {
	b := []byte(src)
	at := -1
	i := 0
	_ = gast.Walk(mdParser.Parse(b), func(node gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering || at >= 0 || !extension.IsTask(node) {
			return gast.WalkContinue, nil
		}
		i++
		if i != n {
			return gast.WalkContinue, nil
		}
		if p, ok := node.FirstChild().(gast.BlockNode); ok && len(p.Source()) > 0 {
			s := p.Source()[0].Start
			if s+2 < len(b) && b[s] == '[' && b[s+2] == ']' {
				at = s + 1
			}
		}
		return gast.WalkStop, nil
	})
	if at < 0 {
		return "", ErrNoTask
	}
	if done {
		b[at] = 'x'
	} else {
		b[at] = ' '
	}
	return string(b), nil
}

// ToHTMLTasks renders Markdown like ToHTML and gives every task item the
// class "<prefix><n>" (n as for SetTask). With a prefix nobody can guess,
// a task item in the sanitized HTML is told apart from a checkbox written
// as HTML into the text.
func ToHTMLTasks(src, prefix string) (string, error) {
	var buf bytes.Buffer
	b := []byte(src)
	doc := mdParser.Parse(b)
	i := 0
	_ = gast.Walk(doc, func(node gast.Node, entering bool) (gast.WalkStatus, error) {
		if entering && extension.IsTask(node) {
			i++
			node.SetAttribute("class", text.NewMultiLineValueFromString(prefix+strconv.Itoa(i), text.IdentityDecoder))
		}
		return gast.WalkContinue, nil
	})
	if err := mdRenderer.Render(&buf, b, doc); err != nil {
		return "", err
	}
	return buf.String(), nil
}
