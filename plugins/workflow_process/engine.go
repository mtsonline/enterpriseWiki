package workflow_process

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/mail"
	"github.com/mtsonline/enterprisewiki/internal/netguard"
)

// normalizeTypes gives steps of definitions saved before step types existed
// their type (snapshots of old cases).
func (d *Definition) normalizeTypes() {
	for i := range d.Steps {
		if d.Steps[i].Type == "" {
			d.Steps[i].Type = StepTask
			if len(d.Steps[i].Actions) == 0 {
				d.Steps[i].Type = StepEnd
			}
		}
	}
}

// actorInfo describes who caused a transition (nil principal = system).
type actorInfo struct {
	p      *identity.Principal
	action string // label of the last action
	note   string
}

func (a actorInfo) handle() string {
	if a.p == nil {
		return "system"
	}
	return a.p.Handle()
}

func (a actorInfo) id() *int64 {
	if a.p == nil || a.p.ID() == 0 {
		return nil
	}
	id := a.p.ID()
	return &id
}

// event records a transition.
func event(ctx context.Context, tx pgx.Tx, id int64, from, to *string, action, label, note string, a actorInfo) error {
	_, err := tx.Exec(ctx, `INSERT INTO plugin_workflow_events(instance_id, from_step, to_step, action, action_label, note, actor_id, actor)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, id, from, to, action, label, note, a.id(), a.handle())
	return err
}

func sp(s string) *string { return &s }

// MaxChain limits automatic steps in a row (protects against loops).
const MaxChain = 25

// enter moves case x into step id and runs automatic steps until a step
// waits for people or time, or the case ends. x.Step must be the step the
// case is leaving. Must run inside the transaction that changed the case.
func (pl *Plugin) enter(ctx context.Context, tx pgx.Tx, x *Instance, id string, a actorInfo) error {
	d := &x.Definition
	for i := 0; ; i++ {
		if i >= MaxChain {
			return apperr.BadRequest("the workflow has more than %d automatic steps in a row (loop?)", MaxChain)
		}
		s := d.Step(id)
		if s == nil {
			return apperr.BadRequest("step %q does not exist", id)
		}
		x.Step, x.StepName = s.ID, s.Name
		switch s.Type {
		case StepEnd:
			x.Status, x.Outcome = "done", s.Outcome
			if err := pl.setStep(ctx, tx, x, nil); err != nil {
				return err
			}
			return pl.notifyStarter(ctx, tx, x, a, "done")
		case StepTask, StepApproval:
			x.Status = "open"
			if err := pl.setStep(ctx, tx, x, nil); err != nil {
				return err
			}
			return pl.notifyAssignees(ctx, tx, x, s, false)
		case StepDelay:
			x.Status = "open"
			wake := pl.wakeTime(x, s)
			return pl.setStep(ctx, tx, x, &wake)
		case StepWebhook:
			x.Status = "open"
			now := time.Now()
			return pl.setStep(ctx, tx, x, &now) // the worker calls the webhook
		case StepCondition:
			ok := evaluate(x, s)
			next, label := s.Next, "true"
			if !ok {
				next, label = s.NextElse, "false"
			}
			if err := event(ctx, tx, x.ID, sp(s.ID), sp(next), "condition", label, "", actorInfo{}); err != nil {
				return err
			}
			id = next
		case StepEmail:
			note := ""
			if err := pl.sendStepEmail(ctx, tx, x, s, a); err != nil {
				note = "error: " + err.Error()
				pl.host.Log.Warn("workflow e-mail not sent", "case", x.ID, "step", s.ID, "err", err)
			}
			if err := event(ctx, tx, x.ID, sp(s.ID), sp(s.Next), "email", render(s.Email.Subject, pl.vars(ctx, tx, x, a)), note, actorInfo{}); err != nil {
				return err
			}
			id = s.Next
		default:
			return apperr.BadRequest("unknown step type %q", s.Type)
		}
	}
}

// setStep stores the current step of x.
func (pl *Plugin) setStep(ctx context.Context, tx pgx.Tx, x *Instance, wake *time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET step=$2, status=$3, updated_at=now(), step_entered_at=now(),
		wake_at=$4, reminded_at=NULL, attempts=0, last_error='', closed_at=CASE WHEN $3='open' THEN NULL ELSE now() END WHERE id=$1`,
		x.ID, x.Step, x.Status, wake)
	return err
}

