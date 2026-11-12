package workflow_process

// Steps of Workflow 2: notification, values, HTTP query, page, properties,
// sub-workflow; deadlines with warnings and escalation; notices on entering
// and leaving a step.

import (
	"net/url"
	"sort"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	props "github.com/mtsonline/enterprisewiki/plugins/core_properties"
)

// Channels of notifications.
const (
	ChanInbox = "inbox" // notification in the wiki (bell)
	ChanEmail = "email"
	ChanBoth  = "both"
)

// NotifySpec is a message of a notify step and of notices. Recipients like
// the e-mail step: starter, actor, assignees, user:NAME, group:NAME,
// role:NAME and (for e-mails) addresses.
type NotifySpec struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body,omitempty"`
	Channel string `json:"channel,omitempty"` // inbox | email | both (default)
}

// Notice is a message when a case enters or leaves a step.
type Notice struct {
	On string `json:"on"` // enter | leave
	NotifySpec
}

// Assign sets a value of the case: a text with placeholders or a formula
// over number fields (as in properties: + - * /, round, min, max, if …).
type Assign struct {
	Field   string `json:"field"`
	Value   string `json:"value,omitempty"`
	Formula string `json:"formula,omitempty"`
}

// HTTPSpec queries another system (allow list plugins.macros.http_allow)
// and stores values of the JSON answer in fields of the case.
type HTTPSpec struct {
	URL    string `json:"url"`              // with placeholders (values are URL-encoded)
	Method string `json:"method,omitempty"` // GET (default), POST, PUT, PATCH, DELETE
	Body   string `json:"body,omitempty"`   // JSON with placeholders (values are JSON-escaped)
	// Map: field of the case -> path in the answer (a.b.0.c; "" = whole answer as text).
	Map map[string]string `json:"map,omitempty"`
}

// CreateSpec creates a page (Markdown) in a category.
type CreateSpec struct {
	Category int64  `json:"category"`
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"` // Markdown with placeholders
	Tags     string `json:"tags,omitempty"` // comma separated, with placeholders
	// StoreID and StoreURL: fields of the case that receive id and address
	// of the new page (for later steps, e.g. properties).
	StoreID  string `json:"store_id,omitempty"`
	StoreURL string `json:"store_url,omitempty"`
}

// PropsSpec sets properties of a page.
type PropsSpec struct {
	// Element: id of the page, with placeholders (e.g. {{field.page}}).
	Element string `json:"element"`
	// Values: key of the property -> value (with placeholders).
	Values map[string]string `json:"values"`
}

// SubflowSpec starts another workflow.
type SubflowSpec struct {
	Workflow int64             `json:"workflow"`
	Title    string            `json:"title,omitempty"`
	Data     map[string]string `json:"data,omitempty"`   // field of the other workflow -> value (placeholders)
	Wait     bool              `json:"wait,omitempty"`   // continue when the other case ended
	Return   map[string]string `json:"return,omitempty"` // field of this case -> field of the other case
}

// Escalation happens when a deadline passed.
type Escalation struct {
	// Action: notify (only a message), reassign (other assignee), action
	// (perform an action of the step: an action id, approve or reject),
	// cancel (close the case).
	Action   string      `json:"action"`
	Notify   *NotifySpec `json:"notify,omitempty"`
	Assignee *Assignee   `json:"assignee,omitempty"`
	ActionID string      `json:"action_id,omitempty"`
}

// Deadline of a task or approval.
type Deadline struct {
	Days  int `json:"days,omitempty"`
	Hours int `json:"hours,omitempty"`
	// Field: a date field; the deadline is that day (end of day) plus days/hours.
	Field string `json:"field,omitempty"`
	// Workdays counts days without weekends and holidays (setting "holidays").
	Workdays bool `json:"workdays,omitempty"`
	// Warn: hours before the deadline at which the assignees are warned.
	Warn     []int       `json:"warn,omitempty"`
	Escalate *Escalation `json:"escalate,omitempty"`
}

var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// targets returns the ids of the values a step writes.
func (s *Step) targets() []string {
	var out []string
	for _, a := range s.Set {
		out = append(out, a.Field)
	}
	if s.HTTP != nil {
		for k := range s.HTTP.Map {
			out = append(out, k)
		}
	}
	if s.Create != nil {
		out = append(out, s.Create.StoreID, s.Create.StoreURL)
	}
	if s.Subflow != nil {
		for k := range s.Subflow.Return {
			out = append(out, k)
		}
	}
	return out
}

