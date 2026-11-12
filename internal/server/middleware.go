package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
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

// clientIP returns the caller IP. X-Forwarded-For is only honoured when the
// direct peer is a trusted proxy; the header is read from the right and the
// first address that is not a trusted proxy is the client (addresses further
// left were set by the client and could be forged).
func (s *Server) clientIP(r *http.Request) string {
	host := s.peerIP(r)
	trusted := s.app.Config.Get().Server.TrustedProxies
	if len(trusted) == 0 || !isTrusted(trusted, host) {
		return host
	}
	parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if net.ParseIP(ip) == nil {
			break // garbage: stop at the last valid hop
		}
		host = ip
		if !isTrusted(trusted, ip) {
			break
		}
	}
	return host
}

// isTrusted reports whether ip is one of the trusted proxies (IPs or CIDRs).
func isTrusted(trusted []string, ip string) bool {
	for _, t := range trusted {
		if t == ip || ipInCIDR(ip, t) {
			return true
		}
	}
	return false
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
		r = r.WithContext(identity.WithSite(r.Context(), s.app.Site))
		cfg := s.app.Config.Get()
		h := sw.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		frame, script := s.officeCSP() // origins of active office programs
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'"+script+"; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; frame-src 'self'"+frame+"; object-src 'none'; frame-ancestors 'self'; base-uri 'self'; form-action 'self'"+frame)
		if cfg.Server.SecureCookies {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
			if err := pagingParams(r); err != nil { // S-08
				httpx.WriteError(sw, r, err)
				return
			}
			max := int64(cfg.Server.MaxBodyMB)
			if max <= 0 {
				max = 64
			}
			if r.URL.Path != "/api/v1/files" { // uploads have their own limit (storage.max_upload_mb)
				r.Body = http.MaxBytesReader(sw, r.Body, max<<20)
			}
		}
		r = r.WithContext(audit.WithRequest(r.Context(), s.clientIP(r), r.UserAgent()))
		ctx, err := s.authenticate(sw, r)
		if err != nil {
			httpx.WriteError(sw, r, err)
			return
		}
		r = r.WithContext(ctx)
		if p := identity.From(ctx); !p.InProcess() && s.rateLimited(sw, r, p.ID(), cfg) {
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Body != nil && r.Body != http.NoBody {
			// S-02: small bodies for guests, JSON bounded; uploads keep max_body_mb
			if lim := bodyLimit(cfg, identity.From(ctx) == nil, r); lim > 0 {
				if r.ContentLength > lim {
					httpx.WriteError(sw, r, apperr.TooLarge("request body too large"))
					return
				}
				r.Body = http.MaxBytesReader(sw, r.Body, lim)
			}
			// at most 16 large bodies at the same time (memory); guests send
			// small bodies only and cannot hold these places
			if identity.From(ctx) != nil && (r.ContentLength > 1<<20 || r.ContentLength < 0) {
				select {
				case bigBodies <- struct{}{}:
					defer func() { <-bigBodies }()
				case <-r.Context().Done():
					return
				case <-time.After(30 * time.Second):
					httpx.WriteError(sw, r, apperr.TooMany("server busy, try again"))
					return
				}
			}
		}
		// guests: answer from the cache (or fill it)
		if identity.From(ctx) == nil && s.app.Cache.ServeAPI(sw, r, next) {
			return
		}
		next.ServeHTTP(sw, r)
		// requests refused for missing permissions (audit.log_denied)
		if sw.status == http.StatusForbidden && cfg.Audit.LogDenied && strings.HasPrefix(r.URL.Path, "/api/") {
			_ = audit.Log(ctx, s.app.DB, "access.denied", "request", 0, nil, map[string]string{"method": r.Method, "path": r.URL.Path})
		}
		// any successful change invalidates the guest cache
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions &&
			strings.HasPrefix(r.URL.Path, "/api/") && sw.status < 400 && !noInvalidate(r.URL.Path) {
			s.app.Cache.Invalidate(ctx, r.Method+" "+r.URL.Path)
		}
	})
}

