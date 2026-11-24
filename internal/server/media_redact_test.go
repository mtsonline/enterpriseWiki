package server_test

import (
	"bytes"
	"fmt"
	"image/color"
	"image/png"
	"testing"
)

// TestMediaRedactAndPurge: an area is made unreadable as a new version;
// the older versions (with the original) are deleted with their files only
// by someone who manages the medium; afterwards no way leads to the
// original any more, and the audit log names it.
func TestMediaRedactAndPurge(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var folder struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Photos"}, "hidden": true}, &folder, 201)
	var bob struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd!x", "display_name": "Bob Baum"}, &bob, 201)
	// bob may view and edit, but not manage
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", folder.ID), map[string]any{"subject_type": "user", "subject_id": bob.ID, "permissions": 3, "inherit": true}, nil, 201)
	bc := e.client()
	bc.login("bob", "Bob-Passw0rd!x")

	code, img := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": folder.ID, "title": "Car", "alt": "A car"}, "car.png", testPNG(120, 80))
	if code != 201 {
		t.Fatal(code)
	}
	var first []struct {
		Version int    `json:"version"`
		Hash    string `json:"hash"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/media/%d/versions", img.ID), nil, &first, 200)
	original := first[0].Hash

	// bob makes the number plate unreadable (edit right)
	var edited mediumResp
	bc.must("POST", fmt.Sprintf("/api/v1/media/%d/edit", img.ID), map[string]any{"ops": []map[string]any{{"type": "redact", "x": 20, "y": 30, "w": 40, "h": 20, "mode": "fill"}}}, &edited, 200)
	if edited.Version != 2 {
		t.Fatalf("redacted: %+v", edited)
	}
	_, body, _ := admin.fetch(t, fmt.Sprintf("/api/v1/media/%d/download", img.ID))
	pic, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if c := color.RGBAModel.Convert(pic.At(30, 40)).(color.RGBA); c != (color.RGBA{0, 0, 0, 255}) {
		t.Fatalf("area not black: %v", c)
	}
	if c := color.RGBAModel.Convert(pic.At(5, 5)); c == (color.RGBA{0, 0, 0, 255}) {
		t.Fatal("outside of the area changed")
	}
	bc.must("POST", fmt.Sprintf("/api/v1/media/%d/edit", img.ID), map[string]any{"ops": []map[string]any{{"type": "redact", "x": 0, "y": 0, "w": 10, "h": 10, "mode": "blur"}}}, nil, 400)

	// the original is still there in version 1 – that is what purging is for
	if c, _, _ := admin.fetch(t, fmt.Sprintf("/api/v1/media/%d/view?v=1", img.ID)); c != 200 {
		t.Fatalf("version 1 before purging: %d", c)
	}
	// bob may edit, but not delete versions; guests and the wrong method neither
	bc.must("DELETE", fmt.Sprintf("/api/v1/media/%d/versions", img.ID), nil, nil, 403)
	if c, _, _ := e.client().fetch(t, fmt.Sprintf("/api/v1/media/%d/versions", img.ID)); c != 401 && c != 404 {
		t.Fatalf("guest: %d", c)
	}

	var res struct {
		Versions int `json:"versions"`
		Files    int `json:"files"`
		Kept     int `json:"kept"`
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/media/%d/versions", img.ID), nil, &res, 200)
	if res.Versions != 1 || res.Files != 1 || res.Kept != 0 {
		t.Fatalf("purge: %+v", res)
	}
	var left []struct {
		Version int `json:"version"`
	}
	admin.must("GET", fmt.Sprintf("/api/v1/media/%d/versions", img.ID), nil, &left, 200)
	if len(left) != 1 || left[0].Version != 2 {
		t.Fatalf("versions after purging: %+v", left)
	}
	// no way leads to the original: not the version, not its hash, not the file on disk
	if c, _, _ := admin.fetch(t, fmt.Sprintf("/api/v1/media/%d/view?v=1", img.ID)); c == 200 {
		t.Fatal("version 1 still served")
	}
	admin.must("POST", fmt.Sprintf("/api/v1/media/%d/versions/1/restore", img.ID), nil, nil, 404)
	if c, _, _ := admin.fetch(t, "/api/v1/files/"+original); c == 200 {
		t.Fatal("original still served by its hash")
	}
	var n int
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM files WHERE hash=$1`, original).Scan(&n); err != nil || n != 0 {
		t.Fatalf("file row of the original: %d %v", n, err)
	}
	// the current version still works; purging again does nothing
	if c, _, _ := admin.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", img.ID)); c != 200 {
		t.Fatalf("current version: %d", c)
	}
	admin.must("DELETE", fmt.Sprintf("/api/v1/media/%d/versions", img.ID), nil, &res, 200)
	if res.Versions != 0 {
		t.Fatalf("second purge: %+v", res)
	}
	// the audit log names who deleted which versions
	var events int
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM audit_log WHERE action='media.versions_purge' AND entity_id=$1`, img.ID).Scan(&events); err != nil || events != 2 {
		t.Fatalf("audit: %d %v", events, err)
	}

	// content shared with another medium stays for that one (kept)
	_, a := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": folder.ID}, "a.png", testPNG(30, 30))
	_, b := admin.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": folder.ID}, "b.png", testPNG(30, 30))
	admin.must("POST", fmt.Sprintf("/api/v1/media/%d/edit", a.ID), map[string]any{"ops": []map[string]any{{"type": "rotate", "deg": 90}}}, nil, 200)
	admin.must("DELETE", fmt.Sprintf("/api/v1/media/%d/versions", a.ID), nil, &res, 200)
	if res.Versions != 1 || res.Files != 0 || res.Kept != 1 {
		t.Fatalf("shared content: %+v", res)
	}
	if c, _, _ := admin.fetch(t, fmt.Sprintf("/api/v1/media/%d/view", b.ID)); c != 200 {
		t.Fatalf("the other medium lost its file: %d", c)
	}
}
