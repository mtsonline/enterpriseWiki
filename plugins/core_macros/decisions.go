package core_macros

// Decisions (plan of blocks, 3.2): a decision in the text with status,
// day and who is responsible, kept as data for reports (content/data.go),
// and the report of the decisions of an area. Who is responsible is a
// person of the wiki (shown by the rules of the people blocks: guests see
// nobody, see content.PeopleNames) and/or a text (for example a board).

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// DecisionKind is the kind of data of decisions.
const DecisionKind = "core.macros.decision"

var decisionStates = []string{"open", "decided", "rejected", "postponed"}

var decisionStateLabels = map[string]map[string]string{
	"open":      lm("Open", "Offen"),
	"decided":   lm("Decided", "Entschieden"),
	"rejected":  lm("Rejected", "Verworfen"),
	"postponed": lm("Postponed", "Zurückgestellt"),
}

var decisionColors = map[string]string{"open": "yellow", "decided": "green", "rejected": "red", "postponed": "grey"}

// DecisionDataKind is the schema of the data of decisions.
var DecisionDataKind = content.DataKind{
	Kind: DecisionKind, Version: 1, Labels: lm("Decisions", "Entscheidungen"),
	Fields: []content.DataField{
		{Key: "status", Type: content.DataChoice, Options: decisionStates, OptionLabels: decisionStateLabels, Column: content.ColState, Required: true, Labels: lm("Status", "Status")},
		{Key: "day", Type: content.DataDay, Column: content.ColDay, Labels: lm("Day", "Datum")},
		{Key: "owner", Type: content.DataText, Max: 200, Labels: lm("Responsible", "Verantwortlich")},
		{Key: "person", Type: content.DataPerson, Column: content.ColPerson, Labels: lm("Responsible person", "Verantwortliche Person")},
		{Key: "title", Type: content.DataText, Max: 300, Column: content.ColLabel, Labels: lm("Decision", "Entscheidung")},
		{Key: "text", Type: content.DataLong, Max: 4000, Labels: lm("Text", "Text")},
	},
	MaxPerElement: 200,
}

func label(m map[string]string, lang string) string {
	if strings.HasPrefix(lang, "de") {
		return m["de"]
	}
	return m["en"]
}

// ---------------------------------------------------------------- decision

type decision struct{}

func (decision) Name() string              { return "decision" }
func (decision) Group() string             { return "text" }
func (decision) Labels() map[string]string { return lm("Decision", "Entscheidung") }
func (decision) Description() map[string]string {
	return lm("A decision with status, day and who is responsible; the report of decisions lists it.",
		"Eine Entscheidung mit Status, Datum und Verantwortlichen; der Entscheidungs-Bericht listet sie.")
}
func (decision) HasBody() bool { return true }
func (decision) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "title", Labels: lm("Decision in short", "Entscheidung in Kürze"), Type: "text", Required: true},
		{Name: "status", Labels: lm("Status", "Status"), Type: "select", Options: decisionStates, Default: "open", OptionLabels: decisionStateLabels},
		{Name: "day", Labels: lm("Day", "Datum"), Type: "date"},
		{Name: "person", Labels: lm("Responsible person", "Verantwortliche Person"), Type: "custom:user"},
		{Name: "owner", Labels: lm("Responsible (text, for example a board)", "Verantwortlich (Text, zum Beispiel ein Gremium)"), Type: "text"},
	}
}

// DataKind implements content.DataMacro.
func (decision) DataKind() string { return DecisionKind }

// decisionOf checks the parameters (also for the data).
func decisionOf(p map[string]string) (title, status, day, owner string, person int64, err error) {
	title = strings.TrimSpace(p["title"])
	if title == "" || utf8.RuneCountInString(title) > 300 {
		return "", "", "", "", 0, apperr.BadRequest("a decision needs a short title (at most 300 characters)")
	}
	status = p["status"]
	if _, ok := decisionStateLabels[status]; !ok {
		status = "open"
	}
	day = strings.TrimSpace(p["day"])
	if day != "" {
		if _, e := time.Parse("2006-01-02", day); e != nil {
			return "", "", "", "", 0, apperr.BadRequest("the date: a day as YYYY-MM-DD, for example 2026-11-22")
		}
	}
	owner = strings.TrimSpace(p["owner"])
	if utf8.RuneCountInString(owner) > 200 {
		return "", "", "", "", 0, apperr.BadRequest("responsible: at most %d characters", 200)
	}
	if v := strings.TrimSpace(p["person"]); v != "" {
		person = idParam(v)
		if person == 0 {
			return "", "", "", "", 0, apperr.BadRequest("the responsible person: the id of a user")
		}
	}
	return title, status, day, owner, person, nil
}

