package content_forms

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

// Form is the body of a form element.
type Form struct {
	Intro   string  `json:"intro"`  // text above the form (paragraphs)
	Fields  []Field `json:"fields"` // in reading order
	Layout  string  `json:"layout"` // "grid" (default, accessible) or "free" (placed in pixels)
	Height  int     `json:"height"` // height of the free layout in pixels
	Submit  string  `json:"submit_label"`
	Success string  `json:"success_message"`
	// Limits of submitting
	OncePerUser bool       `json:"once_per_user"` // signed-in persons submit once
	SignedIn    bool       `json:"signed_in"`     // only signed-in persons
	Opens       *time.Time `json:"opens_at,omitempty"`
	Closes      *time.Time `json:"closes_at,omitempty"`
	MaxEntries  int        `json:"max_submissions"` // 0 = no limit
	// Retention: submissions older than this are deleted (0 = kept).
	RetentionDays int `json:"retention_days"`
	// Copy: signed-in persons get a copy to their confirmed address.
	Copy bool `json:"copy_to_submitter"`
	// ProofOfWork: the browser solves a small puzzle before submitting (bots
	// without a browser are slowed down; no CAPTCHA, no third party).
	ProofOfWork bool `json:"proof_of_work"`
	// PublicResults: everyone who sees the form sees the counts of its
	// choice fields (macro "form-results"); never the single submissions.
	PublicResults bool    `json:"public_results"`
	Targets       Targets `json:"targets"`
}

// Field is one field of a form.
type Field struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Label        string   `json:"label"`
	Help         string   `json:"help,omitempty"`
	Required     bool     `json:"required,omitempty"`
	Placeholder  string   `json:"placeholder,omitempty"`
	Default      string   `json:"default,omitempty"`
	Prefill      string   `json:"prefill,omitempty"` // user.name, user.email, user.username, param:NAME
	Options      []Option `json:"options,omitempty"`
	Min          *float64 `json:"min,omitempty"`
	Max          *float64 `json:"max,omitempty"`
	Step         float64  `json:"step,omitempty"`
	MinLen       int      `json:"min_length,omitempty"`
	MaxLen       int      `json:"max_length,omitempty"`
	Pattern      string   `json:"pattern,omitempty"`
	PatternHelp  string   `json:"pattern_help,omitempty"`
	Rows         int      `json:"rows,omitempty"`
	Autocomplete string   `json:"autocomplete,omitempty"`
	ShowIf       *Cond    `json:"show_if,omitempty"`
	Fields       []Field  `json:"fields,omitempty"`   // repeated group: the fields of a row
	MinRows      int      `json:"min_rows,omitempty"` // repeated group
	MaxRows      int      `json:"max_rows,omitempty"` // repeated group
	Formula      string   `json:"formula,omitempty"`  // computed fields
	Decimals     int      `json:"decimals,omitempty"` // computed fields: digits after the point
	// grid layout: columns of 12; free layout: position and size in pixels
	Width int `json:"width,omitempty"`
	X     int `json:"x,omitempty"`
	Y     int `json:"y,omitempty"`
	W     int `json:"w,omitempty"`
	H     int `json:"h,omitempty"`
	// files
	Accept        []string `json:"accept,omitempty"` // kinds (image, pdf, office, text, archive, audio, video) or extensions (.csv)
	MaxFiles      int      `json:"max_files,omitempty"`
	MaxSizeMB     int      `json:"max_size_mb,omitempty"`
	FileComment   bool     `json:"file_comment,omitempty"`
	AllowMacros   bool     `json:"allow_macros,omitempty"`   // Office files with macros
	AllowActive   bool     `json:"allow_active,omitempty"`   // PDF with JavaScript, actions or embedded files
	MediaCategory int64    `json:"media_category,omitempty"` // also store the files in this category (media library)
}

// Option is a choice of select, radio and checkbox fields.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Cond shows a field only when another field has a value.
type Cond struct {
	Field string `json:"field"`
	Op    string `json:"op"` // eq, ne, empty, notempty, contains
	Value string `json:"value,omitempty"`
}

