package workflow_process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/mail"
	"github.com/mtsonline/enterprisewiki/internal/netguard"
	"github.com/mtsonline/enterprisewiki/internal/notify"
	props "github.com/mtsonline/enterprisewiki/plugins/core_properties"
)

// Kinds of notifications in the wiki (users switch their e-mails in the profile).
const (
	KindNotice = "workflow.notice" // with e-mail (as chosen in the profile)
	KindInbox  = "workflow.inbox"  // in the wiki only (e-mail off unless chosen)
)

func init() {
	notify.RegisterKind(KindNotice, "workflow.kind.notice")
	notify.RegisterKindOff(KindInbox, "workflow.kind.inbox")
}

// MaxRecipients limits the recipients of one message.
const MaxRecipients = 500

// siteVars are the placeholders that do not depend on a case.
func (pl *Plugin) siteVars() map[string]string {
	cfg := pl.host.Config.Get()
	base := strings.TrimSuffix(cfg.Server.BaseURL, "/")
	now := time.Now()
	return map[string]string{"site.name": cfg.UI.SiteName, "site.url": base, "date": now.Format("2006-01-02"), "time": now.Format("15:04")}
}

// people resolves recipients: starter, actor, assignees (of the current
// step), user:NAME, group:NAME, role:NAME and e-mail addresses.
func (pl *Plugin) people(ctx context.Context, tx pgx.Tx, x *Instance, spec string, vars map[string]string, a actorInfo) (users []int64, addrs []string, err error) {
	seen := map[int64]bool{}
	add := func(ids []int64) {
		for _, id := range ids {
			if !seen[id] && len(users) < MaxRecipients {
				seen[id] = true
				users = append(users, id)
			}
		}
	}
	for _, tok := range mail.SplitAddresses(render(spec, vars)) {
		low := strings.ToLower(tok)
		var as *Assignee
		switch {
		case low == "starter":
			as = &Assignee{Type: "starter"}
		case low == "actor":
			if a.p != nil && a.p.ID() != 0 {
				add([]int64{a.p.ID()})
			}
			continue
		case low == "assignees":
			if s := x.Definition.Step(x.Step); s != nil && s.Manual() {
				as2 := x.assignee(s)
				as = &as2
			}
		case strings.HasPrefix(low, "user:"), strings.HasPrefix(low, "group:"), strings.HasPrefix(low, "role:"):
			k, v, _ := strings.Cut(tok, ":")
			as = &Assignee{Type: strings.ToLower(k), Value: v}
		default:
			addr, err := mail.ParseAddress(tok)
			if err != nil {
				return nil, nil, err
			}
			if len(addrs) < MaxRecipients {
				addrs = append(addrs, addr)
			}
			continue
		}
		if as == nil {
			continue
		}
		ids, err := pl.members(ctx, tx, x, *as)
		if err != nil {
			return nil, nil, err
		}
		add(ids)
	}
	return users, addrs, nil
}