func (pl *Plugin) wakeTime(x *Instance, s *Step) time.Time {
	base := time.Now()
	if s.Delay.Until != "" {
		if v, ok := x.Data[s.Delay.Until].(string); ok {
			if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
				base = t
			}
		}
	}
	return base.Add(time.Duration(s.Delay.Days)*24*time.Hour + time.Duration(s.Delay.Hours)*time.Hour)
}

// ---------------------------------------------------------------- conditions

func dataString(x *Instance, field string) string {
	if field == "case.title" {
		return x.Title
	}
	switch v := x.Data[field].(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case map[string]any:
		n, _ := v["name"].(string)
		return n
	}
	return fmt.Sprint(x.Data[field])
}

func compare(a, b string) (int, bool) {
	fa, ea := strconv.ParseFloat(strings.ReplaceAll(a, ",", "."), 64)
	fb, eb := strconv.ParseFloat(strings.ReplaceAll(b, ",", "."), 64)
	if ea == nil && eb == nil {
		switch {
		case fa < fb:
			return -1, true
		case fa > fb:
			return 1, true
		}
		return 0, true
	}
	if a == "" || b == "" {
		return 0, false
	}
	return strings.Compare(a, b), true // dates (YYYY-MM-DD) compare as text
}

// evaluate checks the rules of a condition step.
func evaluate(x *Instance, s *Step) bool {
	any := s.Match == "any"
	for _, r := range s.Rules {
		v := dataString(x, r.Field)
		lv, lr := strings.ToLower(strings.TrimSpace(v)), strings.ToLower(strings.TrimSpace(r.Value))
		var ok bool
		switch r.Op {
		case "equals":
			ok = lv == lr
		case "not_equals":
			ok = lv != lr
		case "contains":
			ok = strings.Contains(lv, lr)
		case "not_contains":
			ok = !strings.Contains(lv, lr)
		case "empty":
			ok = lv == "" || lv == "false"
		case "not_empty":
			ok = lv != "" && lv != "false"
		default:
			c, valid := compare(strings.TrimSpace(v), strings.TrimSpace(r.Value))
			ok = valid && ((r.Op == "gt" && c > 0) || (r.Op == "gte" && c >= 0) || (r.Op == "lt" && c < 0) || (r.Op == "lte" && c <= 0))
		}
		if any && ok {
			return true
		}
		if !any && !ok {
			return false
		}
	}
	return !any
}

// ---------------------------------------------------------------- placeholders

// Placeholders lists the placeholders for the editor.
var Placeholders = []string{
	"case.id", "case.title", "case.url", "case.status", "case.step", "case.started_at",
	"workflow.title", "workflow.url", "tasks.url",
	"starter.name", "starter.email", "starter.username",
	"actor.name", "actor.email", "actor.username",
	"last.action", "last.note",
	"site.name", "site.url", "date", "time",
	"field.<id>",
}

var placeholderRe = regexp.MustCompile(`\{\{\s*([a-z0-9_.-]+)\s*\}\}`)

// render replaces {{name}} placeholders (unknown ones become empty).
func render(tpl string, vars map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(tpl, func(m string) string {
		return vars[placeholderRe.FindStringSubmatch(m)[1]]
	})
}

type userRow struct {
	name, email, username string
}

func loadUser(ctx context.Context, q pgx.Tx, id *int64) userRow {
	var u userRow
	if id == nil {
		return u
	}
	_ = q.QueryRow(ctx, `SELECT CASE WHEN display_name='' THEN username ELSE display_name END, email, username || '@' || provider FROM users WHERE id=$1`, *id).
		Scan(&u.name, &u.email, &u.username)
	return u
}