// Targets are what happens with a submission besides being stored.
type Targets struct {
	Store    bool            `json:"store"` // keep the submissions in the wiki (default true)
	Email    []EmailTarget   `json:"email,omitempty"`
	Webhook  *WebhookTarget  `json:"webhook,omitempty"`
	PropDB   *PropDBTarget   `json:"propdb,omitempty"`
	Workflow *WorkflowTarget `json:"workflow,omitempty"`
	Files    *FilesTarget    `json:"files,omitempty"`
}

// FilesTarget stores each submission as a file in the media library.
type FilesTarget struct {
	Category int64    `json:"category_id"`
	Formats  []string `json:"formats"` // pdf, json, csv, txt
	Name     string   `json:"name"`    // file name without extension (placeholders)
}

// EmailTarget sends each submission by e-mail.
type EmailTarget struct {
	To      string `json:"to"` // addresses, user:NAME, group:NAME (comma separated)
	Subject string `json:"subject"`
	Body    string `json:"body"`  // with placeholders {{field.ID}}, {{form.title}}, {{all}} …
	Files   bool   `json:"files"` // attach the uploaded files as links (never as attachments)
}

// WebhookTarget posts each submission as JSON (signed with the secret).
type WebhookTarget struct {
	URL    string `json:"url"`
	Secret string `json:"secret,omitempty"`
}

// PropDBTarget adds each submission as an entry of a property database:
// an element in the category of the database with the fields as properties.
type PropDBTarget struct {
	Element int64             `json:"element_id"`
	Title   string            `json:"title"`   // title of the entry (placeholders)
	Map     map[string]string `json:"mapping"` // field id -> property key
}

// WorkflowTarget starts a case of a workflow with the submission.
type WorkflowTarget struct {
	Element int64             `json:"element_id"`
	Map     map[string]string `json:"mapping"` // form field id -> workflow field id
	Files   bool              `json:"files"`   // links to the files in the case data
}

// Field types.
var (
	InputTypes = []string{"text", "textarea", "email", "tel", "url", "number", "date", "time", "datetime", "select", "multiselect", "radio", "checkbox", "checkboxes", "rating", "range", "file", "hidden", "consent", "calc", "signature", "repeat"}
	// RowTypes are the field types inside a repeated group.
	RowTypes    = []string{"text", "textarea", "email", "tel", "url", "number", "date", "time", "select", "checkbox"}
	StaticTypes = []string{"heading", "paragraph", "divider", "page"}
	FileFormats = []string{"pdf", "json", "csv", "txt"}
	CondOps     = []string{"eq", "ne", "empty", "notempty", "contains"}
	FileKinds   = map[string][]string{
		"image":   {".png", ".jpg", ".jpeg", ".gif", ".webp"},
		"pdf":     {".pdf"},
		"office":  {".docx", ".xlsx", ".pptx", ".odt", ".ods", ".odp", ".doc", ".xls", ".ppt", ".rtf"},
		"text":    {".txt", ".csv", ".md"},
		"archive": {".zip"},
		"audio":   {".mp3", ".ogg", ".wav", ".m4a"},
		"video":   {".mp4", ".webm"},
	}
	// AutocompleteValues are the allowed autocomplete tokens (WCAG 1.3.5).
	AutocompleteValues = []string{"name", "given-name", "family-name", "email", "tel", "organization", "organization-title",
		"street-address", "postal-code", "address-level2", "country-name", "bday", "url", "username", "off"}
)

// Limits of a form.
const (
	MaxFields      = 200
	MaxOptions     = 200
	MaxText        = 10000 // characters of a text value
	MaxLabel       = 500
	MaxFilesField  = 20
	MaxFileMB      = 100
	MaxEmailTarget = 5
)

var idRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// Decode reads a form body (without checking).
func Decode(raw []byte) (*Form, error) {
	f := &Form{}
	if len(raw) == 0 {
		return f, nil
	}
	if err := json.Unmarshal(raw, f); err != nil {
		return nil, apperr.BadRequest("invalid form: %v", err)
	}
	return f, nil
}

// Field returns the field with id.
func (f *Form) Field(id string) *Field {
	for i := range f.Fields {
		if f.Fields[i].ID == id {
			return &f.Fields[i]
		}
	}
	return nil
}

func clean(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}