func validateSpec(stepID string, n *NotifySpec, needBody bool) error {
	var err error
	if n.To, err = clean(n.To, 1000); err != nil {
		return err
	}
	if n.Subject, err = clean(n.Subject, 250); err != nil {
		return err
	}
	if n.Body, err = clean(n.Body, 20000); err != nil {
		return err
	}
	if n.Channel == "" {
		n.Channel = ChanBoth
	}
	if !in(n.Channel, ChanInbox, ChanEmail, ChanBoth) {
		return apperr.BadRequest("step %q: unknown channel %q", stepID, n.Channel)
	}
	if n.To == "" || n.Subject == "" || (needBody && n.Body == "") {
		return apperr.BadRequest("step %q: recipients and subject of the message are required", stepID)
	}
	return nil
}

func validateNotices(stepID string, list []Notice) ([]Notice, error) {
	if len(list) > 10 {
		return nil, apperr.BadRequest("step %q: at most 10 notices", stepID)
	}
	out := []Notice{}
	for _, n := range list {
		if n.On != "enter" && n.On != "leave" {
			return nil, apperr.BadRequest("step %q: a notice is sent on enter or leave", stepID)
		}
		if err := validateSpec(stepID, &n.NotifySpec, false); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func templ(v string, max int, what, stepID string) (string, error) {
	v, err := clean(v, max)
	if err != nil {
		return "", apperr.BadRequest("step %q: %s longer than %d characters", stepID, what, max)
	}
	return v, nil
}

// validateExtra checks the fields of the steps of Workflow 2 (and removes
// them from steps of other types).
func (s *Step) validateExtra(fieldType map[string]string) error {
	var err error
	n, set, hs, cr, pr, sf := s.Notify, s.Set, s.HTTP, s.Create, s.Props, s.Subflow
	s.Notify, s.Set, s.HTTP, s.Create, s.Props, s.Subflow = nil, nil, nil, nil, nil, nil
	ac, mb, mv := s.Access, s.Member, s.Move
	s.Access, s.Member, s.Move = nil, nil, nil
	runAs := s.RunAs
	s.RunAs = ""
	if !in(s.Type, StepNotify, StepSet, StepParallel) && !apiStep(s.Type) {
		return nil
	}
	// automatic: no assignee, actions, reminders
	s.Assignee, s.Actions, s.Outcome, s.DueDays, s.Required = Assignee{}, []Action{}, "", 0, 0
	s.ApproveLabel, s.RejectLabel, s.Match, s.Rules = "", "", "", nil
	if s.Next == "" {
		return apperr.BadRequest("step %q: the next step is required", s.ID)
	}
	if apiStep(s.Type) {
		if runAs == "" {
			runAs = "definer"
		}
		if !in(runAs, "definer", "starter") {
			return apperr.BadRequest("step %q: run as definer or starter", s.ID)
		}
		s.RunAs = runAs
	}
	target := func(id, what string) error {
		if !idRe.MatchString(id) {
			return apperr.BadRequest("step %q: %s %q must consist of a-z, 0-9, _ and -", s.ID, what, id)
		}
		return nil
	}
	switch s.Type {
	case StepNotify:
		s.NextElse = ""
		if n == nil {
			return apperr.BadRequest("step %q: the message is missing", s.ID)
		}
		if err := validateSpec(s.ID, n, false); err != nil {
			return err
		}
		s.Notify = n
	case StepSet:
		s.NextElse = ""
		if len(set) == 0 || len(set) > 30 {
			return apperr.BadRequest("step %q: 1 to 30 values", s.ID)
		}
		for i := range set {
			a := &set[i]
			if err := target(a.Field, "field"); err != nil {
				return err
			}
			if a.Value, err = templ(a.Value, 5000, "value", s.ID); err != nil {
				return err
			}
			if a.Formula, err = templ(a.Formula, 1000, "formula", s.ID); err != nil {
				return err
			}
			if a.Formula != "" {
				a.Value = ""
				f, err := props.ParseFormula(a.Formula)
				if err != nil {
					return apperr.BadRequest("step %q, formula of %q: %s", s.ID, a.Field, err.Error())
				}
				for _, k := range f.Keys() {
					if _, ok := fieldType[k]; !ok {
						return apperr.BadRequest("step %q, formula of %q: unknown field %q", s.ID, a.Field, k)
					}
				}
			}
		}
		s.Set = set
	case StepHTTP:
		if hs == nil {
			return apperr.BadRequest("step %q: the request is missing", s.ID)
		}
		h := *hs
		if h.URL, err = templ(h.URL, 2000, "address", s.ID); err != nil {
			return err
		}
		probe := placeholderRe.ReplaceAllString(h.URL, "x")
		if u, err := url.Parse(probe); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || placeholderRe.MatchString(hostPart(h.URL)) {
			return apperr.BadRequest("step %q: invalid address (placeholders only in path and query)", s.ID)
		}
		h.Method = strings.ToUpper(h.Method)
		if h.Method == "" {
			h.Method = "GET"
		}
		if !in(h.Method, httpMethods...) {
			return apperr.BadRequest("step %q: unknown method %q", s.ID, h.Method)
		}
		if h.Body, err = templ(h.Body, 20000, "body", s.ID); err != nil {
			return err
		}
		if len(h.Map) > 30 {
			return apperr.BadRequest("step %q: at most 30 values from the answer", s.ID)
		}
		for k, v := range h.Map {
			if err := target(k, "field"); err != nil {
				return err
			}
			if len(v) > 200 {
				return apperr.BadRequest("step %q: path too long", s.ID)
			}
		}
		s.HTTP = &h
	case StepCreate:
		s.NextElse = ""
		if cr == nil || cr.Category <= 0 {
			return apperr.BadRequest("step %q: the category of the new page is required", s.ID)
		}
		c := *cr
		if c.Title, err = templ(c.Title, 500, "title", s.ID); err != nil || c.Title == "" {
			return apperr.BadRequest("step %q: the title of the new page is required", s.ID)
		}
		if c.Body, err = templ(c.Body, 100000, "text", s.ID); err != nil {
			return err
		}
		if c.Tags, err = templ(c.Tags, 1000, "tags", s.ID); err != nil {
			return err
		}
		for _, id := range []string{c.StoreID, c.StoreURL} {
			if id != "" {
				if err := target(id, "field"); err != nil {
					return err
				}
			}
		}
		s.Create = &c
	case StepProps:
		s.NextElse = ""
		if pr == nil {
			return apperr.BadRequest("step %q: the properties are missing", s.ID)
		}
		p := *pr
		if p.Element, err = templ(p.Element, 200, "page", s.ID); err != nil || p.Element == "" {
			return apperr.BadRequest("step %q: the page is required (id or placeholder)", s.ID)
		}
		if len(p.Values) == 0 || len(p.Values) > 50 {
			return apperr.BadRequest("step %q: 1 to 50 properties", s.ID)
		}
		for k, v := range p.Values {
			if len(k) == 0 || len(k) > 64 || len(v) > 5000 {
				return apperr.BadRequest("step %q: invalid property %q", s.ID, k)
			}
		}
		s.Props = &p
	case StepSubflow:
		if sf == nil || sf.Workflow <= 0 {
			return apperr.BadRequest("step %q: the workflow to start is required", s.ID)
		}
		f := *sf
		if f.Title, err = templ(f.Title, 200, "title", s.ID); err != nil {
			return err
		}
		if len(f.Data) > 50 || len(f.Return) > 50 {
			return apperr.BadRequest("step %q: at most 50 values", s.ID)
		}
		for k, v := range f.Data {
			if err := target(k, "field"); err != nil {
				return err
			}
			if len(v) > 5000 {
				return apperr.BadRequest("step %q: value too long", s.ID)
			}
		}
		if !f.Wait {
			f.Return = nil
			s.NextElse = ""
		}
		for k, v := range f.Return {
			if err := target(k, "field"); err != nil {
				return err
			}
			if !idRe.MatchString(v) {
				return apperr.BadRequest("step %q: invalid field %q of the other workflow", s.ID, v)
			}
		}
		s.Subflow = &f
	case StepAccess, StepMember, StepMove:
		return s.validatePart3(ac, mb, mv)
	}
	return nil
}

// hostPart returns scheme and host of a URL template (no placeholders allowed
// there: the allow list must be checked on the real host).
func hostPart(u string) string {
	rest := u
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// validate checks a deadline (only tasks and approvals have one).
func (dl *Deadline) validate(s *Step, fieldType map[string]string) (*Deadline, error) {
	if dl == nil || !s.Manual() {
		return nil, nil
	}
	d := *dl
	if d.Days < 0 || d.Days > 3650 || d.Hours < 0 || d.Hours > 10000 {
		return nil, apperr.BadRequest("step %q: invalid deadline", s.ID)
	}
	if d.Field != "" && fieldType[d.Field] != "date" {
		return nil, apperr.BadRequest("step %q: %q is not a date field", s.ID, d.Field)
	}
	if d.Field == "" && d.Days == 0 && d.Hours == 0 {
		return nil, nil // no deadline
	}
	if len(d.Warn) > 5 {
		return nil, apperr.BadRequest("step %q: at most 5 warnings", s.ID)
	}
	warn := []int{}
	for _, h := range d.Warn {
		if h <= 0 || h > 10000 {
			return nil, apperr.BadRequest("step %q: warnings 1 to 10000 hours before the deadline", s.ID)
		}
		if !in2(warn, h) {
			warn = append(warn, h)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(warn))) // the earliest warning first
	d.Warn = warn
	if e := d.Escalate; e != nil {
		x := *e
		switch x.Action {
		case "":
			d.Escalate = nil
		case "notify":
			x.Assignee, x.ActionID = nil, ""
			if x.Notify == nil {
				return nil, apperr.BadRequest("step %q: the message of the escalation is missing", s.ID)
			}
			n := *x.Notify
			if err := validateSpec(s.ID, &n, false); err != nil {
				return nil, err
			}
			x.Notify = &n
		case "reassign":
			x.ActionID = ""
			if x.Assignee == nil || !in(x.Assignee.Type, "starter", "user", "group", "role") ||
				(x.Assignee.Type != "starter" && strings.TrimSpace(x.Assignee.Value) == "") {
				return nil, apperr.BadRequest("step %q: the new assignee of the escalation is required", s.ID)
			}
			a := *x.Assignee
			a.Value = strings.TrimSpace(a.Value)
			if a.Type == "starter" {
				a.Value = ""
			}
			x.Assignee = &a
		case "action":
			x.Assignee = nil
			ok := false
			if s.Type == StepApproval {
				ok = x.ActionID == "approve" || x.ActionID == "reject"
			}
			for _, a := range s.Actions {
				ok = ok || a.ID == x.ActionID
			}
			if !ok {
				return nil, apperr.BadRequest("step %q: unknown action %q of the escalation", s.ID, x.ActionID)
			}
		case "cancel":
			x.Assignee, x.ActionID = nil, ""
		default:
			return nil, apperr.BadRequest("step %q: unknown escalation %q", s.ID, x.Action)
		}
		if x.Action != "notify" && x.Notify != nil { // an optional message with every escalation
			n := *x.Notify
			if n.To == "" {
				x.Notify = nil
			} else if err := validateSpec(s.ID, &n, false); err != nil {
				return nil, err
			} else {
				x.Notify = &n
			}
		}
		if x.Action != "" {
			d.Escalate = &x
		}
	}
	return &d, nil
}

func in2(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// Modes of fields in the form of a step.
var FormModes = []string{"edit", "required", "read", "hidden"}

// validateForm checks the field rights of a step (tasks and approvals).
func (s *Step) validateForm(fieldType map[string]string) error {
	if !s.Manual() {
		s.Form = nil
		return nil
	}
	if len(s.Form) > 50 {
		return apperr.BadRequest("step %q: at most 50 fields in the form", s.ID)
	}
	seen := map[string]bool{}
	out := []StepField{}
	for _, f := range s.Form {
		t, ok := fieldType[f.Field]
		if !ok {
			return apperr.BadRequest("step %q: unknown field %q", s.ID, f.Field)
		}
		if t == "file" && (f.Mode == "edit" || f.Mode == "required") {
			return apperr.BadRequest("step %q: files are only added in the start form", s.ID)
		}
		if !in(f.Mode, FormModes...) {
			return apperr.BadRequest("step %q: unknown mode %q of field %q", s.ID, f.Mode, f.Field)
		}
		if seen[f.Field] {
			continue
		}
		seen[f.Field] = true
		out = append(out, f)
	}
	if len(out) == 0 {
		out = nil
	}
	s.Form = out
	return nil
}

// FieldMode returns the mode of a field in the form of step s ("" = not listed).
func (s *Step) FieldMode(field string) string {
	for _, f := range s.Form {
		if f.Field == field {
			return f.Mode
		}
	}
	return ""
}

// validateParallel checks the parallel steps (after all steps are known).
func (d *Definition) validateParallel() error {
	for i := range d.Steps {
		s := &d.Steps[i]
		p := s.Parallel
		if s.Type != StepParallel {
			s.Parallel = nil
			continue
		}
		if p == nil || len(p.Branches) == 0 || len(p.Branches) > 20 {
			return apperr.BadRequest("step %q: 1 to 20 branches", s.ID)
		}
		if s.Next == "" {
			return apperr.BadRequest("step %q: the step where the branches meet is required", s.ID)
		}
		x := *p
		for _, b := range x.Branches {
			if d.Step(b) == nil || b == s.ID || b == s.Next {
				return apperr.BadRequest("step %q: invalid branch %q", s.ID, b)
			}
		}
		if x.ForEach != "" {
			ok := false
			for _, f := range d.Fields {
				ok = ok || f.ID == x.ForEach
			}
			for _, st := range d.Steps {
				for _, t := range st.targets() {
					ok = ok || t == x.ForEach
				}
			}
			if !ok || len(x.Branches) != 1 {
				return apperr.BadRequest("step %q: one branch per value needs a field and exactly one branch", s.ID)
			}
		}
		if x.Require < 0 || (x.ForEach == "" && x.Require > len(x.Branches)) || x.Require > 100 {
			return apperr.BadRequest("step %q: invalid number of branches to wait for", s.ID)
		}
		s.Parallel = &x
	}
	return nil
}