// responsible is who is responsible in words: the name of the person, if
// the reader may see them, and the text.
func responsible(names map[int64]string, person int64, owner string) string {
	var parts []string
	if n := names[person]; person != 0 && n != "" {
		parts = append(parts, n)
	}
	if owner != "" {
		parts = append(parts, owner)
	}
	return strings.Join(parts, ", ")
}

// Data implements content.DataMacro.
func (decision) Data(_ context.Context, u content.DataUse) (map[string]any, error) {
	title, status, day, owner, person, err := decisionOf(u.Params)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(u.Text)
	if utf8.RuneCountInString(text) > 4000 {
		text = string([]rune(text)[:4000])
	}
	d := map[string]any{"title": title, "status": status, "day": day, "owner": owner, "text": text}
	if person != 0 {
		d["person"] = person
	}
	return d, nil
}

func (decision) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	title, status, day, owner, person, err := decisionOf(mc.Params)
	if err != nil {
		return "", err
	}
	names := map[int64]string{}
	if person != 0 && mc.Service != nil {
		if names, err = mc.Service.PeopleNames(ctx, []int64{person}); err != nil {
			return "", err
		}
	}
	who := responsible(names, person, owner)
	var b strings.Builder
	fmt.Fprintf(&b, `<section class="ew-decision ew-decision--%s" aria-label="%s">`, status, esc(tr(mc.Lang, "Decision", "Entscheidung")+": "+title))
	fmt.Fprintf(&b, `<p class="ew-decision-head"><span class="ew-status ew-status--%s">%s</span> <strong class="ew-decision-title">%s</strong></p>`,
		decisionColors[status], esc(label(decisionStateLabels[status], mc.Lang)), esc(title))
	var meta []string
	if day != "" {
		d, _ := time.Parse("2006-01-02", day)
		meta = append(meta, fmt.Sprintf(`<time datetime="%s">%s</time>`, day, esc(FormatDate(d, mc.Lang, "long"))))
	}
	if who != "" {
		meta = append(meta, esc(tr(mc.Lang, "Responsible: ", "Verantwortlich: ")+who))
	}
	if len(meta) > 0 {
		b.WriteString(`<p class="ew-decision-meta">` + strings.Join(meta, " · ") + `</p>`)
	}
	if strings.TrimSpace(mc.Body) != "" {
		b.WriteString(`<div class="ew-decision-body">` + mc.Body + `</div>`)
	}
	b.WriteString(`</section>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- report

type decisions struct{}

func (decisions) Name() string  { return "decisions" }
func (decisions) Group() string { return "lists" }
func (decisions) Labels() map[string]string {
	return lm("Report of decisions", "Entscheidungs-Bericht")
}
func (decisions) Description() map[string]string {
	return lm("The decisions of an area (or of the whole wiki) as a table, filtered by status; only pages the reader may see.",
		"Die Entscheidungen eines Bereichs (oder des ganzen Wikis) als Tabelle, nach Status gefiltert; nur Seiten, die die lesende Person sehen darf.")
}
func (decisions) HasBody() bool { return false }
func (decisions) Params() []content.MacroParam {
	opts := append([]string{"all"}, decisionStates...)
	ol := map[string]map[string]string{"all": lm("All", "Alle")}
	for k, v := range decisionStateLabels {
		ol[k] = v
	}
	return []content.MacroParam{
		{Name: "category", Labels: lm("Area (empty: the whole wiki)", "Bereich (leer: das ganze Wiki)"), Type: "custom:category"},
		{Name: "status", Labels: lm("Status", "Status"), Type: "select", Options: opts, Default: "all", OptionLabels: ol},
		{Name: "who", Labels: lm("Whose decisions", "Wessen Entscheidungen"), Type: "select", Options: []string{"all", "mine"}, Default: "all",
			OptionLabels: map[string]map[string]string{"all": lm("All", "Alle"), "mine": lm("Mine (I am the responsible person)", "Meine (ich bin die verantwortliche Person)")}},
		{Name: "sorting", Labels: lm("Order", "Reihenfolge"), Type: "select", Options: []string{"newest", "oldest"}, Default: "newest",
			OptionLabels: map[string]map[string]string{"newest": lm("Newest first", "Neueste zuerst"), "oldest": lm("Oldest first", "Älteste zuerst")}},
		{Name: "max", Labels: lm("At most (1–500)", "Höchstens (1–500)"), Type: "number", Default: "50"},
		{Name: "label", Labels: lm("Name for screen readers", "Name für Screenreader"), Type: "text"},
	}
}

func (decisions) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	q := content.DataQuery{Kind: DecisionKind, Category: idParam(mc.Params["category"]), Sort: "-day", Limit: countParam(mc.Params["max"], 50, content.DataQueryMax)}
	if mc.Params["sorting"] == "oldest" {
		q.Sort = "day"
	}
	if st := mc.Params["status"]; st != "" && st != "all" {
		if _, ok := decisionStateLabels[st]; !ok {
			return "", apperr.BadRequest("status: one of all, %s", strings.Join(decisionStates, ", "))
		}
		q.States = []string{st}
	}
	mine := false
	switch mc.Params["who"] {
	case "", "all":
	case "mine":
		mine = true
	default:
		return "", apperr.BadRequest("whose decisions: all or mine")
	}
	name := strings.TrimSpace(mc.Params["label"])
	if name == "" {
		name = tr(mc.Lang, "Decisions", "Entscheidungen")
		if mine {
			name = tr(mc.Lang, "My decisions", "Meine Entscheidungen")
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<section class="ew-decisions" aria-label="%s">`, esc(name))
	if mine {
		// the decisions of the reader; guests have none (never all of them)
		p := identity.From(ctx)
		if p == nil || p.ID() == 0 {
			b.WriteString(`<p class="ew-muted">` + esc(tr(mc.Lang, "Sign in to see your decisions.", "Melden Sie sich an, um Ihre Entscheidungen zu sehen.")) + `</p></section>`)
			return b.String(), nil
		}
		q.Person = p.ID()
	}
	res, err := mc.Service.QueryData(ctx, q)
	if err != nil {
		return "", err
	}
	if !res.Complete {
		b.WriteString(`<p class="ew-muted" role="note">` + esc(tr(mc.Lang, "The report is being built; some decisions may still be missing.", "Der Bericht wird gerade aufgebaut; einzelne Entscheidungen können noch fehlen.")) + `</p>`)
	}
	if len(res.Items) == 0 {
		none := tr(mc.Lang, "No decisions.", "Keine Entscheidungen.")
		if mine {
			none = tr(mc.Lang, "You are not responsible for any of these decisions.", "Sie sind für keine dieser Entscheidungen verantwortlich.")
		}
		b.WriteString(`<p class="ew-muted">` + esc(none) + `</p></section>`)
		return b.String(), nil
	}
	h := func(en, de string) string { return `<th scope="col">` + esc(tr(mc.Lang, en, de)) + `</th>` }
	b.WriteString(`<div class="ew-table-wrap"><table class="ew-table"><caption class="sr-only">` + esc(name) + `</caption><thead><tr>` +
		h("Decision", "Entscheidung") + h("Status", "Status") + h("Day", "Datum") + h("Responsible", "Verantwortlich") + h("Page", "Seite") + `</tr></thead><tbody>`)
	for _, it := range res.Items {
		str := func(k string) string { s, _ := it.Data[k].(string); return s }
		st := str("status")
		day := str("day")
		dayHTML := ""
		if d, err := time.Parse("2006-01-02", day); err == nil {
			dayHTML = fmt.Sprintf(`<time datetime="%s">%s</time>`, day, esc(FormatDate(d, mc.Lang, "short")))
		}
		fmt.Fprintf(&b, `<tr><th scope="row">%s</th><td><span class="ew-status ew-status--%s">%s</span></td><td>%s</td><td>%s</td><td><a href="/e/%s">%s</a></td></tr>`,
			esc(str("title")), decisionColors[st], esc(label(decisionStateLabels[st], mc.Lang)), dayHTML, esc(responsible(res.People, personOf(it.Data["person"]), str("owner"))),
			strconv.FormatInt(it.ElementID, 10), esc(it.Title))
	}
	b.WriteString(`</tbody></table></div>`)
	if res.Total > len(res.Items) {
		b.WriteString(`<p class="ew-muted">` + esc(fmt.Sprintf(tr(mc.Lang, "%d of %d shown.", "%d von %d gezeigt."), len(res.Items), res.Total)) + `</p>`)
	}
	b.WriteString(`</section>`)
	return b.String(), nil
}

// personOf is the id of a person in the data (a number from JSON).
func personOf(v any) int64 {
	switch x := v.(type) {
	case float64:
		if x >= 1 && x <= 1<<53 && x == float64(int64(x)) {
			return int64(x)
		}
	case int64:
		return max(x, 0)
	case json.Number:
		n, _ := x.Int64()
		return max(n, 0)
	}
	return 0
}
