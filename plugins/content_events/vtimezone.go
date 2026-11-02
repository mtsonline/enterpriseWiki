package content_events

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// VTIMEZONE (RFC 5545 3.6.5) for the time zones used with TZID. Modern
// clients know the IANA names, but older Outlook versions and some other
// clients need the definition in the file.

type tzTransition struct {
	at       time.Time // instant of the change
	from, to int       // UTC offsets in seconds
	name     string    // abbreviation after the change
}

// transitions finds the offset changes of loc in [from, to).
func transitions(loc *time.Location, from, to time.Time) []tzTransition {
	var out []tzTransition
	_, prev := from.In(loc).Zone()
	for t := from; t.Before(to); {
		next := t.Add(24 * time.Hour)
		_, off := next.In(loc).Zone()
		if off != prev {
			lo, hi := t, next // bisect to the second
			for hi.Sub(lo) > time.Second {
				mid := lo.Add(hi.Sub(lo) / 2)
				if _, o := mid.In(loc).Zone(); o == prev {
					lo = mid
				} else {
					hi = mid
				}
			}
			name, _ := hi.In(loc).Zone()
			out = append(out, tzTransition{at: hi, from: prev, to: off, name: name})
			prev = off
		}
		t = next
	}
	return out
}

func icalOffset(sec int) string {
	sign := "+"
	if sec < 0 {
		sign, sec = "-", -sec
	}
	return fmt.Sprintf("%s%02d%02d", sign, sec/3600, sec%3600/60)
}

var icalDays = [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

// weekdayIndex returns n (1…4 or -1 for the last) of the weekday of day d in its month.
func weekdayIndex(d time.Time) int {
	last := time.Date(d.Year(), d.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if d.Day()+7 > last {
		return -1
	}
	return (d.Day()-1)/7 + 1
}

// occurrence returns the n-th weekday wd of month m in year y (n = -1: last).
func occurrence(y int, m time.Month, wd time.Weekday, n int) int {
	if n == -1 {
		last := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC)
		return last.Day() - (int(last.Weekday())-int(wd)+7)%7
	}
	first := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	return 1 + (int(wd)-int(first.Weekday())+7)%7 + (n-1)*7
}

// VTimezone writes the definition of tzid with the rules valid at ref.
func VTimezone(b *strings.Builder, tzid string, ref time.Time) bool {
	loc, err := time.LoadLocation(tzid)
	if err != nil || tzid == "UTC" {
		return false
	}
	y := ref.In(loc).Year()
	fold(b, "BEGIN:VTIMEZONE")
	fold(b, "TZID:"+tzid)
	year := transitions(loc, time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC).Add(-14*time.Hour), time.Date(y+1, 1, 1, 0, 0, 0, 0, time.UTC).Add(14*time.Hour))
	if len(year) > 2 {
		year = year[:0] // irregular year: fall back to explicit dates below
	}
	component := func(tr tzTransition) string {
		if tr.to > tr.from {
			return "DAYLIGHT"
		}
		return "STANDARD"
	}
	switch {
	case len(year) == 0 && len(transitions(loc, time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(y+5, 1, 1, 0, 0, 0, 0, time.UTC))) == 0:
		// no daylight saving time
		name, off := ref.In(loc).Zone()
		fold(b, "BEGIN:STANDARD")
		fold(b, "DTSTART:19700101T000000")
		fold(b, "TZOFFSETFROM:"+icalOffset(off))
		fold(b, "TZOFFSETTO:"+icalOffset(off))
		fold(b, "TZNAME:"+name)
		fold(b, "END:STANDARD")
	case len(year) == 2:
		// yearly rules, e.g. last Sunday of March at 02:00
		for _, tr := range year {
			wall := tr.at.Add(time.Duration(tr.from) * time.Second).UTC() // local time before the change
			n := weekdayIndex(wall)
			start := time.Date(1970, wall.Month(), occurrence(1970, wall.Month(), wall.Weekday(), n), wall.Hour(), wall.Minute(), wall.Second(), 0, time.UTC)
			c := component(tr)
			fold(b, "BEGIN:"+c)
			fold(b, "DTSTART:"+start.Format("20060102T150405"))
			fold(b, fmt.Sprintf("RRULE:FREQ=YEARLY;BYMONTH=%d;BYDAY=%d%s", int(wall.Month()), n, icalDays[wall.Weekday()]))
			fold(b, "TZOFFSETFROM:"+icalOffset(tr.from))
			fold(b, "TZOFFSETTO:"+icalOffset(tr.to))
			fold(b, "TZNAME:"+tr.name)
			fold(b, "END:"+c)
		}
	default:
		// irregular changes: explicit dates for some years before and after ref
		all := transitions(loc, time.Date(y-2, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(y+6, 1, 1, 0, 0, 0, 0, time.UTC))
		if len(all) == 0 {
			name, off := ref.In(loc).Zone()
			all = []tzTransition{{at: time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), from: off, to: off, name: name}}
		}
		type key struct {
			from, to int
			name     string
		}
		groups := map[key][]time.Time{}
		var order []key
		for _, tr := range all {
			k := key{tr.from, tr.to, tr.name}
			if _, ok := groups[k]; !ok {
				order = append(order, k)
			}
			groups[k] = append(groups[k], tr.at.Add(time.Duration(tr.from)*time.Second).UTC())
		}
		sort.SliceStable(order, func(i, j int) bool { return groups[order[i]][0].Before(groups[order[j]][0]) })
		for _, k := range order {
			c := component(tzTransition{from: k.from, to: k.to})
			ds := groups[k]
			fold(b, "BEGIN:"+c)
			fold(b, "DTSTART:"+ds[0].Format("20060102T150405"))
			for _, d := range ds[1:] {
				fold(b, "RDATE:"+d.Format("20060102T150405"))
			}
			fold(b, "TZOFFSETFROM:"+icalOffset(k.from))
			fold(b, "TZOFFSETTO:"+icalOffset(k.to))
			fold(b, "TZNAME:"+k.name)
			fold(b, "END:"+c)
		}
	}
	fold(b, "END:VTIMEZONE")
	return true
}
