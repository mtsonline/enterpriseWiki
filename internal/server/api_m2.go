package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/storage"
)

const secretMask = "********"

// maskConfig hides secrets of a configuration copy before it leaves the server.
func maskConfig(c *config.Config) {
	c.Database.URL = maskURL(c.Database.URL)
	maskSecrets(c.Settings)
	if p := c.Mail.SMTP.Password; p != "" && !config.HasEnvRef(p) {
		c.Mail.SMTP.Password = secretMask
	}
	if p := c.Mail.Bounce.Password; p != "" && !config.HasEnvRef(p) {
		c.Mail.Bounce.Password = secretMask
	}
}

// maskSecrets hides plugin settings whose key looks secret (unless they are
// ${ENV} references, which are safe to show).
func maskSecrets(settings map[string]map[string]any) {
	for _, m := range settings {
		for k, v := range m {
			lk := strings.ToLower(k)
			if !(strings.Contains(lk, "password") || strings.Contains(lk, "secret") || strings.HasSuffix(lk, "_key")) {
				continue
			}
			if s, ok := v.(string); ok && s != "" && !config.HasEnvRef(s) {
				m[k] = secretMask
			}
		}
	}
}

// isTrustedProxy reports whether the direct peer is a configured trusted proxy.
func (s *Server) isTrustedProxy(r *http.Request) bool {
	trusted := s.app.Config.Get().Server.TrustedProxies
	if len(trusted) == 0 {
		return false
	}
	return isTrusted(trusted, s.peerIP(r))
}

