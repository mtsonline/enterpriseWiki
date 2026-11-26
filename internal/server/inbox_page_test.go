package server_test

import (
	"fmt"
	"testing"
)

// TestInboxPages: the page of all notifications lists only the caller's own,
// newest first and page by page without gaps or repeats, optionally only the
// unread ones; who caused one carries an id only when the reader may open
// that person. A cursor of somebody else's notification yields nothing.
func TestInboxPages(t *testing.T) {
	e := setup(t)
	ctx := t.Context()
	admin := e.client()
	admin.login("admin", adminPW)
	const pw = "Passw0rd!Passw0rd"
	var g, ida, idb, idc idResp
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Inbox team"}, &g, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ina", "password": pw, "display_name": "Ina", "groups": []int64{g.ID}}, &ida, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ben", "password": pw, "display_name": "Ben", "groups": []int64{g.ID}}, &idb, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "cid", "password": pw, "display_name": "Cid"}, &idc, 201)
	ina, cid := e.client(), e.client()
	ina.login("ina", pw)
	cid.login("cid", pw)

	// 7 for Ina: even ones by Ben (same group), odd ones by Cid (a stranger);
	// all at the same moment, so the id decides the order
	for i := 0; i < 7; i++ {
		actor := idb.ID
		if i%2 == 1 {
			actor = idc.ID
		}
		if _, err := e.app.DB.Exec(ctx, `INSERT INTO notifications (user_id, kind, title, actor_id, created_at, read_at)
			VALUES ($1, 'test', $2, $3, '2026-11-01T10:00:00Z', CASE WHEN $4 THEN now() END)`, ida.ID, fmt.Sprintf("N%d", i), actor, i < 3); err != nil {
			t.Fatal(err)
		}
	}
	var foreign int64
	if err := e.app.DB.QueryRow(ctx, `INSERT INTO notifications (user_id, kind, title) VALUES ($1, 'test', 'not yours') RETURNING id`, idc.ID).Scan(&foreign); err != nil {
		t.Fatal(err)
	}

	type item struct {
		ID      int64  `json:"id"`
		Title   string `json:"title"`
		Actor   string `json:"actor"`
		ActorID *int64 `json:"actor_id"`
	}
	type page struct {
		Items  []item `json:"items"`
		Unread int    `json:"unread"`
		More   bool   `json:"more"`
	}
	get := func(c *client, q string) page {
		t.Helper()
		var p page
		c.must("GET", "/api/v1/me/inbox?"+q, nil, &p, 200)
		return p
	}
	var titles []string
	var before int64
	for n := 0; ; n++ {
		q := "limit=3"
		if before > 0 {
			q += fmt.Sprintf("&before=%d", before)
		}
		p := get(ina, q)
		if p.Unread != 4 {
			t.Fatalf("unread: %d", p.Unread)
		}
		for _, it := range p.Items {
			titles = append(titles, it.Title)
			switch it.Actor {
			case "Ben":
				if it.ActorID == nil || *it.ActorID != idb.ID {
					t.Fatalf("Ben may be opened: %+v", it)
				}
			case "Cid":
				if it.ActorID != nil {
					t.Fatalf("Cid must not be opened: %+v", it)
				}
			default:
				t.Fatalf("actor: %+v", it)
			}
		}
		if !p.More {
			if n != 2 || len(p.Items) != 1 {
				t.Fatalf("last page %d: %+v", n, p)
			}
			break
		}
		before = p.Items[len(p.Items)-1].ID
	}
	if fmt.Sprint(titles) != "[N6 N5 N4 N3 N2 N1 N0]" {
		t.Fatalf("order: %v", titles)
	}
	if p := get(ina, "unread=1&limit=50"); len(p.Items) != 4 || p.More || p.Items[3].Title != "N3" {
		t.Fatalf("unread only: %+v", p)
	}
	// somebody else's notification as the cursor: nothing, no hint about it
	if p := get(ina, fmt.Sprintf("before=%d", foreign)); len(p.Items) != 0 {
		t.Fatalf("foreign cursor: %+v", p)
	}
	if p := get(cid, "limit=50"); len(p.Items) != 1 || p.Items[0].Title != "not yours" {
		t.Fatalf("cid sees: %+v", p)
	}
	// a guest has no notifications
	if code := e.client().do("GET", "/api/v1/me/inbox", nil, nil); code != 401 {
		t.Fatalf("guest: %d", code)
	}
}
