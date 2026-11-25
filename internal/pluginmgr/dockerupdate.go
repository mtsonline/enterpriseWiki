package pluginmgr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The Docker update helper (scripts/update-docker.sh) runs on the host, not
// in the wiki: the wiki never gets access to Docker. They talk through two
// files in the data directory (./data/wiki on the host, /data in the
// container):
//
//   - update-available, written by the wiki when the update check found a
//     newer published release of the core (removed otherwise). The helper
//     treats it as untrusted: it only takes a version of a strict pattern.
//   - update-timer.json, written by the helper (next run, last run and its
//     result). The wiki treats it as untrusted too and only shows values
//     that pass ReadTimerState.

// MarkerFile and TimerFile are the names of the two files.
const (
	MarkerFile = "update-available"
	TimerFile  = "update-timer.json"
)

// releaseVersion: a published release, no pre-release or build suffix. The
// helper checks the same pattern (scripts/update-docker.sh).
var releaseVersion = regexp.MustCompile(`^[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}$`)

// UpdateMarker is the content of update-available.
type UpdateMarker struct {
	Version   string    `json:"version"` // e.g. 7.6.0
	URL       string    `json:"url,omitempty"`
	Security  bool      `json:"security"`
	CheckedAt time.Time `json:"checked_at"`
}

// SyncUpdateMarker writes update-available into dir when rep says that a
// newer published release of the core exists, and removes it otherwise.
// dir "" does nothing (not a Docker installation). The file is replaced
// atomically, so the helper never reads half of it.
func SyncUpdateMarker(dir string, rep *UpdateReport) error {
	if dir == "" || rep == nil {
		return nil
	}
	path := filepath.Join(dir, MarkerFile)
	var mk *UpdateMarker
	for _, it := range rep.Items {
		if it.ID != "core" || !it.Available || it.Error != "" {
			continue
		}
		v := strings.TrimPrefix(it.Tag, "v")
		if !releaseVersion.MatchString(v) {
			continue // pre-releases and odd tags never reach the helper
		}
		url := it.URL
		if !strings.HasPrefix(url, "https://") || len(url) > 300 || strings.ContainsAny(url, "\"\\\n\r") {
			url = ""
		}
		mk = &UpdateMarker{Version: v, URL: url, Security: it.Security, CheckedAt: rep.CheckedAt}
	}
	if mk == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	b, err := json.Marshal(mk)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update-available-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// TimerState is what the helper reports in update-timer.json, checked.
type TimerState struct {
	Mode        string     `json:"mode"`                   // systemd | cron
	NextRun     *time.Time `json:"next_run"`               // nil: the timer is off
	WindowMin   int        `json:"window_minutes"`         // random delay after next_run (systemd)
	LastRun     *time.Time `json:"last_run"`               // nil: never ran
	LastResult  string     `json:"last_result,omitempty"`  // see timerResults
	LastVersion string     `json:"last_version,omitempty"` // version the last run installed or tried
	UpdatedAt   time.Time  `json:"updated_at"`
	// Fresh: the helper reported recently and its next run lies ahead (or
	// is just due); only then the overview names the time.
	Fresh bool `json:"fresh"`
}

// timerResults are the results the helper may report.
var timerResults = map[string]bool{
	"": true, "no-update": true, "updated": true, "failed": true, "backup-failed": true,
	"invalid-marker": true, "self-built": true, "not-reached": true, "busy": true,
}

// maxTimerFile limits what the wiki reads of update-timer.json.
const maxTimerFile = 4096

// ReadTimerState reads update-timer.json from dir. It returns nil without
// an error when there is no file. A file that does not pass the checks is
// an error: the overview then shows the commands and says that the state
// of the helper could not be read.
func ReadTimerState(dir string, now time.Time) (*TimerState, error) {
	if dir == "" {
		return nil, nil
	}
	f, err := os.Open(filepath.Join(dir, TimerFile)) // #nosec G304 -- fixed name in the data directory
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxTimerFile+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxTimerFile {
		return nil, fmt.Errorf("%s is larger than %d bytes", TimerFile, maxTimerFile)
	}
	var in struct {
		Schema      int        `json:"schema"`
		Mode        string     `json:"mode"`
		NextRun     *time.Time `json:"next_run"`
		WindowMin   int        `json:"window_minutes"`
		LastRun     *time.Time `json:"last_run"`
		LastResult  string     `json:"last_result"`
		LastVersion string     `json:"last_version"`
		UpdatedAt   time.Time  `json:"updated_at"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("%s: %w", TimerFile, err)
	}
	switch {
	case in.Schema != 1:
		return nil, fmt.Errorf("%s: unknown schema %d", TimerFile, in.Schema)
	case in.Mode != "systemd" && in.Mode != "cron":
		return nil, fmt.Errorf("%s: unknown mode", TimerFile)
	case in.WindowMin < 0 || in.WindowMin > 24*60:
		return nil, fmt.Errorf("%s: window out of range", TimerFile)
	case !timerResults[in.LastResult]:
		return nil, fmt.Errorf("%s: unknown result", TimerFile)
	case in.LastVersion != "" && !releaseVersion.MatchString(in.LastVersion):
		return nil, fmt.Errorf("%s: invalid version", TimerFile)
	case in.UpdatedAt.IsZero() || in.UpdatedAt.After(now.Add(time.Hour)):
		return nil, fmt.Errorf("%s: invalid time of writing", TimerFile)
	}
	st := &TimerState{Mode: in.Mode, NextRun: in.NextRun, WindowMin: in.WindowMin, LastRun: in.LastRun,
		LastResult: in.LastResult, LastVersion: in.LastVersion, UpdatedAt: in.UpdatedAt}
	// fresh: written within the last 8 days (the timer runs daily) and the
	// next run at most 8 days ahead and not long past (a run is due or
	// still within its random delay)
	if st.NextRun != nil {
		due := st.NextRun.Add(time.Duration(st.WindowMin)*time.Minute + 2*time.Hour)
		st.Fresh = now.Sub(st.UpdatedAt) <= 8*24*time.Hour && now.Before(due) && st.NextRun.Before(now.Add(8*24*time.Hour))
	}
	return st, nil
}
