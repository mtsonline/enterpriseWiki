package server_test

import (
	"fmt"
	"net/http"
	"strings"
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

// TestReadablePaths covers /wiki/… paths, former paths and short URLs
// (redirects) and the help on "page not found".
func TestReadablePaths(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type resolved struct {
		Type     string `json:"type"`
		ID       int64  `json:"id"`
		Path     string `json:"path"`
		Redirect bool   `json:"redirect"`
	}
	resolve := func(p string) (resolved, int) {
		var r resolved
		code := admin.do("GET", "/api/v1/resolve?path="+p, nil, &r)
		return r, code
	}
	var hr, leave cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Human Resources"}}, &hr, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": hr.ID, "titles": map[string]string{"en": "Leave"}}, &leave, 201)
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": leave.ID, "type": "core.html", "title": "Request holidays", "body": htmlBody("<p>Ask your manager about vacation</p>")}, &page, 201)
	var v struct {
		URL string `json:"url"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", page.ID), nil, &v, 200)
	if v.URL != "/wiki/human-resources/leave/request-holidays" {
		t.Fatalf("path: %q", v.URL)
	}
	if r, _ := resolve("wiki/human-resources/leave/request-holidays"); r.ID != page.ID || r.Redirect {
		t.Fatalf("resolve: %+v", r)
	}
	if r, _ := resolve("wiki/human-resources/leave"); r.Type != "category" || r.ID != leave.ID {
		t.Fatalf("resolve category: %+v", r)
	}
	// same title in the same category: -2
	var twin elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": leave.ID, "type": "core.html", "title": "Request holidays", "body": htmlBody("<p>x</p>")}, &twin, 201)
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", twin.ID), nil, &v, 200)
	if v.URL != "/wiki/human-resources/leave/request-holidays-2" {
		t.Fatalf("twin path: %q", v.URL)
	}
	// new title: new path, the old one redirects
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "Holiday requests", "body": htmlBody("<p>v2</p>"), "base_rev": 1}, nil, 200)
	if r, _ := resolve("wiki/human-resources/leave/request-holidays"); r.ID != page.ID || !r.Redirect || r.Path != "/wiki/human-resources/leave/holiday-requests" {
		t.Fatalf("former path: %+v", r)
	}
	// renamed category: former paths below it still work
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", hr.ID), map[string]any{"titles": map[string]string{"en": "People"}}, nil, 200)
	if r, _ := resolve("wiki/human-resources/leave/holiday-requests"); r.ID != page.ID || r.Path != "/wiki/people/leave/holiday-requests" {
		t.Fatalf("after renaming the category: %+v", r)
	}
	// moved page
	var other cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Archive"}}, &other, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/elements/%d/move", page.ID), map[string]any{"category_id": other.ID}, nil, 200)
	if r, _ := resolve("wiki/people/leave/holiday-requests"); r.ID != page.ID || r.Path != "/wiki/archive/holiday-requests" {
		t.Fatalf("after moving: %+v", r)
	}
	// the server redirects former addresses (301)
	noRedir := &http.Client{Jar: admin.http.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", e.srv.URL+"/wiki/human-resources/leave/request-holidays?lang=en", nil)
	res, err := noRedir.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 301 || res.Header.Get("Location") != "/wiki/archive/holiday-requests?lang=en" {
		t.Fatalf("redirect: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	// short URLs: the former one redirects to the new one
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"short_url": "urlaub"}, nil, 200)
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"short_url": "ferien"}, nil, 200)
	if r, _ := resolve("urlaub"); r.ID != page.ID || !r.Redirect || r.Path != "/ferien" {
		t.Fatalf("former short URL: %+v", r)
	}
	if r, _ := resolve("ferien"); r.ID != page.ID || r.Redirect {
		t.Fatalf("short URL: %+v", r)
	}
	// deleted page: "deleted" with restore for who may restore, and suggestions
	admin.must("DELETE", fmt.Sprintf("/api/v1/elements/%d", twin.ID), nil, nil, 204)
	if _, code := resolve("wiki/people/leave/request-holidays-2"); code != 404 {
		t.Fatalf("deleted page resolved: %d", code)
	}
	var nf struct {
		Deleted *struct {
			ID         int64 `json:"id"`
			CanRestore bool  `json:"can_restore"`
		} `json:"deleted"`
		Suggestions []struct {
			ID   int64  `json:"id"`
			Path string `json:"path"`
		} `json:"suggestions"`
	}
	admin.must("GET", "/api/v1/not-found?path=wiki/people/leave/request-holidays-2", nil, &nf, 200)
	if nf.Deleted == nil || nf.Deleted.ID != twin.ID || !nf.Deleted.CanRestore {
		t.Fatalf("deleted info: %+v", nf)
	}
	nf.Deleted, nf.Suggestions = nil, nil
	admin.must("GET", "/api/v1/not-found?path=wiki/old/holiday-request", nil, &nf, 200)
	found := false
	for _, s := range nf.Suggestions {
		found = found || (s.ID == page.ID && strings.HasPrefix(s.Path, "/wiki/"))
	}
	if !found {
		t.Fatalf("suggestions: %+v", nf.Suggestions)
	}
	// guests learn nothing about pages they may not see
	guest := e.client()
	var gnf struct {
		Deleted     any   `json:"deleted"`
		Suggestions []any `json:"suggestions"`
	}
	guest.must("GET", "/api/v1/not-found?path=wiki/people/leave/request-holidays-2", nil, &gnf, 200)
	if gnf.Deleted != nil || len(gnf.Suggestions) != 0 {
		t.Fatalf("guest not-found info: %+v", gnf)
	}
}
