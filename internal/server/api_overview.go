package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
	"github.com/mtsonline/enterprisewiki/internal/version"
	"github.com/mtsonline/enterprisewiki/internal/worker"
)

// The overview of the administration (GET /api/v1/admin/status, page
// /admin/overview): beyond the state of the wiki, each part only for those
// who may open the matching administration page.

// installInfo says how this instance was installed, as far as a reliable
// signal tells it: EWIKI_DOCKER/EWIKI_IMAGE/EWIKI_IMAGE_ORIGIN set by the
// Dockerfile (origin "published" only by the publishing workflow),
// EWIKI_INSTALL=systemd set by scripts/install.sh. Kind "" means unknown.
type installInfo struct {
	Kind   string `json:"kind"`             // docker | systemd | ""
	Image  string `json:"image,omitempty"`  // lite | full (docker)
	Origin string `json:"origin,omitempty"` // published | self-built (docker; "" = unknown)
	Dir    string `json:"dir,omitempty"`    // directory of the installation (systemd)
	// Commands that install an available update of the core; several when
	// the kind of installation is unknown. The wiki never runs them: that
	// would need access to Docker or root on the host.
	Commands []updateCommand `json:"commands"`
}

// updateCommand is one way to update; Kind names the installation it is for
// (docker-published, docker-self-built, systemd).
type updateCommand struct {
	Kind  string   `json:"kind"`
	Lines []string `json:"lines"`
}

// defaultInstallDir is the default directory of scripts/install.sh.
const defaultInstallDir = "/opt/enterprisewiki"

func detectInstall() installInfo {
	cmdPublished := updateCommand{"docker-published", []string{"docker compose pull", "docker compose up -d"}}
	cmdBuilt := updateCommand{"docker-self-built", []string{"git pull && docker compose -f docker-compose.build.yml up -d --build"}}
	systemd := func(dir string) updateCommand {
		return updateCommand{"systemd", []string{"sudo " + filepath.Join(dir, "scripts", "update.sh")}}
	}
	if os.Getenv("EWIKI_DOCKER") == "1" {
		in := installInfo{Kind: "docker", Image: os.Getenv("EWIKI_IMAGE")}
		switch os.Getenv("EWIKI_IMAGE_ORIGIN") {
		case "published":
			in.Origin, in.Commands = "published", []updateCommand{cmdPublished}
		case "self-built":
			in.Origin, in.Commands = "self-built", []updateCommand{cmdBuilt}
		default: // an image from before the marker
			in.Commands = []updateCommand{cmdPublished, cmdBuilt}
		}
		return in
	}
	if os.Getenv("EWIKI_INSTALL") == "systemd" {
		dir := defaultInstallDir
		// scripts/install.sh puts the binary into <dir>/bin
		if exe, err := os.Executable(); err == nil && filepath.Base(filepath.Dir(exe)) == "bin" {
			dir = filepath.Dir(filepath.Dir(exe))
		}
		return installInfo{Kind: "systemd", Dir: dir, Commands: []updateCommand{systemd(dir)}}
	}
	return installInfo{Commands: []updateCommand{cmdPublished, cmdBuilt, systemd(defaultInstallDir)}}
}

// dockerDataDir is the data directory of a Docker installation (/data,
// ./data/wiki on the host), where the wiki and the update helper on the
// host exchange update-available and update-timer.json; "" elsewhere.
func dockerDataDir() string {
	if os.Getenv("EWIKI_DOCKER") != "1" {
		return ""
	}
	return os.Getenv("EWIKI_DATA")
}

// checkUpdates runs the update check and keeps the marker for the update
// helper in step with its result.
func (s *Server) checkUpdates(ctx context.Context, force bool) (*pluginmgr.UpdateReport, error) {
	rep, err := s.plugins.CheckUpdates(ctx, version.Version, s.manifests(), force)
	if err != nil {
		return nil, err
	}
	if err := pluginmgr.SyncUpdateMarker(dockerDataDir(), rep); err != nil {
		s.log.Error("update marker for the update helper", "err", err)
	}
	return rep, nil
}

func (s *Server) manifests() []coreplugin.Manifest {
	ms := []coreplugin.Manifest{}
	for _, p := range s.app.Kernel.List() {
		ms = append(ms, p.Manifest)
	}
	return ms
}

