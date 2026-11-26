package core_macros

// Report of the dates in pages (plan "Aufgaben-Bericht", step 2): the
// appointments in the text (a date block with a title, core.macros.date)
// of an area, upcoming or past, with time and page. Only pages the reader
// may see.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

type dates struct {
	loc func() *time.Location
}

func (dates) Name() string              { return "dates" }
func (dates) Group() string             { return "lists" }
func (dates) Labels() map[string]string { return lm("Report of dates", "Termin-Bericht") }
func (dates) Description() map[string]string {
	return lm("The appointments in pages (date blocks with a title) of an area as a list with day, time and page; only pages the reader may see.",
		"Die Termine in Seiten (Datum-Bausteine mit Titel) eines Bereichs als Liste mit Tag, Uhrzeit und Seite; nur Seiten, die die lesende Person sehen darf.")
}
func (dates) HasBody() bool { return false }
func (dates) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "category", Labels: lm("Area (empty: the whole wiki)", "Bereich (leer: das ganze Wiki)"), Type: "custom:category"},
		{Name: "range", Labels: lm("Which dates", "Welche Termine"), Type: "select", Options: []string{"upcoming", "past", "all"}, Default: "upcoming",
			OptionLabels: map[string]map[string]string{"upcoming": lm("Upcoming (from today)", "Kommende (ab heute)"), "past": lm("Past", "Vergangene"), "all": lm("All", "Alle")}},
		{Name: "days", Labels: lm("Days ahead or back (1–730; empty: no limit)", "Tage voraus oder zurück (1–730; leer: ohne Grenze)"), Type: "number"},
		{Name: "max", Labels: lm("At most (1–500)", "Höchstens (1–500)"), Type: "number", Default: "50"},
		{Name: "label", Labels: lm("Name for screen readers", "Name für Screenreader"), Type: "text"},
	}
}

func (d dates) location() *time.Location {
	if d.loc != nil {
		if l := d.loc(); l != nil {
			return l
		}
	}
	return time.Local
}

func (d dates) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	loc := d.location()
	now := time.Now().In(loc)
	today := now.Format("2006-01-02")
	q := content.DataQuery{Kind: DateKind, Category: idParam(mc.Params["category"]), States: []string{"planned"}, Sort: "when", Prefer: mc.Lang,
		Limit: countParam(mc.Params["max"], 50, content.DataQueryMax)}
	days := 0
	if v := strings.TrimSpace(mc.Params["days"]); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 730 {
			return "", apperr.BadRequest("days: 1 to 730")
		}
		days = n
	}
	switch mc.Params["range"] {
	case "", "upcoming":
		q.From = today
		if days > 0 {
			q.To = now.AddDate(0, 0, days).Format("2006-01-02")
		}
	case "past":
		q.To, q.Sort = now.AddDate(0, 0, -1).Format("2006-01-02"), "-when"
		if days > 0 {
			q.From = now.AddDate(0, 0, -days).Format("2006-01-02")
		}
	case "all":
	default:
		return "", apperr.BadRequest("which dates: upcoming, past or all")
	}
	name := strings.TrimSpace(mc.Params["label"])
	if name == "" {
		name = tr(mc.Lang, "Dates", "Termine")
	}
	res, err := mc.Service.QueryData(ctx, q)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<section class="ew-dates" aria-label="%s">`, esc(name))
	if !res.Complete {
		b.WriteString(`<p class="ew-muted" role="note">` + esc(tr(mc.Lang, "The report is being built; some dates may still be missing.", "Der Bericht wird gerade aufgebaut; einzelne Termine können noch fehlen.")) + `</p>`)
	}
	if len(res.Items) == 0 {
		b.WriteString(`<p class="ew-muted">` + esc(tr(mc.Lang, "No dates.", "Keine Termine.")) + `</p></section>`)
		return b.String(), nil
	}
	h := func(en, de string) string { return `<th scope="col">` + esc(tr(mc.Lang, en, de)) + `</th>` }
	b.WriteString(`<div class="ew-table-wrap"><table class="ew-table"><caption class="sr-only">` + esc(name) + `</caption><thead><tr>` +
		h("Day", "Tag") + h("Time", "Uhrzeit") + h("Appointment", "Termin") + h("Page", "Seite") + `</tr></thead><tbody>`)
	for _, it := range res.Items {
		str := func(k string) string { s, _ := it.Data[k].(string); return s }
		dayHTML := ""
		if day, err := time.Parse("2006-01-02", str("day")); err == nil {
			dayHTML = fmt.Sprintf(`<time datetime="%s">%s</time>`, str("day"), esc(FormatDate(day, mc.Lang, "weekday")))
		}
		clock := ""
		if at, err := time.Parse(time.RFC3339, str("at")); err == nil {
			at = at.In(loc)
			clock = fmt.Sprintf(`<time datetime="%s">%s</time>`, at.Format(time.RFC3339), at.Format("15:04"))
			if end, err := time.Parse(time.RFC3339, str("end")); err == nil {
				clock += "–" + end.In(loc).Format("15:04")
			}
		}
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><th scope="row">%s</th><td><a href="/e/%d">%s</a></td></tr>`,
			dayHTML, clock, esc(str("title")), it.ElementID, esc(it.Title))
	}
	b.WriteString(`</tbody></table></div>`)
	if res.Total > len(res.Items) {
		b.WriteString(`<p class="ew-muted">` + esc(fmt.Sprintf(tr(mc.Lang, "%d of %d shown.", "%d von %d gezeigt."), len(res.Items), res.Total)) + `</p>`)
	}
	b.WriteString(`</section>`)
	return b.String(), nil
}
