package core_html

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

func TestSanitize(t *testing.T) {
	ty := &Type{}
	attacks := []string{
		`<script>alert(1)</script>`, `<img src=x onerror=alert(1)>`, `<a href="javascript:alert(1)">x</a>`,
		`<iframe src="https://evil"></iframe>`, `<svg onload=alert(1)>`, `<p style="background:url(javascript:x)">x</p>`,
	}
	for _, a := range attacks {
		raw, _ := json.Marshal(Body{Format: "html", Source: a})
		norm, err := ty.Normalize(raw)
		if err != nil {
			t.Fatal(err)
		}
		html, _ := ty.Render(context.Background(), content.RenderContext{}, norm)
		low := strings.ToLower(html)
		for _, bad := range []string{"<script", "onerror", "javascript:", "<iframe", "onload", "style="} {
			if strings.Contains(low, bad) {
				t.Errorf("%q survived sanitizing: %s", bad, html)
			}
		}
	}
}

func TestMarkdown(t *testing.T) {
	ty := &Type{}
	raw, _ := json.Marshal(Body{Format: "markdown", Source: "# Title\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n<script>x</script>"})
	html, err := ty.Render(context.Background(), content.RenderContext{}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "<h1") || !strings.Contains(html, "<table>") || strings.Contains(html, "<script") {
		t.Fatalf("%s", html)
	}
	if txt := ty.Text(raw); !strings.Contains(txt, "Title") || strings.Contains(txt, "<") {
		t.Fatalf("text %q", txt)
	}
}

func TestDecodeValidation(t *testing.T) {
	if _, err := Decode(json.RawMessage(`{"format":"pdf"}`)); err == nil {
		t.Fatal("invalid format accepted")
	}
}

// Task list checkboxes get an accessible name in the language of the page.
func TestTaskLabels(t *testing.T) {
	ty := &Type{}
	raw, _ := json.Marshal(Body{Format: "markdown", Source: "- [x] done\n- [ ] open\n"})
	html, err := ty.Render(context.Background(), content.RenderContext{Lang: "de"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `checked="" disabled="" type="checkbox" aria-label="Erledigt"`) || !strings.Contains(html, `type="checkbox" aria-label="Offen"`) {
		t.Fatalf("%s", html)
	}
	if html, _ = ty.Render(context.Background(), content.RenderContext{Lang: "fr"}, raw); !strings.Contains(html, `aria-label="Done"`) {
		t.Fatalf("fallback: %s", html)
	}
}