// sendNotice sends a message of a notify step, a notice or an escalation.
func (pl *Plugin) sendNotice(ctx context.Context, tx pgx.Tx, x *Instance, n *NotifySpec, a actorInfo) error {
	if n == nil {
		return nil
	}
	vars := pl.vars(ctx, tx, x, a)
	users, addrs, err := pl.people(ctx, tx, x, n.To, vars, a)
	if err != nil {
		return err
	}
	subject, body := render(n.Subject, vars), render(n.Body, vars)
	link := fmt.Sprintf("/e/%d?case=%d", x.ElementID, x.ID)
	if n.Channel != ChanEmail && len(users) > 0 {
		kind := KindNotice
		if n.Channel == ChanInbox {
			kind = KindInbox
		}
		// in the wiki: only people who may see the workflow
		// sent by the workflow: the person who caused it gets it too
		if _, err := pl.host.Notify.SendQ(identity.With(ctx, &identity.Principal{}), tx, pl.canView(ctx, tx, x, users), notify.Message{Kind: kind, EntityType: "workflow_case", EntityID: x.ID,
			Link: link, Text: func(string) (string, string) { return subject, body }}); err != nil {
			return err
		}
	}
	if n.Channel == ChanInbox {
		return nil
	}
	var to []string
	if n.Channel == ChanEmail {
		us, err := pl.host.Mail.Users(ctx, tx, users)
		if err != nil {
			return err
		}
		for _, u := range us {
			if u.Email != "" {
				to = append(to, u.Email)
			}
		}
	}
	to = append(to, addrs...)
	if len(to) == 0 {
		return nil
	}
	if !pl.host.Mail.Enabled() {
		return fmt.Errorf("e-mail is switched off (mail.enabled: false)")
	}
	text := body + "\n\n" + vars["case.url"] + "\n"
	for _, addr := range to { // one e-mail per recipient: nobody sees the others
		if _, err := pl.host.Mail.Enqueue(ctx, tx, mail.Message{To: []string{addr}, Subject: subject, Text: text,
			Source: "workflow.process", Ref: fmt.Sprintf("case:%d", x.ID)}); err != nil {
			return err
		}
	}
	return nil
}

// linksFor returns the links of the decisions for a task e-mail ("" = none).
func (pl *Plugin) linksFor(ctx context.Context, tx pgx.Tx, x *Instance, s *Step, uid int64, lang string) string {
	if t := pl.linkText(ctx, tx, x, s, uid, lang); t != "" {
		return "\n\n" + t
	}
	return ""
}

