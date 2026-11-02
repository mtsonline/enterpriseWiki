package server_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func (e *env) guestPage(path, lang string) (string, http.Header) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b), res.Header
}

func TestPrerenderForGuests(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var pub, hidden cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Public", "de": "Öffentlich"}, "descriptions": map[string]string{"en": "For everyone"}}, &pub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": pub.ID, "titles": map[string]string{"en": "Board"}}, &hidden, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", pub.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": false}, nil, 201)
	var page, secret elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "core.html", "title": "Opening hours <&>",
		"body": htmlBody("<p>We are open <strong>Monday to Friday</strong> from 8 to 17.</p><script>alert(1)</script>"), "short_url": "hours"}, &page, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": hidden.ID, "type": "core.html", "title": "Secret minutes", "body": htmlBody("<p>top secret</p>")}, &secret, 201)

	body, hdr := e.guestPage(fmt.Sprintf("/e/%d", page.ID), "de-AT,de;q=0.9")
	for _, want := range []string{`<html lang="en">`, `<title>Opening hours &lt;&amp;&gt; – Enterprise Wiki</title>`, `<meta name="description" content="We are open Monday to Friday from 8 to 17.">`,
		`rel="canonical"`, `<meta property="og:type" content="article">`, `<strong>Monday to Friday</strong>`, `<h1>Opening hours &lt;&amp;&gt;</h1>`} {
		if !strings.Contains(body, want) {
			t.Errorf("guest page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<script>alert") {
		t.Error("script from content in prerendered page")
	}
	if hdr.Get("Content-Language") != "en" {
		t.Errorf("content language %q (only an english revision exists)", hdr.Get("Content-Language"))
	}
	var more elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": pub.ID, "type": "core.html", "title": "Contact", "body": htmlBody("<p>Call us</p>")}, &more, 201)
	// short URL, category with german title (first element as its content), no hidden subcategories
	body, _ = e.guestPage("/hours", "")
	if !strings.Contains(body, "Opening hours") {
		t.Error("short URL not prerendered")
	}
	body, _ = e.guestPage(fmt.Sprintf("/c/%d", pub.ID), "de")
	if !strings.Contains(body, "<h1>Öffentlich</h1>") || !strings.Contains(body, "Monday to Friday") || !strings.Contains(body, fmt.Sprintf(`<a href="/e/%d">Contact</a>`, more.ID)) || strings.Contains(body, "Board") {
		t.Errorf("category page:\n%s", body)
	}
	// hidden pages and other routes are not prerendered
	for _, p := range []string{fmt.Sprintf("/e/%d", secret.ID), "/login", "/admin", "/nothing"} {
		if body, _ := e.guestPage(p, ""); strings.Contains(body, "ew-prerender") || strings.Contains(body, "top secret") {
			t.Errorf("%s prerendered for guests", p)
		}
	}
	// logged in users get the plain app
	if code := admin.do("GET", fmt.Sprintf("/e/%d", page.ID), nil, nil); code != 200 {
		t.Fatal(code)
	}
	// API: guest view, also when asked by an administrator
	var r struct {
		Available bool `json:"available"`
		Page      struct {
			Title string `json:"title"`
			HTML  string `json:"html"`
		} `json:"page"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/prerender?path=/e/%d", secret.ID), nil, &r, 200)
	if r.Available {
		t.Error("hidden element available in guest view")
	}
	admin.must("GET", "/api/v1/prerender?path=/hours", nil, &r, 200)
	if !r.Available || r.Page.Title != "Opening hours <&>" {
		t.Errorf("api: %+v", r)
	}
	// switched off
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"ui": map[string]any{"site_name": "Enterprise Wiki", "template": "theme.wiki", "prerender": false}}, nil, 200)
	if body, _ := e.guestPage("/hours", ""); strings.Contains(body, "ew-prerender") {
		t.Error("prerender not switched off")
	}
}
