package server_test

import "testing"

// TestWAFTasks: ticking tasks, querying them and the report pass the Core
// Rule Set (a value "done" of a block parameter looked like a shell
// command, so the report says "completed").
func TestWAFTasks(t *testing.T) {
	for _, crs := range []string{"3", "4"} {
		waf := wafFor(t, crs, "")
		for _, r := range [][3]string{
			{"POST", "/api/v1/x/core.html/tasks/12/3", `{"rev":4,"done":true,"lang":"de"}`},
			{"POST", "/api/v1/x/core.html/tasks/12/3", `{"rev":4,"done":false}`},
			{"GET", "/api/v1/data/core.html.task?person=me&state=done&no_day=true", ""},
			{"GET", "/api/v1/data/core.html.task?state=open,done", ""},
			{"POST", "/api/v1/drafts", draftBody("::tasks{status=completed z=1}\n")},
			{"POST", "/api/v1/drafts", draftBody("- [x] Done it @anna :date{value=2026-11-30}\n- [ ] done @bob\n")},
		} {
			if rules, blocked := wafCheck(t, waf, r[0], r[1], r[2]); blocked || len(rules) > 0 {
				t.Errorf("CRS %s: %s %s: %v", crs, r[1], r[2], rules)
			}
		}
	}
}
