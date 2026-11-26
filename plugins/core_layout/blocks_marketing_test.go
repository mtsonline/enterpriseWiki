package core_layout

import (
	"strings"
	"testing"
	"time"
)

func TestMarketingBlocks(t *testing.T) {
	// price table: the highlighted plan has a word, not only a colour
	out, err := render(t, pricing{}, "de", map[string]string{"featured": "2"}, "<h3>Basis</h3><p>0 €</p><hr><h3>Team</h3><p>5 €</p>")
	if err != nil || !strings.Contains(out, `ewl-pricing--2`) || !strings.Contains(out, `<div class="ewl-plan ewl-plan--featured"><p class="ewl-plan-badge">Empfohlen</p><h3>Team</h3>`) {
		t.Fatalf("pricing: %v %s", err, out)
	}
	out, _ = render(t, pricing{}, "en", map[string]string{"featured": "1", "badge": "<b>Best</b>"}, "<p>A</p>")
	if !strings.Contains(out, "&lt;b&gt;Best&lt;/b&gt;") {
		t.Errorf("badge not escaped: %s", out)
	}
	for _, c := range []struct{ featured, body string }{{"3", "<p>A</p><hr><p>B</p>"}, {"none", "<p>1</p><hr><p>2</p><hr><p>3</p><hr><p>4</p><hr><p>5</p>"}} {
		if _, err := render(t, pricing{}, "en", map[string]string{"featured": c.featured}, c.body); err == nil {
			t.Errorf("pricing %v accepted", c)
		}
	}

	// hotspots: numbered links to the texts below the picture
	out, err = render(t, hotspots{}, "de", map[string]string{"image": "/api/v1/media/3/view", "alt": "Plan <x>"},
		"<ul><li>10/20: Eingang</li><li><p>100 / 0 : <strong>Ausgang</strong></p></li></ul>")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`alt="Plan &lt;x&gt;"`, `<a class="ewl-hotspot" href="#ewl-hs-1-1" style="left: 10%; top: 20%"><span class="sr-only">Punkt </span>1</a>`,
		`href="#ewl-hs-1-2" style="left: 100%; top: 0%"`, `<li id="ewl-hs-1-1">Eingang</li>`, `<li id="ewl-hs-1-2"><p><strong>Ausgang</strong></p></li>`} {
		if !strings.Contains(out, want) {
			t.Errorf("hotspots: missing %q in %s", want, out)
		}
	}
	for _, c := range []map[string]string{
		{"image": "https://evil.example/x.png", "alt": "a"}, {"image": "/api/v1/media/3/view", "alt": ""}, {"image": "javascript:alert(1)", "alt": "a"},
	} {
		if _, err := render(t, hotspots{}, "en", c, "<ul><li>1/1: a</li></ul>"); err == nil {
			t.Errorf("hotspots %v accepted", c)
		}
	}
	for _, body := range []string{"<p>no list</p>", "<ul><li>101/5: a</li></ul>", "<ul><li>5;5: a</li></ul>", "<ul><li>a 5/5: b</li></ul>", "<ul><li>-5/5: b</li></ul>"} {
		if _, err := render(t, hotspots{}, "en", map[string]string{"image": "/api/v1/media/3/view", "alt": "a"}, body); err == nil {
			t.Errorf("hotspots body %q accepted", body)
		}
	}

	// before and after: both pictures with their description and caption
	p := map[string]string{"before": "/api/v1/media/1/view", "after": "/api/v1/files/b.png", "before_alt": "Old hall", "after_alt": "New hall", "start": "30"}
	out, err = render(t, compare{}, "de", p, "")
	if err != nil || !strings.Contains(out, `data-start="30"`) || !strings.Contains(out, `<img src="/api/v1/media/1/view" alt="Old hall" loading="lazy"><figcaption>Vorher</figcaption>`) ||
		!strings.Contains(out, `<figcaption>Nachher</figcaption>`) {
		t.Fatalf("compare: %v %s", err, out)
	}
	for k, v := range map[string]string{"after_alt": "", "before": "https://evil.example/a.png", "start": "101", "before_label": strings.Repeat("x", 41)} {
		q := map[string]string{}
		for a, b := range p {
			q[a] = b
		}
		q[k] = v
		if _, err := render(t, compare{}, "en", q, ""); err == nil {
			t.Errorf("compare with %s=%q accepted", k, v)
		}
	}

	// countdown: the moment in the time zone of the wiki, the date always readable
	vienna, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		t.Skip("no time zone data")
	}
	c := countdown{loc: func() *time.Location { return vienna }}
	out, err = render(t, c, "de", map[string]string{"date": "2026-12-24", "time": "18:00", "label": "Bis zum Fest", "reached": `Frohe "Feste"`}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-to="2026-12-24T17:00:00Z"`, `data-reached="Frohe &#34;Feste&#34;"`, `<p class="ewl-countdown-label">Bis zum Fest</p>`,
		`<time datetime="2026-12-24T18:00:00+01:00">24.12.2026, 18:00 Uhr</time>`} {
		if !strings.Contains(out, want) {
			t.Errorf("countdown: missing %q in %s", want, out)
		}
	}
	out, _ = render(t, c, "en", map[string]string{"date": "2026-07-01"}, "")
	if !strings.Contains(out, `>1 July 2026, 00:00</time>`) || !strings.Contains(out, `data-to="2026-06-30T22:00:00Z"`) {
		t.Errorf("countdown en: %s", out)
	}
	for _, q := range []map[string]string{{"date": ""}, {"date": "2026-13-01"}, {"date": "2026-12-01", "time": "25:00"}, {"date": "1900-01-01"}} {
		if _, err := render(t, c, "en", q, ""); err == nil {
			t.Errorf("countdown %v accepted", q)
		}
	}
}
