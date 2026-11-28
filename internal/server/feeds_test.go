package server_test

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

func TestFeeds(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var news, team, hidden cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "News"}}, &news, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": news.ID, "titles": map[string]string{"en": "Team news"}}, &team, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": news.ID, "titles": map[string]string{"en": "Board"}}, &hidden, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", news.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": false}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", team.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": news.ID, "type": "core.html", "title": "Launch & party", "body": htmlBody("<p>We launch <b>today</b></p>")}, nil, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": team.ID, "type": "core.html", "title": "New colleague", "body": htmlBody("<p>Welcome Sam</p>")}, nil, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": hidden.ID, "type": "core.html", "title": "Budget cuts", "body": htmlBody("<p>secret</p>")}, nil, 201)
	guest := e.client()
	path := fmt.Sprintf("/api/v1/feeds/categories/%d", news.ID)
	if code := guest.do("GET", path, nil, nil); code != 404 {
		t.Fatalf("feed must be off by default: %d", code)
	}
	if code := admin.do("PUT", fmt.Sprintf("/api/v1/categories/%d", news.ID), map[string]any{"feed": "sometimes"}, nil); code != 400 {
		t.Fatalf("invalid feed setting: %d", code)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", news.ID), map[string]any{"feed": "recursive"}, nil, 200)

	resp, body := guest.raw(path, nil)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/atom+xml") {
		t.Fatalf("atom: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var atom struct {
		Title   string `xml:"title"`
		Entries []struct {
			Title   string `xml:"title"`
			Summary string `xml:"summary"`
			Link    struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal([]byte(body), &atom); err != nil {
		t.Fatalf("atom does not parse: %v\n%s", err, body)
	}
	if len(atom.Entries) != 2 || strings.Contains(body, "Budget") || !strings.Contains(body, "Launch &amp; party") || !strings.Contains(body, "We launch today") {
		t.Fatalf("guest atom feed: %s", body)
	}
	// conditional request
	resp2, _ := guest.raw(path, map[string]string{"If-None-Match": resp.Header.Get("ETag")})
	if resp2.StatusCode != 304 {
		t.Fatalf("conditional GET: %d", resp2.StatusCode)
	}
	// RSS with content
	resp, body = guest.raw(path+"?format=rss&content=1", nil)
	var rss struct {
		Channel struct {
			Items []struct {
				Title string `xml:"title"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if resp.StatusCode != 200 || xml.Unmarshal([]byte(body), &rss) != nil || len(rss.Channel.Items) != 2 || !strings.Contains(body, "<![CDATA[<p>We launch <strong>today</strong></p>") {
		t.Fatalf("rss: %d %s", resp.StatusCode, body)
	}
	// personal feed links: each one has a key of its own, shown once
	type key struct {
		ID         int64   `json:"id"`
		Name       string  `json:"name"`
		Prefix     string  `json:"prefix"`
		Token      string  `json:"token"`
		LastUsedAt *string `json:"last_used_at"`
	}
	var k1, k2 key
	admin.must("POST", "/api/v1/me/feed-keys", map[string]any{"name": "Reader at home"}, &k1, 201)
	admin.must("POST", "/api/v1/me/feed-keys", map[string]any{"name": "Phone"}, &k2, 201)
	if !strings.HasPrefix(k1.Token, "ft_") || k1.Token == k2.Token || !strings.HasPrefix(k1.Token, k1.Prefix) {
		t.Fatalf("keys: %+v %+v", k1, k2)
	}
	for _, k := range []key{k1, k2} {
		_, body = guest.raw(path+"?token="+k.Token, nil)
		if !strings.Contains(body, "Budget cuts") || strings.Contains(body, k.Token) {
			t.Fatalf("personal feed: %s", body)
		}
	}
	// the database has only the hash of a key
	var plain int
	if err := e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM feed_keys WHERE position($1 in name||prefix||encode(token_hash,'escape')) > 0`, k1.Token).Scan(&plain); err != nil || plain != 0 {
		t.Fatalf("a feed key is stored in plain: %d %v", plain, err)
	}
	// the list never has the keys; the use is noted
	var list []key
	admin.must("GET", "/api/v1/me/feed-keys", nil, &list, 200)
	if len(list) != 2 || list[0].Token != "" || list[1].Token != "" || list[0].Name != "Phone" || list[1].LastUsedAt == nil {
		t.Fatalf("list: %+v", list)
	}
	// a key is no credential for the rest of the API
	if code := guest.do("GET", "/api/v1/categories?token="+k1.Token, nil, nil); code == 200 {
		var cats []cat
		guest.must("GET", fmt.Sprintf("/api/v1/categories?parent=%d&token=%s", news.ID, k1.Token), nil, &cats, 200)
		for _, c := range cats {
			if c.ID == hidden.ID {
				t.Fatal("feed key accepted by the API")
			}
		}
	}
	if code := guest.do("GET", "/api/v1/me/feed-keys?token="+k1.Token, nil, nil); code != 401 {
		t.Fatalf("feed key lists keys: %d", code)
	}
	// wrong, cut or changed keys are refused
	for _, bad := range []string{"ft_" + strings.Repeat("0", 96), k1.Token[:len(k1.Token)-1], k1.Token + "0", strings.ToUpper(k1.Token), "x" + k1.Token[1:]} {
		if r, _ := guest.raw(path+"?token="+bad, nil); r.StatusCode != 401 {
			t.Fatalf("key %q: %d", bad, r.StatusCode)
		}
	}
	// someone else can neither see nor delete them; an API key cannot make one
	bob := e.client()
	var bobID struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "feedbob", "password": "Passw0rd!Passw0rd"}, &bobID, 201)
	bob.login("feedbob", "Passw0rd!Passw0rd")
	if code := bob.do("DELETE", fmt.Sprintf("/api/v1/me/feed-keys/%d", k1.ID), nil, nil); code != 404 {
		t.Fatalf("someone else deletes a key: %d", code)
	}
	var mine []key
	bob.must("GET", "/api/v1/me/feed-keys", nil, &mine, 200)
	if len(mine) != 0 {
		t.Fatalf("someone else sees keys: %+v", mine)
	}
	if code := bob.do("GET", "/api/v1/admin/users/1/feed-keys", nil, nil); code != 403 {
		t.Fatalf("list of another account without the right: %d", code)
	}
	var ak struct {
		Token string `json:"token"`
	}
	bob.must("POST", "/api/v1/me/api-keys", map[string]any{"name": "k", "permissions": 1}, &ak, 201)
	kc := e.client()
	kc.token = ak.Token
	if code := kc.do("POST", "/api/v1/me/feed-keys", map[string]any{"name": "via key"}, nil); code != 403 {
		t.Fatalf("an API key made a feed key: %d", code)
	}
	// deleting one key: only its links stop, the other keeps working
	admin.must("DELETE", fmt.Sprintf("/api/v1/me/feed-keys/%d", k1.ID), nil, nil, 204)
	if r, _ := guest.raw(path+"?token="+k1.Token, nil); r.StatusCode != 401 {
		t.Fatalf("deleted key: %d", r.StatusCode)
	}
	if r, _ := guest.raw(path+"?token="+k2.Token, nil); r.StatusCode != 200 {
		t.Fatalf("the other key after deleting one: %d", r.StatusCode)
	}
	// the limit of the expert settings
	admin.must("PUT", "/api/v1/admin/limits", map[string]any{"values": map[string]any{"core.feeds.keys_per_user": 2}}, nil, 200)
	admin.must("POST", "/api/v1/me/feed-keys", map[string]any{"name": "Tablet"}, nil, 201)
	admin.must("POST", "/api/v1/me/feed-keys", map[string]any{"name": "One too many"}, nil, 409)
	admin.must("POST", "/api/v1/me/feed-keys", map[string]any{"name": "bad\nname"}, nil, 400)
	// a disabled person's keys stop working
	var bk key
	bob.must("POST", "/api/v1/me/feed-keys", map[string]any{"name": "Bob's reader"}, &bk, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", bobID.ID), map[string]any{"disabled": true}, nil, 200)
	if r, _ := guest.raw(path+"?token="+bk.Token, nil); r.StatusCode != 401 {
		t.Fatalf("key of a disabled person: %d", r.StatusCode)
	}
	// those who manage people see the names and delete all keys of a person
	admin.must("GET", fmt.Sprintf("/api/v1/admin/users/%d/feed-keys", bobID.ID), nil, &list, 200)
	if len(list) != 1 || list[0].Name != "Bob's reader" || list[0].Token != "" {
		t.Fatalf("admin list: %+v", list)
	}
	var n struct {
		Deleted int `json:"deleted"`
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/admin/users/%d/feed-keys", bobID.ID), nil, &n, 200)
	admin.must("GET", fmt.Sprintf("/api/v1/admin/users/%d/feed-keys", bobID.ID), nil, &list, 200)
	if n.Deleted != 1 || len(list) != 0 {
		t.Fatalf("admin delete all: %d %+v", n.Deleted, list)
	}
	// deleting all own keys
	admin.must("DELETE", "/api/v1/me/feed-keys", nil, &n, 200)
	if r, _ := guest.raw(path+"?token="+k2.Token, nil); r.StatusCode != 401 || n.Deleted != 2 {
		t.Fatalf("after deleting all: %d (%d deleted)", r.StatusCode, n.Deleted)
	}
	// scoped search
	var sr struct {
		Elements []struct {
			Title string `json:"title"`
		} `json:"elements"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/search?q=welcome&category=%d", team.ID), nil, &sr, 200)
	if len(sr.Elements) != 1 {
		t.Fatalf("scoped search: %+v", sr)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/search?q=welcome&category=%d&recursive=0", news.ID), nil, &sr, 200)
	if len(sr.Elements) != 0 {
		t.Fatalf("non-recursive scoped search: %+v", sr)
	}
}