// registerUpdateCheck: the daily update check (plugins.updates.check, on
// by default), once per instance (on the first site). It fills the
// overview and, in Docker, the marker for the update helper.
func (s *Server) registerUpdateCheck() {
	if s.app.Site != "main" || s.app.Workers == nil {
		return
	}
	s.app.Workers.Register(worker.Job{Name: "updates.check", Interval: 24 * time.Hour,
		Active: func() bool { c := s.app.Config.Get().Plugins.Updates.Check; return c == nil || *c },
		Run: func(ctx context.Context) error {
			_, err := s.checkUpdates(ctx, true)
			return err
		}})
}

// instanceScope: the caller administers the whole instance (on the first
// site: every administrator), as for the pages Updates and License.
func (s *Server) instanceScope(r *http.Request) bool {
	p := identity.From(r.Context())
	return p != nil && (s.app.Site == "main" || p.Instance())
}

// overview adds the parts of the overview the caller may see to out.
func (s *Server) overview(r *http.Request, out map[string]any) error {
	ctx := r.Context()
	p := identity.From(ctx)
	may := func(key string) bool { return perm.HasP(p, key) }

	// updates: the last check (no request to GitHub here) and how to install
	if may("core.plugins.manage") && s.instanceScope(r) {
		in := detectInstall()
		up := map[string]any{"core": nil, "security": false, "install": in}
		// the state the update helper reports (Docker only)
		if in.Kind == "docker" {
			timer, err := pluginmgr.ReadTimerState(dockerDataDir(), time.Now())
			up["timer"], up["timer_error"] = timer, ""
			if err != nil {
				s.log.Warn("state of the update helper", "err", err)
				up["timer_error"] = "unreadable"
			}
		}
		if rep := s.plugins.LastUpdateReport(); rep != nil {
			for _, it := range rep.Items {
				if it.ID == "core" {
					c := it
					up["core"] = &c
				}
				if it.Available && it.Security {
					up["security"] = true
				}
			}
		}
		out["updates"] = up
	}

	// plugins: all run in the wiki process (core plugins); plugins as own
	// processes (SDK) do not exist yet
	if may("core.plugins.manage") {
		var total, enabled, active int
		for _, pl := range s.app.Kernel.List() {
			total++
			if pl.Enabled {
				enabled++
			}
			if pl.Active {
				active++
			}
		}
		out["plugins"] = map[string]any{"total": total, "enabled": enabled, "active": active, "in_process": total}
	}

	// users of this site and the last run of each directory sync
	if may("core.users.manage") {
		u, err := userCounts(ctx, s.app.DB)
		if err != nil {
			return err
		}
		out["users"] = u
	}

	// security: which sign-in methods require a second factor
	if may("core.settings.manage") {
		cfg, err := s.app.Auth.MFAConfig(ctx)
		if err != nil {
			return err
		}
		type provider struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Required int    `json:"required"`
		}
		ps := make([]provider, 0, len(cfg.Providers))
		for _, x := range cfg.Providers {
			ps = append(ps, provider{x.ID, x.Name, x.Required})
		}
		out["security"] = map[string]any{"providers": ps, "factors": len(cfg.Factors)}
	}

	// license: the state of the key (as on the license page)
	if may("core.license.manage") {
		out["license"] = s.LicenseStatus(ctx)
	}
	return nil
}

type directoryRun struct {
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	Error     string    `json:"error,omitempty"`
}

// userCounts counts the accounts of this site (without demo accounts) and
// returns the last run of every directory sync.
func userCounts(ctx context.Context, q db.Querier) (map[string]any, error) {
	var active, disabled, invited int
	if err := q.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE NOT disabled AND provider NOT IN ('demo', 'invite')),
		count(*) FILTER (WHERE disabled AND provider NOT IN ('demo', 'invite')),
		count(*) FILTER (WHERE provider = 'invite')
		FROM users`).Scan(&active, &disabled, &invited); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT ON (provider) provider, status, started_at, error
		FROM directory_sync_runs WHERE NOT dry_run ORDER BY provider, started_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	runs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[directoryRun])
	if err != nil {
		return nil, err
	}
	if runs == nil {
		runs = []directoryRun{}
	}
	return map[string]any{"active": active, "disabled": disabled, "invited": invited, "directory": runs}, nil
}
