package content_events

import (
	"context"
	"hash/fnv"
	"html"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/feedfetch"
)

// External calendars (iCalendar subscriptions) shown read-only in a
// calendar: the VEVENTs of the document as events with negative ids, no
// registration, no editing. Supported: DTSTART/DTEND (UTC, TZID, floating,
// VALUE=DATE), DURATION, SUMMARY, LOCATION, DESCRIPTION, URL, UID, RRULE
// (as far as the own rule engine knows it), EXDATE. Everything is text
// (escaped when shown); at most 2000 events per calendar.

const maxExternalEvents = 2000

// unfold joins folded content lines (RFC 5545 3.1).
func unfold(doc string) []string {
	doc = strings.ReplaceAll(doc, "\r\n", "\n")
	var out []string
	for _, l := range strings.Split(doc, "\n") {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(out) > 0 {
			out[len(out)-1] += l[1:]
			continue
		}
		out = append(out, l)
	}
	return out
}

func icsUnescape(s string) string {
	return strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}

// prop splits "NAME;PARAM=x:VALUE" into name, params and value.
func prop(line string) (string, map[string]string, string) {
	head, val, ok := strings.Cut(line, ":")
	if !ok {
		return "", nil, ""
	}
	parts := strings.Split(head, ";")
	params := map[string]string{}
	for _, p := range parts[1:] {
		if k, v, ok := strings.Cut(p, "="); ok {
			params[strings.ToUpper(k)] = strings.Trim(v, `"`)
		}
	}
	return strings.ToUpper(parts[0]), params, val
}

func icsTime(val string, params map[string]string) (time.Time, bool, string, bool) {
	if params["VALUE"] == "DATE" || len(val) == 8 {
		t, err := time.ParseInLocation("20060102", val, time.UTC)
		return t, true, "", err == nil
	}
	if strings.HasSuffix(val, "Z") {
		t, err := time.Parse("20060102T150405Z", val)
		return t, false, "UTC", err == nil
	}
	loc := time.UTC
	tz := "UTC"
	if id := params["TZID"]; id != "" {
		if l, err := time.LoadLocation(id); err == nil {
			loc, tz = l, id
		}
	}
	t, err := time.ParseInLocation("20060102T150405", val, loc)
	return t, false, tz, err == nil
}

// icsDuration reads a DURATION such as P1D, PT1H30M, P1W.
func icsDuration(s string) time.Duration {
	s = strings.TrimPrefix(strings.ToUpper(s), "+")
	if !strings.HasPrefix(s, "P") {
		return 0
	}
	var d time.Duration
	n := 0
	inTime := false
	for _, r := range s[1:] {
		switch {
		case r >= '0' && r <= '9':
			n = n*10 + int(r-'0')
		case r == 'T':
			inTime = true
		case r == 'W':
			d += time.Duration(n) * 7 * 24 * time.Hour
			n = 0
		case r == 'D':
			d += time.Duration(n) * 24 * time.Hour
			n = 0
		case r == 'H' && inTime:
			d += time.Duration(n) * time.Hour
			n = 0
		case r == 'M' && inTime:
			d += time.Duration(n) * time.Minute
			n = 0
		case r == 'S' && inTime:
			d += time.Duration(n) * time.Second
			n = 0
		}
	}
	return d
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// externalID is a stable negative id for an event of a subscription.
func externalID(source, uid string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(source + "\x00" + uid))
	return -int64(h.Sum64()>>2) - 1
}

