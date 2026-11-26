package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

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

// TestReadablePathsPerLanguage covers one path per language, "only the
// short URL" and switching readable paths off.
func TestReadablePathsPerLanguage(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type resolved struct {
		Type     string            `json:"type"`
		ID       int64             `json:"id"`
		Path     string            `json:"path"`
		Redirect bool              `json:"redirect"`
		Lang     string            `json:"lang"`
		Paths    map[string]string `json:"paths"`
	}
	resolve := func(p string) (resolved, int) {
		var r resolved
		code := admin.do("GET", "/api/v1/resolve?path="+p, nil, &r)
		return r, code
	}
	var team cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"de": "Unser Team", "en": "Our team"}, "original_lang": "de"}, &team, 201)
	var page elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": team.ID, "type": "core.html", "lang": "de", "title": "Über mich", "body": htmlBody("<p>Hallo</p>")}, &page, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "About me", "body": htmlBody("<p>Hello</p>")}, nil, 200)
	var v struct {
		URL  string            `json:"url"`
		URLs map[string]string `json:"urls"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", page.ID), nil, &v, 200)
	if v.URLs["de"] != "/wiki/unser-team/ueber-mich" || v.URLs["en"] != "/wiki/our-team/about-me" || v.URL != "/wiki/our-team/about-me" {
		t.Fatalf("paths: %+v", v)
	}
	// French has no translation: the original (German) segments
	if v.URLs["fr"] != "/wiki/unser-team/ueber-mich" {
		t.Fatalf("fallback path: %q", v.URLs["fr"])
	}
	for p, lang := range map[string]string{"wiki/unser-team/ueber-mich": "de", "wiki/our-team/about-me": "en"} {
		if r, _ := resolve(p); r.ID != page.ID || r.Redirect || r.Lang != lang {
			t.Fatalf("%s: %+v", p, r)
		}
	}
	// mixed languages: valid, but redirected to one language
	if r, _ := resolve("wiki/our-team/ueber-mich"); r.ID != page.ID || !r.Redirect || r.Path != "/wiki/unser-team/ueber-mich" {
		t.Fatalf("mixed: %+v", r)
	}
	// a new English title: the former English path still works
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"lang": "en", "title": "About us", "body": htmlBody("<p>Hello</p>"), "base_rev": 1}, nil, 200)
	if r, _ := resolve("wiki/our-team/about-me"); r.ID != page.ID || !r.Redirect || r.Path != "/wiki/our-team/about-us" {
		t.Fatalf("former English path: %+v", r)
	}
	// only the short URL
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"short_url": "about", "short_only": true}, nil, 200)
	if _, code := resolve("wiki/unser-team/ueber-mich"); code != 404 {
		t.Fatalf("readable path of a short-only page: %d", code)
	}
	if _, code := resolve("wiki/our-team/about-me"); code != 404 {
		t.Fatalf("former path of a short-only page: %d", code)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", page.ID), nil, &v, 200)
	if v.URL != "/about" {
		t.Fatalf("short-only url: %+v", v)
	}
	if r, _ := resolve("about"); r.ID != page.ID || r.Redirect {
		t.Fatalf("short URL: %+v", r)
	}
	admin.must("PATCH", fmt.Sprintf("/api/v1/elements/%d", page.ID), map[string]any{"short_only": false}, nil, 200)
	// readable paths switched off
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": map[string]any{"readable_urls": false}}, nil, 200)
	if _, code := resolve("wiki/unser-team/ueber-mich"); code != 404 {
		t.Fatalf("readable paths off: %d", code)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=en", page.ID), nil, &v, 200)
	if v.URL != "" {
		t.Fatalf("url with readable paths off: %+v", v)
	}
	if r, _ := resolve("about"); r.ID != page.ID {
		t.Fatalf("short URL with readable paths off: %+v", r)
	}
}
