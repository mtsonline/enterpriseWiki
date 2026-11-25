package server_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// overviewStatus is the part of GET admin/status that the overview of the
// administration adds.
type overviewStatus struct {
	Updates *struct {
		Core *struct {
			Latest        string `json:"latest"`
			Available     bool   `json:"available"`
			Security      bool   `json:"security"`
			SecurityNotes string `json:"security_notes"`
		} `json:"core"`
		Security bool `json:"security"`
		Install  struct {
			Kind     string `json:"kind"`
			Image    string `json:"image"`
			Origin   string `json:"origin"`
			Dir      string `json:"dir"`
			Commands []struct {
				Kind  string   `json:"kind"`
				Lines []string `json:"lines"`
			} `json:"commands"`
		} `json:"install"`
	} `json:"updates"`
	Plugins *struct {
		Total     int `json:"total"`
		Enabled   int `json:"enabled"`
		Active    int `json:"active"`
		InProcess int `json:"in_process"`
	} `json:"plugins"`
	Users *struct {
		Active    int `json:"active"`
		Disabled  int `json:"disabled"`
		Invited   int `json:"invited"`
		Directory []struct {
			Provider string `json:"provider"`
			Status   string `json:"status"`
		} `json:"directory"`
	} `json:"users"`
	Security *struct {
		Providers []struct {
			ID       string `json:"id"`
			Required int    `json:"required"`
		} `json:"providers"`
	} `json:"security"`
	License *struct {
		Level string `json:"level"`
		Valid bool   `json:"valid"`
	} `json:"license"`
}

func (o overviewStatus) commandKinds() string {
	var k []string
	for _, c := range o.Updates.Install.Commands {
		k = append(k, c.Kind)
	}
	return strings.Join(k, ",")
}

