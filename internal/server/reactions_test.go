package server_test

import (
	"fmt"
	"testing"
)

// TestReactions covers emoji reactions on pages, categories and comments,
// the blocklist, permissions and the notification of the author.
func TestReactions(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	pw := "Passw0rd!Passw0rd"
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Reactions"}}, &c, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil, 201)
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "core.html", "title": "Hello", "body": htmlBody("<p>x</p>")}, &page, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rea", "password": pw, "display_name": "Rea", "roles": []int64{}}, nil, 201)
	rea := e.client()
	rea.login("rea", pw)

	type summary struct {
		Reactions []struct {
			Emoji string   `json:"emoji"`
			Count int      `json:"count"`
			Mine  bool     `json:"mine"`
			Users []string `json:"users"`
		} `json:"reactions"`
		CanReact bool `json:"can_react"`
	}
	var s summary
	path := fmt.Sprintf("/api/v1/x/core.reactions/element/%d", page.ID)
	rea.must("POST", path, map[string]any{"emoji": "👍🏽"}, &s, 200)
	rea.must("POST", path, map[string]any{"emoji": "🎉"}, &s, 200)
	admin.must("POST", path, map[string]any{"emoji": "🎉"}, &s, 200)
	if len(s.Reactions) != 2 || s.Reactions[1].Emoji != "🎉" || s.Reactions[1].Count != 2 || !s.Reactions[1].Mine || s.Reactions[0].Mine {
		t.Fatalf("summary: %+v", s)
	}
	if code := rea.do("POST", path, map[string]any{"emoji": "hello"}, nil); code != 400 {
		t.Fatalf("text accepted as emoji: %d", code)
	}
	// the author (admin) got one notification from Rea (not one per emoji)
	var inbox struct {
		Items []struct {
			Kind  string `json:"kind"`
			Title string `json:"title"`
		} `json:"items"`
	}
	admin.must("GET", "/api/v1/me/inbox", nil, &inbox, 200)
	n := 0
	for _, it := range inbox.Items {
		if it.Kind == "reaction" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("reaction notifications: %d %+v", n, inbox.Items)
	}
	// e-mail of reactions is off by default
	var prefs struct {
		Prefs map[string]bool `json:"prefs"`
	}
	admin.must("GET", "/api/v1/me/notifications", nil, &prefs, 200)
	if v, ok := prefs.Prefs["reaction"]; !ok || v {
		t.Fatalf("reaction mails should be off by default: %v", prefs.Prefs)
	}
	// blocklist: needs core.reactions.manage; blocks all skin tones; existing reactions hidden
	if code := rea.do("PUT", "/api/v1/x/core.reactions/settings", map[string]any{"blocked": []string{"👍"}}, nil); code != 403 {
		t.Fatalf("blocklist without permission: %d", code)
	}
	admin.must("PUT", "/api/v1/x/core.reactions/settings", map[string]any{"blocked": []string{"👍"}}, nil, 200)
	if code := rea.do("POST", path, map[string]any{"emoji": "👍🏿"}, nil); code != 400 {
		t.Fatalf("blocked emoji accepted: %d", code)
	}
	rea.must("GET", path, nil, &s, 200)
	if len(s.Reactions) != 1 || s.Reactions[0].Emoji != "🎉" {
		t.Fatalf("blocked reaction still shown: %+v", s)
	}
	// remove own reaction
	rea.must("POST", path, map[string]any{"emoji": "🎉", "on": false}, &s, 200)
	if s.Reactions[0].Count != 1 || s.Reactions[0].Mine {
		t.Fatalf("after removing: %+v", s)
	}
	// categories and comments; batch
	rea.must("POST", fmt.Sprintf("/api/v1/x/core.reactions/category/%d", c.ID), map[string]any{"emoji": "❤️"}, nil, 200)
	var cm struct{ ID int64 }
	rea.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", page.ID), map[string]any{"body": "nice"}, &cm, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/x/core.reactions/comment/%d", cm.ID), map[string]any{"emoji": "😄"}, nil, 200)
	var batch []struct {
		TargetID  int64 `json:"target_id"`
		Reactions []any `json:"reactions"`
	}
	rea.must("GET", fmt.Sprintf("/api/v1/x/core.reactions/batch?type=comment&ids=%d,999999", cm.ID), nil, &batch, 200)
	if len(batch) != 1 || len(batch[0].Reactions) != 1 {
		t.Fatalf("batch: %+v", batch)
	}
	// guests and users without view permission
	guest := e.client()
	if code := guest.do("POST", path, map[string]any{"emoji": "🎉"}, nil); code != 401 {
		t.Fatalf("guest reacted: %d", code)
	}
	var hidden cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Hidden"}}, &hidden, 201)
	if code := rea.do("POST", fmt.Sprintf("/api/v1/x/core.reactions/category/%d", hidden.ID), map[string]any{"emoji": "🎉"}, nil); code != 404 && code != 403 {
		t.Fatalf("reaction on invisible category: %d", code)
	}
}
