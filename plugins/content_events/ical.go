package content_events

import (
	"fmt"
	"github.com/mtsonline/enterprisewiki/internal/acl"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/feed"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

// icalEscape escapes TEXT values (RFC 5545 3.3.11).
func icalEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", "").Replace(s)
}

// fold writes a content line folded at 75 octets (RFC 5545 3.1).
func fold(b *strings.Builder, line string) {
	for len(line) > 75 {
		cut := 75
		for cut > 0 && (line[cut]&0xC0) == 0x80 { // do not split UTF-8 sequences
			cut--
		}
		b.WriteString(line[:cut] + "\r\n ")
		line = line[cut:]
	}
	b.WriteString(line + "\r\n")
}

// usesTZID reports whether the event is written in local time with TZID
// (series keep their wall time across daylight saving changes).
func usesTZID(e *Event) bool { return !e.AllDay && e.RRule != "" && e.TZ != "" && e.TZ != "UTC" }

// VEvent writes an event (a series with RRULE/EXDATE in its time zone).
func VEvent(b *strings.Builder, e *Event, link string, now time.Time) {
	utc := func(t time.Time) string { return t.UTC().Format("20060102T150405Z") }
	loc := e.Loc()
	fold(b, "BEGIN:VEVENT")
	fold(b, "UID:"+e.UID)
	fold(b, fmt.Sprintf("SEQUENCE:%d", e.Sequence))
	fold(b, "DTSTAMP:"+utc(now))
	fold(b, "LAST-MODIFIED:"+utc(e.UpdatedAt))
	fold(b, "CREATED:"+utc(e.CreatedAt))
	switch {
	case e.AllDay:
		fold(b, "DTSTART;VALUE=DATE:"+e.StartsAt.In(loc).Format("20060102"))
		fold(b, "DTEND;VALUE=DATE:"+e.EndsAt.In(loc).Format("20060102"))
	case usesTZID(e):
		// series keep their wall time across daylight saving changes
		fold(b, "DTSTART;TZID="+e.TZ+":"+e.StartsAt.In(loc).Format("20060102T150405"))
		fold(b, "DTEND;TZID="+e.TZ+":"+e.EndsAt.In(loc).Format("20060102T150405"))
	default:
		fold(b, "DTSTART:"+utc(e.StartsAt))
		fold(b, "DTEND:"+utc(e.EndsAt))
	}
	if e.RRule != "" {
		fold(b, "RRULE:"+e.RRule)
		for _, d := range e.ExDates {
			t, err := time.ParseInLocation("2006-01-02", d, loc)
			if err != nil {
				continue
			}
			if e.AllDay {
				fold(b, "EXDATE;VALUE=DATE:"+t.Format("20060102"))
			} else {
				h, m, s := e.StartsAt.In(loc).Clock()
				x := time.Date(t.Year(), t.Month(), t.Day(), h, m, s, 0, loc)
				if e.TZ != "UTC" {
					fold(b, "EXDATE;TZID="+e.TZ+":"+x.Format("20060102T150405"))
				} else {
					fold(b, "EXDATE:"+utc(x))
				}
			}
		}
	}
	e.writeDetails(b, e.Title, e.Description, e.Location, link)
	fold(b, "END:VEVENT")
	// changed occurrences: same UID with RECURRENCE-ID (RFC 5545 3.8.4.4)
	if e.RRule == "" || len(e.Overrides) == 0 {
		return
	}
	dates := make([]string, 0, len(e.Overrides))
	for d := range e.Overrides {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	for _, d := range dates {
		in, ok := e.InstanceAt(d)
		if !ok || in.Override == nil {
			continue
		}
		ov := in.Override
		fold(b, "BEGIN:VEVENT")
		fold(b, "UID:"+e.UID)
		fold(b, fmt.Sprintf("SEQUENCE:%d", e.Sequence))
		fold(b, "DTSTAMP:"+utc(now))
		fold(b, "LAST-MODIFIED:"+utc(e.UpdatedAt))
		switch {
		case e.AllDay:
			fold(b, "RECURRENCE-ID;VALUE=DATE:"+in.Original.In(loc).Format("20060102"))
			fold(b, "DTSTART;VALUE=DATE:"+in.Start.In(loc).Format("20060102"))
			fold(b, "DTEND;VALUE=DATE:"+in.End.In(loc).Format("20060102"))
		case usesTZID(e):
			fold(b, "RECURRENCE-ID;TZID="+e.TZ+":"+in.Original.In(loc).Format("20060102T150405"))
			fold(b, "DTSTART;TZID="+e.TZ+":"+in.Start.In(loc).Format("20060102T150405"))
			fold(b, "DTEND;TZID="+e.TZ+":"+in.End.In(loc).Format("20060102T150405"))
		default:
			fold(b, "RECURRENCE-ID:"+utc(in.Original))
			fold(b, "DTSTART:"+utc(in.Start))
			fold(b, "DTEND:"+utc(in.End))
		}
		desc := e.Description
		if ov.Description != "" {
			desc = ov.Description
		}
		e.writeDetails(b, e.instTitle(in), desc, e.instLocation(in), link)
		fold(b, "END:VEVENT")
	}
}

func (e *Event) writeDetails(b *strings.Builder, title, desc, location, link string) {
	fold(b, "SUMMARY:"+icalEscape(title))
	if desc != "" {
		fold(b, "DESCRIPTION:"+icalEscape(desc))
	}
	if location != "" {
		fold(b, "LOCATION:"+icalEscape(location))
	}
	if e.Category != "" {
		fold(b, "CATEGORIES:"+icalEscape(e.Category))
	}
	if link != "" {
		fold(b, "URL:"+link)
	}
}

// Calendar renders a VCALENDAR.
func Calendar(name string, events []*Event, linkOf func(*Event) string) string {
	var b strings.Builder
	now := time.Now()
	for _, l := range []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Enterprise Wiki//Events//EN", "CALSCALE:GREGORIAN", "METHOD:PUBLISH"} {
		fold(&b, l)
	}
	if name != "" {
		fold(&b, "X-WR-CALNAME:"+icalEscape(name))
	}
	fold(&b, "REFRESH-INTERVAL;VALUE=DURATION:PT1H")
	fold(&b, "X-PUBLISHED-TTL:PT1H")
	// definitions of the time zones referenced with TZID (older Outlook versions need them)
	seen := map[string]bool{}
	for _, e := range events {
		if usesTZID(e) && !seen[e.TZ] {
			seen[e.TZ] = true
			VTimezone(&b, e.TZ, now)
		}
	}
	for _, e := range events {
		VEvent(&b, e, linkOf(e), now)
	}
	fold(&b, "END:VCALENDAR")
	return b.String()
}

