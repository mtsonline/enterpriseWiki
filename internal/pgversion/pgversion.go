// Package pgversion checks the PostgreSQL version of the database: whether
// a newer major version is recommended and when community support for the
// running version ends. Administrators see it under Administration, in the
// server log and by e-mail (once when a newer version is recommended, and
// 180 and 30 days before and after the end of support).
package pgversion

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RecommendedMajor is the PostgreSQL version Enterprise Wiki is tested with
// and recommends. Renovate keeps it in step with docker-compose.yml.
const RecommendedMajor = 18 // renovate: postgres

// EOL returns the end of community support of a major version: five years
// after the release, on the minor release day of November (the second
// Thursday), see https://www.postgresql.org/support/versioning/.
func EOL(major int) time.Time {
	year := major + 2012 // 10 was released 2017, every year one major version
	d := time.Date(year, time.November, 1, 0, 0, 0, 0, time.UTC)
	for d.Weekday() != time.Thursday {
		d = d.AddDate(0, 0, 1)
	}
	return d.AddDate(0, 0, 7)
}

// Status of the database version.
type Status struct {
	Version     string `json:"version"` // e.g. 16.4
	Major       int    `json:"major"`
	Recommended int    `json:"recommended"`
	EOL         string `json:"eol"` // YYYY-MM-DD
	DaysLeft    int    `json:"days_left"`
	// Level: ok | info (a newer version is recommended) | warning (support
	// ends within 180 days) | error (support has ended)
	Level  string `json:"level"`
	Docker bool   `json:"docker"` // running in the Docker image (upgrade with ./docker-postgres-upgrade.sh)
}

// Evaluate builds the status of a server version (SHOW server_version_num).
func Evaluate(versionNum int, version string, now time.Time) Status {
	major := versionNum / 10000
	eol := EOL(major)
	st := Status{Version: version, Major: major, Recommended: RecommendedMajor, EOL: eol.Format("2006-01-02"),
		DaysLeft: int(eol.Sub(now).Hours() / 24), Level: "ok", Docker: InDocker()}
	switch {
	case !now.Before(eol):
		st.Level = "error"
	case st.DaysLeft <= 180:
		st.Level = "warning"
	case major < RecommendedMajor:
		st.Level = "info"
	}
	return st
}

// Check asks the database for its version.
func Check(ctx context.Context, pool *pgxpool.Pool, now time.Time) (Status, error) {
	var num, ver string
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num'), current_setting('server_version')`).Scan(&num, &ver); err != nil {
		return Status{}, err
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return Status{}, fmt.Errorf("server_version_num %q: %w", num, err)
	}
	if i := strings.IndexByte(ver, ' '); i > 0 {
		ver = ver[:i]
	}
	return Evaluate(n, ver, now), nil
}

// InDocker reports whether the wiki runs in its Docker image.
func InDocker() bool {
	if os.Getenv("EWIKI_DOCKER") == "1" {
		return true
	}
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

// Notices returns the keys of the one-time e-mails due for a status
// (the caller sends each key only once).
func Notices(st Status) []string {
	var out []string
	if st.Major < st.Recommended {
		out = append(out, fmt.Sprintf("pg.recommended.%d.%d", st.Major, st.Recommended))
	}
	for _, days := range []int{180, 30, 0} {
		if st.DaysLeft <= days {
			out = append(out, fmt.Sprintf("pg.eol.%d.%d", st.Major, days))
		}
	}
	return out
}
