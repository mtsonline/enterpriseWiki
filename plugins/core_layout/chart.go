package core_layout

// Chart (blocks B, part 4): bars, lines or a pie from a table in the body of
// the macro. The server draws the chart as SVG from the numbers (no script,
// no library, nothing loaded from elsewhere); the table stays readable
// below it for screen readers and anyone who wants the numbers. Colors come
// from CSS variables with at least 3:1 contrast on the page (WCAG 1.4.11);
// lines also differ by the shape of their points, so color is never the
// only difference (WCAG 1.4.1).

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type chart struct{}

func (chart) Name() string              { return "chart" }
func (chart) Group() string             { return "data" }
func (chart) Labels() map[string]string { return lm("Chart", "Diagramm") }
func (chart) Description() map[string]string {
	return lm("Bars, lines or a pie from a table: the first row names the series, the first column the categories, the other cells are numbers. The table stays readable below the chart.",
		"Balken, Linien oder Torte aus einer Tabelle: Die erste Zeile nennt die Reihen, die erste Spalte die Kategorien, die übrigen Zellen sind Zahlen. Die Tabelle bleibt unter dem Diagramm lesbar.")
}
func (chart) HasBody() bool { return true }
func (chart) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "title", Labels: lm("Title", "Titel"), Type: "text", Required: true},
		{Name: "type", Labels: lm("Kind", "Art"), Type: "select", Options: []string{"bar", "line", "pie"}, Default: "bar",
			OptionLabels: map[string]map[string]string{"bar": lm("Bars", "Balken"), "line": lm("Lines", "Linien"), "pie": lm("Pie (one series)", "Torte (eine Reihe)")}},
		{Name: "unit", Labels: lm("Unit (e.g. €, %, h)", "Einheit (z. B. €, %, h)"), Type: "text"},
		{Name: "table", Labels: lm("Show the table", "Tabelle zeigen"), Type: "select", Options: []string{"closed", "open"}, Default: "closed",
			OptionLabels: map[string]map[string]string{"closed": lm("Folded (opens with a click)", "Zugeklappt (öffnet mit einem Klick)"), "open": lm("Open below the chart", "Offen unter dem Diagramm")}},
	}
}

// chartData is the table of a chart: series by category.
type chartData struct {
	axis       string      // the heading of the first column
	series     []string    // names of the series (first row)
	categories []string    // the first column
	values     [][]float64 // [category][series], NaN = empty
	decimals   int
}

const (
	chartMaxCategories = 50
	chartMaxSeries     = 6
	chartMaxSlices     = 12
)

var cellSpace = regexp.MustCompile(`[\s\x{00a0}\x{202f}]+`)

func cellText(n *nethtml.Node) string {
	var b strings.Builder
	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(cellSpace.ReplaceAllString(b.String(), " "))
}

var groupedDE = regexp.MustCompile(`^-?\d{1,3}(\.\d{3})+(,\d+)?$`)
var groupedEN = regexp.MustCompile(`^-?\d{1,3}(,\d{3})+(\.\d+)?$`)
var numberPart = regexp.MustCompile(`^[^\d\-−]*(-|−)?[\d.,]+`)