// Validate checks and normalizes a form.
func (f *Form) Validate() error {
	f.Intro = clean(f.Intro, 20000)
	f.Submit = clean(f.Submit, 100)
	f.Success = clean(f.Success, 2000)
	if f.Layout != "free" {
		f.Layout = "grid"
	}
	f.Height = min(max(f.Height, 0), 5000)
	f.RetentionDays = min(max(f.RetentionDays, 0), 3650)
	f.MaxEntries = max(f.MaxEntries, 0)
	if f.Opens != nil && f.Closes != nil && !f.Closes.After(*f.Opens) {
		return apperr.BadRequest("the form must close after it opens")
	}
	if len(f.Fields) > MaxFields {
		return apperr.BadRequest("at most %d fields", MaxFields)
	}
	seen := map[string]bool{}
	for i := range f.Fields {
		x := &f.Fields[i]
		if err := x.validate(); err != nil {
			return apperr.BadRequest("field %d (%s): %v", i+1, x.Label, err)
		}
		if seen[x.ID] {
			return apperr.BadRequest("field id %q is used twice", x.ID)
		}
		seen[x.ID] = true
	}
	// formulas use input fields above them
	for i, x := range f.Fields {
		if x.Type != "calc" {
			continue
		}
		_, refs, _ := parseFormula(x.Formula)
		for _, r := range refs {
			ok := false
			for _, y := range f.Fields[:i] {
				if y.ID == r && y.Input() && y.Type != "file" && y.Type != "signature" && y.Type != "repeat" {
					ok = true
				}
			}
			if !ok {
				return apperr.BadRequest("field %q: the formula uses %q, which is no field above it", x.ID, r)
			}
		}
	}
	// conditions refer to earlier input fields
	for i, x := range f.Fields {
		if x.ShowIf == nil {
			continue
		}
		ok := false
		for _, y := range f.Fields[:i] {
			if y.ID == x.ShowIf.Field && slices.Contains(InputTypes, y.Type) {
				ok = true
			}
		}
		if !ok {
			return apperr.BadRequest("field %q: the condition must refer to a field above it", x.ID)
		}
	}
	return f.Targets.validate(f)
}

