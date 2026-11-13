// Package workflow_process provides the "workflow.process" element type: a
// process with a start form, steps, assignees (starter, user, group, role)
// and actions. Every case keeps a snapshot of the process it was started
// with; every transition is recorded and never deleted.
//
// Who sees a case: the person who started it, everyone who is or was
// responsible for one of its steps, users with the Manage permission on the
// category and administrators.
package workflow_process

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"

	_ "embed"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// Plugin is the workflow plugin.
type Plugin struct{ host *sdk.Host }

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Instance is a case.
type Instance struct {
	ID          int64             `json:"id"`
	ElementID   int64             `json:"element_id"`
	CategoryID  int64             `json:"category_id"`
	Element     map[string]string `json:"element_titles"`
	Lang        string            `json:"lang"`
	Title       string            `json:"title"`
	Data        map[string]any    `json:"data"`
	Definition  Definition        `json:"definition"`
	Step        string            `json:"step"`
	StepName    string            `json:"step_name"`
	Status      string            `json:"status"`
	Outcome     string            `json:"outcome,omitempty"`
	StartedByID *int64            `json:"-"`
	StartedBy   string            `json:"started_by"`
	StartedAt   time.Time         `json:"started_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	ClosedAt    *time.Time        `json:"closed_at"`
	Visited     []string          `json:"-"`
	Actors      []int64           `json:"-"`
	StepEntered time.Time         `json:"step_entered_at"`
	WakeAt      *time.Time        `json:"wake_at"`
	LastError   string            `json:"last_error,omitempty"`
	DefinerID   *int64            `json:"-"`
	Trigger     map[string]string `json:"trigger,omitempty"`
	Depth       int               `json:"-"`
	ParentID    *int64            `json:"parent_id,omitempty"`
	ParentStep  string            `json:"-"`
	DueAt       *time.Time        `json:"due_at"`
	Warned      int               `json:"-"`
	Escalated   bool              `json:"escalated,omitempty"`
	// Override is the assignee of the current step after an escalation,
	// a hand-over, a claim or a question to the starter (Handover).
	Override *Assignee `json:"override,omitempty"`
	Handover string    `json:"handover,omitempty"`
	Prev     *Assignee `json:"-"`
	Branch   bool      `json:"branch,omitempty"`
	Rev      int       `json:"rev"`
	Test     bool      `json:"test,omitempty"`
	// Branches of a parallel step (for the caller who sees the case).
	Branches []Branch `json:"branches,omitempty"`
	// Form: the fields the caller fills in or sees in the current step.
	Form []StepField `json:"form,omitempty"`
	// Progress of an approval step, e.g. "1/2".
	Progress string `json:"progress,omitempty"`
	// for the caller
	Actions   []Action `json:"actions"`
	CanCancel bool     `json:"can_cancel"`
	Events    []Event  `json:"events,omitempty"`
}

// Event is a recorded transition.
type Event struct {
	ID          int64     `json:"id"`
	FromStep    *string   `json:"from_step"`
	ToStep      *string   `json:"to_step"`
	Action      string    `json:"action"`
	ActionLabel string    `json:"action_label"`
	Note        string    `json:"note"`
	Actor       string    `json:"actor"`
	At          time.Time `json:"at"`
	Signature   *int64    `json:"signature,omitempty"` // id of the signature of the decision
}

const instCols = `i.id, i.element_id, e.category_id, e.titles, i.lang, i.title, i.data, i.definition, i.step, i.status, i.started_by,
	COALESCE((SELECT u.display_name || ' (' || u.username || '@' || u.provider || ')' FROM users u WHERE u.id=i.started_by), ''),
	i.started_at, i.updated_at, i.closed_at,
	COALESCE((SELECT array_agg(DISTINCT x.to_step) FROM plugin_workflow_events x WHERE x.instance_id=i.id AND x.to_step IS NOT NULL), '{}'),
	COALESCE((SELECT array_agg(DISTINCT x.actor_id) FROM plugin_workflow_events x WHERE x.instance_id=i.id AND x.actor_id IS NOT NULL), '{}'),
	i.step_entered_at, i.wake_at, i.last_error, i.definer_id, i.trigger, i.depth, i.parent_id, i.parent_step, i.due_at, i.warned, i.escalated, i.override, i.handover, i.prev, i.branch, i.rev, i.test`

func scanInstance(r pgx.CollectableRow) (*Instance, error) {
	var x Instance
	err := r.Scan(&x.ID, &x.ElementID, &x.CategoryID, &x.Element, &x.Lang, &x.Title, &x.Data, &x.Definition, &x.Step, &x.Status,
		&x.StartedByID, &x.StartedBy, &x.StartedAt, &x.UpdatedAt, &x.ClosedAt, &x.Visited, &x.Actors, &x.StepEntered, &x.WakeAt, &x.LastError,
		&x.DefinerID, &x.Trigger, &x.Depth, &x.ParentID, &x.ParentStep, &x.DueAt, &x.Warned, &x.Escalated, &x.Override, &x.Handover, &x.Prev, &x.Branch, &x.Rev, &x.Test)
	if err != nil {
		return nil, err
	}
	x.Definition.normalizeTypes()
	if s := x.Definition.Step(x.Step); s != nil {
		x.StepName = s.Name
		if x.Status == "done" {
			x.Outcome = s.Outcome
		}
	}
	x.Actions = []Action{}
	return &x, nil
}

// who resolves group and role names for the caller.
type who struct {
	p      *identity.Principal
	groups map[string]bool
	roles  map[string]bool
	// users: handles of the people the caller is the deputy of today
	users map[string]bool
}

func (pl *Plugin) caller(ctx context.Context) (*who, error) {
	p := identity.From(ctx)
	w := &who{p: p, groups: map[string]bool{}, roles: map[string]bool{}, users: map[string]bool{}}
	if p == nil {
		return w, nil
	}
	if p.ID() > 0 {
		if err := pl.deputyFor(ctx, w); err != nil {
			return nil, err
		}
	}
	if len(p.Groups) > 0 {
		rows, err := pl.host.DB.Query(ctx, `SELECT name FROM groups WHERE id = ANY($1)`, p.Groups)
		if err != nil {
			return nil, err
		}
		names, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			w.groups[strings.ToLower(n)] = true
		}
		for _, id := range p.Groups {
			w.groups["#"+strconv.FormatInt(id, 10)] = true // by ID: stays valid when renamed
		}
	}
	if len(p.Roles) > 0 {
		rows, err := pl.host.DB.Query(ctx, `SELECT name FROM roles WHERE id = ANY($1)`, p.Roles)
		if err != nil {
			return nil, err
		}
		names, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			w.roles[strings.ToLower(n)] = true
		}
		for _, id := range p.Roles {
			w.roles["#"+strconv.FormatInt(id, 10)] = true
		}
	}
	return w, nil
}

// assignee returns who handles step s of x (after an escalation: the new assignee).
func (x *Instance) assignee(s *Step) Assignee {
	if x.Test && s.Manual() { // a test run: everything goes to the person testing
		return Assignee{Type: "starter"}
	}
	if x.Override != nil && s.ID == x.Step {
		return *x.Override
	}
	return s.Assignee
}

// assigned reports whether the caller is responsible for step s of x.
func (w *who) assigned(x *Instance, s *Step) bool {
	if w.p == nil || w.p.ID() == 0 || s == nil || !s.Manual() {
		return false
	}
	as := x.assignee(s)
	v := strings.ToLower(as.Value)
	switch as.Type {
	case "starter":
		return x.StartedByID != nil && *x.StartedByID == w.p.ID()
	case "user":
		return v == strings.ToLower(w.p.Handle()) || (!strings.Contains(v, "@") && v == strings.ToLower(w.p.Username) && w.p.Provider == "local") || w.users[v]
	case "group":
		return w.groups[v]
	case "role":
		return w.roles[v]
	}
	return false
}

// visible reports whether the caller may see the case (manage = Manage
// permission on the category).
func (w *who) visible(x *Instance, manage bool) bool {
	if w.p == nil {
		return false
	}
	if manage || w.p.Admin() || (x.StartedByID != nil && *x.StartedByID == w.p.ID()) || slices.Contains(x.Actors, w.p.ID()) {
		return true
	}
	steps := append([]string{x.Step}, x.Visited...)
	if len(x.Definition.Steps) > 0 {
		steps = append(steps, x.Definition.Steps[0].ID)
	}
	for _, id := range steps {
		if w.assigned(x, x.Definition.Step(id)) {
			return true
		}
	}
	return false
}

// System actions of managers (failed webhooks).
const (
	ActRetry = "_retry"
	ActSkip  = "_skip"
)

// decorate fills the caller specific fields (available actions).
func (pl *Plugin) decorate(ctx context.Context, w *who, x *Instance, manage bool) {
	x.Actions = []Action{}
	x.CanCancel = false
	if x.Status != "open" {
		return
	}
	x.CanCancel = manage || w.p.Admin() || (x.StartedByID != nil && w.p != nil && *x.StartedByID == w.p.ID())
	s := x.Definition.Step(x.Step)
	if s == nil {
		return
	}
	switch s.Type {
	case StepTask:
		if w.assigned(x, s) { // only the assignees of the step (also not administrators)
			if x.Handover == "ask" {
				x.Actions = handoverActions(w, x, s) // the starter answers first
			} else {
				x.Actions = append(append([]Action{}, s.Actions...), handoverActions(w, x, s)...)
			}
			markSign(s, x.Actions)
			x.Form = s.Form
		}
	case StepApproval:
		tx, err := pl.host.DB.Begin(ctx)
		if err != nil {
			return
		}
		defer func() { _ = tx.Rollback(ctx) }() // no-op after commit
		voted, err := pl.voters(ctx, tx, x)
		if err != nil {
			return
		}
		need, err := pl.required(ctx, tx, x, s)
		if err != nil {
			return
		}
		x.Progress = fmt.Sprintf("%d/%d", len(voted), need)
		if w.assigned(x, s) && w.p != nil && !voted[w.p.ID()] {
			x.Actions = []Action{{ID: "approve", Label: s.ApproveLabel, Next: s.Next, Note: "optional"},
				{ID: "reject", Label: s.RejectLabel, Next: s.NextElse, Note: "required"}}
			markSign(s, x.Actions)
			x.Form = s.Form
		}
	case StepWebhook, StepHTTP, StepCreate, StepProps, StepSubflow, StepAccess, StepMember, StepMove, StepFile, StepCalendar:
		if x.WakeAt == nil && x.LastError != "" && (manage || w.p.Admin()) { // failed, waiting for a decision
			x.Actions = []Action{{ID: ActRetry, Next: s.ID}, {ID: ActSkip, Next: s.Next, Note: "optional"}}
		}
	}
}

// manage reports whether the caller manages the cases of a category
// (permission workflow.process.cases or workflow administrator).
func (pl *Plugin) manage(ctx context.Context, category int64) bool {
	return pl.host.Can(ctx, category, permCases)
}

func (pl *Plugin) load(ctx context.Context, id int64) (*Instance, *who, bool, error) {
	rows, err := pl.host.DB.Query(ctx, `SELECT `+instCols+` FROM plugin_workflow_instances i JOIN elements e ON e.id=i.element_id WHERE i.id=$1`, id)
	if err != nil {
		return nil, nil, false, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanInstance)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, false, apperr.NotFound("case")
	}
	if err != nil {
		return nil, nil, false, err
	}
	if _, err := pl.host.Content.ElementCategory(ctx, x.ElementID, acl.View); err != nil {
		return nil, nil, false, apperr.NotFound("case")
	}
	w, err := pl.caller(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	m := pl.manage(ctx, x.CategoryID)
	if !w.visible(x, m) {
		return nil, nil, false, apperr.NotFound("case")
	}
	pl.decorate(ctx, w, x, m)
	hideFields(w, x, m)
	pl.loadBranches(ctx, x)
	return x, w, m, nil
}

func (pl *Plugin) events(ctx context.Context, id int64) ([]Event, error) {
	rows, err := pl.host.DB.Query(ctx, `SELECT id, from_step, to_step, action, action_label, note, actor, at, signature_id FROM plugin_workflow_events WHERE instance_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Event])
}

