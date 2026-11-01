package server

import (
	"net/http"
	"strconv"

	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/mail"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/version"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

func (s *Server) routesM3() {
	// ---------------------------------------------------------------- macros
	// Lists the macros of active plugins (used by the editor's macro dialog).
	s.handle("GET /api/v1/macros", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Content.Macros(), nil
	})

	// ---------------------------------------------------------------- updates
	manifests := func() []sdk.Manifest {
		ms := []sdk.Manifest{}
		for _, p := range s.app.Kernel.List() {
			ms = append(ms, p.Manifest)
		}
		return ms
	}
	// GET /api/v1/admin/updates?force=1 checks GitHub releases of the core
	// and all plugins with a repository (cached for an hour).
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
		return s.plugins.CheckUpdates(r.Context(), version.Version, manifests(), force)
	})
	// POST /api/v1/admin/updates/{id} installs the latest release of a
	// plugin and starts the rebuild (1-click update).
	s.handle("POST /api/v1/admin/updates/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id := r.PathValue("id")
		if id == "core" {
			return nil, apperr.BadRequest("the core is updated with scripts/update.sh (or the update timer) on the server")
		}
		res, err := s.plugins.UpdatePlugin(r.Context(), id)
		if err != nil {
			return nil, err
		}
		s.auditSettings(r, "plugin.update", id)
		return res, nil
	})
}

func (s *Server) routesM4() {
	// ---------------------------------------------------------------- e-mail (admin)
	s.handle("GET /api/v1/admin/mail", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.app.Mail.GetStatus(r.Context())
	})
	// POST /api/v1/admin/mail/test {"to": "..."} sends a test message right away.
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
	s.handle("GET /api/v1/admin/mail/outbox", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.app.Mail.Outbox(r.Context(), r.URL.Query().Get("status"), h.QueryInt(r, "limit", 100), h.QueryInt(r, "offset", 0))
	})
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
	s.handle("GET /api/v1/admin/jobs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.app.Workers.Names(), nil
	})
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
		for _, k := range mail.NotificationKinds {
			v, ok := us[0].Notify[k]
			prefs[k] = !ok || v
		}
		return map[string]any{"email": us[0].Email, "prefs": prefs, "kinds": mail.NotificationKinds}, nil
	})
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
