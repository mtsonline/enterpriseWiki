package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

const (
	sessionCookie = "ewiki_session"
	csrfHeader    = "X-Requested-With"
	csrfValue     = "ewiki"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(s int) {
	w.status = s
	w.ResponseWriter.WriteHeader(s)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// clientIP returns the caller IP, honouring X-Forwarded-For only from trusted proxies.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	trusted := s.app.Config.Get().Server.TrustedProxies
	if len(trusted) == 0 {
		return host
	}
	ip := net.ParseIP(host)
	ok := false
	for _, t := range trusted {
		if _, n, err := net.ParseCIDR(t); err == nil && ip != nil && n.Contains(ip) {
			ok = true
		} else if t == host {
			ok = true
		}
	}
	if ok {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	return host
}

// wrap applies recovery, logging, security headers, body limits and authentication.
func (s *Server) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "err", rec, "stack", string(debug.Stack()))
				httpx.WriteError(sw, r, fmt.Errorf("panic: %v", rec))
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				s.log.Debug("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "dur", time.Since(start))
			}
		}()
		cfg := s.app.Config.Get()
		h := sw.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'self'; base-uri 'self'; form-action 'self'")
		if cfg.Server.SecureCookies {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
			max := int64(cfg.Server.MaxBodyMB)
			if max <= 0 {
				max = 64
			}
			if r.URL.Path != "/api/v1/files" { // uploads have their own limit (storage.max_upload_mb)
				r.Body = http.MaxBytesReader(sw, r.Body, max<<20)
			}
		}
		ctx, err := s.authenticate(sw, r)
		if err != nil {
			httpx.WriteError(sw, r, err)
			return
		}
		next.ServeHTTP(sw, r.WithContext(ctx))
	})
}

// authenticate resolves the caller from a bearer token or the session cookie.
// Cookie-authenticated state-changing requests must carry the
// X-Requested-With: ewiki header (CSRF protection: browsers do not send
// custom headers cross-site without a CORS preflight, which is never allowed).
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (context.Context, error) {
	ctx := r.Context()
	if p := identity.From(ctx); p != nil && p.Via == "cli" {
		return ctx, nil // in-process CLI call
	}
	if a := r.Header.Get("Authorization"); a != "" {
		tok, ok := strings.CutPrefix(a, "Bearer ")
		if !ok {
			return ctx, apperr.ErrUnauthorized
		}
		p, err := s.app.Auth.Authenticate(ctx, strings.TrimSpace(tok))
		if err != nil {
			return ctx, err
		}
		return identity.With(ctx, p), nil
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return ctx, nil
	}
	p, err := s.app.Auth.Authenticate(ctx, c.Value)
	if err != nil {
		s.clearCookie(w)
		return ctx, nil
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		if r.Header.Get(csrfHeader) != csrfValue {
			return ctx, &apperr.Error{Status: http.StatusForbidden, Code: "csrf", Msg: "missing X-Requested-With header"}
		}
	}
	return identity.With(ctx, p), nil
}

func (s *Server) setCookie(w http.ResponseWriter, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: exp, HttpOnly: true,
		Secure: s.app.Config.Get().Server.SecureCookies, SameSite: http.SameSiteLaxMode})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.app.Config.Get().Server.SecureCookies, SameSite: http.SameSiteLaxMode})
}

// peerIP returns the address of the direct peer (ignoring X-Forwarded-For).
func (s *Server) peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func ipInCIDR(ip, cidr string) bool {
	_, n, err := net.ParseCIDR(cidr)
	p := net.ParseIP(ip)
	return err == nil && p != nil && n.Contains(p)
}
