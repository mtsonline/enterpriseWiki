package pluginmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSyncUpdateMarker(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 25, 3, 0, 0, 0, time.UTC)
	rep := func(tag, url string, available, security bool) *UpdateReport {
		return &UpdateReport{CheckedAt: at, Items: []UpdateInfo{
			{ID: "core", Tag: tag, URL: url, Available: available, Security: security},
			{ID: "some.plugin", Tag: "v9.9.9", Available: true},
		}}
	}
	read := func() (UpdateMarker, bool) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, MarkerFile))
		if os.IsNotExist(err) {
			return UpdateMarker{}, false
		}
		if err != nil {
			t.Fatal(err)
		}
		var m UpdateMarker
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: %v", b, err)
		}
		// the helper reads the version at the start of one line
		if !strings.HasPrefix(string(b), `{"version":"`) || strings.Count(string(b), "\n") != 1 {
			t.Fatalf("format for the helper: %q", b)
		}
		return m, true
	}
	if err := SyncUpdateMarker(dir, rep("v7.6.0", "https://github.com/mtsonline/enterpriseWiki/releases/tag/v7.6.0", true, true)); err != nil {
		t.Fatal(err)
	}
	if m, ok := read(); !ok || m.Version != "7.6.0" || !m.Security || m.URL == "" || !m.CheckedAt.Equal(at) {
		t.Fatalf("marker: %+v %v", m, ok)
	}
	// no newer release: removed; a pre-release or an odd tag never reaches the helper
	for _, r := range []*UpdateReport{
		rep("v7.6.0", "", false, false), rep("v8.0.0-rc1", "", true, false), rep(`v7.6.0"; touch /tmp/x; "`, "", true, false),
		rep("7.6", "", true, false), {Items: []UpdateInfo{{ID: "core", Tag: "v7.7.0", Available: true, Error: "rate limit"}}},
	} {
		if err := SyncUpdateMarker(dir, rep("v7.6.0", "", true, false)); err != nil {
			t.Fatal(err)
		}
		if err := SyncUpdateMarker(dir, r); err != nil {
			t.Fatal(err)
		}
		if m, ok := read(); ok {
			t.Fatalf("marker for %+v: %+v", r.Items[0], m)
		}
	}
	// an address that is not https or contains a quote is left out
	if err := SyncUpdateMarker(dir, rep("v7.6.1", `https://example.com/"x`, true, false)); err != nil {
		t.Fatal(err)
	}
	if m, _ := read(); m.Version != "7.6.1" || m.URL != "" {
		t.Fatalf("url: %+v", m)
	}
	// not a Docker installation: nothing happens
	if err := SyncUpdateMarker("", rep("v7.6.0", "", true, false)); err != nil {
		t.Fatal(err)
	}
	// no temporary files are left
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Fatalf("files: %v", es)
	}
}

func TestReadTimerState(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, TimerFile), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if st, err := ReadTimerState(dir, now); st != nil || err != nil {
		t.Fatalf("no file: %v %v", st, err)
	}
	const ok = `{"schema":1,"mode":"systemd","next_run":"2026-11-26T02:30:00Z","window_minutes":60,"last_run":"2026-11-25T02:41:00Z","last_result":"updated","last_version":"7.6.0","updated_at":"2026-11-25T02:45:00Z"}`
	write(ok)
	st, err := ReadTimerState(dir, now)
	if err != nil || !st.Fresh || st.Mode != "systemd" || st.WindowMin != 60 || st.LastResult != "updated" || st.LastVersion != "7.6.0" || st.NextRun == nil {
		t.Fatalf("valid: %+v %v", st, err)
	}
	// old: the helper has not reported for long, or the next run is long past
	for _, s := range []string{
		strings.Replace(ok, `"updated_at":"2026-11-25T02:45:00Z"`, `"updated_at":"2026-11-10T02:45:00Z"`, 1),
		strings.Replace(ok, `"next_run":"2026-11-26T02:30:00Z"`, `"next_run":"2026-11-24T02:30:00Z"`, 1),
		strings.Replace(ok, `"next_run":"2026-11-26T02:30:00Z"`, `"next_run":"2027-01-01T02:30:00Z"`, 1),
		strings.Replace(ok, `"next_run":"2026-11-26T02:30:00Z"`, `"next_run":null`, 1),
	} {
		write(s)
		if st, err := ReadTimerState(dir, now); err != nil || st.Fresh {
			t.Fatalf("not fresh: %s → %+v %v", s, st, err)
		}
	}
	// a run that is due right now (within its random delay) still counts
	write(strings.Replace(ok, `"next_run":"2026-11-26T02:30:00Z"`, `"next_run":"2026-11-25T11:30:00Z"`, 1))
	if st, err := ReadTimerState(dir, now); err != nil || !st.Fresh {
		t.Fatalf("due: %+v %v", st, err)
	}
	// untrusted: everything that does not pass the checks is refused
	for _, s := range []string{
		`not json`,
		strings.Replace(ok, `"schema":1`, `"schema":2`, 1),
		strings.Replace(ok, `"mode":"systemd"`, `"mode":"at"`, 1),
		strings.Replace(ok, `"last_result":"updated"`, `"last_result":"<script>"`, 1),
		strings.Replace(ok, `"last_version":"7.6.0"`, `"last_version":"7.6.0 and more"`, 1),
		strings.Replace(ok, `"window_minutes":60`, `"window_minutes":-5`, 1),
		strings.Replace(ok, `"updated_at":"2026-11-25T02:45:00Z"`, `"updated_at":"2030-01-01T00:00:00Z"`, 1),
		strings.Replace(ok, `"schema":1,`, `"schema":1,"command":"rm -rf /",`, 1),
		ok[:len(ok)-1] + `,"x":"` + strings.Repeat("a", 5000) + `"}`,
	} {
		write(s)
		if st, err := ReadTimerState(dir, now); err == nil {
			t.Fatalf("accepted %.80s: %+v", s, st)
		}
	}
}