// authenticate resolves the caller from a bearer token or the session cookie.
// Cookie-authenticated state-changing requests must carry the
// X-Requested-With: ewiki header (CSRF protection: browsers do not send
// custom headers cross-site without a CORS preflight, which is never allowed).
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (context.Context, error) {
	ctx := r.Context()
	if p := identity.From(ctx); p.InProcess() {
		return ctx, nil // in-process CLI call
	}
	if ssoInbound(r.URL.Path) || scimInbound(r.URL.Path) {
		// messages of identity providers (assertions, logout requests) are
		// authenticated by their signature, never by the browser's session:
		// a same-site IdP's form POST carries the cookie but no CSRF header;
		// SCIM requests by their own bearer token (see scim.go)
		return ctx, nil
	}
	if s.app.Media.TokenPath(r.URL.Path) {
		// office servers authenticate with the access token of one file
		// (checked by the WOPI handlers); sessions and API keys never count
		return ctx, nil
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
		// S-07: also without a session (sign-in, guest forms) a changing
		// request needs the header: no foreign page can sign a visitor in
		// to an account of the attacker (login CSRF)
		if strings.HasPrefix(r.URL.Path, "/api/") && !safeMethod(r.Method) && r.Header.Get(csrfHeader) != csrfValue {
			return ctx, &apperr.Error{Status: http.StatusForbidden, Code: "csrf", Msg: "missing X-Requested-With header"}
		}
		return ctx, nil
	}
	if r.Header.Get("X-Ewiki-Background") == "1" {
		ctx = auth.WithBackground(ctx)
	}
	p, err := s.app.Auth.Authenticate(ctx, c.Value)
	if errors.Is(err, auth.ErrNetworkDenied) {
		return ctx, err // the session stays valid in the allowed networks
	}
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
	// #nosec G124 -- HttpOnly, SameSite=Lax; Secure follows server.secure_cookies (set for HTTPS base URLs)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: exp, HttpOnly: true,
		Secure: s.app.Config.Get().Server.SecureCookies, SameSite: http.SameSiteLaxMode})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	// #nosec G124 -- deletes the cookie
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

// ssoInbound reports whether path receives messages of an identity
// provider (/api/v1/auth/sso/<provider>/callback or /logout).
func ssoInbound(path string) bool {
	rest, ok := strings.CutPrefix(path, "/api/v1/auth/sso/")
	if !ok {
		return false
	}
	_, end, ok := strings.Cut(rest, "/")
	return ok && (end == "callback" || end == "logout")
}

// noInvalidate lists write requests that do not change what guests see.
func noInvalidate(p string) bool {
	for _, x := range []string{"/api/v1/auth/", "/api/v1/me/password", "/api/v1/me/api-keys", "/api/v1/me/ssh-keys", "/api/v1/me/feed-token",
		"/api/v1/me/watches", "/api/v1/me/notifications", "/api/v1/me/inbox", "/api/v1/elements/preview", "/api/v1/files"} {
		if strings.HasPrefix(p, x) {
			return true
		}
	}
	return false
}

// bigBodies limits large request bodies processed at the same time.
var bigBodies = make(chan struct{}, 16)

// bodyLimit returns the largest body of an API request after the caller is
// known (0: the general limit server.max_body_mb applies).
func bodyLimit(cfg *config.Config, guest bool, r *http.Request) int64 {
	if guest {
		if strings.HasPrefix(r.URL.Path, "/api/v1/auth/sso/") {
			return 1 << 20 // SAML responses with certificates
		}
		return 64 << 10
	}
	if strings.HasSuffix(r.URL.Path, "/import") {
		return 0 // packages and translation files (own limits)
	}
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if ct == "" || strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ct, "text/") {
		kb := int64(cfg.Server.MaxJSONKB)
		if kb <= 0 {
			kb = 4096
		}
		return kb << 10
	}
	return 0
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// pagingParams refuses negative or malformed limit, offset and page
// parameters of API requests (400 instead of a database error).
func pagingParams(r *http.Request) error {
	q := r.URL.Query()
	for _, k := range [...]string{"limit", "offset", "page", "per_page"} {
		for _, v := range q[k] {
			if v == "" {
				continue
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return apperr.BadRequest("%s: a number from 0", k)
			}
		}
	}
	return nil
}
