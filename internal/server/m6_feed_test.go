package server_test

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func (c *client) raw(path string, hdr map[string]string) (*http.Response, string) {
	req, _ := http.NewRequest("GET", c.e.srv.URL+path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

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
	// personal feed link: sees what the user sees, without a session
	var tok struct {
		Token string `json:"token"`
	}
	admin.must("GET", "/api/v1/me/feed-token", nil, &tok, 200)
	_, body = guest.raw(path+"?token="+tok.Token, nil)
	if !strings.Contains(body, "Budget cuts") || strings.Contains(body, tok.Token) {
		t.Fatalf("personal feed: %s", body)
	}
	if code := guest.do("GET", "/api/v1/categories?token="+tok.Token, nil, nil); code == 200 {
		// the token must not work as a general API credential: the guest answer has no hidden category
		var cats []cat
		guest.must("GET", fmt.Sprintf("/api/v1/categories?parent=%d&token=%s", news.ID, tok.Token), nil, &cats, 200)
		for _, c := range cats {
			if c.ID == hidden.ID {
				t.Fatal("feed token accepted by the API")
			}
		}
	}
	var tok2 struct {
		Token string `json:"token"`
	}
	admin.must("POST", "/api/v1/me/feed-token", nil, &tok2, 200)
	if r, _ := guest.raw(path+"?token="+tok.Token, nil); r.StatusCode != 401 || tok2.Token == tok.Token {
		t.Fatalf("old token after rotation: %d", r.StatusCode)
	}
	admin.must("DELETE", "/api/v1/me/feed-token", nil, nil, 204)
	if r, _ := guest.raw(path+"?token="+tok2.Token, nil); r.StatusCode != 401 {
		t.Fatalf("revoked token: %d", r.StatusCode)
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
