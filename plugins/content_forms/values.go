package content_forms

import (
	"encoding/json"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// FieldError is a problem with one value (code for the frontend's text).
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"` // required, too_short, too_long, pattern, email, url, tel, number, min, max, date, time, option, consent, files
	Param string `json:"param,omitempty"`
}

var telRe = regexp.MustCompile(`^[+0-9 ()./-]{3,40}$`)

// CheckValues validates submitted values (by field id) against the form and
// returns the normalized values of the visible input fields. File fields are
// checked separately (counts in files).
func (f *Form) CheckValues(in map[string]json.RawMessage, files map[string]int) (map[string]any, []FieldError) {
	out := map[string]any{}
	var errs []FieldError
	bad := func(id, code, param string) { errs = append(errs, FieldError{Field: id, Code: code, Param: param}) }
	for i := range f.Fields {
		x := &f.Fields[i]
		if !x.Input() {
			continue
		}
		if !x.Visible(out) {
			continue // hidden by its condition: the value is dropped
		}
		raw, given := in[x.ID]
		switch x.Type {
		case "file":
			n := files[x.ID]
			if x.Required && n == 0 {
				bad(x.ID, "required", "")
			}
			if n > x.MaxFiles {
				bad(x.ID, "files", strconv.Itoa(x.MaxFiles))
			}
			continue
		case "checkbox", "consent":
			var b bool
			if given && json.Unmarshal(raw, &b) != nil {
				var s string
				if json.Unmarshal(raw, &s) == nil {
					b = s == "true" || s == "on" || s == "1"
				}
			}
			if x.Required && !b {
				if x.Type == "consent" {
					bad(x.ID, "consent", "")
				} else {
					bad(x.ID, "required", "")
				}
			}
			out[x.ID] = b
			continue
		case "multiselect", "checkboxes":
			var list []string
			if given && json.Unmarshal(raw, &list) != nil {
				bad(x.ID, "option", "")
				continue
			}
			var keep []string
			for _, v := range list {
				if !x.hasOption(v) {
					bad(x.ID, "option", "")
					break
				}
				if !slices.Contains(keep, v) {
					keep = append(keep, v)
				}
			}
			if x.Required && len(keep) == 0 {
				bad(x.ID, "required", "")
			}
			out[x.ID] = keep
			continue
		}
		var s string
		if given {
			if json.Unmarshal(raw, &s) != nil {
				var n float64
				if json.Unmarshal(raw, &n) == nil {
					s = strconv.FormatFloat(n, 'f', -1, 64)
				} else {
					bad(x.ID, "invalid", "")
					continue
				}
			}
		}
		s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
		if x.Type != "textarea" {
			s = strings.Join(strings.Fields(s), " ")
		}
		if s == "" {
			if x.Required {
				bad(x.ID, "required", "")
			}
			out[x.ID] = ""
			continue
		}
		n := utf8.RuneCountInString(s)
		if n > x.MaxLen {
			bad(x.ID, "too_long", strconv.Itoa(x.MaxLen))
			continue
		}
		if n < x.MinLen {
			bad(x.ID, "too_short", strconv.Itoa(x.MinLen))
			continue
		}
		if x.Pattern != "" && !regexp.MustCompile(`^(?:`+x.Pattern+`)$`).MatchString(s) {
			bad(x.ID, "pattern", x.PatternHelp)
			continue
		}
		switch x.Type {
		case "email":
			a, err := mail.ParseAddress(s)
			if err != nil || a.Address != s || strings.ContainsAny(s, "<>\" ") {
				bad(x.ID, "email", "")
				continue
			}
		case "url":
			u, err := url.Parse(s)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				bad(x.ID, "url", "")
				continue
			}
		case "tel":
			if !telRe.MatchString(s) {
				bad(x.ID, "tel", "")
				continue
			}
		case "number", "range", "rating":
			v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
			if err != nil {
				bad(x.ID, "number", "")
				continue
			}
			lo, hi := x.Min, x.Max
			if x.Type == "rating" {
				one, five := 1.0, float64(max(5, int(ptrOr(x.Max, 5))))
				lo, hi = &one, &five
			}
			if lo != nil && v < *lo {
				bad(x.ID, "min", strconv.FormatFloat(*lo, 'f', -1, 64))
				continue
			}
			if hi != nil && v > *hi {
				bad(x.ID, "max", strconv.FormatFloat(*hi, 'f', -1, 64))
				continue
			}
			out[x.ID] = v
			continue
		case "date":
			if _, err := time.Parse("2006-01-02", s); err != nil {
				bad(x.ID, "date", "")
				continue
			}
		case "time":
			if _, err := time.Parse("15:04", s); err != nil {
				bad(x.ID, "time", "")
				continue
			}
		case "datetime":
			if _, err := time.Parse("2006-01-02T15:04", s); err != nil {
				bad(x.ID, "date", "")
				continue
			}
		case "select", "radio":
			if !x.hasOption(s) {
				bad(x.ID, "option", "")
				continue
			}
		}
		out[x.ID] = s
	}
	return out, errs
}

func ptrOr(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}

func (x *Field) hasOption(v string) bool {
	for _, o := range x.Options {
		if o.Value == v {
			return true
		}
	}
	return false
}

// Text returns a value as text (for e-mails, exports, placeholders).
func (f *Form) Text(id string, v any) string {
	x := f.Field(id)
	label := func(s string) string {
		if x != nil {
			for _, o := range x.Options {
				if o.Value == s {
					return o.Label
				}
			}
		}
		return s
	}
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return label(t)
	case bool:
		if t {
			return "✓"
		}
		return ""
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []string:
		parts := make([]string, len(t))
		for i, s := range t {
			parts[i] = label(s)
		}
		return strings.Join(parts, ", ")
	case []any:
		parts := make([]string, 0, len(t))
		for _, s := range t {
			if str, ok := s.(string); ok {
				parts = append(parts, label(str))
			}
		}
		return strings.Join(parts, ", ")
	}
	b, _ := json.Marshal(v)
	return string(b)
}
