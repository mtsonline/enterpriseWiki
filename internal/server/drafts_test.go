package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDraftsAndTemplates(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	pw := "Passw0rd!Passw0rd"
	user := func(name string) (*client, int64) {
		var u struct {
			ID int64 `json:"id"`
		}
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": pw, "email": name + "@example.com"}, &u, 201)
		c := e.client()
		c.login(name, pw)
		return c, u.ID
	}
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "News"}}, &c, 201)
	frank, frankID := user("frank")
	gina, ginaID := user("gina")
	rita, ritaID := user("rita")
	for _, uid := range []int64{frankID, ginaID} {
		admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": uid, "preset": "editor"}, nil, 201)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": ritaID, "preset": "reader"}, nil, 201)

	// a new element as draft: invisible for readers, editable together
	var d struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		UpdatedBy string `json:"updated_by"`
	}
	frank.must("POST", "/api/v1/drafts", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Draft news", "body": htmlBody("<p>secret plan</p>")}, &d, 201)
	if code := rita.do("GET", fmt.Sprintf("/api/v1/drafts/%d", d.ID), nil, nil); code != 404 {
		t.Fatalf("reader sees a draft: %d", code)
	}
	gina.must("PUT", fmt.Sprintf("/api/v1/drafts/%d", d.ID), map[string]any{"title": "Draft news (edited)", "body": htmlBody("<p>plan</p>")}, &d, 200)
	var list []struct {
		ID int64 `json:"id"`
	}
	frank.must("GET", fmt.Sprintf("/api/v1/drafts?category=%d", c.ID), nil, &list, 200)
	rita.must("GET", fmt.Sprintf("/api/v1/drafts?category=%d", c.ID), nil, &list, 200)
	if len(list) != 0 {
		t.Fatalf("reader lists drafts: %+v", list)
	}
	var els struct {
		Total int `json:"total"`
	}
	rita.must("GET", fmt.Sprintf("/api/v1/elements?category=%d", c.ID), nil, &els, 200)
	if els.Total != 0 {
		t.Fatalf("draft listed as element: %+v", els)
	}
	// publish: the element appears, the draft is gone
	var pub struct {
		Element struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			HTML  string `json:"html"`
		} `json:"element"`
	}
	frank.must("POST", fmt.Sprintf("/api/v1/drafts/%d/publish", d.ID), map[string]any{}, &pub, 200)
	if pub.Element.Title != "Draft news (edited)" || !strings.Contains(pub.Element.HTML, "plan") {
		t.Fatalf("published: %+v", pub)
	}
	if code := frank.do("GET", fmt.Sprintf("/api/v1/drafts/%d", d.ID), nil, nil); code != 404 {
		t.Fatalf("draft still there: %d", code)
	}
	elID := pub.Element.ID
	// changes as draft: readers keep seeing the published version
	rita.must("PUT", "/api/v1/me/watches", map[string]any{"target_type": "element", "target_id": elID}, nil, 200)
	frank.must("POST", "/api/v1/drafts", map[string]any{"element_id": elID, "lang": "en", "title": "Draft news", "body": htmlBody("<p>new version</p>"), "base_rev": 1}, &d, 201)
	var el elem
	rita.must("GET", fmt.Sprintf("/api/v1/elements/%d", elID), nil, &el, 200)
	if strings.Contains(el.HTML, "new version") {
		t.Fatal("reader sees the draft")
	}
	// scheduled publication, without notifying the watchers
	at := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	var sch struct {
		Scheduled *time.Time `json:"scheduled"`
	}
	frank.must("POST", fmt.Sprintf("/api/v1/drafts/%d/publish", d.ID), map[string]any{"at": at, "notify": false}, &sch, 200)
	if sch.Scheduled == nil {
		t.Fatal("not scheduled")
	}
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE element_drafts SET publish_at = now() - interval '1 minute' WHERE id=$1`, d.ID); err != nil {
		t.Fatal(err)
	}
	var before int
	_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM watch_events WHERE user_id=$1`, ritaID).Scan(&before)
	if err := e.app.Workers.RunOnce(context.Background(), "drafts.publish"); err != nil {
		t.Fatal(err)
	}
	rita.must("GET", fmt.Sprintf("/api/v1/elements/%d", elID), nil, &el, 200)
	if !strings.Contains(el.HTML, "new version") || el.Rev != 2 {
		t.Fatalf("scheduled draft not published: rev %d %s", el.Rev, el.HTML)
	}
	var after int
	_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM watch_events WHERE user_id=$1`, ritaID).Scan(&after)
	if after != before {
		t.Fatal("watchers notified although notify=false")
	}

	// templates: personal ones, global ones only with permission
	var tpl struct {
		ID     int64 `json:"id"`
		Global bool  `json:"global"`
	}
	frank.must("POST", "/api/v1/templates", map[string]any{"name": "Meeting notes", "type": "core.html", "title": "Meeting", "body": htmlBody("<h2>Agenda</h2>")}, &tpl, 201)
	var tpls []struct {
		ID   int64           `json:"id"`
		Body json.RawMessage `json:"body"`
	}
	gina.must("GET", "/api/v1/templates", nil, &tpls, 200)
	if len(tpls) != 0 {
		t.Fatalf("personal template visible to others: %+v", tpls)
	}
	if code := frank.do("POST", "/api/v1/templates", map[string]any{"name": "For all", "type": "core.html", "body": htmlBody("x"), "global": true}, nil); code != 403 {
		t.Fatalf("global template without permission: %d", code)
	}
	admin.must("POST", "/api/v1/templates", map[string]any{"name": "Official", "type": "core.html", "body": htmlBody("<p>logo</p>"), "global": true}, &tpl, 201)
	gina.must("GET", "/api/v1/templates?type=core.html", nil, &tpls, 200)
	if len(tpls) != 1 {
		t.Fatalf("global template: %+v", tpls)
	}
	gina.must("GET", fmt.Sprintf("/api/v1/templates/%d", tpl.ID), nil, &tpls[0], 200)
	if !strings.Contains(string(tpls[0].Body), "logo") {
		t.Fatalf("template body: %s", tpls[0].Body)
	}
	if code := gina.do("DELETE", fmt.Sprintf("/api/v1/templates/%d", tpl.ID), nil, nil); code != 403 {
		t.Fatalf("deleted a global template without permission: %d", code)
	}
}
