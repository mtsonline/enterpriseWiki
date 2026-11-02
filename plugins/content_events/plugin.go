// Package content_events provides the "Event calendar" element type: events
// with month, week and list views, recurring series, categories, registration
// with capacity and waiting list, e-mail confirmations and reminders through
// the mail queue, iCal export/subscription and the "events" macro.
//
// Permissions follow the category of the calendar element: View shows the
// calendar, Create adds events (and edits one's own), Edit edits all events
// and sees participants.
package content_events

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // time zones without system tzdata
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
	"github.com/mtsonline/enterprisewiki/plugins/core_html"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// TypeID is the element type of calendars.
const TypeID = "content.events"

// Plugin is the event calendar plugin.
type Plugin struct{ host *sdk.Host }

var plugin = &Plugin{}

func init() { sdk.Register(plugin) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// ---------------------------------------------------------------- calendar settings

// Category is an event category with a colour.
type Category struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Color string `json:"color"`
}

// Config is the body of a calendar element.
type Config struct {
	Intro      core_html.Body `json:"intro"`
	View       string         `json:"view"`       // month | week | list
	WeekStart  int            `json:"week_start"` // 1 = Monday, 0 = Sunday
	Categories []Category     `json:"categories"`
	ICal       bool           `json:"ical"`      // offer iCal subscription
	ListDays   int            `json:"list_days"` // range of the list view
}

var (
	colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	catIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,30}$`)
)

func decodeConfig(raw []byte) (*Config, error) {
	c := &Config{}
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, apperr.BadRequest("invalid calendar settings: %v", err)
	}
	if c.View != "week" && c.View != "list" {
		c.View = "month"
	}
	if c.WeekStart != 0 || !strings.Contains(string(raw), `"week_start"`) {
		c.WeekStart = 1 // Monday unless Sunday (0) is chosen explicitly
	}
	if c.ListDays <= 0 || c.ListDays > 730 {
		c.ListDays = 90
	}
	if c.Intro.Format == "" {
		c.Intro.Format = "markdown"
	}
	raw2, _ := json.Marshal(c.Intro)
	clean, err := (&core_html.Type{}).Normalize(raw2)
	if err != nil {
		return nil, apperr.BadRequest("intro: %v", err)
	}
	_ = json.Unmarshal(clean, &c.Intro)
	seen := map[string]bool{}
	if len(c.Categories) > 30 {
		return nil, apperr.BadRequest("at most 30 categories")
	}
	for i := range c.Categories {
		k := &c.Categories[i]
		k.Label = strings.TrimSpace(k.Label)
		if k.ID == "" {
			k.ID = slug(k.Label)
		}
		if !catIDRe.MatchString(k.ID) || seen[k.ID] || k.Label == "" || utf8.RuneCountInString(k.Label) > 60 {
			return nil, apperr.BadRequest("category %d: a unique id (a-z, 0-9, -) and a label are required", i+1)
		}
		if !colorRe.MatchString(k.Color) {
			k.Color = "#0c66e4"
		}
		seen[k.ID] = true
	}
	if c.Categories == nil {
		c.Categories = []Category{}
	}
	return c, nil
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == 'ä':
			b.WriteString("ae")
		case r == 'ö':
			b.WriteString("oe")
		case r == 'ü':
			b.WriteString("ue")
		case r == 'ß':
			b.WriteString("ss")
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 30 {
		out = out[:30]
	}
	if out == "" {
		out = "c"
	}
	return out
}

// ---------------------------------------------------------------- events

// Event is a single event or a series.
type Event struct {
	ID          int64     `json:"id"`
	CalendarID  int64     `json:"calendar_id"`
	UID         string    `json:"uid"`
	Sequence    int       `json:"sequence"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	URL         string    `json:"url"`
	Category    string    `json:"category"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	AllDay      bool      `json:"all_day"`
	TZ          string    `json:"tz"`
	RRule       string    `json:"rrule"`
	ExDates     []string  `json:"exdates"`
	// Overrides changes single occurrences (key: original local date).
	Overrides    map[string]Override `json:"overrides"`
	Registration bool                `json:"registration"`
	Capacity     int                 `json:"capacity"`
	Waitlist     bool                `json:"waitlist"`
	RegDeadline  int                 `json:"reg_deadline"`
	Reminders    []int               `json:"reminders"`
	CreatedBy    *int64              `json:"created_by"`
	Author       string              `json:"author"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
	// computed for the caller
	DescriptionHTML string `json:"description_html"`
	CanEdit         bool   `json:"can_edit"`
	CanManage       bool   `json:"can_manage"` // sees participants
}

