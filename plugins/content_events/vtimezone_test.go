package content_events

import (
	"strings"
	"testing"
	"time"
)

func TestVTimezone(t *testing.T) {
	ref := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	cases := map[string][]string{
		"Europe/Vienna": {"BEGIN:DAYLIGHT", "DTSTART:19700329T020000", "RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU", "TZOFFSETFROM:+0100", "TZOFFSETTO:+0200", "TZNAME:CEST",
			"BEGIN:STANDARD", "DTSTART:19701025T030000", "RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU", "TZNAME:CET"},
		"America/New_York": {"RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=2SU", "RRULE:FREQ=YEARLY;BYMONTH=11;BYDAY=1SU", "TZOFFSETTO:-0400", "TZOFFSETTO:-0500"},
		"Australia/Sydney": {"RRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=1SU", "RRULE:FREQ=YEARLY;BYMONTH=4;BYDAY=1SU", "TZOFFSETTO:+1100"},
		"Asia/Tokyo":       {"BEGIN:STANDARD", "TZOFFSETFROM:+0900", "TZOFFSETTO:+0900", "TZNAME:JST"},
	}
	for tz, want := range cases {
		var b strings.Builder
		if !VTimezone(&b, tz, ref) {
			t.Fatalf("%s: not written", tz)
		}
		out := b.String()
		for _, w := range want {
			if !strings.Contains(out, w+"\r\n") {
				t.Errorf("%s: missing %q in\n%s", tz, w, out)
			}
		}
		if tz == "Asia/Tokyo" && strings.Contains(out, "DAYLIGHT") {
			t.Errorf("Tokyo has no daylight saving time:\n%s", out)
		}
	}
	// the derived rule reproduces the real changes in other years
	vienna, _ := time.LoadLocation("Europe/Vienna")
	for y := 2027; y <= 2032; y++ {
		tr := transitions(vienna, time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(y+1, 1, 1, 0, 0, 0, 0, time.UTC))
		if len(tr) != 2 || tr[0].at.Day() != occurrence(y, time.March, time.Sunday, -1) || tr[1].at.Day() != occurrence(y, time.October, time.Sunday, -1) {
			t.Errorf("%d: %+v", y, tr)
		}
	}
	// calendars contain the definition for series in local time only once
	ev := func(id int64) *Event {
		return &Event{ID: id, UID: "x", Title: "Jour fixe", TZ: "Europe/Vienna", RRule: "FREQ=WEEKLY",
			StartsAt: time.Date(2026, 3, 23, 9, 0, 0, 0, vienna), EndsAt: time.Date(2026, 3, 23, 10, 0, 0, 0, vienna)}
	}
	cal := Calendar("Team", []*Event{ev(1), ev(2)}, func(*Event) string { return "" })
	if strings.Count(cal, "BEGIN:VTIMEZONE") != 1 || !strings.Contains(cal, "DTSTART;TZID=Europe/Vienna:20260323T090000") ||
		strings.Index(cal, "BEGIN:VTIMEZONE") > strings.Index(cal, "BEGIN:VEVENT") {
		t.Errorf("calendar:\n%s", cal)
	}
}
