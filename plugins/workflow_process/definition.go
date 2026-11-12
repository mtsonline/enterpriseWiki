package workflow_process

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
)

// Definition is the body of a workflow element (per language).
type Definition struct {
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
	Steps       []Step  `json:"steps"`
	// Triggers start cases without a person (schedule, changes, events,
	// incoming webhooks).
	Triggers []Trigger `json:"triggers,omitempty"`
	// NoManual hides the start form: cases start only by triggers (and
	// forms or other workflows).
	NoManual bool `json:"no_manual,omitempty"`
	// Retention: closed cases are deleted or anonymized after some days.
	Retention *Retention `json:"retention,omitempty"`
	// Links: task e-mails carry links for the decisions.
	Links bool `json:"links,omitempty"`
	// LinkHours: how long a link stays valid (default LinkDays; never past
	// the deadline of the step).
	LinkHours int `json:"link_hours,omitempty"`
	// Rev is the revision the definition was read from (not stored).
	Rev int `json:"-"`
}

// Retention of closed cases.
type Retention struct {
	Days int    `json:"days"`
	Mode string `json:"mode"` // delete | anonymize
}

// Field is an input of the start form.
type Field struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Type     string   `json:"type"` // text | textarea | email | date | number | select | checkbox | file
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
	Help     string   `json:"help,omitempty"`
	// Later: not in the start form – filled in by the assignees of a step
	// (Step.Form).
	Later bool `json:"later,omitempty"`
}

// StepField is a field of the case in the form of a step: edit, required
// (edit and must be filled), read (shown, not editable) or hidden (not
// shown to the assignees of the step).
type StepField struct {
	Field string `json:"field"`
	Mode  string `json:"mode"`
}

// ParallelSpec runs branches at the same time: each branch starts at a
// step and ends when it reaches Join (the next step of the parallel step).
type ParallelSpec struct {
	Branches []string `json:"branches"`
	// Require: branches that must end before the case continues (0 = all);
	// the others are cancelled then.
	Require int `json:"require,omitempty"`
	// ForEach: a field with several values (one per line or comma, or a
	// multiple choice): one branch per value (with a single branch step);
	// the value is {{field.item}} in the branch.
	ForEach string `json:"for_each,omitempty"`
}

// Assignee decides who handles a step.
type Assignee struct {
	Type  string `json:"type"`  // starter | user | group | role
	Value string `json:"value"` // user name (name or name@provider), group or role name
}

// Action leads from a step to the next one.
type Action struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Next  string `json:"next"`
	Note  string `json:"note,omitempty"` // "" | optional | required
}

// Step types.
const (
	StepTask      = "task"      // a person decides with one of the actions
	StepApproval  = "approval"  // one or more approvals (four-eyes principle), one rejection ends it
	StepCondition = "condition" // automatic branch on form data
	StepEmail     = "email"     // automatic: send a predefined e-mail
	StepWebhook   = "webhook"   // automatic: call an external system
	StepDelay     = "delay"     // automatic: wait for a time or a date from the form
	StepNotify    = "notify"    // automatic: message in the wiki and/or by e-mail
	StepSet       = "set"       // automatic: set or compute values of the case
	StepHTTP      = "http"      // automatic: query another system, take values of the answer
	StepCreate    = "create"    // automatic: create a page from a template
	StepProps     = "props"     // automatic: set properties of a page
	StepSubflow   = "subflow"   // automatic: start another workflow (and wait for it)
	StepParallel  = "parallel"  // automatic: branches at the same time, continue when they ended
	StepEnd       = "end"       // closes the case
)

// StepTypes lists all step types.
var StepTypes = []string{StepTask, StepApproval, StepCondition, StepEmail, StepNotify, StepSet, StepWebhook, StepHTTP,
	StepCreate, StepProps, StepSubflow, StepParallel, StepDelay, StepEnd}