// TestUpdateDockerScript runs scripts/update-docker.sh against a fake docker
// command: the whole run with the marker the wiki writes and the state the
// overview reads, an untrusted marker, a self-built image, a failed backup,
// a version not reached and the dry run.
func TestUpdateDockerScript(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the helper runs on Linux hosts")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	src, err := os.ReadFile("../../scripts/update-docker.sh")
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		code  int
		out   string
		calls string
	}
	setup := func(t *testing.T) (string, func(env ...string) result) {
		dir := t.TempDir()
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(os.MkdirAll(filepath.Join(dir, "scripts"), 0o755))
		must(os.MkdirAll(filepath.Join(dir, "data", "wiki"), 0o755))
		must(os.MkdirAll(filepath.Join(dir, "bin"), 0o755))
		must(os.WriteFile(filepath.Join(dir, "scripts", "update-docker.sh"), src, 0o755)) // #nosec G306 -- a script in a test directory
		must(os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  postgres:\n    environment:\n      POSTGRES_DB: ewiki\n      POSTGRES_USER: ewiki\n"), 0o600))
		must(os.WriteFile(filepath.Join(dir, ".env"), []byte(fmt.Sprintf("EWIKI_UID=%d\nEWIKI_GID=%d\n", os.Getuid(), os.Getgid())), 0o600))
		must(os.WriteFile(filepath.Join(dir, "data", "update-docker.conf"), []byte("mode=cron\ntime=03:30\n"), 0o600))
		fake := `#!/usr/bin/env bash
echo "$*" >> "$FAKE_CALLS"
case "$*" in
"compose config wiki") if [ -n "${FAKE_BUILT:-}" ]; then printf 'name: x\nservices:\n  wiki:\n    build:\n      context: .\n'; else printf 'name: x\nservices:\n  wiki:\n    image: ghcr.io/x\n'; fi ;;
"compose exec -T wiki printenv EWIKI_IMAGE_ORIGIN") echo published ;;
"compose exec -T postgres pg_dump"*) [ -z "${FAKE_DUMP_FAIL:-}" ] || exit 1; echo DUMP ;;
"compose exec -T wiki ewiki version") echo "ewiki v${FAKE_VERSION:-7.6.0} (abc)" ;;
*) ;;
esac
`
		must(os.WriteFile(filepath.Join(dir, "bin", "docker"), []byte(fake), 0o755)) // #nosec G306 -- a fake command in a test directory
		run := func(env ...string) result {
			calls := filepath.Join(dir, "calls")
			_ = os.Remove(calls)
			cmd := exec.Command("bash", filepath.Join(dir, "scripts", "update-docker.sh"))
			for _, e := range env {
				if strings.HasPrefix(e, "--") {
					cmd.Args = append(cmd.Args, e)
				}
			}
			cmd.Env = append(os.Environ(), "EWIKI_UPDATE_DOCKER="+filepath.Join(dir, "bin", "docker"), "FAKE_CALLS="+calls)
			for _, e := range env {
				if !strings.HasPrefix(e, "--") {
					cmd.Env = append(cmd.Env, e)
				}
			}
			out, _ := cmd.CombinedOutput()
			c, _ := os.ReadFile(calls)
			return result{cmd.ProcessState.ExitCode(), string(out), string(c)}
		}
		return dir, run
	}
	marker := func(t *testing.T, dir, version string) {
		t.Helper()
		if err := SyncUpdateMarker(filepath.Join(dir, "data", "wiki"), &UpdateReport{CheckedAt: time.Now().UTC(),
			Items: []UpdateInfo{{ID: "core", Tag: "v" + version, Available: true, Security: true}}}); err != nil {
			t.Fatal(err)
		}
	}
	state := func(t *testing.T, dir string) *TimerState {
		t.Helper()
		st, err := ReadTimerState(filepath.Join(dir, "data", "wiki"), time.Now())
		if err != nil || st == nil {
			t.Fatalf("state of the helper: %v %v", st, err)
		}
		return st
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	t.Run("no update", func(t *testing.T) {
		dir, run := setup(t)
		if r := run(); r.code != 0 || r.calls != "" {
			t.Fatalf("%+v", r)
		}
		if st := state(t, dir); st.LastResult != "no-update" || !st.Fresh || st.Mode != "cron" || st.LastRun == nil {
			t.Fatalf("%+v", st)
		}
	})
	t.Run("update", func(t *testing.T) {
		dir, run := setup(t)
		marker(t, dir, "7.6.0")
		r := run()
		if r.code != 0 || !strings.Contains(r.calls, "compose pull wiki\ncompose up -d\n") || !strings.Contains(r.out, "security update") {
			t.Fatalf("%+v", r)
		}
		// the backup comes before the new image
		if strings.Index(r.calls, "pg_dump") > strings.Index(r.calls, "compose pull") {
			t.Fatalf("order: %s", r.calls)
		}
		dumps, _ := filepath.Glob(filepath.Join(dir, "data", "backups", "before-update-*.dump"))
		if len(dumps) != 1 {
			t.Fatalf("backup: %v", dumps)
		}
		if exists(filepath.Join(dir, "data", "wiki", MarkerFile)) {
			t.Fatal("marker kept after the update")
		}
		if st := state(t, dir); st.LastResult != "updated" || st.LastVersion != "7.6.0" {
			t.Fatalf("%+v", st)
		}
		log, _ := os.ReadFile(filepath.Join(dir, "data", "update-docker.log"))
		if !strings.Contains(string(log), "updated to 7.6.0") {
			t.Fatalf("log: %s", log)
		}
	})
	t.Run("untrusted marker", func(t *testing.T) {
		dir, run := setup(t)
		for _, bad := range []string{
			`{"version":"7.6.0; touch /tmp/x","security":false}`,
			`{"version":"$(id)","security":false}`,
			`{"url":"x","version":"7.6.0"}`,
			"{\"version\":\"7.6.0\"}\n{\"version\":\"7.7.0\"}",
			`{"version":"7.6.0","pad":"` + strings.Repeat("a", 5000) + `"}`,
		} {
			if err := os.WriteFile(filepath.Join(dir, "data", "wiki", MarkerFile), []byte(bad+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if r := run(); r.code == 0 || r.calls != "" {
				t.Fatalf("accepted %.60q: %+v", bad, r)
			}
			if st := state(t, dir); st.LastResult != "invalid-marker" {
				t.Fatalf("%+v", st)
			}
		}
	})
	t.Run("self-built", func(t *testing.T) {
		dir, run := setup(t)
		marker(t, dir, "7.6.0")
		if r := run("FAKE_BUILT=1"); r.code != 0 || strings.Contains(r.calls, "pull") || strings.Contains(r.calls, "pg_dump") || !strings.Contains(r.out, "git pull") {
			t.Fatalf("%+v", r)
		}
		if st := state(t, dir); st.LastResult != "self-built" {
			t.Fatalf("%+v", st)
		}
	})
	t.Run("backup fails", func(t *testing.T) {
		dir, run := setup(t)
		marker(t, dir, "7.6.0")
		if r := run("FAKE_DUMP_FAIL=1"); r.code == 0 || strings.Contains(r.calls, "pull") {
			t.Fatalf("%+v", r)
		}
		if st := state(t, dir); st.LastResult != "backup-failed" {
			t.Fatalf("%+v", st)
		}
		if dumps, _ := filepath.Glob(filepath.Join(dir, "data", "backups", "*")); len(dumps) != 0 {
			t.Fatalf("partial backup kept: %v", dumps)
		}
	})
	t.Run("version not reached", func(t *testing.T) {
		dir, run := setup(t)
		marker(t, dir, "7.6.0")
		if r := run("FAKE_VERSION=7.5.0"); r.code == 0 || !strings.Contains(r.out, "EWIKI_IMAGE_TAG") {
			t.Fatalf("%+v", r)
		}
		if st := state(t, dir); st.LastResult != "not-reached" || !exists(filepath.Join(dir, "data", "wiki", MarkerFile)) {
			t.Fatalf("%+v", st)
		}
	})
	t.Run("dry run", func(t *testing.T) {
		dir, run := setup(t)
		marker(t, dir, "7.6.0")
		r := run("--dry-run")
		if r.code != 0 || strings.Contains(r.calls, "pull") || strings.Contains(r.calls, "pg_dump") || !strings.Contains(r.out, "would run: docker compose pull wiki") {
			t.Fatalf("%+v", r)
		}
		if exists(filepath.Join(dir, "data", "wiki", TimerFile)) || exists(filepath.Join(dir, "data", "update-docker.log")) {
			t.Fatal("the dry run changed files")
		}
	})
	t.Run("systemd timer", func(t *testing.T) {
		if os.Getuid() != 0 {
			t.Skip("units are written as root only (without root the helper uses the crontab of the user)")
		}
		dir, run := setup(t)
		units := filepath.Join(dir, "units")
		if err := os.MkdirAll(units, 0o755); err != nil {
			t.Fatal(err)
		}
		if r := run("--enable-timer", "--cron-unused", "EWIKI_UPDATE_UNITS="+units); r.code == 0 {
			t.Fatalf("a wrong time accepted: %+v", r)
		}
		cmd := exec.Command("bash", filepath.Join(dir, "scripts", "update-docker.sh"), "--enable-timer", "04:15")
		cmd.Env = append(os.Environ(), "EWIKI_UPDATE_UNITS="+units)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		timer, _ := os.ReadFile(filepath.Join(units, "enterprisewiki-docker-update.timer"))
		service, _ := os.ReadFile(filepath.Join(units, "enterprisewiki-docker-update.service"))
		if !strings.Contains(string(timer), "OnCalendar=*-*-* 04:15") || !strings.Contains(string(timer), "RandomizedDelaySec=1h") ||
			!strings.Contains(string(service), "ExecStart="+filepath.Join(dir, "scripts", "update-docker.sh")) {
			t.Fatalf("units:\n%s\n%s", timer, service)
		}
		if st := state(t, dir); st.Mode != "systemd" || st.WindowMin != 60 || !st.Fresh {
			t.Fatalf("%+v", st)
		}
		cmd = exec.Command("bash", filepath.Join(dir, "scripts", "update-docker.sh"), "--disable-timer")
		cmd.Env = append(os.Environ(), "EWIKI_UPDATE_UNITS="+units)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		if exists(filepath.Join(units, "enterprisewiki-docker-update.timer")) || exists(filepath.Join(dir, "data", "wiki", TimerFile)) {
			t.Fatal("timer not removed")
		}
	})
}

// TestDockerSetupDocker runs ./docker-setup.sh (without a terminal, so it
// asks nothing): it refuses a user who cannot use Docker, with the ways to
// fix it, and prepares the installation for one who can.
func TestDockerSetupDocker(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getuid() == 0 {
		t.Skip("docker-setup.sh refuses root; runs as a normal user (CI)")
	}
	dir := t.TempDir()
	for _, f := range []string{"docker-setup.sh", ".env.example", "docker-compose.yml"} {
		b, err := os.ReadFile(filepath.Join("..", "..", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), b, 0o700); err != nil { // #nosec G306 -- copies in a test directory
			t.Fatal(err)
		}
	}
	// docker info of a user without access fails, of one with access works
	for name, code := range map[string]int{"docker-denied": 1, "docker-ok": 0} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(fmt.Sprintf("#!/bin/sh\n[ \"$1\" = info ] && exit %d\nexit 0\n", code)), 0o700); err != nil { // #nosec G306 -- a fake command in a test directory
			t.Fatal(err)
		}
	}
	run := func(docker string) (int, string) {
		cmd := exec.Command("bash", filepath.Join(dir, "docker-setup.sh"))
		cmd.Env = append(os.Environ(), "EWIKI_SETUP_DOCKER="+docker)
		cmd.Stdin = strings.NewReader("")
		out, _ := cmd.CombinedOutput()
		return cmd.ProcessState.ExitCode(), string(out)
	}
	if code, out := run("no-such-docker-ewiki"); code == 0 || !strings.Contains(out, "not installed") {
		t.Fatalf("no docker: %d %s", code, out)
	}
	code, out := run(filepath.Join(dir, "docker-denied"))
	if code == 0 || !strings.Contains(out, "sudo usermod -aG docker") || !strings.Contains(out, "log out and in again") || !strings.Contains(out, "rootless") {
		t.Fatalf("no access: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
		t.Fatal(".env created although the check failed")
	}
	// access: .env with the own ids, the folders
	code, out = run(filepath.Join(dir, "docker-ok"))
	if code != 0 || !strings.Contains(out, "ready") {
		t.Fatalf("access: %d %s", code, out)
	}
	env, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if !strings.Contains(string(env), fmt.Sprintf("EWIKI_UID=%d\n", os.Getuid())) || !strings.Contains(string(env), "EWIKI_AUTO_UPDATE=\n") {
		t.Fatalf(".env: %s", env)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "wiki")); err != nil {
		t.Fatal(err)
	}
}
