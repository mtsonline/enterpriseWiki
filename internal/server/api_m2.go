package server

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
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
	ip := s.peerIP(r)
	for _, t := range trusted {
		if t == ip || ipInCIDR(ip, t) {
			return true
		}
	}
	return false
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
			http.Redirect(w, r, res.Next, http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, redirect, http.StatusFound)
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
		http.Redirect(w, r, res.Next, http.StatusSeeOther)
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
				part.Close()
				continue
			}
			f, err := s.app.Files.Put(r.Context(), part, part.FileName())
			part.Close()
			if err != nil {
				return nil, err
			}
			return h.Created{V: f}, nil
		}
	})
	// Images are served to anybody who knows the (unguessable) SHA-256, so
	// pictures embedded in public pages work for guests. Other file types are
	// only delivered through the element they belong to (permission checked).
	h.RecordRoute("GET /api/v1/files/{hash}")
	s.app.Mux.HandleFunc("GET /api/v1/files/{hash}", func(w http.ResponseWriter, r *http.Request) {
		f, err := s.app.Files.Get(r.Context(), r.PathValue("hash"))
		if err != nil || !strings.HasPrefix(f.Mime, "image/") || strings.Contains(f.Mime, "svg") {
			h.WriteError(w, r, apperr.NotFound("image"))
			return
		}
		s.app.Files.Serve(w, r, f, "", true)
	})
	_ = storage.ValidHash
}

// ssoError sends the browser back to the login page with a message.
func (s *Server) ssoError(w http.ResponseWriter, r *http.Request, err error) {
	_, code, msg := apperr.Status(err)
	if code == "internal" {
		s.log.Error("sso failed", "err", err)
	}
	http.Redirect(w, r, "/login?error="+url.QueryEscape(msg), http.StatusSeeOther)
}
