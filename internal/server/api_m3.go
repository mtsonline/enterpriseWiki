package server

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/mail"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/pgversion"
	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
	"github.com/mtsonline/enterprisewiki/internal/version"
)

func (s *Server) routesM3() {
	// ---------------------------------------------------------------- macros
	// Lists the macros of active plugins (used by the editor's macro dialog).
	// action: core.macros
	s.handle("GET /api/v1/macros", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Macros(), nil
	})

	// ---------------------------------------------------------------- updates
	s.registerUpdateCheck()
	// GET /api/v1/admin/updates?force=1 checks GitHub releases of the core
	// and all plugins with a repository (cached for an hour).
	// action: core.updates.list
	s.handle("GET /api/v1/admin/updates", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		force := r.URL.Query().Get("force") == "1"
		if !force {
			if rep := s.plugins.LastUpdateReport(); rep != nil {
				return rep, nil
			}
			if c := s.app.Config.Get().Plugins.Updates.Check; c != nil && !*c {
				return map[string]any{"checked_at": nil, "items": []any{}}, nil
			}
		}
		return s.checkUpdates(r.Context(), force)
	})
	// GET /api/v1/admin/status: the state of the wiki at a glance (tile of
	// the dashboard, overview of the administration, ewiki status): version,
	// updates from the last check (no request to GitHub here), e-mails that
	// failed or wait, the backlog of the change queue; the further parts of
	// the overview (updates with the commands to install them, plugins,
	// users, security, license) only for those who may open the matching
	// administration page (see overview).
	// action: core.admin.status
	s.handle("GET /api/v1/admin/status", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		out := map[string]any{"version": version.Version, "updates_checked_at": nil, "updates_available": 0}
		if rep := s.plugins.LastUpdateReport(); rep != nil {
			n := 0
			for _, x := range rep.Items {
				if x.Available {
					n++
				}
			}
			out["updates_checked_at"], out["updates_available"] = rep.CheckedAt, n
		}
		var failed, queued, pending int
		var oldest *time.Time
		if err := s.app.DB.QueryRow(r.Context(), `SELECT
			(SELECT count(*) FROM mail_outbox WHERE status IN ('failed', 'partial')),
			(SELECT count(*) FROM mail_outbox WHERE status = 'queued'),
			(SELECT count(*) FROM change_events),
			(SELECT min(created_at) FROM change_events)`).Scan(&failed, &queued, &pending, &oldest); err != nil {
			return nil, err
		}
		out["mail_failed"], out["mail_queued"], out["queue_pending"], out["queue_oldest"] = failed, queued, pending, oldest
		// security: failed sign-ins of the last 24 hours, audit forwarding
		// that fails
		var loginFailed, forwardErrors int
		if err := s.app.DB.QueryRow(r.Context(), `SELECT
			(SELECT count(*) FROM audit_log WHERE action = 'auth.login_failed' AND at > now() - interval '24 hours'),
			(SELECT count(*) FROM audit_forward_state WHERE errors > 0)`).Scan(&loginFailed, &forwardErrors); err != nil {
			return nil, err
		}
		out["login_failed_24h"], out["audit_forward_errors"] = loginFailed, forwardErrors
		// plugins: switched on but not running (missing dependency), restart
		// or failed build
		inactive := []string{}
		for _, p := range s.app.Kernel.List() {
			if p.Enabled && !p.Active {
				inactive = append(inactive, p.ID)
			}
		}
		slices.Sort(inactive)
		b := s.plugins.BuildStatus()
		out["plugins_inactive"], out["restart_pending"] = inactive, s.plugins.RestartPending()
		out["build_failed"] = !b.Running && b.FinishedAt != nil && !b.OK
		out["license_level"] = s.LicenseStatus(r.Context()).Level
		out["database_level"] = ""
		if st, err := pgversion.Check(r.Context(), s.app.DB, time.Now()); err == nil {
			out["database_level"] = st.Level
		}
		if err := s.overview(r, out); err != nil {
			return nil, err
		}
		return out, nil
	})
	// POST /api/v1/admin/updates/{id} installs the latest release of a
	// plugin and starts the rebuild (1-click update).
	// action: core.updates.install
	s.handle("POST /api/v1/admin/updates/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireInstance(r); err != nil { // replaces code shared by all sites
			return nil, err
		}
		id := r.PathValue("id")
		if id == "core" {
			return nil, apperr.BadRequest("the core is updated with scripts/update.sh (or the update timer) on the server")
		}
		var in struct {
			AcceptExternal bool `json:"accept_external"` // external plugins: confirm every update
		}
		if r.ContentLength != 0 {
			if err := h.Decode(r, &in); err != nil {
				return nil, err
			}
		}
		res, err := s.plugins.UpdatePlugin(r.Context(), id, pluginmgr.InstallOptions{AcceptExternal: in.AcceptExternal, By: handleOf(r)})
		if err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.update", fmt.Sprintf("%s level=%s source=%s commit=%s accepted=%v",
			id, res.Origin.Level, res.Origin.Source, res.Origin.Commit, res.Origin.Accepted))
		return res, nil
	})
}

