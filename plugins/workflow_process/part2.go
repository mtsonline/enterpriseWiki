package workflow_process

// Workflow 2, part 2: parallel branches (also one per value of a field),
// hand-overs of tasks (claim from a group, give back, delegate, ask the
// starter), deputies while someone is away and forms of steps with field
// rights.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

// startFields are the fields of the start form (without the later ones).
func startFields(fs []Field) []Field {
	out := []Field{}
	for _, f := range fs {
		if !f.Later {
			out = append(out, f)
		}
	}
	return out
}

// ---------------------------------------------------------------- parallel branches

// Branch is a branch of a parallel step as shown in its case.
type Branch struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Step     string `json:"step"`
	StepName string `json:"step_name"`
	Status   string `json:"status"`
	Outcome  string `json:"outcome,omitempty"`
}

// MaxBranches limits the branches of one parallel step.
const MaxBranches = 50

// items returns the values of a field for one branch each.
func items(v any) []string {
	var out []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" && len(out) < MaxBranches {
			out = append(out, s)
		}
	}
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			add(fmt.Sprint(e))
		}
	case string:
		for _, p := range strings.FieldsFunc(x, func(r rune) bool { return r == '\n' || r == ',' || r == ';' }) {
			add(p)
		}
	}
	return out
}

type branchRow struct {
	id      int64
	status  string
	step    string
	data    map[string]any
	outcome string
}

