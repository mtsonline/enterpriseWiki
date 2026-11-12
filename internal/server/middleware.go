package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/auth"
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

// clientIP returns the address of the caller. Behind reverse proxies it
// is taken from the header of server.client_ip_header - only when the
// direct peer is one of server.trusted_proxies: anybody else could forge
// the header (see noteUntrustedForward).
func (s *Server) clientIP(r *http.Request) string {
	host := s.peerIP(r)
	c := s.app.Config.Get().Server
	if len(c.TrustedProxies) == 0 || !isTrusted(c.TrustedProxies, host) {
		return host
	}
	return forwardedClient(r.Header, c.ClientIPHeader, c.TrustedProxies, host)
}

// forwardedClient reads the client address from the forwarding header kind
// ("x-forwarded-for", the default; "forwarded" (RFC 7239); or the name of a
// header with one address such as X-Real-IP or CF-Connecting-IP). Lists
// are read from the right: the first address that is not a trusted proxy
// is the client (addresses further left were sent by the client and could
// be forged). Without a usable value the peer counts.
func forwardedClient(hd http.Header, kind string, trusted []string, peer string) string {
	var hops []string
	switch k := strings.ToLower(strings.TrimSpace(kind)); k {
	case "", "x-forwarded-for":
		hops = strings.Split(strings.Join(hd.Values("X-Forwarded-For"), ","), ",")
	case "forwarded":
		for _, el := range strings.Split(strings.Join(hd.Values("Forwarded"), ","), ",") {
			hop := ""
			for _, pair := range strings.Split(el, ";") {
				if name, v, ok := strings.Cut(strings.TrimSpace(pair), "="); ok && strings.EqualFold(name, "for") {
					hop = forwardedNode(v)
				}
			}
			hops = append(hops, hop) // an element without "for" ends the chain
		}
	default:
		if v := strings.TrimSpace(hd.Get(k)); net.ParseIP(v) != nil {
			return v
		}
		return peer
	}
	host := peer
	for i := len(hops) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(hops[i])
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

// forwardedNode returns the address of a "for" value of Forwarded:
// 192.0.2.1, "192.0.2.1:4711", "[2001:db8::1]:4711"; empty for names
// (unknown, _hidden).
func forwardedNode(v string) string {
	v = strings.Trim(strings.TrimSpace(v), `"`)
	if strings.HasPrefix(v, "[") {
		if end := strings.Index(v, "]"); end > 0 {
			return v[1:end]
		}
		return ""
	}
	if net.ParseIP(v) != nil {
		return v
	}
	if h, _, err := net.SplitHostPort(v); err == nil {
		return h
	}
	return ""
}

// forwardHeader is the name of the configured forwarding header.
func forwardHeader(kind string) string {
	switch k := strings.ToLower(strings.TrimSpace(kind)); k {
	case "", "x-forwarded-for":
		return "X-Forwarded-For"
	default:
		return http.CanonicalHeaderKey(k)
	}
}

// untrustedForwards remembers peers that sent the forwarding header
// without being a trusted proxy: most often a reverse proxy that is
// missing in server.trusted_proxies - then all its visitors share one
// address (limits of requests, sign-in protection, audit log). Few
// entries and one audit record per peer and day: anybody can send the
// header, it must not flood anything.
type untrustedForwards struct {
	mu    sync.Mutex
	peers map[string]*UntrustedForward
}

// UntrustedForward is one such peer.
type UntrustedForward struct {
	Peer     string    `json:"peer"`
	Header   string    `json:"header"`
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
	Requests int64     `json:"requests"`
	audited  time.Time
}

const untrustedForwardPeers = 20

// note counts a request of peer; true: write an audit record now.
func (u *untrustedForwards) note(peer, header string, now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	e := u.peers[peer]
	if e == nil {
		if len(u.peers) >= untrustedForwardPeers {
			var oldest string
			for k, v := range u.peers {
				if now.Sub(v.Last) > 24*time.Hour && (oldest == "" || v.Last.Before(u.peers[oldest].Last)) {
					oldest = k
				}
			}
			if oldest == "" {
				return false // full: the known ones are shown, nothing more is recorded
			}
			delete(u.peers, oldest)
		}
		if u.peers == nil {
			u.peers = map[string]*UntrustedForward{}
		}
		e = &UntrustedForward{Peer: peer, Header: header, First: now}
		u.peers[peer] = e
	}
	e.Last, e.Header = now, header
	e.Requests++
	if now.Sub(e.audited) < 24*time.Hour {
		return false
	}
	e.audited = now
	return true
}

func (u *untrustedForwards) list() []UntrustedForward {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]UntrustedForward, 0, len(u.peers))
	for _, e := range u.peers {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Requests > out[j].Requests })
	return out
}

// noteUntrustedForward records a request with the forwarding header from
// a peer that is not a trusted proxy (the header is ignored): warning in
// the log, in the expert settings and - once per peer and day - in the
// audit log.
func (s *Server) noteUntrustedForward(r *http.Request) {
	c := s.app.Config.Get().Server
	name := forwardHeader(c.ClientIPHeader)
	if r.Header.Get(name) == "" {
		return
	}
	peer := s.peerIP(r)
	if isTrusted(c.TrustedProxies, peer) {
		return
	}
	if !s.forwards.note(peer, name, time.Now()) {
		return
	}
	s.log.Warn("forwarding header from an address that is not a trusted proxy: ignored (server.trusted_proxies)", "peer", peer, "header", name)
	ctx := identity.WithSite(context.Background(), s.app.Site) // the system reports, not the visitor
	go func() {
		if err := audit.Log(ctx, s.app.DB, "server.proxy_untrusted", "server", 0, nil, map[string]any{"peer": peer, "header": name}); err != nil {
			s.log.Warn("audit: untrusted proxy", "err", err)
		}
	}()
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
		s.noteUntrustedForward(r)
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
		// S-02: the size of the body depends on the caller and the route
		// (expert settings), large bodies wait for one of a few places
		release, err := s.limitBody(sw, r)
		if err != nil {
			httpx.WriteError(sw, r, err)
			return
		}
		defer release()
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
	if _, pattern := s.app.Mux.Handler(r); pattern != "" && httpx.Signed(pattern) {
		// messages of other systems (incoming webhooks of plugins) are
		// authenticated by the handler with their signature; a session or
		// API key never counts there
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