// chartValue reads a number of a cell as people write it ("1.234,5 €",
// "1,234.5", "12 %", "−3"): the value and its decimals; ok is false for an
// empty cell. The language decides what "1.234" means.
func chartValue(s, lang string) (float64, int, bool, error) {
	s = strings.TrimSpace(cellSpace.ReplaceAllString(s, ""))
	if s == "" || s == "-" || s == "–" {
		return 0, 0, false, nil
	}
	m := numberPart.FindString(s)
	if m == "" {
		return 0, 0, false, apperr.BadRequest("%q is not a number", s)
	}
	m = strings.Map(func(r rune) rune {
		if r == '−' {
			return '-'
		}
		return r
	}, strings.TrimLeftFunc(m, func(r rune) bool { return !(r >= '0' && r <= '9') && r != '-' && r != '−' }))
	de := strings.HasPrefix(lang, "de")
	switch {
	case groupedDE.MatchString(m) && (de || strings.Contains(m, ",")):
		m = strings.ReplaceAll(strings.ReplaceAll(m, ".", ""), ",", ".")
	case groupedEN.MatchString(m) && (!de || strings.Contains(m, ".")):
		m = strings.ReplaceAll(m, ",", "")
	case strings.Contains(m, ",") && strings.Contains(m, "."):
		if strings.LastIndex(m, ",") > strings.LastIndex(m, ".") {
			m = strings.ReplaceAll(strings.ReplaceAll(m, ".", ""), ",", ".")
		} else {
			m = strings.ReplaceAll(m, ",", "")
		}
	default:
		m = strings.ReplaceAll(m, ",", ".")
	}
	f, err := strconv.ParseFloat(m, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) >= 1e15 {
		return 0, 0, false, apperr.BadRequest("%q is not a number", s)
	}
	dec := 0
	if i := strings.IndexByte(m, '.'); i >= 0 {
		dec = min(len(m)-i-1, 3)
	}
	return f, dec, true, nil
}

