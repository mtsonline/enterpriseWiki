package server_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func (e *env) guestPage(path, lang string) (string, http.Header) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b), res.Header
}

func TestPrerenderForGuests(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var pub, hidden cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Public", "de": "Öffentlich"}, "descriptions": map[string]string{"en": "For everyone"}}, &pub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": pub.ID, "titles": map[string]string{"en": "Board"}}, &hidden, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", pub.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": false}, nil, 201)
	var page, secret elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "core.html", "title": "Opening hours <&>",
		"body": htmlBody("<p>We are open <strong>Monday to Friday</strong> from 8 to 17.</p><script>alert(1)</script>"), "short_url": "hours"}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": hidden.ID, "type": "core.html", "title": "Secret minutes", "body": htmlBody("<p>top secret</p>")}, &secret, 201)

	body, hdr := e.guestPage(fmt.Sprintf("/e/%d", page.ID), "de-AT,de;q=0.9")
	for _, want := range []string{`<html lang="en">`, `<title>Opening hours &lt;&amp;&gt; – Enterprise Wiki</title>`, `<meta name="description" content="We are open Monday to Friday from 8 to 17.">`,
		`rel="canonical"`, `<meta property="og:type" content="article">`, `<strong>Monday to Friday</strong>`, `<h1>Opening hours &lt;&amp;&gt;</h1>`} {
		if !strings.Contains(body, want) {
			t.Errorf("guest page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<script>alert") {
		t.Error("script from content in prerendered page")
	}
	if hdr.Get("Content-Language") != "en" {
		t.Errorf("content language %q (only an english revision exists)", hdr.Get("Content-Language"))
	}
	var more elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "core.html", "title": "Contact", "body": htmlBody("<p>Call us</p>")}, &more, 201)
	// short URL, category with german title (first element as its content), no hidden subcategories
	body, _ = e.guestPage("/hours", "")
	if !strings.Contains(body, "Opening hours") {
		t.Error("short URL not prerendered")
	}
	body, _ = e.guestPage(fmt.Sprintf("/c/%d", pub.ID), "de")
	if !strings.Contains(body, "<h1>Öffentlich</h1>") || !strings.Contains(body, "Monday to Friday") || !strings.Contains(body, fmt.Sprintf(`<a href="/e/%d">Contact</a>`, more.ID)) || strings.Contains(body, "Board") {
		t.Errorf("category page:\n%s", body)
	}
	// hidden pages and other routes are not prerendered
	for _, p := range []string{fmt.Sprintf("/e/%d", secret.ID), "/login", "/admin", "/nothing"} {
		if body, _ := e.guestPage(p, ""); strings.Contains(body, "ew-prerender") || strings.Contains(body, "top secret") {
			t.Errorf("%s prerendered for guests", p)
		}
	}
	// logged in users get the plain app
	if code := admin.do("GET", fmt.Sprintf("/e/%d", page.ID), nil, nil); code != 200 {
		t.Fatal(code)
	}
	// API: guest view, also when asked by an administrator
	var r struct {
		Available bool `json:"available"`
		Page      struct {
			Title string `json:"title"`
			HTML  string `json:"html"`
		} `json:"page"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/prerender?path=/e/%d", secret.ID), nil, &r, 200)
	if r.Available {
		t.Error("hidden element available in guest view")
	}
	admin.must("GET", "/api/v1/prerender?path=/hours", nil, &r, 200)
	if !r.Available || r.Page.Title != "Opening hours <&>" {
		t.Errorf("api: %+v", r)
	}
	// switched off
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": map[string]any{"site_name": "Enterprise Wiki", "template": "theme.wiki", "prerender": false}}, nil, 200)
	if body, _ := e.guestPage("/hours", ""); strings.Contains(body, "ew-prerender") {
		t.Error("prerender not switched off")
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
