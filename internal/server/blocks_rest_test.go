package server_test

import (
	"strings"
	"testing"
)

// TestBlocksRest: widths of grid columns (checked for room), icons in
// buttons (decoration: the text names the button), news as tiles with a
// filter by tag, the live search of the search box.
func TestBlocksRest(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var cat struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Rest"}}, &cat, 201)
	render := func(src string) string {
		t.Helper()
		var el struct {
			ID   int64  `json:"id"`
			HTML string `json:"html"`
		}
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "R", "body": map[string]any{"format": "markdown", "source": src}, "tags": []string{"hr"}}, &el, 201)
		admin.must("GET", "/api/v1/elements/"+itoa(el.ID), nil, &el, 200)
		return el.HTML
	}
	// grid widths
	h := render(":::grid{desktop=2 widths=\"2 1\"}\nA\n***\nB\n:::\n")
	if !strings.Contains(h, `style="--ewl-widths: 2fr 1fr"`) {
		t.Errorf("grid widths: %s", h)
	}
	for _, bad := range []string{`desktop=2 widths="2 1 1"`, `desktop=3 widths="1 5 5"`, `desktop=2 widths="2 x"`, `desktop=2 widths="9 1"`} {
		if h := render(":::grid{" + bad + "}\nA\n***\nB\n:::\n"); !strings.Contains(h, "ew-macro-error") || strings.Contains(h, "--ewl-widths") {
			t.Errorf("grid %s accepted: %s", bad, h)
		}
	}
	// a button with an icon: the icon is decoration
	h = render("::buttons{text1=\"Call\" link1=\"/\" icon1=\"phone\"}\n\n::cta{title=\"T\" button=\"Go\" link=\"/\" icon=\"arrow-right\"}\n")
	if !strings.Contains(h, `data-icon="phone"`) || !strings.Contains(h, `data-icon="arrow-right"`) || strings.Count(h, `aria-hidden="true"`) < 2 || strings.Contains(h, `role="img"`) {
		t.Errorf("buttons with icons: %s", h)
	}
	if h := render("::buttons{text1=\"Call\" link1=\"/\" icon1=\"Bad Name\"}\n"); !strings.Contains(h, "ew-macro-error") || strings.Contains(h, "Bad Name\"") {
		t.Errorf("a bad icon name: %s", h)
	}
	// news as tiles with the filter: the tags of the items for the browser
	h = render("::news{layout=tiles columns=3 tablet=2 tag_buttons=true}\n")
	if !strings.Contains(h, `class="ew-news ew-news--tiles" style="--ew-tiles:3;--ew-tiles-t:2"`) || !strings.Contains(h, `data-ew-widget="core.macros.newsfilter"`) || !strings.Contains(h, `data-tags="hr"`) {
		t.Errorf("news tiles: %s", h)
	}
	if h := render("::news{layout=tiles columns=5 tablet=3}\n"); strings.Contains(h, "ew-macro-error") {
		t.Errorf("tiles with as many columns as have room: %s", h)
	}
	// the live search of a search box (on unless switched off)
	if h := render("::searchbox{scope=all}\n\n::searchbox{scope=all live=false}\n"); strings.Count(h, `data-ew-widget="core.macros.searchlive"`) != 1 {
		t.Errorf("live search: %s", h)
	}
}
