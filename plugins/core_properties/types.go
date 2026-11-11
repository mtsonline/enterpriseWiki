package core_properties

import (
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

// Storage is where the typed value of a property lives (one column of
// plugin_prop_values each, indexed per key), so filters, sorting and sums
// run in the database.
type Storage string

// Storages.
const (
	StoreText  Storage = "txt"   // text, choices
	StoreNum   Storage = "num"   // numbers, yes/no
	StoreTime  Storage = "ts"    // dates and times
	StoreRange Storage = "range" // ts .. ts_end
	StoreArr   Storage = "arr"   // several choices
	StoreRefs  Storage = "refs"  // users, groups, elements, categories
)

// TypeDef describes a type of property.
type TypeDef struct {
	ID      string  `json:"id"`
	Group   string  `json:"group"` // text | number | time | choice | reference | auto
	Storage Storage `json:"storage"`
	// Options names the options of a definition that apply (for the editor).
	Options []string `json:"options,omitempty"`
	// Computed: not entered by people (numbering, formula).
	Computed bool `json:"computed,omitempty"`
}

// Types are all types of properties (tags are the built-in property "tags").
var Types = []TypeDef{
	{ID: "text", Group: "text", Storage: StoreText, Options: []string{"max_len", "pattern"}},
	{ID: "textarea", Group: "text", Storage: StoreText, Options: []string{"max_len"}},
	{ID: "email", Group: "text", Storage: StoreText},
	{ID: "phone", Group: "text", Storage: StoreText},
	{ID: "url", Group: "text", Storage: StoreText},
	{ID: "color", Group: "text", Storage: StoreText},
	{ID: "code", Group: "text", Storage: StoreText, Options: []string{"pattern"}},
	{ID: "language", Group: "text", Storage: StoreText},
	{ID: "country", Group: "text", Storage: StoreText},
	{ID: "integer", Group: "number", Storage: StoreNum, Options: []string{"min", "max", "unit"}},
	{ID: "decimal", Group: "number", Storage: StoreNum, Options: []string{"min", "max", "decimals", "unit"}},
	{ID: "percent", Group: "number", Storage: StoreNum, Options: []string{"min", "max", "decimals"}},
	{ID: "currency", Group: "number", Storage: StoreNum, Options: []string{"min", "max", "currency"}},
	{ID: "unit", Group: "number", Storage: StoreNum, Options: []string{"min", "max", "decimals", "unit"}},
	{ID: "rating", Group: "number", Storage: StoreNum, Options: []string{"max"}},
	{ID: "progress", Group: "number", Storage: StoreNum},
	{ID: "duration", Group: "number", Storage: StoreNum},
	{ID: "boolean", Group: "choice", Storage: StoreNum},
	{ID: "date", Group: "time", Storage: StoreTime},
	{ID: "datetime", Group: "time", Storage: StoreTime},
	{ID: "time", Group: "time", Storage: StoreText},
	{ID: "daterange", Group: "time", Storage: StoreRange},
	{ID: "select", Group: "choice", Storage: StoreText, Options: []string{"choices"}},
	{ID: "status", Group: "choice", Storage: StoreText, Options: []string{"choices"}},
	{ID: "multiselect", Group: "choice", Storage: StoreArr, Options: []string{"choices"}},
	{ID: "user", Group: "reference", Storage: StoreRefs, Options: []string{"multiple"}},
	{ID: "group", Group: "reference", Storage: StoreRefs, Options: []string{"multiple"}},
	{ID: "element", Group: "reference", Storage: StoreRefs, Options: []string{"multiple"}},
	{ID: "category", Group: "reference", Storage: StoreRefs},
	{ID: "media", Group: "reference", Storage: StoreText},
	{ID: "location", Group: "reference", Storage: StoreText},
	{ID: "version", Group: "text", Storage: StoreText},
	{ID: "autonumber", Group: "auto", Storage: StoreText, Options: []string{"prefix", "digits"}, Computed: true},
	{ID: "formula", Group: "auto", Storage: StoreNum, Options: []string{"formula", "decimals", "unit"}, Computed: true},
}

// TypeByID returns a type.
func TypeByID(id string) (TypeDef, bool) {
	i := slices.IndexFunc(Types, func(t TypeDef) bool { return t.ID == id })
	if i < 0 {
		return TypeDef{}, false
	}
	return Types[i], true
}

// Choice is a value of a select, multiselect or status property.
type Choice struct {
	Value  string            `json:"value"`
	Labels map[string]string `json:"labels,omitempty"`
	// Color of a status: one of the palette names (accessible contrast)
	Color string `json:"color,omitempty"`
}

// Options of a definition (only those of its type apply).
type Options struct {
	Choices  []Choice `json:"choices,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Decimals *int     `json:"decimals,omitempty"`
	Unit     string   `json:"unit,omitempty"`
	Currency string   `json:"currency,omitempty"`
	MaxLen   int      `json:"max_len,omitempty"`
	Pattern  string   `json:"pattern,omitempty"`
	Multiple bool     `json:"multiple,omitempty"`
	Prefix   string   `json:"prefix,omitempty"`
	Digits   int      `json:"digits,omitempty"`
	Formula  string   `json:"formula,omitempty"`
}

// Palette of status colours (the template maps them to accessible colours).
var Palette = []string{"grey", "blue", "green", "yellow", "orange", "red", "purple", "teal"}

var (
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
	langRe     = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)
	countryRe  = regexp.MustCompile(`^[A-Z]{2}$`)
	colorRe    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	phoneRe    = regexp.MustCompile(`^\+?[0-9 ()/.-]{3,30}$`)
	timeRe     = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	versionRe  = regexp.MustCompile(`^v?(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})(?:\.(0|[1-9][0-9]{0,5}))?(?:-[0-9A-Za-z.-]{1,40})?$`)
	prefixRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{0,12}$`)
	mediaRe    = regexp.MustCompile(`^/api/v1/(files|media)/[A-Za-z0-9/_.-]{1,200}$`)
	unitRe     = regexp.MustCompile(`^[\p{L}\p{N} %°/²³.-]{0,16}$`)
)

