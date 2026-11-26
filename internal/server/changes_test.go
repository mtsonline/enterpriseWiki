package server_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Notifications are never lost: what follows from a change is written with
// the change (same transaction) or queued durably and repeated.
func TestNotificationsAreNotLost(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.client()
	admin.login("admin", adminPW)
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 7, "inherit": true}, nil, 201)
	ids := map[string]int64{}
	clients := map[string]*client{}
	for _, name := range []string{"anna", "bert"} {
		var u struct {
			ID int64 `json:"id"`
		}
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": "Note-Passw0rd!", "display_name": name, "roles": []int64{}}, &u, 201)
		ids[name] = u.ID
		clients[name] = e.client()
		clients[name].login(name, "Note-Passw0rd!")
	}
	anna, bert := clients["anna"], ids["bert"]
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := e.app.DB.QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	mentions := func() int {
		return count(`SELECT count(*) FROM notifications WHERE user_id=$1 AND kind='mention'`, bert)
	}

	// a page with a mention: notified, nothing left in the queue
	var page elem
	anna.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Plan", "body": htmlBody("<p>Hello @bert, please read.</p>")}, &page, 201)
	if mentions() != 1 || count(`SELECT count(*) FROM change_events`) != 0 {
		t.Fatalf("after saving: %d mentions, %d queued changes", mentions(), count(`SELECT count(*) FROM change_events`))
	}

	// a hook that fails: the change stays queued and is repeated with the
	// account that made it
	fail, calls, actor := true, 0, int64(0)
	e.app.Content.OnChangeDurable("test", func(ctx context.Context, event, etype string, id int64) error {
		if etype != "element" || id != page.ID {
			return nil
		}
		calls++
		actor = identity.From(ctx).ID()
		if fail {
			return fmt.Errorf("not now")
		}
		return nil
	})
	anna.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"title": "Plan", "body": htmlBody("<p>Hello @bert, read again.</p>")}, nil, 200)
	var hooks []string
	if err := e.app.DB.QueryRow(ctx, `SELECT hooks FROM change_events WHERE entity_id=$1`, page.ID).Scan(&hooks); err != nil || len(hooks) != 1 || hooks[0] != "test" {
		t.Fatalf("queued change: %v %v", hooks, err)
	}
	if n, _ := e.app.Content.RetryChanges(ctx); n != 0 {
		t.Fatalf("repeated before its time: %d", n)
	}
	fail, actor = false, 0
	if _, err := e.app.DB.Exec(ctx, `UPDATE change_events SET next_at=now()`); err != nil {
		t.Fatal(err)
	}
	if n, err := e.app.Content.RetryChanges(ctx); n != 1 || err != nil || calls != 2 || actor != ids["anna"] {
		t.Fatalf("repeat: %d changes, %v, %d calls, actor %d", n, err, calls, actor)
	}
	if count(`SELECT count(*) FROM change_events`) != 0 || mentions() != 1 {
		t.Fatalf("after the repeat: %d queued, %d mentions (once per page)", count(`SELECT count(*) FROM change_events`), mentions())
	}

	// the server stopped between commit and hooks: the entry written with
	// the page is all that is left - the worker makes up for it
	if _, err := e.app.DB.Exec(ctx, `DELETE FROM notifications; DELETE FROM plugin_mentions`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.DB.Exec(ctx, `INSERT INTO change_events(event, entity_type, entity_id, actor_id, hooks, next_at) VALUES ('update','element',$1,$2,'{core.mentions,watch,test}', now())`, page.ID, ids["anna"]); err != nil {
		t.Fatal(err)
	}
	if n, err := e.app.Content.RetryChanges(ctx); n != 1 || err != nil || mentions() != 1 {
		t.Fatalf("after a crash: %d changes, %v, %d mentions", n, err, mentions())
	}

	// comment with a mention and a reaction: stored with their notifications ...
	var com struct{ ID int64 }
	anna.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", page.ID), map[string]any{"body": "@bert what do you think?"}, &com, 201)
	if n := count(`SELECT count(*) FROM notifications WHERE user_id=$1 AND kind='mention' AND entity_type='comment'`, bert); n != 1 {
		t.Fatalf("mention in a comment: %d", n)
	}
	bertC := clients["bert"]
	bertC.must("POST", fmt.Sprintf("/api/v1/x/core.reactions/element/%d", page.ID), map[string]any{"emoji": "👍", "on": true}, nil, 200)
	if n := count(`SELECT count(*) FROM notifications WHERE user_id=$1 AND kind='reaction'`, ids["anna"]); n != 1 {
		t.Fatalf("reaction: %d", n)
	}
	// ... or not at all: while notifications cannot be written, neither
	// the comment nor the reaction is stored (and the caller learns it)
	if _, err := e.app.DB.Exec(ctx, `ALTER TABLE notifications RENAME TO notifications_away`); err != nil {
		t.Fatal(err)
	}
	comments, reactions := count(`SELECT count(*) FROM plugin_comments`), count(`SELECT count(*) FROM plugin_reactions`)
	if st, _ := anna.send("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", page.ID), map[string]any{"body": "@bert once more"}); st < 500 {
		t.Errorf("comment without its notification: %d", st)
	}
	var page2 elem
	_, _ = e.app.DB.Exec(ctx, `ALTER TABLE notifications_away RENAME TO notifications`)
	anna.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Two", "body": htmlBody("<p>x</p>")}, &page2, 201)
	_, _ = e.app.DB.Exec(ctx, `ALTER TABLE notifications RENAME TO notifications_away`)
	if st, _ := bertC.send("POST", fmt.Sprintf("/api/v1/x/core.reactions/element/%d", page2.ID), map[string]any{"emoji": "👍", "on": true}); st < 500 {
		t.Errorf("reaction without its notification: %d", st)
	}
	if _, err := e.app.DB.Exec(ctx, `ALTER TABLE notifications_away RENAME TO notifications`); err != nil {
		t.Fatal(err)
	}
	if c2, r2 := count(`SELECT count(*) FROM plugin_comments`), count(`SELECT count(*) FROM plugin_reactions`); c2 != comments || r2 != reactions {
		t.Fatalf("stored without notification: comments %d -> %d, reactions %d -> %d", comments, c2, reactions, r2)
	}
}

