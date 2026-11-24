package server_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestPageDates: a date block with a title is an appointment of the page
// (kept as data, with the time of the wiki's time zone); the report of
// dates lists it; a calendar with a category or pages as source shows the
// appointments (and, if chosen, the open tasks with a due day) read-only,
// only to readers who may see the page.
func TestPageDates(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"mail": map[string]any{"digest_timezone": "Europe/Vienna"}}, nil, 200)
	var team, sub, secret, site obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}, "guest_visible": true}, &team, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Sub"}, "parent_id": team.ID}, &sub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Secret"}}, &secret, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Site"}, "guest_visible": true}, &site, 201)
	md := func(src string) map[string]any { return map[string]any{"format": "markdown", "source": src} }
	var page, subPage, hidden obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": team.ID, "type": "core.html", "title": "Plan", "body": md(
		"Meeting :date{value=2026-12-01 label=\"Team meeting\" time=10:00 end=11:30}, deadline :date{value=2026-12-03}.\n\n" +
			"- [ ] Send the minutes @admin :date{value=2026-12-02}\n\n::dates{range=all}\n")}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "Sub page", "body": md(
		":date{value=2026-12-04 label=\"Sub party\"}\n")}, &subPage, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "core.html", "title": "Secret page", "body": md(
		":date{value=2026-12-05 label=\"Board dinner\" time=19:00}\n")}, &hidden, 201)
	// invalid times are refused when shown, never kept
	var bad obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": team.ID, "type": "core.html", "title": "Bad", "body": md(
		":date{value=2026-12-06 label=\"Bad\" time=25:00} :date{value=2026-12-06 label=\"Bad end\" end=10:00}\n")}, &bad, 201)

	type result struct {
		Items []struct {
			ElementID int64          `json:"element_id"`
			Data      map[string]any `json:"data"`
		} `json:"items"`
	}
	var r result
	admin.must("GET", "/api/v1/data/core.macros.date?sort=when", nil, &r, 200)
	var got []string
	for _, it := range r.Items {
		got = append(got, fmt.Sprintf("%v %v %v %v", it.Data["title"], it.Data["day"], it.Data["at"], it.Data["end"]))
	}
	want := "Team meeting 2026-12-01 2026-12-01T09:00:00Z 2026-12-01T10:30:00Z|Sub party 2026-12-04 <nil> <nil>|Board dinner 2026-12-05 2026-12-05T18:00:00Z <nil>"
	if strings.Join(got, "|") != want {
		t.Fatalf("dates: %v", got)
	}
	var v struct {
		HTML string `json:"html"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", page.ID), nil, &v, 200)
	for _, w := range []string{`datetime="2026-12-01T10:00+01:00">1 December 2026, 10:00–11:30</time>`, `<th scope="row">Team meeting</th>`, "Board dinner", "Sub party"} {
		if !strings.Contains(v.HTML, w) {
			t.Fatalf("page for the admin, missing %q: %s", w, v.HTML)
		}
	}
	guest := e.client()
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", page.ID), nil, &v, 200)
	if strings.Contains(v.HTML, "Board dinner") || !strings.Contains(v.HTML, "Team meeting") {
		t.Fatalf("report for a guest: %s", v.HTML)
	}

	// a calendar with the category (with subcategories), a page of the
	// secret category and the tasks
	var cal obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": site.ID, "type": "content.events", "title": "Dates of pages",
		"body": map[string]any{"view": "list", "page_tasks": true, "more_sources": fmt.Sprintf("category:%d+sub; elements:%d", team.ID, hidden.ID)}}, &cal, 201)
	if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": site.ID, "type": "content.events", "title": "Bad",
		"body": map[string]any{"more_sources": fmt.Sprintf("-category:%d", team.ID)}}, nil); code != 400 {
		t.Fatalf("an excluding source accepted: %d", code)
	}
	type evList struct {
		Events []struct {
			ID        int64  `json:"id"`
			Title     string `json:"title"`
			AllDay    bool   `json:"all_day"`
			StartsAt  string `json:"starts_at"`
			External  bool   `json:"external"`
			PageID    int64  `json:"page_id"`
			PageKind  string `json:"page_kind"`
			PageTitle string `json:"page_title"`
		} `json:"events"`
	}
	events := func(c *client) map[string]string {
		var l evList
		c.must("GET", fmt.Sprintf("/api/v1/x/content.events/calendars/%d/events?from=2026-11-25&to=2026-12-31", cal.ID), nil, &l, 200)
		out := map[string]string{}
		for _, x := range l.Events {
			if !x.External || x.ID >= 0 || x.PageID == 0 {
				t.Fatalf("an event of a page: %+v", x)
			}
			out[x.Title] = fmt.Sprintf("%s %v %d %s %s", x.StartsAt, x.AllDay, x.PageID, x.PageKind, x.PageTitle)
		}
		return out
	}
	all := events(admin)
	if all["Team meeting"] != fmt.Sprintf("2026-12-01T09:00:00Z false %d date Plan", page.ID) || all["Sub party"] == "" || all["Board dinner"] == "" ||
		all["Send the minutes @admin"] != fmt.Sprintf("2026-12-02T00:00:00Z true %d task Plan", page.ID) || len(all) != 4 {
		t.Fatalf("calendar for the admin: %v", all)
	}
	pub := events(guest)
	// (the subcategory is not visible to guests)
	if _, ok := pub["Board dinner"]; ok || pub["Team meeting"] == "" || len(pub) != 2 {
		t.Fatalf("calendar for a guest: %v", pub)
	}
	// the block of upcoming events with pages as source (a date soon)
	soon := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	var soonPage, up obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": team.ID, "type": "core.html", "title": "Soon", "body": md(
		":date{value=" + soon + " label=\"Soon okapi\" time=09:15}\n\n- [ ] Soon task @admin :date{value=" + soon + "}\n")}, &soonPage, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": site.ID, "type": "core.html", "title": "Upcoming", "body": md(
		fmt.Sprintf("::events{source=\"category:%d\" days=60 limit=10}\n", team.ID))}, &up, 201)
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", up.ID), nil, &v, 200)
	if !strings.Contains(v.HTML, fmt.Sprintf(`<a href="/e/%d">`, soonPage.ID)) || !strings.Contains(v.HTML, "Soon okapi") || strings.Contains(v.HTML, "Board dinner") || strings.Contains(v.HTML, "Soon task") {
		t.Fatalf("upcoming from a category (without tasks): %s", v.HTML)
	}
}
