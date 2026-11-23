package core_html

// Tasks in pages (plan "Aufgaben-Bericht", step 1): a task item of a page
// ("- [ ] Check the offer @anna :date{value=2026-11-30}") with a person
// or a day is kept as data for reports (kind core.html.task, the common
// fields of points in time, content.TimeFields), one entry per person.
// People it belongs to may tick it in reports when they have the
// category permission core.html.task_done_own; who may edit the page may
// tick every task. Ticking saves a new revision: the page keeps its
// author (created by), the person who ticked is who changed it last.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	"github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/markdown"
)

// TaskKind is the kind of data of the tasks in pages.
const TaskKind = "core.html.task"

// TaskDoneOwn is the category permission to tick the own tasks of pages
// one may only read.
const TaskDoneOwn = "core.html.task_done_own"

// TypeID is the element type of rich text pages.
const TypeID = "core.html"

func lm(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }

// TaskStates are the states of a task.
var TaskStates = []string{"open", "done"}

// TaskDataKind is the schema of the data of tasks: the common fields of
// points in time and the number of the task item on the page ("item").
var TaskDataKind = content.DataKind{
	Kind: TaskKind, Version: 1, Labels: lm("Tasks in pages", "Aufgaben in Seiten"),
	Fields: append(content.TimeFields(TaskStates, map[string]map[string]string{
		"open": lm("Open", "Offen"),
		"done": lm("Done", "Erledigt"),
	}), content.DataField{Key: "item", Type: content.DataNumber, Column: content.ColNum, Required: true, Labels: lm("Item", "Punkt")}),
	MaxPerElement: 1000,
}

// Task is a task item of a page.
type Task struct {
	// N numbers the task items of the text (1 = the first; as markdown.SetTask)
	N      int
	Done   bool
	Title  string
	Day    string // YYYY-MM-DD of the first date in the item, or ""
	People []string
	// Hidden: inside a block that shows its content only to some readers
	Hidden bool
}