const eventCols = `e.id, e.calendar_id, e.uid, e.sequence, e.title, e.description, e.location, e.url, e.category, e.starts_at, e.ends_at,
	e.all_day, e.tz, e.rrule, e.exdates::text[], e.overrides, e.registration, e.capacity, e.waitlist, e.reg_deadline, e.reminders, e.created_by,
	COALESCE(NULLIF(u.display_name, ''), u.username, ''), e.created_at, e.updated_at`

const eventFrom = ` FROM plugin_events e LEFT JOIN users u ON u.id = e.created_by`

func scanEvent(r pgx.CollectableRow) (*Event, error) {
	var e Event
	err := r.Scan(&e.ID, &e.CalendarID, &e.UID, &e.Sequence, &e.Title, &e.Description, &e.Location, &e.URL, &e.Category, &e.StartsAt, &e.EndsAt,
		&e.AllDay, &e.TZ, &e.RRule, &e.ExDates, &e.Overrides, &e.Registration, &e.Capacity, &e.Waitlist, &e.RegDeadline, &e.Reminders, &e.CreatedBy,
		&e.Author, &e.CreatedAt, &e.UpdatedAt)
	if e.ExDates == nil {
		e.ExDates = []string{}
	}
	if e.Reminders == nil {
		e.Reminders = []int{}
	}
	if e.Overrides == nil {
		e.Overrides = map[string]Override{}
	}
	return &e, err
}

// Loc returns the time zone of the event.
func (e *Event) Loc() *time.Location {
	if l, err := time.LoadLocation(e.TZ); err == nil {
		return l
	}
	return time.UTC
}

// Rule returns the parsed recurrence (nil for single events).
func (e *Event) Rule() *Rule {
	r, _ := ParseRule(e.RRule)
	return r
}

func (e *Event) exdateSet() map[string]bool {
	m := map[string]bool{}
	for _, d := range e.ExDates {
		m[d] = true
	}
	return m
}

// Override changes one occurrence of a series. Empty fields keep the value
// of the series; StartsAt and EndsAt are set together.
type Override struct {
	Title       string     `json:"title,omitempty"`
	Description string     `json:"description,omitempty"`
	Location    string     `json:"location,omitempty"`
	StartsAt    *time.Time `json:"starts_at,omitempty"`
	EndsAt      *time.Time `json:"ends_at,omitempty"`
}

// MaxShift is how far a single occurrence may be moved.
const MaxShift = 31 * 24 * time.Hour

// Instance is one occurrence with its changes applied.
type Instance struct {
	Date     string    // original local date (key for registrations and overrides)
	Original time.Time // start according to the rule
	Start    time.Time
	End      time.Time
	Override *Override
}

// Title, Location and Description of an instance.
func (e *Event) instTitle(in Instance) string {
	if in.Override != nil && in.Override.Title != "" {
		return in.Override.Title
	}
	return e.Title
}

func (e *Event) instLocation(in Instance) string {
	if in.Override != nil && in.Override.Location != "" {
		return in.Override.Location
	}
	return e.Location
}

// Instances returns the occurrences overlapping [from, to) with their
// changes applied (moved occurrences are found up to MaxShift away).
func (e *Event) Instances(from, to time.Time) []Instance {
	loc := e.Loc()
	dur := e.EndsAt.Sub(e.StartsAt)
	pad := time.Duration(0)
	if len(e.Overrides) > 0 {
		pad = MaxShift
	}
	var out []Instance
	for _, s := range e.Rule().Expand(e.StartsAt, dur, loc, from.Add(-pad), to.Add(pad), e.exdateSet()) {
		in := Instance{Date: s.In(loc).Format("2006-01-02"), Original: s, Start: s, End: s.Add(dur)}
		if ov, ok := e.Overrides[in.Date]; ok {
			o := ov
			in.Override = &o
			if o.StartsAt != nil && o.EndsAt != nil {
				in.Start, in.End = *o.StartsAt, *o.EndsAt
			}
		}
		if in.Start.Before(to) && (in.End.After(from) || (dur == 0 && !in.Start.Before(from))) {
			out = append(out, in)
		}
	}
	return out
}

