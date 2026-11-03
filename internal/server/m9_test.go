package server_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/license"
	"github.com/mtsonline/enterprisewiki/internal/pgversion"
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
	if code := admin.do("PUT", "/api/v1/admin/license", map[string]string{"key": "EWL1.bogus.key"}, nil); code != 400 {
		t.Fatalf("invalid key accepted: %d", code)
	}
	key, _ := license.Sign(license.License{ID: "L-T", Licensee: "Test AG", Edition: "business", Users: 1, Issued: "2026-01-01", Expires: "2099-12-31"}, priv)
	admin.must("PUT", "/api/v1/admin/license", map[string]string{"key": key}, &st, 200)
	if !st.Valid || st.License == nil || st.License.Licensee != "Test AG" || st.Level != "ok" {
		t.Fatalf("installed: %+v", st)
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "second", "password": "Passw0rd!Passw0rd", "display_name": "S"}, nil, 201)
	admin.must("GET", "/api/v1/admin/license", nil, &st, 200)
	if !st.OverUsers || st.Level != "warning" {
		t.Fatalf("over users: %+v", st)
	}
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
}

func TestPostgresVersionNotices(t *testing.T) {
	e := setup(t)
	c := &capture{}
	e.app.Mail.SetTransport(c)
	admin := e.client()
	admin.login("admin", adminPW)
	var st struct {
		Major       int    `json:"major"`
		Recommended int    `json:"recommended"`
		Level       string `json:"level"`
		EOL         string `json:"eol"`
	}
	admin.must("GET", "/api/v1/admin/database", nil, &st, 200)
	if st.Major < 14 || st.Recommended != pgversion.RecommendedMajor || st.EOL == "" {
		t.Fatalf("status: %+v", st)
	}
	if st.Major >= st.Recommended {
		t.Skipf("test database is PostgreSQL %d (no notice expected)", st.Major)
	}
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE users SET email='root@example.com' WHERE username='admin'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // the second run sends nothing again
		if err := e.app.Workers.RunOnce(context.Background(), "pgversion"); err != nil {
			t.Fatal(err)
		}
	}
	e.flushMail(t)
	n := 0
	c.mu.Lock()
	for _, m := range c.msgs {
		if strings.Contains(decodeQP(string(m.Data)), fmt.Sprintf("PostgreSQL %d", pgversion.RecommendedMajor)) {
			n++
		}
	}
	c.mu.Unlock()
	if n != 1 {
		t.Fatalf("notice e-mails: %d", n)
	}
}
