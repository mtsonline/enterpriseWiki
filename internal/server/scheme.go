package server

import (
	"context"
	"cmp"
	"net/http"
	"slices"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Light and dark: every person chooses "system" (as the device is set),
// "light" or "dark". Signed-in persons keep the choice in their personal
// settings (pref "scheme"), guests in a small cookie the server sets. It is
// technically necessary (it only keeps the choice someone made) and holds
// nothing else. The server writes the scheme into the page it sends, so a
// page in the dark scheme does not flash light while it loads.

// schemeCookie keeps the choice of a guest (and of a person on this device).
const schemeCookie = "ewiki_scheme"

// schemeChoice is the scheme chosen by the person or this browser ("" = none).
func schemeChoice(r *http.Request, p *identity.Principal) string {
	if p != nil {
		if v := p.Prefs["scheme"]; slices.Contains(coreplugin.ColorSchemes, v) {
			return v
		}
	}
	if c, err := r.Cookie(schemeCookie); err == nil && slices.Contains(coreplugin.ColorSchemes, c.Value) {
		return c.Value
	}
	return ""
}

// templateScheme is the scheme a template shows by default ("" = light
// colours only).
func (s *Server) templateScheme(ctx context.Context, id string) string {
	if id == "core.basic" || id == "" {
		return "system"
	}
	if !s.app.Kernel.IsTemplate(id) {
		// a template a plugin offers (templates of the designer: with dark colors or light only)
		return s.app.Content.TemplateColorScheme(ctx, id)
	}
	p, ok := s.app.Kernel.Get(id)
	if !ok {
		return ""
	}
	return p.Manifest().ColorScheme
}

// injectScheme marks the page with the scheme of the site template and the
// choice of the person (only known values, never text from the request).
func (s *Server) injectScheme(page []byte, r *http.Request) []byte {
	choice := schemeChoice(r, identity.From(r.Context()))
	attrs := ""
	if def := s.templateScheme(r.Context(), s.app.Config.Get().UI.Template); def != "" {
		attrs = ` data-scheme="` + cmp.Or(choice, def) + `"`
	}
	if choice != "" {
		attrs += ` data-scheme-choice="` + choice + `"`
	}
	if attrs == "" {
		return page
	}
	loc := htmlTagRe.FindIndex(page)
	if loc == nil {
		return page
	}
	end := loc[1] - 1 // the ">" of <html …>
	return slices.Concat(page[:end], []byte(attrs), page[end:])
}

func (s *Server) routesScheme() {
	// choose light, dark or as the device is set ("" = the default of the
	// template): kept in a cookie and, when signed in, in the personal settings
	// action: core.scheme.set
	s.handle("PUT /api/v1/scheme", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Scheme string `json:"scheme"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if in.Scheme != "" && !slices.Contains(coreplugin.ColorSchemes, in.Scheme) {
			return nil, apperr.BadRequest("scheme: one of system, light, dark (or empty for the default)")
		}
		if p := identity.From(r.Context()); p != nil && p.UserID != 0 {
			if err := s.app.Auth.SetPrefs(r.Context(), map[string]string{"scheme": in.Scheme}); err != nil {
				return nil, err
			}
		}
		// #nosec G124 -- HttpOnly, SameSite=Lax; Secure follows server.secure_cookies (set for HTTPS base URLs)
		ck := &http.Cookie{Name: schemeCookie, Value: in.Scheme, Path: "/", HttpOnly: true,
			Secure: s.app.Config.Get().Server.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: int((400 * 24 * time.Hour).Seconds())}
		if in.Scheme == "" {
			ck.MaxAge = -1
		}
		http.SetCookie(w, ck)
		return map[string]any{"scheme": in.Scheme}, nil
	})
}
