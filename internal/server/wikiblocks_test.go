package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestWikiBlocks: include page and excerpt, anchor, page tree, search box
// and "new page from a template" read everything with the permissions of
// the reader; included content never leaves its classification; includes
// are limited over all levels; anchors are unique on a page.
func TestWikiBlocks(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var cat, sub, secretCat, hiddenCat obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Handbook"}}, &cat, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Chapter"}, "parent_id": cat.ID}, &sub, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Board only"}, "parent_id": cat.ID}, &secretCat, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Media folder"}, "parent_id": cat.ID, "hidden": true}, &hiddenCat, 201)
	var rea obj
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rea", "password": "Passw0rd!Passw0rd", "email": "rea@example.com"}, &rea, 201)
	// the reader: the handbook itself and the chapter, not the board category
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "user", "subject_id": rea.ID, "permissions": 1, "inherit": false}, nil, 201)
	for _, c := range []int64{sub.ID, hiddenCat.ID} {
		admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c), map[string]any{"subject_type": "user", "subject_id": rea.ID, "permissions": 1}, nil, 201)
	}
	reader := e.client()
	reader.login("rea", "Passw0rd!Passw0rd")

	page := func(c int64, title, src string) int64 {
		t.Helper()
		var el obj
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c, "type": "core.html", "title": title, "body": map[string]any{"format": "markdown", "source": src}}, &el, 201)
		return el.ID
	}
	view := func(c *client, id int64) string {
		t.Helper()
		var v struct {
			HTML string `json:"html"`
		}
		c.must("GET", fmt.Sprintf("/api/v1/elements/%d", id), nil, &v, 200)
		return v.HTML
	}
	// the secret category is closed to the reader: is it really?
	if code := reader.do("GET", fmt.Sprintf("/api/v1/categories/%d", secretCat.ID), nil, nil); code != 403 && code != 404 {
		t.Fatalf("test setup: the reader sees the closed category (%d)", code)
	}

	// --- include and excerpt
	source := page(sub.ID, "Holiday rules", "Intro text okapi.\n\n:::excerpt\nThe short version: ten days.\n:::\n\n:::excerpt{name=\"long\"}\nThe long version: twenty days.\n:::\n")
	secret := page(secretCat.ID, "Board salaries", "Salary okapi-secret.")
	host := page(cat.ID, "Overview", fmt.Sprintf("::include{page=%d}\n\n::include{page=%d excerpt=true title=false}\n\n::include{page=%d excerpt=true name=\"long\"}\n\n::include{page=%d}\n", source, source, source, secret))
	h := view(reader, host)
	for _, want := range []string{"Intro text okapi", "The short version", "The long version", fmt.Sprintf(`href="/e/%d"`, source)} {
		if !strings.Contains(h, want) {
			t.Errorf("include: %q missing in %s", want, h)
		}
	}
	// the excerpts alone, without the intro: only the full include has it
	if n := strings.Count(h, "Intro text okapi"); n != 1 {
		t.Errorf("include: the whole page %d times (want 1: the excerpts carry only their part): %s", n, h)
	}
	if strings.Contains(h, "okapi-secret") || strings.Contains(h, "Board salaries") {
		t.Fatalf("include shows a page the reader may not see: %s", h)
	}
	if !strings.Contains(h, "not there or not visible") {
		t.Errorf("include of a hidden page: no note: %s", h)
	}
	// the administrator sees it (same page, other reader: nothing is cached across people)
	if !strings.Contains(view(admin, host), "okapi-secret") {
		t.Errorf("include: the administrator does not see the page they may see")
	}
	// unknown excerpt, itself
	h = view(admin, page(cat.ID, "Bad", fmt.Sprintf("::include{page=%d excerpt=true name=\"nope\"}\n", source)))
	if !strings.Contains(h, "ew-macro-error") || strings.Contains(h, "Intro text") {
		t.Errorf("unknown excerpt: %s", h)
	}

	// --- classification: stricter content is not shown in a less strict page (not even its title)
	conf := page(sub.ID, "Merger plans", "Plan okapi-merger.")
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d/acl", conf), map[string]any{"classification": 2}, nil, 200)
	h = view(admin, page(cat.ID, "Open page", fmt.Sprintf("::include{page=%d}\n", conf)))
	if strings.Contains(h, "okapi-merger") || strings.Contains(h, "Merger plans") || !strings.Contains(h, "classified stricter") {
		t.Fatalf("confidential content in an open page: %s", h)
	}
	// in a page classified as strictly, it is shown
	strict := page(cat.ID, "Confidential overview", fmt.Sprintf("::include{page=%d}\n", conf))
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d/acl", strict), map[string]any{"classification": 2}, nil, 200)
	if !strings.Contains(view(admin, strict), "okapi-merger") {
		t.Errorf("include into an equally classified page: not shown")
	}

	// --- a loop stops; includes are limited over all levels
	a := page(cat.ID, "A", "A text")
	b := page(cat.ID, "B", fmt.Sprintf("B text\n\n::include{page=%d}\n", a))
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", a), map[string]any{"title": "A", "body": map[string]any{"format": "markdown", "source": fmt.Sprintf("A text\n\n::include{page=%d}\n", b)}}, nil, 200)
	if h := view(admin, a); !strings.Contains(h, "nested too deeply") {
		t.Errorf("include loop: does not stop with a note: %.400s", h)
	}
	var many strings.Builder
	for range 60 {
		fmt.Fprintf(&many, "::include{page=%d title=false}\n\n", source)
	}
	h = view(admin, page(cat.ID, "Many", many.String()))
	if n := strings.Count(h, "Intro text okapi"); n != 50 {
		t.Errorf("includes per page: %d shown, want the limit 50", n)
	}
	if !strings.Contains(h, "includes too many pages") {
		t.Errorf("includes per page: no note on the ones left out")
	}

	// --- a page as the footer: a content block that includes a page, read with
	// the permissions of the visitor and with the same limit of includes
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", sub.ID), map[string]any{"subject_type": "guest", "permissions": 1}, nil, 201)
	footer := page(sub.ID, "Footer page", "Imprint walrus-footer")
	var fb strings.Builder
	fmt.Fprintf(&fb, "::include{page=%d title=false}\n\n::include{page=%d}\n\n", footer, secret)
	for range 60 {
		fmt.Fprintf(&fb, "::include{page=%d title=false}\n\n", footer)
	}
	admin.must("POST", "/api/v1/x/core.blocks/blocks", map[string]any{"name": "Footer", "location": "footer", "audience": "all", "enabled": true,
		"bodies": map[string]any{"en": map[string]string{"format": "markdown", "source": fb.String()}}}, nil, 201)
	var blocks []struct {
		HTML string `json:"html"`
	}
	e.client().must("GET", "/api/v1/x/core.blocks/blocks?lang=en", nil, &blocks, 200)
	if len(blocks) != 1 || !strings.Contains(blocks[0].HTML, "walrus-footer") || strings.Contains(blocks[0].HTML, "okapi-secret") {
		t.Fatalf("a page as the footer for guests: %+v", blocks)
	}
	if n := strings.Count(blocks[0].HTML, "walrus-footer"); n != 49 {
		t.Errorf("includes in a content block: %d shown, want the limit 50 (one of them the hidden page)", n)
	}

	// --- anchors: valid names, unique on a page, not like ids of the interface
	h = view(admin, page(cat.ID, "Anchors", "Text :anchor{name=\"budget\"} and :anchor{name=\"budget\"} and :anchor{name=\"main\"} and :anchor{name=\"cb-3\"} and :anchor{name=\"step-2-a\"}"))
	if strings.Count(h, `id="budget"`) != 1 || !strings.Contains(h, "twice") {
		t.Errorf("anchor twice: %s", h)
	}
	if strings.Contains(h, `id="main"`) || strings.Contains(h, `id="cb-3"`) || !strings.Contains(h, `id="step-2-a"`) {
		t.Errorf("anchor names: %s", h)
	}

	// --- page tree: what the reader may see, no hidden categories
	tree := page(cat.ID, "Map", fmt.Sprintf("::pagetree{category=%d depth=3}\n", cat.ID))
	h = view(reader, tree)
	for _, want := range []string{"Chapter", "Holiday rules", `data-ew-widget="core.macros.pagetree"`, `<nav class="ew-pagetree" aria-label="Pages of Handbook"`} {
		if !strings.Contains(h, want) {
			t.Errorf("page tree: %q missing: %s", want, h)
		}
	}
	for _, not := range []string{"Board only", "Board salaries", "Media folder", ">Map<"} {
		if strings.Contains(h, not) {
			t.Errorf("page tree shows %q: %s", not, h)
		}
	}
	if !strings.Contains(view(admin, tree), "Board only") {
		t.Errorf("page tree: the administrator does not see the closed category")
	}

	// --- search box: a plain form, scoped to the category; the search honours it
	h = view(reader, page(cat.ID, "Find", fmt.Sprintf("::searchbox{category=%d label=\"Find in the chapter\"}\n\n::searchbox{scope=all}\n", sub.ID)))
	if !strings.Contains(h, fmt.Sprintf(`name="category" value="%d"`, sub.ID)) || strings.Count(h, `name="category"`) != 1 || !strings.Contains(h, "Find in the chapter") {
		t.Errorf("search box: %s", h)
	}
	// two boxes on a page: two labels with their own fields
	if strings.Count(h, `<label for="ew-searchbox-`) != 2 || strings.Count(h, `<input id="ew-searchbox-`) != 2 {
		t.Errorf("search box labels: %s", h)
	}
	// a page with a search box included twice: still one id per field
	boxPage := page(cat.ID, "Box", "::searchbox{scope=all}\n")
	h = view(admin, page(cat.ID, "Twice", fmt.Sprintf("::include{page=%d}\n\n::include{page=%d}\n", boxPage, boxPage)))
	ids := map[string]bool{}
	for _, part := range strings.Split(h, `<input id="`)[1:] {
		id := part[:strings.Index(part, `"`)]
		if ids[id] {
			t.Errorf("search box included twice: id %s twice", id)
		}
		ids[id] = true
	}
	if len(ids) != 2 {
		t.Errorf("search box included twice: %d fields", len(ids))
	}
	var sr struct {
		Elements []struct {
			ElementID int64 `json:"element_id"`
		} `json:"elements"`
	}
	page(cat.ID, "Okapi outside", "okapi outside the chapter")
	reader.must("GET", fmt.Sprintf("/api/v1/search?q=okapi&category=%d", sub.ID), nil, &sr, 200)
	if len(sr.Elements) == 0 {
		t.Errorf("scoped search finds nothing")
	}
	for _, x := range sr.Elements {
		if x.ElementID != source && x.ElementID != conf {
			t.Errorf("scoped search: page %d outside the category", x.ElementID)
		}
	}
	if code := reader.do("GET", fmt.Sprintf("/api/v1/search?q=okapi&category=%d", secretCat.ID), nil, nil); code < 400 {
		t.Errorf("scoped search in a closed category: %d", code)
	}

	// --- new page from a template: only for people who may create pages there
	var tpl obj
	admin.must("POST", "/api/v1/templates", map[string]any{"name": "Meeting notes", "type": "core.html", "lang": "en", "title": "Meeting", "global": true,
		"body": map[string]any{"format": "markdown", "source": "## Agenda"}}, &tpl, 201)
	np := page(cat.ID, "Start", fmt.Sprintf("::newpage{template=%d}\n", tpl.ID))
	h = view(admin, np)
	if !strings.Contains(h, fmt.Sprintf(`href="/c/%d/new?type=core.html&amp;template=%d"`, cat.ID, tpl.ID)) || !strings.Contains(h, "Meeting notes") {
		t.Errorf("new page button: %s", h)
	}
	if h := view(reader, np); strings.Contains(h, "/new?") {
		t.Errorf("new page button for a reader without the right to create: %s", h)
	}
	if h := view(admin, page(cat.ID, "Start 2", "::newpage{template=999999}\n")); strings.Contains(h, "/new?") {
		t.Errorf("new page button with an unknown template: %s", h)
	}
}

