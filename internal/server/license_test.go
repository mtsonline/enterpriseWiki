package server_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/license"
)

func TestLicenseKey(t *testing.T) {
	e := setup(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	old := license.PublicKeys
	license.PublicKeys = append([]string{base64.StdEncoding.EncodeToString(pub)}, old...)
	t.Cleanup(func() { license.PublicKeys = old })
	admin := e.client()
	admin.login("admin", adminPW)
	var st struct {
		Installed   bool   `json:"installed"`
		Valid       bool   `json:"valid"`
		Level       string `json:"level"`
		ActiveUsers int    `json:"active_users"`
		OverUsers   bool   `json:"over_users"`
		License     *struct {
			Licensee string `json:"licensee"`
		} `json:"license"`
	}
	admin.must("GET", "/api/v1/admin/license", nil, &st, 200)
	if st.Installed || st.Level != "info" || st.ActiveUsers != 1 {
		t.Fatalf("no license: %+v", st)
	}
	// the seal (footer, administration) only with a valid license; guests see
	// that and nothing else of the license
	sealed := func(want bool) {
		t.Helper()
		res, body := e.client().raw("/api/v1/site", nil)
		if res.StatusCode != 200 || strings.Contains(body, `"licensed":true`) != want || strings.Contains(body, "Test AG") || strings.Contains(body, "L-T") {
			t.Fatalf("site for guests, licensed %v: %d %s", want, res.StatusCode, body)
		}
	}
	sealed(false)
	if code := admin.do("PUT", "/api/v1/admin/license", map[string]string{"key": "EWL1.bogus.key"}, nil); code != 400 {
		t.Fatalf("invalid key accepted: %d", code)
	}
	key, _ := license.Sign(license.License{ID: "L-T", Licensee: "Test AG", Edition: "business", Users: 1, Issued: "2026-01-01", Expires: "2099-12-31"}, priv)
	admin.must("PUT", "/api/v1/admin/license", map[string]string{"key": key}, &st, 200)
	if !st.Valid || st.License == nil || st.License.Licensee != "Test AG" || st.Level != "ok" {
		t.Fatalf("installed: %+v", st)
	}
	sealed(true)
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "second", "password": "Passw0rd!Passw0rd", "display_name": "S"}, nil, 201)
	admin.must("GET", "/api/v1/admin/license", nil, &st, 200)
	if !st.OverUsers || st.Level != "warning" {
		t.Fatalf("over users: %+v", st)
	}
	sealed(true) // more users than licensed: still a valid license
	expired, _ := license.Sign(license.License{ID: "L-T", Licensee: "Test AG", Edition: "business", Issued: "2020-01-01", Expires: "2021-12-31"}, priv)
	admin.must("PUT", "/api/v1/admin/license", map[string]string{"key": expired}, nil, 200)
	sealed(false)
	admin.must("PUT", "/api/v1/admin/license", map[string]string{"key": key}, nil, 200)
	// stored in config.yaml
	if e.app.Config.Get().License.Key != key {
		t.Fatal("key not stored")
	}
	user := e.client()
	user.login("second", "Passw0rd!Passw0rd")
	if code := user.do("GET", "/api/v1/admin/license", nil, nil); code != 403 {
		t.Fatalf("license for non-admins: %d", code)
	}
	admin.must("DELETE", "/api/v1/admin/license", nil, nil, 204)
	admin.must("GET", "/api/v1/admin/license", nil, &st, 200)
	if st.Installed {
		t.Fatal("not removed")
	}
	sealed(false)
}
