package core_macros

// Report of the tasks in pages (plan "Aufgaben-Bericht", step 1): the task
// items with a person (@name) or a day (core.html.task, written by
// core.html), with who it belongs to, when it is due and the page. People
// it belongs to tick it here when they have the category permission
// core.html.task_done_own; who may edit the page ticks every task. The
// server checks this again when ticking.

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// TaskKind is the kind of data of the tasks in pages (plugin core.html).
const TaskKind = "core.html.task"

// taskDoneOwn is the permission to tick own tasks (plugin core.html).
const taskDoneOwn = "core.html.task_done_own"

type tasks struct{}

func (tasks) Name() string              { return "tasks" }
func (tasks) Group() string             { return "lists" }
func (tasks) Labels() map[string]string { return lm("Report of tasks", "Aufgaben-Bericht") }
func (tasks) Description() map[string]string {
	return lm("The tasks of pages (list items with a checkbox and @name or a date) of an area as a table; people tick their tasks here. Only pages the reader may see.",
		"Die Aufgaben von Seiten (Listenpunkte mit Kästchen und @Name oder Datum) eines Bereichs als Tabelle; die zuständigen Personen haken sie hier ab. Nur Seiten, die die lesende Person sehen darf.")
}
func (tasks) HasBody() bool { return false }
func (tasks) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "category", Labels: lm("Area (empty: the whole wiki)", "Bereich (leer: das ganze Wiki)"), Type: "custom:category"},
		{Name: "who", Labels: lm("Whose tasks", "Wessen Aufgaben"), Type: "select", Options: []string{"all", "mine"}, Default: "all",
			OptionLabels: map[string]map[string]string{"all": lm("All", "Alle"), "mine": lm("Mine (@ my name)", "Meine (@ mein Name)")}},
		{Name: "status", Labels: lm("Status", "Status"), Type: "select", Options: []string{"open", "completed", "all"}, Default: "open",
			OptionLabels: map[string]map[string]string{"open": lm("Open", "Offen"), "completed": lm("Done", "Erledigt"), "all": lm("All", "Alle")}},
		{Name: "due", Labels: lm("Due", "Fällig"), Type: "select", Options: []string{"all", "overdue", "week", "none"}, Default: "all",
			OptionLabels: map[string]map[string]string{"all": lm("All", "Alle"), "overdue": lm("Overdue", "Überfällig"),
				"week": lm("Within 7 days or overdue", "In 7 Tagen oder überfällig"), "none": lm("Without a date", "Ohne Datum")}},
		{Name: "sorting", Labels: lm("Order", "Reihenfolge"), Type: "select", Options: []string{"due", "page"}, Default: "due",
			OptionLabels: map[string]map[string]string{"due": lm("By due date", "Nach Fälligkeit"), "page": lm("By page", "Nach Seite")}},
		{Name: "max", Labels: lm("At most (1–500)", "Höchstens (1–500)"), Type: "number", Default: "50"},
		{Name: "label", Labels: lm("Name for screen readers", "Name für Screenreader"), Type: "text"},
	}
}

// taskRow is one task: the entries of its people together.
type taskRow struct {
	elementID, categoryID int64
	page, lang, title     string
	rev, item             int
	done                  bool
	day                   string
	people                []int64
}