// deferred steps are run by the background worker (they call other
// systems or the wiki's API and may take a while).
func (s *Step) deferred() bool {
	return s.Type == StepWebhook || s.Type == StepHTTP || s.Type == StepCreate || s.Type == StepProps || s.Type == StepSubflow || s.Type == StepParallel
}

// Rule is a condition on the case data.
type Rule struct {
	Field string `json:"field"` // form field id or case.title
	Op    string `json:"op"`    // equals | not_equals | contains | not_contains | empty | not_empty | gt | gte | lt | lte
	Value string `json:"value,omitempty"`
}

// EmailSpec is the e-mail of an email step. All texts may contain placeholders.
type EmailSpec struct {
	To      string `json:"to"`                 // addresses, starter, actor, user:NAME, group:NAME, role:NAME, {{field.email}}
	Cc      string `json:"cc,omitempty"`       // like To
	From    string `json:"from,omitempty"`     // sender (must be allowed in mail.allowed_from, otherwise used as Reply-To)
	ReplyTo string `json:"reply_to,omitempty"` // reply address
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// WebhookSpec calls an external system with the case as JSON.
type WebhookSpec struct {
	URL    string `json:"url"`
	Method string `json:"method,omitempty"` // POST (default) or PUT
	// Secret signs the body: header X-Ewiki-Signature: sha256=<hex HMAC>.
	// It is only sent to the server: saved, it is kept outside of the
	// workflow (SecretRef; package secrets) – nobody reads it back.
	Secret    string `json:"secret,omitempty"`
	SecretRef string `json:"secret_ref,omitempty"`
}

// DelaySpec waits a fixed time or until a date from the form.
type DelaySpec struct {
	Days  int    `json:"days,omitempty"`
	Hours int    `json:"hours,omitempty"`
	Until string `json:"until,omitempty"` // id of a date field: wait until that date (plus days/hours)
}

// Step is a state of the process.
type Step struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Assignee    Assignee `json:"assignee"`
	Actions     []Action `json:"actions"`
	// Outcome of an end step: success | failure | neutral.
	Outcome string `json:"outcome,omitempty"`
	// DueDays sends a reminder to the assignees of a task or approval after
	// this many days (0 = no reminder).
	DueDays int `json:"due_days,omitempty"`
	// Required approvals (approval steps, 0 = all members of the group/role).
	Required     int    `json:"required,omitempty"`
	ApproveLabel string `json:"approve_label,omitempty"`
	RejectLabel  string `json:"reject_label,omitempty"`
	// Next: following step of approval (approved), condition (true), email,
	// webhook and delay steps. NextElse: approval rejected, condition false,
	// webhook failed (optional).
	Next     string       `json:"next,omitempty"`
	NextElse string       `json:"next_else,omitempty"`
	Match    string       `json:"match,omitempty"` // condition: all (default) | any
	Rules    []Rule       `json:"rules,omitempty"`
	Email    *EmailSpec   `json:"email,omitempty"`
	Webhook  *WebhookSpec `json:"webhook,omitempty"`
	Delay    *DelaySpec   `json:"delay,omitempty"`
	Notify   *NotifySpec  `json:"notify,omitempty"`
	Set      []Assign     `json:"set,omitempty"`
	HTTP     *HTTPSpec    `json:"http,omitempty"`
	Create   *CreateSpec  `json:"create,omitempty"`
	Props    *PropsSpec   `json:"props,omitempty"`
	Subflow  *SubflowSpec `json:"subflow,omitempty"`
	// RunAs of http, create, props and subflow steps: "definer" (default:
	// the person who saved the version of the workflow) or "starter".
	RunAs string `json:"run_as,omitempty"`
	// Deadline of a task or approval: warnings and escalation.
	Deadline *Deadline `json:"deadline,omitempty"`
	// Notices are sent when a case enters or leaves the step.
	Notices []Notice `json:"notices,omitempty"`
	// Form: fields of the case the assignees of a task or approval fill in
	// or see (field rights of the step).
	Form     []StepField   `json:"form,omitempty"`
	Parallel *ParallelSpec `json:"parallel,omitempty"`
}

