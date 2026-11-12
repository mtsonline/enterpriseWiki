package workflow_process

// Triggers start cases without a person at the start form:
//
//   - schedule: at the minutes of a schedule (cron);
//   - content: a page in the sources was created, changed, deleted, moved,
//     restored or got new properties – optionally only when a property
//     takes a value ("status becomes review");
//   - event: a comment, a form submission, a registration for an event or
//     the end of another workflow (chains);
//   - webhook: another system calls a signed address.
//
// Security: a case started by a trigger has the rights of the person who
// saved the current version of the workflow ("definer"), or – for changes
// and events, if chosen – of the person who caused the event; that person
// must see the workflow and the object of the event (sources are resolved
// with their rights). Every start is recorded, starts per hour are limited
// and chains of workflows end after MaxDepth links.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/db"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/netlist"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
	"github.com/mtsonline/enterprisewiki/internal/webhook"
)

// Trigger types.
const (
	TrigSchedule = "schedule"
	TrigContent  = "content"
	TrigEvent    = "event"
	TrigWebhook  = "webhook"
)

// TriggerTypes lists the trigger types.
var TriggerTypes = []string{TrigSchedule, TrigContent, TrigEvent, TrigWebhook}

// ContentEvents are the changes of pages a content trigger can react to.
var ContentEvents = []string{"create", "update", "delete", "move", "restore", "properties"}

// TriggerEvents are the events an event trigger can react to.
var TriggerEvents = []string{"comment.create", "form.submitted", "workflow.completed", "workflow.cancelled",
	"event.registration", "event.registration_cancelled",
	"user.created", "user.disabled", "user.enabled", "user.deleted", "group.member_added", "group.member_removed"}

// accountEvent reports whether ev concerns accounts (people and groups):
// only who manages them may react to it (permission accountPerm).
func accountEvent(ev string) bool {
	return strings.HasPrefix(ev, "user.") || strings.HasPrefix(ev, "group.")
}

// accountPerm is the global permission needed for triggers of accounts.
const accountPerm = "core.users.manage"

var dataKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// MaxDepth ends chains of workflows started by workflows.
const MaxDepth = 5

// LimitPerHour is the expert setting for starts of one trigger per hour.
const LimitPerHour = "workflow.process.trigger_per_hour"

// Trigger starts cases.
type Trigger struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
	Off  bool   `json:"off,omitempty"`
	// Title of the case and values of its fields (placeholders trigger.*,
	// site.*, date, time).
	Title string            `json:"title,omitempty"`
	Data  map[string]string `json:"data,omitempty"`
	// RunAs: definer (default) or actor (content and event triggers).
	RunAs string `json:"run_as,omitempty"`
	// schedule
	Cron string `json:"cron,omitempty"`
	// content
	Events  []string `json:"events,omitempty"`
	Sources string   `json:"sources,omitempty"`
	Prop    *Rule    `json:"prop,omitempty"` // Field = key of the property
	// event
	Event    string `json:"event,omitempty"`
	Category int64  `json:"category,omitempty"` // 0: everywhere
	Workflow int64  `json:"workflow,omitempty"` // workflow.*: the other workflow
	Outcome  string `json:"outcome,omitempty"`  // workflow.completed: success | failure | neutral | ""
	Group    int64  `json:"group,omitempty"`    // group.*: only this group (0: all)
	// webhook: senders (addresses, networks, host names); empty: everybody
	// with the secret
	AllowFrom []string `json:"allow_from,omitempty"`
}

// webhookEvents returns the event names (of internal/webhook) a trigger listens to.
func (t *Trigger) webhookEvents() []string {
	switch t.Type {
	case TrigContent:
		out := []string{}
		for _, e := range t.Events {
			out = append(out, "element."+e)
		}
		if t.Prop != nil && !in("element.update", out...) {
			out = append(out, "element.update", "element.properties")
		}
		return out
	case TrigEvent:
		return []string{t.Event}
	}
	return []string{}
}

// Trigger returns the trigger with id.
func (d *Definition) Trigger(id string) *Trigger {
	for i := range d.Triggers {
		if d.Triggers[i].ID == id {
			return &d.Triggers[i]
		}
	}
	return nil
}

