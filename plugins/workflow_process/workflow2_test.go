package workflow_process

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCron(t *testing.T) {
	loc := time.Local
	c, err := ParseCron("0 7 * * 1-5")
	if err != nil {
		t.Fatal(err)
	}
	mon := time.Date(2026, 11, 16, 7, 0, 0, 0, loc) // a Monday
	if !c.Match(mon) || c.Match(mon.Add(time.Minute)) || c.Match(time.Date(2026, 11, 15, 7, 0, 0, 0, loc)) {
		t.Fatal("match")
	}
	if n, ok := c.Next(time.Date(2026, 11, 13, 7, 0, 0, 0, loc)); !ok || !n.Equal(mon) { // Friday 7:00 -> Monday 7:00
		t.Fatalf("next: %v", n)
	}
	for _, s := range []string{"@daily", "*/15 * * * *", "0 0 1,15 * *", "30 6 * 1-3 0", "0 12 * * 7"} {
		if _, err := ParseCron(s); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	for _, s := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "5-1 * * * *", "*/0 * * * *", "a * * * *"} {
		if _, err := ParseCron(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
	sun, _ := ParseCron("0 0 * * 7")
	if !sun.Match(time.Date(2026, 11, 15, 0, 0, 0, 0, loc)) {
		t.Error("7 is Sunday")
	}
	// both day fields restricted: either matches (as in cron)
	either, _ := ParseCron("0 0 13 * 5")
	if !either.Match(time.Date(2026, 11, 13, 0, 0, 0, 0, loc)) || !either.Match(time.Date(2026, 11, 20, 0, 0, 0, 0, loc)) {
		t.Error("day of month or weekday")
	}
}

func TestEscapingAndPaths(t *testing.T) {
	vars := map[string]string{"field.q": `A&B/../x?y#z`, "field.s": `"}, "admin": true, {"`}
	u := renderWith("https://api.example.com/items/{{field.q}}?q={{field.q}}", vars, url.QueryEscape)
	pu, err := url.Parse(u)
	if err != nil || pu.Host != "api.example.com" || pu.Query().Get("q") != vars["field.q"] || strings.Contains(pu.EscapedPath(), "/../") || pu.Fragment != "" {
		t.Fatalf("url: %s %v", u, err)
	}
	body := renderWith(`{"name": "{{field.s}}"}`, vars, jsonEscape)
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil || len(m) != 1 || m["name"] != vars["field.s"] {
		t.Fatalf("json injection: %s %v", body, err)
	}
	if hostPart("https://{{field.h}}.example.com/x") != "{{field.h}}.example.com" || hostPart("http://a.b:8080?x") != "a.b:8080" {
		t.Fatal("host part")
	}
	var doc any
	_ = json.Unmarshal([]byte(`{"a":{"b":[{"c":"x"},{"c":2.5}]},"t":true}`), &doc)
	for path, want := range map[string]string{"a.b.0.c": "x", "a.b.1.c": "2.5", "t": "true", "a.b.9.c": "", "a.x": "", "a.b": `[{"c":"x"},{"c":2.5}]`} {
		if got := pick(doc, path); got != want {
			t.Errorf("pick %s: %q, want %q", path, got, want)
		}
	}
	out := map[string]string{}
	big := map[string]any{"__proto__": "x", "bad key!": "x", "ok": strings.Repeat("y", 5000)}
	for i := 0; i < 500; i++ {
		big["k"+strings.Repeat("a", i%50)+string(rune('a'+i%26))] = "v"
	}
	flatten("trigger.data", big, out, 0)
	if _, bad := out["trigger.data.bad key!"]; len(out) > 121 || bad || out["trigger.data.__proto__"] != "" {
		t.Fatalf("flatten: %d keys", len(out))
	}
	small := map[string]string{}
	flatten("x", map[string]any{"ok": strings.Repeat("y", 5000)}, small, 0)
	if len([]rune(small["x.ok"])) != 2000 {
		t.Fatal("long values are cut")
	}
}

func TestHookSignature(t *testing.T) {
	a := hookSignature([]byte("s"), "100", []byte(`{"x":1}`))
	if a == hookSignature([]byte("s"), "101", []byte(`{"x":1}`)) || a == hookSignature([]byte("t"), "100", []byte(`{"x":1}`)) || len(a) != 64 {
		t.Fatal("the signature must cover secret, time and body")
	}
}

func TestValidateWorkflow2(t *testing.T) {
	base := func() *Definition {
		return &Definition{Fields: []Field{{ID: "d", Label: "D", Type: "date"}, {ID: "n", Label: "N", Type: "number"}},
			Steps: []Step{{ID: "t", Name: "T", Type: StepTask, Assignee: Assignee{Type: "starter"}, Actions: []Action{{ID: "ok", Label: "OK", Next: "e"}}},
				{ID: "e", Name: "E", Type: StepEnd}}}
	}
	d := base()
	d.Steps[0].Deadline = &Deadline{Days: 1, Warn: []int{4, 48, 4}, Escalate: &Escalation{Action: "action", ActionID: "ok"}}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if w := d.Steps[0].Deadline.Warn; len(w) != 2 || w[0] != 48 {
		t.Fatalf("warnings sorted, unique: %v", w)
	}
	d = base()
	d.Steps[0].Deadline = &Deadline{Days: 1, Escalate: &Escalation{Action: "action", ActionID: "nope"}}
	if d.Validate() == nil {
		t.Fatal("unknown escalation action accepted")
	}
	d = base()
	d.Steps = append([]Step{{ID: "s", Name: "S", Type: StepSet, Next: "t", Set: []Assign{{Field: "x", Formula: "n * unknown"}}}}, d.Steps...)
	if d.Validate() == nil {
		t.Fatal("formula with an unknown field accepted")
	}
	d = base()
	d.Steps = append([]Step{{ID: "s", Name: "S", Type: StepSet, Next: "t", Set: []Assign{{Field: "x", Formula: "n * 2"}}},
		{ID: "c", Name: "C", Type: StepCondition, Next: "t", NextElse: "e", Rules: []Rule{{Field: "x", Op: "gt", Value: "3"}}}}, d.Steps...)
	if err := d.Validate(); err != nil {
		t.Fatalf("a value of a set step in a condition: %v", err)
	}
	// automatic steps lose assignees; deadlines only on manual steps
	httpDef := func(runAs string) *Definition {
		d := base()
		d.Steps = append([]Step{{ID: "h", Name: "H", Type: StepHTTP, Next: "t", HTTP: &HTTPSpec{URL: "https://x.example.com/a?b={{field.n}}"},
			Assignee: Assignee{Type: "user", Value: "x"}, Deadline: &Deadline{Days: 1}, RunAs: runAs}}, d.Steps...)
		return d
	}
	if httpDef("root").Validate() == nil {
		t.Fatal("run as root accepted")
	}
	d = httpDef("")
	if err := d.Validate(); err != nil || d.Steps[0].Assignee.Type != "" || d.Steps[0].Deadline != nil || d.Steps[0].RunAs != "definer" || d.Steps[0].HTTP.Method != "GET" {
		t.Fatalf("http step normalized: %+v %v", d.Steps[0], err)
	}
}

func TestDueTimeWorkdays(t *testing.T) {
	thu := time.Date(2026, 12, 24, 10, 0, 0, 0, time.Local) // the Thursday before Christmas
	x := &Instance{Data: map[string]any{"until": "2026-12-31"}}
	hol := map[string]bool{"12-25": true, "2026-12-28": true}
	// Fri 25 (holiday), Sat, Sun, Mon 28 (holiday) do not count
	if d := dueTimeWith(x, &Deadline{Days: 2, Workdays: true}, thu, hol); d.Day() != 30 || d.Hour() != 10 {
		t.Fatalf("two working days after 24 Dec: %v", d)
	}
	if d := dueTimeWith(x, &Deadline{Days: 1, Hours: 2}, thu, nil); !d.Equal(thu.Add(26 * time.Hour)) {
		t.Fatalf("calendar days: %v", d)
	}
	if d := dueTimeWith(x, &Deadline{Field: "until"}, thu, nil); d.Day() != 31 || d.Hour() != 23 {
		t.Fatalf("end of the day of a date field: %v", d)
	}
	// a deadline that lands on a day off moves to the next working day
	sat := time.Date(2026, 11, 13, 10, 0, 0, 0, time.Local) // Friday
	if d := dueTimeWith(x, &Deadline{Hours: 24, Workdays: true}, sat, hol); d.Weekday() != time.Monday {
		t.Fatalf("on a weekend: %v", d)
	}
}