func (x *Field) validate() error {
	if !slices.Contains(InputTypes, x.Type) && !slices.Contains(StaticTypes, x.Type) {
		return fmt.Errorf("unknown type %q", x.Type)
	}
	if !idRe.MatchString(x.ID) {
		return fmt.Errorf("the id must start with a letter and have only a-z, 0-9 and _ (at most 40)")
	}
	x.Label = clean(x.Label, MaxLabel)
	x.Help = clean(x.Help, 2000)
	x.Placeholder = clean(x.Placeholder, 200)
	x.Default = clean(x.Default, MaxText)
	x.PatternHelp = clean(x.PatternHelp, 300)
	if x.Label == "" && x.Type != "divider" && x.Type != "hidden" && x.Type != "page" {
		return fmt.Errorf("a label is needed (screen readers)")
	}
	if x.Prefill != "" && !slices.Contains([]string{"user.name", "user.email", "user.username"}, x.Prefill) && !strings.HasPrefix(x.Prefill, "param:") {
		return fmt.Errorf("unknown prefill %q", x.Prefill)
	}
	if strings.HasPrefix(x.Prefill, "param:") && !idRe.MatchString(strings.TrimPrefix(x.Prefill, "param:")) {
		return fmt.Errorf("invalid URL parameter name")
	}
	if x.Autocomplete != "" && !slices.Contains(AutocompleteValues, x.Autocomplete) {
		return fmt.Errorf("unknown autocomplete value %q", x.Autocomplete)
	}
	switch x.Type {
	case "select", "multiselect", "radio", "checkboxes":
		if len(x.Options) == 0 || len(x.Options) > MaxOptions {
			return fmt.Errorf("1 to %d options", MaxOptions)
		}
		vals := map[string]bool{}
		for i := range x.Options {
			o := &x.Options[i]
			o.Label = clean(o.Label, MaxLabel)
			o.Value = clean(o.Value, 200)
			if o.Value == "" {
				o.Value = o.Label
			}
			if o.Value == "" || vals[o.Value] {
				return fmt.Errorf("options need different values")
			}
			vals[o.Value] = true
		}
	default:
		x.Options = nil
	}
	if x.Min != nil && x.Max != nil && *x.Max < *x.Min {
		return fmt.Errorf("maximum below minimum")
	}
	x.Step = max(x.Step, 0)
	x.MinLen, x.MaxLen = max(x.MinLen, 0), max(x.MaxLen, 0)
	if x.MaxLen == 0 || x.MaxLen > MaxText {
		x.MaxLen = MaxText
		if x.Type == "text" || x.Type == "email" || x.Type == "tel" || x.Type == "url" {
			x.MaxLen = 500
		}
	}
	if x.MinLen > x.MaxLen {
		return fmt.Errorf("minimum length above maximum length")
	}
	if x.Pattern != "" {
		if len(x.Pattern) > 300 {
			return fmt.Errorf("pattern too long")
		}
		if _, err := regexp.Compile(`^(?:` + x.Pattern + `)$`); err != nil {
			return fmt.Errorf("invalid pattern: %v", err)
		}
	}
	x.Rows = min(max(x.Rows, 0), 30)
	x.Width = min(max(x.Width, 0), 12)
	if x.Width == 0 {
		x.Width = 12
	}
	x.X, x.Y = min(max(x.X, 0), 4000), min(max(x.Y, 0), 5000)
	x.W, x.H = min(max(x.W, 0), 4000), min(max(x.H, 0), 2000)
	if x.Type == "signature" {
		// a drawn signature is one PNG; a typed name is a text value
		x.Accept, x.MaxFiles, x.MaxSizeMB, x.MediaCategory = []string{".png"}, 1, 1, 0
		x.AllowMacros, x.AllowActive = false, false
	} else if x.Type == "file" {
		x.MaxFiles = min(max(x.MaxFiles, 1), MaxFilesField)
		if x.MaxSizeMB <= 0 || x.MaxSizeMB > MaxFileMB {
			x.MaxSizeMB = 10
		}
		if len(x.Accept) == 0 {
			x.Accept = []string{"image", "pdf"}
		}
		for i, a := range x.Accept {
			a = strings.ToLower(strings.TrimSpace(a))
			x.Accept[i] = a
			if _, ok := FileKinds[a]; !ok && !regexp.MustCompile(`^\.[a-z0-9]{1,8}$`).MatchString(a) {
				return fmt.Errorf("unknown file kind %q", a)
			}
			if slices.Contains([]string{".exe", ".js", ".html", ".htm", ".svg", ".bat", ".cmd", ".sh", ".ps1", ".msi", ".jar", ".scr", ".vbs", ".hta", ".dll", ".com", ".lnk", ".xhtml", ".php"}, a) {
				return fmt.Errorf("%s files cannot be accepted", a)
			}
		}
	} else {
		x.Accept, x.MaxFiles, x.MaxSizeMB, x.MediaCategory = nil, 0, 0, 0
	}
	if x.Type == "repeat" {
		if len(x.Fields) == 0 || len(x.Fields) > 30 {
			return fmt.Errorf("a repeated group has 1 to 30 fields")
		}
		ids := map[string]bool{}
		for i := range x.Fields {
			y := &x.Fields[i]
			if !slices.Contains(RowTypes, y.Type) {
				return fmt.Errorf("%q cannot be used in a repeated group", y.Type)
			}
			y.ShowIf = nil
			if err := y.validate(); err != nil {
				return fmt.Errorf("%s: %v", y.Label, err)
			}
			if ids[y.ID] {
				return fmt.Errorf("field id %q is used twice in the group", y.ID)
			}
			ids[y.ID] = true
		}
		if x.MaxRows <= 0 {
			x.MaxRows = 20
		}
		x.MaxRows = min(x.MaxRows, 100)
		x.MinRows = min(max(x.MinRows, 0), x.MaxRows)
	} else {
		x.Fields, x.MinRows, x.MaxRows = nil, 0, 0
	}
	if x.Type == "calc" {
		if _, _, err := parseFormula(x.Formula); err != nil {
			return fmt.Errorf("formula: %v", err)
		}
		x.Decimals = min(max(x.Decimals, 0), 6)
		x.Required = false
	} else {
		x.Formula, x.Decimals = "", 0
	}
	if x.ShowIf != nil {
		if !slices.Contains(CondOps, x.ShowIf.Op) {
			return fmt.Errorf("unknown condition %q", x.ShowIf.Op)
		}
		x.ShowIf.Value = clean(x.ShowIf.Value, 200)
	}
	return nil
}

