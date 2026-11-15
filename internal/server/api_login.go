package server

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// Limits of the sign-in page settings.
const (
	loginLabelMax = 80
	loginOrderMax = 1000
	loginButtons  = 100
	loginDomains  = 100
	loginGroups   = 100
)

// loginPage is what the sign-in page needs beyond the providers: passkeys
// in the user name field, and an automatic redirect (only to a configured
// redirect provider).
func (s *Server) loginPage(ctx context.Context) map[string]any {
	c := s.app.Config.Get().Login
	auto := ""
	if c.AutoRedirect != "" && s.app.Auth.RedirectProviderConfigured(c.AutoRedirect) {
		auto = c.AutoRedirect
	}
	return map[string]any{"passkey_autofill": c.Autofill(), "auto_redirect": auto, "registration": s.app.Auth.RegistrationOpen(ctx),
		"password_reset": s.app.Auth.PasswordResetOpen(ctx)}
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
		reg := c.Registration
		if reg.Domains == nil {
			reg.Domains = []string{}
		}
		if reg.Groups == nil {
			reg.Groups = []int64{}
		}
		// registration_here: the accounts of this site are its own (else only
		// the main site registers); mail_ready: e-mail can be sent
		mailReady := s.app.Auth.MailReady == nil || s.app.Auth.MailReady()
		return map[string]any{"passkey_autofill": c.Autofill(), "auto_redirect": c.AutoRedirect, "buttons": c.Buttons,
			"providers": s.app.Auth.AllProviderInfos(), "registration": reg, "password_reset": c.ResetEnabled(),
			"registration_here": s.app.Auth.RegistrationAllowedHere(r.Context()), "mail_ready": mailReady}, nil
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
		if err := s.checkLogin(r.Context(), &in, before); err != nil {
			return nil, err
		}
		if err := s.app.Config.Update(func(c *config.Config) error {
			c.Login = in
			return nil
		}); err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), s.app.DB, "auth.login_page.update", "settings", 0, before, in)
		return s.loginPage(r.Context()), nil
	})

	// local accounts people create themselves (when switched on): the same
	// answer whether or not the address has an account
	// action: core.auth.register
	s.handle("POST /api/v1/auth/register", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in auth.Registration
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := s.app.Auth.Register(r.Context(), in, s.clientIP(r)); err != nil {
			return nil, err
		}
		h.Write(w, http.StatusAccepted, map[string]string{"status": "check_mail"})
		return h.Handled{}, nil
	})
	// "Forgot your password?" for local accounts: the same answer for every
	// address; the mail goes out in the background
	// action: core.auth.password_reset
	s.handle("POST /api/v1/auth/password-reset", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Email string `json:"email"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := s.app.Auth.RequestPasswordReset(r.Context(), in.Email, s.clientIP(r)); err != nil {
			return nil, err
		}
		h.Write(w, http.StatusAccepted, map[string]string{"status": "check_mail"})
		return h.Handled{}, nil
	})
	// action: core.auth.password_reset.confirm
	s.handle("POST /api/v1/auth/password-reset/confirm", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		u, err := s.app.Auth.ResetPassword(r.Context(), in.Token, in.Password)
		if err != nil {
			return nil, err
		}
		return map[string]string{"username": u.Username}, nil
	})
	// action: core.auth.register.confirm
	s.handle("POST /api/v1/auth/register/confirm", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Token string `json:"token"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		u, err := s.app.Auth.ConfirmRegistration(r.Context(), in.Token)
		if err != nil {
			return nil, err
		}
		return map[string]string{"username": u.Username, "email": u.Email}, nil
	})
}

// checkLogin validates and normalises the sign-in page settings: buttons
// and the automatic redirect only for configured redirect providers, short
// plain labels. Entries that are unchanged from the stored ones stay even if
// their provider is not configured at the moment (removed or incomplete), so
// a later save neither fails nor loses them.
func (s *Server) checkLogin(ctx context.Context, in *config.Login, stored config.Login) error {
	if err := s.checkRegistration(ctx, &in.Registration, stored.Registration); err != nil {
		return err
	}
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
	if in.ResetEnabled() {
		in.PasswordReset = nil // the default
	}
	return nil
}

// checkRegistration validates the registration settings: only where the
// accounts of the site are its own, plain domain names, existing groups
// without an administrator role.
func (s *Server) checkRegistration(ctx context.Context, in *config.Registration, stored config.Registration) error {
	if in.Enabled && !stored.Enabled && !s.app.Auth.RegistrationAllowedHere(ctx) {
		return apperr.BadRequest("registration: this site shares the accounts of the main site; switch it on there")
	}
	if len(in.Domains) > loginDomains {
		return apperr.BadRequest("registration.domains: at most %d", loginDomains)
	}
	seen := map[string]bool{}
	domains := in.Domains[:0]
	for _, d := range in.Domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" || seen[d] {
			continue
		}
		if !validDomain(strings.TrimPrefix(d, "*.")) {
			return apperr.BadRequest("registration.domains: %q is not a domain name (example.com or *.example.com)", d)
		}
		seen[d] = true
		domains = append(domains, d)
	}
	in.Domains = domains
	if len(in.Domains) == 0 {
		in.Domains = nil
	}
	if len(in.Groups) > loginGroups {
		return apperr.BadRequest("registration.groups: at most %d", loginGroups)
	}
	if len(in.Groups) == 0 {
		in.Groups = nil
		return nil
	}
	slices.Sort(in.Groups)
	in.Groups = slices.Compact(in.Groups)
	var found int
	var admin bool
	if err := s.app.DB.QueryRow(ctx, `SELECT count(*), coalesce(bool_or(EXISTS(SELECT 1 FROM group_roles gr JOIN roles r ON r.id=gr.role_id
		WHERE gr.group_id=g.id AND r.is_admin)), false) FROM groups g WHERE g.id = ANY($1)`, in.Groups).Scan(&found, &admin); err != nil {
		return err
	}
	if found != len(in.Groups) {
		return apperr.BadRequest("registration.groups: a group does not exist")
	}
	if admin {
		return apperr.BadRequest("registration.groups: new accounts never join a group with an administrator role")
	}
	return nil
}

// validDomain: labels of letters, digits and hyphens, at least two labels.
func validDomain(d string) bool {
	if len(d) > 253 || !strings.Contains(d, ".") {
		return false
	}
	for _, l := range strings.Split(d, ".") {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, r := range l {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r > 127) {
				return false
			}
		}
	}
	return true
}