// parseChart reads the first table of the body.
func parseChart(body, lang string) (*chartData, error) {
	nodes, err := nethtml.ParseFragment(strings.NewReader(body), &nethtml.Node{Type: nethtml.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return nil, apperr.BadRequest("put a table into the chart")
	}
	var table *nethtml.Node
	var find func(*nethtml.Node)
	find = func(n *nethtml.Node) {
		if table != nil {
			return
		}
		if n.Type == nethtml.ElementNode && n.DataAtom == atom.Table {
			table = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	for _, n := range nodes {
		find(n)
	}
	if table == nil {
		return nil, apperr.BadRequest("put a table into the chart")
	}
	var rows [][]string
	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode && n.DataAtom == atom.Tr {
			var row []string
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == nethtml.ElementNode && (c.DataAtom == atom.Td || c.DataAtom == atom.Th) {
					row = append(row, cellText(c))
				}
			}
			rows = append(rows, row)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(table)
	if len(rows) < 2 || len(rows[0]) < 2 {
		return nil, apperr.BadRequest("the table needs a row of names and at least one row of numbers, and at least two columns")
	}
	d := &chartData{axis: rows[0][0], series: rows[0][1:]}
	if len(d.series) > chartMaxSeries {
		return nil, apperr.BadRequest("at most %d series (columns of numbers)", chartMaxSeries)
	}
	if len(rows)-1 > chartMaxCategories {
		return nil, apperr.BadRequest("at most %d rows of numbers", chartMaxCategories)
	}
	for i, s := range d.series {
		if s == "" {
			d.series[i] = fmt.Sprintf("%d", i+1)
		}
		if len([]rune(s)) > 60 {
			d.series[i] = string([]rune(s)[:60])
		}
	}
	for _, r := range rows[1:] {
		cat := ""
		if len(r) > 0 {
			cat = r[0]
		}
		if len([]rune(cat)) > 60 {
			cat = string([]rune(cat)[:60])
		}
		vals := make([]float64, len(d.series))
		for j := range vals {
			vals[j] = math.NaN()
			if j+1 < len(r) {
				f, dec, ok, err := chartValue(r[j+1], lang)
				if err != nil {
					return nil, err
				}
				if ok {
					vals[j] = f
					d.decimals = max(d.decimals, dec)
				}
			}
		}
		d.categories = append(d.categories, cat)
		d.values = append(d.values, vals)
	}
	return d, nil
}

// niceScale gives the axis from lo to hi: rounded ends and a step of 1, 2
// or 5 times a power of ten, about five steps.
func niceScale(lo, hi float64) (float64, float64, float64) {
	if hi == lo {
		hi = lo + 1
	}
	raw := (hi - lo) / 5
	p := math.Pow(10, math.Floor(math.Log10(raw)))
	step := p
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if raw <= m*p {
			step = m * p
			break
		}
	}
	return math.Floor(lo/step) * step, math.Ceil(hi/step) * step, step
}

func chartTexts(lang string) map[string]string {
	if strings.HasPrefix(lang, "de") {
		return map[string]string{"bar": "Balkendiagramm", "line": "Liniendiagramm", "pie": "Tortendiagramm", "table": "Daten als Tabelle",
			"desc": "%s mit %d Reihen und %d Werten; die Zahlen stehen in der Tabelle", "share": "Anteil"}
	}
	return map[string]string{"bar": "Bar chart", "line": "Line chart", "pie": "Pie chart", "table": "Data as a table",
		"desc": "%s with %d series and %d values; the numbers are in the table", "share": "Share"}
}

const (
	chartW, chartH                   = 640.0, 320.0
	padL, padR, padT, padB           = 64.0, 16.0, 16.0, 56.0
	chartPieR, chartPieCX, chartPieY = 130.0, 320.0, 160.0
)

func (chart) Render(_ context.Context, mc content.MacroContext) (string, error) {
	title, err := shortText("title", mc.Params["title"], 120)
	if err != nil {
		return "", err
	}
	if title == "" {
		return "", apperr.BadRequest("a chart needs a title")
	}
	unit, err := shortText("unit", mc.Params["unit"], 12)
	if err != nil {
		return "", err
	}
	kind := oneOf(mc.Params["type"], []string{"bar", "line", "pie"})
	d, err := parseChart(mc.Body, mc.Lang)
	if err != nil {
		return "", err
	}
	if kind == "pie" {
		if len(d.series) != 1 {
			return "", apperr.BadRequest("a pie shows one series: the table needs exactly two columns")
		}
		if len(d.categories) > chartMaxSlices {
			return "", apperr.BadRequest("a pie has at most %d slices", chartMaxSlices)
		}
		for _, v := range d.values {
			if v[0] < 0 {
				return "", apperr.BadRequest("a pie needs numbers of at least 0")
			}
		}
	}
	tx := chartTexts(mc.Lang)
	num := func(f float64) string {
		s := formatNumber(f, d.decimals, mc.Lang)
		if unit != "" {
			s += " " + unit
		}
		return s
	}
	var svg string
	switch kind {
	case "pie":
		svg, err = pieSVG(d, num)
	default:
		svg, err = axisSVG(d, kind, num, mc.Lang)
	}
	if err != nil {
		return "", err
	}
	label := title + ". " + fmt.Sprintf(tx["desc"], tx[kind], len(d.series), len(d.categories))
	var b strings.Builder
	fmt.Fprintf(&b, `<figure class="ewl-chart ewl-chart--%s"><figcaption class="ewl-chart-title">%s</figcaption>`, kind, esc(title))
	fmt.Fprintf(&b, `<svg class="ewl-chart-svg" viewBox="0 0 %.0f %.0f" role="img" aria-label="%s" preserveAspectRatio="xMidYMid meet">%s</svg>`, chartW, chartH, esc(label), svg)
	// the legend is for the eye; the table carries the same for everyone
	if len(d.series) > 1 || kind == "pie" {
		b.WriteString(`<ul class="ewl-chart-legend" aria-hidden="true">`)
		names := d.series
		if kind == "pie" {
			names = d.categories
		}
		total := 0.0
		if kind == "pie" {
			for _, v := range d.values {
				total += v[0]
			}
		}
		for i, s := range names {
			extra := ""
			if kind == "pie" && total > 0 {
				extra = " · " + formatNumber(d.values[i][0]/total*100, 0, mc.Lang) + " %"
			}
			fmt.Fprintf(&b, `<li><span class="ewl-chart-key ewl-chart-c%d ewl-chart-m%d"></span>%s%s</li>`, i%chartMaxSlices+1, i%chartMaxSeries+1, esc(s), esc(extra))
		}
		b.WriteString(`</ul>`)
	}
	open := ""
	if mc.Params["table"] == "open" {
		open = " open"
	}
	fmt.Fprintf(&b, `<details class="ewl-chart-data"%s><summary>%s</summary><div class="ew-table-wrap"><table><caption>%s</caption><thead><tr><th scope="col">%s</th>`, open, esc(tx["table"]), esc(title), esc(d.axis))
	for _, s := range d.series {
		fmt.Fprintf(&b, `<th scope="col">%s</th>`, esc(s))
	}
	b.WriteString(`</tr></thead><tbody>`)
	for i, c := range d.categories {
		fmt.Fprintf(&b, `<tr><th scope="row">%s</th>`, esc(c))
		for _, v := range d.values[i] {
			if math.IsNaN(v) {
				b.WriteString(`<td>–</td>`)
			} else {
				fmt.Fprintf(&b, `<td>%s</td>`, esc(num(v)))
			}
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table></div></details></figure>`)
	return b.String(), nil
}

// axisSVG draws bars or lines with a value axis.
func axisSVG(d *chartData, kind string, num func(float64) string, lang string) (string, error) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, row := range d.values {
		for _, v := range row {
			if !math.IsNaN(v) {
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
		}
	}
	if math.IsInf(lo, 1) {
		return "", apperr.BadRequest("the table has no numbers")
	}
	// bars always start at zero; lines too unless all values are far from it
	if kind == "bar" || lo > 0 && lo < hi/2 {
		lo = math.Min(lo, 0)
	}
	hi = math.Max(hi, 0)
	if kind == "bar" {
		lo = math.Min(lo, 0)
	}
	lo, hi, step := niceScale(lo, hi)
	plotW, plotH := chartW-padL-padR, chartH-padT-padB
	y := func(v float64) float64 { return padT + plotH - (v-lo)/(hi-lo)*plotH }
	var b strings.Builder
	// grid and value axis
	b.WriteString(`<g class="ewl-chart-grid">`)
	for v := lo; v <= hi+step/2; v += step {
		fmt.Fprintf(&b, `<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f"/>`, padL, chartW-padR, y(v), y(v))
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" text-anchor="end" dominant-baseline="middle">%s</text>`, padL-6, y(v), esc(num(v)))
	}
	b.WriteString(`</g>`)
	n := len(d.categories)
	group := plotW / float64(n)
	// category labels: every k-th one when there are many
	every := 1
	if n > 12 {
		every = int(math.Ceil(float64(n) / 12))
	}
	b.WriteString(`<g class="ewl-chart-cats">`)
	for i, c := range d.categories {
		if i%every != 0 {
			continue
		}
		r := []rune(c)
		if len(r) > 14 {
			c = string(r[:13]) + "…"
		}
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" text-anchor="middle">%s</text>`, padL+group*(float64(i)+0.5), chartH-padB+18, esc(c))
	}
	b.WriteString(`</g>`)
	fmt.Fprintf(&b, `<line class="ewl-chart-axis" x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f"/>`, padL, chartW-padR, y(math.Max(lo, math.Min(0, hi))), y(math.Max(lo, math.Min(0, hi))))
	ns := len(d.series)
	if kind == "bar" {
		bw := group * 0.8 / float64(ns)
		showValues := n*ns <= 16
		for i, row := range d.values {
			for j, v := range row {
				if math.IsNaN(v) {
					continue
				}
				x := padL + group*float64(i) + group*0.1 + bw*float64(j)
				top, bottom := y(math.Max(v, 0)), y(math.Min(v, 0))
				fmt.Fprintf(&b, `<rect class="ewl-chart-c%d" x="%.1f" y="%.1f" width="%.1f" height="%.1f"/>`, j+1, x, top, math.Max(bw-2, 1), math.Max(bottom-top, 0.5))
				if showValues {
					fmt.Fprintf(&b, `<text class="ewl-chart-value" x="%.1f" y="%.1f" text-anchor="middle">%s</text>`, x+bw/2-1, top-4, esc(num(v)))
				}
			}
		}
	} else {
		for j := range d.series {
			var pts []string
			for i, row := range d.values {
				if !math.IsNaN(row[j]) {
					pts = append(pts, fmt.Sprintf("%.1f,%.1f", padL+group*(float64(i)+0.5), y(row[j])))
				}
			}
			fmt.Fprintf(&b, `<polyline class="ewl-chart-line ewl-chart-c%d" points="%s"/>`, j+1, strings.Join(pts, " "))
			for _, p := range pts {
				xy := strings.Split(p, ",")
				px, _ := strconv.ParseFloat(xy[0], 64)
				py, _ := strconv.ParseFloat(xy[1], 64)
				b.WriteString(marker(j, px, py))
			}
		}
	}
	_ = lang
	return b.String(), nil
}

// marker draws the point of a line: each series its own shape.
func marker(series int, x, y float64) string {
	c := series + 1
	switch series % chartMaxSeries {
	case 0:
		return fmt.Sprintf(`<circle class="ewl-chart-c%d" cx="%.1f" cy="%.1f" r="4"/>`, c, x, y)
	case 1:
		return fmt.Sprintf(`<rect class="ewl-chart-c%d" x="%.1f" y="%.1f" width="8" height="8"/>`, c, x-4, y-4)
	case 2:
		return fmt.Sprintf(`<path class="ewl-chart-c%d" d="M%.1f %.1fL%.1f %.1fL%.1f %.1fZ"/>`, c, x, y-5, x+5, y+4, x-5, y+4)
	case 3:
		return fmt.Sprintf(`<path class="ewl-chart-c%d" d="M%.1f %.1fL%.1f %.1fL%.1f %.1fL%.1f %.1fZ"/>`, c, x, y-5, x+5, y, x, y+5, x-5, y)
	case 4:
		return fmt.Sprintf(`<path class="ewl-chart-c%d" d="M%.1f %.1fL%.1f %.1fL%.1f %.1fZ"/>`, c, x, y+5, x+5, y-4, x-5, y-4)
	default:
		return fmt.Sprintf(`<path class="ewl-chart-c%d ewl-chart-cross" d="M%.1f %.1fL%.1f %.1fM%.1f %.1fL%.1f %.1f"/>`, c, x-4, y-4, x+4, y+4, x-4, y+4, x+4, y-4)
	}
}

// pieSVG draws the slices of one series, the largest first from the top.
func pieSVG(d *chartData, num func(float64) string) (string, error) {
	total := 0.0
	for _, v := range d.values {
		if !math.IsNaN(v[0]) {
			total += v[0]
		}
	}
	if total <= 0 {
		return "", apperr.BadRequest("the table has no numbers")
	}
	var b strings.Builder
	a := -math.Pi / 2
	for i, v := range d.values {
		if math.IsNaN(v[0]) || v[0] == 0 {
			continue
		}
		share := v[0] / total
		if share >= 0.9999 {
			fmt.Fprintf(&b, `<circle class="ewl-chart-c%d ewl-chart-slice" cx="%.1f" cy="%.1f" r="%.1f"/>`, i+1, chartPieCX, chartPieY, chartPieR)
			continue
		}
		e := a + share*2*math.Pi
		large := 0
		if share > 0.5 {
			large = 1
		}
		fmt.Fprintf(&b, `<path class="ewl-chart-c%d ewl-chart-slice" d="M%.1f %.1fL%.2f %.2fA%.1f %.1f 0 %d 1 %.2f %.2fZ"/>`,
			i+1, chartPieCX, chartPieY, chartPieCX+chartPieR*math.Cos(a), chartPieY+chartPieR*math.Sin(a), chartPieR, chartPieR, large,
			chartPieCX+chartPieR*math.Cos(e), chartPieY+chartPieR*math.Sin(e))
		// the share on slices that have room for it
		if share >= 0.06 {
			m := (a + e) / 2
			fmt.Fprintf(&b, `<text class="ewl-chart-pielabel" x="%.1f" y="%.1f" text-anchor="middle" dominant-baseline="middle">%s</text>`,
				chartPieCX+chartPieR*0.65*math.Cos(m), chartPieY+chartPieR*0.65*math.Sin(m), esc(fmt.Sprintf("%.0f %%", share*100)))
		}
		a = e
	}
	_ = num
	return b.String(), nil
}