// Occurrences returns the starts of occurrences overlapping [from, to).
func (e *Event) Occurrences(from, to time.Time) []time.Time {
	var out []time.Time
	for _, in := range e.Instances(from, to) {
		out = append(out, in.Start)
	}
	return out
}

// InstanceAt returns the occurrence with the original local date.
func (e *Event) InstanceAt(date string) (Instance, bool) {
	loc := e.Loc()
	d, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return Instance{}, false
	}
	dur := e.EndsAt.Sub(e.StartsAt)
	for _, t := range e.Rule().Expand(e.StartsAt, 0, loc, d.Add(-time.Second), d.AddDate(0, 0, 1), e.exdateSet()) {
		if t.Format("2006-01-02") == date {
			in := Instance{Date: date, Original: t, Start: t, End: t.Add(dur)}
			if ov, ok := e.Overrides[date]; ok {
				o := ov
				in.Override = &o
				if o.StartsAt != nil && o.EndsAt != nil {
					in.Start, in.End = *o.StartsAt, *o.EndsAt
				}
			}
			return in, true
		}
	}
	return Instance{}, false
}

// OccurrenceAt returns the (possibly moved) start of the occurrence on a local date.
func (e *Event) OccurrenceAt(date string) (time.Time, bool) {
	in, ok := e.InstanceAt(date)
	return in.Start, ok
}

// instanceByStart finds the occurrence starting at start (for e-mails).
func (e *Event) instanceByStart(start time.Time) Instance {
	for _, in := range e.Instances(start.Add(-time.Second), start.Add(time.Second)) {
		if in.Start.Equal(start) {
			return in
		}
	}
	return Instance{Date: start.In(e.Loc()).Format("2006-01-02"), Original: start, Start: start, End: start.Add(e.EndsAt.Sub(e.StartsAt))}
}