func (d *Definition) validateTriggers() error {
	if len(d.Triggers) > 20 {
		return apperr.BadRequest("at most 20 triggers")
	}
	fields := map[string]bool{}
	for _, f := range d.Fields {
		fields[f.ID] = true
	}
	seen := map[string]bool{}
	for i := range d.Triggers {
		t := &d.Triggers[i]
		var err error
		if !idRe.MatchString(t.ID) || seen[t.ID] {
			return apperr.BadRequest("trigger %d: id must be unique and consist of a-z, 0-9, _ and -", i+1)
		}
		seen[t.ID] = true
		if !in(t.Type, TriggerTypes...) {
			return apperr.BadRequest("trigger %q: unknown type %q", t.ID, t.Type)
		}
		if t.Name, err = clean(t.Name, 200); err != nil {
			return err
		}
		if t.Title, err = clean(t.Title, 500); err != nil {
			return err
		}
		if len(t.Data) > 50 {
			return apperr.BadRequest("trigger %q: at most 50 values", t.ID)
		}
		for k, v := range t.Data {
			if !fields[k] {
				return apperr.BadRequest("trigger %q: unknown field %q", t.ID, k)
			}
			if utf8.RuneCountInString(v) > 5000 {
				return apperr.BadRequest("trigger %q: value of %q too long", t.ID, k)
			}
		}
		if t.RunAs == "" {
			t.RunAs = "definer"
		}
		if !in(t.RunAs, "definer", "actor") || (t.RunAs == "actor" && t.Type != TrigContent && t.Type != TrigEvent) {
			return apperr.BadRequest("trigger %q: run as the definer (or the actor of changes and events)", t.ID)
		}
		// only the fields of the type
		cron, evs, src, prop, ev, cat, wf, out, allow, grp := t.Cron, t.Events, t.Sources, t.Prop, t.Event, t.Category, t.Workflow, t.Outcome, t.AllowFrom, t.Group
		t.Cron, t.Events, t.Sources, t.Prop, t.Event, t.Category, t.Workflow, t.Outcome, t.AllowFrom, t.Group = "", nil, "", nil, "", 0, 0, "", nil, 0
		switch t.Type {
		case TrigSchedule:
			if _, err := ParseCron(cron); err != nil {
				return apperr.BadRequest("trigger %q: %s", t.ID, err.Error())
			}
			t.Cron = strings.TrimSpace(cron)
		case TrigContent:
			list, err := content.ParseSources(src)
			if err != nil {
				return apperr.BadRequest("trigger %q: %s", t.ID, err.Error())
			}
			if !content.HasInclude(list) {
				return apperr.BadRequest("trigger %q: sources are required (which pages?)", t.ID)
			}
			t.Sources = content.FormatSources(list)
			for _, e := range evs {
				if !in(e, ContentEvents...) {
					return apperr.BadRequest("trigger %q: unknown change %q", t.ID, e)
				}
				if !in(e, t.Events...) {
					t.Events = append(t.Events, e)
				}
			}
			if prop != nil && strings.TrimSpace(prop.Field) != "" {
				r := *prop
				r.Field = strings.TrimSpace(r.Field)
				if len(r.Field) > 64 || !in(r.Op, RuleOps...) {
					return apperr.BadRequest("trigger %q: invalid property condition", t.ID)
				}
				if r.Value, err = clean(r.Value, 500); err != nil {
					return err
				}
				t.Prop = &r
			}
			if len(t.Events) == 0 && t.Prop == nil {
				return apperr.BadRequest("trigger %q: choose the changes", t.ID)
			}
		case TrigEvent:
			if !in(ev, TriggerEvents...) {
				return apperr.BadRequest("trigger %q: unknown event %q", t.ID, ev)
			}
			t.Event, t.Category = ev, max(cat, 0)
			if accountEvent(ev) {
				// accounts belong to no category; nobody else acts for them
				t.Category = 0
				if t.RunAs == "actor" {
					return apperr.BadRequest("trigger %q: changes of accounts run as the definer", t.ID)
				}
				if strings.HasPrefix(ev, "group.") {
					t.Group = max(grp, 0)
				}
			}
			if strings.HasPrefix(ev, "workflow.") {
				if wf <= 0 {
					return apperr.BadRequest("trigger %q: choose the other workflow", t.ID)
				}
				t.Workflow = wf
				if ev == "workflow.completed" && in(out, "success", "failure", "neutral") {
					t.Outcome = out
				}
			}
		case TrigWebhook:
			list, err := netlist.Parse(allow)
			if err != nil {
				return apperr.BadRequest("trigger %q: %s", t.ID, err.Error())
			}
			if len(list) > 0 {
				t.AllowFrom = list
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- index of triggers

// syncTriggers copies the triggers of the current version of a workflow
// into plugin_workflow_triggers (q: the transaction of the change).
func syncTriggers(ctx context.Context, q db.Querier, element int64) error {
	var raw json.RawMessage
	var deleted bool
	err := q.QueryRow(ctx, `SELECT r.body, e.deleted_at IS NOT NULL FROM elements e JOIN element_revisions r ON r.element_id=e.id
		WHERE e.id=$1 AND e.type='workflow.process' ORDER BY (r.lang=e.original_lang) DESC, r.rev DESC LIMIT 1`, element).Scan(&raw, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	since := map[string]time.Time{}
	rows, err := q.Query(ctx, `DELETE FROM plugin_workflow_triggers WHERE element_id=$1 RETURNING trigger_id, since`, element)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			rows.Close()
			return err
		}
		since[id] = at
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if deleted {
		return nil
	}
	d, err := Decode(raw)
	if err != nil {
		return nil // not ours to judge here; the definition is checked when saved
	}
	for _, t := range d.Triggers {
		if t.Off {
			continue
		}
		at, ok := since[t.ID]
		if !ok {
			at = time.Now()
		}
		if _, err := q.Exec(ctx, `INSERT INTO plugin_workflow_triggers(element_id, trigger_id, type, events, spec, since) VALUES ($1,$2,$3,$4,$5,$6)`,
			element, t.ID, t.Type, t.webhookEvents(), t, at); err != nil {
			return err
		}
	}
	return nil
}

// syncAll copies the triggers of all workflows (at the start of the worker).
func (pl *Plugin) syncAll(ctx context.Context) error {
	rows, err := pl.host.DB.Query(ctx, `SELECT id FROM elements WHERE type='workflow.process'`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := syncTriggers(ctx, pl.host.DB, id); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- events

type depthKey struct{}

// withDepth marks changes made by automatic steps of a case (chains).
func withDepth(ctx context.Context, d int) context.Context {
	return context.WithValue(ctx, depthKey{}, d)
}

func depthOf(ctx context.Context) int {
	d, _ := ctx.Value(depthKey{}).(int)
	return d
}

// listen receives all events (webhook service) in the transaction of the
// change: it keeps the index of triggers current and queues the events
// that may start cases.
func (pl *Plugin) listen(ctx context.Context, q db.Querier, ev webhook.Event) error {
	if !pl.host.Active() {
		return nil
	}
	if tx, ok := q.(pgx.Tx); ok {
		// a savepoint: a failure here never breaks the change itself
		sp, err := tx.Begin(ctx)
		if err != nil {
			return err
		}
		if err := pl.listenQ(ctx, sp, ev); err != nil {
			_ = sp.Rollback(ctx)
			pl.host.Log.Error("workflow triggers: event not queued", "event", ev.Name, "err", err)
			return nil
		}
		return sp.Commit(ctx)
	}
	return pl.listenQ(ctx, q, ev)
}

func (pl *Plugin) listenQ(ctx context.Context, q db.Querier, ev webhook.Event) error {
	if ev.Object.Type == "element" && strings.HasPrefix(ev.Name, "element.") {
		var typ string
		if err := q.QueryRow(ctx, `SELECT type FROM elements WHERE id=$1`, ev.Object.ID).Scan(&typ); err == nil && typ == "workflow.process" {
			if err := syncTriggers(ctx, q, ev.Object.ID); err != nil {
				return err
			}
		}
	}
	depth := depthOf(ctx)
	if ev.Object.Type == "workflow_case" {
		var d int
		if err := q.QueryRow(ctx, `SELECT depth FROM plugin_workflow_instances WHERE id=$1`, ev.Object.ID).Scan(&d); err == nil {
			depth = max(depth, d+1)
		}
	}
	var actor *int64
	if id := identity.From(ctx).ID(); id > 0 {
		actor = &id
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	at := ev.OccurredAt // only what happened after the trigger was saved
	if at.IsZero() {
		at = time.Now()
	}
	tag, err := q.Exec(ctx, `INSERT INTO plugin_workflow_queue(element_id, trigger_id, event, actor_id, depth)
		SELECT element_id, trigger_id, $2, $3, $4 FROM plugin_workflow_triggers WHERE $1 = ANY(events) AND since <= $5`, ev.Name, b, actor, depth, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		w := pl.host.Workers // read now: the timer must not touch the plugin (a new Setup may replace its host)
		time.AfterFunc(200*time.Millisecond, func() { w.Trigger("workflow.process.triggers") })
	}
	return nil
}

// ---------------------------------------------------------------- worker

var synced sync.Map // site -> true once all triggers were copied

// runTriggers handles queued events and due schedules (background worker).
func (pl *Plugin) runTriggers(ctx context.Context) error {
	if _, done := synced.Load(pl.host.Site); !done {
		if err := pl.syncAll(ctx); err != nil {
			return err
		}
		synced.Store(pl.host.Site, true)
	}
	for i := 0; i < 100; i++ {
		done, err := pl.queueOne(ctx)
		if err != nil {
			return err
		}
		if done {
			break
		}
	}
	if err := pl.schedules(ctx); err != nil {
		return err
	}
	_, err := pl.host.DB.Exec(ctx, `DELETE FROM plugin_workflow_fired WHERE at < now() - interval '90 days' AND ref LIKE 'cron:%'`)
	return err
}

// queued is an event waiting in plugin_workflow_queue.
type queued struct {
	id, element int64
	trigger     string
	ev          webhook.Event
	actor       *int64
	depth       int
	attempts    int
}

// queueOne handles one queued event; done is true when there is none.
func (pl *Plugin) queueOne(ctx context.Context) (done bool, err error) {
	err = pgx.BeginFunc(ctx, pl.host.DB, func(tx pgx.Tx) error {
		var qe queued
		var raw json.RawMessage
		err := tx.QueryRow(ctx, `SELECT id, element_id, trigger_id, event, actor_id, depth, attempts FROM plugin_workflow_queue
			WHERE next_at <= now() ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&qe.id, &qe.element, &qe.trigger, &raw, &qe.actor, &qe.depth, &qe.attempts)
		if errors.Is(err, pgx.ErrNoRows) {
			done = true
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &qe.ev); err != nil {
			_, err = tx.Exec(ctx, `DELETE FROM plugin_workflow_queue WHERE id=$1`, qe.id)
			return err
		}
		ferr := pl.fire(ctx, &qe)
		if ferr != nil && !errors.Is(ferr, errSkip) && qe.attempts < 4 {
			// e.g. the database was busy: again in 1, 2, 4, 8 minutes
			_, err := tx.Exec(ctx, `UPDATE plugin_workflow_queue SET attempts=attempts+1, next_at=now() + make_interval(mins => $2) WHERE id=$1`, qe.id, 1<<qe.attempts)
			return err
		}
		if ferr != nil && !errors.Is(ferr, errSkip) {
			pl.noteTrigger(ctx, qe.element, qe.trigger, ferr.Error())
		}
		_, err = tx.Exec(ctx, `DELETE FROM plugin_workflow_queue WHERE id=$1`, qe.id)
		return err
	})
	return done, err
}

// errSkip: the event does not start a case (no error).
var errSkip = errors.New("skip")

// noteTrigger records the last result of a trigger (status for the editor).
func (pl *Plugin) noteTrigger(ctx context.Context, element int64, trigger, failure string) {
	_, _ = pl.host.DB.Exec(ctx, `UPDATE plugin_workflow_triggers SET spec = spec || jsonb_build_object('_last_at', now()::text, '_last_error', $3::text)
		WHERE element_id=$1 AND trigger_id=$2`, element, trigger, failure)
}

// fire decides about one event and starts the case.
func (pl *Plugin) fire(ctx context.Context, qe *queued) error {
	def, _, definer, err := pl.definitionBy(ctx, qe.element, "")
	if err != nil {
		return errSkip // the workflow is gone
	}
	t := def.Trigger(qe.trigger)
	if t == nil || t.Off {
		return errSkip
	}
	if qe.depth > MaxDepth {
		return fmt.Errorf("more than %d workflows started each other in a chain; stopped", MaxDepth)
	}
	uid := definer
	if t.RunAs == "actor" {
		if qe.actor == nil {
			return errSkip // a change without a person (system)
		}
		uid = *qe.actor
	}
	pctx, err := pl.as(ctx, uid, qe.element)
	if err != nil {
		return err
	}
	ev := qe.ev
	ref := "q:" + strconv.FormatInt(qe.id, 10)
	switch t.Type {
	case TrigContent:
		if ev.Object.Type != "element" {
			return errSkip
		}
		ok, err := pl.inSources(pctx, t.Sources, ev.Object.ID)
		if err != nil || !ok {
			return err
		}
		if t.Prop != nil {
			hist, ok, err := pl.propBecame(ctx, ev.Object.ID, t.Prop)
			if err != nil || !ok {
				return err
			}
			ref = "prop:" + strconv.FormatInt(hist, 10)
		} else if !in(strings.TrimPrefix(ev.Name, "element."), t.Events...) {
			return errSkip
		}
	case TrigEvent:
		if ev.Name != t.Event {
			return errSkip
		}
		if ev.CategoryID != 0 && !pl.host.UserCan(ctx, uid, ev.CategoryID, acl.View) {
			return errSkip // nobody learns about what they may not see
		}
		if t.Category != 0 {
			var below bool
			if err := pl.host.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM categories a, categories c WHERE a.id=$1 AND c.id=$2 AND a.path @> c.path)`,
				t.Category, ev.CategoryID).Scan(&below); err != nil || !below {
				return err
			}
		}
		if accountEvent(t.Event) {
			// only who manages accounts learns about them (checked when firing:
			// the rights of the definer may have changed since saving)
			if !perm.HasP(identity.From(pctx), accountPerm) {
				return fmt.Errorf("%s may no longer manage accounts (%s)", identity.From(pctx).Handle(), accountPerm)
			}
			if g, _ := ev.Data["group_id"].(float64); t.Group != 0 && int64(g) != t.Group {
				return errSkip
			}
		}
		if strings.HasPrefix(t.Event, "workflow.") {
			wf, _ := ev.Data["workflow_id"].(float64)
			if int64(wf) != t.Workflow {
				return errSkip
			}
			if out, _ := ev.Data["outcome"].(string); t.Outcome != "" && out != t.Outcome {
				return errSkip
			}
		}
	case TrigWebhook:
		if d, _ := ev.Data["_ref"].(string); d != "" {
			ref = d
		}
	default:
		return errSkip
	}
	return pl.startByTrigger(pctx, qe.element, def, t, ref, triggerVars(t, ev), qe.depth)
}

// as returns ctx with the principal uid, who must see the workflow.
func (pl *Plugin) as(ctx context.Context, uid, workflow int64) (context.Context, error) {
	if uid <= 0 {
		return nil, fmt.Errorf("the workflow has no definer (the person who saved it was deleted)")
	}
	p, err := pl.host.Auth.LoadPrincipal(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("the account %d is not active", uid)
	}
	p.Via = "workflow"
	pctx := identity.With(ctx, p)
	if _, err := pl.host.Content.ElementCategory(pctx, workflow, acl.View); err != nil {
		return nil, fmt.Errorf("%s can no longer see the workflow", p.Handle())
	}
	return pctx, nil
}

// inSources reports whether element is in the sources (rights of ctx).
func (pl *Plugin) inSources(ctx context.Context, sources string, element int64) (bool, error) {
	list, err := content.ParseSources(sources)
	if err != nil {
		return false, nil
	}
	set, err := pl.host.Content.ResolveSources(ctx, list)
	if err != nil || set.Empty() {
		return false, err
	}
	args := []any{element}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	var ok bool
	err = pl.host.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM elements e WHERE e.id=$1 AND `+content.SourceSQL(set, arg)+`)`, args...).Scan(&ok)
	return ok, err
}

// propBecame reports whether the latest change of a property made the rule
// true (it was not true before) within the last hour; hist is the id of
// that change.
func (pl *Plugin) propBecame(ctx context.Context, element int64, r *Rule) (hist int64, ok bool, err error) {
	rows, err := pl.host.DB.Query(ctx, `SELECT id, raw, changed_at FROM plugin_prop_history WHERE element_id=$1 AND key=$2 ORDER BY id DESC LIMIT 2`, element, r.Field)
	if err != nil {
		return 0, false, err
	}
	type row struct {
		id  int64
		raw []byte
		at  time.Time
	}
	var list []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.raw, &x.at); err != nil {
			rows.Close()
			return 0, false, err
		}
		list = append(list, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(list) == 0 || time.Since(list[0].at) > time.Hour {
		return 0, false, err
	}
	matches := func(raw []byte) bool {
		x := &Instance{Data: map[string]any{}}
		var v any
		if len(raw) > 0 && json.Unmarshal(raw, &v) == nil {
			x.Data["v"] = v
		}
		return evaluate(x, &Step{Rules: []Rule{{Field: "v", Op: r.Op, Value: r.Value}}})
	}
	if !matches(list[0].raw) {
		return 0, false, nil
	}
	if len(list) == 2 && matches(list[1].raw) {
		return 0, false, nil // it was already true
	}
	return list[0].id, true, nil
}

// triggerVars are the placeholders trigger.* of a case.
func triggerVars(t *Trigger, ev webhook.Event) map[string]string {
	v := map[string]string{"trigger.id": t.ID, "trigger.type": t.Type, "trigger.name": t.Name, "trigger.event": ev.Name,
		"trigger.actor": ev.Actor, "trigger.time": ev.OccurredAt.Local().Format("2006-01-02 15:04"),
		"trigger.object.type": ev.Object.Type, "trigger.object.title": ev.Object.Title, "trigger.object.url": ev.Object.URL}
	if ev.Object.ID != 0 {
		v["trigger.object.id"] = strconv.FormatInt(ev.Object.ID, 10)
	}
	flatten("trigger.data", ev.Data, v, 0)
	return v
}

// flatten turns a JSON value into text values (at most 100, depth 3).
func flatten(prefix string, val any, out map[string]string, depth int) {
	if len(out) >= 120 || depth > 3 {
		return
	}
	set := func(s string) {
		if utf8.RuneCountInString(s) > 2000 {
			s = string([]rune(s)[:2000])
		}
		out[prefix] = s
	}
	switch x := val.(type) {
	case map[string]any:
		for k, v := range x {
			if strings.HasPrefix(k, "_") || !dataKeyRe.MatchString(k) {
				continue
			}
			flatten(prefix+"."+strings.ToLower(k), v, out, depth+1)
		}
	case []any:
		for i, v := range x {
			if i >= 20 {
				break
			}
			flatten(prefix+"."+strconv.Itoa(i), v, out, depth+1)
		}
	case string:
		set(x)
	case float64:
		set(strconv.FormatFloat(x, 'f', -1, 64))
	case bool:
		set(strconv.FormatBool(x))
	}
}

// startByTrigger starts a case (ctx: the principal of the trigger).
func (pl *Plugin) startByTrigger(ctx context.Context, element int64, def *Definition, t *Trigger, ref string, tv map[string]string, depth int) error {
	// starts per hour
	var n int
	if err := pl.host.DB.QueryRow(ctx, `SELECT count(*) FROM plugin_workflow_fired WHERE element_id=$1 AND trigger_id=$2 AND at > now() - interval '1 hour'`,
		element, t.ID).Scan(&n); err != nil {
		return err
	}
	if limit := pl.host.Limit(LimitPerHour); int64(n) >= limit {
		return fmt.Errorf("more than %d starts in an hour; this start was skipped", limit)
	}
	tag, err := pl.host.DB.Exec(ctx, `INSERT INTO plugin_workflow_fired(element_id, trigger_id, ref) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, element, t.ID, ref)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errSkip // started already
	}
	vars := pl.siteVars()
	for k, v := range tv {
		vars[k] = v
	}
	in := map[string]any{}
	for _, f := range def.Fields {
		tpl, ok := t.Data[f.ID]
		if !ok {
			continue
		}
		v := strings.TrimSpace(render(tpl, vars))
		if f.Type == "checkbox" {
			in[f.ID] = in2s(strings.ToLower(v), "1", "true", "yes", "ja", "on", "x", "✓")
			continue
		}
		if f.Type == "file" || v == "" {
			continue
		}
		in[f.ID] = v
	}
	title := strings.TrimSpace(render(t.Title, vars))
	if title == "" {
		title = firstNonEmpty(t.Name, tv["trigger.object.title"], t.ID)
	}
	err = pgx.BeginFunc(ctx, pl.host.DB, func(tx pgx.Tx) error {
		_, err := pl.startTx(ctx, tx, element, startInput{Title: title, Data: in, Trigger: tv, Depth: depth, Via: "trigger:" + t.ID})
		return err
	})
	if err != nil {
		_, _ = pl.host.DB.Exec(ctx, `DELETE FROM plugin_workflow_fired WHERE element_id=$1 AND trigger_id=$2 AND ref=$3`, element, t.ID, ref)
		return err
	}
	pl.noteTrigger(ctx, element, t.ID, "")
	return nil
}

func in2s(v string, xs ...string) bool { return in(v, xs...) }

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

// schedules starts the cases of schedule triggers whose minute came (the
// latest scheduled minute of the last ten, once).
func (pl *Plugin) schedules(ctx context.Context) error {
	rows, err := pl.host.DB.Query(ctx, `SELECT element_id, trigger_id, spec FROM plugin_workflow_triggers WHERE type='schedule'`)
	if err != nil {
		return err
	}
	type sched struct {
		element int64
		trigger string
		t       Trigger
	}
	var list []sched
	for rows.Next() {
		var s sched
		var raw json.RawMessage
		if err := rows.Scan(&s.element, &s.trigger, &raw); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal(raw, &s.t) == nil {
			list = append(list, s)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	now := time.Now().Truncate(time.Minute)
	for _, s := range list {
		c, err := ParseCron(s.t.Cron)
		if err != nil {
			continue
		}
		var due time.Time
		for m := now; m.After(now.Add(-10 * time.Minute)); m = m.Add(-time.Minute) {
			if c.Match(m) {
				due = m
				break
			}
		}
		if due.IsZero() {
			continue
		}
		var fired bool
		if err := pl.host.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM plugin_workflow_fired WHERE element_id=$1 AND trigger_id=$2 AND ref=$3)`,
			s.element, s.trigger, "cron:"+due.UTC().Format(time.RFC3339)).Scan(&fired); err != nil || fired {
			continue
		}
		if err := pl.fireSchedule(ctx, s.element, s.trigger, due); err != nil && !errors.Is(err, errSkip) {
			pl.host.Log.Warn("scheduled workflow not started", "workflow", s.element, "trigger", s.trigger, "err", err)
			pl.noteTrigger(ctx, s.element, s.trigger, err.Error())
			// no second try for this minute (the error is shown in the editor)
			_, _ = pl.host.DB.Exec(ctx, `INSERT INTO plugin_workflow_fired(element_id, trigger_id, ref) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
				s.element, s.trigger, "cron:"+due.UTC().Format(time.RFC3339))
		}
	}
	return nil
}

func (pl *Plugin) fireSchedule(ctx context.Context, element int64, trigger string, due time.Time) error {
	def, _, definer, err := pl.definitionBy(ctx, element, "")
	if err != nil {
		return errSkip
	}
	t := def.Trigger(trigger)
	if t == nil || t.Off || t.Type != TrigSchedule {
		return errSkip
	}
	pctx, err := pl.as(ctx, definer, element)
	if err != nil {
		return err
	}
	ev := webhook.Event{Name: "schedule", OccurredAt: due}
	return pl.startByTrigger(pctx, element, def, t, "cron:"+due.UTC().Format(time.RFC3339), triggerVars(t, ev), 0)
}

// ---------------------------------------------------------------- incoming webhooks

func init() {
	secrets.Register(secrets.Column{Table: "plugin_workflow_hook_secrets", Key: "element_id::text || ':' || trigger_id", Value: "secret"})
}

func hookAAD(element int64, trigger string) string {
	return secrets.AAD("plugin_workflow_hook_secrets", strconv.FormatInt(element, 10)+":"+trigger)
}

// HookTolerance is how far the time of a signed request may differ.
const HookTolerance = 5 * time.Minute

// hookSignature is the signature of a request: hex HMAC-SHA256 of
// "<timestamp>.<body>" with the secret of the trigger.
func hookSignature(secret []byte, ts string, body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(ts + "."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// receiveHook handles POST /hooks/{id}/{trigger}. Every failure answers
// with the same 403 (nothing to learn about workflows and triggers).
func (pl *Plugin) receiveHook(w http.ResponseWriter, r *http.Request) (any, error) {
	ctx := r.Context()
	denied := apperr.Forbidden("invalid signature")
	element, err := h.PathID(r, "id")
	if err != nil {
		return nil, denied
	}
	trigger := r.PathValue("trigger")
	var raw json.RawMessage
	var secret []byte
	err = pl.host.DB.QueryRow(ctx, `SELECT t.spec, s.secret FROM plugin_workflow_triggers t JOIN plugin_workflow_hook_secrets s
		ON s.element_id=t.element_id AND s.trigger_id=t.trigger_id WHERE t.element_id=$1 AND t.trigger_id=$2 AND t.type='webhook'`, element, trigger).Scan(&raw, &secret)
	if err != nil {
		return nil, denied
	}
	var t Trigger
	if json.Unmarshal(raw, &t) != nil {
		return nil, denied
	}
	if secret, err = pl.host.Secrets.Open(secret, hookAAD(element, trigger)); err != nil {
		pl.host.Log.Error("workflow webhook: secret cannot be read", "workflow", element, "trigger", trigger, "err", err)
		return nil, denied
	}
	ip := audit.ClientIP(ctx)
	if len(t.AllowFrom) > 0 && !netlist.Allowed(ctx, t.AllowFrom, ip) {
		pl.host.Log.Info("workflow webhook from an address that is not allowed", "workflow", element, "trigger", trigger, "ip", ip)
		return nil, denied
	}
	body, err := io.ReadAll(r.Body) // limited by the middleware (requests of guests)
	if err != nil {
		return nil, apperr.TooLarge("request body too large")
	}
	ts := r.Header.Get("X-Ewiki-Timestamp")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || time.Since(time.Unix(sec, 0)).Abs() > HookTolerance {
		return nil, denied
	}
	sig := strings.TrimPrefix(r.Header.Get("X-Ewiki-Signature"), "sha256=")
	if !hmac.Equal([]byte(sig), []byte(hookSignature(secret, ts, body))) {
		return nil, denied
	}
	var payload any = map[string]any{}
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, apperr.BadRequest("the body must be JSON")
		}
	}
	data, ok := payload.(map[string]any)
	if !ok {
		data = map[string]any{"value": payload}
	}
	// a delivery is started once: by its id or by its signature
	ref := "sig:" + sig
	if id := strings.TrimSpace(r.Header.Get("X-Ewiki-Delivery")); id != "" && len(id) <= 100 {
		ref = "hook:" + id
	}
	data["_ref"] = ref
	ev := webhook.Event{Name: "webhook", OccurredAt: time.Now().UTC(), Data: data}
	b, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	if _, err := pl.host.DB.Exec(ctx, `INSERT INTO plugin_workflow_queue(element_id, trigger_id, event) VALUES ($1,$2,$3)`, element, trigger, b); err != nil {
		return nil, err
	}
	pl.host.Workers.Trigger("workflow.process.triggers")
	h.Write(w, http.StatusAccepted, map[string]bool{"queued": true})
	return h.Handled{}, nil
}

// newHookSecret creates (or replaces) the secret of a webhook trigger; it
// is returned once.
func (pl *Plugin) newHookSecret(ctx context.Context, element int64, trigger string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(b)
	sealed, err := pl.host.Secrets.Seal([]byte(secret), hookAAD(element, trigger))
	if err != nil {
		return "", err
	}
	p := identity.From(ctx)
	err = pgx.BeginFunc(ctx, pl.host.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO plugin_workflow_hook_secrets(element_id, trigger_id, secret, created_by) VALUES ($1,$2,$3,$4)
			ON CONFLICT (element_id, trigger_id) DO UPDATE SET secret=EXCLUDED.secret, created_by=EXCLUDED.created_by, created_at=now()`,
			element, trigger, sealed, nullID(p.ID())); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "workflow.hook_secret", "element", element, nil, map[string]string{"trigger": trigger})
	})
	return secret, err
}
