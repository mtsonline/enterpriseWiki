package core_macros

import (
	"context"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

func TestFormula(t *testing.T) {
	render := func(tex, display string) (string, error) {
		return formula{}.Render(context.Background(), content.MacroContext{Params: map[string]string{"tex": tex, "display": display}})
	}
	got, err := render(` \frac{a}{b} < c & "d" `, "block")
	if err != nil || got != `<span class="ew-math ew-math--block" data-display="block"><code class="ew-math-tex">\frac{a}{b} &lt; c &amp; &#34;d&#34;</code></span>` {
		t.Fatalf("render: %q %v", got, err)
	}
	if got, _ := render("x", "<b>"); !strings.Contains(got, `data-display="inline"`) {
		t.Errorf("display: %q", got)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", MathMax+1), "a\x00b", "a\x1bb"} {
		if out, err := render(bad, ""); err == nil {
			t.Errorf("tex %q accepted: %q", bad, out)
		}
	}
	if _, err := render(strings.Repeat("x", MathMax), ""); err != nil {
		t.Errorf("the longest formula: %v", err)
	}
}
