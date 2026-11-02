package content_events

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Rule is the supported subset of iCalendar RRULE (RFC 5545):
// FREQ=DAILY|WEEKLY|MONTHLY|YEARLY, INTERVAL, COUNT, UNTIL, BYDAY
// (weekly: MO,WE; monthly: 2TU or -1FR) and BYMONTHDAY (monthly).
type Rule struct {
	Freq       string         `json:"freq"`
	Interval   int            `json:"interval"`
	Count      int            `json:"count"`
	Until      time.Time      `json:"until"` // inclusive (local date of the event)
	ByDay      []time.Weekday `json:"-"`
	ByDayN     int            `json:"-"` // monthly: nth weekday (1..5, -1 = last); 0 = every
	ByMonthDay int            `json:"-"`
}

var dayNames = map[string]time.Weekday{"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday}

// MaxOccurrences limits how many occurrences a series may produce.
const MaxOccurrences = 3660

// ParseRule parses an RRULE value ("" = no recurrence).
func ParseRule(s string) (*Rule, error) {
	s = strings.TrimPrefix(strings.TrimSpace(strings.ToUpper(s)), "RRULE:")
	if s == "" {
		return nil, nil
	}
	r := &Rule{Interval: 1}
	for _, part := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("invalid recurrence part %q", part)
		}
		switch k {
		case "FREQ":
			if v != "DAILY" && v != "WEEKLY" && v != "MONTHLY" && v != "YEARLY" {
				return nil, fmt.Errorf("unsupported frequency %q (DAILY, WEEKLY, MONTHLY, YEARLY)", v)
			}
			r.Freq = v
		case "INTERVAL":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 1000 {
				return nil, fmt.Errorf("invalid interval %q", v)
			}
			r.Interval = n
		case "COUNT":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > MaxOccurrences {
				return nil, fmt.Errorf("invalid count %q (1–%d)", v, MaxOccurrences)
			}
			r.Count = n
		case "UNTIL":
			d := v
			if len(d) >= 8 {
				d = d[:8]
			}
			t, err := time.Parse("20060102", d)
			if err != nil {
				return nil, fmt.Errorf("invalid end date %q", v)
			}
			r.Until = t
		case "BYDAY":
			for _, d := range strings.Split(v, ",") {
				n := 0
				if len(d) > 2 {
					var err error
					if n, err = strconv.Atoi(d[:len(d)-2]); err != nil || n == 0 || n < -5 || n > 5 {
						return nil, fmt.Errorf("invalid weekday %q", d)
					}
					d = d[len(d)-2:]
				}
				wd, ok := dayNames[d]
				if !ok {
					return nil, fmt.Errorf("invalid weekday %q", d)
				}
				r.ByDay = append(r.ByDay, wd)
				if n != 0 {
					r.ByDayN = n
				}
			}
		case "BYMONTHDAY":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 31 {
				return nil, fmt.Errorf("invalid day of month %q", v)
			}
			r.ByMonthDay = n
		case "WKST":
		default:
			return nil, fmt.Errorf("unsupported recurrence part %q", k)
		}
	}
	if r.Freq == "" {
		return nil, fmt.Errorf("recurrence needs FREQ")
	}
	if r.ByDayN != 0 && (r.Freq != "MONTHLY" || len(r.ByDay) != 1) {
		return nil, fmt.Errorf("nth weekday (e.g. 2TU) is only supported for monthly series with one weekday")
	}
	return r, nil
}

// String returns the canonical RRULE value.
func (r *Rule) String() string {
	if r == nil {
		return ""
	}
	parts := []string{"FREQ=" + r.Freq}
	if r.Interval > 1 {
		parts = append(parts, fmt.Sprintf("INTERVAL=%d", r.Interval))
	}
	if len(r.ByDay) > 0 {
		var ds []string
		for _, d := range r.ByDay {
			name := ""
			for k, v := range dayNames {
				if v == d {
					name = k
				}
			}
			if r.ByDayN != 0 {
				name = strconv.Itoa(r.ByDayN) + name
			}
			ds = append(ds, name)
		}
		parts = append(parts, "BYDAY="+strings.Join(ds, ","))
	}
	if r.ByMonthDay > 0 {
		parts = append(parts, fmt.Sprintf("BYMONTHDAY=%d", r.ByMonthDay))
	}
	if r.Count > 0 {
		parts = append(parts, fmt.Sprintf("COUNT=%d", r.Count))
	}
	if !r.Until.IsZero() {
		parts = append(parts, "UNTIL="+r.Until.Format("20060102"))
	}
	return strings.Join(parts, ";")
}

