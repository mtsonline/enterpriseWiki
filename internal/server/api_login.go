package server

import (
	"net/http"
	"strings"
	"unicode"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// Limits of the sign-in page settings.
const (
	loginLabelMax = 80
	loginOrderMax = 1000
	loginButtons  = 100
)

// loginPage is what the sign-in page needs beyond the providers: passkeys
// in the user name field, and an automatic redirect (only to a configured
// redirect provider).
func (s *Server) loginPage() map[string]any {
	c := s.app.Config.Get().Login
	auto := ""
	if c.AutoRedirect != "" && s.app.Auth.RedirectProviderConfigured(c.AutoRedirect) {
		auto = c.AutoRedirect
	}
	return map[string]any{"passkey_autofill": c.Autofill(), "auto_redirect": auto}
}

func (s *Server) routesLogin() {
	// action: core.auth.login_page.view
	s.handle("GET /api/v1/admin/auth/login", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		c := s.app.Config.Get().Login
		if c.Buttons == nil {
			c.Buttons = map[string]config.LoginButton{}
		}
		return map[string]any{"passkey_autofill": c.Autofill(), "auto_redirect": c.AutoRedirect, "buttons": c.Buttons,
			"providers": s.app.Auth.AllProviderInfos()}, nil
	})
	// action: core.auth.login_page.update
	s.handle("PUT /api/v1/admin/auth/login", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		var in config.Login
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		before := s.app.Config.Get().Login
		if err := s.checkLogin(&in, before); err != nil {
			return nil, err
		}
		if err := s.app.Config.Update(func(c *config.Config) error {
			c.Login = in
			return nil
		}); err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), s.app.DB, "auth.login_page.update", "settings", 0, before, in)
		return s.loginPage(), nil
	})
}

// checkLogin validates and normalises the sign-in page settings: buttons
// and the automatic redirect only for configured redirect providers, short
// plain labels. Entries that are unchanged from the stored ones stay even if
// their provider is not configured at the moment (removed or incomplete), so
// a later save neither fails nor loses them.
func (s *Server) checkLogin(in *config.Login, stored config.Login) error {
	in.AutoRedirect = strings.TrimSpace(in.AutoRedirect)
	if in.AutoRedirect != "" && in.AutoRedirect != stored.AutoRedirect && !s.app.Auth.RedirectProviderConfigured(in.AutoRedirect) {
		return apperr.BadRequest("auto_redirect: %q is not a configured sign-in provider with its own sign-in page", in.AutoRedirect)
	}
	if len(in.Buttons) > loginButtons {
		return apperr.BadRequest("too many buttons (at most %d)", loginButtons)
	}
	for id, b := range in.Buttons {
		if old, ok := stored.Buttons[id]; !(ok && old == b) && !s.app.Auth.RedirectProviderConfigured(id) {
			return apperr.BadRequest("buttons: %q is not a configured sign-in provider with its own sign-in page", id)
		}
		if b.Order < -loginOrderMax || b.Order > loginOrderMax {
			return apperr.BadRequest("buttons.%s.order: between %d and %d", id, -loginOrderMax, loginOrderMax)
		}
		b.Label = strings.TrimSpace(b.Label)
		if len([]rune(b.Label)) > loginLabelMax {
			return apperr.BadRequest("buttons.%s.label: at most %d characters", id, loginLabelMax)
		}
		if strings.IndexFunc(b.Label, unicode.IsControl) >= 0 {
			return apperr.BadRequest("buttons.%s.label: no control characters", id)
		}
		if b == (config.LoginButton{}) {
			delete(in.Buttons, id)
			continue
		}
		in.Buttons[id] = b
	}
	if len(in.Buttons) == 0 {
		in.Buttons = nil
	}
	if in.Autofill() {
		in.PasskeyAutofill = nil // the default
	}
	return nil
}