// definition loads the current process of an element in lang (or its fallback).
func (pl *Plugin) definition(ctx context.Context, element int64, lang string) (*Definition, string, error) {
	d, shown, _, err := pl.definitionBy(ctx, element, lang)
	return d, shown, err
}

// definitionBy is definition with the person who saved that version (the
// "definer": automatic steps act with their rights).
func (pl *Plugin) definitionBy(ctx context.Context, element int64, lang string) (*Definition, string, int64, error) {
	return definitionByQ(ctx, pl.host.DB, element, lang)
}

// definitionByQ is definitionBy with q (inside a transaction: no second
// connection, S-01).
func definitionByQ(ctx context.Context, q db.Querier, element int64, lang string) (*Definition, string, int64, error) {
	var raw json.RawMessage
	var shown string
	var by *int64
	var rev int
	err := q.QueryRow(ctx, `SELECT r.body, r.lang, r.created_by, r.rev FROM element_revisions r JOIN elements e ON e.id=r.element_id
		WHERE r.element_id=$1 AND e.type='workflow.process' AND e.deleted_at IS NULL
		ORDER BY (r.lang=$2) DESC, (r.lang=e.original_lang) DESC, r.rev DESC LIMIT 1`, element, lang).Scan(&raw, &shown, &by, &rev)
	if err != nil {
		return nil, "", 0, apperr.NotFound("workflow")
	}
	d, err := Decode(raw)
	if err != nil {
		return nil, "", 0, err
	}
	d.normalizeTypes()
	d.Rev = rev
	var definer int64
	if by != nil {
		definer = *by
	}
	return d, shown, definer, nil
}