// TestAdminOverview covers the overview of the administration: the parts
// of GET admin/status, the kind of installation with the commands to
// update, security updates from the release notes, and that an
// administrator of a further site does not see the updates of the instance.
func TestAdminOverview(t *testing.T) {
	t.Setenv("EWIKI_DOCKER", "")
	t.Setenv("EWIKI_INSTALL", "")
	t.Setenv("EWIKI_IMAGE_ORIGIN", "")
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)

	// users: active, deactivated, invited
	var u struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "olga", "password": "Olga-Passw0rd!x"}, &u, 201)
	var before overviewStatus
	admin.must("GET", "/api/v1/admin/status", nil, &before, 200)
	if before.Updates == nil || before.Plugins == nil || before.Users == nil || before.Security == nil || before.License == nil {
		t.Fatalf("overview incomplete: %+v", before)
	}
	if before.Users.Active < 2 || before.Users.Directory == nil {
		t.Fatalf("users: %+v", before.Users)
	}
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), map[string]any{"disabled": true}, nil, 200)
	var after overviewStatus
	admin.must("GET", "/api/v1/admin/status", nil, &after, 200)
	if after.Users.Active != before.Users.Active-1 || after.Users.Disabled != before.Users.Disabled+1 {
		t.Fatalf("deactivating: %+v → %+v", before.Users, after.Users)
	}
	// plugins: all run in the wiki process
	if p := after.Plugins; p.Total == 0 || p.Enabled > p.Total || p.Active > p.Enabled || p.InProcess != p.Total {
		t.Fatalf("plugins: %+v", p)
	}
	// security: the sign-in methods with their number of factors
	if len(after.Security.Providers) == 0 || after.Security.Providers[0].Required < 1 {
		t.Fatalf("security: %+v", after.Security)
	}
	if after.License.Level != "info" || after.License.Valid {
		t.Fatalf("license without key: %+v", after.License)
	}

	// the kind of installation: unknown → every way, each with its label
	if after.Updates.Install.Kind != "" || after.commandKinds() != "docker-published,docker-self-built,systemd" {
		t.Fatalf("unknown installation: %+v", after.Updates.Install)
	}
	if after.Updates.Core != nil {
		t.Fatal("core update without a check")
	}
	kind := func(env map[string]string) overviewStatus {
		t.Helper()
		for _, k := range []string{"EWIKI_DOCKER", "EWIKI_IMAGE", "EWIKI_IMAGE_ORIGIN", "EWIKI_INSTALL"} {
			t.Setenv(k, env[k])
		}
		var st overviewStatus
		admin.must("GET", "/api/v1/admin/status", nil, &st, 200)
		return st
	}
	st := kind(map[string]string{"EWIKI_DOCKER": "1", "EWIKI_IMAGE": "lite", "EWIKI_IMAGE_ORIGIN": "published"})
	if in := st.Updates.Install; in.Kind != "docker" || in.Image != "lite" || in.Origin != "published" || st.commandKinds() != "docker-published" ||
		strings.Join(in.Commands[0].Lines, ";") != "docker compose pull;docker compose up -d" {
		t.Fatalf("published image: %+v", in)
	}
	st = kind(map[string]string{"EWIKI_DOCKER": "1", "EWIKI_IMAGE": "full", "EWIKI_IMAGE_ORIGIN": "self-built"})
	if in := st.Updates.Install; in.Origin != "self-built" || st.commandKinds() != "docker-self-built" ||
		in.Commands[0].Lines[0] != "git pull && docker compose -f docker-compose.build.yml up -d --build" {
		t.Fatalf("self-built image: %+v", in)
	}
	// an image from before the marker: both Docker ways
	st = kind(map[string]string{"EWIKI_DOCKER": "1", "EWIKI_IMAGE": "full", "EWIKI_IMAGE_ORIGIN": "anything"})
	if st.Updates.Install.Origin != "" || st.commandKinds() != "docker-published,docker-self-built" {
		t.Fatalf("image without origin: %+v", st.Updates.Install)
	}
	st = kind(map[string]string{"EWIKI_INSTALL": "systemd"})
	if in := st.Updates.Install; in.Kind != "systemd" || st.commandKinds() != "systemd" || in.Dir == "" ||
		!strings.HasPrefix(in.Commands[0].Lines[0], "sudo ") || !strings.HasSuffix(in.Commands[0].Lines[0], "/scripts/update.sh") {
		t.Fatalf("systemd: %+v", in)
	}
	kind(nil)

	// security updates: the section "## Security" of every release after
	// the installed version, newest first
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/mtsonline/enterpriseWiki/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v99.2.0","html_url":"https://github.com/mtsonline/enterpriseWiki/releases/tag/v99.2.0","body":"## Features\n- more"}`))
		case "/repos/mtsonline/enterpriseWiki/releases":
			_, _ = w.Write([]byte(`[
				{"tag_name":"v99.3.0-rc1","prerelease":true,"body":"## Security\n- not released"},
				{"tag_name":"v99.2.0","body":"## Features\n- more"},
				{"tag_name":"v99.1.0","body":"## Sicherheit\n- XSS behoben\n## Fixes\n- x"},
				{"tag_name":"v0.0.1","body":"## Security\n- old"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Plugins.Updates.APIURL = gh.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	admin.must("GET", "/api/v1/admin/updates?force=1", nil, nil, 200)
	admin.must("GET", "/api/v1/admin/status", nil, &st, 200)
	if c := st.Updates.Core; c == nil || !c.Available || c.Latest != "99.2.0" || !c.Security || c.SecurityNotes != "v99.1.0:\n- XSS behoben" || !st.Updates.Security {
		t.Fatalf("security update: %+v", st.Updates.Core)
	}

	// an administrator of a further site sees the parts of the site, not
	// the updates of the instance
	admin.must("POST", "/api/v1/instance/sites", map[string]any{"name": "ov", "hostnames": []string{"ov.example.test"}}, nil, 201)
	admin.host = "ov.example.test"
	admin.login("admin", adminPW)
	var roles []struct {
		ID      int64 `json:"id"`
		IsAdmin bool  `json:"is_admin"`
	}
	admin.must("GET", "/api/v1/roles", nil, &roles, 200)
	var adminRole int64
	for _, r := range roles {
		if r.IsAdmin {
			adminRole = r.ID
		}
	}
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "ovadmin", "password": "Ov-Admin-Passw0rd!"}, &u, 201)
	admin.must("PUT", fmt.Sprintf("/api/v1/admin/users/%d", u.ID), map[string]any{"roles": []int64{adminRole}}, nil, 200)
	sa := e.client()
	sa.host = "ov.example.test"
	sa.login("ovadmin", "Ov-Admin-Passw0rd!")
	var site overviewStatus
	sa.must("GET", "/api/v1/admin/status", nil, &site, 200)
	if site.Updates != nil {
		t.Fatalf("site administrator sees the updates of the instance: %+v", site.Updates)
	}
	if site.Users == nil || site.Plugins == nil {
		t.Fatalf("site administrator: %+v", site)
	}
	// the instance administrator sees them on that site
	admin.must("GET", "/api/v1/admin/status", nil, &site, 200)
	if site.Updates == nil {
		t.Fatal("instance administrator on a further site: no updates")
	}
}

// TestUpdateHelperFiles covers the files the wiki shares with the update
// helper on the host (Docker): the marker of a new published release,
// written by the update check and removed when there is none, and the
// state of the helper in the overview, refused when it does not pass the
// checks. The daily update check is a background job.
func TestUpdateHelperFiles(t *testing.T) {
	data := t.TempDir()
	t.Setenv("EWIKI_DOCKER", "1")
	t.Setenv("EWIKI_IMAGE", "lite")
	t.Setenv("EWIKI_IMAGE_ORIGIN", "published")
	t.Setenv("EWIKI_DATA", data)
	t.Setenv("EWIKI_INSTALL", "")
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)

	latest := "v99.1.0"
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/mtsonline/enterpriseWiki/releases/latest":
			_, _ = fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://github.com/mtsonline/enterpriseWiki/releases/tag/%s","body":"## Security\n- fix"}`, latest, latest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Plugins.Updates.APIURL = gh.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(data, "update-available")
	admin.must("GET", "/api/v1/admin/updates?force=1", nil, nil, 200)
	b, err := os.ReadFile(marker)
	if err != nil || !strings.HasPrefix(string(b), `{"version":"99.1.0"`) || !strings.Contains(string(b), `"security":true`) {
		t.Fatalf("marker: %s %v", b, err)
	}
	// a pre-release is not for the helper: the marker goes
	latest = "v99.2.0-rc1"
	admin.must("GET", "/api/v1/admin/updates?force=1", nil, nil, 200)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker kept: %v", err)
	}

	type timerStatus struct {
		Updates struct {
			Timer *struct {
				Mode       string `json:"mode"`
				Fresh      bool   `json:"fresh"`
				LastResult string `json:"last_result"`
			} `json:"timer"`
			TimerError string `json:"timer_error"`
		} `json:"updates"`
	}
	var st timerStatus
	admin.must("GET", "/api/v1/admin/status", nil, &st, 200)
	if st.Updates.Timer != nil || st.Updates.TimerError != "" {
		t.Fatalf("no helper: %+v", st.Updates)
	}
	now := time.Now().UTC()
	state := fmt.Sprintf(`{"schema":1,"mode":"cron","next_run":%q,"window_minutes":0,"last_run":%q,"last_result":"no-update","last_version":"","updated_at":%q}`,
		now.Add(10*time.Hour).Format(time.RFC3339), now.Add(-time.Hour).Format(time.RFC3339), now.Add(-time.Hour).Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(data, "update-timer.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	admin.must("GET", "/api/v1/admin/status", nil, &st, 200)
	if tm := st.Updates.Timer; tm == nil || !tm.Fresh || tm.Mode != "cron" || tm.LastResult != "no-update" || st.Updates.TimerError != "" {
		t.Fatalf("helper: %+v", st.Updates)
	}
	if err := os.WriteFile(filepath.Join(data, "update-timer.json"), []byte(strings.Replace(state, `"no-update"`, `"<img src=x>"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	st = timerStatus{}
	admin.must("GET", "/api/v1/admin/status", nil, &st, 200)
	if st.Updates.Timer != nil || st.Updates.TimerError != "unreadable" {
		t.Fatalf("untrusted state shown: %+v", st.Updates)
	}

	// the daily check is a background job
	var jobs []string
	admin.must("GET", "/api/v1/admin/jobs", nil, &jobs, 200)
	if !slices.Contains(jobs, "updates.check") {
		t.Fatalf("jobs: %v", jobs)
	}
}
