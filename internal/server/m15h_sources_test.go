package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestResolvedSources covers the source kinds resolved to elements: a
// search ("search:…") and a properties filter ("props:…") in lists of
// several sources, that readers only get what they may see and that bad
// values are refused.
func TestResolvedSources(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()

	var site, board cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Site"}}, &site, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", site.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Board"}}, &board, 201)
	admin.must("POST", "/api/v1/x/core.properties/defs", map[string]any{"key": "stage", "type": "text", "labels": map[string]string{"en": "Stage"}}, nil, 201)
	post := func(c int64, title, stage string) {
		body := map[string]any{"category_id": c, "type": "core.html", "title": title, "body": htmlBody("<p>The quokkafest " + title + "</p>")}
		if stage != "" {
			body["fields"] = map[string]any{"core.properties": map[string]any{"values": map[string]any{"stage": stage}}}
		}
		admin.must("POST", "/api/v1/elements", body, nil, 201)
	}
	post(site.ID, "Public plan", "live")
	post(site.ID, "Draft idea", "draft")
	post(board.ID, "Board secret", "live")

	// ------------------------------------------------------------ search
	sid := macroPage(t, admin, site.ID, "news", map[string]string{"source": "search:quokkafest", "layout": "list", "limit": "20"})
	if html := viewHTML(t, guest, sid); !strings.Contains(html, "Public plan") || !strings.Contains(html, "Draft idea") || strings.Contains(html, "Board secret") {
		t.Fatalf("guest search source: %s", html)
	}
	if html := viewHTML(t, admin, sid); !strings.Contains(html, "Board secret") {
		t.Fatalf("admin search source: %s", html)
	}
	// a search without hits shows nothing (never everything)
	nid := macroPage(t, admin, site.ID, "news", map[string]string{"source": "search:zzxqnohit", "layout": "list"})
	if html := viewHTML(t, admin, nid); strings.Contains(html, "Public plan") {
		t.Fatalf("empty search shows elements: %s", html)
	}

	// ------------------------------------------------------------ properties filter ("&&" between the filters)
	pid := macroPage(t, admin, site.ID, "news", map[string]string{"source": "props:stage = live && stage notempty", "layout": "list", "limit": "20"})
	if html := viewHTML(t, guest, pid); !strings.Contains(html, "Public plan") || strings.Contains(html, "Draft idea") || strings.Contains(html, "Board secret") {
		t.Fatalf("guest props source: %s", html)
	}
	if html := viewHTML(t, admin, pid); !strings.Contains(html, "Board secret") || strings.Contains(html, "Draft idea") {
		t.Fatalf("admin props source: %s", html)
	}
	// combined with a category: either matches
	cid := macroPage(t, admin, site.ID, "news", map[string]string{"source": fmt.Sprintf("props:stage = draft; category:%d", board.ID), "layout": "list", "limit": "20"})
	if html := viewHTML(t, admin, cid); !strings.Contains(html, "Draft idea") || !strings.Contains(html, "Board secret") || strings.Contains(html, "Public plan") {
		t.Fatalf("combined sources: %s", html)
	}

	// ------------------------------------------------------------ in the properties query itself
	var q struct {
		Rows []struct {
			Title string `json:"title"`
		} `json:"rows"`
	}
	guest.must("POST", "/api/v1/x/core.properties/query", map[string]any{"sources": []map[string]any{{"kind": "search", "value": "quokkafest"}}, "columns": []string{"stage"}}, &q, 200)
	for _, r := range q.Rows {
		if r.Title == "Board secret" {
			t.Fatal("guest query sees the board via a search source")
		}
	}
	if len(q.Rows) != 2 {
		t.Fatalf("guest query rows: %+v", q.Rows)
	}

	// ------------------------------------------------------------ bad values are refused
	for _, bad := range []string{"search:", "props:", "props:" + strings.Repeat("a", 2100)} {
		if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": site.ID, "type": "content.blog", "title": "B",
			"body": map[string]any{"category_id": site.ID, "more_sources": bad}}, nil); code != 400 {
			t.Fatalf("more_sources %q accepted: %d", bad, code)
		}
	}
}
