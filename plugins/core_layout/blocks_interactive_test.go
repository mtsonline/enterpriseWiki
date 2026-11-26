package core_layout

import (
	"context"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

func render(t *testing.T, m content.Macro, lang string, params map[string]string, body string) (string, error) {
	t.Helper()
	return m.Render(context.Background(), content.MacroContext{RenderContext: content.RenderContext{Lang: lang}, Params: params, Body: body, Index: 1})
}

func TestNumbers(t *testing.T) {
	for in, want := range map[string][2]string{
		"1234": {"1,234", "1.234"}, "1234567.5": {"1,234,567.5", "1.234.567,5"}, "-12,25": {"−12.25", "−12,25"},
		"0": {"0", "0"}, "999": {"999", "999"}, "1000": {"1,000", "1.000"},
	} {
		v, dec, err := number(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if en, de := formatNumber(v, dec, "en"), formatNumber(v, dec, "de"); en != want[0] || de != want[1] {
			t.Errorf("%q: %q %q", in, en, de)
		}
	}
	for _, bad := range []string{"", "abc", "1e400", "NaN", "Inf", "1.23456", "1e15", "0x10", "1 000"} {
		if _, _, err := number(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestInteractiveBlocks(t *testing.T) {
	// progress: the value is text; the bar has its name and value for screen readers
	out, err := render(t, progress{}, "de", map[string]string{"label": "Umbau <b>", "value": "40"}, "")
	if err != nil || !strings.Contains(out, `role="progressbar"`) || !strings.Contains(out, `aria-valuenow="40"`) || !strings.Contains(out, "40 %") ||
		!strings.Contains(out, "Umbau &lt;b&gt;") || !strings.Contains(out, "width: 40.00%") {
		t.Fatalf("progress: %v %s", err, out)
	}
	out, err = render(t, progress{}, "en", map[string]string{"label": "Seats", "value": "12", "max": "48", "color": "evil;x"}, "")
	if err != nil || !strings.Contains(out, "12 / 48") || !strings.Contains(out, "ewl-progress--primary") || !strings.Contains(out, "width: 25.00%") {
		t.Fatalf("progress with a maximum: %v %s", err, out)
	}
	// quotes in the name stay inside its attribute
	out, _ = render(t, progress{}, "en", map[string]string{"label": `a" onmouseover="x`, "value": "1"}, "")
	if !strings.Contains(out, `aria-label="a&#34; onmouseover=&#34;x"`) {
		t.Fatalf("progress name in its attribute: %s", out)
	}
	for _, p := range []map[string]string{{"value": "4"}, {"label": "x", "value": "101"}, {"label": "x", "value": "-1"}, {"label": "x", "value": "1", "max": "0"}, {"label": "x", "value": "x"}} {
		if _, err := render(t, progress{}, "en", p, ""); err == nil {
			t.Errorf("progress %v accepted", p)
		}
	}
	// counter: the whole number is text; the widget only when counting
	out, err = render(t, counter{}, "de", map[string]string{"value": "1234.5", "label": "Kunden", "prefix": "€", "suffix": "%"}, "")
	if err != nil || !strings.Contains(out, ">1.234,5<") || !strings.Contains(out, `data-to="1234.5"`) || !strings.Contains(out, `data-decimals="1"`) || !strings.Contains(out, "> %<") {
		t.Fatalf("counter: %v %s", err, out)
	}
	out, _ = render(t, counter{}, "en", map[string]string{"value": "7", "label": "x", "count": "false"}, "")
	if strings.Contains(out, "data-ew-widget") {
		t.Fatalf("counter without counting: %s", out)
	}
	if _, err := render(t, counter{}, "en", map[string]string{"value": "7"}, ""); err == nil {
		t.Error("counter without a name accepted")
	}
	if _, err := render(t, counter{}, "en", map[string]string{"value": "7", "label": "x", "prefix": "1234567890123"}, ""); err == nil {
		t.Error("long prefix accepted")
	}
	// timeline: entries as a list; horizontal scrolls in a named region
	body := "<h2>2024</h2><p>A</p><hr><h2>2025 &amp; more</h2><p>B</p>"
	out, err = render(t, timeline{}, "en", map[string]string{"label": "History"}, body)
	if err != nil || strings.Count(out, `<li class="ewl-timeline-entry">`) != 2 || !strings.Contains(out, `<p class="ewl-timeline-when">2025 &amp; more</p>`) || !strings.Contains(out, `<ol class="ewl-timeline ewl-timeline--vertical" aria-label="History">`) {
		t.Fatalf("timeline: %v %s", err, out)
	}
	out, err = render(t, timeline{}, "en", map[string]string{"label": "History", "direction": "horizontal"}, body)
	if err != nil || !strings.Contains(out, `role="region" aria-label="History" tabindex="0"`) {
		t.Fatalf("horizontal timeline: %v %s", err, out)
	}
	if _, err := render(t, timeline{}, "en", map[string]string{"direction": "horizontal"}, body); err == nil {
		t.Error("horizontal timeline without a name accepted")
	}
	// dialog: readable without script; never open by itself
	out, err = render(t, dialog{}, "en", map[string]string{"button": "More", "title": "Details", "look": "x"}, "<p>Text</p>")
	if err != nil || !strings.Contains(out, `<details class="ewl-dialog" data-ew-widget="ewl.dialog">`) || strings.Contains(out, " open") || !strings.Contains(out, "ewl-cta--secondary") || !strings.Contains(out, "<p>Text</p>") {
		t.Fatalf("dialog: %v %s", err, out)
	}
	for _, p := range []map[string]string{{"title": "x"}, {"button": "x"}} {
		if _, err := render(t, dialog{}, "en", p, "<p>T</p>"); err == nil {
			t.Errorf("dialog %v accepted", p)
		}
	}
	if _, err := render(t, dialog{}, "en", map[string]string{"button": "x", "title": "y"}, " "); err == nil {
		t.Error("empty dialog accepted")
	}
	// tooltip: escaped, the explanation in brackets without script
	out, err = render(t, tooltip{}, "en", map[string]string{"text": "API", "tip": `"Interface" <x>`}, "")
	if err != nil || !strings.Contains(out, `<span class="ewl-tip-text"> (&#34;Interface&#34; &lt;x&gt;)</span>`) {
		t.Fatalf("tooltip: %v %s", err, out)
	}
}

func TestQuoteAndLogos(t *testing.T) {
	out, err := render(t, testimonial{}, "en", map[string]string{"name": "Ana <b>", "role": "CEO", "image": "/api/v1/media/7/view"}, "<p>Great.</p>")
	if err != nil || !strings.Contains(out, `<blockquote class="ewl-quote-text"><p>Great.</p></blockquote>`) || !strings.Contains(out, `src="/api/v1/media/7/view" alt=""`) || !strings.Contains(out, "Ana &lt;b&gt;") {
		t.Fatalf("quote: %v %s", err, out)
	}
	for _, p := range []map[string]string{{"role": "x"}, {"name": "x", "image": "https://evil.example/a.png"}, {"name": "x", "image": "javascript:x"}} {
		if _, err := render(t, testimonial{}, "en", p, "<p>T</p>"); err == nil {
			t.Errorf("quote %v accepted", p)
		}
	}
	if _, err := render(t, testimonial{}, "en", map[string]string{"name": "x"}, " "); err == nil {
		t.Error("quote without text accepted")
	}
	body := `<p><a href="https://a.example"><img src="/api/v1/media/1/view" alt="Acme"></a> <img src="/api/v1/files/b.png" alt="Beta"> text</p>`
	out, err = render(t, logos{}, "en", map[string]string{"label": "Partners", "grey": "true"}, body)
	if err != nil || strings.Count(out, "<li>") != 2 || !strings.Contains(out, `aria-label="Partners"`) || !strings.Contains(out, "ewl-logos--grey") || strings.Contains(out, "text") {
		t.Fatalf("logos: %v %s", err, out)
	}
	for name, c := range map[string][2]string{
		"no name":            {"", body},
		"no pictures":        {"P", "<p>none</p>"},
		"no alt":             {"P", `<img src="/api/v1/media/1/view">`},
		"empty alt":          {"P", `<img src="/api/v1/media/1/view" alt=" ">`},
		"from elsewhere":     {"P", `<img src="https://evil.example/t.png" alt="X">`},
		"a path of the wiki": {"P", `<img src="/api/v1/admin/users" alt="X">`},
	} {
		if _, err := render(t, logos{}, "en", map[string]string{"label": c[0]}, c[1]); err == nil {
			t.Errorf("logos %s accepted", name)
		}
	}
}
