package core_layout

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const chartTable = `<table><thead><tr><th>Quarter</th><th>North</th><th>South</th></tr></thead>` +
	`<tbody><tr><td>Q1</td><td>1.200,5</td><td>800</td></tr><tr><td>Q2</td><td>1.350</td><td></td></tr><tr><td>Q3 &lt;b&gt;</td><td>-200</td><td>900</td></tr></tbody></table>`

// TestChart: bars, lines and a pie from a table; the table stays as data,
// texts are escaped, wrong tables are refused with a reason.
func TestChart(t *testing.T) {
	out, err := render(t, chart{}, "de", map[string]string{"title": "Umsatz", "unit": "€"}, chartTable)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<figure class="ewl-chart ewl-chart--bar">`, `role="img"`, `aria-label="Umsatz. Balkendiagramm mit 2 Reihen und 3 Werten`,
		`<details class="ewl-chart-data"><summary>Daten als Tabelle</summary>`, `<th scope="row">Q3 &lt;b&gt;</th>`, "1.200,5 €", `<td>–</td>`, `class="ewl-chart-c2"`, `ewl-chart-legend`} {
		if !strings.Contains(out, want) {
			t.Errorf("bar: %q missing in %s", want, out)
		}
	}
	if strings.Contains(out, "<b>") {
		t.Error("cell text not escaped")
	}
	// a negative bar goes below the zero line: the axis shows a negative value
	if !strings.Contains(out, "−") {
		t.Error("negative values: no negative axis")
	}
	out, err = render(t, chart{}, "en", map[string]string{"title": "Sales", "type": "line", "table": "open"}, chartTable)
	if err != nil || !strings.Contains(out, `<polyline class="ewl-chart-line ewl-chart-c1"`) || !strings.Contains(out, "<circle") || !strings.Contains(out, "<rect class=\"ewl-chart-c2\"") || !strings.Contains(out, "<details class=\"ewl-chart-data\" open>") {
		t.Errorf("line: %v %s", err, out)
	}
	pie := `<table><tr><th>Kind</th><th>Count</th></tr><tr><td>A</td><td>3</td></tr><tr><td>B</td><td>1</td></tr></table>`
	out, err = render(t, chart{}, "en", map[string]string{"title": "Shares", "type": "pie"}, pie)
	if err != nil || strings.Count(out, `ewl-chart-slice`) != 2 || !strings.Contains(out, "75 %") || !strings.Contains(out, "A · 75 %") {
		t.Errorf("pie: %v %s", err, out)
	}
	for name, c := range map[string]struct {
		params map[string]string
		body   string
	}{
		"no title":      {map[string]string{}, chartTable},
		"no table":      {map[string]string{"title": "x"}, "<p>1 2 3</p>"},
		"only a head":   {map[string]string{"title": "x"}, `<table><tr><th>a</th><th>b</th></tr></table>`},
		"text value":    {map[string]string{"title": "x"}, `<table><tr><th>a</th><th>b</th></tr><tr><td>x</td><td>many</td></tr></table>`},
		"pie, 2 series": {map[string]string{"title": "x", "type": "pie"}, chartTable},
		"pie, negative": {map[string]string{"title": "x", "type": "pie"}, `<table><tr><th>a</th><th>b</th></tr><tr><td>x</td><td>-1</td></tr></table>`},
		"7 series":      {map[string]string{"title": "x"}, `<table><tr><th>a</th><th>1</th><th>2</th><th>3</th><th>4</th><th>5</th><th>6</th><th>7</th></tr><tr><td>x</td><td>1</td></tr></table>`},
	} {
		if _, err := render(t, chart{}, "en", c.params, c.body); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestChartValue: numbers as people write them, by language.
func TestChartValue(t *testing.T) {
	for _, c := range []struct {
		in, lang string
		want     float64
		ok       bool
	}{
		{"1.234,5", "de", 1234.5, true}, {"1,234.5", "en", 1234.5, true}, {"1.234", "de", 1234, true}, {"1.234", "en", 1.234, true},
		{"1,5", "en", 1.5, true}, {"12 %", "de", 12, true}, {"€ 3,50", "de", 3.5, true}, {"−7", "de", -7, true}, {"", "de", 0, false}, {"–", "en", 0, false},
		{"1 234,5", "de", 1234.5, true},
	} {
		got, _, ok, err := chartValue(c.in, c.lang)
		if err != nil || ok != c.ok || math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%q (%s): %v %v %v", c.in, c.lang, got, ok, err)
		}
	}
	if _, _, _, err := chartValue("abc", "en"); err == nil {
		t.Error("text accepted as a number")
	}
}

func lum(hex string) float64 {
	v, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	f := func(c uint64) float64 {
		x := float64(c) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*f(v>>16&255) + 0.7152*f(v>>8&255) + 0.0722*f(v&255)
}

func contrast(a, b string) float64 {
	la, lb := lum(a), lum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestChartColors: every colour of a chart has at least 3:1 on the light
// page and the dark template (WCAG 1.4.11).
func TestChartColors(t *testing.T) {
	light, _ := os.ReadFile("web/style.css")
	dark, _ := os.ReadFile("../theme_wiki_dark/web/style.css")
	re := regexp.MustCompile(`--ewl-chart-(\d+),? ?:? ?(#[0-9a-f]{6})`)
	check := func(src []byte, bg, where string) {
		seen := map[string]bool{}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			seen[m[1]] = true
			if c := contrast(m[2], bg); c < 3 {
				t.Errorf("%s: colour %s of series %s has %.2f:1 on %s", where, m[2], m[1], c, bg)
			}
		}
		if len(seen) != chartMaxSlices {
			t.Errorf("%s: %d colours, want %d", where, len(seen), chartMaxSlices)
		}
	}
	check(light, "#ffffff", "light")
	check(dark, "#1d2125", "dark")
}
