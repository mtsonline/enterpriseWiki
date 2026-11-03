package content_events

import (
	"fmt"
	"strings"
	"time"
)

// Calendar invitations (iTIP, RFC 5546): registration e-mails carry the
// occurrence as METHOD:REQUEST, so Outlook, Apple Mail, Thunderbird and
// Gmail put it into the calendar; changes send an updated REQUEST and
// cancellations a METHOD:CANCEL that removes the entry again. Every
// registered occurrence is its own calendar entry (UID per occurrence).

// invitationKinds maps e-mail kinds to the iTIP method they carry.
var invitationKinds = map[string]string{
	"mail_registered":         "REQUEST",
	"mail_promoted":           "REQUEST",
	"mail_occurrence_changed": "REQUEST",
	"mail_cancelled":          "CANCEL",
	"mail_event_cancelled":    "CANCEL",
}

// sequence grows with time, so every later message wins over earlier ones
// (also a new registration after a cancellation).
func sequence(now time.Time) int64 { return now.Unix() - 1_700_000_000 }

// Invitation renders one occurrence as iTIP message.
func Invitation(e *Event, in Instance, method, calendar, organizer, attendee, attendeeName, link string, now time.Time) string {
	var b strings.Builder
	utc := func(t time.Time) string { return t.UTC().Format("20060102T150405Z") }
	loc := e.Loc()
	for _, l := range []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Enterprise Wiki//Events//EN", "CALSCALE:GREGORIAN", "METHOD:" + method} {
		fold(&b, l)
	}
	local := !e.AllDay && e.TZ != "" && e.TZ != "UTC"
	if local {
		VTimezone(&b, e.TZ, now)
	}
	fold(&b, "BEGIN:VEVENT")
	fold(&b, fmt.Sprintf("UID:%s-%s", e.UID, in.Date))
	fold(&b, fmt.Sprintf("SEQUENCE:%d", sequence(now)))
	fold(&b, "DTSTAMP:"+utc(now))
	switch {
	case e.AllDay:
		fold(&b, "DTSTART;VALUE=DATE:"+in.Start.In(loc).Format("20060102"))
		fold(&b, "DTEND;VALUE=DATE:"+in.End.In(loc).Format("20060102"))
	case local:
		fold(&b, "DTSTART;TZID="+e.TZ+":"+in.Start.In(loc).Format("20060102T150405"))
		fold(&b, "DTEND;TZID="+e.TZ+":"+in.End.In(loc).Format("20060102T150405"))
	default:
		fold(&b, "DTSTART:"+utc(in.Start))
		fold(&b, "DTEND:"+utc(in.End))
	}
	desc := e.Description
	if in.Override != nil && in.Override.Description != "" {
		desc = in.Override.Description
	}
	if link != "" {
		desc = strings.TrimSpace(desc + "\n\n" + link)
	}
	fold(&b, "SUMMARY:"+icalEscape(e.instTitle(in)))
	if desc != "" {
		fold(&b, "DESCRIPTION:"+icalEscape(desc))
	}
	if l := e.instLocation(in); l != "" {
		fold(&b, "LOCATION:"+icalEscape(l))
	}
	if link != "" {
		fold(&b, "URL:"+link)
	}
	fold(&b, "ORGANIZER;CN="+paramValue(calendar)+":mailto:"+organizer)
	partstat := "ACCEPTED"
	if method == "CANCEL" {
		partstat = "DECLINED"
	}
	fold(&b, "ATTENDEE;CN="+paramValue(attendeeName)+";ROLE=REQ-PARTICIPANT;PARTSTAT="+partstat+";RSVP=FALSE:mailto:"+attendee)
	if method == "CANCEL" {
		fold(&b, "STATUS:CANCELLED")
	} else {
		fold(&b, "STATUS:CONFIRMED")
		fold(&b, "TRANSP:OPAQUE")
	}
	fold(&b, "END:VEVENT")
	fold(&b, "END:VCALENDAR")
	return b.String()
}

// paramValue quotes a parameter value (RFC 5545 3.2: no quotes or control characters).
func paramValue(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '"' || r < 32 {
			return -1
		}
		return r
	}, s)
	return `"` + s + `"`
}