// vars builds the placeholder values of a case.
func (pl *Plugin) vars(ctx context.Context, tx pgx.Tx, x *Instance, a actorInfo) map[string]string {
	cfg := pl.host.Config.Get()
	base := strings.TrimSuffix(cfg.Server.BaseURL, "/")
	now := time.Now()
	v := map[string]string{
		"case.id": strconv.FormatInt(x.ID, 10), "case.title": x.Title, "case.status": x.Status, "case.step": x.StepName,
		"case.url":        fmt.Sprintf("%s/e/%d?case=%d", base, x.ElementID, x.ID),
		"case.started_at": x.StartedAt.Local().Format("2006-01-02 15:04"),
		"workflow.title":  pickTitle(x.Element, x.Lang), "workflow.url": fmt.Sprintf("%s/e/%d", base, x.ElementID),
		"tasks.url": base + "/tasks", "site.name": cfg.UI.SiteName, "site.url": base,
		"date": now.Format("2006-01-02"), "time": now.Format("15:04"),
		"last.action": a.action, "last.note": a.note,
	}
	st := loadUser(ctx, tx, x.StartedByID)
	v["starter.name"], v["starter.email"], v["starter.username"] = st.name, st.email, st.username
	if a.p != nil {
		au := loadUser(ctx, tx, a.id())
		if au.name == "" {
			au.name = a.p.DisplayName
		}
		v["actor.name"], v["actor.email"], v["actor.username"] = au.name, au.email, a.p.Handle()
	}
	for _, f := range x.Definition.Fields {
		val := dataString(x, f.ID)
		if f.Type == "checkbox" {
			if b, _ := x.Data[f.ID].(bool); b {
				val = "✓"
			} else {
				val = "–"
			}
		}
		v["field."+f.ID] = val
	}
	return v
}

func pickTitle(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	for _, v := range m {
		return v
	}
	return ""
}

// ---------------------------------------------------------------- recipients

