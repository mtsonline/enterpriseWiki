package server_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

func TestEventInvitations(t *testing.T) {
	e := setup(t)
	c := &capture{}
	e.app.Mail.SetTransport(c)
	if err := e.app.Config.Update(func(cfg *config.Config) error { cfg.Mail.From = "wiki@example.com"; return nil }); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "eva", "password": "Passw0rd!Passw0rd", "display_name": "Eva", "email": "eva@example.com"}, nil, 201)
	eva := e.client()
	eva.login("eva", "Passw0rd!Passw0rd")
	var team cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &team, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", team.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	var calEl elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": team.ID, "type": "content.events", "title": "Courses", "body": map[string]any{"view": "list"}}, &calEl, 201)
	vienna := mustLoc("Europe/Vienna")
	now := time.Now().In(vienna)
	start := time.Date(now.Year(), now.Month(), now.Day(), 18, 0, 0, 0, vienna).AddDate(0, 0, 3)
	var ev struct {
		ID  int64  `json:"id"`
		UID string `json:"uid"`
	}
	admin.must("POST", fmt.Sprintf("/api/v1/x/content.events/calendars/%d/events", calEl.ID), map[string]any{"title": "Yoga", "starts_at": start, "ends_at": start.Add(time.Hour),
		"tz": "Europe/Vienna", "location": "Hall", "registration": true}, &ev, 201)
	date := start.Format("2006-01-02")
	reg := fmt.Sprintf("/api/v1/x/content.events/events/%d/registrations", ev.ID)
	eva.must("POST", reg, map[string]any{"occurrence": date}, nil, 200)
	e.flushMail(t)
	parts := c.calendarParts("eva@example.com")
	if len(parts) != 1 {
		t.Fatalf("invitations: %d", len(parts))
	}
	inv := parts[0]
	for _, want := range []string{"REQUEST\n", "METHOD:REQUEST", "UID:" + ev.UID + "-" + date, "BEGIN:VTIMEZONE", "DTSTART;TZID=Europe/Vienna:" + start.Format("20060102T150405"),
		"SUMMARY:Yoga", "LOCATION:Hall", "ORGANIZER;CN=\"Courses\":mailto:wiki@example.com", "mailto:eva@example.com", "STATUS:CONFIRMED"} {
		if !strings.Contains(inv, want) {
			t.Errorf("invitation lacks %q:\n%s", want, inv)
		}
	}
	// the whole event moves → updated invitation
	moved := start.Add(2 * time.Hour)
	admin.must("PUT", fmt.Sprintf("/api/v1/x/content.events/events/%d", ev.ID), map[string]any{"title": "Yoga", "starts_at": moved, "ends_at": moved.Add(time.Hour),
		"tz": "Europe/Vienna", "location": "Hall", "registration": true}, nil, 200)
	e.flushMail(t)
	parts = c.calendarParts("eva@example.com")
	if len(parts) != 2 || !strings.Contains(parts[1], "DTSTART;TZID=Europe/Vienna:"+moved.Format("20060102T150405")) || !strings.Contains(parts[1], "UID:"+ev.UID+"-"+date) {
		t.Fatalf("update: %v", parts)
	}
	// unregister → CANCEL removes the entry
	eva.must("DELETE", reg+"?occurrence="+date, nil, nil, 204)
	e.flushMail(t)
	parts = c.calendarParts("eva@example.com")
	if len(parts) != 3 || !strings.HasPrefix(parts[2], "CANCEL\n") || !strings.Contains(parts[2], "STATUS:CANCELLED") || !strings.Contains(parts[2], "UID:"+ev.UID+"-"+date) {
		t.Fatalf("cancel: %v", parts)
	}
}

