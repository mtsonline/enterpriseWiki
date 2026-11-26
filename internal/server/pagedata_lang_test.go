package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestPageDataLanguageVersions: a page with tasks and dates in two language
// versions shows them once in the reports, in the reader's language; a page
// without a version in that language shows those of its original language.
// The data API does the same with prefer=LANG and keeps all versions
// without it.
func TestPageDataLanguageVersions(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var cat, both, only obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Team", "de": "Team"}}, &cat, 201)
	md := func(src string) map[string]any { return map[string]any{"format": "markdown", "source": src} }
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "lang": "en", "title": "Move",
		"body": md("- [ ] Check the offer :date{value=2026-11-30}\n\n:date{value=2099-12-12 label=\"Moving day\"}\n")}, &both, 201)
	zero := 0
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", both.ID), map[string]any{"lang": "de", "title": "Umzug", "base_rev": zero,
		"body": md("- [ ] Angebot prüfen :date{value=2026-11-30}\n\n:date{value=2099-12-12 label=\"Umzugstag\"}\n")}, nil, 200)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "lang": "en", "title": "Only English",
		"body": md("- [ ] Order cables :date{value=2026-12-01}\n")}, &only, 201)
	var report obj
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "lang": "en", "title": "Report",
		"body": md(fmt.Sprintf("::tasks{category=%d}\n\n::dates{category=%d}\n", cat.ID, cat.ID))}, &report, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", report.ID), map[string]any{"lang": "de", "title": "Bericht", "base_rev": zero,
		"body": md(fmt.Sprintf("::tasks{category=%d}\n\n::dates{category=%d}\n", cat.ID, cat.ID))}, nil, 200)

	for lang, want := range map[string][2][]string{
		"de": {{"Angebot prüfen", "Order cables", "Umzugstag"}, {"Check the offer", "Moving day"}},
		"en": {{"Check the offer", "Order cables", "Moving day"}, {"Angebot prüfen", "Umzugstag"}},
	} {
		var v struct {
			HTML string `json:"html"`
		}
		admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=%s", report.ID, lang), nil, &v, 200)
		for _, s := range want[0] {
			// the visible text (the tick box names the task once more)
			if n := strings.Count(v.HTML, ">"+s+"<"); n != 1 {
				t.Errorf("%s report: %q %d times", lang, s, n)
			}
		}
		for _, s := range want[1] {
			if strings.Contains(v.HTML, s) {
				t.Errorf("%s report shows the other language version: %q", lang, s)
			}
		}
	}
	var r struct {
		Items []struct {
			Lang string `json:"lang"`
		} `json:"items"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/data/core.html.task?category=%d", cat.ID), nil, &r, 200)
	if len(r.Items) != 3 {
		t.Fatalf("all versions: %+v", r.Items)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/data/core.html.task?category=%d&prefer=de", cat.ID), nil, &r, 200)
	if len(r.Items) != 2 {
		t.Fatalf("prefer=de: %+v", r.Items)
	}
}