// startInput describes a new case.
type startInput struct {
	Title, Lang string
	Data        map[string]any
	Trigger     map[string]string
	Depth       int
	ParentID    *int64
	ParentStep  string
	// Via: how it was started (session, form, trigger:ID, workflow);
	// Manual: by a person with the start form (refused for NoManual).
	Via    string
	Manual bool
	// Test: a test run (all tasks to the person testing, nothing leaves the wiki).
	Test bool
}

// startTx starts a case of workflow element with the principal of ctx in tx.
func (pl *Plugin) startTx(ctx context.Context, tx pgx.Tx, element int64, in startInput) (int64, error) {
	p := identity.From(ctx)
	if p == nil || p.ID() == 0 {
		return 0, apperr.ErrUnauthorized
	}
	// everything with tx: no second connection while the case is written (S-01)
	var cat int64
	if err := tx.QueryRow(ctx, `SELECT e.category_id FROM elements e JOIN categories c ON c.id=e.category_id
		WHERE e.id=$1 AND e.deleted_at IS NULL AND c.deleted_at IS NULL`, element).Scan(&cat); err != nil || !pl.host.ACL.Can(ctx, p, cat, acl.View) {
		return 0, apperr.NotFound("workflow")
	}
	def, shown, definer, err := definitionByQ(ctx, tx, element, in.Lang)
	if err != nil {
		return 0, err
	}
	if def.NoManual && in.Manual && (p.Via == "" || p.Via == "session" || p.Via == "apikey" || p.Via == "cli" || p.Via == "ssh") {
		return 0, apperr.Forbidden("this workflow is started by its triggers only")
	}
	if in.Data == nil {
		in.Data = map[string]any{}
	}
	data, err := validateData(startFields(def.Fields), in.Data)
	if err != nil {
		return 0, err
	}
	if !in.Manual && utf8.RuneCountInString(in.Title) > 200 { // titles of triggers and workflows
		in.Title = string([]rune(in.Title)[:200])
	}
	title, err := clean(in.Title, 200)
	if err != nil {
		return 0, err
	}
	if title == "" {
		title = titleFrom(def.Fields, data)
	}
	if title == "" {
		title = p.DisplayName
	}
	if in.Trigger == nil {
		in.Trigger = map[string]string{}
	}
	first := def.Steps[0]
	var iid int64
	if err := tx.QueryRow(ctx, `INSERT INTO plugin_workflow_instances(element_id, lang, title, data, definition, step, status, started_by,
		definer_id, trigger, depth, parent_id, parent_step, rev, test) VALUES ($1,$2,$3,$4,$5,$6,'open',$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`,
		element, shown, title, data, def, first.ID, p.ID(), nullID(definer), in.Trigger, in.Depth, in.ParentID, in.ParentStep, def.Rev, in.Test).Scan(&iid); err != nil {
		return 0, err
	}
	label := ""
	if strings.HasPrefix(in.Via, "trigger:") {
		label = in.Via
	}
	if err := event(ctx, tx, iid, nil, sp(first.ID), "start", label, "", actorInfo{p: p}); err != nil {
		return 0, err
	}
	x, err := pl.loadTx(ctx, tx, iid)
	if err != nil {
		return 0, err
	}
	pl.emit(ctx, tx, "workflow.started", x, actorInfo{p: p})
	x.Step = "" // entering the first step (nothing is left)
	if err := pl.enter(ctx, tx, x, first.ID, actorInfo{p: p}); err != nil {
		return 0, err
	}
	return iid, audit.Log(ctx, tx, "workflow.start", "workflow_case", iid, nil, map[string]any{"element_id": element, "title": title, "via": in.Via})
}

