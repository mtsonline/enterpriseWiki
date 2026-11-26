package server_test

import (
	"strconv"
	"strings"
	"testing"
)

// Tags are edited together with the page: saving sets them, null keeps them,
// a draft without tags keeps them and [] removes them on publishing.
func TestTagsWithSave(t *testing.T) {
	e := setup(t)
	c := e.client()
	c.login("admin", adminPW)
	var cat, el struct {
		ID int64 `json:"id"`
	}
	c.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Tags"}}, &cat, 201)
	c.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "T", "body": htmlBody("<p>x</p>"), "tags": []string{"one"}}, &el, 201)
	path := "/api/v1/elements/" + strconv.FormatInt(el.ID, 10)
	tags := func() []string {
		var v struct {
			Tags []string `json:"tags"`
		}
		c.must("GET", path, nil, &v, 200)
		return v.Tags
	}
	rev := func() int {
		var v struct {
			Rev int `json:"rev"`
		}
		c.must("GET", path, nil, &v, 200)
		return v.Rev
	}
	body := map[string]string{"format": "markdown", "source": "x"}
	r := rev()
	c.must("PUT", path, map[string]any{"lang": "en", "title": "T", "body": body, "base_rev": r, "tags": []string{"Two", "#three"}}, nil, 200)
	if got := strings.Join(tags(), ","); got != "two,three" {
		t.Fatalf("tags after save: %s", got)
	}
	r = rev()
	c.must("PUT", path, map[string]any{"lang": "en", "title": "T2", "body": body, "base_rev": r}, nil, 200)
	if got := strings.Join(tags(), ","); got != "two,three" {
		t.Fatalf("tags kept: %s", got)
	}
	// invalid tags: nothing is saved
	r = rev()
	if got := c.do("PUT", path, map[string]any{"lang": "en", "title": "T3", "body": body, "base_rev": r, "tags": []string{"no spaces allowed"}}, nil); got != 400 {
		t.Fatalf("invalid tags: %d", got)
	}
	if rev() != r {
		t.Fatal("revision saved despite invalid tags")
	}
	// drafts
	var d struct {
		ID int64 `json:"id"`
	}
	c.must("POST", "/api/v1/drafts", map[string]any{"element_id": el.ID, "lang": "en", "title": "D", "body": body, "base_rev": rev()}, &d, 201)
	c.must("POST", "/api/v1/drafts/"+strconv.FormatInt(d.ID, 10)+"/publish", map[string]any{}, nil, 200)
	if got := strings.Join(tags(), ","); got != "two,three" {
		t.Fatalf("draft without tags changed them: %s", got)
	}
	c.must("POST", "/api/v1/drafts", map[string]any{"element_id": el.ID, "lang": "en", "title": "D2", "body": body, "base_rev": rev(), "tags": []string{}}, &d, 201)
	c.must("POST", "/api/v1/drafts/"+strconv.FormatInt(d.ID, 10)+"/publish", map[string]any{}, nil, 200)
	if got := tags(); len(got) != 0 {
		t.Fatalf("draft with [] kept tags: %v", got)
	}
}
