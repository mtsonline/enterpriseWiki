package workflow_process

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedules of triggers: five fields "minute hour day-of-month month
// day-of-week" (numbers, *, lists a,b, ranges a-b and steps */n or a-b/n;
// Sunday is 0 or 7) or @hourly, @daily, @weekly, @monthly, @yearly. Times are
// in the time zone of the server.

type cronField struct{ ok [61]bool }

// Cron is a parsed schedule.
type Cron struct {
	min, hour, dom, mon, dow cronField
	domAny, dowAny           bool
}

var cronAliases = map[string]string{
	"@hourly": "0 * * * *", "@daily": "0 0 * * *", "@midnight": "0 0 * * *",
	"@weekly": "0 0 * * 0", "@monthly": "0 0 1 * *", "@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *",
}

// ParseCron reads a schedule.
func ParseCron(s string) (*Cron, error) {
	s = strings.TrimSpace(s)
	if a, ok := cronAliases[strings.ToLower(s)]; ok {
		s = a
	}
	parts := strings.Fields(s)
	if len(parts) != 5 {
		return nil, fmt.Errorf("a schedule has five fields: minute hour day month weekday")
	}
	c := &Cron{}
	specs := []struct {
		f        *cronField
		lo, hi   int
		name     string
		anyFound *bool
	}{
		{&c.min, 0, 59, "minute", nil}, {&c.hour, 0, 23, "hour", nil}, {&c.dom, 1, 31, "day", &c.domAny},
		{&c.mon, 1, 12, "month", nil}, {&c.dow, 0, 7, "weekday", &c.dowAny},
	}
	for i, sp := range specs {
		if err := sp.f.parse(parts[i], sp.lo, sp.hi); err != nil {
			return nil, fmt.Errorf("%s: %w", sp.name, err)
		}
		if sp.anyFound != nil {
			*sp.anyFound = parts[i] == "*" || parts[i] == "?"
		}
	}
	if c.dow.ok[7] {
		c.dow.ok[0] = true
	}
	return c, nil
}

func (f *cronField) parse(s string, lo, hi int) error {
	for _, part := range strings.Split(s, ",") {
		step := 1
		if base, st, ok := strings.Cut(part, "/"); ok {
			n, err := strconv.Atoi(st)
			if err != nil || n < 1 || n > hi {
				return fmt.Errorf("invalid step %q", st)
			}
			step, part = n, base
		}
		a, b := lo, hi
		switch {
		case part == "*" || part == "?":
		case strings.Contains(part, "-"):
			x, y, _ := strings.Cut(part, "-")
			var e1, e2 error
			a, e1 = strconv.Atoi(x)
			b, e2 = strconv.Atoi(y)
			if e1 != nil || e2 != nil || a > b {
				return fmt.Errorf("invalid range %q", part)
			}
		default:
			n, err := strconv.Atoi(part)
			if err != nil {
				return fmt.Errorf("invalid value %q", part)
			}
			a, b = n, n
			if step > 1 {
				b = hi
			}
		}
		if a < lo || b > hi {
			return fmt.Errorf("%q is outside of %d-%d", part, lo, hi)
		}
		for v := a; v <= b; v += step {
			f.ok[v] = true
		}
	}
	return nil
}

// Match reports whether the minute of t is scheduled.
func (c *Cron) Match(t time.Time) bool {
	if !c.min.ok[t.Minute()] || !c.hour.ok[t.Hour()] || !c.mon.ok[int(t.Month())] {
		return false
	}
	dom, dow := c.dom.ok[t.Day()], c.dow.ok[int(t.Weekday())]
	if c.domAny || c.dowAny { // as in cron: both restricted means either
		return dom && dow
	}
	return dom || dow
}

// Next returns the first scheduled minute after t (within about five years).
func (c *Cron) Next(t time.Time) (time.Time, bool) {
	t = t.Truncate(time.Minute).Add(time.Minute)
	end := t.AddDate(5, 0, 0)
	for t.Before(end) {
		switch {
		case !c.mon.ok[int(t.Month())]:
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
		case !c.dayOK(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
		case !c.hour.ok[t.Hour()]:
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
		case !c.min.ok[t.Minute()]:
			t = t.Add(time.Minute)
		default:
			return t, true
		}
	}
	return time.Time{}, false
}

func (c *Cron) dayOK(t time.Time) bool {
	dom, dow := c.dom.ok[t.Day()], c.dow.ok[int(t.Weekday())]
	if c.domAny || c.dowAny {
		return dom && dow
	}
	return dom || dow
}
