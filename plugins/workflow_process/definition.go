package workflow_process

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

// Definition is the body of a workflow element (per language).
type Definition struct {
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
	Steps       []Step  `json:"steps"`
}

// Field is an input of the start form.
type Field struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Type     string   `json:"type"` // text | textarea | email | date | number | select | checkbox | file
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
	Help     string   `json:"help,omitempty"`
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

// Step is a state of the process. A step without actions ends the case.
type Step struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Assignee    Assignee `json:"assignee"`
	Actions     []Action `json:"actions"`
	// Outcome of a final step: success | failure | neutral.
	Outcome string `json:"outcome,omitempty"`
}

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
		if s.Actions == nil {
			s.Actions = []Action{}
		}
		if len(s.Actions) == 0 {
			if !in(s.Outcome, "success", "failure", "neutral") {
				s.Outcome = "neutral"
			}
			s.Assignee = Assignee{}
			continue
		}
		s.Outcome = ""
		s.Assignee.Value = strings.TrimSpace(s.Assignee.Value)
		if !in(s.Assignee.Type, "starter", "user", "group", "role") {
			return apperr.BadRequest("step %q: assignee type must be starter, user, group or role", s.ID)
		}
		if s.Assignee.Type == "starter" {
			s.Assignee.Value = ""
		} else if s.Assignee.Value == "" {
			return apperr.BadRequest("step %q: assignee is required", s.ID)
		}
		if len(s.Actions) > 10 {
			return apperr.BadRequest("step %q: at most 10 actions", s.ID)
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
	}
	for _, s := range d.Steps {
		for _, a := range s.Actions {
			if d.Step(a.Next) == nil {
				return apperr.BadRequest("step %q, action %q: next step %q does not exist", s.ID, a.ID, a.Next)
			}
		}
	}
	if d.Fields == nil {
		d.Fields = []Field{}
	}
	return nil
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
type Type struct{}

// TypeID implements content.ElementType.
func (Type) TypeID() string { return "workflow.process" }

// Labels implements content.ElementType.
func (Type) Labels() map[string]string { return map[string]string{"en": "Workflow", "de": "Workflow"} }

// EmptyBody implements content.ElementType.
func (Type) EmptyBody() json.RawMessage {
	return json.RawMessage(`{"description":"","fields":[],"steps":[{"id":"start","name":"Start","assignee":{"type":"starter","value":""},"actions":[{"id":"done","label":"Done","next":"done"}]},{"id":"done","name":"Done","actions":[],"outcome":"success"}]}`)
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
	"process":  {"en": "Process", "de": "Ablauf"},
	"assignee": {"en": "Handled by", "de": "Bearbeitet von"},
	"starter":  {"en": "the person who started the case", "de": "die Person, die den Vorgang gestartet hat"},
	"user":     {"en": "user %s", "de": "Benutzer %s"},
	"group":    {"en": "group %s", "de": "Gruppe %s"},
	"role":     {"en": "role %s", "de": "Rolle %s"},
	"end":      {"en": "End of the process", "de": "Ende des Ablaufs"},
	"goto":     {"en": "%s → %s", "de": "%s → %s"},
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
func (Type) Render(_ context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	d, err := Decode(raw)
	if err != nil {
		return "", err
	}
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(`<div class="ewf">`)
	b.WriteString(Paragraphs(d.Description))
	fmt.Fprintf(&b, `<details class="ewf-overview"><summary>%s</summary><ol class="ewf-steps">`, esc(msg(rc.Lang, "process")))
	for _, s := range d.Steps {
		fmt.Fprintf(&b, `<li class="ewf-step"><strong>%s</strong>`, esc(s.Name))
		if s.Description != "" {
			b.WriteString(`<br><span class="ewf-step-desc">` + esc(s.Description) + `</span>`)
		}
		if len(s.Actions) == 0 {
			b.WriteString(`<br><span class="ewf-end ewf-end--` + esc(s.Outcome) + `">` + esc(msg(rc.Lang, "end")) + `</span></li>`)
			continue
		}
		who := msg(rc.Lang, "starter")
		if s.Assignee.Type != "starter" {
			who = fmt.Sprintf(msg(rc.Lang, s.Assignee.Type), s.Assignee.Value)
		}
		fmt.Fprintf(&b, `<br><span class="ewf-assignee">%s: %s</span><ul class="ewf-actions">`, esc(msg(rc.Lang, "assignee")), esc(who))
		for _, a := range s.Actions {
			next := a.Next
			if n := d.Step(a.Next); n != nil {
				next = n.Name
			}
			b.WriteString(`<li>` + esc(fmt.Sprintf(msg(rc.Lang, "goto"), a.Label, next)) + `</li>`)
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