func (t *Targets) validate(f *Form) error {
	if len(t.Email) > MaxEmailTarget {
		return apperr.BadRequest("at most %d e-mail targets", MaxEmailTarget)
	}
	for i := range t.Email {
		e := &t.Email[i]
		e.To = clean(e.To, 2000)
		e.Subject = clean(e.Subject, 300)
		e.Body = clean(e.Body, 20000)
		if len(Recipients(e.To)) == 0 {
			return apperr.BadRequest("e-mail target %d: no recipient", i+1)
		}
		for _, r := range Recipients(e.To) {
			if strings.HasPrefix(r, "user:") || strings.HasPrefix(r, "group:") || strings.HasPrefix(r, "field:") {
				if strings.HasPrefix(r, "field:") {
					fl := f.Field(strings.TrimPrefix(r, "field:"))
					if fl == nil || fl.Type != "email" {
						return apperr.BadRequest("e-mail target %d: %s is no e-mail field", i+1, r)
					}
				}
				continue
			}
			if _, err := mail.ParseAddress(r); err != nil {
				return apperr.BadRequest("e-mail target %d: invalid address %q", i+1, r)
			}
		}
	}
	if w := t.Webhook; w != nil {
		w.URL = strings.TrimSpace(w.URL)
		if w.URL == "" {
			t.Webhook = nil
		} else if u, err := url.Parse(w.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return apperr.BadRequest("webhook: invalid address")
		}
		if t.Webhook != nil && len(w.Secret) > 200 {
			return apperr.BadRequest("webhook: secret too long")
		}
	}
	if p := t.PropDB; p != nil {
		if p.Element <= 0 {
			t.PropDB = nil
		} else {
			p.Title = clean(p.Title, 300)
			for k, v := range p.Map {
				if f.Field(k) == nil || !idRe.MatchString(v) && !regexp.MustCompile(`^[a-z0-9_.-]{1,64}$`).MatchString(v) {
					return apperr.BadRequest("property database: invalid mapping %s → %s", k, v)
				}
			}
		}
	}
	if ft := t.Files; ft != nil {
		if ft.Category <= 0 {
			t.Files = nil
		} else {
			ft.Name = clean(ft.Name, 200)
			if len(ft.Formats) == 0 {
				ft.Formats = []string{"pdf"}
			}
			for _, x := range ft.Formats {
				if !slices.Contains(FileFormats, x) {
					return apperr.BadRequest("files: unknown format %q", x)
				}
			}
			slices.Sort(ft.Formats)
			ft.Formats = slices.Compact(ft.Formats)
		}
	}
	if w := t.Workflow; w != nil {
		if w.Element <= 0 {
			t.Workflow = nil
		} else {
			for k, v := range w.Map {
				if f.Field(k) == nil || !idRe.MatchString(v) {
					return apperr.BadRequest("workflow: invalid mapping %s → %s", k, v)
				}
			}
		}
	}
	return nil
}

// Recipients splits an address list.
func Recipients(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Input reports whether the field takes a value.
func (x *Field) Input() bool { return slices.Contains(InputTypes, x.Type) }

// Visible evaluates the condition of x for the values.
func (x *Field) Visible(values map[string]any) bool {
	c := x.ShowIf
	if c == nil {
		return true
	}
	v := values[c.Field]
	s := ""
	var list []string
	switch t := v.(type) {
	case string:
		s = t
	case bool:
		if t {
			s = "true"
		}
	case []string:
		list = t
		s = strings.Join(t, ",")
	}
	switch c.Op {
	case "empty":
		return s == ""
	case "notempty":
		return s != ""
	case "eq":
		return s == c.Value || slices.Contains(list, c.Value)
	case "ne":
		return s != c.Value && !slices.Contains(list, c.Value)
	case "contains":
		return strings.Contains(strings.ToLower(s), strings.ToLower(c.Value))
	}
	return true
}
