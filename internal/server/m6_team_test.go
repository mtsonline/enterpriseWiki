package server_test

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestTeamAndProfile(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var sales, it struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "Sales", "description": "We sell"}, &sales, 200)
	admin.must("POST", "/api/v1/admin/groups", map[string]any{"name": "IT"}, &it, 200)
	var zoe, adam struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "zoe", "password": "Passw0rd!Passw0rd", "display_name": "Zoe Zimmer", "email": "zoe@example.com", "groups": []int64{sales.ID, it.ID}}, &zoe, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "adam", "password": "Passw0rd!Passw0rd", "display_name": "Adam Ast", "email": "adam@example.com", "groups": []int64{sales.ID}}, &adam, 201)
	zc := e.client()
	zc.login("zoe", "Passw0rd!Passw0rd")

	// own business card, with a photo
	var img bytes.Buffer
	_ = png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "me.png")
	_, _ = io.Copy(fw, &img)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/files", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "ewiki")
	resp, err := zc.http.Do(req)
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("upload: %v %d", err, resp.StatusCode)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	hash := strings.Split(strings.Split(string(data), `"hash":"`)[1], `"`)[0]
	zc.must("PUT", "/api/v1/me/profile", map[string]string{"job_title": "Head of Sales", "phone": "+43 1 234", "photo": hash, "about": "Loves **cycling**"}, nil, 200)
	if code := zc.do("PUT", "/api/v1/me/profile", map[string]string{"salary": "1"}, nil); code != 400 {
		t.Fatalf("unknown field: %d", code)
	}
	if code := zc.do("PUT", "/api/v1/me/profile", map[string]string{"photo": strings.Repeat("a", 64)}, nil); code != 400 {
		t.Fatalf("photo that is not an image: %d", code)
	}
	if code := zc.do("PUT", fmt.Sprintf("/api/v1/admin/users/%d/profile", adam.ID), map[string]string{"job_title": "x"}, nil); code != 403 {
		t.Fatalf("user edits another profile: %d", code)
	}
	if code := zc.do("GET", fmt.Sprintf("/api/v1/admin/users/%d/profile", adam.ID), nil, nil); code != 403 {
		t.Fatalf("user reads admin profile route: %d", code)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d/profile", adam.ID), map[string]string{"job_title": "Account manager", "department": "Sales"}, nil, 200)

	// team page
	var c cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "About us"}}, &c, 201)
	var team elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.team", "title": "Our team",
		"body": map[string]any{"groups": []int64{sales.ID, it.ID}, "fields": []string{"photo", "job_title", "phone", "about", "groups"}, "layout": "cards", "by_group": true,
			"description": map[string]string{"format": "markdown", "source": "The **people** behind it"}}}, &team, 201)
	if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": c.ID, "type": "content.team", "title": "Bad",
		"body": map[string]any{"fields": []string{"password_hash"}}}, nil); code != 400 {
		t.Fatalf("unknown field accepted: %d", code)
	}
	var view struct {
		HTML string `json:"html"`
	}
	e.client().must("GET", fmt.Sprintf("/api/v1/elements/%d", team.ID), nil, &view, 200)
	for _, want := range []string{"<strong>people</strong>", `<h2 class="ewt-group-name">Sales</h2>`, "Head of Sales", `href="tel:+431234"`, "/api/v1/files/" + hash,
		"<strong>cycling</strong>", `<span class="ewt-photo ewt-initials" aria-hidden="true">AA</span>`, "IT, Sales"} {
		if !strings.Contains(view.HTML, want) {
			t.Fatalf("team html misses %q: %s", want, view.HTML)
		}
	}
	if strings.Contains(view.HTML, "zoe@example.com") {
		t.Fatal("e-mail shown although not chosen")
	}
	// Adam sorted before Zoe within Sales
	if strings.Index(view.HTML, "Adam Ast") > strings.Index(view.HTML, "Zoe Zimmer") {
		t.Fatal("members not sorted by name")
	}
	var members struct {
		Sections []struct {
			Name    string `json:"name"`
			Members []struct {
				Name string `json:"name"`
			} `json:"members"`
		} `json:"sections"`
	}
	zc.must("GET", fmt.Sprintf("/api/v1/x/content.team/elements/%d/members", team.ID), nil, &members, 200)
	if len(members.Sections) != 2 || len(members.Sections[0].Members) != 2 || len(members.Sections[1].Members) != 1 {
		t.Fatalf("members api: %+v", members)
	}
	var groups []struct {
		Name    string `json:"name"`
		Members int    `json:"members"`
	}
	zc.must("GET", "/api/v1/x/content.team/groups", nil, &groups, 200)
	if len(groups) < 2 {
		t.Fatalf("groups: %+v", groups)
	}
	zc.must("GET", "/api/v1/x/content.team/fields", nil, nil, 200)
	if code := e.client().do("GET", "/api/v1/x/content.team/groups", nil, nil); code != 401 {
		t.Fatalf("guest lists groups: %d", code)
	}
}