// ParseICS reads the events of an iCalendar document.
func ParseICS(doc, source string) []*Event {
	var out []*Event
	var cur *Event
	var dur time.Duration
	hasEnd := false
	for _, line := range unfold(doc) {
		name, params, val := prop(line)
		switch {
		case name == "BEGIN" && strings.EqualFold(val, "VEVENT"):
			cur, dur, hasEnd = &Event{TZ: "UTC", ExDates: []string{}, Reminders: []int{}, Overrides: map[string]Override{}}, 0, false
		case name == "END" && strings.EqualFold(val, "VEVENT") && cur != nil:
			if !cur.StartsAt.IsZero() && cur.Title != "" {
				if !hasEnd {
					switch {
					case dur > 0:
						cur.EndsAt = cur.StartsAt.Add(dur)
					case cur.AllDay:
						cur.EndsAt = cur.StartsAt.AddDate(0, 0, 1)
					default:
						cur.EndsAt = cur.StartsAt
					}
				}
				if cur.EndsAt.Before(cur.StartsAt) || cur.EndsAt.Sub(cur.StartsAt) > 366*24*time.Hour {
					cur.EndsAt = cur.StartsAt
				}
				cur.External, cur.Source = true, source
				if cur.RRule != "" {
					if _, err := ParseRule(cur.RRule); err != nil {
						cur.RRule = "" // a rule the engine does not know: the first occurrence only
					}
				}
				if cur.UID == "" {
					cur.UID = cur.Title + cur.StartsAt.String()
				}
				cur.ID = externalID(source, cur.UID)
				cur.URL = clip(cur.URL, 2000)
				if !strings.HasPrefix(cur.URL, "https://") && !strings.HasPrefix(cur.URL, "http://") {
					cur.URL = ""
				}
				out = append(out, cur)
				if len(out) >= maxExternalEvents {
					return out
				}
			}
			cur = nil
		case cur == nil:
		case name == "SUMMARY":
			cur.Title = clip(icsUnescape(val), 250)
		case name == "LOCATION":
			cur.Location = clip(icsUnescape(val), 250)
		case name == "DESCRIPTION":
			cur.Description = clip(icsUnescape(val), 4000)
		case name == "URL":
			cur.URL = strings.TrimSpace(val)
		case name == "UID":
			cur.UID = clip(val, 250)
		case name == "DTSTART":
			if t, allDay, tz, ok := icsTime(val, params); ok {
				cur.StartsAt, cur.AllDay, cur.TZ = t, allDay, tz
				if allDay {
					cur.TZ = "UTC"
				}
			}
		case name == "DTEND":
			if t, _, _, ok := icsTime(val, params); ok {
				cur.EndsAt, hasEnd = t, true
			}
		case name == "DURATION":
			dur = icsDuration(val)
		case name == "RRULE":
			cur.RRule = clip(val, 500)
		case name == "EXDATE":
			for _, v := range strings.Split(val, ",") {
				if t, _, _, ok := icsTime(v, params); ok {
					cur.ExDates = append(cur.ExDates, t.In(cur.Loc()).Format("2006-01-02"))
				}
			}
		}
	}
	return out
}

// eventSources are further sources of a calendar or the events macro.
type eventSources struct {
	Calendars []int64
	ICal      []string
}

func parseEventSources(s string) (*eventSources, error) {
	list, err := content.ParseSourcesKinds(s, []string{"calendar", "ical"})
	if err != nil {
		return nil, err
	}
	out := &eventSources{}
	for _, x := range list {
		switch x.Kind {
		case "calendar":
			id, err := strconv.ParseInt(strings.TrimSpace(x.Value), 10, 64)
			if err != nil || id <= 0 {
				return nil, apperr.BadRequest("calendar: element id of a calendar")
			}
			out.Calendars = append(out.Calendars, id)
		case "ical":
			if err := feedfetch.CheckURL(x.Value); err != nil {
				return nil, apperr.BadRequest("ical: %v", err)
			}
			out.ICal = append(out.ICal, x.Value)
		default:
			return nil, apperr.BadRequest("calendars take the sources calendar:ID and ical:https://…")
		}
	}
	if len(out.ICal) > 10 {
		return nil, apperr.BadRequest("at most 10 subscribed calendars")
	}
	return out, nil
}

// externalEvents are the events of subscribed calendars that may overlap
// [from, to) (from the cache; fetched in the background when new).
func (p *Plugin) externalEvents(ctx context.Context, urls []string, from, to time.Time) []*Event {
	var out []*Event
	for _, u := range urls {
		doc, _, err := p.host.Feeds.Calendar(ctx, u)
		if err != nil || doc == "" {
			continue
		}
		for _, e := range p.parsed(u, doc) {
			if e.StartsAt.Before(to) && (e.RRule != "" || !e.EndsAt.Before(from)) {
				out = append(out, e)
			}
		}
	}
	return out
}

// parsed caches the parse of a document (by address and content hash).
func (p *Plugin) parsed(u, doc string) []*Event {
	h := fnv.New64a()
	_, _ = h.Write([]byte(doc))
	key := u + "#" + strconv.FormatUint(h.Sum64(), 16)
	p.icsMu.Lock()
	defer p.icsMu.Unlock()
	if p.ics == nil || len(p.ics) > 50 {
		p.ics = map[string][]*Event{}
	}
	if v, ok := p.ics[key]; ok {
		return v
	}
	v := ParseICS(doc, u)
	p.ics[key] = v
	return v
}

// plainHTML shows text as escaped paragraphs (line breaks kept).
func plainHTML(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, para := range strings.Split(s, "\n\n") {
		if para = strings.TrimSpace(para); para != "" {
			b.WriteString("<p>" + strings.ReplaceAll(html.EscapeString(para), "\n", "<br>") + "</p>")
		}
	}
	return b.String()
}