// Tasks returns the task items of a page body (all of them, also those
// without a person or a day). hidden tells which nodes lie in blocks that
// show their content only to some readers (content.Service.HiddenNode).
func Tasks(body json.RawMessage, hidden func(*nethtml.Node) bool) ([]Task, error) {
	src, err := markdownOf(body)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(src, "[") {
		return nil, nil
	}
	// a prefix nobody can guess: a checkbox written as HTML into the text
	// is not a task item (it would shift the numbers)
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	prefix := "ewt" + hex.EncodeToString(nonce[:]) + "-"
	out, err := markdown.ToHTMLTasks(src, prefix)
	if err != nil {
		return nil, err
	}
	ctxNode := &nethtml.Node{Type: nethtml.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := nethtml.ParseFragment(strings.NewReader(policy.Sanitize(out)), ctxNode)
	if err != nil {
		return nil, err
	}
	root := &nethtml.Node{Type: nethtml.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	var tasks []Task
	var walk func(n *nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode && n.DataAtom == atom.Li {
			if num := taskNumber(n, prefix); num > 0 {
				tasks = append(tasks, taskOf(n, num, hidden))
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return tasks, nil
}

// markdownOf returns the Markdown of a body (HTML is converted as on save).
func markdownOf(body json.RawMessage) (string, error) {
	b, err := Decode(body)
	if err != nil {
		return "", err
	}
	if b.Format == "html" {
		return markdown.FromHTML(policy.Sanitize(b.Source)), nil
	}
	return b.Source, nil
}

func taskNumber(li *nethtml.Node, prefix string) int {
	for _, a := range li.Attr {
		if a.Key != "class" {
			continue
		}
		for _, c := range strings.Fields(a.Val) {
			if rest, ok := strings.CutPrefix(c, prefix); ok {
				if n, err := strconv.Atoi(rest); err == nil && n > 0 {
					return n
				}
			}
		}
	}
	return 0
}

// taskOf reads a task item: its own content, without nested lists.
func taskOf(li *nethtml.Node, num int, hidden func(*nethtml.Node) bool) Task {
	t := Task{N: num, Hidden: hidden != nil && hidden(li)}
	var title, mentionText strings.Builder
	first := true
	var walk func(n *nethtml.Node, code bool)
	walk = func(n *nethtml.Node, code bool) {
		switch n.Type {
		case nethtml.TextNode:
			title.WriteString(n.Data)
			if !code {
				mentionText.WriteString(n.Data)
			} else {
				mentionText.WriteByte(' ')
			}
			return
		case nethtml.ElementNode:
			switch n.DataAtom {
			case atom.Ul, atom.Ol:
				return // nested items are tasks of their own
			case atom.Input:
				if first {
					first = false
					t.Done = hasAttr(n, "checked")
				}
				return
			case atom.Code, atom.Pre:
				code = true
			case atom.Span, atom.Div:
				if macroName(n) == "date" && t.Day == "" {
					t.Day = dateParam(n)
				}
			case atom.P, atom.Br:
				title.WriteByte(' ')
				mentionText.WriteByte(' ')
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, code)
		}
	}
	walk(li, false)
	t.Title = strings.Join(strings.Fields(title.String()), " ")
	if utf8.RuneCountInString(t.Title) > 300 {
		t.Title = string([]rune(t.Title)[:299]) + "…"
	}
	t.People = content.Mentions(mentionText.String(), content.PeoplePerItem)
	return t
}

func hasAttr(n *nethtml.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func macroName(n *nethtml.Node) string {
	for _, a := range n.Attr {
		if a.Key == "data-macro" {
			return a.Val
		}
	}
	return ""
}

// dateParam returns the day of a date block (YYYY-MM-DD) or "".
func dateParam(n *nethtml.Node) string {
	for _, a := range n.Attr {
		if a.Key != "data-params" {
			continue
		}
		var p map[string]any
		if json.Unmarshal([]byte(a.Val), &p) != nil {
			return ""
		}
		v, _ := p["value"].(string)
		v = strings.TrimSpace(v)
		if len(v) == 10 && v[4] == '-' && v[7] == '-' {
			return v
		}
	}
	return ""
}

// taskExtractor keeps the tasks of pages as data (content.DataExtractor).
type taskExtractor struct{}

func (taskExtractor) DataKind() string { return TaskKind }

func (taskExtractor) ExtractData(typ string, body json.RawMessage, hidden func(*nethtml.Node) bool) []content.DataItem {
	if typ != TypeID {
		return nil
	}
	tasks, err := Tasks(body, hidden)
	if err != nil {
		return nil
	}
	var out []content.DataItem
	for _, t := range tasks {
		if t.Hidden || (t.Day == "" && len(t.People) == 0) {
			continue
		}
		state := "open"
		if t.Done {
			state = "done"
		}
		d := map[string]any{"title": t.Title, "state": state, "item": t.N}
		if t.Day != "" {
			d["day"] = t.Day
		}
		out = append(out, content.DataItem{Index: t.N, Data: d, People: t.People, NeedPerson: t.Day == ""})
	}
	return out
}

// ---------------------------------------------------------------- ticking

type tickInput struct {
	// Lang: the language of the page the task is in ("": the original)
	Lang string `json:"lang"`
	// Rev is the revision the task was shown from: when the page was
	// changed since, nothing is ticked (conflict).
	Rev  int  `json:"rev"`
	Done bool `json:"done"`
}

var tickComments = map[bool]map[string]string{
	true:  lm("Task done: ", "Aufgabe erledigt: "),
	false: lm("Task reopened: ", "Aufgabe wieder offen: "),
}

func registerTasks(h *coreplugin.Host) {
	h.RegisterDataKind(TaskDataKind)
	h.Content.RegisterDataExtractor(h.Manifest.ID, taskExtractor{})
	// action: core.html.task_done
	h.Handle("POST", "/tasks/{id}/{n}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			return nil, apperr.BadRequest("invalid element id")
		}
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil || n < 1 {
			return nil, apperr.BadRequest("invalid task number")
		}
		var in tickInput
		if err := httpx.Decode(r, &in); err != nil {
			return nil, err
		}
		return tick(r.Context(), h, id, n, in)
	})
}

// TickResult is the answer of ticking a task.
type TickResult struct {
	Rev   int    `json:"rev"`
	State string `json:"state"`
}

func tick(ctx context.Context, h *coreplugin.Host, id int64, n int, in tickInput) (*TickResult, error) {
	me := identity.From(ctx)
	if me == nil || me.UserID == 0 {
		return nil, apperr.ErrUnauthorized
	}
	e, rev, title, body, err := h.Content.LatestRevision(ctx, id, in.Lang)
	if err != nil {
		return nil, err
	}
	if e.Type != TypeID {
		return nil, apperr.NotFound("task")
	}
	if in.Lang == "" {
		in.Lang = e.OriginalLang
	}
	if in.Rev != rev {
		return nil, apperr.Conflict("the page was changed since it was shown (revision %d); reload it", rev)
	}
	tasks, err := Tasks(body, h.Content.HiddenNode())
	if err != nil {
		return nil, apperr.BadRequest("the page cannot be read: %v", err)
	}
	i := slices.IndexFunc(tasks, func(t Task) bool { return t.N == n })
	// a task in a block shown only to some readers is not one of the
	// reports: nobody ticks it there (editors tick it in the page)
	if i < 0 || tasks[i].Hidden {
		return nil, apperr.NotFound("task")
	}
	t := tasks[i]
	state := map[bool]string{true: "done", false: "open"}
	if t.Done == in.Done {
		return &TickResult{Rev: rev, State: state[t.Done]}, nil
	}
	src, err := markdownOf(body)
	if err != nil {
		return nil, err
	}
	src, err = markdown.SetTask(src, n, in.Done)
	if errors.Is(err, markdown.ErrNoTask) {
		return nil, apperr.NotFound("task")
	}
	if err != nil {
		return nil, err
	}
	newBody, err := json.Marshal(Body{Format: "markdown", Source: src})
	if err != nil {
		return nil, err
	}
	lang := "en"
	if strings.HasPrefix(in.Lang, "de") {
		lang = "de"
	}
	comment := tickComments[in.Done][lang] + t.Title
	if utf8.RuneCountInString(comment) > 200 {
		comment = string([]rune(comment)[:199]) + "…"
	}
	v, err := h.Content.SaveElementChecked(ctx, id, content.SaveInput{Lang: in.Lang, Title: title, Body: newBody, Comment: comment, BaseRev: &rev},
		func(e *content.Element, perms acl.Perm) error {
			if perms&acl.Edit != 0 {
				return nil
			}
			if !h.ACL.CanElementAction(ctx, me, e.ID, e.CategoryID, TaskDoneOwn) {
				return apperr.ErrForbidden
			}
			ids, err := content.UserIDsByName(ctx, h.DB, t.People)
			if err != nil {
				return err
			}
			for _, uid := range ids {
				if uid == me.UserID {
					return nil
				}
			}
			return apperr.Forbidden("only the people of the task and who may edit the page tick it")
		})
	if err != nil {
		return nil, err
	}
	return &TickResult{Rev: v.Rev, State: state[in.Done]}, nil
}