func nthWeekday(year int, month time.Month, wd time.Weekday, n int, loc *time.Location) (int, bool) {
	if n > 0 {
		first := time.Date(year, month, 1, 0, 0, 0, 0, loc)
		d := 1 + (int(wd)-int(first.Weekday())+7)%7 + (n-1)*7
		if d > daysIn(year, month) {
			return 0, false
		}
		return d, true
	}
	last := daysIn(year, month)
	lt := time.Date(year, month, last, 0, 0, 0, 0, loc)
	d := last - (int(lt.Weekday())-int(wd)+7)%7 + (n+1)*7
	return d, d >= 1
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// Expand returns the start times of all occurrences of a series starting
// at start (wall time in loc) that begin before to and end after from
// (duration dur). exdates are local dates (YYYY-MM-DD) that are skipped.
func (r *Rule) Expand(start time.Time, dur time.Duration, loc *time.Location, from, to time.Time, exdates map[string]bool) []time.Time {
	start = start.In(loc)
	var out []time.Time
	if r == nil {
		if start.Before(to) && start.Add(dur).After(from) && !exdates[start.Format("2006-01-02")] {
			out = append(out, start)
		}
		return out
	}
	h, mi, s := start.Clock()
	at := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, h, mi, s, 0, loc) }
	n := 0 // occurrences counted for COUNT (exdates count too, like RFC 5545)
	emit := func(t time.Time) bool {
		if t.Before(start) {
			return true
		}
		if !r.Until.IsZero() && t.After(time.Date(r.Until.Year(), r.Until.Month(), r.Until.Day(), 23, 59, 59, 0, loc)) {
			return false
		}
		if r.Count > 0 && n >= r.Count {
			return false
		}
		n++
		if n > MaxOccurrences || !t.Before(to) {
			return false
		}
		if t.Add(dur).After(from) && !exdates[t.Format("2006-01-02")] {
			out = append(out, t)
		}
		return true
	}
	y, m, d := start.Date()
	for i := 0; i < MaxOccurrences*7; i++ {
		var cands []time.Time
		switch r.Freq {
		case "DAILY":
			cands = []time.Time{at(y, m, d+i*r.Interval)}
		case "WEEKLY":
			// week of the start (Monday based), every interval weeks
			base := at(y, m, d-((int(start.Weekday())+6)%7)+i*7*r.Interval)
			days := r.ByDay
			if len(days) == 0 {
				days = []time.Weekday{start.Weekday()}
			}
			for _, wd := range days {
				by, bm, bd := base.Date()
				cands = append(cands, at(by, bm, bd+(int(wd)+6)%7))
			}
			sort.Slice(cands, func(a, b int) bool { return cands[a].Before(cands[b]) })
		case "MONTHLY":
			mt := time.Date(y, m+time.Month(i*r.Interval), 1, 0, 0, 0, 0, loc)
			my, mm, _ := mt.Date()
			switch {
			case r.ByDayN != 0:
				if day, ok := nthWeekday(my, mm, r.ByDay[0], r.ByDayN, loc); ok {
					cands = []time.Time{at(my, mm, day)}
				}
			default:
				day := d
				if r.ByMonthDay > 0 {
					day = r.ByMonthDay
				}
				if day <= daysIn(my, mm) { // 31st only in long months (RFC behaviour)
					cands = []time.Time{at(my, mm, day)}
				}
			}
		case "YEARLY":
			yy := y + i*r.Interval
			if d <= daysIn(yy, m) {
				cands = []time.Time{at(yy, m, d)}
			}
		}
		for _, c := range cands {
			if !emit(c) {
				return out
			}
		}
	}
	return out
}