func (p *Plugin) icalRoutes(hs *sdk.Host) {
	link := func(r *http.Request) func(e *Event) string {
		base := feed.BaseURL(hs.Config.Get().Server.BaseURL, r)
		return func(e *Event) string { return fmt.Sprintf("%s/e/%d#event-%d", base, e.CalendarID, e.ID) }
	}
	// GET /calendars/{id}/ical?token= – subscription (personal link with token)
	hs.Handle("GET", "/calendars/{id}/ical", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx, private, err := hs.Auth.FeedContext(r)
		if err != nil {
			return nil, err
		}
		cal, err := p.loadCalendar(ctx, id, r.URL.Query().Get("lang"))
		if err != nil {
			return nil, err
		}
		if !cal.Config.ICal {
			return nil, apperr.NotFound("iCal subscription (switch it on in the calendar settings)")
		}
		if hs.ACL.Classification(ctx, cal.Category) >= acl.Confidential {
			return nil, apperr.Forbidden("confidential calendars cannot be subscribed")
		}
		// events of the last 90 days and everything later
		from := time.Now().AddDate(0, 0, -90)
		events, err := p.eventsOf(ctx, []int64{id}, from, time.Now().AddDate(5, 0, 0), "", "")
		if err != nil {
			return nil, err
		}
		cc := "public, max-age=900"
		if private {
			cc = "private, max-age=900"
		}
		w.Header().Set("Cache-Control", cc)
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		w.Header().Set("Content-Disposition", `inline; filename="calendar.ics"`)
		_, _ = w.Write([]byte(Calendar(cal.Title, events, link(r))))
		return h.Handled{}, nil
	})
	// GET /events/{id}/ical – one event as .ics download ("add to my calendar")
	hs.Handle("GET", "/events/{id}/ical", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		e, _, err := p.event(r.Context(), id, "")
		if err != nil {
			return nil, err
		}
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="event-%d.ics"`, id))
		_, _ = w.Write([]byte(Calendar("", []*Event{e}, link(r))))
		return h.Handled{}, nil
	})
}
