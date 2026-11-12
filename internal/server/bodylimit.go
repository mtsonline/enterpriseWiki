package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
)

func init() {
	// uploads are streamed to the storage with their own limit (storage.max_upload_mb)
	for _, p := range []string{"POST /api/v1/files", "POST /api/v1/media", "POST /api/v1/media/{id}/file"} {
		limits.RouteBody(p, limits.RequestUpload)
	}
	for _, p := range []string{"POST /api/v1/categories/import", "POST /api/v1/admin/i18n/{lang}/import", "PUT /api/v1/admin/i18n/{lang}"} {
		limits.RouteBody(p, limits.RequestImportMB)
	}
	for _, p := range []string{"POST /api/v1/elements", "PUT /api/v1/elements/{id}", "POST /api/v1/elements/preview", "POST /api/v1/sources/preview",
		"POST /api/v1/drafts", "PUT /api/v1/drafts/{id}", "POST /api/v1/drafts/{id}/publish", "POST /api/v1/templates", "PUT /api/v1/templates/{id}"} {
		limits.RouteBody(p, limits.RequestContentKB)
	}
}

// largeGate limits how many large request bodies are in memory at once
// (one gate for the process: all sites share its memory).
var largeGate = &gate{ch: make(chan struct{})}

type gate struct {
	mu    sync.Mutex
	inUse int
	ch    chan struct{} // closed and replaced whenever a place becomes free
}

// acquire takes a place (at most limit at once) or gives up after wait.
func (g *gate) acquire(ctx context.Context, limit int, wait time.Duration) bool {
	t := time.NewTimer(wait)
	defer t.Stop()
	for {
		g.mu.Lock()
		if g.inUse < limit {
			g.inUse++
			g.mu.Unlock()
			return true
		}
		ch := g.ch
		g.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return false
		case <-t.C:
			return false
		}
	}
}

func (g *gate) release() {
	g.mu.Lock()
	g.inUse--
	close(g.ch)
	g.ch = make(chan struct{})
	g.mu.Unlock()
}

// limitBody applies the size limit of the request body: the limit the route
// declared (limits.RouteBody), otherwise the one for guests or for signed-in
// callers. Large bodies additionally need one of the few places for large
// requests. release must be called when the request is done.
func (s *Server) limitBody(w http.ResponseWriter, r *http.Request) (release func(), err error) {
	release = func() {}
	if r.Body == nil || r.Body == http.NoBody || !strings.HasPrefix(r.URL.Path, "/api/") {
		return release, nil
	}
	p := identity.From(r.Context())
	if p.InProcess() {
		return release, nil // command line of the server itself
	}
	cfg := s.app.Config.Get()
	_, pattern := s.app.Mux.Handler(r)
	var max int64
	switch key := limits.BodyKey(pattern); {
	case key == limits.RequestUpload:
		return release, nil
	case key == limits.RequestImportMB && cfg.Limits[key] == 0 && cfg.Server.MaxBodyMB > 0:
		max = int64(cfg.Server.MaxBodyMB) << 20 // the older setting server.max_body_mb
	case key != "":
		max = limits.Bytes(cfg, key)
	case s.app.Media.TokenPath(r.URL.Path):
		max = s.app.Files.MaxBytes() + 1<<20 // an office server stores a file
	case ssoInbound(r.URL.Path):
		max = limits.Bytes(cfg, limits.RequestSSOKB)
	case p == nil:
		max = limits.Bytes(cfg, limits.RequestGuestKB)
	default:
		max = limits.Bytes(cfg, limits.RequestBodyKB)
	}
	if r.ContentLength > max {
		return release, apperr.TooLarge("request body too large (%d kB, limit %d kB, see Administration → Expert settings)", r.ContentLength>>10, max>>10)
	}
	r.Body = http.MaxBytesReader(w, r.Body, max)
	if r.ContentLength > limits.LargeBody || (r.ContentLength < 0 && max > limits.LargeBody) {
		wait := time.Duration(limits.Value(cfg, limits.LargeWaitSeconds)) * time.Second
		if !largeGate.acquire(r.Context(), int(limits.Value(cfg, limits.LargeParallel)), wait) {
			w.Header().Set("Retry-After", strconv.Itoa(5))
			return release, apperr.Busy("too many large requests at the same time; nothing was stored, please repeat")
		}
		release = largeGate.release
	}
	return release, nil
}