// TestContentIDs: ids in content never take ids of the interface and stay
// unique on the page shown, with included pages and anchors.
func TestContentIDs(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var cat struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "IDs"}}, &cat, 201)
	page := func(title, src string) (int64, string) {
		t.Helper()
		var el struct {
			ID   int64  `json:"id"`
			HTML string `json:"html"`
		}
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": title, "body": map[string]any{"format": "markdown", "source": src}}, &el, 201)
		admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", el.ID), nil, &el, 200)
		return el.ID, el.HTML
	}
	_, h := page("Headings", "::toc\n\n## Main\n\n## EW tools\n\n## Q 12\n\n## F12-name\n\n## Budget\n\n## Budget\n\n<h2 id=\"search\">Raw</h2>\n\nText :anchor{name=\"budget\"}\n")
	for _, want := range []string{`id="h-main"`, `id="h-ew-tools"`, `id="h-q-12"`, `id="h-f12-name"`, `id="budget"`, `id="budget-1"`, `id="h-search"`, `href="#h-main"`, `href="#budget-1"`, `<span class="ew-macro-error"`} {
		if !strings.Contains(h, want) {
			t.Errorf("%s missing: %s", want, h)
		}
	}
	for _, not := range []string{`id="main"`, `id="search"`, `id="q-12"`} {
		if strings.Contains(h, not) {
			t.Errorf("%s in content: %s", not, h)
		}
	}
	// the anchor may not take the id of a heading
	if strings.Count(h, `id="budget"`) != 1 || !strings.Contains(h, "twice") {
		t.Errorf("anchor with the id of a heading: %s", h)
	}
	// an included page with the same headings: its ids get their own number,
	// its table of contents follows them
	inc, _ := page("Included", "::toc\n\n## Intro\n\n## Details\n")
	_, h = page("Host", fmt.Sprintf("## Intro\n\n::include{page=%d}\n", inc))
	if strings.Count(h, `id="intro"`) != 1 || !strings.Contains(h, `id="intro-2"`) || !strings.Contains(h, `href="#intro-2"`) {
		t.Errorf("ids of an included page: %s", h)
	}
}