var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// validateData checks submitted form values against the fields.
func validateData(fields []Field, in map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for _, f := range fields {
		v, ok := in[f.ID]
		if !ok || v == nil {
			if f.Required {
				return nil, apperr.BadRequest("%s is required", f.Label)
			}
			continue
		}
		switch f.Type {
		case "checkbox":
			b, ok := v.(bool)
			if !ok {
				return nil, apperr.BadRequest("%s: expected true or false", f.Label)
			}
			if f.Required && !b {
				return nil, apperr.BadRequest("%s must be confirmed", f.Label)
			}
			out[f.ID] = b
		case "file":
			m, ok := v.(map[string]any)
			hash, _ := m["hash"].(string)
			if !ok || !hashRe.MatchString(hash) {
				return nil, apperr.BadRequest("%s: invalid file", f.Label)
			}
			name, _ := m["name"].(string)
			mime, _ := m["mime"].(string)
			size, _ := m["size"].(float64)
			if utf8.RuneCountInString(name) > 255 {
				name = string([]rune(name)[:255])
			}
			out[f.ID] = map[string]any{"hash": hash, "name": name, "mime": mime, "size": size}
		default:
			s, ok := v.(string)
			if !ok {
				if n, isNum := v.(float64); isNum && f.Type == "number" {
					s = strconv.FormatFloat(n, 'f', -1, 64)
				} else {
					return nil, apperr.BadRequest("%s: expected text", f.Label)
				}
			}
			s = strings.TrimSpace(s)
			if s == "" {
				if f.Required {
					return nil, apperr.BadRequest("%s is required", f.Label)
				}
				continue
			}
			max := 500
			if f.Type == "textarea" {
				max = 20000
			}
			if utf8.RuneCountInString(s) > max {
				return nil, apperr.BadRequest("%s: at most %d characters", f.Label, max)
			}
			switch f.Type {
			case "email":
				if a, err := mail.ParseAddress(s); err != nil || a.Address != s {
					return nil, apperr.BadRequest("%s: invalid e-mail address", f.Label)
				}
			case "date":
				if _, err := time.Parse("2006-01-02", s); err != nil {
					return nil, apperr.BadRequest("%s: invalid date (YYYY-MM-DD)", f.Label)
				}
			case "number":
				if _, err := strconv.ParseFloat(s, 64); err != nil {
					return nil, apperr.BadRequest("%s: invalid number", f.Label)
				}
			case "select":
				if !slices.Contains(f.Options, s) {
					return nil, apperr.BadRequest("%s: invalid choice", f.Label)
				}
			}
			out[f.ID] = s
		}
	}
	return out, nil
}

