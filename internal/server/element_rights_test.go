package server_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestElementRights: a page with "own rights only" appears on no path for
// people without a rule on it (view, search, tags, listings, blog, feed,
// history, revisions, comments, reactions, favorites, watches, sharing,
// mentions, duplicate, move, delete), while managers of the category by a
// rule, administrators and people with a rule on the page keep access.
// Rules only give what their author has; nobody locks themselves out.
func TestElementRights(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID         int64 `json:"id"`
		CategoryID int64 `json:"category_id"`
	}
	var cat, other, blog, secret, open obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Board"}}, &cat, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Elsewhere"}}, &other, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d", cat.ID), map[string]any{"feed": "on"}, nil, 200)
	users := map[string]int64{}
	clients := map[string]*client{}
	for _, n := range []string{"rea", "edi", "man", "gra"} {
		var u obj
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": n, "password": "Passw0rd!Passw0rd", "email": n + "@example.com"}, &u, 201)
		users[n] = u.ID
	}
	rule := func(c int64, who string, perms int) {
		admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c), map[string]any{"subject_type": "user", "subject_id": users[who], "permissions": perms}, nil, 201)
	}
	rule(cat.ID, "rea", 1)
	rule(cat.ID, "edi", 1|2|4|8)
	rule(cat.ID, "man", 1|16)
	rule(other.ID, "edi", 1|2|4|8)
	rule(other.ID, "man", 1|2|4|8|16)
	for n := range users {
		clients[n] = e.client()
		clients[n].login(n, "Passw0rd!Passw0rd")
	}
	rea, edi, man, gra := clients["rea"], clients["edi"], clients["man"], clients["gra"]
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "content.blog", "title": "Blog", "body": map[string]any{"category_id": cat.ID}}, &blog, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Zebrasecret plan", "tags": []string{"zebratag"},
		"body": htmlBody("<p>zebrasecret content</p>")}, &secret, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Zebra open", "body": htmlBody("<p>zebrasecret is not here</p>")}, &open, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", secret.ID), map[string]any{"body": "first"}, nil, 201)

	// own rights only, and a rule for gra (who has no rights in the category)
	var acl struct {
		OwnRightsOnly bool `json:"own_rights_only"`
		Rules         []struct {
			ID int64 `json:"id"`
		} `json:"rules"`
		Mine []string `json:"mine"`
	}
	man.must("PUT", fmt.Sprintf("/api/v1/elements/%d/acl", secret.ID), map[string]any{"own_rights_only": true}, &acl, 200)
	if !acl.OwnRightsOnly || !slices.Contains(acl.Mine, "grant") {
		t.Fatalf("acl: %+v", acl)
	}
	var graRule obj
	man.must("POST", fmt.Sprintf("/api/v1/elements/%d/acl", secret.ID), map[string]any{"subject_type": "user", "subject_id": users["gra"], "permissions": 1 | 32}, &graRule, 201)

	// --- people without a rule: nothing on any path
	for _, who := range []struct {
		name string
		c    *client
	}{{"reader", rea}, {"editor", edi}} {
		c := who.c
		if code := c.do("GET", fmt.Sprintf("/api/v1/elements/%d", secret.ID), nil, nil); code != 404 {
			t.Fatalf("%s: element %d", who.name, code)
		}
		for _, path := range []string{"/revisions", "/acl"} {
			if code := c.do("GET", fmt.Sprintf("/api/v1/elements/%d%s", secret.ID, path), nil, nil); code != 404 {
				t.Fatalf("%s: %s %d", who.name, path, code)
			}
		}
		if code := c.do("GET", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", secret.ID), nil, nil); code != 404 {
			t.Fatalf("%s: comments %d", who.name, code)
		}
		if code := c.do("POST", fmt.Sprintf("/api/v1/x/core.reactions/element/%d", secret.ID), map[string]any{"emoji": "👍"}, nil); code != 404 {
			t.Fatalf("%s: reaction %d", who.name, code)
		}
		if code := c.do("POST", "/api/v1/x/core.favorites/", map[string]any{"type": "element", "id": secret.ID}, nil); code != 404 {
			t.Fatalf("%s: favorite %d", who.name, code)
		}
		if code := c.do("PUT", "/api/v1/me/watches", map[string]any{"target_type": "element", "target_id": secret.ID}, nil); code != 404 {
			t.Fatalf("%s: watch %d", who.name, code)
		}
		var sr struct {
			Elements []struct {
				ElementID int64 `json:"element_id"`
			} `json:"elements"`
		}
		c.must("GET", "/api/v1/search?q=zebrasecret", nil, &sr, 200)
		if len(sr.Elements) != 1 || sr.Elements[0].ElementID != open.ID {
			t.Fatalf("%s: search %+v", who.name, sr)
		}
		c.must("GET", "/api/v1/search?tags=zebratag", nil, &sr, 200)
		if len(sr.Elements) != 0 {
			t.Fatalf("%s: tag search %+v", who.name, sr)
		}
		var tags []struct {
			Tag string `json:"tag"`
		}
		c.must("GET", "/api/v1/tags?prefix=zebra", nil, &tags, 200)
		if len(tags) != 0 {
			t.Fatalf("%s: tags %+v", who.name, tags)
		}
		var cv struct {
			Elements []obj `json:"elements"`
		}
		c.must("GET", fmt.Sprintf("/api/v1/categories/%d", cat.ID), nil, &cv, 200)
		for _, x := range cv.Elements {
			if x.ID == secret.ID {
				t.Fatalf("%s: listed in the category", who.name)
			}
		}
		var list struct {
			Items []obj `json:"items"`
		}
		c.must("GET", fmt.Sprintf("/api/v1/x/content.blog/elements/%d/posts?lang=en", blog.ID), nil, &list, 200)
		for _, x := range list.Items {
			if x.ID == secret.ID {
				t.Fatalf("%s: in the blog", who.name)
			}
		}
		var found struct {
			Items []obj `json:"items"`
		}
		c.must("GET", "/api/v1/elements?q=Zebrasecret", nil, &found, 200)
		for _, x := range found.Items {
			if x.ID == secret.ID {
				t.Fatalf("%s: in the element finder", who.name)
			}
		}
		var hist []struct {
			EntityType string `json:"entity_type"`
			EntityID   int64  `json:"entity_id"`
		}
		c.must("GET", fmt.Sprintf("/api/v1/categories/%d/history", cat.ID), nil, &hist, 200)
		for _, x := range hist {
			if x.EntityType == "element" && x.EntityID == secret.ID {
				t.Fatalf("%s: in the history of the category", who.name)
			}
		}
		if code, body := c.rawBody("GET", fmt.Sprintf("/api/v1/feeds/categories/%d", cat.ID), ""); code != 200 || strings.Contains(body, "Zebrasecret") {
			t.Fatalf("%s: feed %d", who.name, code)
		}
	}
	// the editor of the category may not change, copy, move or delete it
	if code := edi.do("PUT", fmt.Sprintf("/api/v1/elements/%d", secret.ID), map[string]any{"lang": "en", "title": "x", "body": htmlBody("<p>x</p>"), "base_rev": 1}, nil); code != 404 {
		t.Fatalf("editor changed it: %d", code)
	}
	if code := edi.do("POST", fmt.Sprintf("/api/v1/elements/%d/duplicate", secret.ID), map[string]any{"category_id": other.ID}, nil); code != 404 {
		t.Fatalf("editor copied it: %d", code)
	}
	if code := edi.do("POST", fmt.Sprintf("/api/v1/elements/%d/move", secret.ID), map[string]any{"category_id": other.ID}, nil); code != 404 {
		t.Fatalf("editor moved it: %d", code)
	}
	if code := edi.do("DELETE", fmt.Sprintf("/api/v1/elements/%d", secret.ID), nil, nil); code != 404 {
		t.Fatalf("editor deleted it: %d", code)
	}
	// sharing with someone who cannot see it, mentioning them
	var share struct {
		WithoutAccess []string `json:"without_access"`
	}
	admin.must("POST", "/api/v1/x/core.share/share", map[string]any{"target_type": "element", "target_id": secret.ID, "users": []int64{users["rea"]}}, &share, 200)
	if len(share.WithoutAccess) != 1 {
		t.Fatalf("share: %+v", share)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", secret.ID), map[string]any{"body": "see @rea and @gra"}, nil, 201)
	var n int
	_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE user_id=$1`, users["rea"]).Scan(&n)
	if n != 0 {
		t.Fatalf("reader was notified about the page (%d)", n)
	}
	_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE user_id=$1 AND kind='mention'`, users["gra"]).Scan(&n)
	if n != 1 {
		t.Fatalf("gra (with a rule) was not mentioned: %d", n)
	}

	// --- with a rule: gra sees it (only it), may not edit it
	gra.must("GET", fmt.Sprintf("/api/v1/elements/%d", secret.ID), nil, nil, 200)
	if code := gra.do("GET", fmt.Sprintf("/api/v1/categories/%d", cat.ID), nil, nil); code != 404 {
		t.Fatalf("gra sees the category: %d", code)
	}
	var sr struct {
		Elements []struct {
			ElementID int64 `json:"element_id"`
		} `json:"elements"`
	}
	gra.must("GET", "/api/v1/search?q=zebrasecret", nil, &sr, 200)
	if len(sr.Elements) != 1 || sr.Elements[0].ElementID != secret.ID {
		t.Fatalf("gra: search %+v", sr)
	}
	if code := gra.do("PUT", fmt.Sprintf("/api/v1/elements/%d", secret.ID), map[string]any{"lang": "en", "title": "x", "body": htmlBody("<p>x</p>"), "base_rev": 1}, nil); code != 403 {
		t.Fatalf("gra changed it: %d", code)
	}
	gra.must("GET", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", secret.ID), nil, nil, 200)
	// gra may give rights, but not more than view and grant
	if code := gra.do("POST", fmt.Sprintf("/api/v1/elements/%d/acl", secret.ID), map[string]any{"subject_type": "user", "subject_id": users["rea"], "permissions": 3}, nil); code != 400 {
		t.Fatalf("gra gave edit: %d", code)
	}
	var reaRule obj
	gra.must("POST", fmt.Sprintf("/api/v1/elements/%d/acl", secret.ID), map[string]any{"subject_type": "user", "subject_id": users["rea"], "permissions": 1}, &reaRule, 201)
	rea.must("GET", fmt.Sprintf("/api/v1/elements/%d", secret.ID), nil, nil, 200)
	gra.must("DELETE", fmt.Sprintf("/api/v1/element-acl/%d", reaRule.ID), nil, nil, 204)
	if code := rea.do("GET", fmt.Sprintf("/api/v1/elements/%d", secret.ID), nil, nil); code != 404 {
		t.Fatalf("reader still sees it: %d", code)
	}
	// nobody locks themselves out
	if code := gra.do("DELETE", fmt.Sprintf("/api/v1/element-acl/%d", graRule.ID), nil, nil); code != 400 {
		t.Fatalf("gra removed their own grant: %d", code)
	}
	if code := gra.do("PUT", fmt.Sprintf("/api/v1/element-acl/%d", graRule.ID), map[string]any{"permissions": 1}, nil); code != 400 {
		t.Fatalf("gra dropped their own grant: %d", code)
	}
	// the reader cannot change rules
	if code := rea.do("PUT", fmt.Sprintf("/api/v1/element-acl/%d", graRule.ID), map[string]any{"permissions": 33}, nil); code != 404 {
		t.Fatalf("reader changed a rule: %d", code)
	}

	// --- managers by a rule and administrators keep access
	man.must("GET", fmt.Sprintf("/api/v1/elements/%d", secret.ID), nil, nil, 200)
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", secret.ID), nil, nil, 200)

	// --- copies: a manager copies the category; the copy keeps the rights
	var cp obj
	man.must("POST", fmt.Sprintf("/api/v1/categories/%d/duplicate", cat.ID), map[string]any{"parent_id": other.ID}, &cp, 201)
	var copied int64
	if err := e.app.DB.QueryRow(context.Background(), `SELECT id FROM elements WHERE category_id=$1 AND titles->>'en'='Zebrasecret plan'`, cp.ID).Scan(&copied); err != nil {
		t.Fatal(err)
	}
	if code := edi.do("GET", fmt.Sprintf("/api/v1/elements/%d", copied), nil, nil); code != 404 {
		t.Fatalf("editor sees the copy: %d", code)
	}
	// a copy without its rights needs the permission to give rights
	if code := gra.do("POST", fmt.Sprintf("/api/v1/elements/%d/duplicate", secret.ID), map[string]any{"category_id": other.ID}, nil); code != 403 && code != 404 {
		t.Fatalf("gra copied it into a category without rights there: %d", code)
	}

	// --- classification of a page: internal is not for guests
	var pub obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Public"}}, &pub, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", pub.ID), map[string]any{"subject_type": "guest", "permissions": 1}, nil, 201)
	var pubPage obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "core.html", "title": "Okapi notes", "body": htmlBody("<p>okapi</p>")}, &pubPage, 201)
	guest := e.client()
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d", pubPage.ID), nil, nil, 200)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d/acl", pubPage.ID), map[string]any{"classification": 1}, nil, 200)
	if code := guest.do("GET", fmt.Sprintf("/api/v1/elements/%d", pubPage.ID), nil, nil); code != 401 {
		t.Fatalf("guest sees an internal page: %d", code)
	}
	guest.must("GET", "/api/v1/search?q=okapi", nil, &sr, 200)
	if len(sr.Elements) != 0 {
		t.Fatalf("guest finds an internal page: %+v", sr)
	}
	rea.must("GET", fmt.Sprintf("/api/v1/elements/%d", pubPage.ID), nil, nil, 200)
	// strictly confidential pages are not copied
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d/acl", pubPage.ID), map[string]any{"classification": 3}, nil, 200)
	if code := admin.do("POST", fmt.Sprintf("/api/v1/elements/%d/duplicate", pubPage.ID), map[string]any{}, nil); code != 403 {
		t.Fatalf("strictly confidential page copied: %d", code)
	}
	rea.must("GET", "/api/v1/search?q=okapi", nil, &sr, 200)
	if len(sr.Elements) != 0 {
		t.Fatalf("strictly confidential page in the general search: %+v", sr)
	}

	// --- own rights only off again: the category's rules apply
	man.must("PUT", fmt.Sprintf("/api/v1/elements/%d/acl", secret.ID), map[string]any{"own_rights_only": false}, nil, 200)
	edi.must("GET", fmt.Sprintf("/api/v1/elements/%d", secret.ID), nil, nil, 200)
	// changes are in the audit log
	_ = e.app.DB.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE entity_type='element' AND entity_id=$1 AND action LIKE 'element.acl.%'`, secret.ID).Scan(&n)
	if n < 3 {
		t.Fatalf("audit of rule changes: %d", n)
	}
}
