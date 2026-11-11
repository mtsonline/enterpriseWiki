package server_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// uploadPNG uploads a small image as c and returns its hash.
func (c *client) uploadPNG() string {
	c.e.t.Helper()
	var img bytes.Buffer
	_ = png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 3, 3)))
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "me.png")
	_, _ = io.Copy(fw, &img)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.e.srv.URL+"/api/v1/files", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "ewiki")
	res, err := c.http.Do(req)
	if err != nil || res.StatusCode != 201 {
		c.e.t.Fatalf("upload: %v %d", err, res.StatusCode)
	}
	defer res.Body.Close()
	var out struct {
		Hash string `json:"hash"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out.Hash
}

func TestPhotoVisibility(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "pia", "password": "Pia-Passw0rd!x", "display_name": "Pia"}, &u, 201)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "olaf", "password": "Olaf-Passw0rd!x", "display_name": "Olaf"}, nil, 201)
	pia, olaf, guest := e.client(), e.client(), e.client()
	pia.login("pia", "Pia-Passw0rd!x")
	olaf.login("olaf", "Olaf-Passw0rd!x")
	hash := pia.uploadPNG()
	pia.must("PUT", "/api/v1/me/profile", map[string]string{"photo": hash}, nil, 200)
	img := "/api/v1/files/" + hash
	check := func(name string, c *client, want int) {
		t.Helper()
		if got := c.do("GET", img, nil, nil); got != want {
			t.Errorf("%s: %s sees the photo: %d, want %d", name, img, got, want)
		}
	}
	// default: signed-in people
	check("default", guest, 404)
	check("default", olaf, 200)
	// the person's choice within the limits
	pia.must("PUT", "/api/v1/me/profile", map[string]string{"photo_visibility": "public"}, nil, 200)
	check("public", guest, 200)
	pia.must("PUT", "/api/v1/me/profile", map[string]string{"photo_visibility": "private"}, nil, 200)
	check("private", olaf, 404)
	check("private", pia, 200)
	check("private", admin, 200)
	if got := pia.do("PUT", "/api/v1/me/profile", map[string]string{"photo_visibility": "everyone"}, nil); got != 400 {
		t.Fatalf("invalid choice: %d", got)
	}
	// administrators narrow the limit: a public choice is capped
	pia.must("PUT", "/api/v1/me/profile", map[string]string{"photo_visibility": "public"}, nil, 200)
	setPhotoPolicy(t, e, "users", "users", false)
	check("capped", guest, 404)
	check("capped", olaf, 200)
	if got := pia.do("PUT", "/api/v1/me/profile", map[string]string{"photo_visibility": "public"}, nil); got != 400 {
		t.Fatalf("choice above the limit: %d", got)
	}
	var prof struct {
		Policy struct {
			Default string `json:"default"`
			Max     string `json:"max"`
			Fixed   bool   `json:"fixed"`
		} `json:"photo_policy"`
	}
	pia.must("GET", "/api/v1/me/profile", nil, &prof, 200)
	if prof.Policy.Max != "users" || prof.Policy.Default != "users" {
		t.Fatalf("policy %+v", prof.Policy)
	}
	// fixed: no choice, the own private choice no longer applies
	pia.must("PUT", "/api/v1/me/profile", map[string]string{"photo_visibility": "private"}, nil, 200)
	setPhotoPolicy(t, e, "users", "public", true)
	check("fixed", olaf, 200)
	if got := pia.do("PUT", "/api/v1/me/profile", map[string]string{"photo_visibility": "private"}, nil); got != 400 {
		t.Fatalf("choice although fixed: %d", got)
	}
	// settings: invalid idle durations are refused
	if got := admin.do("PUT", "/api/v1/admin/settings", map[string]any{"auth": map[string]any{"session_ttl": "168h", "api_key_max_ttl": "8760h",
		"ssh_token_ttl": "1h", "lockout_duration": "15m", "session_idle": "soon"}}, nil); got != 400 {
		t.Fatalf("invalid session_idle: %d", got)
	}
}

func setPhotoPolicy(t *testing.T, e *env, def, max string, fixed bool) {
	t.Helper()
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Auth.PhotoVisibility, c.Auth.PhotoVisibilityMax, c.Auth.PhotoVisibilityFixed = def, max, fixed
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