func titleFrom(fields []Field, data map[string]any) string {
	for _, f := range fields {
		if s, ok := data[f.ID].(string); ok && s != "" && f.Type != "textarea" {
			if utf8.RuneCountInString(s) > 120 {
				s = string([]rune(s)[:120])
			}
			return s
		}
	}
	return ""
}

// Setup registers the element type and the API.
func (pl *Plugin) Setup(host *sdk.Host) error {
	pl.host = host
	guardHosts.Store(host.Site, host)
	host.RegisterElementType(Type{name: func(ctx context.Context, kind, value string) string {
		id, err := strconv.ParseInt(strings.TrimPrefix(value, "#"), 10, 64)
		if err != nil {
			return value
		}
		var n string
		switch kind {
		case "group":
			err = host.DB.QueryRow(ctx, `SELECT name FROM groups WHERE id=$1`, id).Scan(&n)
		case "role":
			err = host.DB.QueryRow(ctx, `SELECT name FROM roles WHERE id=$1`, id).Scan(&n)
		default:
			return value
		}
		if err != nil {
			return value
		}
		return n
	}})
	host.Files.AddReference("plugin_workflow_instances", "data")
	host.RegisterWorker("timers", 30*time.Second, pl.runTimers)
	host.RegisterWorker("triggers", 30*time.Second, pl.runTriggers)
	if host.Webhooks != nil {
		host.Webhooks.Listen(pl.listen) // events that start cases (triggers)
	}
	pl.triggerRoutes(host)
	pl.deputyRoutes(host)
	pl.linkRoutes(host)
	pl.statsRoutes(host)
	pl.signRoutes(host)
	host.Signatures.RegisterViewer(SignPurpose, pl.signViewer)
	host.RegisterWorker("retention", 6*time.Hour, pl.retention)
	host.RegisterWorker("grants", 10*time.Minute, pl.expireGrants)

	// GET /meta describes step types, rule operators and placeholders (editor).
	host.Handle("GET", "/meta", func(w http.ResponseWriter, r *http.Request) (any, error) {
		st, err := host.Mail.GetStatus(r.Context())
		mailOK := err == nil && st.Transport != ""
		return map[string]any{"step_types": StepTypes, "rule_ops": RuleOps, "placeholders": Placeholders,
			"field_types": FieldTypes, "mail_configured": mailOK, "http_allow": len(host.Config.Get().Plugins.Macros.HTTPAllow) > 0,
			"trigger_types": TriggerTypes, "content_events": ContentEvents, "trigger_events": TriggerEvents,
			"max_depth": MaxDepth, "per_hour": host.Limit(LimitPerHour)}, nil
	})

	// GET /elements/{id}/instances lists the cases of a workflow the caller can see.
	host.Handle("GET", "/elements/{id}/instances", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		cat, err := host.Content.ElementCategory(ctx, id, acl.View)
		if err != nil {
			return nil, err
		}
		def, shown, err := pl.definition(ctx, id, r.URL.Query().Get("lang"))
		if err != nil {
			return nil, err
		}
		me, err := pl.caller(ctx)
		if err != nil {
			return nil, err
		}
		status := r.URL.Query().Get("status")
		rows, err := host.DB.Query(ctx, `SELECT `+instCols+` FROM plugin_workflow_instances i JOIN elements e ON e.id=i.element_id
			WHERE i.element_id=$1 AND NOT i.branch AND i.test = $3 AND ($2='' OR i.status=$2) ORDER BY i.updated_at DESC LIMIT 1000`, id, status, r.URL.Query().Get("test") == "1")
		if err != nil {
			return nil, err
		}
		all, err := pgx.CollectRows(rows, scanInstance)
		if err != nil {
			return nil, err
		}
		m := pl.manage(ctx, cat)
		list := []*Instance{}
		for _, x := range all {
			if me.visible(x, m) {
				pl.decorate(ctx, me, x, m)
				hideFields(me, x, m)
				list = append(list, x)
			}
		}
		_, cerr := pl.mayChange(ctx, id)
		return map[string]any{"instances": list, "can_start": me.p != nil && me.p.ID() != 0 && !def.NoManual, "fields": startFields(def.Fields),
			"can_test": cerr == nil, "can_stats": cerr == nil || m, "rev": def.Rev,
			"lang": shown, "first_step": def.Steps[0].Name, "manage": m}, nil
	})

	// POST /elements/{id}/instances starts a case: {"data": {...}, "title": "", "lang": ""}.
	host.Handle("POST", "/elements/{id}/instances", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		p := identity.From(ctx)
		if p == nil || p.ID() == 0 {
			return nil, apperr.ErrUnauthorized
		}
		if _, err := host.Content.ElementCategory(ctx, id, acl.View); err != nil {
			return nil, err
		}
		var in struct {
			Title string         `json:"title"`
			Lang  string         `json:"lang"`
			Data  map[string]any `json:"data"`
			Test  bool           `json:"test"` // a test run (who may change the workflow)
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if in.Test {
			if _, err := pl.mayChange(ctx, id); err != nil {
				return nil, err
			}
		}
		var iid int64
		err = pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
			iid, err = pl.startTx(ctx, tx, id, startInput{Title: in.Title, Lang: in.Lang, Data: in.Data, Via: p.Via, Manual: !in.Test, Test: in.Test})
			return err
		})
		if err != nil {
			return nil, err
		}
		x, _, _, err := pl.load(ctx, iid)
		if err != nil {
			return nil, err
		}
		return h.Created{V: x}, nil
	})

	// GET /instances/{id} returns a case with its history.
	host.Handle("GET", "/instances/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		x, _, _, err := pl.load(r.Context(), id)
		if err != nil {
			return nil, err
		}
		if x.Events, err = pl.events(r.Context(), id); err != nil {
			return nil, err
		}
		return x, nil
	})

	// GET /instances/{id}/files/{field} downloads a file of a case form
	// (only for people who can see the case).
	host.Handle("GET", "/instances/{id}/files/{field}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		x, _, _, err := pl.load(r.Context(), id)
		if err != nil {
			return nil, err
		}
		m, ok := x.Data[r.PathValue("field")].(map[string]any)
		hash, _ := m["hash"].(string)
		if !ok || hash == "" {
			return nil, apperr.NotFound("file")
		}
		f, err := host.Files.Get(r.Context(), hash)
		if err != nil {
			return nil, err
		}
		name, _ := m["name"].(string)
		host.Files.Serve(w, r, f, name, r.URL.Query().Get("inline") == "1")
		return h.Handled{}, nil
	})

	// POST /instances/{id}/actions performs an action: {"action": "approve", "note": ""}.
	host.Handle("POST", "/instances/{id}/actions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Action string         `json:"action"`
			Note   string         `json:"note"`
			To     string         `json:"to"`   // _delegate: the person who takes over
			Data   map[string]any `json:"data"` // fields of the form of the step
			// Signature of the decision (POST /instances/{id}/sign first)
			Signature *signIn `json:"signature"`
			Lang      string  `json:"lang"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return pl.transition(r.Context(), id, in.Action, in.Note, actionInput{To: in.To, Data: in.Data, Sign: in.Signature, Lang: in.Lang})
	})

	// POST /instances/{id}/cancel cancels an open case: {"note": ""}.
	host.Handle("POST", "/instances/{id}/cancel", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Note string `json:"note"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return pl.transition(r.Context(), id, "", in.Note, actionInput{})
	})

	// GET /tasks lists the open cases waiting for the caller and the cases
	// the caller started.
	host.Handle("GET", "/tasks", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		me, err := pl.caller(ctx)
		if err != nil {
			return nil, err
		}
		if me.p == nil || me.p.ID() == 0 {
			return nil, apperr.ErrUnauthorized
		}
		rows, err := host.DB.Query(ctx, `SELECT `+instCols+` FROM plugin_workflow_instances i JOIN elements e ON e.id=i.element_id
			WHERE e.deleted_at IS NULL AND (i.status='open' OR i.started_by=$1) ORDER BY i.updated_at DESC LIMIT 2000`, me.p.ID())
		if err != nil {
			return nil, err
		}
		all, err := pgx.CollectRows(rows, scanInstance)
		if err != nil {
			return nil, err
		}
		cats := make([]int64, len(all))
		for i, x := range all {
			cats[i] = x.CategoryID
		}
		perms, err := host.ACL.PermsMap(ctx, me.p, cats)
		if err != nil {
			return nil, err
		}
		tasks, started := []*Instance{}, []*Instance{}
		for _, x := range all {
			if perms[x.CategoryID]&acl.View == 0 {
				continue
			}
			m := pl.host.Can(ctx, x.CategoryID, permCases)
			if x.Status == "open" && me.assigned(x, x.Definition.Step(x.Step)) {
				pl.decorate(ctx, me, x, m)
				if len(x.Actions) > 0 { // e.g. not yet voted
					tasks = append(tasks, x)
					continue
				}
			}
			if x.StartedByID != nil && *x.StartedByID == me.p.ID() && len(started) < 100 {
				pl.decorate(ctx, me, x, m)
				started = append(started, x)
			}
		}
		return map[string]any{"tasks": tasks, "started": started}, nil
	})
	return nil
}