// Input creates or changes an event.
type Input struct {
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Location     string    `json:"location"`
	URL          string    `json:"url"`
	Category     string    `json:"category"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	AllDay       bool      `json:"all_day"`
	TZ           string    `json:"tz"`
	RRule        string    `json:"rrule"`
	ExDates      []string  `json:"exdates"`
	Registration bool      `json:"registration"`
	Capacity     int       `json:"capacity"`
	Waitlist     *bool     `json:"waitlist"`
	RegDeadline  int       `json:"reg_deadline"`
	Reminders    []int     `json:"reminders"`
}

func (in *Input) check(cfg *Config) error {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || utf8.RuneCountInString(in.Title) > 200 {
		return apperr.BadRequest("a title (max. 200 characters) is required")
	}
	if len(in.Description) > 50000 || utf8.RuneCountInString(in.Location) > 300 {
		return apperr.BadRequest("description or location too long")
	}
	if in.URL != "" {
		u, err := url.Parse(in.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return apperr.BadRequest("the link must be an http(s) URL")
		}
	}
	if in.TZ == "" {
		in.TZ = "UTC"
	}
	loc, err := time.LoadLocation(in.TZ)
	if err != nil {
		return apperr.BadRequest("unknown time zone %q", in.TZ)
	}
	if in.StartsAt.IsZero() {
		return apperr.BadRequest("starts_at is required")
	}
	if in.EndsAt.IsZero() {
		in.EndsAt = in.StartsAt.Add(time.Hour)
	}
	if in.AllDay {
		// whole local days; the end is exclusive (midnight after the last day)
		s := in.StartsAt.In(loc)
		e := in.EndsAt.In(loc)
		in.StartsAt = time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, loc)
		end := time.Date(e.Year(), e.Month(), e.Day(), 0, 0, 0, 0, loc)
		if !end.After(in.StartsAt) {
			end = in.StartsAt.AddDate(0, 0, 1)
		}
		in.EndsAt = end
	}
	if in.EndsAt.Before(in.StartsAt) {
		return apperr.BadRequest("the end must not be before the start")
	}
	if in.EndsAt.Sub(in.StartsAt) > 366*24*time.Hour {
		return apperr.BadRequest("an event may last at most one year")
	}
	r, err := ParseRule(in.RRule)
	if err != nil {
		return apperr.BadRequest("recurrence: %v", err)
	}
	in.RRule = r.String()
	clean := []string{}
	for _, d := range in.ExDates {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return apperr.BadRequest("invalid excluded date %q", d)
		}
		clean = append(clean, d)
	}
	sort.Strings(clean)
	in.ExDates = clean
	if in.Category != "" {
		ok := false
		for _, c := range cfg.Categories {
			ok = ok || c.ID == in.Category
		}
		if !ok {
			return apperr.BadRequest("unknown category %q", in.Category)
		}
	}
	if in.Capacity < 0 || in.Capacity > 100000 || in.RegDeadline < 0 || in.RegDeadline > 60*24*90 {
		return apperr.BadRequest("invalid capacity or registration deadline")
	}
	if len(in.Reminders) > 5 {
		return apperr.BadRequest("at most 5 reminders")
	}
	rem := []int{}
	seen := map[int]bool{}
	for _, m := range in.Reminders {
		if m <= 0 || m > 60*24*28 {
			return apperr.BadRequest("reminders must be between 1 minute and 4 weeks before the start")
		}
		if !seen[m] {
			rem = append(rem, m)
			seen[m] = true
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(rem)))
	in.Reminders = rem
	if in.Waitlist == nil {
		t := true
		in.Waitlist = &t
	}
	return nil
}

func newUID(host string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	if host == "" {
		host = "ewiki"
	}
	return hex.EncodeToString(b) + "@" + host
}

// ---------------------------------------------------------------- permissions

type calendar struct {
	ID       int64
	Category int64
	Title    string
	Config   *Config
	Perms    acl.Perm
}

// loadCalendar reads a calendar element for the caller (View required).
func (p *Plugin) loadCalendar(ctx context.Context, id int64, lang string) (*calendar, error) {
	typ, body, _, cat, err := p.host.Content.ElementBody(ctx, id, lang)
	if err != nil {
		return nil, err
	}
	if typ != TypeID {
		return nil, apperr.NotFound("calendar")
	}
	cfg, err := decodeConfig(body)
	if err != nil {
		return nil, err
	}
	perms, err := p.host.ACL.Perms(ctx, identity.From(ctx), cat)
	if err != nil {
		return nil, err
	}
	var titles map[string]string
	_ = p.host.DB.QueryRow(ctx, `SELECT titles FROM elements WHERE id=$1`, id).Scan(&titles)
	return &calendar{ID: id, Category: cat, Title: pickText(titles, lang), Config: cfg, Perms: perms}, nil
}

func pickText(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	for _, v := range m {
		if v != "" {
			return v
		}
	}
	return ""
}

func (c *calendar) canCreate() bool { return c.Perms&acl.Create != 0 && c.Perms&acl.View != 0 }

// decorate fills computed fields of an event for the caller.
func (c *calendar) decorate(ctx context.Context, e *Event) {
	uid := identity.From(ctx).ID()
	e.CanManage = c.Perms&acl.Edit != 0 || (uid != 0 && e.CreatedBy != nil && *e.CreatedBy == uid && c.canCreate())
	e.CanEdit = e.CanManage
	if strings.TrimSpace(e.Description) != "" {
		e.DescriptionHTML = core_html.ToHTML(&core_html.Body{Format: "markdown", Source: e.Description})
	}
}

// event loads an event with its calendar (View required).
func (p *Plugin) event(ctx context.Context, id int64, lang string) (*Event, *calendar, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT `+eventCols+eventFrom+` WHERE e.id=$1`, id)
	if err != nil {
		return nil, nil, err
	}
	e, err := pgx.CollectExactlyOneRow(rows, scanEvent)
	if err != nil {
		return nil, nil, apperr.NotFound("event")
	}
	cal, err := p.loadCalendar(ctx, e.CalendarID, lang)
	if err != nil {
		if errors.Is(err, apperr.ErrUnauthorized) {
			return nil, nil, err
		}
		return nil, nil, apperr.NotFound("event")
	}
	cal.decorate(ctx, e)
	return e, cal, nil
}

// ---------------------------------------------------------------- occurrences