// Automatic reports whether the step runs without a person.
func (s *Step) Automatic() bool { return !s.Manual() && s.Type != StepEnd }

// Manual reports whether people act on the step.
func (s *Step) Manual() bool { return s.Type == StepTask || s.Type == StepApproval }

// Step returns the step with id (nil if unknown).
func (d *Definition) Step(id string) *Step {
	for i := range d.Steps {
		if d.Steps[i].ID == id {
			return &d.Steps[i]
		}
	}
	return nil
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// FieldTypes are the supported form field types.
var FieldTypes = []string{"text", "textarea", "email", "date", "number", "select", "checkbox", "file"}

func in(v string, xs ...string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func clean(s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		return "", apperr.BadRequest("text longer than %d characters", max)
	}
	return s, nil
}

// Validate normalizes and checks a definition.
func (d *Definition) Validate() error {
	var err error
	if d.Description, err = clean(d.Description, 20000); err != nil {
		return err
	}
	if len(d.Fields) > 50 || len(d.Steps) > 50 {
		return apperr.BadRequest("at most 50 fields and 50 steps")
	}
	seen := map[string]bool{}
	for i := range d.Fields {
		f := &d.Fields[i]
		if !idRe.MatchString(f.ID) || seen["f:"+f.ID] {
			return apperr.BadRequest("field %d: id must be unique and consist of a-z, 0-9, _ and -", i+1)
		}
		seen["f:"+f.ID] = true
		if f.Label, err = clean(f.Label, 200); err != nil || f.Label == "" {
			return apperr.BadRequest("field %q: label is required (max. 200 characters)", f.ID)
		}
		if f.Help, err = clean(f.Help, 500); err != nil {
			return err
		}
		if f.Type == "" {
			f.Type = "text"
		}
		if !in(f.Type, FieldTypes...) {
			return apperr.BadRequest("field %q: unknown type %q", f.ID, f.Type)
		}
		opts := []string{}
		for _, o := range f.Options {
			if o = strings.TrimSpace(o); o != "" && len(opts) < 100 {
				opts = append(opts, o)
			}
		}
		f.Options = opts
		if f.Type == "select" && len(opts) == 0 {
			return apperr.BadRequest("field %q: a selection needs options", f.ID)
		}
		if f.Type != "select" {
			f.Options = nil
		}
	}
	if len(d.Steps) == 0 {
		return apperr.BadRequest("a workflow needs at least one step")
	}
	fieldType := map[string]string{}
	for _, f := range d.Fields {
		fieldType[f.ID] = f.Type
	}
	// values set by steps (set, http, create, subflow) are usable like fields
	for _, s := range d.Steps {
		for _, id := range s.targets() {
			if _, ok := fieldType[id]; !ok && idRe.MatchString(id) {
				fieldType[id] = "text"
			}
		}
	}
	for i := range d.Steps {
		s := &d.Steps[i]
		if !idRe.MatchString(s.ID) || seen["s:"+s.ID] {
			return apperr.BadRequest("step %d: id must be unique and consist of a-z, 0-9, _ and -", i+1)
		}
		seen["s:"+s.ID] = true
		if s.Name, err = clean(s.Name, 200); err != nil || s.Name == "" {
			return apperr.BadRequest("step %q: name is required (max. 200 characters)", s.ID)
		}
		if s.Description, err = clean(s.Description, 2000); err != nil {
			return err
		}
		if s.Type == "" { // definitions before step types existed
			s.Type = StepTask
			if len(s.Actions) == 0 {
				s.Type = StepEnd
			}
		}
		if !in(s.Type, StepTypes...) {
			return apperr.BadRequest("step %q: unknown type %q", s.ID, s.Type)
		}
		if err := s.validate(fieldType); err != nil {
			return err
		}
	}
	for _, s := range d.Steps {
		for _, a := range s.Actions {
			if d.Step(a.Next) == nil {
				return apperr.BadRequest("step %q, action %q: next step %q does not exist", s.ID, a.ID, a.Next)
			}
		}
		for _, n := range []string{s.Next, s.NextElse} {
			if n != "" && d.Step(n) == nil {
				return apperr.BadRequest("step %q: next step %q does not exist", s.ID, n)
			}
		}
	}
	if err := d.validateParallel(); err != nil {
		return err
	}
	if d.Fields == nil {
		d.Fields = []Field{}
	}
	if r := d.Retention; r != nil {
		if r.Days <= 0 {
			d.Retention = nil
		} else if r.Days > 36500 || (r.Mode != "delete" && r.Mode != "anonymize") {
			return apperr.BadRequest("retention: 1 to 36500 days, delete or anonymize")
		}
	}
	if d.LinkHours < 0 || d.LinkHours > MaxLinkHours {
		return apperr.BadRequest("links: valid for 1 to %d hours", MaxLinkHours)
	}
	return d.validateTriggers()
}

// RuleOps are the operators of condition rules.
var RuleOps = []string{"equals", "not_equals", "contains", "not_contains", "empty", "not_empty", "gt", "gte", "lt", "lte"}

func (s *Step) validate(fieldType map[string]string) error {
	var err error
	need := func(v, what string) error {
		if v == "" {
			return apperr.BadRequest("step %q: %s is required", s.ID, what)
		}
		return nil
	}
	reset := func() { // fields that do not apply to the type
		s.Assignee, s.Actions, s.Outcome, s.DueDays, s.Required = Assignee{}, []Action{}, "", 0, 0
		s.ApproveLabel, s.RejectLabel, s.Match, s.Rules = "", "", "", nil
	}
	assignee := func() error {
		s.Assignee.Value = strings.TrimSpace(s.Assignee.Value)
		if !in(s.Assignee.Type, "starter", "user", "group", "role") {
			return apperr.BadRequest("step %q: assignee type must be starter, user, group or role", s.ID)
		}
		if s.Assignee.Type == "starter" {
			s.Assignee.Value = ""
		} else if s.Assignee.Value == "" {
			return apperr.BadRequest("step %q: assignee is required", s.ID)
		}
		if s.DueDays < 0 || s.DueDays > 365 {
			return apperr.BadRequest("step %q: reminder after 0 to 365 days", s.ID)
		}
		return nil
	}
	email, webhook, delay := s.Email, s.Webhook, s.Delay
	s.Email, s.Webhook, s.Delay = nil, nil, nil
	deadline, notices := s.Deadline, s.Notices
	if err := s.validateExtra(fieldType); err != nil { // the steps of Workflow 2
		return err
	}
	switch s.Type {
	case StepEnd:
		out := s.Outcome
		reset()
		s.Next, s.NextElse = "", ""
		if !in(out, "success", "failure", "neutral") {
			out = "neutral"
		}
		s.Outcome = out
	case StepTask:
		s.Outcome, s.Required, s.ApproveLabel, s.RejectLabel, s.Next, s.NextElse, s.Match, s.Rules = "", 0, "", "", "", "", "", nil
		if err := assignee(); err != nil {
			return err
		}
		if len(s.Actions) == 0 || len(s.Actions) > 10 {
			return apperr.BadRequest("step %q: a task needs 1 to 10 actions", s.ID)
		}
		aseen := map[string]bool{}
		for j := range s.Actions {
			a := &s.Actions[j]
			if !idRe.MatchString(a.ID) || aseen[a.ID] {
				return apperr.BadRequest("step %q, action %d: id must be unique", s.ID, j+1)
			}
			aseen[a.ID] = true
			if a.Label, err = clean(a.Label, 100); err != nil || a.Label == "" {
				return apperr.BadRequest("step %q, action %q: label is required", s.ID, a.ID)
			}
			if !in(a.Note, "", "optional", "required") {
				a.Note = ""
			}
		}
	case StepApproval:
		s.Actions, s.Outcome, s.Match, s.Rules = []Action{}, "", "", nil
		if err := assignee(); err != nil {
			return err
		}
		if s.Required < 0 || s.Required > 100 {
			return apperr.BadRequest("step %q: required approvals 0 to 100", s.ID)
		}
		if s.ApproveLabel, err = clean(s.ApproveLabel, 100); err != nil {
			return err
		}
		if s.RejectLabel, err = clean(s.RejectLabel, 100); err != nil {
			return err
		}
		if err := need(s.Next, "the step after approval"); err != nil {
			return err
		}
		if err := need(s.NextElse, "the step after rejection"); err != nil {
			return err
		}
	case StepCondition:
		match, rules := s.Match, s.Rules
		reset()
		if match != "any" {
			match = "all"
		}
		s.Match = match
		if len(rules) == 0 || len(rules) > 20 {
			return apperr.BadRequest("step %q: a condition needs 1 to 20 rules", s.ID)
		}
		for j := range rules {
			r := &rules[j]
			if r.Field != "case.title" {
				if _, ok := fieldType[r.Field]; !ok {
					return apperr.BadRequest("step %q, rule %d: unknown field %q", s.ID, j+1, r.Field)
				}
			}
			if !in(r.Op, RuleOps...) {
				return apperr.BadRequest("step %q, rule %d: unknown operator %q", s.ID, j+1, r.Op)
			}
			if r.Value, err = clean(r.Value, 500); err != nil {
				return err
			}
		}
		s.Rules = rules
		if err := need(s.Next, "the step if the condition is met"); err != nil {
			return err
		}
		if err := need(s.NextElse, "the step if the condition is not met"); err != nil {
			return err
		}
	case StepEmail:
		reset()
		s.NextElse = ""
		if email == nil {
			return apperr.BadRequest("step %q: the e-mail is missing", s.ID)
		}
		e := *email
		for _, f := range []*string{&e.To, &e.Cc, &e.From, &e.ReplyTo} {
			if *f, err = clean(*f, 1000); err != nil {
				return err
			}
		}
		if e.Subject, err = clean(e.Subject, 250); err != nil {
			return err
		}
		if e.Body, err = clean(e.Body, 50000); err != nil {
			return err
		}
		if e.To == "" || e.Subject == "" || e.Body == "" {
			return apperr.BadRequest("step %q: recipient, subject and text of the e-mail are required", s.ID)
		}
		s.Email = &e
		if err := need(s.Next, "the next step"); err != nil {
			return err
		}
	case StepWebhook:
		reset()
		if webhook == nil {
			return apperr.BadRequest("step %q: the webhook is missing", s.ID)
		}
		w := *webhook
		w.URL = strings.TrimSpace(w.URL)
		if u, err := url.Parse(w.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return apperr.BadRequest("step %q: invalid webhook URL", s.ID)
		}
		w.Method = strings.ToUpper(w.Method)
		if w.Method == "" {
			w.Method = "POST"
		}
		if w.Method != "POST" && w.Method != "PUT" {
			return apperr.BadRequest("step %q: webhook method must be POST or PUT", s.ID)
		}
		// the secret itself never stays in the body (see Type.Prepare)
		w.Secret = ""
		if w.SecretRef != "" && !secrets.ValidRef(w.SecretRef) {
			return apperr.BadRequest("step %q: invalid reference of the secret", s.ID)
		}
		s.Webhook = &w
		if err := need(s.Next, "the next step"); err != nil {
			return err
		}
	case StepDelay:
		reset()
		s.NextElse = ""
		if delay == nil {
			return apperr.BadRequest("step %q: the waiting time is missing", s.ID)
		}
		dl := *delay
		if dl.Days < 0 || dl.Days > 3650 || dl.Hours < 0 || dl.Hours > 10000 {
			return apperr.BadRequest("step %q: invalid waiting time", s.ID)
		}
		if dl.Until != "" && fieldType[dl.Until] != "date" {
			return apperr.BadRequest("step %q: %q is not a date field", s.ID, dl.Until)
		}
		if dl.Until == "" && dl.Days == 0 && dl.Hours == 0 {
			return apperr.BadRequest("step %q: enter a waiting time or a date field", s.ID)
		}
		s.Delay = &dl
		if err := need(s.Next, "the next step"); err != nil {
			return err
		}
	}
	if s.Actions == nil {
		s.Actions = []Action{}
	}
	if s.Deadline, err = deadline.validate(s, fieldType); err != nil {
		return err
	}
	if s.Notices, err = validateNotices(s.ID, notices); err != nil {
		return err
	}
	return s.validateForm(fieldType)
}

// Decode parses a body.
func Decode(raw json.RawMessage) (*Definition, error) {
	var d Definition
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return nil, apperr.BadRequest("invalid workflow: %s", err.Error())
	}
	return &d, nil
}