// members returns the user ids behind an assignee.
func (pl *Plugin) members(ctx context.Context, q pgx.Tx, x *Instance, as Assignee) ([]int64, error) {
	var rows pgx.Rows
	var err error
	v := strings.TrimSpace(as.Value)
	switch as.Type {
	case "starter":
		if x.StartedByID == nil {
			return nil, nil
		}
		return []int64{*x.StartedByID}, nil
	case "user":
		if strings.Contains(v, "@") {
			rows, err = q.Query(ctx, `SELECT id FROM users WHERE lower(username || '@' || provider) = lower($1) AND NOT disabled`, v)
		} else {
			rows, err = q.Query(ctx, `SELECT id FROM users WHERE lower(username) = lower($1) AND provider='local' AND NOT disabled`, v)
		}
	case "group":
		rows, err = q.Query(ctx, `SELECT ug.user_id FROM user_groups ug JOIN groups g ON g.id=ug.group_id JOIN users u ON u.id=ug.user_id
			WHERE lower(g.name)=lower($1) AND NOT u.disabled`, v)
	case "role":
		rows, err = q.Query(ctx, `SELECT DISTINCT x.uid FROM (
				SELECT ur.user_id AS uid FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE lower(r.name)=lower($1)
				UNION SELECT ug.user_id FROM user_groups ug JOIN group_roles gr ON gr.group_id=ug.group_id JOIN roles r ON r.id=gr.role_id WHERE lower(r.name)=lower($1)
			) x JOIN users u ON u.id=x.uid WHERE NOT u.disabled`, v)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// canView keeps the users that may see the workflow page.
func (pl *Plugin) canView(ctx context.Context, x *Instance, ids []int64) []int64 {
	out := []int64{}
	for _, id := range ids {
		p, err := pl.host.Auth.LoadPrincipal(ctx, id)
		if err == nil && pl.host.ACL.Can(ctx, p, x.CategoryID, acl.View) {
			out = append(out, id)
		}
	}
	return out
}

// recipients resolves an address list of an e-mail step: addresses (after
// placeholders), starter, actor, user:NAME, group:NAME, role:NAME.
func (pl *Plugin) recipients(ctx context.Context, tx pgx.Tx, x *Instance, spec string, vars map[string]string, a actorInfo) ([]string, error) {
	out := []string{}
	for _, tok := range mail.SplitAddresses(render(spec, vars)) {
		low := strings.ToLower(tok)
		var as *Assignee
		switch {
		case low == "starter":
			as = &Assignee{Type: "starter"}
		case low == "actor":
			if a.p != nil && a.p.ID() != 0 {
				as = &Assignee{Type: "user", Value: a.p.Handle()}
			}
		case strings.HasPrefix(low, "user:"), strings.HasPrefix(low, "group:"), strings.HasPrefix(low, "role:"):
			k, v, _ := strings.Cut(tok, ":")
			as = &Assignee{Type: strings.ToLower(k), Value: v}
		default:
			addr, err := mail.ParseAddress(tok)
			if err != nil {
				return nil, err
			}
			out = append(out, addr)
			continue
		}
		if as == nil {
			continue
		}
		ids, err := pl.members(ctx, tx, x, *as)
		if err != nil {
			return nil, err
		}
		us, err := pl.host.Mail.Users(ctx, tx, ids)
		if err != nil {
			return nil, err
		}
		for _, u := range us {
			if u.Email != "" {
				out = append(out, u.Email)
			}
		}
	}
	return out, nil
}

// sendStepEmail queues the e-mail of an email step.
func (pl *Plugin) sendStepEmail(ctx context.Context, tx pgx.Tx, x *Instance, s *Step, a actorInfo) error {
	vars := pl.vars(ctx, tx, x, a)
	to, err := pl.recipients(ctx, tx, x, s.Email.To, vars, a)
	if err != nil {
		return err
	}
	if len(to) == 0 {
		return fmt.Errorf("no recipient with an e-mail address")
	}
	cc, err := pl.recipients(ctx, tx, x, s.Email.Cc, vars, a)
	if err != nil {
		return err
	}
	_, err = pl.host.Mail.Enqueue(ctx, tx, mail.Message{
		To: to, Cc: cc, From: strings.TrimSpace(render(s.Email.From, vars)), ReplyTo: strings.TrimSpace(render(s.Email.ReplyTo, vars)),
		Subject: render(s.Email.Subject, vars), Text: render(s.Email.Body, vars),
		Source: "workflow.process", Ref: fmt.Sprintf("case:%d", x.ID),
	})
	return err
}

// ---------------------------------------------------------------- notifications

// notifyAssignees tells the people responsible for step s about a new task
// (or reminds them).
func (pl *Plugin) notifyAssignees(ctx context.Context, tx pgx.Tx, x *Instance, s *Step, reminder bool) error {
	ids, err := pl.members(ctx, tx, x, s.Assignee)
	if err != nil {
		return err
	}
	if s.Type == StepApproval && reminder { // only those who have not voted yet
		voted, err := pl.voters(ctx, tx, x)
		if err != nil {
			return err
		}
		keep := []int64{}
		for _, id := range ids {
			if !voted[id] {
				keep = append(keep, id)
			}
		}
		ids = keep
	}
	us, err := pl.host.Mail.Users(ctx, tx, pl.canView(ctx, x, ids))
	if err != nil {
		return err
	}
	vars := pl.vars(ctx, tx, x, actorInfo{})
	key := "workflow.mail.task"
	if reminder {
		key = "workflow.mail.reminder"
	}
	for _, u := range us {
		if !u.Wants("tasks") {
			continue
		}
		params := map[string]string{"name": u.Name, "case": x.Title, "step": s.Name, "workflow": vars["workflow.title"],
			"url": vars["case.url"], "tasks": vars["tasks.url"], "site": vars["site.name"]}
		if _, err := pl.host.Mail.Enqueue(ctx, tx, mail.Message{To: []string{u.Email},
			Subject: pl.host.T(ctx, u.Lang, key+"_subject", params), Text: pl.host.T(ctx, u.Lang, key+"_body", params),
			Source: "workflow.process", Ref: fmt.Sprintf("case:%d", x.ID)}); err != nil {
			pl.host.Log.Warn("notification not queued", "user", u.ID, "err", err)
		}
	}
	return nil
}

// notifyStarter tells the person who started a case that it ended.
func (pl *Plugin) notifyStarter(ctx context.Context, tx pgx.Tx, x *Instance, a actorInfo, what string) error {
	if x.StartedByID == nil || (a.p != nil && a.p.ID() == *x.StartedByID) {
		return nil
	}
	us, err := pl.host.Mail.Users(ctx, tx, []int64{*x.StartedByID})
	if err != nil || len(us) == 0 || !us[0].Wants("cases") {
		return err
	}
	u := us[0]
	vars := pl.vars(ctx, tx, x, a)
	params := map[string]string{"name": u.Name, "case": x.Title, "step": x.StepName, "workflow": vars["workflow.title"],
		"url": vars["case.url"], "site": vars["site.name"], "note": a.note}
	key := "workflow.mail." + what
	_, err = pl.host.Mail.Enqueue(ctx, tx, mail.Message{To: []string{u.Email},
		Subject: pl.host.T(ctx, u.Lang, key+"_subject", params), Text: pl.host.T(ctx, u.Lang, key+"_body", params),
		Source: "workflow.process", Ref: fmt.Sprintf("case:%d", x.ID)})
	if err != nil {
		pl.host.Log.Warn("notification not queued", "user", u.ID, "err", err)
	}
	return nil
}

// ---------------------------------------------------------------- approvals

// voters returns the users who approved in the current visit of the step.
func (pl *Plugin) voters(ctx context.Context, q pgx.Tx, x *Instance) (map[int64]bool, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT e.actor_id FROM plugin_workflow_events e JOIN plugin_workflow_instances i ON i.id=e.instance_id
		WHERE e.instance_id=$1 AND e.from_step=$2 AND e.action='approve' AND e.at >= i.step_entered_at AND e.actor_id IS NOT NULL`, x.ID, x.Step)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// required returns the number of approvals a step needs for case x.
func (pl *Plugin) required(ctx context.Context, q pgx.Tx, x *Instance, s *Step) (int, error) {
	if s.Required > 0 {
		return s.Required, nil
	}
	ids, err := pl.members(ctx, q, x, s.Assignee)
	if err != nil {
		return 0, err
	}
	return max(1, len(ids)), nil
}

// ---------------------------------------------------------------- webhooks

// callWebhook sends the case to an external system.
func (pl *Plugin) callWebhook(ctx context.Context, x *Instance, s *Step, vars map[string]string) error {
	sec := pl.host.Config.Get().Plugins.Macros
	u, err := url.Parse(s.Webhook.URL)
	if err != nil {
		return err
	}
	if !netguard.Allowed(u, sec.HTTPAllow) {
		return fmt.Errorf("%s is not on the allow list (plugins.macros.http_allow in config.yaml)", u.Redacted())
	}
	body, _ := json.Marshal(map[string]any{
		"event": "workflow.step", "step": s.ID,
		"case": map[string]any{"id": x.ID, "title": x.Title, "url": vars["case.url"], "started_at": x.StartedAt, "lang": x.Lang,
			"started_by": map[string]string{"name": vars["starter.name"], "email": vars["starter.email"], "username": vars["starter.username"]}},
		"workflow": map[string]any{"element_id": x.ElementID, "title": vars["workflow.title"], "url": vars["workflow.url"]},
		"data":     x.Data,
	})
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, s.Webhook.Method, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "enterprise-wiki-workflow")
	if s.Webhook.Secret != "" {
		m := hmac.New(sha256.New, []byte(s.Webhook.Secret))
		m.Write(body)
		req.Header.Set("X-Ewiki-Signature", "sha256="+hex.EncodeToString(m.Sum(nil)))
	}
	resp, err := netguard.Client(sec.HTTPAllow, sec.HTTPAllowPrivate, 20*time.Second).Do(req)
	if err != nil {
		var ue *url.Error
		if ok := asURLError(err, &ue); ok {
			err = ue.Err
		}
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("the server answered %d", resp.StatusCode)
	}
	return nil
}

func asURLError(err error, target **url.Error) bool {
	ue, ok := err.(*url.Error)
	if ok {
		*target = ue
	}
	return ok
}