// loadTx loads a case inside a transaction and locks it.
func (pl *Plugin) loadTx(ctx context.Context, tx pgx.Tx, id int64) (*Instance, error) {
	rows, err := tx.Query(ctx, `SELECT `+instCols+` FROM plugin_workflow_instances i JOIN elements e ON e.id=i.element_id WHERE i.id=$1 FOR NO KEY UPDATE OF i`, id)
	if err != nil {
		return nil, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanInstance)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("case")
	}
	return x, err
}

// transition performs action (or cancels the case when action is "").
// actionInput holds what comes with an action besides the note.
type actionInput struct {
	To   string
	Data map[string]any
	Sign *signIn // signature of the decision (if the action needs one)
	Lang string
}

func (pl *Plugin) transition(ctx context.Context, id int64, action, note string, ai2 actionInput) (*Instance, error) {
	x, w, _, err := pl.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if x.Status != "open" {
		return nil, apperr.Conflict("the case is already closed")
	}
	note, err = clean(note, 5000)
	if err != nil {
		return nil, err
	}
	p := identity.From(ctx)
	s := x.Definition.Step(x.Step)
	var a *Action
	if action == "" {
		if !x.CanCancel {
			return nil, apperr.ErrForbidden
		}
	} else {
		for i := range x.Actions {
			if x.Actions[i].ID == action {
				a = &x.Actions[i]
			}
		}
		if a == nil {
			if s != nil && w.assigned(x, s) && s.Type == StepApproval {
				return nil, apperr.Conflict("you have already voted")
			}
			if s != nil && (s.Type == StepTask && slices.ContainsFunc(s.Actions, func(sa Action) bool { return sa.ID == action }) ||
				s.Type == StepApproval && (action == "approve" || action == "reject")) {
				return nil, apperr.ErrForbidden
			}
			return nil, apperr.BadRequest("unknown action %q", action)
		}
		if a.Note == "required" && note == "" {
			return nil, apperr.BadRequest("a note is required")
		}
	}
	// the form of the step: values the assignees fill in with a decision
	var stepValues map[string]any
	if a != nil && s != nil && !isHandover(action) && action != ActRetry && action != ActSkip && action != "reject" {
		if stepValues, err = stepData(&x.Definition, s, ai2.Data); err != nil {
			return nil, err
		}
	}
	err = pgx.BeginFunc(ctx, pl.host.DB, func(tx pgx.Tx) error {
		cur, err := pl.loadTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if cur.Status != "open" || cur.Step != x.Step || !cur.StepEntered.Equal(x.StepEntered) {
			return apperr.Conflict("the case was changed by someone else, please reload")
		}
		// a signature (if needed) covers exactly this decision on this state of the case
		var sigID int64
		if a != nil && signReq(s, *a) != "" {
			if sigID, err = pl.completeSign(ctx, tx, cur, s, *a, note, ai2.To, stepValues, ai2.Sign, ai2.Lang); err != nil {
				return err
			}
		}
		label := ""
		if a != nil {
			label = a.Label
		}
		ai := actorInfo{p: p, action: label, note: note}
		before := cur.Step
		if len(stepValues) > 0 {
			for k, v := range stepValues {
				cur.Data[k] = v
			}
			if err := saveData(ctx, tx, cur); err != nil {
				return err
			}
		}
		switch {
		case action == "":
			if err := event(ctx, tx, id, sp(cur.Step), nil, "cancel", "", note, ai); err != nil {
				return err
			}
			if err := pl.cancelTx(ctx, tx, cur); err != nil {
				return err
			}
			pl.emit(ctx, tx, "workflow.cancelled", cur, ai)
			if err := pl.notifyStarter(ctx, tx, cur, ai, "cancelled"); err != nil {
				return err
			}
		case s.Type == StepApproval && action == "approve":
			if a.Label == "" {
				ai.action = "approve"
			}
			if err := event(ctx, tx, id, sp(cur.Step), sp(cur.Step), "approve", a.Label, note, ai); err != nil {
				return err
			}
			if sigID != 0 {
				if err := signedEvent(ctx, tx, id, sigID); err != nil {
					return err
				}
			}
			voted, err := pl.voters(ctx, tx, cur)
			if err != nil {
				return err
			}
			need, err := pl.required(ctx, tx, cur, s)
			if err != nil {
				return err
			}
			if len(voted) >= need {
				if err := pl.enter(ctx, tx, cur, s.Next, ai); err != nil {
					return err
				}
			} else if _, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET updated_at=now() WHERE id=$1`, id); err != nil {
				return err
			}
		case isHandover(action):
			if err := pl.handover(ctx, tx, cur, s, action, ai2.To, ai); err != nil {
				return err
			}
		case action == ActRetry:
			if err := event(ctx, tx, id, sp(cur.Step), sp(cur.Step), "retry", "", note, ai); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET wake_at=now(), attempts=0, last_error='', updated_at=now() WHERE id=$1`, id); err != nil {
				return err
			}
		default: // task action, rejection, skip
			if err := event(ctx, tx, id, sp(cur.Step), sp(a.Next), action, a.Label, note, ai); err != nil {
				return err
			}
			if sigID != 0 {
				if err := signedEvent(ctx, tx, id, sigID); err != nil {
					return err
				}
			}
			if err := pl.enter(ctx, tx, cur, a.Next, ai); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, "workflow."+map[bool]string{true: "cancel", false: "action"}[action == ""], "workflow_case", id,
			map[string]string{"step": before}, map[string]string{"step": cur.Step, "action": action, "status": cur.Status})
	})
	if err != nil {
		return nil, err
	}
	nx, _, _, err := pl.load(ctx, id)
	if err != nil {
		// the actor may no longer see the case (e.g. handed over to another group)
		return &Instance{ID: id, Status: "open", Actions: []Action{}}, nil
	}
	if nx.Events, err = pl.events(ctx, id); err != nil {
		return nil, err
	}
	return nx, nil
}

func nullID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}