// Occurrence is one date of an event as shown in a calendar.
type Occurrence struct {
	EventID    int64     `json:"event_id"`
	Date       string    `json:"date"` // local date of the start (key for registrations)
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Registered int       `json:"registered"`
	Waiting    int       `json:"waiting"`
	MyStatus   string    `json:"my_status,omitempty"` // registered | waitlist | interested
	Full       bool      `json:"full"`
	RegOpen    bool      `json:"registration_open"`
	// changed single occurrence (empty fields: as the series)
	Changed         bool       `json:"changed,omitempty"`
	Title           string     `json:"title,omitempty"`
	Location        string     `json:"location,omitempty"`
	DescriptionHTML string     `json:"description_html,omitempty"`
	OriginalStart   *time.Time `json:"original_start,omitempty"` // when moved
}

func (e *Event) regOpen(start, now time.Time) bool {
	return e.Registration && now.Before(start.Add(-time.Duration(e.RegDeadline)*time.Minute))
}

// occurrences expands events in [from, to) and adds registration counts.
func (p *Plugin) occurrences(ctx context.Context, events []*Event, from, to time.Time) ([]Occurrence, error) {
	var out []Occurrence
	ids := []int64{}
	for _, e := range events {
		for _, in := range e.Instances(from, to) {
			o := Occurrence{EventID: e.ID, Date: in.Date, Start: in.Start, End: in.End}
			if in.Override != nil {
				o.Changed = true
				o.Title, o.Location = in.Override.Title, in.Override.Location
				if in.Override.Description != "" {
					o.DescriptionHTML = core_html.ToHTML(&core_html.Body{Format: "markdown", Source: in.Override.Description})
				}
				if in.Override.StartsAt != nil {
					o.OriginalStart = &in.Original
				}
			}
			out = append(out, o)
			if len(out) > 5000 {
				return nil, apperr.BadRequest("too many events in this range, choose a shorter one")
			}
		}
		ids = append(ids, e.ID)
	}
	if len(out) == 0 {
		return []Occurrence{}, nil
	}
	type key struct {
		id   int64
		date string
	}
	counts := map[key][2]int{}
	mine := map[key]string{}
	uid := identity.From(ctx).ID()
	rows, err := p.host.DB.Query(ctx, `SELECT event_id, occurrence::text, status, count(*), bool_or(user_id = $2) FROM plugin_event_registrations
		WHERE event_id = ANY($1) AND status IN ('registered', 'waitlist', 'interested') GROUP BY event_id, occurrence, status`, ids, uid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k key
		var st string
		var n int
		var me bool
		if err := rows.Scan(&k.id, &k.date, &st, &n, &me); err != nil {
			rows.Close()
			return nil, err
		}
		c := counts[k]
		switch st {
		case "registered":
			c[0] = n
		case "waitlist":
			c[1] = n
		}
		counts[k] = c
		if me {
			mine[k] = st
		}
	}
	rows.Close()
	byID := map[int64]*Event{}
	for _, e := range events {
		byID[e.ID] = e
	}
	now := time.Now()
	for i := range out {
		o := &out[i]
		k := key{o.EventID, o.Date}
		o.Registered, o.Waiting, o.MyStatus = counts[k][0], counts[k][1], mine[k]
		e := byID[o.EventID]
		o.Full = e.Capacity > 0 && o.Registered >= e.Capacity
		o.RegOpen = e.regOpen(o.Start, now)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Start.Before(out[b].Start) })
	return out, nil
}

