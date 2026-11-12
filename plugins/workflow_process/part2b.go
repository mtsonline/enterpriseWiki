package workflow_process

// Workflow 2, part 2b: links for decisions in task e-mails, test runs,
// versions of cases, the evaluation of a workflow and the retention of
// closed cases.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

func init() {
	secrets.Register(secrets.Column{Table: "plugin_workflow_secret", Key: "id", Value: "secret"})
}

// ---------------------------------------------------------------- links in e-mails

// LinkDays is how long a link of a task e-mail works.
const LinkDays = 14

var linkAAD = secrets.AAD("plugin_workflow_secret", "1")

// linkKey returns the key of the links (created once per site).
func (pl *Plugin) linkKey(ctx context.Context, q pgx.Tx) ([]byte, error) {
	var v []byte
	err := q.QueryRow(ctx, `SELECT secret FROM plugin_workflow_secret WHERE id=1`).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return nil, err
		}
		sealed, serr := pl.host.Secrets.Seal(k, linkAAD)
		if serr != nil {
			return nil, serr
		}
		if _, err := q.Exec(ctx, `INSERT INTO plugin_workflow_secret(id, secret) VALUES (1, $1) ON CONFLICT (id) DO NOTHING`, sealed); err != nil {
			return nil, err
		}
		err = q.QueryRow(ctx, `SELECT secret FROM plugin_workflow_secret WHERE id=1`).Scan(&v)
	}
	if err != nil {
		return nil, err
	}
	return pl.host.Secrets.Open(v, linkAAD)
}

// link is what a link of a task e-mail stands for: one person, one
// decision in one visit of a step of a case.
type link struct {
	uid, caseID int64
	entered     int64 // step_entered_at in microseconds
	action      string
	exp         int64
}

func (l link) payload() string {
	return fmt.Sprintf("v1|%d|%d|%d|%s|%d", l.uid, l.caseID, l.entered, l.action, l.exp)
}

