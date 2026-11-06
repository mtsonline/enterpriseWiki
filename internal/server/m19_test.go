package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
)

type mediumResp struct {
	ID          int64    `json:"id"`
	CategoryID  int64    `json:"category_id"`
	Version     int      `json:"version"`
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	Permissions []string `json:"permissions"`
	ViewOnly    bool     `json:"view_only"`
	OwnACL      bool     `json:"own_acl"`
	Mime        string   `json:"mime"`
}

func testPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// uploadMedium posts meta + file to /api/v1/media (or path).
func (c *client) uploadMedium(t *testing.T, path string, meta map[string]any, name string, data []byte) (int, mediumResp) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mj, _ := json.Marshal(meta)
	_ = mw.WriteField("meta", string(mj))
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.e.srv.URL+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "ewiki")
	if c.host != "" {
		req.Host = c.host
	}
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var m mediumResp
	b, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(b, &m)
	if res.StatusCode >= 300 {
		t.Logf("upload %s: %d %s", path, res.StatusCode, b)
	}
	return res.StatusCode, m
}

func (c *client) fetch(t *testing.T, path string) (int, []byte, http.Header) {
	t.Helper()
	req, _ := http.NewRequest("GET", c.e.srv.URL+path, nil)
	if c.host != "" {
		req.Host = c.host
	}
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b, res.Header
}