func TestEventCalendar(t *testing.T) {
	e := setup(t)
	c := &capture{}
	e.app.Mail.SetTransport(c)
	admin := e.client()
	admin.login("admin", adminPW)
	var team cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &team, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", team.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	for _, n := range []string{"ann", "ben"} {
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": n, "password": "Passw0rd!Passw0rd", "display_name": n, "email": n + "@example.com"}, nil, 201)
	}
	ann, ben := e.client(), e.client()
	ann.login("ann", "Passw0rd!Passw0rd")
	ben.login("ben", "Passw0rd!Passw0rd")
	var calEl elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": team.ID, "type": "content.events", "title": "Team calendar",
		"body": map[string]any{"view": "month", "ical": true, "categories": []map[string]string{{"label": "Training", "color": "#00aa00"}}}}, &calEl, 201)
	base := fmt.Sprintf("/api/v1/x/content.events/calendars/%d", calEl.ID)

	// only Create may add events
	start := time.Now().Add(48 * time.Hour).Truncate(time.Hour)
	ev := map[string]any{"title": "Workshop", "starts_at": start, "ends_at": start.Add(2 * time.Hour), "tz": "Europe/Vienna", "category": "training",
		"registration": true, "capacity": 1, "waitlist": true, "reminders": []int{1440, 60}, "location": "Room 1"}
	if code := ann.do("POST", base+"/events", ev, nil); code != 403 {
		t.Fatalf("viewer created an event: %d", code)
	}
	if code := admin.do("POST", base+"/events", map[string]any{"title": "x", "starts_at": start, "category": "nope"}, nil); code != 400 {
		t.Fatalf("unknown category accepted: %d", code)
	}
	var w struct {
		ID  int64  `json:"id"`
		UID string `json:"uid"`
	}
	admin.must("POST", base+"/events", ev, &w, 201)
	date := start.In(mustLoc("Europe/Vienna")).Format("2006-01-02")
	reg := fmt.Sprintf("/api/v1/x/content.events/events/%d/registrations", w.ID)
	var st struct {
		Status string `json:"status"`
	}
	ann.must("POST", reg, map[string]any{"occurrence": date}, &st, 200)
	if st.Status != "registered" {
		t.Fatalf("ann: %s", st.Status)
	}
	ben.must("POST", reg, map[string]any{"occurrence": date, "note": "vegetarian"}, &st, 200)
	if st.Status != "waitlist" {
		t.Fatalf("ben: %s", st.Status)
	}
	if code := ben.do("POST", reg, map[string]any{"occurrence": "2020-01-01"}, nil); code != 400 {
		t.Fatalf("registration for a wrong date: %d", code)
	}
	if code := ann.do("GET", reg, nil, nil); code != 403 {
		t.Fatalf("participant list for a viewer: %d", code)
	}
	var parts []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	admin.must("GET", reg, nil, &parts, 200)
	if len(parts) != 2 || parts[0].Name != "ann" || parts[1].Note != "vegetarian" {
		t.Fatalf("participants: %+v", parts)
	}
	// ann cancels → ben moves up
	ann.must("DELETE", reg+"?occurrence="+date, nil, nil, 204)
	var list struct {
		Occurrences []struct {
			Registered int    `json:"registered"`
			Waiting    int    `json:"waiting"`
			MyStatus   string `json:"my_status"`
			Full       bool   `json:"full"`
		} `json:"occurrences"`
	}
	ben.must("GET", base+"/events?from="+time.Now().Format("2006-01-02")+"&to="+time.Now().AddDate(0, 0, 7).Format("2006-01-02"), nil, &list, 200)
	if len(list.Occurrences) != 1 || list.Occurrences[0].Registered != 1 || list.Occurrences[0].MyStatus != "registered" || !list.Occurrences[0].Full {
		t.Fatalf("after cancellation: %+v", list)
	}
	e.flushMail(t)
	for _, want := range []struct{ to, text string }{
		{"ann@example.com", "Registration confirmed: Workshop"}, {"ben@example.com", "Waiting list: Workshop"},
		{"ann@example.com", "Cancellation confirmed"}, {"ben@example.com", "you are in: Workshop"},
	} {
		if !c.find(want.to, want.text) {
			t.Fatalf("missing mail to %s: %q", want.to, want.text)
		}
	}

	// a weekly series with one cancelled date
	series := map[string]any{"title": "Stand-up", "starts_at": start, "ends_at": start.Add(15 * time.Minute), "tz": "Europe/Vienna",
		"rrule": "FREQ=WEEKLY;COUNT=4", "exdates": []string{start.AddDate(0, 0, 7).In(mustLoc("Europe/Vienna")).Format("2006-01-02")}}
	var s struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", base+"/events", series, &s, 201)
	list.Occurrences = nil
	admin.must("GET", base+"/events?q=stand&from="+time.Now().Format("2006-01-02")+"&to="+time.Now().AddDate(0, 2, 0).Format("2006-01-02"), nil, &list, 200)
	if len(list.Occurrences) != 3 {
		t.Fatalf("series occurrences: %+v", list)
	}
	// iCal subscription with RRULE and EXDATE
	resp, body := admin.raw(base+"/ical", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "BEGIN:VCALENDAR") || !strings.Contains(body, "RRULE:FREQ=WEEKLY;COUNT=4") ||
		!strings.Contains(body, "EXDATE;TZID=Europe/Vienna:") || !strings.Contains(body, "UID:"+w.UID) || !strings.Contains(body, "LOCATION:Room 1") {
		t.Fatalf("ical: %d %s", resp.StatusCode, body)
	}
	if r2, _ := e.client().raw(base+"/ical?token=ft_invalid", nil); r2.StatusCode != 401 {
		t.Fatalf("invalid token: %d", r2.StatusCode)
	}

	// reminders: an event starting in 30 minutes with a 1 h reminder
	soon := time.Now().Add(30 * time.Minute)
	var r struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", base+"/events", map[string]any{"title": "Coffee", "starts_at": soon, "tz": "UTC", "reminders": []int{60}}, &r, 201)
	ann.must("POST", fmt.Sprintf("/api/v1/x/content.events/events/%d/registrations", r.ID), map[string]any{"occurrence": soon.UTC().Format("2006-01-02"), "interested": true}, &st, 200)
	if st.Status != "interested" {
		t.Fatalf("interested: %s", st.Status)
	}
	for i := 0; i < 2; i++ { // twice: no duplicate reminders
		if err := e.app.Workers.RunOnce(context.Background(), "content.events.reminders"); err != nil {
			t.Fatal(err)
		}
	}
	e.flushMail(t)
	n := 0
	c.mu.Lock()
	for _, m := range c.msgs {
		if strings.Contains(decodeQP(string(m.Data)), "Reminder: Coffee") {
			n++
		}
	}
	c.mu.Unlock()
	if n != 1 {
		t.Fatalf("reminders sent: %d", n)
	}
	// server-rendered list and the macro
	var view struct {
		HTML string `json:"html"`
	}
	ann.must("GET", fmt.Sprintf("/api/v1/elements/%d", calEl.ID), nil, &view, 200)
	if !strings.Contains(view.HTML, `data-ew-widget="events"`) || !strings.Contains(view.HTML, "Workshop") {
		t.Fatalf("calendar html: %s", view.HTML)
	}
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": team.ID, "type": "core.html", "title": "Home",
		"body": htmlBody(fmt.Sprintf(`<div data-macro="events" data-params='{"calendar":"%d","limit":"2"}'></div>`, calEl.ID))}, &page, 201)
	ann.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, &view, 200)
	if !strings.Contains(view.HTML, "ewe-upcoming") || strings.Count(view.HTML, `class="ewe-up"`) != 2 {
		t.Fatalf("macro: %s", view.HTML)
	}
	// deleting the workshop informs ben
	admin.must("DELETE", fmt.Sprintf("/api/v1/x/content.events/events/%d", w.ID), nil, nil, 204)
	e.flushMail(t)
	if !c.find("ben@example.com", "Cancelled: Workshop") {
		t.Fatal("no cancellation mail")
	}
}