// ---------------------------------------------------------------- element type

// Type implements content.ElementType.
type Type struct {
	// name shows a group or role given by "#ID" with its current name
	name func(ctx context.Context, kind, value string) string
}

// TypeID implements content.ElementType.
func (Type) TypeID() string { return "workflow.process" }

// Labels implements content.ElementType.
func (Type) Labels() map[string]string { return map[string]string{"en": "Workflow", "de": "Workflow"} }

// EmptyBody implements content.ElementType.
func (Type) EmptyBody() json.RawMessage {
	return json.RawMessage(`{"description":"","fields":[],"steps":[{"id":"start","name":"Start","assignee":{"type":"starter","value":""},"actions":[{"id":"done","label":"Done","next":"done"}]},{"id":"done","name":"Done","actions":[],"outcome":"success"}]}`)
}

// Prepare implements content.Preparer: secrets of webhook steps are moved
// out of the body into the secret store before the body is saved.
func (Type) Prepare(ctx context.Context, _ *content.Service, _ int64, _, _ string, body json.RawMessage) (json.RawMessage, error) {
	d, err := Decode(body)
	if err != nil {
		return nil, err
	}
	gh := guardHost(ctx)
	changed := false
	for i := range d.Steps {
		w := d.Steps[i].Webhook
		if w == nil || w.Secret == "" {
			continue
		}
		if len(w.Secret) > 200 {
			return nil, apperr.BadRequest("step %q: secret too long", d.Steps[i].ID)
		}
		if gh == nil {
			return nil, apperr.BadRequest("secrets cannot be stored here")
		}
		ref, err := secrets.PutRef(ctx, gh.DB, gh.Secrets, identity.From(ctx).ID(), w.Secret)
		if err != nil {
			return nil, err
		}
		w.Secret, w.SecretRef, changed = "", ref, true
	}
	if !changed {
		return body, nil
	}
	return json.Marshal(d)
}

