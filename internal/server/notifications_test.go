package server_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

func TestNotificationsShareMentionsAssignedWatches(t *testing.T) {
	e := setup(t)
	mc := &capture{}
	e.app.Mail.SetTransport(mc)
	if err := e.app.Config.Update(func(cfg *config.Config) error { cfg.Mail.From = "wiki@example.com"; return nil }); err != nil {
		t.Fatal(err)
	}
	admin := e.client()
	admin.login("admin", adminPW)
	pw := "Passw0rd!Passw0rd"
	user := func(name string) (*client, int64) {
		var u struct {
			ID int64 `json:"id"`
		}
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": name, "password": pw, "display_name": strings.ToUpper(name[:1]) + name[1:], "email": name + "@example.com"}, &u, 201)
		c := e.client()
		c.login(name, pw)
		return c, u.ID
	}
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team"}}, &c, 201)
	anna, annaID := user("anna")
	ben, benID := user("ben")
	_, carlID := user("carl") // no access
	var grp struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Staff"}, &grp, 201)
	// by default people share only with their own groups: anna gets a role
	// that may list all groups (she is not in the group; see TestListRights)
	var sharers struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/roles", map[string]any{"name": "Sharers", "permissions": []string{"core.groups.list_all"}}, &sharers, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", annaID), map[string]any{"roles": []int64{sharers.ID}}, nil, 200)
	anna.login("anna", pw) // the new role is in the next session
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", benID), map[string]any{"groups": []int64{grp.ID}}, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", carlID), map[string]any{"groups": []int64{grp.ID}}, nil, 200)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": annaID, "preset": "editor"}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "user", "subject_id": benID, "preset": "reader"}, nil, 201)
	var page elem
	anna.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Plan", "body": htmlBody("<p>Hello</p>")}, &page, 201)

	// share with a group: carl may not see the page and is skipped
	var res struct {
		Recipients    int      `json:"recipients"`
		WithoutAccess []string `json:"without_access"`
	}
	anna.must("POST", "/api/v1/x/core.share/check", map[string]any{"target_type": "element", "target_id": page.ID, "groups": []int64{grp.ID}}, &res, 200)
	if res.Recipients != 1 || len(res.WithoutAccess) != 1 || res.WithoutAccess[0] != "Carl" {
		t.Fatalf("check: %+v", res)
	}
	anna.must("POST", "/api/v1/x/core.share/share", map[string]any{"target_type": "element", "target_id": page.ID, "groups": []int64{grp.ID}, "message": "Please read"}, &res, 200)
	var inbox struct {
		Items []struct {
			ID    int64  `json:"id"`
			Kind  string `json:"kind"`
			Title string `json:"title"`
			Link  string `json:"link"`
		} `json:"items"`
		Unread int `json:"unread"`
	}
	ben.must("GET", "/api/v1/me/inbox", nil, &inbox, 200)
	if inbox.Unread != 1 || inbox.Items[0].Kind != "share" || inbox.Items[0].Link != fmt.Sprintf("/e/%d", page.ID) {
		t.Fatalf("ben's notifications: %+v", inbox)
	}
	if err := e.app.Workers.RunOnce(context.Background(), "mail"); err != nil {
		t.Fatal(err)
	}
	if !mc.find("ben@example.com", "Please read") || mc.find("carl@example.com", "Plan") {
		t.Fatal("share e-mail")
	}
	ben.must("POST", "/api/v1/me/inbox/read", map[string]any{}, nil, 204)
	ben.must("GET", "/api/v1/me/inbox", nil, &inbox, 200)
	if inbox.Unread != 0 {
		t.Fatal("not marked as read")
	}

	// mentions: once per page, only people with access, e-mail can be switched off
	ben.must("PUT", "/api/v1/me/notifications", map[string]bool{"mention": false}, nil, 204)
	anna.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Plan", "body": htmlBody("<p>Hello @ben and @carl, see you</p>")}, nil, 200)
	anna.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Plan", "body": htmlBody("<p>Hello @ben and @carl, see you!</p>")}, nil, 200)
	ben.must("GET", "/api/v1/me/inbox", nil, &inbox, 200)
	mentions := 0
	for _, n := range inbox.Items {
		if n.Kind == "mention" {
			mentions++
		}
	}
	if mentions != 1 {
		t.Fatalf("mentions of ben: %+v", inbox.Items)
	}
	var carlN int
	_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE user_id=$1 AND kind='mention'`, carlID).Scan(&carlN)
	if carlN != 0 {
		t.Fatal("mentioned person without access was notified")
	}
	if err := e.app.Workers.RunOnce(context.Background(), "mail"); err != nil {
		t.Fatal(err)
	}
	if mc.find("ben@example.com", "mentioned") {
		t.Fatal("mention e-mail although switched off")
	}
	// mention in a comment
	ben.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", page.ID), map[string]any{"body": "Thanks @anna!"}, nil, 201)
	anna.must("GET", "/api/v1/me/inbox", nil, &inbox, 200)
	if len(inbox.Items) == 0 || inbox.Items[0].Kind != "mention" {
		t.Fatalf("comment mention: %+v", inbox.Items)
	}

	// assigned watches: for a group, optional ones can be unsubscribed
	if code := anna.do("POST", "/api/v1/admin/watch-assignments", map[string]any{"subject_type": "group", "subject_id": grp.ID, "target_type": "category", "target_id": c.ID}, nil); code != 403 {
		t.Fatalf("assignment without permission: %d", code)
	}
	var as struct {
		ID      int64 `json:"id"`
		Members int   `json:"members"`
	}
	admin.must("POST", "/api/v1/admin/watch-assignments", map[string]any{"subject_type": "group", "subject_id": grp.ID, "target_type": "category", "target_id": c.ID, "mandatory": false}, &as, 201)
	if as.Members != 2 {
		t.Fatalf("assignment members: %+v", as)
	}
	var mine []struct {
		ID        int64 `json:"id"`
		Mandatory bool  `json:"mandatory"`
	}
	ben.must("GET", "/api/v1/me/watch-assignments", nil, &mine, 200)
	if len(mine) != 1 {
		t.Fatalf("ben's assignments: %+v", mine)
	}
	countEvents := func(uid int64) int {
		var n int
		_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM watch_events WHERE user_id=$1`, uid).Scan(&n)
		return n
	}
	anna.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Plan 2", "body": htmlBody("<p>v3</p>")}, nil, 200)
	if countEvents(benID) == 0 {
		t.Fatal("assigned watch not effective")
	}
	ben.must("PUT", fmt.Sprintf("/api/v1/me/watch-assignments/%d", as.ID), map[string]any{"subscribed": false}, nil, 204)
	_, _ = e.app.DB.Exec(context.Background(), `DELETE FROM watch_events`)
	anna.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Plan 3", "body": htmlBody("<p>v4</p>")}, nil, 200)
	if countEvents(benID) != 0 {
		t.Fatal("unsubscribed optional watch still effective")
	}
	// mandatory: cannot be unsubscribed
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/watch-assignments/%d", as.ID), map[string]any{"subject_type": "group", "subject_id": grp.ID, "target_type": "category", "target_id": c.ID, "mandatory": true}, nil, 200)
	anna.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Plan 4", "body": htmlBody("<p>v5</p>")}, nil, 200)
	if countEvents(benID) == 0 {
		t.Fatal("mandatory watch not effective")
	}
	if code := ben.do("PUT", fmt.Sprintf("/api/v1/me/watch-assignments/%d", as.ID), map[string]any{"subscribed": false}, nil); code != 403 {
		t.Fatalf("unsubscribed from a mandatory watch: %d", code)
	}
}
