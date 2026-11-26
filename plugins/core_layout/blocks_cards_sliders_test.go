package core_layout

import (
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

func TestCardAndSliderBlocks(t *testing.T) {
	// person card: escaped, a picture of the wiki only, safe ways to reach
	out, err := render(t, personcard{}, "en", map[string]string{"name": "Ana <b>", "role": "Lead", "email": "ana@example.com", "phone": "+43 (1) 234-5", "link": "javascript:alert(1)", "image": "/api/v1/media/3/view"}, "<p>About.</p>")
	if err != nil || !strings.Contains(out, "Ana &lt;b&gt;") || !strings.Contains(out, `href="mailto:ana@example.com"`) || !strings.Contains(out, `href="tel:+4312345"`) ||
		!strings.Contains(out, `href="#"`) || !strings.Contains(out, `alt=""`) || !strings.Contains(out, "<p>About.</p>") {
		t.Fatalf("person card: %v %s", err, out)
	}
	for _, p := range []map[string]string{{"role": "x"}, {"name": "x", "email": "x\"@y.z"}, {"name": "x", "email": "no-at"}, {"name": "x", "phone": "call me"}, {"name": "x", "image": "https://evil.example/a.png"}} {
		if _, err := render(t, personcard{}, "en", p, ""); err == nil {
			t.Errorf("person card %v accepted", p)
		}
	}
	// changing words: all words in the heading, readable without script
	out, err = render(t, rotating{}, "en", map[string]string{"before": "We build", "words": "wikis | sites|<tools>", "level": "9"}, "")
	if err != nil || !strings.Contains(out, `<h2 class="ewl-rot-heading">We build <span class="ewl-rot-words"><span class="ewl-rot-word">wikis</span><span class="ewl-rot-sep">, </span><span class="ewl-rot-word">sites</span><span class="ewl-rot-sep">, </span><span class="ewl-rot-word">&lt;tools&gt;</span></span>`) {
		t.Fatalf("rotating: %v %s", err, out)
	}
	for _, w := range []string{"one", "a|" + strings.Repeat("x", 41), strings.Repeat("a|", 11)} {
		if _, err := render(t, rotating{}, "en", map[string]string{"words": w}, ""); err == nil {
			t.Errorf("rotating %q accepted", w)
		}
	}
	// ticker: a named region with the list
	out, err = render(t, ticker{}, "en", map[string]string{"label": "News", "speed": "x"}, "<ul><li>One</li><li><a href=\"/e/1\">Two</a></li></ul>")
	if err != nil || !strings.Contains(out, `role="region" aria-label="News"`) || strings.Count(out, "<li>") != 2 || !strings.Contains(out, "ewl-ticker--slow") {
		t.Fatalf("ticker: %v %s", err, out)
	}
	if _, err := render(t, ticker{}, "en", map[string]string{}, "<ul><li>x</li></ul>"); err == nil {
		t.Error("ticker without a name accepted")
	}
	if _, err := render(t, ticker{}, "en", map[string]string{"label": "N"}, "<p>no list</p>"); err == nil {
		t.Error("ticker without a list accepted")
	}
	// slider: the carousel pattern; parts stay readable without script
	out, err = render(t, slider{}, "en", map[string]string{"label": "Steps", "auto": "true"}, "<p>A</p><hr><p>B</p><hr><p>C</p>")
	if err != nil || !strings.Contains(out, `aria-roledescription="carousel" aria-label="Steps"`) || strings.Count(out, `aria-roledescription="slide"`) != 3 || !strings.Contains(out, `aria-label="2 / 3"`) || !strings.Contains(out, `data-auto="true"`) {
		t.Fatalf("slider: %v %s", err, out)
	}
	if _, err := render(t, slider{}, "en", map[string]string{"label": "S"}, "<p>only one</p>"); err == nil {
		t.Error("slider with one part accepted")
	}
	if _, err := render(t, slider{}, "en", map[string]string{}, "<p>A</p><hr><p>B</p>"); err == nil {
		t.Error("slider without a name accepted")
	}
}

// TestPartNames: a part without a heading is named in the language of the
// page ("Teil 1", not "Part 1"); tabs and accordion announce their parts to
// the editor.
func TestPartNames(t *testing.T) {
	for lang, want := range map[string]string{"de": "Teil 1", "en": "Part 1", "fr": "Part 1"} {
		out, err := render(t, accordion{}, lang, nil, "<p>a</p><hr><h3>B</h3><p>b</p>")
		if err != nil || !strings.Contains(out, "<summary>"+want+"</summary>") || !strings.Contains(out, "<summary>B</summary>") {
			t.Errorf("%s: %v %s", lang, err, out)
		}
	}
	out, err := render(t, tabs{}, "de", nil, "<p>a</p>")
	if err != nil || !strings.Contains(out, ">Reiter 1</h3>") {
		t.Errorf("tabs: %v %s", err, out)
	}
	for _, m := range []content.PartsMacro{accordion{}, tabs{}, cards{}, grid{}, timeline{}, slider{}} {
		p := m.Parts()
		if p.Labels["en"] == "" || p.Labels["de"] == "" {
			t.Errorf("%T: parts without names %v", m, p.Labels)
		}
	}
}