// eventsOf loads the events of calendars that may overlap [from, to).
func (p *Plugin) eventsOf(ctx context.Context, calendars []int64, from, to time.Time, category, q string) ([]*Event, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT `+eventCols+eventFrom+` WHERE e.calendar_id = ANY($1)
		AND e.starts_at < $3 AND (e.rrule <> '' OR e.ends_at > $2) AND ($4 = '' OR e.category = $4)
		AND ($5 = '' OR e.title ILIKE '%' || $5 || '%' OR e.description ILIKE '%' || $5 || '%' OR e.location ILIKE '%' || $5 || '%')
		ORDER BY e.starts_at LIMIT 5000`, calendars, from, to, category, strings.ReplaceAll(strings.ReplaceAll(q, "%", ""), "_", ""))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanEvent)
}

// visibleCalendars returns calendar elements the caller may view (all or the given ones).
func (p *Plugin) visibleCalendars(ctx context.Context, only []int64) (map[int64]string, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT e.id, e.category_id, e.titles FROM elements e JOIN categories c ON c.id = e.category_id
		WHERE e.type = $1 AND e.deleted_at IS NULL AND c.deleted_at IS NULL AND (cardinality($2::bigint[]) = 0 OR e.id = ANY($2))`, TypeID, only)
	if err != nil {
		return nil, err
	}
	type row struct {
		id, cat int64
		titles  map[string]string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		return x, r.Scan(&x.id, &x.cat, &x.titles)
	})
	if err != nil {
		return nil, err
	}
	cats := make([]int64, 0, len(list))
	for _, x := range list {
		cats = append(cats, x.cat)
	}
	perms, err := p.host.ACL.PermsMap(ctx, identity.From(ctx), cats)
	if err != nil {
		return nil, err
	}
	out := map[int64]string{}
	for _, x := range list {
		if perms[x.cat]&acl.View != 0 {
			out[x.id] = pickText(x.titles, "")
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- element type

// Type implements content.ElementType for calendars.
type Type struct{}

// TypeID implements content.ElementType.
func (Type) TypeID() string { return TypeID }

// Labels implements content.ElementType.
func (Type) Labels() map[string]string {
	return map[string]string{"en": "Event calendar", "de": "Veranstaltungskalender"}
}

// EmptyBody implements content.ElementType.
func (Type) EmptyBody() json.RawMessage {
	return json.RawMessage(`{"intro":{"format":"markdown","source":""},"view":"month","week_start":1,"categories":[],"ical":true,"list_days":90}`)
}

// Normalize implements content.ElementType.
func (Type) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	c, err := decodeConfig(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

// Text implements content.ElementType.
func (Type) Text(raw json.RawMessage) string {
	var c Config
	_ = json.Unmarshal(raw, &c)
	return c.Intro.Source
}

// Render implements content.ElementType: intro and the upcoming events as a
// list (without JavaScript); the "events" widget shows the full calendar.
func (Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	c, err := decodeConfig(raw)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewe" data-ew-widget="events" data-element="%d" data-view="%s">`, rc.ElementID, c.View)
	if strings.TrimSpace(c.Intro.Source) != "" {
		b.WriteString(`<div class="ewe-intro">` + core_html.ToHTML(&c.Intro) + `</div>`)
	}
	list, err := plugin.upcoming(ctx, []int64{rc.ElementID}, time.Now(), c.ListDays, 30)
	if err != nil {
		return "", err
	}
	b.WriteString(`<div class="ewe-static">`)
	b.WriteString(renderUpcoming(list, rc.Lang, false))
	b.WriteString(`</div></div>`)
	return b.String(), nil
}

type upcomingItem struct {
	Event *Event
	Occ   time.Time
	End   time.Time
	Cal   string
	Date  string // original local date
}

func (p *Plugin) upcoming(ctx context.Context, calendars []int64, from time.Time, days, limit int) ([]upcomingItem, error) {
	vis, err := p.visibleCalendars(ctx, calendars)
	if err != nil || len(vis) == 0 {
		return nil, err
	}
	ids := make([]int64, 0, len(vis))
	for id := range vis {
		ids = append(ids, id)
	}
	to := from.AddDate(0, 0, days)
	events, err := p.eventsOf(ctx, ids, from, to, "", "")
	if err != nil {
		return nil, err
	}
	var out []upcomingItem
	for _, e := range events {
		for _, in := range e.Instances(from, to) {
			ev := e
			if in.Override != nil {
				c := *e
				c.Title, c.Location = e.instTitle(in), e.instLocation(in)
				ev = &c
			}
			out = append(out, upcomingItem{Event: ev, Occ: in.Start, End: in.End, Cal: vis[e.CalendarID], Date: in.Date})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Occ.Before(out[b].Occ) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

var weekdays = map[string][]string{
	"de": {"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"},
	"en": {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
}

// FormatWhen formats an occurrence in the event's time zone.
func FormatWhen(start, end time.Time, allDay bool, loc *time.Location, lang string) string {
	s, e := start.In(loc), end.In(loc)
	wd := weekdays["en"]
	if w, ok := weekdays[lang]; ok {
		wd = w
	}
	date := func(t time.Time) string {
		if lang == "de" {
			return fmt.Sprintf("%s, %s", wd[t.Weekday()], t.Format("02.01.2006"))
		}
		return fmt.Sprintf("%s, %s", wd[t.Weekday()], t.Format("Jan 2, 2006"))
	}
	clock := func(t time.Time) string {
		if lang == "de" {
			return t.Format("15:04")
		}
		return t.Format("3:04 PM")
	}
	if allDay {
		last := e.AddDate(0, 0, -1)
		if last.Format("2006-01-02") == s.Format("2006-01-02") {
			return date(s)
		}
		return date(s) + " – " + date(last)
	}
	if s.Format("2006-01-02") == e.Format("2006-01-02") {
		return date(s) + ", " + clock(s) + "–" + clock(e)
	}
	return date(s) + ", " + clock(s) + " – " + date(e) + ", " + clock(e)
}

var staticMsgs = map[string]map[string]string{
	"en": {"none": "No upcoming events."},
	"de": {"none": "Keine bevorstehenden Termine."},
}

func renderUpcoming(list []upcomingItem, lang string, withCal bool) string {
	esc := html.EscapeString
	m := staticMsgs["en"]
	if x, ok := staticMsgs[lang]; ok {
		m = x
	}
	if len(list) == 0 {
		return `<p class="ewe-none">` + esc(m["none"]) + `</p>`
	}
	var b strings.Builder
	b.WriteString(`<ul class="ewe-upcoming">`)
	for _, it := range list {
		e := it.Event
		date := it.Date
		fmt.Fprintf(&b, `<li class="ewe-up"><a href="/e/%d#event-%d-%s"><time datetime="%s">%s</time> <strong>%s</strong></a>`,
			e.CalendarID, e.ID, date, it.Occ.UTC().Format(time.RFC3339), esc(FormatWhen(it.Occ, it.End, e.AllDay, e.Loc(), lang)), esc(e.Title))
		if e.Location != "" {
			b.WriteString(` <span class="ewe-up-loc">` + esc(e.Location) + `</span>`)
		}
		if withCal && it.Cal != "" {
			b.WriteString(` <span class="ewe-up-cal">` + esc(it.Cal) + `</span>`)
		}
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul>`)
	return b.String()
}

// ---------------------------------------------------------------- macro

// UpcomingMacro lists upcoming events of one or all calendars in rich text.
type UpcomingMacro struct{}

// Name implements content.Macro.
func (UpcomingMacro) Name() string { return "events" }

// Labels implements content.Macro.
func (UpcomingMacro) Labels() map[string]string {
	return map[string]string{"en": "Upcoming events", "de": "Nächste Termine"}
}

// Description implements content.Macro.
func (UpcomingMacro) Description() map[string]string {
	return map[string]string{"en": "Lists the next events of a calendar (or of all calendars you can see).", "de": "Listet die nächsten Termine eines Kalenders (oder aller sichtbaren Kalender)."}
}

// HasBody implements content.Macro.
func (UpcomingMacro) HasBody() bool { return false }

// Params implements content.Macro.
func (UpcomingMacro) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "calendar", Labels: map[string]string{"en": "Calendar (element id, empty = all)", "de": "Kalender (Element-ID, leer = alle)"}, Type: "number"},
		{Name: "limit", Labels: map[string]string{"en": "Number of events", "de": "Anzahl Termine"}, Type: "number", Default: "5"},
		{Name: "days", Labels: map[string]string{"en": "Days ahead", "de": "Tage im Voraus"}, Type: "number", Default: "60"},
	}
}

// Render implements content.Macro.
func (UpcomingMacro) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	var cals []int64
	if v := mc.Params["calendar"]; v != "" {
		var id int64
		if _, err := fmt.Sscan(v, &id); err == nil && id > 0 {
			cals = []int64{id}
		}
	}
	limit, days := 5, 60
	_, _ = fmt.Sscan(mc.Params["limit"], &limit)
	_, _ = fmt.Sscan(mc.Params["days"], &days)
	if limit < 1 || limit > 50 {
		limit = 5
	}
	if days < 1 || days > 730 {
		days = 60
	}
	list, err := plugin.upcoming(ctx, cals, time.Now(), days, limit)
	if err != nil {
		return "", err
	}
	return `<div class="ewe-macro">` + renderUpcoming(list, mc.Lang, len(cals) == 0) + `</div>`, nil
}