func TestEventOccurrenceOverrides(t *testing.T) {
	e := setup(t)
	c := &capture{}
	e.app.Mail.SetTransport(c)
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ann", "password": "Passw0rd!Passw0rd", "display_name": "ann", "email": "ann@example.com"}, nil, 201)
	ann := e.client()
	ann.login("ann", "Passw0rd!Passw0rd")
	var team cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &team, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", team.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	var calEl elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": team.ID, "type": "content.events", "title": "Team calendar", "body": map[string]any{"view": "list", "ical": true}}, &calEl, 201)
	base := fmt.Sprintf("/api/v1/x/content.events/calendars/%d", calEl.ID)
	vienna := mustLoc("Europe/Vienna")
	now := time.Now().In(vienna)
	start := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, vienna).AddDate(0, 0, 2)
	var ev struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", base+"/events", map[string]any{"title": "Jour fixe", "starts_at": start, "ends_at": start.Add(time.Hour), "tz": "Europe/Vienna",
		"rrule": "FREQ=DAILY;COUNT=5", "location": "Room 1", "registration": true}, &ev, 201)
	second := start.AddDate(0, 0, 1)
	date := second.Format("2006-01-02")
	ann.must("POST", fmt.Sprintf("/api/v1/x/content.events/events/%d/registrations", ev.ID), map[string]any{"occurrence": date}, nil, 200)
	occ := fmt.Sprintf("/api/v1/x/content.events/events/%d/occurrences/", ev.ID)

	if code := ann.do("PUT", occ+date, map[string]any{"title": "x"}, nil); code != 403 {
		t.Fatalf("viewer changed an occurrence: %d", code)
	}
	if code := admin.do("PUT", occ+"2001-01-01", map[string]any{"title": "x"}, nil); code != 404 {
		t.Fatalf("unknown occurrence: %d", code)
	}
	if code := admin.do("PUT", occ+date, map[string]any{"starts_at": second.AddDate(0, 2, 0)}, nil); code != 400 {
		t.Fatalf("start without end / too far: %d", code)
	}
	moved := second.Add(5 * time.Hour) // 14:00
	admin.must("PUT", occ+date, map[string]any{"title": "Jour fixe (special)", "location": "Aula", "starts_at": moved, "ends_at": moved.Add(90 * time.Minute)}, nil, 200)
	e.flushMail(t)
	if !c.find("ann@example.com", "Aula") {
		t.Error("registered person not told about the new time and place")
	}

	var list struct {
		Occurrences []struct {
			Date          string     `json:"date"`
			Start         time.Time  `json:"start"`
			Changed       bool       `json:"changed"`
			Title         string     `json:"title"`
			Location      string     `json:"location"`
			OriginalStart *time.Time `json:"original_start"`
			Registered    int        `json:"registered"`
		} `json:"occurrences"`
	}
	q := fmt.Sprintf("?from=%s&to=%s", start.AddDate(0, 0, -1).Format(time.RFC3339), start.AddDate(0, 0, 10).Format(time.RFC3339))
	q = strings.ReplaceAll(q, "+", "%2B")
	admin.must("GET", base+"/events"+q, nil, &list, 200)
	if len(list.Occurrences) != 5 {
		t.Fatalf("occurrences: %+v", list.Occurrences)
	}
	o := list.Occurrences[1]
	if o.Date != date || !o.Changed || o.Title != "Jour fixe (special)" || o.Location != "Aula" || !o.Start.Equal(moved) || o.OriginalStart == nil || o.Registered != 1 {
		t.Fatalf("changed occurrence: %+v", o)
	}
	if list.Occurrences[0].Changed || list.Occurrences[2].Title != "" {
		t.Fatalf("other occurrences changed: %+v", list.Occurrences)
	}
	// iCal: the change as an extra VEVENT with RECURRENCE-ID
	res, err := admin.http.Get(e.srv.URL + fmt.Sprintf("/api/v1/x/content.events/events/%d/ical", ev.ID))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	ics := string(b)
	for _, want := range []string{"BEGIN:VTIMEZONE", "RECURRENCE-ID;TZID=Europe/Vienna:" + second.Format("20060102T150405"),
		"DTSTART;TZID=Europe/Vienna:" + moved.Format("20060102T150405"), "SUMMARY:Jour fixe (special)", "LOCATION:Aula"} {
		if !strings.Contains(ics, want) {
			t.Errorf("ics lacks %q:\n%s", want, ics)
		}
	}
	if strings.Count(ics, "BEGIN:VEVENT") != 2 {
		t.Errorf("ics events:\n%s", ics)
	}
	// reset
	admin.must("DELETE", occ+date, nil, nil, 204)
	list.Occurrences = nil
	admin.must("GET", base+"/events"+q, nil, &list, 200)
	if list.Occurrences[1].Changed || !list.Occurrences[1].Start.Equal(second) {
		t.Fatalf("reset: %+v", list.Occurrences[1])
	}
}

// calendarParts returns the decoded text/calendar parts of the e-mails to addr.
func (c *capture) calendarParts(addr string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	re := regexp.MustCompile(`(?s)Content-Type: text/calendar; charset=utf-8; method=(\w+)\r\nContent-Transfer-Encoding: base64\r\n\r\n(.*?)\r\n--`)
	var out []string
	for _, m := range c.msgs {
		for _, r := range m.Recipients {
			if r != addr {
				continue
			}
			for _, p := range re.FindAllStringSubmatch(string(m.Data), -1) {
				b, _ := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(p[2]), ""))
				out = append(out, p[1]+"\n"+strings.ReplaceAll(string(b), "\r\n ", ""))
			}
		}
	}
	return out
}

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}