func signLink(key []byte, l link) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(l.payload()))
	return base64.RawURLEncoding.EncodeToString([]byte(l.payload())) + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func parseLink(key []byte, tok string) (link, error) {
	bad := apperr.BadRequest("the link is invalid or expired")
	p, s, ok := strings.Cut(tok, ".")
	if !ok || len(tok) > 400 {
		return link{}, bad
	}
	raw, err1 := base64.RawURLEncoding.DecodeString(p)
	sig, err2 := base64.RawURLEncoding.DecodeString(s)
	if err1 != nil || err2 != nil {
		return link{}, bad
	}
	m := hmac.New(sha256.New, key)
	m.Write(raw)
	if !hmac.Equal(sig, m.Sum(nil)) {
		return link{}, bad
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 6 || parts[0] != "v1" {
		return link{}, bad
	}
	var l link
	var err error
	if l.uid, err = strconv.ParseInt(parts[1], 10, 64); err != nil {
		return link{}, bad
	}
	if l.caseID, err = strconv.ParseInt(parts[2], 10, 64); err != nil {
		return link{}, bad
	}
	if l.entered, err = strconv.ParseInt(parts[3], 10, 64); err != nil {
		return link{}, bad
	}
	l.action = parts[4]
	if l.exp, err = strconv.ParseInt(parts[5], 10, 64); err != nil || time.Now().Unix() > l.exp {
		return link{}, bad
	}
	return l, nil
}

// linkText lists the decisions of step s of x for the person uid as links.
func (pl *Plugin) linkText(ctx context.Context, tx pgx.Tx, x *Instance, s *Step, uid int64, lang string) string {
	if !x.Definition.Links || x.Test || !s.Manual() || x.Handover == "ask" {
		return ""
	}
	key, err := pl.linkKey(ctx, tx)
	if err != nil {
		pl.host.Log.Warn("workflow links: no key", "err", err)
		return ""
	}
	var acts []Action
	if s.Type == StepApproval {
		acts = []Action{{ID: "approve", Label: firstNonEmpty(s.ApproveLabel, pl.host.T(ctx, lang, "workflow.approve", nil))},
			{ID: "reject", Label: firstNonEmpty(s.RejectLabel, pl.host.T(ctx, lang, "workflow.reject", nil))}}
	} else {
		acts = s.Actions
	}
	base := strings.TrimSuffix(pl.host.Config.Get().Server.BaseURL, "/")
	exp := time.Now().Add(LinkDays * 24 * time.Hour).Unix()
	var b strings.Builder
	b.WriteString(pl.host.T(ctx, lang, "workflow.mail.links", nil) + "\n")
	for _, a := range acts {
		tok := signLink(key, link{uid: uid, caseID: x.ID, entered: x.StepEntered.UnixMicro(), action: a.ID, exp: exp})
		fmt.Fprintf(&b, "- %s: %s/workflow/link?t=%s\n", a.Label, base, tok)
	}
	return b.String()
}

// linkTarget checks a link and returns the case it stands for (the caller
// is not involved: the link names its person).
func (pl *Plugin) linkTarget(ctx context.Context, tok string) (link, *Instance, error) {
	var l link
	err := pgx.BeginFunc(ctx, pl.host.DB, func(tx pgx.Tx) error {
		key, err := pl.linkKey(ctx, tx)
		if err != nil {
			return err
		}
		l, err = parseLink(key, tok)
		return err
	})
	if err != nil {
		return l, nil, err
	}
	rows, err := pl.host.DB.Query(ctx, `SELECT `+instCols+` FROM plugin_workflow_instances i JOIN elements e ON e.id=i.element_id WHERE i.id=$1`, l.caseID)
	if err != nil {
		return l, nil, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanInstance)
	if err != nil {
		return l, nil, apperr.BadRequest("the link is invalid or expired")
	}
	if x.Status != "open" || x.StepEntered.UnixMicro() != l.entered {
		return l, x, apperr.Conflict("the case went on already – the link is no longer needed")
	}
	return l, x, nil
}

func linkHash(tok string) string {
	s := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(s[:])
}

func (pl *Plugin) linkRoutes(host *sdk.Host) {
	withoutLogin := func(ctx context.Context, x *Instance) bool {
		on, _ := host.Settings()["links_without_login"].(bool)
		return on && host.ACL.Classification(ctx, x.CategoryID) < acl.Confidential
	}
	// GET /links/info?t=…: what a link of a task e-mail stands for
	host.Handle("GET", "/links/info", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		l, x, err := pl.linkTarget(ctx, r.URL.Query().Get("t"))
		if err != nil {
			return nil, err
		}
		p := identity.From(ctx)
		s := x.Definition.Step(x.Step)
		label, note := l.action, ""
		if s != nil {
			for _, a := range s.Actions {
				if a.ID == l.action {
					label, note = a.Label, a.Note
				}
			}
			if l.action == "approve" {
				label, note = firstNonEmpty(s.ApproveLabel, "approve"), "optional"
			}
			if l.action == "reject" {
				label, note = firstNonEmpty(s.RejectLabel, "reject"), "required"
			}
		}
		return map[string]any{"case_id": x.ID, "element_id": x.ElementID, "title": x.Title, "step": x.StepName, "action": l.action,
			"label": label, "note": note, "mine": p != nil && p.ID() == l.uid, "signed_in": p != nil && p.ID() > 0,
			"without_login": withoutLogin(ctx, x)}, nil
	})
	// POST /links/act {"t": "…", "note": ""}: the decision of the link without
	// signing in (only when allowed, below confidential, once)
	host.Handle("POST", "/links/act", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		var in struct {
			T    string `json:"t"`
			Note string `json:"note"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		l, x, err := pl.linkTarget(ctx, in.T)
		if err != nil {
			return nil, err
		}
		if !withoutLogin(ctx, x) {
			return nil, apperr.Forbidden("please sign in to decide")
		}
		tag, err := host.DB.Exec(ctx, `INSERT INTO plugin_workflow_links_used(hash) VALUES ($1) ON CONFLICT DO NOTHING`, linkHash(in.T))
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, apperr.Conflict("the link was used already")
		}
		p, err := host.Auth.LoadPrincipal(ctx, l.uid)
		if err != nil {
			return nil, apperr.Forbidden("the account of the link is not active")
		}
		p.Via = "link"
		pctx := identity.With(ctx, p)
		if _, err := pl.transition(pctx, x.ID, l.action, in.Note, actionInput{}); err != nil {
			_, _ = host.DB.Exec(ctx, `DELETE FROM plugin_workflow_links_used WHERE hash=$1`, linkHash(in.T))
			return nil, err
		}
		_ = audit.Log(pctx, host.DB, "workflow.link_action", "workflow_case", x.ID, nil, map[string]string{"action": l.action})
		return map[string]any{"done": true, "title": x.Title}, nil
	})
}

// ---------------------------------------------------------------- test runs

// sideEffect reports whether a step type leaves the case (skipped in test runs).
func sideEffect(t string) bool {
	return in(t, StepEmail, StepNotify, StepWebhook, StepHTTP, StepCreate, StepProps, StepSubflow)
}

// ---------------------------------------------------------------- evaluation

// Stats is the evaluation of a workflow (without test runs).
type Stats struct {
	Open      int            `json:"open"`
	Done      int            `json:"done"`
	Cancelled int            `json:"cancelled"`
	Outcomes  map[string]int `json:"outcomes"`
	// Durations of the closed cases in hours.
	AvgHours    float64     `json:"avg_hours"`
	MedianHours float64     `json:"median_hours"`
	Overdue     int         `json:"overdue"`
	Escalations int         `json:"escalations"`
	Steps       []StepStats `json:"steps"`
	Versions    []int       `json:"versions"`
}

// StepStats is the evaluation of one step.
type StepStats struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Open     int     `json:"open"`
	Visits   int     `json:"visits"`
	AvgHours float64 `json:"avg_hours"`
}

func (pl *Plugin) stats(ctx context.Context, element int64, def *Definition) (*Stats, error) {
	st := &Stats{Outcomes: map[string]int{}, Steps: []StepStats{}, Versions: []int{}}
	err := pl.host.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='open'), count(*) FILTER (WHERE status='done'), count(*) FILTER (WHERE status='cancelled'),
		count(*) FILTER (WHERE status='open' AND due_at < now()),
		COALESCE(extract(epoch FROM avg(closed_at - started_at) FILTER (WHERE status='done')) / 3600, 0),
		COALESCE(extract(epoch FROM percentile_cont(0.5) WITHIN GROUP (ORDER BY closed_at - started_at) FILTER (WHERE status='done')) / 3600, 0)
		FROM plugin_workflow_instances WHERE element_id=$1 AND NOT branch AND NOT test`, element).
		Scan(&st.Open, &st.Done, &st.Cancelled, &st.Overdue, &st.AvgHours, &st.MedianHours)
	if err != nil {
		return nil, err
	}
	rows, err := pl.host.DB.Query(ctx, `SELECT COALESCE((SELECT s->>'outcome' FROM jsonb_array_elements(i.definition->'steps') s WHERE s->>'id'=i.step), ''), count(*)
		FROM plugin_workflow_instances i WHERE i.element_id=$1 AND i.status='done' AND NOT i.branch AND NOT i.test GROUP BY 1`, element)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var o string
		var n int
		if err := rows.Scan(&o, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if o == "" {
			o = "neutral"
		}
		st.Outcomes[o] += n
	}
	rows.Close()
	if err := pl.host.DB.QueryRow(ctx, `SELECT count(*) FROM plugin_workflow_events e JOIN plugin_workflow_instances i ON i.id=e.instance_id
		WHERE i.element_id=$1 AND NOT i.test AND e.action='escalate'`, element).Scan(&st.Escalations); err != nil {
		return nil, err
	}
	// time in each step: from entering it to the next event that leaves it
	type agg struct {
		visits int
		hours  float64
	}
	per := map[string]*agg{}
	rows, err = pl.host.DB.Query(ctx, `SELECT step, count(*), COALESCE(extract(epoch FROM avg(left_at - at)) / 3600, 0) FROM (
			SELECT e.to_step AS step, e.at, (SELECT min(n.at) FROM plugin_workflow_events n WHERE n.instance_id=e.instance_id AND n.id > e.id
				AND n.from_step = e.to_step AND n.to_step IS DISTINCT FROM e.to_step) AS left_at
			FROM plugin_workflow_events e JOIN plugin_workflow_instances i ON i.id=e.instance_id
			WHERE i.element_id=$1 AND NOT i.test AND e.to_step IS NOT NULL AND e.to_step IS DISTINCT FROM e.from_step) v
		WHERE left_at IS NOT NULL GROUP BY step`, element)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		a := &agg{}
		if err := rows.Scan(&id, &a.visits, &a.hours); err != nil {
			rows.Close()
			return nil, err
		}
		per[id] = a
	}
	rows.Close()
	open := map[string]int{}
	rows, err = pl.host.DB.Query(ctx, `SELECT step, count(*) FROM plugin_workflow_instances WHERE element_id=$1 AND status='open' AND NOT test GROUP BY step`, element)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		open[id] = n
	}
	rows.Close()
	for _, s := range def.Steps {
		if s.Type == StepEnd {
			continue
		}
		ss := StepStats{ID: s.ID, Name: s.Name, Open: open[s.ID]}
		if a := per[s.ID]; a != nil {
			ss.Visits, ss.AvgHours = a.visits, a.hours
		}
		st.Steps = append(st.Steps, ss)
	}
	rows, err = pl.host.DB.Query(ctx, `SELECT DISTINCT rev FROM plugin_workflow_instances WHERE element_id=$1 AND NOT test ORDER BY rev`, element)
	if err != nil {
		return nil, err
	}
	vs, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return nil, err
	}
	st.Versions = vs
	return st, nil
}