func TestMediaLibrary(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var folder, public struct {
		ID int64 `json:"id"`
	}
	// a hidden category as folder, only for the team; a public page category
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Media"}, "hidden": true}, &folder, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"guest_visible": true, "titles": map[string]string{"en": "News"}}, &public, 201)
	var bob struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd!x", "display_name": "Bob Baum"}, &bob, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", folder.ID), map[string]any{"subject_type": "user", "subject_id": bob.ID, "permissions": 1, "inherit": true}, nil, 201)
	bc := e.client()
	bc.login("bob", "Bob-Passw0rd!x")
	guest := e.client()

	// hidden categories stay out of the navigation
	var tree []struct {
		ID int64 `json:"id"`
	}
	bc.must("GET", "/api/v1/categories", nil, &tree, 200)
	for _, c := range tree {
		if c.ID == folder.ID {
			t.Fatal("hidden category in the navigation")
		}
	}

	// upload: rights of the category
	code, img := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": folder.ID, "title": "Team", "alt": "The team"}, "team.png", testPNG(120, 80))
	if code != 201 || img.Width != 120 || img.Version != 1 {
		t.Fatalf("upload: %d %+v", code, img)
	}
	if c, _, _ := guest.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", img.ID)); c != 401 && c != 404 {
		t.Fatalf("guest sees a medium of a closed folder: %d", c)
	}
	var seen mediumResp
	bc.must("GET", fmt.Sprintf("/api/v1/media/%d", img.ID), nil, &seen, 200)
	if seen.ViewOnly || len(seen.Permissions) != 2 { // view, download
		t.Fatalf("bob: %+v", seen)
	}
	if c, body, hdr := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/view?w=60", img.ID)); c != 200 || hdr.Get("Content-Type") != "image/png" {
		t.Fatalf("thumbnail: %d %s", c, hdr.Get("Content-Type"))
	} else if cfg, _, err := image.DecodeConfig(bytes.NewReader(body)); err != nil || cfg.Width != 60 {
		t.Fatalf("thumbnail size: %+v %v", cfg, err)
	}
	// bob cannot upload into a folder he can only see
	if c, _ := bc.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": folder.ID}, "x.png", testPNG(4, 4)); c != 403 {
		t.Fatalf("upload without the right: %d", c)
	}

	// embedded in a public page: guests see it there
	var page struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": public.ID, "type": "core.html", "lang": "en", "title": "Hello",
		"body": htmlBody(fmt.Sprintf(`<p><img src="/api/v1/media/%d/view" alt="The team"></p>`, img.ID))}, &page, 201)
	if c, _, _ := guest.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", img.ID)); c != 200 {
		t.Fatalf("guest does not see an embedded medium: %d", c)
	}
	var usage []int64
	admin.must("GET", fmt.Sprintf("/api/v1/media/%d/usage", img.ID), nil, &usage, 200)
	if len(usage) != 1 || usage[0] != page.ID {
		t.Fatalf("usage: %v", usage)
	}

	// own rights replace category and embedding: only bob, view only
	admin.must("PATCH", fmt.Sprintf("/api/v1/media/%d", img.ID), map[string]any{"acl": []map[string]any{{"subject_type": "user", "subject_id": bob.ID, "perms": []string{"view"}}}}, nil, 200)
	if c, _, _ := guest.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", img.ID)); c == 200 {
		t.Fatal("own rights did not replace the page's")
	}
	bc.must("GET", fmt.Sprintf("/api/v1/media/%d", img.ID), nil, &seen, 200)
	if !seen.ViewOnly {
		t.Fatalf("bob should only view: %+v", seen)
	}
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", img.ID)); c != 403 {
		t.Fatalf("view only, but download: %d", c)
	}
	// bob cannot give rights he does not have
	bc.must("PATCH", fmt.Sprintf("/api/v1/media/%d", img.ID), map[string]any{"acl": []map[string]any{{"subject_type": "guest", "perms": []string{"view"}}}}, nil, 403)
	admin.must("PATCH", fmt.Sprintf("/api/v1/media/%d", img.ID), map[string]any{"own_acl": false}, nil, 200)

	// image editor: new version, old one in the history
	var edited mediumResp
	admin.must("POST", fmt.Sprintf("/api/v1/media/%d/edit", img.ID), map[string]any{"ops": []map[string]any{{"type": "crop", "x": 10, "y": 10, "w": 50, "h": 40}, {"type": "rotate", "deg": 90}}, "note": "cropped"}, &edited, 200)
	if edited.Version != 2 || edited.Width != 40 || edited.Height != 50 {
		t.Fatalf("edited: %+v", edited)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/media/%d/edit", img.ID), map[string]any{"ops": []map[string]any{{"type": "explode"}}}, nil, 400)
	var versions []struct {
		Version int `json:"version"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/media/%d/versions", img.ID), nil, &versions, 200)
	if len(versions) != 2 {
		t.Fatalf("versions: %+v", versions)
	}
	bc.must("GET", fmt.Sprintf("/api/v1/media/%d/versions", img.ID), nil, nil, 403)
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/view?v=1", img.ID)); c != 403 {
		t.Fatalf("old version for a reader: %d", c)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/media/%d/versions/1/restore", img.ID), nil, &edited, 200)
	if edited.Version != 3 || edited.Width != 120 {
		t.Fatalf("restored: %+v", edited)
	}

	// watermark with the viewer's name: the view differs per viewer, the
	// file is never served raw by hash
	code, wm := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": folder.ID, "watermark": map[string]any{"mode": "viewer_view", "user": true, "date": true}}, "plan.png", testPNG(300, 200))
	if code != 201 {
		t.Fatal(code)
	}
	_, a, _ := admin.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", wm.ID))
	_, b, hdr := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", wm.ID))
	if bytes.Equal(a, b) || len(b) == 0 {
		t.Fatal("watermark is not personal")
	}
	if hdr.Get("Cache-Control") == "" || hdr.Get("Content-Security-Policy") == "" {
		t.Fatalf("headers: %v", hdr)
	}
	_, orig, _ := admin.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", wm.ID))
	if !bytes.Equal(orig, testPNG(300, 200)) {
		t.Fatal("download of viewer_view is not the original")
	}
	// "original not stored": even the download carries the uploader's mark
	code, only := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": folder.ID, "watermark": map[string]any{"mode": "uploader_only", "user": true}}, "secret.png", testPNG(300, 200))
	if code != 201 {
		t.Fatal(code)
	}
	_, dl, _ := admin.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", only.ID))
	if bytes.Equal(dl, testPNG(300, 200)) {
		t.Fatal("original stored although not wanted")
	}
	admin.must("PATCH", fmt.Sprintf("/api/v1/media/%d", only.ID), map[string]any{"watermark": map[string]any{"mode": "none"}}, nil, 400)
	// invalid watermark settings
	if c, _ := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": folder.ID, "watermark": map[string]any{"mode": "viewer_all"}}, "x.png", testPNG(4, 4)); c != 400 {
		t.Fatalf("watermark without text: %d", c)
	}

	// download switched off: view only even with the right
	admin.must("PATCH", fmt.Sprintf("/api/v1/media/%d", img.ID), map[string]any{"download": false}, nil, 200)
	if c, _, _ := bc.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", img.ID)); c != 403 {
		t.Fatalf("download although switched off: %d", c)
	}

	// list: only visible media
	var list []mediumResp
	bc.must("GET", fmt.Sprintf("/api/v1/media?category=%d", folder.ID), nil, &list, 200)
	if len(list) != 3 {
		t.Fatalf("list for bob: %d", len(list))
	}
	guest.must("GET", fmt.Sprintf("/api/v1/media?category=%d", folder.ID), nil, &list, 200)
	if len(list) != 1 || list[0].ID != img.ID { // only the one embedded in the public page
		t.Fatalf("list for guests: %+v", list)
	}

	// HTML is never shown inline (stored XSS)
	code, html := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": folder.ID}, "x.html", []byte(`<script>alert(1)</script>`))
	if code != 201 {
		t.Fatal(code)
	}
	if c, _, hdr := admin.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", html.ID)); c != 200 || hdr.Get("Content-Disposition")[:10] != "attachment" {
		t.Fatalf("html download: %d %s", c, hdr.Get("Content-Disposition"))
	}

	// delete
	admin.must("DELETE", fmt.Sprintf("/api/v1/media/%d", wm.ID), nil, nil, 204)
	admin.must("GET", fmt.Sprintf("/api/v1/media/%d", wm.ID), nil, nil, 404)
	bc.must("DELETE", fmt.Sprintf("/api/v1/media/%d", img.ID), nil, nil, 403)
}

// TestHiddenCategorySearch: pages in hidden categories (and below) are not
// found by the general search, only when searching in that category.
func TestHiddenCategorySearch(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var folder, sub struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Hidden stuff"}, "hidden": true}, &folder, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": folder.ID, "titles": map[string]string{"en": "Deeper"}}, &sub, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": sub.ID, "type": "core.html", "lang": "en", "title": "Wombat notes",
		"body": htmlBody(`<p>wombat</p>`)}, nil, 201)
	var res struct {
		Elements   []any `json:"elements"`
		Categories []any `json:"categories"`
	}
	admin.must("GET", "/api/v1/search?q=wombat", nil, &res, 200)
	if len(res.Elements) != 0 {
		t.Fatalf("page of a hidden category found: %+v", res.Elements)
	}
	admin.must("GET", "/api/v1/search?q=Hidden", nil, &res, 200)
	if len(res.Categories) != 0 {
		t.Fatalf("hidden category found: %+v", res.Categories)
	}
	admin.must("GET", fmt.Sprintf("/api/v1/search?q=wombat&category=%d", folder.ID), nil, &res, 200)
	if len(res.Elements) != 1 {
		t.Fatalf("search inside the hidden category: %+v", res.Elements)
	}
}
