package server_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestAttachmentsAndCode: the attachments list shows the files a page shows
// or the files of a category, only those the reader may see, a file that
// may only be viewed without a download link, and the upload button only to
// people who may add files. Code blocks keep language and line numbers.
func TestAttachmentsAndCode(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	type obj struct {
		ID int64 `json:"id"`
	}
	var cat, closed obj
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Files"}}, &cat, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Closed"}}, &closed, 201)
	users := map[string]int64{}
	clients := map[string]*client{}
	for _, n := range []string{"rea", "edi"} {
		var u obj
		admin.must("POST", "/api/v1/admin/users", map[string]any{"username": n, "password": "Passw0rd!Passw0rd", "email": n + "@example.com"}, &u, 201)
		users[n] = u.ID
		clients[n] = e.client()
	}
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "user", "subject_id": users["rea"], "permissions": 1}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", cat.ID), map[string]any{"subject_type": "user", "subject_id": users["edi"], "permissions": 1 | 2 | 4}, nil, 201)
	for n, c := range clients {
		c.login(n, "Passw0rd!Passw0rd")
	}
	rea, edi := clients["rea"], clients["edi"]

	up := func(name string, data []byte, meta map[string]any) int64 {
		t.Helper()
		meta["category_id"] = cat.ID
		code, m := admin.uploadMedium(t, "/api/v1/media", meta, name, data)
		if code != 201 {
			t.Fatalf("upload %s: %d", name, code)
		}
		return m.ID
	}
	big := up("big.png", testPNG(300, 300), map[string]any{"title": "Big picture"})
	small := up("small.png", testPNG(4, 4), map[string]any{"title": "Small picture"})
	viewOnly := up("plan.png", testPNG(20, 20), map[string]any{"title": "View-only plan", "download": false})
	secret := up("secret.png", testPNG(8, 8), map[string]any{"title": "Secret picture",
		"acl": []map[string]any{{"subject_type": "user", "subject_id": users["edi"], "perms": []string{"view"}}}})

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

	// --- the files a page shows
	p := page(cat.ID, "Report", fmt.Sprintf("![small](/api/v1/media/%d/view) ![secret](/api/v1/media/%d/view)\n\n::attachments\n", small, secret))
	h := view(rea, p)
	if !strings.Contains(h, "Small picture") || strings.Contains(h, "Big picture") {
		t.Errorf("files of the page: %s", h)
	}
	if strings.Contains(h, "Secret picture") {
		t.Fatalf("attachments show a file the reader may not see: %s", h)
	}
	if !strings.Contains(view(edi, p), "Secret picture") {
		t.Errorf("attachments: edi does not see the file shared with them")
	}
	if strings.Contains(h, "core.macros.upload") {
		t.Errorf("upload button on the list of a page: %s", h)
	}

	// --- the files of a category: order, view only, upload only with the right
	cp := page(cat.ID, "All files", "::attachments{source=category sort=size}\n")
	h = view(rea, cp)
	iBig, iSmall := strings.Index(h, "Big picture"), strings.Index(h, "Small picture")
	if iBig < 0 || iSmall < 0 || iBig > iSmall {
		t.Errorf("largest first: %s", h)
	}
	if !strings.Contains(h, fmt.Sprintf(`/api/v1/media/%d/download`, big)) || strings.Contains(h, fmt.Sprintf(`/api/v1/media/%d/download`, viewOnly)) || !strings.Contains(h, "View-only plan") {
		t.Errorf("download links: %s", h)
	}
	if strings.Contains(h, "core.macros.upload") {
		t.Errorf("upload button for a reader: %s", h)
	}
	if h := view(edi, cp); !strings.Contains(h, fmt.Sprintf(`data-ew-widget="core.macros.upload" data-category="%d"`, cat.ID)) {
		t.Errorf("no upload button for an editor: %s", h)
	}
	// a category the reader may not see
	if h := view(rea, page(cat.ID, "Other files", fmt.Sprintf("::attachments{source=category category=%d}\n", closed.ID))); !strings.Contains(h, "not there or not visible") {
		t.Errorf("files of a closed category: %s", h)
	}
	// only pictures, by name
	h = view(rea, page(cat.ID, "Pictures", "::attachments{source=category type=image sort=name}\n"))
	if strings.Index(h, "Big picture") > strings.Index(h, "Small picture") {
		t.Errorf("by name: %s", h)
	}

	// --- the API: files a page shows, only for a page one may see
	var list []struct {
		ID int64 `json:"id"`
	}
	rea.must("GET", fmt.Sprintf("/api/v1/media?used_by=%d", p), nil, &list, 200)
	if len(list) != 1 || list[0].ID != small {
		t.Errorf("used_by: %+v", list)
	}
	hidden := page(closed.ID, "Hidden", fmt.Sprintf("![b](/api/v1/media/%d/view)", big))
	if code := rea.do("GET", fmt.Sprintf("/api/v1/media?used_by=%d", hidden), nil, nil); code != 404 && code != 403 {
		t.Errorf("used_by of a page the reader may not see: %d", code)
	}
	if code := rea.do("GET", "/api/v1/media?sort=evil", nil, nil); code != 400 {
		t.Errorf("unknown order: %d", code)
	}

	// --- code blocks: language and line numbers survive, the code is escaped
	h = view(rea, page(cat.ID, "Code", "```go lines\nx := 1 < 2\n```\n\n```\n<script>alert(1)</script>\n```\n"))
	if !strings.Contains(h, `<pre data-lines=""><code class="language-go">x := 1 &lt; 2`) || strings.Contains(h, "<script>") {
		t.Errorf("code blocks: %s", h)
	}
	// HTML from the editor (data-lang) becomes the same Markdown
	var el struct {
		ID   int64 `json:"id"`
		Body struct {
			Source string `json:"source"`
		} `json:"body"`
	}
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Code HTML", "body": map[string]any{"format": "html", "source": `<pre data-lang="python lines"><code>print(1)</code></pre>`}}, &el, 201)
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d?edit=1", el.ID), nil, &el, 200)
	if !strings.Contains(el.Body.Source, "```python lines\nprint(1)\n```") {
		t.Errorf("code block from HTML: %q", el.Body.Source)
	}
}
