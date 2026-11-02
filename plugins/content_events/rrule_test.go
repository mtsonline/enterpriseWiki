package content_events

import (
	"testing"
	"time"
)

func dates(ts []time.Time) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Format("2006-01-02 15:04"))
	}
	return out
}

func TestExpand(t *testing.T) {
	vienna, _ := time.LoadLocation("Europe/Vienna")
	start := time.Date(2026, 3, 23, 9, 0, 0, 0, vienna) // Monday, DST change on 29 March
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, vienna)
	to := time.Date(2026, 5, 1, 0, 0, 0, 0, vienna)
	cases := []struct {
		rule string
		ex   map[string]bool
		want []string
	}{
		{"FREQ=WEEKLY;COUNT=3", nil, []string{"2026-03-23 09:00", "2026-03-30 09:00", "2026-04-06 09:00"}},
		{"FREQ=WEEKLY;BYDAY=MO,TH;UNTIL=20260402", nil, []string{"2026-03-23 09:00", "2026-03-26 09:00", "2026-03-30 09:00", "2026-04-02 09:00"}},
		{"FREQ=DAILY;INTERVAL=10;COUNT=4", map[string]bool{"2026-04-02": true}, []string{"2026-03-23 09:00", "2026-04-12 09:00", "2026-04-22 09:00"}},
		{"FREQ=MONTHLY;BYDAY=-1FR;COUNT=2", nil, []string{"2026-03-27 09:00", "2026-04-24 09:00"}},
		{"FREQ=MONTHLY;BYDAY=2TU", nil, []string{"2026-04-14 09:00"}},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=WE", nil, []string{"2026-03-25 09:00", "2026-04-08 09:00", "2026-04-22 09:00"}},
	}
	for _, c := range cases {
		r, err := ParseRule(c.rule)
		if err != nil {
			t.Fatalf("%s: %v", c.rule, err)
		}
		got := dates(r.Expand(start, time.Hour, vienna, from, to, c.ex))
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v want %v", c.rule, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: got %v want %v", c.rule, got, c.want)
			}
		}
		if r2, _ := ParseRule(r.String()); r2.String() != r.String() {
			t.Fatalf("round trip %s -> %s", c.rule, r.String())
		}
	}
	// the 31st only in long months, yearly on 29 Feb only in leap years
	r, _ := ParseRule("FREQ=MONTHLY;COUNT=3")
	got := dates(r.Expand(time.Date(2026, 1, 31, 8, 0, 0, 0, time.UTC), time.Hour, time.UTC, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), nil))
	if len(got) != 3 || got[1] != "2026-03-31 08:00" || got[2] != "2026-05-31 08:00" {
		t.Fatalf("monthly 31st: %v", got)
	}
	for _, bad := range []string{"FREQ=HOURLY", "FREQ=WEEKLY;BYDAY=XX", "FREQ=DAILY;COUNT=0", "INTERVAL=2", "FREQ=WEEKLY;BYDAY=2MO"} {
		if _, err := ParseRule(bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	// a single event overlapping the window
	if got := (*Rule)(nil).Expand(time.Date(2026, 2, 28, 22, 0, 0, 0, time.UTC), 4*time.Hour, time.UTC, from, to, nil); len(got) != 1 {
		t.Fatalf("overlapping single event: %v", got)
	}
}
