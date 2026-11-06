package markdown

import (
	"bytes"

	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	gmhtml "github.com/yuin/goldmark/v2/renderer/html"
)

// Markdown of pages: GitHub Flavored Markdown with macro directives. Raw
// HTML is allowed (GFM) because the result is always sanitized afterwards.
var (
	mdParser = parser.New(
		parser.WithExtensions(extension.NewGFMParser(), ParserExtension{}),
		parser.WithAutoHeadingID(),
	)
	mdRenderer = gmhtml.New(
		gmhtml.WithExtensions(extension.NewGFMHTMLRenderer(), RendererExtension{}),
		gmhtml.WithUnsafe(),
	)
)

// ToHTML renders Markdown to (unsanitized) HTML.
func ToHTML(src string) (string, error) {
	var buf bytes.Buffer
	b := []byte(src)
	if err := mdRenderer.Render(&buf, b, mdParser.Parse(b)); err != nil {
		return "", err
	}
	return buf.String(), nil
}