// CheckOptions validates the options of a definition of type t.
func CheckOptions(t TypeDef, o *Options) error {
	allowed := map[string]bool{}
	for _, k := range t.Options {
		allowed[k] = true
	}
	if !allowed["choices"] {
		o.Choices = nil
	}
	if len(o.Choices) > 200 {
		return apperr.BadRequest("at most 200 choices")
	}
	seen := map[string]bool{}
	for i := range o.Choices {
		c := &o.Choices[i]
		c.Value = strings.TrimSpace(c.Value)
		if c.Value == "" || utf8.RuneCountInString(c.Value) > 100 || seen[strings.ToLower(c.Value)] {
			return apperr.BadRequest("choices: empty, too long or duplicate value %q", c.Value)
		}
		seen[strings.ToLower(c.Value)] = true
		if c.Color != "" && !slices.Contains(Palette, c.Color) {
			return apperr.BadRequest("choices: unknown colour %q", c.Color)
		}
		for l, v := range c.Labels {
			if !langRe.MatchString(l) || utf8.RuneCountInString(v) > 100 {
				return apperr.BadRequest("choices: invalid label")
			}
		}
	}
	if !allowed["min"] {
		o.Min = nil
	}
	if !allowed["max"] {
		o.Max = nil
	}
	if o.Min != nil && o.Max != nil && *o.Min > *o.Max {
		return apperr.BadRequest("min is larger than max")
	}
	if t.ID == "rating" && (o.Max == nil || *o.Max < 2 || *o.Max > 10) {
		five := 5.0
		o.Max = &five
	}
	if !allowed["decimals"] {
		o.Decimals = nil
	} else if o.Decimals != nil && (*o.Decimals < 0 || *o.Decimals > 6) {
		return apperr.BadRequest("decimals: 0 to 6")
	}
	if !allowed["unit"] {
		o.Unit = ""
	} else if !unitRe.MatchString(o.Unit) {
		return apperr.BadRequest("unit: at most 16 letters, digits or symbols")
	}
	if !allowed["currency"] {
		o.Currency = ""
	} else if o.Currency == "" {
		o.Currency = "EUR"
	} else if !currencyRe.MatchString(o.Currency) {
		return apperr.BadRequest("currency: ISO 4217 code such as EUR")
	}
	if !allowed["max_len"] || o.MaxLen < 0 || o.MaxLen > MaxValueLen {
		o.MaxLen = 0
	}
	if !allowed["pattern"] {
		o.Pattern = ""
	} else if o.Pattern != "" {
		if len(o.Pattern) > 200 {
			return apperr.BadRequest("pattern too long")
		}
		// Go regular expressions run in linear time (no catastrophic backtracking)
		if _, err := regexp.Compile("^(?:" + o.Pattern + ")$"); err != nil {
			return apperr.BadRequest("pattern: %v", err)
		}
	}
	if !allowed["multiple"] {
		o.Multiple = false
	}
	if !allowed["prefix"] {
		o.Prefix, o.Digits = "", 0
	} else {
		if !prefixRe.MatchString(o.Prefix) {
			return apperr.BadRequest("prefix: up to 12 letters, digits, _ or -")
		}
		if o.Digits < 0 || o.Digits > 9 {
			return apperr.BadRequest("digits: 0 to 9")
		}
	}
	if !allowed["formula"] {
		o.Formula = ""
	} else {
		o.Formula = strings.TrimSpace(o.Formula)
		if o.Formula == "" {
			return apperr.BadRequest("a formula is required")
		}
		if _, err := ParseFormula(o.Formula); err != nil {
			return apperr.BadRequest("formula: %v", err)
		}
	}
	return nil
}

