package pgversion

import (
	"testing"
	"time"
)

func TestEOL(t *testing.T) {
	for major, want := range map[int]string{13: "2025-11-13", 14: "2026-11-12", 16: "2028-11-09", 17: "2029-11-08", 18: "2030-11-14"} {
		if got := EOL(major).Format("2006-01-02"); got != want {
			t.Errorf("EOL(%d) = %s, want %s", major, got, want)
		}
	}
}

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		num   int
		level string
		keys  int
	}{
		{180001, "ok", 0},
		{160004, "info", 1},    // newer recommended
		{140015, "warning", 3}, // support ends on 2026-11-12: recommended + 180 + 30
		{130020, "error", 4},   // ended
	}
	for _, c := range cases {
		st := Evaluate(c.num, "x", now)
		if st.Level != c.level || len(Notices(st)) != c.keys {
			t.Errorf("%d: %+v %v", c.num, st, Notices(st))
		}
	}
}