// Every change of content writes its queue entry in the same transaction:
// while the queue cannot be written, the change is not stored either -
// there is no moment in which a stored change has no entry.
func TestChangesAndTheirQueueEntryAreOne(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	admin := e.client()
	admin.login("admin", adminPW)
	var a, b cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "A"}}, &a, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "B"}}, &b, 201)
	var page, cal elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": a.ID, "type": "core.html", "title": "Page", "body": htmlBody("<p>x</p>")}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": a.ID, "type": "content.events", "title": "Dates", "body": map[string]any{"view": "list"}}, &cal, 201)
	// a hook that never succeeds keeps every entry in the queue
	e.app.Content.OnChangeDurable("test", func(context.Context, string, string, int64) error { return fmt.Errorf("later") })
	state := func() string {
		t.Helper()
		var s string
		if err := e.app.DB.QueryRow(ctx, `SELECT concat_ws('|',
			(SELECT string_agg(concat_ws(',', id, category_id, deleted_at IS NOT NULL, COALESCE(short_url, ''), titles::text), ';' ORDER BY id) FROM elements),
			(SELECT string_agg(concat_ws(',', id, parent_id, deleted_at IS NOT NULL, archived_at IS NOT NULL, titles::text, space::text), ';' ORDER BY id) FROM categories),
			(SELECT count(*) FROM element_revisions), (SELECT count(*) FROM plugin_events))`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	start := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Hour)
	ops := []struct {
		name, method, path string
		body               any
	}{
		{"save a page", "PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"title": "Page", "body": htmlBody("<p>y</p>")}},
		{"create a page", "POST", "/api/v1/elements", map[string]any{"category_id": a.ID, "type": "core.html", "title": "New", "body": htmlBody("<p>x</p>")}},
		{"move a page", "POST", fmt.Sprintf("/api/v1/elements/%d/move", page.ID), map[string]any{"category_id": b.ID}},
		{"calendar event", "POST", fmt.Sprintf("/api/v1/x/content.events/calendars/%d/events", cal.ID), map[string]any{"title": "Yoga", "starts_at": start, "ends_at": start.Add(time.Hour)}},
		{"delete a page", "DELETE", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil},
		{"rename a category", "PUT", fmt.Sprintf("/api/v1/categories/%d", b.ID), map[string]any{"titles": map[string]string{"en": "B2"}}},
		{"move a category", "POST", fmt.Sprintf("/api/v1/categories/%d/move", b.ID), map[string]any{"parent_id": a.ID}},
		{"archive a category", "POST", fmt.Sprintf("/api/v1/categories/%d/archive", b.ID), map[string]any{"archived": true}},
		{"create a category", "POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "C"}}},
		{"delete a category", "DELETE", fmt.Sprintf("/api/v1/categories/%d", b.ID), nil},
	}
	// 1. without the queue nothing is stored
	if _, err := e.app.DB.Exec(ctx, `ALTER TABLE change_events RENAME TO change_events_away`); err != nil {
		t.Fatal(err)
	}
	before := state()
	for _, op := range ops {
		st, err := admin.send(op.method, op.path, op.body)
		if err != nil || st < 500 {
			t.Errorf("%s without the queue: status %d %v", op.name, st, err)
		}
		if now := state(); now != before {
			t.Fatalf("%s was stored without its queue entry:\n%s\n%s", op.name, before, now)
		}
	}
	if _, err := e.app.DB.Exec(ctx, `ALTER TABLE change_events_away RENAME TO change_events`); err != nil {
		t.Fatal(err)
	}
	// 2. with the queue every one of them is stored and leaves its entry
	for _, op := range ops {
		var n0, n1 int
		_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM change_events`).Scan(&n0)
		if st, err := admin.send(op.method, op.path, op.body); err != nil || st >= 300 {
			t.Fatalf("%s: status %d %v", op.name, st, err)
		}
		_ = e.app.DB.QueryRow(ctx, `SELECT count(*) FROM change_events WHERE 'test' = ANY(hooks)`).Scan(&n1)
		if n1 <= n0 {
			t.Errorf("%s left no queue entry (%d -> %d)", op.name, n0, n1)
		}
	}
	if state() == before {
		t.Fatal("nothing changed")
	}
}