// notices sends the notices of step s on "enter" or "leave".
func (pl *Plugin) notices(ctx context.Context, tx pgx.Tx, x *Instance, s *Step, on string, a actorInfo) error {
	if x.Test {
		return nil
	}
	for i := range s.Notices {
		n := &s.Notices[i]
		if n.On != on {
			continue
		}
		if err := pl.sendNotice(ctx, tx, x, &n.NotifySpec, a); err != nil {
			pl.host.Log.Warn("workflow notice not sent", "case", x.ID, "step", s.ID, "on", on, "err", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------- values

// numbers are the values of a case usable in formulas.
func numbers(x *Instance) map[string]float64 {
	out := map[string]float64{}
	for k, v := range x.Data {
		switch t := v.(type) {
		case float64:
			out[k] = t
		case bool:
			if t {
				out[k] = 1
			}
		case string:
			if f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(t), ",", "."), 64); err == nil {
				out[k] = f
			} else if d, err := time.Parse("2006-01-02", t); err == nil {
				out[k] = float64(d.Unix() / 86400) // days since 1970-01-01, as in properties
			}
		}
	}
	return out
}

func formatNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// assign carries out a set step (values in order: later ones see earlier ones).
func (pl *Plugin) assign(ctx context.Context, tx pgx.Tx, x *Instance, s *Step, a actorInfo) error {
	var failures []string
	for _, as := range s.Set {
		if as.Formula != "" {
			f, err := props.ParseFormula(as.Formula)
			if err != nil {
				failures = append(failures, as.Field+": "+err.Error())
				continue
			}
			v, err := f.Eval(numbers(x))
			if err != nil {
				failures = append(failures, as.Field+": "+err.Error())
				continue
			}
			x.Data[as.Field] = formatNumber(v)
			continue
		}
		v := render(as.Value, pl.vars(ctx, tx, x, a))
		if utf8.RuneCountInString(v) > 20000 {
			v = string([]rune(v)[:20000])
		}
		x.Data[as.Field] = v
	}
	if err := saveData(ctx, tx, x); err != nil {
		return err
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func saveData(ctx context.Context, tx pgx.Tx, x *Instance) error {
	_, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET data=$2, updated_at=now() WHERE id=$1`, x.ID, x.Data)
	return err
}

// ---------------------------------------------------------------- deferred steps

// runAs returns ctx with the principal a deferred step acts as: the
// definer of the case's version (or its starter); they must still see the
// workflow. Changes are marked with the depth of the chain.
func (pl *Plugin) runAs(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (context.Context, error) {
	id := x.DefinerID
	who := "the person who saved the workflow"
	if s.RunAs == "starter" {
		id, who = x.StartedByID, "the person who started the case"
	}
	if id == nil {
		return nil, fmt.Errorf("%s no longer exists", who)
	}
	p, err := pl.host.Auth.LoadPrincipalQ(ctx, tx, *id) // inside the transaction (S-01)
	if err != nil {
		return nil, fmt.Errorf("%s is no longer active", who)
	}
	p.Via = "workflow"
	if !pl.host.ACL.Can(ctx, p, x.CategoryID, acl.View) {
		return nil, fmt.Errorf("%s can no longer see the workflow", who)
	}
	return withDepth(identity.With(ctx, p), x.Depth+1), nil
}

// result of a deferred step: the next step, a note for the history, or
// "wait" (sub-workflow running).
type result struct {
	next, action, label, note string
	wait                      bool
}

// runStep carries out an http, create, props or subflow step.
func (pl *Plugin) runStep(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (result, error) {
	if s.Type == StepParallel { // no rights needed: branches of the same case
		return pl.runParallel(ctx, tx, x, s)
	}
	pctx, err := pl.runAs(ctx, tx, x, s)
	if err != nil {
		return result{}, err
	}
	switch s.Type {
	case StepHTTP:
		return pl.runHTTP(pctx, tx, x, s)
	case StepCreate:
		return pl.runCreate(pctx, tx, x, s)
	case StepProps:
		return pl.runProps(pctx, tx, x, s)
	case StepSubflow:
		return pl.runSubflow(pctx, tx, x, s)
	case StepAccess:
		return pl.runAccess(pctx, tx, x, s)
	case StepMember:
		return pl.runMember(pctx, tx, x, s)
	case StepMove:
		return pl.runMove(pctx, tx, x, s)
	}
	return result{}, fmt.Errorf("unknown step type %q", s.Type)
}

// renderWith replaces placeholders, escaping each value with esc.
func renderWith(tpl string, vars map[string]string, esc func(string) string) string {
	return placeholderRe.ReplaceAllStringFunc(tpl, func(m string) string {
		return esc(vars[placeholderRe.FindStringSubmatch(m)[1]])
	})
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

// MaxAnswer limits the answer of an HTTP step.
const MaxAnswer = 1 << 20

func (pl *Plugin) runHTTP(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (result, error) {
	vars := pl.vars(ctx, tx, x, actorInfo{})
	sec := pl.host.Config.Get().Plugins.Macros
	u, err := url.Parse(renderWith(s.HTTP.URL, vars, url.QueryEscape))
	if err != nil {
		return result{}, err
	}
	if !netguard.Allowed(u, sec.HTTPAllow) {
		return result{}, fmt.Errorf("%s is not on the allow list (plugins.macros.http_allow in config.yaml)", u.Redacted())
	}
	var body io.Reader
	if s.HTTP.Method != "GET" && s.HTTP.Method != "DELETE" && s.HTTP.Body != "" {
		raw := renderWith(s.HTTP.Body, vars, jsonEscape)
		if !json.Valid([]byte(raw)) {
			return result{}, fmt.Errorf("the body is not valid JSON")
		}
		body = strings.NewReader(raw)
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, s.HTTP.Method, u.String(), body)
	if err != nil {
		return result{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "enterprise-wiki-workflow")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := netguard.Client(sec.HTTPAllow, sec.HTTPAllowPrivate, 20*time.Second).Do(req)
	if err != nil {
		var ue *url.Error
		if asURLError(err, &ue) {
			err = ue.Err
		}
		return result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxAnswer+1))
	if err != nil {
		return result{}, err
	}
	if len(raw) > MaxAnswer {
		return result{}, fmt.Errorf("the answer is larger than 1 MB")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return result{}, fmt.Errorf("the server answered %d", resp.StatusCode)
	}
	if len(s.HTTP.Map) > 0 {
		var doc any
		_ = json.Unmarshal(raw, &doc) // not JSON: only "" (the whole answer) works
		for field, path := range s.HTTP.Map {
			v := ""
			if strings.TrimSpace(path) == "" {
				v = string(raw)
			} else {
				v = pick(doc, path)
			}
			if utf8.RuneCountInString(v) > 20000 {
				v = string([]rune(v)[:20000])
			}
			x.Data[field] = v
		}
		if err := saveData(ctx, tx, x); err != nil {
			return result{}, err
		}
	}
	return result{next: s.Next, action: "http", label: strconv.Itoa(resp.StatusCode)}, nil
}

// pick returns the value at a path (a.b.0.c) of a JSON document as text.
func pick(doc any, path string) string {
	cur := doc
	for _, part := range strings.Split(path, ".") {
		switch c := cur.(type) {
		case map[string]any:
			cur = c[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(c) {
				return ""
			}
			cur = c[i]
		default:
			return ""
		}
	}
	switch v := cur.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return formatNumber(v)
	case bool:
		return strconv.FormatBool(v)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func (pl *Plugin) runCreate(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (result, error) {
	vars := pl.vars(ctx, tx, x, actorInfo{})
	c := s.Create
	title := strings.TrimSpace(render(c.Title, vars))
	if utf8.RuneCountInString(title) > 250 {
		title = string([]rune(title)[:250])
	}
	var tags []string
	for _, t := range strings.Split(render(c.Tags, vars), ",") {
		if t = strings.TrimSpace(t); t != "" && len(tags) < 20 {
			tags = append(tags, t)
		}
	}
	body, _ := json.Marshal(map[string]string{"format": "markdown", "source": render(c.Body, vars)})
	e, err := pl.host.Content.CreateElement(ctx, content.ElementInput{CategoryID: c.Category, Type: "core.html", Lang: x.Lang, Title: title,
		Body: body, Tags: tags, Comment: fmt.Sprintf("workflow case %d", x.ID)})
	if err != nil {
		return result{}, err
	}
	if c.StoreID != "" {
		x.Data[c.StoreID] = strconv.FormatInt(e.ID, 10)
	}
	if c.StoreURL != "" {
		x.Data[c.StoreURL] = fmt.Sprintf("%s/e/%d", vars["site.url"], e.ID)
	}
	if err := saveData(ctx, tx, x); err != nil {
		return result{}, err
	}
	return result{next: s.Next, action: "create", label: title, note: fmt.Sprintf("/e/%d", e.ID)}, nil
}

var elementRefRe = regexp.MustCompile(`(?:^|/e/)(\d{1,18})(?:$|[/?#])`)

// call runs a request of the wiki's API in-process with the identity of
// ctx (all checks of the API apply).
func (pl *Plugin) call(ctx context.Context, method, path string, body any) (int, []byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "ewiki")
	rec := httptest.NewRecorder()
	pl.host.Mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes(), nil
}

func apiError(code int, b []byte) error {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(b, &e)
	if e.Error.Message == "" {
		e.Error.Message = http.StatusText(code)
	}
	return fmt.Errorf("%d %s", code, e.Error.Message)
}

func (pl *Plugin) runProps(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (result, error) {
	vars := pl.vars(ctx, tx, x, actorInfo{})
	ref := strings.TrimSpace(render(s.Props.Element, vars))
	m := elementRefRe.FindStringSubmatch(ref)
	if m == nil {
		return result{}, fmt.Errorf("%q is no page (id or address)", ref)
	}
	values := map[string]any{}
	for k, tpl := range s.Props.Values {
		v := strings.TrimSpace(render(tpl, vars))
		if v == "" {
			values[k] = nil // removes the property
		} else {
			values[k] = v
		}
	}
	code, b, err := pl.call(ctx, http.MethodPut, "/api/v1/x/core.properties/elements/"+m[1], map[string]any{"values": values})
	if err != nil {
		return result{}, err
	}
	if code != http.StatusOK {
		return result{}, apiError(code, b)
	}
	return result{next: s.Next, action: "props", note: "/e/" + m[1]}, nil
}

// fieldValues builds the input of a start form from texts.
func fieldValues(fields []Field, texts map[string]string) map[string]any {
	in := map[string]any{}
	for _, f := range fields {
		v, ok := texts[f.ID]
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if f.Type == "checkbox" {
			in[f.ID] = in2s(strings.ToLower(v), "1", "true", "yes", "ja", "on", "x", "✓")
			continue
		}
		if f.Type == "file" || v == "" {
			continue
		}
		in[f.ID] = v
	}
	return in
}

func (pl *Plugin) runSubflow(ctx context.Context, tx pgx.Tx, x *Instance, s *Step) (result, error) {
	sf := s.Subflow
	// woken up: the other case ended (or was started in this visit already)
	var child int64
	var status string
	var data map[string]any
	var outcomeStep string
	var childDef Definition
	err := tx.QueryRow(ctx, `SELECT c.id, c.status, c.data, c.step, c.definition FROM plugin_workflow_instances c, plugin_workflow_instances p
		WHERE p.id=$1 AND c.parent_id=p.id AND c.parent_step=$2 AND c.started_at >= p.step_entered_at ORDER BY c.id DESC LIMIT 1`, x.ID, s.ID).
		Scan(&child, &status, &data, &outcomeStep, &childDef)
	switch {
	case err == nil && status == "open":
		return result{wait: true}, nil
	case err == nil:
		for mine, theirs := range sf.Return {
			if v, ok := data[theirs]; ok {
				x.Data[mine] = v
			}
		}
		if err := saveData(ctx, tx, x); err != nil {
			return result{}, err
		}
		outcome := ""
		if st := childDef.Step(outcomeStep); st != nil {
			outcome = st.Outcome
		}
		next := s.Next
		if (status == "cancelled" || outcome == "failure") && s.NextElse != "" {
			next = s.NextElse
		}
		return result{next: next, action: "subflow_done", label: status + " " + outcome, note: fmt.Sprintf("case %d", child)}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return result{}, err
	}
	if x.Depth+1 > MaxDepth {
		return result{}, fmt.Errorf("more than %d workflows started each other in a chain", MaxDepth)
	}
	def, _, _, err := pl.definitionBy(ctx, sf.Workflow, x.Lang)
	if err != nil {
		return result{}, fmt.Errorf("workflow %d not found", sf.Workflow)
	}
	vars := pl.vars(ctx, tx, x, actorInfo{})
	texts := map[string]string{}
	for k, tpl := range sf.Data {
		texts[k] = render(tpl, vars)
	}
	in := startInput{Title: render(sf.Title, vars), Lang: x.Lang, Data: fieldValues(def.Fields, texts), Depth: x.Depth + 1, Via: "workflow",
		Trigger: map[string]string{"trigger.type": "workflow", "trigger.object.id": strconv.FormatInt(x.ID, 10), "trigger.object.title": x.Title,
			"trigger.object.url": vars["case.url"]}}
	if in.Title == "" {
		in.Title = x.Title
	}
	if sf.Wait {
		in.ParentID, in.ParentStep = &x.ID, s.ID
	}
	id, err := pl.startTx(ctx, tx, sf.Workflow, in)
	if err != nil {
		return result{}, err
	}
	if sf.Wait {
		// the other case may have ended at once (only automatic steps)
		var st string
		if err := tx.QueryRow(ctx, `SELECT status FROM plugin_workflow_instances WHERE id=$1`, id).Scan(&st); err != nil {
			return result{}, err
		}
		if st == "open" {
			if err := event(ctx, tx, x.ID, sp(s.ID), sp(s.ID), "subflow_started", "", fmt.Sprintf("case %d", id), actorInfo{}); err != nil {
				return result{}, err
			}
			return result{wait: true}, nil
		}
		return pl.runSubflow(ctx, tx, x, s)
	}
	return result{next: s.Next, action: "subflow", note: fmt.Sprintf("case %d", id)}, nil
}

// cancelTx closes a case as cancelled (the waiting parent continues).
func (pl *Plugin) cancelTx(ctx context.Context, tx pgx.Tx, x *Instance) error {
	x.Status = "cancelled"
	if _, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET status='cancelled', updated_at=now(), closed_at=now(), wake_at=NULL, due_at=NULL, check_at=NULL
		WHERE id=$1`, x.ID); err != nil {
		return err
	}
	// its open branches end too
	rows, err := tx.Query(ctx, `SELECT id FROM plugin_workflow_instances WHERE parent_id=$1 AND branch AND status='open'`, x.ID)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := cancelBranch(ctx, tx, id); err != nil {
			return err
		}
	}
	return wakeParent(ctx, tx, x)
}

// ---------------------------------------------------------------- deadlines

// holidays returns the days off of the setting "holidays": dates
// (2026-12-24) or yearly days (12-25), separated by commas or lines.
func (pl *Plugin) holidays() map[string]bool {
	out := map[string]bool{}
	raw, _ := pl.host.Settings()["holidays"].(string)
	for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == ';' || r == ' ' }) {
		out[strings.TrimSpace(f)] = true
	}
	return out
}

func offDay(t time.Time, hol map[string]bool) bool {
	wd := t.Weekday()
	return wd == time.Saturday || wd == time.Sunday || hol[t.Format("2006-01-02")] || hol[t.Format("01-02")]
}

// dueTime computes the deadline of a step entered at now.
func (pl *Plugin) dueTime(x *Instance, dl *Deadline, now time.Time) time.Time {
	var hol map[string]bool
	if dl.Workdays {
		hol = pl.holidays()
	}
	return dueTimeWith(x, dl, now, hol)
}

// dueTimeWith computes a deadline with the days off hol.
func dueTimeWith(x *Instance, dl *Deadline, now time.Time, hol map[string]bool) time.Time {
	base := now
	if dl.Field != "" {
		if v, ok := x.Data[dl.Field].(string); ok {
			if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
				base = t.Add(24*time.Hour - time.Second) // end of that day
			}
		}
	}
	if !dl.Workdays {
		return base.Add(time.Duration(dl.Days)*24*time.Hour + time.Duration(dl.Hours)*time.Hour)
	}
	t := base
	for n := dl.Days; n > 0; {
		t = t.AddDate(0, 0, 1)
		if !offDay(t, hol) {
			n--
		}
	}
	for t2 := t.Add(time.Duration(dl.Hours) * time.Hour); ; {
		if !offDay(t2, hol) {
			return t2
		}
		t2 = t2.AddDate(0, 0, 1)
	}
}

// checkTime is when the worker looks at a case next: the next warning or
// the deadline (nil: nothing more to do).
func checkTime(dl *Deadline, due *time.Time, warned int, escalated bool) *time.Time {
	if dl == nil || due == nil || escalated {
		return nil
	}
	if warned < len(dl.Warn) {
		t := due.Add(-time.Duration(dl.Warn[warned]) * time.Hour)
		return &t
	}
	if dl.Escalate == nil {
		return nil
	}
	return due
}

// deadlines sends warnings and escalates overdue steps (worker).
func (pl *Plugin) deadlines(ctx context.Context) error {
	rows, err := pl.host.DB.Query(ctx, `SELECT id FROM plugin_workflow_instances WHERE status='open' AND check_at <= now() ORDER BY check_at LIMIT 200`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := pgx.BeginFunc(ctx, pl.host.DB, func(tx pgx.Tx) error { return pl.deadlineOne(ctx, tx, id) }); err != nil {
			pl.host.Log.Warn("workflow deadline", "case", id, "err", err)
		}
	}
	return nil
}

func (pl *Plugin) deadlineOne(ctx context.Context, tx pgx.Tx, id int64) error {
	x, err := pl.loadTx(ctx, tx, id)
	if err != nil {
		return err
	}
	s := x.Definition.Step(x.Step)
	if x.Status != "open" || s == nil || !s.Manual() || s.Deadline == nil || x.DueAt == nil || x.Escalated {
		_, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET check_at=NULL WHERE id=$1`, id)
		return err
	}
	dl := s.Deadline
	now := time.Now()
	sys := actorInfo{}
	// warnings that are due (several at once after a pause: only the last one)
	sent := false
	for x.Warned < len(dl.Warn) && !now.Before(x.DueAt.Add(-time.Duration(dl.Warn[x.Warned])*time.Hour)) {
		x.Warned++
		sent = true
	}
	if sent && now.Before(*x.DueAt) {
		if err := pl.notifyAssigneesKey(ctx, tx, x, s, "workflow.mail.deadline"); err != nil {
			return err
		}
		if err := event(ctx, tx, x.ID, sp(s.ID), sp(s.ID), "warning", "", x.DueAt.Local().Format("2006-01-02 15:04"), sys); err != nil {
			return err
		}
	}
	if now.Before(*x.DueAt) || dl.Escalate == nil {
		_, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET warned=$2, check_at=$3 WHERE id=$1`, id, x.Warned, checkTime(dl, x.DueAt, x.Warned, false))
		return err
	}
	// escalation
	e := dl.Escalate
	if _, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET warned=$2, escalated=true, check_at=NULL WHERE id=$1`, id, len(dl.Warn)); err != nil {
		return err
	}
	x.Escalated = true
	if err := pl.sendNotice(ctx, tx, x, e.Notify, sys); err != nil {
		pl.host.Log.Warn("escalation message not sent", "case", id, "err", err)
	}
	switch e.Action {
	case "notify":
		return event(ctx, tx, x.ID, sp(s.ID), sp(s.ID), "escalate", "notify", "", sys)
	case "reassign":
		x.Override = e.Assignee
		if _, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET override=$2, updated_at=now() WHERE id=$1`, id, e.Assignee); err != nil {
			return err
		}
		if err := event(ctx, tx, x.ID, sp(s.ID), sp(s.ID), "escalate", "reassign", assigneeText(*e.Assignee), sys); err != nil {
			return err
		}
		return pl.notifyAssignees(ctx, tx, x, s, false)
	case "cancel":
		if err := event(ctx, tx, x.ID, sp(s.ID), nil, "escalate", "cancel", "", sys); err != nil {
			return err
		}
		if err := pl.cancelTx(ctx, tx, x); err != nil {
			return err
		}
		pl.emit(ctx, tx, "workflow.cancelled", x, sys)
		return pl.notifyStarter(ctx, tx, x, sys, "cancelled")
	case "action":
		next := ""
		switch {
		case s.Type == StepApproval && e.ActionID == "approve":
			next = s.Next
		case s.Type == StepApproval && e.ActionID == "reject":
			next = s.NextElse
		default:
			for _, a := range s.Actions {
				if a.ID == e.ActionID {
					next = a.Next
				}
			}
		}
		if next == "" {
			return fmt.Errorf("action %q of the escalation does not exist", e.ActionID)
		}
		if err := event(ctx, tx, x.ID, sp(s.ID), sp(next), "escalate", e.ActionID, "", sys); err != nil {
			return err
		}
		return pl.enter(ctx, tx, x, next, sys)
	}
	return nil
}