// branchesOf returns the branches of the current visit of step s of x.
func branchesOf(ctx context.Context, q pgx.Tx, x *Instance, stepID string) ([]branchRow, error) {
	rows, err := q.Query(ctx, `SELECT c.id, c.status, c.step, c.data FROM plugin_workflow_instances c, plugin_workflow_instances p
		WHERE p.id=$1 AND c.parent_id=p.id AND c.parent_step=$2 AND c.branch AND c.started_at >= p.step_entered_at ORDER BY c.id`, x.ID, stepID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []branchRow
	for rows.Next() {
		var b branchRow
		if err := rows.Scan(&b.id, &b.status, &b.step, &b.data); err != nil {
			return nil, err
		}
		if st := x.Definition.Step(b.step); st != nil && st.Type == StepEnd {
			b.outcome = st.Outcome
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// runParallel starts the branches of a parallel step or, woken by a branch
// that ended, decides whether the case continues.
func (pl *Plugin) runParallel(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (result, error) {
	p := s.Parallel
	list, err := branchesOf(ctx, tx, x, s.ID)
	if err != nil {
		return result{}, err
	}
	if len(list) == 0 {
		if err := pl.spawnBranches(ctx, tx, x, s); err != nil {
			return result{}, err
		}
		if list, err = branchesOf(ctx, tx, x, s.ID); err != nil {
			return result{}, err
		}
		if len(list) == 0 { // no values: nothing to wait for
			return result{next: s.Next, action: "parallel_done", label: "0/0"}, nil
		}
	}
	done, open := 0, 0
	for _, b := range list {
		switch {
		case b.status == "open":
			open++
		case b.status == "done" && b.outcome != "failure":
			done++
		}
	}
	need := len(list)
	if p.Require > 0 && p.Require < need {
		need = p.Require
	}
	label := fmt.Sprintf("%d/%d", done, len(list))
	switch {
	case done >= need: // enough: the others are not needed any more
		for _, b := range list {
			if b.status == "open" {
				if err := cancelBranch(ctx, tx, b.id); err != nil {
					return result{}, err
				}
			}
		}
		// values the branches set or changed (later branches win)
		for _, b := range list {
			if b.status != "done" {
				continue
			}
			for k, v := range b.data {
				if k == "item" {
					continue
				}
				x.Data[k] = v
			}
		}
		if err := saveData(ctx, tx, x); err != nil {
			return result{}, err
		}
		return result{next: s.Next, action: "parallel_done", label: label}, nil
	case done+open < need: // cannot be reached any more
		for _, b := range list {
			if b.status == "open" {
				if err := cancelBranch(ctx, tx, b.id); err != nil {
					return result{}, err
				}
			}
		}
		next := s.Next
		if s.NextElse != "" {
			next = s.NextElse
		}
		return result{next: next, action: "parallel_failed", label: label}, nil
	}
	return result{wait: true}, nil
}

func cancelBranch(ctx context.Context, tx pgx.Tx, id int64) error {
	_, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET status='cancelled', closed_at=now(), updated_at=now(), wake_at=NULL, check_at=NULL
		WHERE id=$1 AND status='open'`, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO plugin_workflow_events(instance_id, from_step, to_step, action, actor) SELECT id, step, NULL, 'branch_cancelled', 'system'
		FROM plugin_workflow_instances WHERE id=$1`, id)
	return err
}

// spawnBranches starts the branches (as cases of the same workflow that
// belong to x).
func (pl *Plugin) spawnBranches(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) error {
	type start struct{ step, item, title string }
	var starts []start
	name := func(id string) string {
		if st := x.Definition.Step(id); st != nil {
			return st.Name
		}
		return id
	}
	if f := s.Parallel.ForEach; f != "" {
		for _, it := range items(x.Data[f]) {
			starts = append(starts, start{s.Parallel.Branches[0], it, x.Title + " – " + it})
		}
	} else {
		for _, b := range s.Parallel.Branches {
			starts = append(starts, start{b, "", x.Title + " – " + name(b)})
		}
	}
	for _, st := range starts {
		data := map[string]any{}
		for k, v := range x.Data {
			data[k] = v
		}
		if st.item != "" {
			data["item"] = st.item
		}
		title := st.title
		if utf8.RuneCountInString(title) > 200 {
			title = string([]rune(title)[:200])
		}
		var cid int64
		if err := tx.QueryRow(ctx, `INSERT INTO plugin_workflow_instances(element_id, lang, title, data, definition, step, status, started_by,
			definer_id, trigger, depth, parent_id, parent_step, branch) VALUES ($1,$2,$3,$4,$5,$6,'open',$7,$8,$9,$10,$11,$12,true) RETURNING id`,
			x.ElementID, x.Lang, title, data, x.Definition, st.step, x.StartedByID, x.DefinerID, x.Trigger, x.Depth, x.ID, s.ID).Scan(&cid); err != nil {
			return err
		}
		if err := event(ctx, tx, cid, nil, sp(st.step), "branch_start", st.item, "", actorInfo{}); err != nil {
			return err
		}
		c, err := pl.loadTx(ctx, tx, cid)
		if err != nil {
			return err
		}
		c.Step = ""
		if err := pl.enter(ctx, tx, c, st.step, actorInfo{}); err != nil {
			return err
		}
	}
	return event(ctx, tx, x.ID, sp(s.ID), sp(s.ID), "parallel_started", strconv.Itoa(len(starts)), "", actorInfo{})
}

// branchEnds reports whether entering step id ends branch x (it reached
// the meeting point of its parallel step).
func branchEnds(x *Instance, id string) bool {
	if !x.Branch || x.ParentStep == "" {
		return false
	}
	ps := x.Definition.Step(x.ParentStep)
	return ps != nil && ps.Type == StepParallel && ps.Next == id
}

// finishBranch closes branch x at step id; the parallel step continues.
func (pl *Plugin) finishBranch(ctx context.Context, tx pgx.Tx, x *Instance, id string) error {
	if err := event(ctx, tx, x.ID, sp(x.Step), sp(id), "branch_done", "", "", actorInfo{}); err != nil {
		return err
	}
	x.Step, x.Status = id, "done"
	if s := x.Definition.Step(id); s != nil {
		x.StepName = s.Name
	}
	return pl.setStep(ctx, tx, x, nil) // wakes the parent
}

// loadBranches fills the branches of the current parallel step of x.
func (pl *Plugin) loadBranches(ctx context.Context, x *Instance) {
	s := x.Definition.Step(x.Step)
	if s == nil || s.Type != StepParallel {
		return
	}
	rows, err := pl.host.DB.Query(ctx, `SELECT c.id, c.title, c.step, c.status FROM plugin_workflow_instances c
		WHERE c.parent_id=$1 AND c.parent_step=$2 AND c.branch AND c.started_at >= $3 ORDER BY c.id`, x.ID, s.ID, x.StepEntered)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var b Branch
		if rows.Scan(&b.ID, &b.Title, &b.Step, &b.Status) == nil {
			if st := x.Definition.Step(b.Step); st != nil {
				b.StepName = st.Name
				if st.Type == StepEnd {
					b.Outcome = st.Outcome
				}
			}
			x.Branches = append(x.Branches, b)
		}
	}
}

// ---------------------------------------------------------------- hand-overs

// Hand-over actions of tasks.
const (
	ActClaim    = "_claim"    // take over the task of a group or role
	ActRelease  = "_release"  // give it back to the group or role
	ActDelegate = "_delegate" // hand it over to another person
	ActAsk      = "_ask"      // ask the starter (with a note)
	ActAnswer   = "_answer"   // the starter answers (with a note)
)

// handoverActions are the hand-over actions of task s of x for the caller
// (who is assigned).
func handoverActions(w *who, x *Instance, s *Step) []Action {
	if s.Type != StepTask || w.p == nil {
		return nil
	}
	if x.Handover == "ask" {
		return []Action{{ID: ActAnswer, Next: s.ID, Note: "required"}}
	}
	out := []Action{{ID: ActDelegate, Next: s.ID, Note: "optional"}}
	as := x.assignee(s)
	if (as.Type == "group" || as.Type == "role") && x.Handover != "claim" {
		out = append(out, Action{ID: ActClaim, Next: s.ID})
	}
	if x.Handover == "claim" && as.Type == "user" {
		out = append(out, Action{ID: ActRelease, Next: s.ID})
	}
	if x.StartedByID != nil && *x.StartedByID != w.p.ID() {
		out = append(out, Action{ID: ActAsk, Next: s.ID, Note: "required"})
	}
	return out
}

func isHandover(action string) bool {
	return action == ActClaim || action == ActRelease || action == ActDelegate || action == ActAsk || action == ActAnswer
}

// handover carries out a hand-over action in tx.
func (pl *Plugin) handover(ctx context.Context, tx pgx.Tx, x *Instance, s *Step, action, to string, a actorInfo) error {
	me := a.p
	cur := x.Override
	var next *Assignee
	prev, kind, label := x.Prev, x.Handover, ""
	switch action {
	case ActClaim:
		next, prev, kind = &Assignee{Type: "user", Value: me.Handle()}, cur, "claim"
	case ActRelease:
		next, prev, kind = x.Prev, nil, ""
	case ActDelegate:
		to = strings.TrimSpace(to)
		if to == "" {
			return apperr.BadRequest("choose the person who takes over")
		}
		ids, err := pl.members(ctx, tx, x, Assignee{Type: "user", Value: to})
		if err != nil {
			return err
		}
		if len(ids) != 1 || len(pl.canView(ctx, tx, x, ids)) != 1 {
			return apperr.BadRequest("%s cannot take over (unknown, inactive or without access to the workflow)", to)
		}
		if ids[0] == me.ID() {
			return apperr.BadRequest("the task is already yours")
		}
		if kind != "delegate" && kind != "claim" { // keep the original to give it back
			prev = cur
		}
		next, kind, label = &Assignee{Type: "user", Value: to}, "delegate", to
	case ActAsk:
		next, prev, kind = &Assignee{Type: "starter"}, cur, "ask"
	case ActAnswer:
		next, prev, kind = x.Prev, nil, ""
	default:
		return apperr.BadRequest("unknown action %q", action)
	}
	x.Override, x.Prev, x.Handover = next, prev, kind
	if _, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET override=$2, prev=$3, handover=$4, updated_at=now() WHERE id=$1`,
		x.ID, next, prev, kind); err != nil {
		return err
	}
	if err := event(ctx, tx, x.ID, sp(s.ID), sp(s.ID), action, label, a.note, a); err != nil {
		return err
	}
	if action == ActClaim || action == ActRelease && next == nil {
		return nil
	}
	return pl.notifyAssignees(ctx, tx, x, s, false) // the new assignees
}

// ---------------------------------------------------------------- deputies

// Deputy is the deputy of a person while they are away.
type Deputy struct {
	DeputyID int64  `json:"deputy_id"`
	Deputy   string `json:"deputy"`
	From     string `json:"from"`
	Until    string `json:"until"`
	Active   bool   `json:"active"`
}

// deputyFor loads the people the caller is the deputy of today (their
// handles, groups and roles count as the caller's for tasks).
func (pl *Plugin) deputyFor(ctx context.Context, w *who) error {
	rows, err := pl.host.DB.Query(ctx, `SELECT user_id FROM plugin_workflow_deputies WHERE deputy_id=$1 AND current_date BETWEEN from_date AND until_date`, w.p.ID())
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		p, err := pl.host.Auth.LoadPrincipal(ctx, id)
		if err != nil {
			continue // disabled: nothing to take over
		}
		w.users[strings.ToLower(p.Handle())] = true
		if p.Provider == "local" {
			w.users[strings.ToLower(p.Username)] = true
		}
		for _, g := range p.Groups {
			w.groups["#"+strconv.FormatInt(g, 10)] = true
		}
		for _, r := range p.Roles {
			w.roles["#"+strconv.FormatInt(r, 10)] = true
		}
		if err := pl.names(ctx, `SELECT name FROM groups WHERE id = ANY($1)`, p.Groups, w.groups); err != nil {
			return err
		}
		if err := pl.names(ctx, `SELECT name FROM roles WHERE id = ANY($1)`, p.Roles, w.roles); err != nil {
			return err
		}
	}
	return nil
}

func (pl *Plugin) names(ctx context.Context, q string, ids []int64, into map[string]bool) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := pl.host.DB.Query(ctx, q, ids)
	if err != nil {
		return err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, n := range names {
		into[strings.ToLower(n)] = true
	}
	return nil
}

// withDeputies adds the deputies (active today) of the people ids.
func withDeputies(ctx context.Context, q pgx.Tx, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return ids, nil
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT deputy_id FROM plugin_workflow_deputies WHERE user_id = ANY($1) AND current_date BETWEEN from_date AND until_date`, ids)
	if err != nil {
		return nil, err
	}
	deps, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	out := []int64{}
	for _, id := range append(ids, deps...) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

func (pl *Plugin) deputyRoutes(host *sdk.Host) {
	get := func(ctx context.Context, uid int64) (*Deputy, error) {
		var d Deputy
		var from, until time.Time
		err := host.DB.QueryRow(ctx, `SELECT d.deputy_id, COALESCE(NULLIF(u.display_name, ''), u.username) || ' (' || u.username || '@' || u.provider || ')',
			d.from_date, d.until_date, current_date BETWEEN d.from_date AND d.until_date FROM plugin_workflow_deputies d JOIN users u ON u.id=d.deputy_id WHERE d.user_id=$1`, uid).
			Scan(&d.DeputyID, &d.Deputy, &from, &until, &d.Active)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		d.From, d.Until = from.Format("2006-01-02"), until.Format("2006-01-02")
		return &d, nil
	}
	user := func(r *http.Request) (*identity.Principal, error) {
		p := identity.From(r.Context())
		if p == nil || p.ID() == 0 {
			return nil, apperr.ErrUnauthorized
		}
		return p, nil
	}
	// GET /me/deputy: my deputy and the people I stand in for
	host.Handle("GET", "/me/deputy", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := user(r)
		if err != nil {
			return nil, err
		}
		d, err := get(r.Context(), p.ID())
		if err != nil {
			return nil, err
		}
		rows, err := host.DB.Query(r.Context(), `SELECT COALESCE(NULLIF(u.display_name, ''), u.username), d.from_date::text, d.until_date::text
			FROM plugin_workflow_deputies d JOIN users u ON u.id=d.user_id WHERE d.deputy_id=$1 AND d.until_date >= current_date ORDER BY d.from_date`, p.ID())
		if err != nil {
			return nil, err
		}
		type standIn struct {
			Name  string `json:"name"`
			From  string `json:"from"`
			Until string `json:"until"`
		}
		list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[standIn])
		if err != nil {
			return nil, err
		}
		return map[string]any{"deputy": d, "stand_in_for": list}, nil
	})
	// PUT /me/deputy {"deputy": "name", "from": "2026-12-20", "until": "2027-01-06"}
	host.Handle("PUT", "/me/deputy", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := user(r)
		if err != nil {
			return nil, err
		}
		var in struct {
			Deputy string `json:"deputy"`
			From   string `json:"from"`
			Until  string `json:"until"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		from, e1 := time.Parse("2006-01-02", in.From)
		until, e2 := time.Parse("2006-01-02", in.Until)
		if e1 != nil || e2 != nil || until.Before(from) || until.Sub(from) > 366*24*time.Hour {
			return nil, apperr.BadRequest("a period of at most a year (YYYY-MM-DD)")
		}
		ctx := r.Context()
		var id int64
		v := strings.TrimSpace(in.Deputy)
		q := `SELECT id FROM users WHERE lower(username)=lower($1) AND provider='local' AND NOT disabled`
		if strings.Contains(v, "@") {
			q = `SELECT id FROM users WHERE lower(username || '@' || provider)=lower($1) AND NOT disabled`
		}
		if err := host.DB.QueryRow(ctx, q, v).Scan(&id); err != nil {
			return nil, apperr.BadRequest("unknown person %q", v)
		}
		if id == p.ID() {
			return nil, apperr.BadRequest("you cannot be your own deputy")
		}
		err = pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO plugin_workflow_deputies(user_id, deputy_id, from_date, until_date) VALUES ($1,$2,$3,$4)
				ON CONFLICT (user_id) DO UPDATE SET deputy_id=EXCLUDED.deputy_id, from_date=EXCLUDED.from_date, until_date=EXCLUDED.until_date`,
				p.ID(), id, from, until); err != nil {
				return err
			}
			return audit.Log(ctx, tx, "workflow.deputy", "user", p.ID(), nil, map[string]any{"deputy": id, "from": in.From, "until": in.Until})
		})
		if err != nil {
			return nil, err
		}
		return get(ctx, p.ID())
	})
	host.Handle("DELETE", "/me/deputy", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := user(r)
		if err != nil {
			return nil, err
		}
		if _, err := host.DB.Exec(r.Context(), `DELETE FROM plugin_workflow_deputies WHERE user_id=$1`, p.ID()); err != nil {
			return nil, err
		}
		return h.NoContent{}, audit.Log(r.Context(), host.DB, "workflow.deputy", "user", p.ID(), nil, map[string]any{"deputy": nil})
	})
}

// ---------------------------------------------------------------- forms of steps

// stepData checks the values the assignees of step s sent with a decision.
func stepData(def *Definition, s *Step, in map[string]any) (map[string]any, error) {
	var fields []Field
	for _, sf := range s.Form {
		if sf.Mode != "edit" && sf.Mode != "required" {
			continue
		}
		for _, f := range def.Fields {
			if f.ID == sf.Field {
				f.Required = sf.Mode == "required"
				fields = append(fields, f)
			}
		}
	}
	if len(fields) == 0 {
		return nil, nil
	}
	if in == nil {
		in = map[string]any{}
	}
	return validateData(fields, in)
}

// hideFields removes the fields hidden from the assignees of the current
// step (the starter, managers and administrators see everything).
func hideFields(w *who, x *Instance, manage bool) {
	s := x.Definition.Step(x.Step)
	if s == nil || len(s.Form) == 0 || manage || w.p == nil || w.p.Admin() || (x.StartedByID != nil && *x.StartedByID == w.p.ID()) {
		return
	}
	if !w.assigned(x, s) {
		return
	}
	for _, f := range s.Form {
		if f.Mode == "hidden" {
			delete(x.Data, f.Field)
		}
	}
}