func (tasks) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if mc.Service == nil {
		return "", nil
	}
	max := countParam(mc.Params["max"], 50, content.DataQueryMax)
	q := content.DataQuery{Kind: TaskKind, Category: idParam(mc.Params["category"]), Sort: "day", Limit: content.DataQueryMax, Prefer: mc.Lang}
	if mc.Params["sorting"] == "page" {
		q.Sort = "page"
	} else if s := mc.Params["sorting"]; s != "" && s != "due" {
		return "", apperr.BadRequest("order: due or page")
	}
	switch st := mc.Params["status"]; st {
	case "", "open":
		q.States = []string{"open"}
	case "completed": // not "done": the Core Rule Set takes it for a shell word
		q.States = []string{"done"}
	case "all":
	default:
		return "", apperr.BadRequest("status: one of open, completed, all")
	}
	today := time.Now().Format("2006-01-02")
	switch mc.Params["due"] {
	case "", "all":
	case "overdue":
		q.To = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	case "week":
		q.To = time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	case "none":
		q.NoDay = true
	default:
		return "", apperr.BadRequest("due: one of all, overdue, week, none")
	}
	mine := false
	switch mc.Params["who"] {
	case "", "all":
	case "mine":
		mine = true
	default:
		return "", apperr.BadRequest("whose tasks: all or mine")
	}
	name := strings.TrimSpace(mc.Params["label"])
	if name == "" {
		name = tr(mc.Lang, "Tasks", "Aufgaben")
		if mine {
			name = tr(mc.Lang, "My tasks", "Meine Aufgaben")
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<section class="ew-tasks" aria-label="%s" data-ew-widget="core.macros.tasks">`, esc(name))
	me := identity.From(ctx)
	var myID int64
	if me != nil {
		myID = me.ID()
	}
	if mine {
		// the tasks of the reader; guests have none (never all of them)
		if myID == 0 {
			b.WriteString(`<p class="ew-muted">` + esc(tr(mc.Lang, "Sign in to see your tasks.", "Melden Sie sich an, um Ihre Aufgaben zu sehen.")) + `</p></section>`)
			return b.String(), nil
		}
		q.Person = myID
	}
	res, err := mc.Service.QueryData(ctx, q)
	if err != nil {
		return "", err
	}
	// one row per task: the entries of its people together
	var rows []*taskRow
	byKey := map[string]*taskRow{}
	for _, it := range res.Items {
		item := int(personOf(it.Data["item"]))
		key := fmt.Sprintf("%d/%s/%d", it.ElementID, it.Lang, item)
		r := byKey[key]
		if r == nil {
			str := func(k string) string { s, _ := it.Data[k].(string); return s }
			r = &taskRow{elementID: it.ElementID, categoryID: it.CategoryID, page: it.Title, lang: it.Lang, title: str("title"),
				rev: it.Rev, item: item, done: str("state") == "done", day: str("day")}
			byKey[key] = r
			rows = append(rows, r)
		}
		if p := personOf(it.Data["person"]); p != 0 && !slices.Contains(r.people, p) {
			r.people = append(r.people, p)
		}
	}
	more := res.Total > len(res.Items) || len(rows) > max
	if len(rows) > max {
		rows = rows[:max]
	}
	if !res.Complete {
		b.WriteString(`<p class="ew-muted" role="note">` + esc(tr(mc.Lang, "The report is being built; some tasks may still be missing.", "Der Bericht wird gerade aufgebaut; einzelne Aufgaben können noch fehlen.")) + `</p>`)
	}
	if len(rows) == 0 {
		none := tr(mc.Lang, "No tasks.", "Keine Aufgaben.")
		if mine {
			none = tr(mc.Lang, "You have no tasks here.", "Sie haben hier keine Aufgaben.")
		}
		b.WriteString(`<p class="ew-muted">` + esc(none) + `</p></section>`)
		return b.String(), nil
	}
	// who may tick: editors of the page all tasks, the people of a task
	// with the permission for own tasks (checked again by the server)
	canEdit := map[int64]bool{}
	canOwn := map[int64]bool{}
	for _, r := range rows {
		if _, ok := canEdit[r.elementID]; ok || myID == 0 {
			continue
		}
		canEdit[r.elementID] = mc.Service.ElementPerms(ctx, r.elementID, r.categoryID)&acl.Edit != 0
		canOwn[r.elementID] = mc.Service.CanElementAction(ctx, r.elementID, r.categoryID, taskDoneOwn)
	}
	h := func(en, de string) string { return `<th scope="col">` + esc(tr(mc.Lang, en, de)) + `</th>` }
	b.WriteString(`<div class="ew-table-wrap"><table class="ew-table"><caption class="sr-only">` + esc(name) + `</caption><thead><tr>` +
		h("Done", "Erledigt") + h("Task", "Aufgabe") + h("Responsible", "Zuständig") + h("Due", "Fällig") + h("Page", "Seite") + `</tr></thead><tbody>`)
	for _, r := range rows {
		title := r.title
		if title == "" {
			title = tr(mc.Lang, "(no text)", "(ohne Text)")
		}
		state := tr(mc.Lang, "Open", "Offen")
		if r.done {
			state = tr(mc.Lang, "Done", "Erledigt")
		}
		mayTick := myID != 0 && (canEdit[r.elementID] || canOwn[r.elementID] && slices.Contains(r.people, myID))
		cell := `<span class="ew-status ew-status--` + map[bool]string{true: "green", false: "grey"}[r.done] + `">` + esc(state) + `</span>`
		if mayTick {
			checked := ""
			if r.done {
				checked = " checked"
			}
			cell = fmt.Sprintf(`<input type="checkbox" class="ew-task-tick"%s data-element="%d" data-lang="%s" data-rev="%d" data-item="%d" aria-label="%s">`,
				checked, r.elementID, esc(r.lang), r.rev, r.item, esc(tr(mc.Lang, "Done: ", "Erledigt: ")+title))
		}
		var names []string
		for _, p := range r.people {
			if n := res.People[p]; n != "" {
				names = append(names, n)
			}
		}
		due := ""
		if d, err := time.Parse("2006-01-02", r.day); err == nil {
			due = fmt.Sprintf(`<time datetime="%s">%s</time>`, r.day, esc(FormatDate(d, mc.Lang, "short")))
			if !r.done && r.day < today {
				due += ` <span class="ew-status ew-status--red">` + esc(tr(mc.Lang, "Overdue", "Überfällig")) + `</span>`
			}
		}
		fmt.Fprintf(&b, `<tr><td>%s</td><th scope="row">%s</th><td>%s</td><td>%s</td><td><a href="/e/%s">%s</a></td></tr>`,
			cell, esc(title), esc(strings.Join(names, ", ")), due, strconv.FormatInt(r.elementID, 10), esc(r.page))
	}
	b.WriteString(`</tbody></table></div>`)
	if more {
		b.WriteString(`<p class="ew-muted">` + esc(fmt.Sprintf(tr(mc.Lang, "The first %d tasks are shown.", "Die ersten %d Aufgaben werden gezeigt."), len(rows))) + `</p>`)
	}
	b.WriteString(`<p class="sr-only" role="status"></p></section>`)
	return b.String(), nil
}
