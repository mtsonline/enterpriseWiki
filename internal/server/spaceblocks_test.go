package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestSpaceBlocks: the list of spaces shows only spaces the reader may see
// (archived and personal ones only if asked); the details of a space only
// for a space the reader may see; a date is written for the language.
func TestSpaceBlocks(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var open, closed, old, sub, rea obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Handbook okapi"}, "descriptions": map[string]string{"en": "All <rules>"}}, &open, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Board walrus"}}, &closed, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Old lynx"}}, &old, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Chapter"}, "parent_id": closed.ID}, &sub, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/categories/%d/space", open.ID), map[string]any{"key": "HB"}, nil, 200)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/archive", old.ID), map[string]any{"archived": true}, nil, 200)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "rea", "password": "Passw0rd!Passw0rd"}, &rea, 201)
	for _, c := range []int64{open.ID, old.ID} {
		admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", c), map[string]any{"subject_type": "user", "subject_id": rea.ID, "permissions": 1}, nil, 201)
	}
	// the reader sees a subcategory of the closed space, not the space itself
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", sub.ID), map[string]any{"subject_type": "user", "subject_id": rea.ID, "permissions": 1}, nil, 201)
	reader := e.client()
	reader.login("rea", "Passw0rd!Passw0rd")
	pageIn := func(c int64, lang, src string) int64 {
		var el obj
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c, "type": "core.html", "lang": lang, "title": "P", "body": map[string]any{"format": "markdown", "source": src}}, &el, 201)
		return el.ID
	}
	page := func(c int64, src string) int64 { return pageIn(c, "en", src) }
	view := func(c *client, id int64, lang string) string {
		var v struct {
			HTML string `json:"html"`
		}
		c.must("GET", fmt.Sprintf("/api/v1/elements/%d?lang=%s", id, lang), nil, &v, 200)
		return v.HTML
	}
	list := page(open.ID, "::spacelist\n\n::spacelist{archived=true layout=list}\n")
	h := view(reader, list, "en")
	parts := strings.SplitN(h, `ew-spacelist--list`, 2)
	if len(parts) != 2 {
		t.Fatalf("two lists: %s", h)
	}
	if !strings.Contains(parts[0], `href="/s/HB"`) || !strings.Contains(parts[0], "Handbook okapi") || strings.Contains(parts[0], "Old lynx") ||
		strings.Contains(h, "Board walrus") || strings.Contains(h, "<rules>") || !strings.Contains(h, "All &lt;rules&gt;") {
		t.Errorf("list of spaces for the reader: %s", h)
	}
	if !strings.Contains(parts[1], "Old lynx") || !strings.Contains(parts[1], "archived") {
		t.Errorf("list with archived spaces: %s", parts[1])
	}
	if h := view(admin, list, "en"); !strings.Contains(h, "Board walrus") {
		t.Errorf("list of spaces for the administrator: %s", h)
	}
	// details: of the own space; nothing of a space the reader may not see
	det := page(open.ID, "::spacedetails\n")
	if h := view(reader, det, "en"); !strings.Contains(h, "<code>HB</code>") || !strings.Contains(h, "Handbook okapi") {
		t.Errorf("details of the space: %s", h)
	}
	inner := page(sub.ID, "::spacedetails\n")
	if h := view(reader, inner, "en"); strings.Contains(h, "Board walrus") {
		t.Errorf("details of a space the reader may not see: %s", h)
	}
	// a date
	date := pageIn(open.ID, "de", "Am :date{value=2026-03-01 format=weekday} and :date{value=2026-02-30}.")
	h = view(reader, date, "de")
	if !strings.Contains(h, `<time class="ew-date" datetime="2026-03-01">Sonntag, 1. März 2026</time>`) {
		t.Errorf("date in German: %s", h)
	}
	if strings.Contains(h, `datetime="2026-02-30"`) {
		t.Errorf("an impossible date: %s", h)
	}
}