func (pl *Plugin) statsRoutes(host *sdk.Host) {
	// GET /elements/{id}/stats: evaluation (case managers and who may change the workflow)
	host.Handle("GET", "/elements/{id}/stats", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		cat, err := host.Content.ElementCategory(ctx, id, acl.View)
		if err != nil {
			return nil, err
		}
		if !pl.manage(ctx, cat) {
			if _, err := pl.mayChange(ctx, id); err != nil {
				return nil, err
			}
		}
		def, _, _, err := pl.definitionBy(ctx, id, r.URL.Query().Get("lang"))
		if err != nil {
			return nil, err
		}
		return pl.stats(ctx, id, def)
	})
}

// ---------------------------------------------------------------- retention

// TestDays is how long test runs are kept.
const TestDays = 7

// retention deletes or anonymizes closed cases after the days of their
// workflow, and test runs after TestDays (background worker).
func (pl *Plugin) retention(ctx context.Context) error {
	return pgx.BeginFunc(ctx, pl.host.DB, func(tx pgx.Tx) error {
		del, err := tx.Exec(ctx, `DELETE FROM plugin_workflow_instances WHERE (test AND started_at < now() - make_interval(days => $1))
			OR (status <> 'open' AND definition->'retention'->>'mode' = 'delete'
				AND closed_at < now() - make_interval(days => (definition->'retention'->>'days')::int))`, TestDays)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id FROM plugin_workflow_instances WHERE status <> 'open' AND title <> '–'
			AND definition->'retention'->>'mode' = 'anonymize' AND closed_at < now() - make_interval(days => (definition->'retention'->>'days')::int) LIMIT 1000`)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			if _, err := tx.Exec(ctx, `UPDATE plugin_workflow_instances SET title='–', data='{}', trigger='{}', started_by=NULL, definer_id=NULL,
				override=NULL, prev=NULL WHERE id = ANY($1)`, ids); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE plugin_workflow_events SET note='', action_label=CASE WHEN action LIKE '\_%' OR action IN ('start','branch_start') THEN '' ELSE action_label END,
				actor_id=NULL, actor=CASE WHEN actor='system' THEN actor ELSE '–' END WHERE instance_id = ANY($1)`, ids); err != nil {
				return err
			}
		}
		if del.RowsAffected() > 0 || len(ids) > 0 {
			return audit.Log(identity.With(ctx, identity.CLI()), tx, "workflow.retention", "settings", 0, nil,
				map[string]any{"deleted": del.RowsAffected(), "anonymized": len(ids)})
		}
		return nil
	})
}
