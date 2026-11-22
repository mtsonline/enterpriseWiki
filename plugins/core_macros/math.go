package core_macros

// A formula (plan of blocks, 3.2): TeX written as text by the server; the
// browser sets it as MathML (Temml, MIT) from a fixed list of elements and
// attributes. Without script the TeX stays readable.

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

// MathMax is the longest formula (characters of TeX).
const MathMax = 2000

type formula struct{}

func (formula) Name() string              { return "math" }
func (formula) Group() string             { return "data" }
func (formula) Labels() map[string]string { return lm("Formula", "Formel") }
func (formula) Description() map[string]string {
	return lm("A mathematical formula written in TeX (for example \\frac{a}{b} or x^2), set by the browser and read as mathematics by screen readers.",
		"Eine mathematische Formel in TeX (zum Beispiel \\frac{a}{b} oder x^2), vom Browser gesetzt und von Screenreadern als Mathematik vorgelesen.")
}
func (formula) HasBody() bool { return false }
func (formula) Inline() bool  { return true }
func (formula) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "tex", Labels: lm("Formula (TeX)", "Formel (TeX)"), Type: "textarea", Required: true},
		{Name: "display", Labels: lm("Shown", "Darstellung"), Type: "select", Options: []string{"inline", "block"}, Default: "inline",
			OptionLabels: map[string]map[string]string{"inline": lm("In the line of text", "In der Textzeile"), "block": lm("On its own line, centred", "In eigener Zeile, zentriert")}},
	}
}

func (formula) Render(_ context.Context, mc content.MacroContext) (string, error) {
	tex := strings.TrimSpace(mc.Params["tex"])
	if tex == "" {
		return "", apperr.BadRequest("a formula needs its TeX")
	}
	if !utf8.ValidString(tex) || utf8.RuneCountInString(tex) > MathMax {
		return "", apperr.BadRequest("a formula has at most %d characters", MathMax)
	}
	for _, r := range tex {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return "", apperr.BadRequest("a formula has no control characters")
		}
	}
	display := "inline"
	if mc.Params["display"] == "block" {
		display = "block"
	}
	return fmt.Sprintf(`<span class="ew-math ew-math--%s" data-display="%s"><code class="ew-math-tex">%s</code></span>`, display, display, esc(tex)), nil
}