func (s *Server) routesM4() {
	// ---------------------------------------------------------------- e-mail (admin)
	// action: core.mail.status
	s.handle("GET /api/v1/admin/mail", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.app.Mail.GetStatus(r.Context())
	})
	// POST /api/v1/admin/mail/test {"to": "..."} sends a test message right away.
	// action: core.mail.test
	s.handle("POST /api/v1/admin/mail/test", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		var in struct {
			To string `json:"to"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		site := s.app.Config.Get().UI.SiteName
		err := s.app.Mail.SendNow(r.Context(), mail.Message{To: []string{in.To}, Source: "test",
			Subject: site + ": test message", Text: "This is a test message from " + site + " (" + s.app.Config.Get().Server.BaseURL + ").\n\nOutgoing e-mail works."})
		if err != nil {
			var ae *apperr.Error
			if asAppErr(err, &ae) {
				return nil, err
			}
			return nil, apperr.BadRequest("sending failed: %s", err.Error())
		}
		s.auditSettings(r, "mail.test", in.To)
		return map[string]string{"status": "sent"}, nil
	})
	// action: core.mail.list
	s.handle("GET /api/v1/admin/mail/outbox", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.app.Mail.Outbox(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("q"), h.QueryInt(r, "limit", 100), h.QueryInt(r, "offset", 0))
	})
	// GET /api/v1/admin/mail/outbox/{id} returns a message with the delivery state of every recipient.
	// action: core.mail.get
	s.handle("GET /api/v1/admin/mail/outbox/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Mail.Get(r.Context(), id)
	})
	// action: core.mail.retry
	s.handle("POST /api/v1/admin/mail/outbox/{id}/retry", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Mail.Retry(r.Context(), id)
	})
	// action: core.mail.delete
	s.handle("DELETE /api/v1/admin/mail/outbox/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if err := s.app.Mail.Delete(r.Context(), id); err != nil {
			return nil, err
		}
		s.auditSettings(r, "mail.delete", strconv.FormatInt(id, 10))
		return h.NoContent{}, nil
	})
	// POST /api/v1/admin/jobs/{name}/run runs a background job now (e.g. "mail").
	// action: core.jobs.list
	s.handle("GET /api/v1/admin/jobs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.app.Workers.Names(), nil
	})
	// action: core.jobs.run
	s.handle("POST /api/v1/admin/jobs/{name}/run", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		if err := s.app.Workers.RunOnce(r.Context(), r.PathValue("name")); err != nil {
			return nil, err
		}
		return h.NoContent{}, nil
	})

	// ---------------------------------------------------------------- notification preferences
	// action: core.me.notifications.show
	s.handle("GET /api/v1/me/notifications", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		us, err := s.app.Mail.Users(r.Context(), nil, []int64{p.ID()})
		if err != nil || len(us) == 0 {
			return nil, apperr.NotFound("user")
		}
		prefs := map[string]bool{}
		kinds := mail.NotificationKinds()
		for _, k := range kinds {
			v, ok := us[0].Notify[k]
			if !ok {
				v = mail.DefaultOn(k)
			}
			prefs[k] = v
		}
		return map[string]any{"email": us[0].Email, "prefs": prefs, "kinds": kinds, "labels": mail.KindLabels(), "enabled": s.app.Mail.Enabled()}, nil
	})
	// action: core.me.notifications.set
	s.handle("PUT /api/v1/me/notifications", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		var in map[string]bool
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Mail.SetPreferences(r.Context(), p.ID(), in)
	})
}
