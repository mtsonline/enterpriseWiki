package server_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/watch"
)

func TestWatches(t *testing.T) {
	e := setup(t)
	// no quiet period (expert setting core.watch.quiet_seconds)
	if err := e.app.Config.Update(func(c *config.Config) error {
		if c.Limits == nil {
			c.Limits = map[string]int64{}
		}
		c.Limits["core.watch.quiet_seconds"] = 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c := &capture{}
	e.app.Mail.SetTransport(c)
	admin := e.client()
	admin.login("admin", adminPW)
	var root, sub, secret cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Handbook"}}, &root, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": root.ID, "titles": map[string]string{"en": "HR"}}, &sub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": root.ID, "titles": map[string]string{"en": "Board"}}, &secret, 201)
	// everybody sees Handbook and HR, nobody (but admins) Board
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": false}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", sub.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	for _, n := range []string{"wanda", "lou"} {
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": n, "password": "Passw0rd!Passw0rd", "display_name": n, "email": n + "@example.com"}, nil, 201)
	}
	wanda, lou := e.client(), e.client()
	wanda.login("wanda", "Passw0rd!Passw0rd")
	lou.login("lou", "Passw0rd!Passw0rd")

	// wanda watches the whole handbook with content, lou one page with link only
	var w struct {
		ID   int64  `json:"id"`
		Mode string `json:"mode"`
	}
	wanda.must("PUT", "/api/v1/me/watches", map[string]any{"target_type": "category", "target_id": root.ID, "mode": "content"}, &w, 200)
	if w.Mode != "content" {
		t.Fatalf("watch: %+v", w)
	}
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "title": "Leave policy", "body": htmlBody("<p>Thirty days</p>")}, &page, 201)
	lou.must("PUT", "/api/v1/me/watches", map[string]any{"target_type": "element", "target_id": page.ID}, nil, 200)
	var st struct {
		Watch *struct {
			Mode string `json:"mode"`
		} `json:"watch"`
		Enabled bool `json:"enabled"`
	}
	lou.must("GET", fmt.Sprintf("/api/v1/me/watches?target_type=element&target_id=%d", page.ID), nil, &st, 200)
	if st.Watch == nil || st.Watch.Mode != "link" || !st.Enabled {
		t.Fatalf("status: %+v", st)
	}
	// several saves in a row → one e-mail per user
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Leave policy", "body": htmlBody("<p>Thirty-two days</p>"), "base_rev": 1}, nil, 200)
	e.flushWatches(t)
	if !c.find("wanda@example.com", "Thirty-two days") || !c.find("lou@example.com", "Leave policy") || c.find("lou@example.com", "Thirty-two days") {
		t.Fatalf("watch mails: %d", c.count())
	}
	if c.count() != 2 {
		t.Fatalf("expected one coalesced mail per user, got %d", c.count())
	}
	// own changes are not reported; hidden pages are not reported
	n := c.count()
	wanda.must("PUT", "/api/v1/me/watches", map[string]any{"target_type": "element", "target_id": page.ID, "mode": "link"}, nil, 200)
	var hidden elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": secret.ID, "type": "core.html", "title": "Salaries", "body": htmlBody("<p>x</p>")}, &hidden, 201)
	e.flushWatches(t)
	if c.count() != n {
		t.Fatalf("notified about a hidden page (%d mails)", c.count()-n)
	}
	// list + delete
	var list struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	wanda.must("GET", "/api/v1/me/watches", nil, &list, 200)
	if len(list.Items) != 2 {
		t.Fatalf("list: %+v", list)
	}
	for _, it := range list.Items {
		wanda.must("DELETE", fmt.Sprintf("/api/v1/me/watches/%d", it.ID), nil, nil, 204)
	}
	if lou.do("DELETE", fmt.Sprintf("/api/v1/me/watches/%d", list.Items[0].ID), nil, nil) != 404 {
		t.Fatal("deleted someone else's watch")
	}
	// guests cannot watch; nobody can watch what they cannot see
	if e.client().do("GET", "/api/v1/me/watches", nil, nil) != 401 {
		t.Fatal("guest watch list")
	}
	if code := lou.do("PUT", "/api/v1/me/watches", map[string]any{"target_type": "category", "target_id": secret.ID}, nil); code != 404 && code != 403 {
		t.Fatalf("watching a hidden category: %d", code)
	}
}

func TestWatchDigests(t *testing.T) {
	e := setup(t)
	// no quiet period (expert setting core.watch.quiet_seconds)
	if err := e.app.Config.Update(func(c *config.Config) error {
		if c.Limits == nil {
			c.Limits = map[string]int64{}
		}
		c.Limits["core.watch.quiet_seconds"] = 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c := &capture{}
	e.app.Mail.SetTransport(c)
	admin := e.client()
	admin.login("admin", adminPW)
	var root cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "News"}}, &root, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	for _, n := range []string{"dora", "ivo"} {
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": n, "password": "Passw0rd!Passw0rd", "display_name": n, "email": n + "@example.com"}, nil, 201)
	}
	dora, ivo := e.client(), e.client()
	dora.login("dora", "Passw0rd!Passw0rd")
	ivo.login("ivo", "Passw0rd!Passw0rd")
	if code := dora.do("PUT", "/api/v1/me/watches", map[string]any{"target_type": "category", "target_id": root.ID, "frequency": "hourly"}, nil); code != 400 {
		t.Fatalf("invalid frequency: %d", code)
	}
	var w struct {
		Frequency string `json:"frequency"`
	}
	dora.must("PUT", "/api/v1/me/watches", map[string]any{"target_type": "category", "target_id": root.ID, "frequency": "daily"}, &w, 200)
	if w.Frequency != "daily" {
		t.Fatalf("watch: %+v", w)
	}
	ivo.must("PUT", "/api/v1/me/watches", map[string]any{"target_type": "category", "target_id": root.ID}, nil, 200)
	var p1 elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Menu of the week", "body": htmlBody("<p>Soup</p>")}, &p1, 201)
	e.flushWatches(t)
	if !c.find("ivo@example.com", "Menu of the week") {
		t.Error("instant watcher got no e-mail")
	}
	if c.find("dora@example.com", "Menu of the week") {
		t.Fatal("daily watcher got an e-mail right away")
	}
	// the change was recorded before the last digest time → part of the daily digest
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE watch_events SET first_at = first_at - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	e.flushWatches(t)
	if !c.find("dora@example.com", "Menu of the week") || !c.find("dora@example.com", "Daily digest") {
		t.Error("daily digest missing")
	}
	// boundaries
	m := config.Defaults().Mail
	m.DigestTimezone = "Europe/Vienna"
	vie := mustLoc("Europe/Vienna")
	d, wk := watch.DigestBoundaries(m, time.Date(2026, 11, 4, 6, 30, 0, 0, vie)) // Wednesday before 7:00
	if !d.Equal(time.Date(2026, 11, 3, 7, 0, 0, 0, vie)) || !wk.Equal(time.Date(2026, 11, 2, 7, 0, 0, 0, vie)) {
		t.Errorf("boundaries: %v %v", d, wk)
	}
}

func (e *env) flushWatches(t *testing.T) {
	t.Helper()
	if err := e.app.Workers.RunOnce(context.Background(), "watch"); err != nil {
		t.Fatal(err)
	}
	e.flushMail(t)
}