// Typed is a checked value: the JSON shown to people and the typed columns.
type Typed struct {
	Raw   json.RawMessage
	Txt   *string
	Num   *float64
	TS    *time.Time
	TSEnd *time.Time
	Arr   []string
	Refs  []int64
}

// Location is the value of a location property.
type Location struct {
	Address string   `json:"address"`
	Lat     *float64 `json:"lat,omitempty"`
	Lng     *float64 `json:"lng,omitempty"`
}

// DateRange is the value of a date range.
type DateRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func bad(key, format string, args ...any) error {
	return apperr.BadRequest("%s: %s", key, fmt.Sprintf(format, args...))
}

func ptr[T any](v T) *T { return &v }

func asString(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return strings.TrimSpace(s), true
}

func asNumber(raw json.RawMessage) (float64, bool) {
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return f, !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	// "12,5" from a text field
	s, ok := asString(raw)
	if !ok || s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
}

func asIDs(raw json.RawMessage) ([]int64, bool) {
	var ids []int64
	if json.Unmarshal(raw, &ids) == nil {
		return ids, true
	}
	var one int64
	if json.Unmarshal(raw, &one) == nil {
		return []int64{one}, true
	}
	return nil, false
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// CheckValue validates a value of type t with options o; key names the
// property in errors. A null or empty value returns nil (no value).
func CheckValue(key string, t TypeDef, o Options, raw json.RawMessage) (*Typed, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if len(raw) > MaxValueLen*4 {
		return nil, bad(key, "value too long")
	}
	text := func(maxLen int) (string, error) {
		s, ok := asString(raw)
		if !ok {
			return "", bad(key, "text expected")
		}
		if maxLen == 0 {
			maxLen = MaxValueLen
		}
		if utf8.RuneCountInString(s) > maxLen {
			return "", bad(key, "at most %d characters", maxLen)
		}
		if strings.ContainsAny(s, "\x00") {
			return "", bad(key, "invalid character")
		}
		return s, nil
	}
	str := func(s string) *Typed { return &Typed{Raw: mustJSON(s), Txt: ptr(s)} }
	num := func(f float64) (*Typed, error) {
		if o.Min != nil && f < *o.Min {
			return nil, bad(key, "at least %v", *o.Min)
		}
		if o.Max != nil && f > *o.Max {
			return nil, bad(key, "at most %v", *o.Max)
		}
		if math.Abs(f) > 1e15 {
			return nil, bad(key, "number too large")
		}
		return &Typed{Raw: mustJSON(f), Num: ptr(f)}, nil
	}
	switch t.ID {
	case "text", "textarea", "code":
		maxLen := o.MaxLen
		if t.ID == "code" && maxLen == 0 {
			maxLen = 100
		}
		s, err := text(maxLen)
		if err != nil || s == "" {
			return nil, err
		}
		if t.ID != "textarea" && strings.ContainsAny(s, "\r\n") {
			return nil, bad(key, "one line only")
		}
		if o.Pattern != "" {
			re, err := regexp.Compile("^(?:" + o.Pattern + ")$")
			if err != nil || !re.MatchString(s) {
				return nil, bad(key, "does not match the required format")
			}
		}
		return str(s), nil
	case "email":
		s, err := text(254)
		if err != nil || s == "" {
			return nil, err
		}
		a, err := mail.ParseAddress(s)
		if err != nil || a.Address != s {
			return nil, bad(key, "not an e-mail address")
		}
		return str(strings.ToLower(s)), nil
	case "phone":
		s, err := text(30)
		if err != nil || s == "" {
			return nil, err
		}
		if !phoneRe.MatchString(s) {
			return nil, bad(key, "not a phone number")
		}
		return str(s), nil
	case "url":
		s, err := text(2000)
		if err != nil || s == "" {
			return nil, err
		}
		u, err := url.Parse(s)
		if err != nil || !((u.Scheme == "https" || u.Scheme == "http") && u.Host != "" || u.Scheme == "mailto" || (u.Scheme == "" && strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//"))) {
			return nil, bad(key, "use an http(s) address, mailto: or a path")
		}
		return str(s), nil
	case "color":
		s, err := text(7)
		if err != nil || s == "" {
			return nil, err
		}
		if !colorRe.MatchString(s) {
			return nil, bad(key, "colour as #rrggbb")
		}
		return str(strings.ToLower(s)), nil
	case "language":
		s, err := text(20)
		if err != nil || s == "" {
			return nil, err
		}
		if !langRe.MatchString(s) {
			return nil, bad(key, "language code such as de or en-GB")
		}
		return str(s), nil
	case "country":
		s, err := text(2)
		if err != nil || s == "" {
			return nil, err
		}
		s = strings.ToUpper(s)
		if !countryRe.MatchString(s) {
			return nil, bad(key, "country code such as AT")
		}
		return str(s), nil
	case "integer":
		f, ok := asNumber(raw)
		if !ok {
			return nil, bad(key, "whole number expected")
		}
		if f != math.Trunc(f) {
			return nil, bad(key, "whole number expected")
		}
		return num(f)
	case "decimal", "percent", "currency", "unit":
		f, ok := asNumber(raw)
		if !ok {
			return nil, bad(key, "number expected")
		}
		d := 6
		if o.Decimals != nil {
			d = *o.Decimals
		}
		if t.ID == "currency" {
			d = 2
		}
		p := math.Pow(10, float64(d))
		return num(math.Round(f*p) / p)
	case "rating":
		f, ok := asNumber(raw)
		mx := 5.0
		if o.Max != nil {
			mx = *o.Max
		}
		if !ok || f != math.Trunc(f) || f < 0 || f > mx {
			return nil, bad(key, "rating from 0 to %v", mx)
		}
		return &Typed{Raw: mustJSON(f), Num: ptr(f)}, nil
	case "progress":
		f, ok := asNumber(raw)
		if !ok || f < 0 || f > 100 {
			return nil, bad(key, "progress from 0 to 100")
		}
		f = math.Round(f)
		return &Typed{Raw: mustJSON(f), Num: ptr(f)}, nil
	case "duration":
		// seconds (number), or text such as "1h 30m", "90m", "2d"
		f, ok := asNumber(raw)
		if !ok {
			s, _ := asString(raw)
			d, err := ParseDuration(s)
			if err != nil {
				return nil, bad(key, "duration such as 1h 30m")
			}
			f = d
		}
		if f < 0 || f > 1e10 {
			return nil, bad(key, "duration out of range")
		}
		f = math.Round(f)
		return &Typed{Raw: mustJSON(f), Num: ptr(f)}, nil
	case "boolean":
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return nil, bad(key, "yes or no expected")
		}
		f := 0.0
		if b {
			f = 1
		}
		return &Typed{Raw: mustJSON(b), Num: ptr(f)}, nil
	case "date":
		s, err := text(10)
		if err != nil || s == "" {
			return nil, err
		}
		d, err := time.Parse("2006-01-02", s)
		if err != nil || d.Year() < 1 || d.Year() > 9999 {
			return nil, bad(key, "date as YYYY-MM-DD")
		}
		return &Typed{Raw: mustJSON(s), TS: ptr(d)}, nil
	case "datetime":
		s, err := text(40)
		if err != nil || s == "" {
			return nil, err
		}
		d, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, bad(key, "date and time (RFC 3339)")
		}
		d = d.UTC()
		return &Typed{Raw: mustJSON(d.Format(time.RFC3339)), TS: ptr(d)}, nil
	case "time":
		s, err := text(5)
		if err != nil || s == "" {
			return nil, err
		}
		if !timeRe.MatchString(s) {
			return nil, bad(key, "time as HH:MM")
		}
		return str(s), nil
	case "daterange":
		var r DateRange
		if json.Unmarshal(raw, &r) != nil {
			return nil, bad(key, "date range {from, to}")
		}
		if r.From == "" && r.To == "" {
			return nil, nil
		}
		f, err1 := time.Parse("2006-01-02", r.From)
		to, err2 := time.Parse("2006-01-02", r.To)
		if err1 != nil || err2 != nil || to.Before(f) {
			return nil, bad(key, "date range: from and to as YYYY-MM-DD, to not before from")
		}
		return &Typed{Raw: mustJSON(r), TS: ptr(f), TSEnd: ptr(to)}, nil
	case "select", "status":
		s, err := text(100)
		if err != nil || s == "" {
			return nil, err
		}
		if len(o.Choices) > 0 {
			i := slices.IndexFunc(o.Choices, func(c Choice) bool { return strings.EqualFold(c.Value, s) })
			if i < 0 {
				return nil, bad(key, "%q is not one of the choices", s)
			}
			s = o.Choices[i].Value
		}
		return str(s), nil
	case "multiselect":
		var list []string
		if json.Unmarshal(raw, &list) != nil {
			return nil, bad(key, "list of choices expected")
		}
		if len(list) > 100 {
			return nil, bad(key, "at most 100 choices")
		}
		out := []string{}
		for _, s := range list {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if utf8.RuneCountInString(s) > 100 {
				return nil, bad(key, "choice too long")
			}
			if len(o.Choices) > 0 {
				i := slices.IndexFunc(o.Choices, func(c Choice) bool { return strings.EqualFold(c.Value, s) })
				if i < 0 {
					return nil, bad(key, "%q is not one of the choices", s)
				}
				s = o.Choices[i].Value
			}
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil, nil
		}
		return &Typed{Raw: mustJSON(out), Arr: out}, nil
	case "user", "group", "element", "category":
		ids, ok := asIDs(raw)
		if !ok {
			return nil, bad(key, "ids expected")
		}
		out := []int64{}
		for _, id := range ids {
			if id <= 0 {
				return nil, bad(key, "invalid id")
			}
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
		if len(out) == 0 {
			return nil, nil
		}
		if (!o.Multiple || t.ID == "category") && len(out) > 1 {
			return nil, bad(key, "only one value")
		}
		if len(out) > 100 {
			return nil, bad(key, "at most 100 values")
		}
		return &Typed{Raw: mustJSON(out), Refs: out}, nil
	case "media":
		s, err := text(250)
		if err != nil || s == "" {
			return nil, err
		}
		if !mediaRe.MatchString(s) || strings.Contains(s, "..") {
			return nil, bad(key, "a file of the wiki (/api/v1/files/…)")
		}
		return str(s), nil
	case "location":
		var l Location
		if json.Unmarshal(raw, &l) != nil {
			return nil, bad(key, "location {address, lat, lng}")
		}
		l.Address = strings.TrimSpace(l.Address)
		if utf8.RuneCountInString(l.Address) > 300 {
			return nil, bad(key, "address too long")
		}
		if (l.Lat == nil) != (l.Lng == nil) || l.Lat != nil && (*l.Lat < -90 || *l.Lat > 90 || *l.Lng < -180 || *l.Lng > 180) {
			return nil, bad(key, "invalid coordinates")
		}
		if l.Address == "" && l.Lat == nil {
			return nil, nil
		}
		return &Typed{Raw: mustJSON(l), Txt: ptr(l.Address)}, nil
	case "version":
		s, err := text(60)
		if err != nil || s == "" {
			return nil, err
		}
		m := versionRe.FindStringSubmatch(s)
		if m == nil {
			return nil, bad(key, "version such as 1.4.2")
		}
		// sortable as number: major, minor, patch
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		c, _ := strconv.Atoi(m[3])
		f := float64(a)*1e12 + float64(b)*1e6 + float64(c)
		return &Typed{Raw: mustJSON(s), Txt: ptr(s), Num: ptr(f)}, nil
	case "autonumber", "formula":
		return nil, bad(key, "is computed")
	}
	return nil, bad(key, "unknown type %q", t.ID)
}

var durRe = regexp.MustCompile(`(?i)^\s*(?:(\d+)\s*d)?\s*(?:(\d+)\s*h)?\s*(?:(\d+)\s*m(?:in)?)?\s*(?:(\d+)\s*s)?\s*$`)

// ParseDuration reads "2d 3h 30m 10s", "90m", "1:30" (h:mm) as seconds.
func ParseDuration(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if h, m, ok := strings.Cut(s, ":"); ok {
		hh, e1 := strconv.Atoi(h)
		mm, e2 := strconv.Atoi(m)
		if e1 != nil || e2 != nil || hh < 0 || mm < 0 || mm > 59 {
			return 0, fmt.Errorf("invalid")
		}
		return float64(hh*3600 + mm*60), nil
	}
	m := durRe.FindStringSubmatch(s)
	if m == nil || s == "" {
		return 0, fmt.Errorf("invalid")
	}
	mul := []float64{86400, 3600, 60, 1}
	total := 0.0
	for i, x := range m[1:] {
		if x != "" {
			n, _ := strconv.Atoi(x)
			total += float64(n) * mul[i]
		}
	}
	return total, nil
}