func (s *Server) routesM2() {
	// ---------------------------------------------------------------- single sign-on
	h.RecordRoute("GET /api/v1/auth/sso/{provider}/start")
	s.app.Mux.HandleFunc("GET /api/v1/auth/sso/{provider}/start", func(w http.ResponseWriter, r *http.Request) {
		next := r.URL.Query().Get("next")
		redirect, res, err := s.app.Auth.SSOStart(r.Context(), r.PathValue("provider"), r, next, s.isTrustedProxy(r), s.clientIP(r), r.UserAgent())
		if err != nil {
			s.ssoError(w, r, err)
			return
		}
		if res != nil {
			s.auditLogin(r, res.Principal, "sso:"+r.PathValue("provider"), "", nil)
			s.setCookie(w, res.Token, res.ExpiresAt)
			http.Redirect(w, r, auth.SafeNext(res.Next), http.StatusSeeOther) // #nosec G710 -- SafeNext allows local paths only
			return
		}
		http.Redirect(w, r, redirect, http.StatusFound) // #nosec G710 -- URL of the configured identity provider, not from the request
	})
	callback := func(w http.ResponseWriter, r *http.Request) {
		res, err := s.app.Auth.SSOCallback(r.Context(), r.PathValue("provider"), r, s.isTrustedProxy(r), s.clientIP(r), r.UserAgent())
		if err != nil {
			s.auditLogin(r, nil, "sso:"+r.PathValue("provider"), "", err)
			s.ssoError(w, r, err)
			return
		}
		s.auditLogin(r, res.Principal, "sso:"+r.PathValue("provider"), "", nil)
		s.setCookie(w, res.Token, res.ExpiresAt)
		http.Redirect(w, r, auth.SafeNext(res.Next), http.StatusSeeOther) // #nosec G710 -- SafeNext allows local paths only
	}
	// Called by the identity provider (redirect or HTTP-POST binding).
	h.RecordRoute("GET /api/v1/auth/sso/{provider}/callback")
	h.RecordRoute("POST /api/v1/auth/sso/{provider}/callback")
	s.app.Mux.HandleFunc("GET /api/v1/auth/sso/{provider}/callback", callback)
	s.app.Mux.HandleFunc("POST /api/v1/auth/sso/{provider}/callback", callback)

	// ---------------------------------------------------------------- two-factor authentication
	s.handle("GET /api/v1/me/totp", func(w http.ResponseWriter, r *http.Request) (any, error) {
		on, err := s.app.Auth.TOTPStatus(r.Context())
		return map[string]bool{"enabled": on}, err
	})
	s.handle("POST /api/v1/me/totp/setup", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Auth.BeginTOTP(r.Context(), s.app.Config.Get().UI.SiteName)
	})
	s.handle("POST /api/v1/me/totp/enable", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Code string `json:"code"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.EnableTOTP(r.Context(), in.Code)
	})
	s.handle("POST /api/v1/me/totp/disable", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Code string `json:"code"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.DisableTOTP(r.Context(), in.Code)
	})
	// second factor by e-mail and network restriction of the own account
	s.handle("GET /api/v1/me/mfa", func(w http.ResponseWriter, r *http.Request) (any, error) {
		st, err := s.app.Auth.MFA(r.Context())
		if err != nil {
			return nil, err
		}
		return map[string]any{"totp": st.TOTP, "email": st.Email, "email_address": st.Address, "local": st.Local,
			"networks": st.Networks, "current_ip": s.clientIP(r)}, nil
	})
	s.handle("POST /api/v1/me/mfa/email/code", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return h.NoContent{}, s.app.Auth.BeginEmailMFA(r.Context())
	})
	mfaEmail := func(on bool) func(w http.ResponseWriter, r *http.Request) (any, error) {
		return func(w http.ResponseWriter, r *http.Request) (any, error) {
			var in struct {
				Code string `json:"code"`
			}
			if err := h.Decode(r, &in); err != nil {
				return nil, err
			}
			return h.NoContent{}, s.app.Auth.SetEmailMFA(r.Context(), on, in.Code)
		}
	}
	s.handle("POST /api/v1/me/mfa/email/enable", mfaEmail(true))
	s.handle("POST /api/v1/me/mfa/email/disable", mfaEmail(false))
	s.handle("PUT /api/v1/me/mfa/networks", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Networks []string `json:"networks"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		nets, err := s.app.Auth.SetOwnNetworks(r.Context(), in.Networks)
		return map[string]any{"networks": nets}, err
	})

	// ---------------------------------------------------------------- files
	s.handle("POST /api/v1/files", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p := identity.From(r.Context())
		if p == nil {
			return nil, apperr.ErrUnauthorized
		}
		if p.Via == "apikey" && p.KeyPerms&(2|4) == 0 {
			return nil, apperr.ErrForbidden
		}
		r.Body = http.MaxBytesReader(w, r.Body, s.app.Files.MaxBytes()+1<<20)
		mr, err := r.MultipartReader()
		if err != nil {
			return nil, apperr.BadRequest("expected multipart/form-data with a field \"file\"")
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				return nil, apperr.BadRequest("no field \"file\" in the upload")
			}
			if part.FormName() != "file" {
				_ = part.Close()
				continue
			}
			f, err := s.app.Files.Put(r.Context(), part, part.FileName())
			_ = part.Close()
			if err != nil {
				return nil, err
			}
			return h.Created{V: f}, nil
		}
	})
	// Images by hash: only settings assets, profile photos, files of media
	// the caller may see and images embedded in elements the caller may see
	// (content from before the media library). Other file types are only
	// delivered through their medium or element (permission checked).
	h.RecordRoute("GET /api/v1/files/{hash}")
	s.app.Mux.HandleFunc("GET /api/v1/files/{hash}", func(w http.ResponseWriter, r *http.Request) {
		store := s.app.Files
		f, err := store.Get(r.Context(), r.PathValue("hash"))
		if err != nil && s.app.Instance != nil && !s.app.SeparateAccounts && s.sharedProfilePhoto(r.Context(), r.PathValue("hash")) {
			// profile photos belong to the shared user account: they may
			// have been uploaded on another site with shared accounts. Only
			// these images are looked up elsewhere - never other files of
			// another site.
			for _, o := range s.app.Instance.Sites() {
				if o == s.app || o.SeparateAccounts {
					continue
				}
				if of, oerr := o.Files.Get(r.Context(), r.PathValue("hash")); oerr == nil {
					store, f, err = o.Files, of, nil
					break
				}
			}
		}
		if err != nil || !strings.HasPrefix(f.Mime, "image/") || strings.Contains(f.Mime, "svg") || !s.fileVisible(r, f.Hash) {
			h.WriteError(w, r, apperr.NotFound("image"))
			return
		}
		store.Serve(w, r, f, "", true)
	})
}

// ssoError sends the browser back to the login page with a message.
func (s *Server) ssoError(w http.ResponseWriter, r *http.Request, err error) {
	_, code, msg := apperr.Status(err)
	if code == "internal" {
		s.log.Error("sso failed", "err", err)
	}
	var ae *apperr.Error
	if code == "link_required" && errors.As(err, &ae) {
		http.Redirect(w, r, "/login?link="+url.QueryEscape(ae.Detail), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login?error="+url.QueryEscape(msg), http.StatusSeeOther)
}

// sharedProfilePhoto reports whether hash is the profile photo of a shared
// (instance) account.
func (s *Server) sharedProfilePhoto(ctx context.Context, hash string) bool {
	if !storage.ValidHash(hash) {
		return false
	}
	var ok bool
	err := s.app.Instance.Shared.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE profile->>'photo' = $1)`, hash).Scan(&ok)
	return err == nil && ok
}
