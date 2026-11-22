package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Maintenance mode (config.Maintenance): read-only or closed for everyone
// but administrators. Signing in always works, so administrators can come
// in; what only signs in or out may also change (sessions, factors while
// signing in), nothing else.

// maintenanceSignIn are the requests that change something and still work
// during maintenance: signing in and out (POST).
var maintenanceSignIn = []string{
	"/api/v1/auth/login", "/api/v1/auth/logout",
	"/api/v1/auth/mfa/", "/api/v1/auth/passwordless/", "/api/v1/auth/ssh/",
	"/api/v1/auth/sso/", "/api/v1/auth/link/info",
}

// maintenanceClosedRead are the reads that work while the wiki is closed:
// what the page needs to say so and to sign in (the site, texts, the look
// of the template, the sign-in itself).
var maintenanceClosedRead = []string{"/api/v1/site", "/api/v1/i18n/", "/api/v1/auth/", "/api/v1/x/core.template/"}

func hasPrefixAny(s string, list []string) bool {
	for _, p := range list {
		if s == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(s, p)) {
			return true
		}
	}
	return false
}

// maintenanceRefuses reports whether the request is refused by the
// maintenance mode m (administrators never are).
func maintenanceRefuses(m config.Maintenance, r *http.Request, p *identity.Principal) bool {
	if !m.Active() || p.Admin() {
		return false
	}
	path := r.URL.Path
	safe := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
	if !safe {
		return r.Method != http.MethodPost || !hasPrefixAny(path, maintenanceSignIn)
	}
	// live editing changes pages over a connection opened with GET
	if strings.HasPrefix(path, CollabPath) {
		return true
	}
	if m.Mode == config.MaintenanceClosed {
		if strings.HasPrefix(path, "/api/") {
			return !hasPrefixAny(path, maintenanceClosedRead)
		}
		// SCIM, the other service of the wiki; the pages themselves are the
		// plain application (serveFrontend shows no content while closed)
		return strings.HasPrefix(path, "/scim/")
	}
	return false
}

// errMaintenance is the answer to refused requests.
func errMaintenance(m config.Maintenance) error {
	if m.Mode == config.MaintenanceClosed {
		return apperr.New(http.StatusServiceUnavailable, "maintenance", "the wiki is closed for maintenance; please try again later")
	}
	return apperr.New(http.StatusServiceUnavailable, "maintenance", "the wiki is read-only for maintenance; nothing can be changed at the moment")
}

// maintenanceRetryAfter: seconds until the announced end (at least 60, at
// most a day; 300 without an end).
func maintenanceRetryAfter(m config.Maintenance) int {
	if t, err := time.Parse(time.RFC3339, m.Until); err == nil {
		d := int(time.Until(t) / time.Second)
		return min(max(d, 60), 86400)
	}
	return 300
}

// routesMaintenance: show and change the maintenance mode (administrators).
func (s *Server) routesMaintenance() {
	// the maintenance mode of the site
	// action: core.maintenance.get
	s.handle("GET /api/v1/admin/maintenance", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.app.Config.Get().Maintenance, nil
	})
	// switch the maintenance mode on, change it or switch it off
	// action: core.maintenance.set
	s.handle("PUT /api/v1/admin/maintenance", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		var in config.Maintenance
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		for l, t := range in.Message {
			in.Message[l] = strings.TrimSpace(t)
			if in.Message[l] == "" {
				delete(in.Message, l)
			}
		}
		if err := config.ValidateMaintenance(in); err != nil {
			return nil, apperr.BadRequest("%s", err.Error())
		}
		before := s.app.Config.Get().Maintenance
		if err := s.app.Config.Update(func(c *config.Config) error { c.Maintenance = in; return nil }); err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), s.app.DB, "maintenance.update", "settings", 0, before, in)
		s.app.Cache.Invalidate(r.Context(), "maintenance")
		return in, nil
	})
}

// siteMaintenance is what everybody learns about the maintenance mode (the
// site answer): the mode, the message and the expected end.
func siteMaintenance(m config.Maintenance) any {
	if !m.Active() {
		return nil
	}
	msg := m.Message
	if msg == nil {
		msg = map[string]string{}
	}
	return map[string]any{"mode": m.Mode, "message": msg, "until": m.Until}
}