// SecretRefs implements content.SecretHolder.
func (Type) SecretRefs(body json.RawMessage) []string {
	d, err := Decode(body)
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range d.Steps {
		if s.Webhook != nil && s.Webhook.SecretRef != "" {
			out = append(out, s.Webhook.SecretRef)
		}
	}
	return out
}

// Normalize implements content.ElementType.
func (Type) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	d, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(d)
}

// Text implements content.ElementType.
func (Type) Text(raw json.RawMessage) string {
	d, err := Decode(raw)
	if err != nil {
		return ""
	}
	parts := []string{d.Description}
	for _, s := range d.Steps {
		parts = append(parts, s.Name, s.Description)
	}
	for _, f := range d.Fields {
		parts = append(parts, f.Label)
	}
	return strings.Join(parts, "\n")
}

var msgs = map[string]map[string]string{
	"process":   {"en": "Process", "de": "Ablauf"},
	"assignee":  {"en": "Handled by", "de": "Bearbeitet von"},
	"starter":   {"en": "the person who started the case", "de": "die Person, die den Vorgang gestartet hat"},
	"user":      {"en": "user %s", "de": "Benutzer %s"},
	"group":     {"en": "group %s", "de": "Gruppe %s"},
	"role":      {"en": "role %s", "de": "Rolle %s"},
	"end":       {"en": "End of the process", "de": "Ende des Ablaufs"},
	"goto":      {"en": "%s → %s", "de": "%s → %s"},
	"approval":  {"en": "Approval by %s (%s)", "de": "Freigabe durch %s (%s)"},
	"all":       {"en": "all", "de": "alle"},
	"n":         {"en": "%d approval(s)", "de": "%d Freigabe(n)"},
	"approved":  {"en": "approved", "de": "freigegeben"},
	"rejected":  {"en": "rejected", "de": "abgelehnt"},
	"condition": {"en": "Automatic check of the details", "de": "Automatische Prüfung der Angaben"},
	"yes":       {"en": "condition met", "de": "Bedingung erfüllt"},
	"no":        {"en": "otherwise", "de": "sonst"},
	"email":     {"en": "Automatic e-mail: %s", "de": "Automatische E-Mail: %s"},
	"webhook":   {"en": "Automatic transfer to another system", "de": "Automatische Übergabe an ein anderes System"},
	"failed":    {"en": "on error", "de": "bei Fehler"},
	"delay":     {"en": "Waiting time", "de": "Wartezeit"},
	"then":      {"en": "then → %s", "de": "dann → %s"},
	"reminder":  {"en": "Reminder after %d day(s)", "de": "Erinnerung nach %d Tag(en)"},
	"notify":    {"en": "Automatic message", "de": "Automatische Nachricht"},
	"set":       {"en": "Automatic values", "de": "Automatische Werte"},
	"http":      {"en": "Automatic query of another system", "de": "Automatische Abfrage eines anderen Systems"},
	"create":    {"en": "Automatic new page", "de": "Automatisch neue Seite"},
	"props":     {"en": "Automatic properties of a page", "de": "Automatische Eigenschaften einer Seite"},
	"subflow":   {"en": "Another workflow", "de": "Weiterer Workflow"},
	"parallel":  {"en": "Branches at the same time", "de": "Zweige gleichzeitig"},
	"deadline":  {"en": "With a deadline", "de": "Mit Frist"},
	"triggers":  {"en": "Starts automatically", "de": "Startet automatisch"},
}

func msg(lang, key string) string {
	if v := msgs[key][lang]; v != "" {
		return v
	}
	return msgs[key]["en"]
}

// Paragraphs renders plain text with blank-line separated paragraphs.
func Paragraphs(s string) string {
	var b strings.Builder
	for _, p := range regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(s), -1) {
		if p = strings.TrimSpace(p); p != "" {
			b.WriteString("<p>" + strings.ReplaceAll(html.EscapeString(p), "\n", "<br>") + "</p>")
		}
	}
	return b.String()
}

// Render implements content.ElementType: description, process overview and
// the interactive part (hydrated by the frontend widget "workflow").
func (t Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	d, err := Decode(raw)
	if err != nil {
		return "", err
	}
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(`<div class="ewf">`)
	b.WriteString(Paragraphs(d.Description))
	fmt.Fprintf(&b, `<details class="ewf-overview"><summary>%s</summary><ol class="ewf-steps">`, esc(msg(rc.Lang, "process")))
	name := func(id string) string {
		if n := d.Step(id); n != nil {
			return n.Name
		}
		return id
	}
	li := func(text string) { b.WriteString(`<li>` + esc(text) + `</li>`) }
	for _, s := range d.Steps {
		fmt.Fprintf(&b, `<li class="ewf-step ewf-step--%s"><strong>%s</strong>`, esc(s.Type), esc(s.Name))
		if s.Description != "" {
			b.WriteString(`<br><span class="ewf-step-desc">` + esc(s.Description) + `</span>`)
		}
		who := msg(rc.Lang, "starter")
		if s.Assignee.Type != "starter" && s.Assignee.Type != "" {
			v := s.Assignee.Value
			if t.name != nil && strings.HasPrefix(v, "#") {
				v = t.name(ctx, s.Assignee.Type, v)
			}
			who = fmt.Sprintf(msg(rc.Lang, s.Assignee.Type), v)
		}
		switch s.Type {
		case StepEnd:
			b.WriteString(`<br><span class="ewf-end ewf-end--` + esc(s.Outcome) + `">` + esc(msg(rc.Lang, "end")) + `</span></li>`)
			continue
		case StepTask:
			fmt.Fprintf(&b, `<br><span class="ewf-assignee">%s: %s</span><ul class="ewf-actions">`, esc(msg(rc.Lang, "assignee")), esc(who))
			for _, a := range s.Actions {
				li(fmt.Sprintf(msg(rc.Lang, "goto"), a.Label, name(a.Next)))
			}
		case StepApproval:
			req := msg(rc.Lang, "all")
			if s.Required > 0 {
				req = fmt.Sprintf(msg(rc.Lang, "n"), s.Required)
			}
			fmt.Fprintf(&b, `<br><span class="ewf-assignee">%s</span><ul class="ewf-actions">`, esc(fmt.Sprintf(msg(rc.Lang, "approval"), who, req)))
			li(fmt.Sprintf(msg(rc.Lang, "goto"), msg(rc.Lang, "approved"), name(s.Next)))
			li(fmt.Sprintf(msg(rc.Lang, "goto"), msg(rc.Lang, "rejected"), name(s.NextElse)))
		case StepCondition:
			fmt.Fprintf(&b, `<br><span class="ewf-assignee">%s</span><ul class="ewf-actions">`, esc(msg(rc.Lang, "condition")))
			li(fmt.Sprintf(msg(rc.Lang, "goto"), msg(rc.Lang, "yes"), name(s.Next)))
			li(fmt.Sprintf(msg(rc.Lang, "goto"), msg(rc.Lang, "no"), name(s.NextElse)))
		case StepEmail:
			fmt.Fprintf(&b, `<br><span class="ewf-assignee">%s</span><ul class="ewf-actions">`, esc(fmt.Sprintf(msg(rc.Lang, "email"), s.Email.Subject)))
			li(fmt.Sprintf(msg(rc.Lang, "then"), name(s.Next)))
		case StepWebhook:
			fmt.Fprintf(&b, `<br><span class="ewf-assignee">%s</span><ul class="ewf-actions">`, esc(msg(rc.Lang, "webhook")))
			li(fmt.Sprintf(msg(rc.Lang, "then"), name(s.Next)))
			if s.NextElse != "" {
				li(fmt.Sprintf(msg(rc.Lang, "goto"), msg(rc.Lang, "failed"), name(s.NextElse)))
			}
		case StepDelay:
			fmt.Fprintf(&b, `<br><span class="ewf-assignee">%s</span><ul class="ewf-actions">`, esc(msg(rc.Lang, "delay")))
			li(fmt.Sprintf(msg(rc.Lang, "then"), name(s.Next)))
		default: // notify, set, http, create, props, subflow
			fmt.Fprintf(&b, `<br><span class="ewf-assignee">%s</span><ul class="ewf-actions">`, esc(msg(rc.Lang, s.Type)))
			li(fmt.Sprintf(msg(rc.Lang, "then"), name(s.Next)))
			if s.NextElse != "" {
				li(fmt.Sprintf(msg(rc.Lang, "goto"), msg(rc.Lang, "failed"), name(s.NextElse)))
			}
		}
		if s.Deadline != nil {
			li(msg(rc.Lang, "deadline"))
		}
		if s.DueDays > 0 {
			li(fmt.Sprintf(msg(rc.Lang, "reminder"), s.DueDays))
		}
		b.WriteString(`</ul></li>`)
	}
	b.WriteString(`</ol></details>`)
	if rc.ElementID != 0 {
		fmt.Fprintf(&b, `<div class="ewf-app" data-ew-widget="workflow" data-element="%d"></div>`, rc.ElementID)
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}
