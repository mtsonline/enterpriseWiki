// Package server exposes the REST API (/api/v1) and serves the embedded
// frontend. The frontend uses exactly the same API as external clients.
package server

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/collab"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
)

// Server is the HTTP server.
type Server struct {
	app     *app.App
	log     *slog.Logger
	web     fs.FS
	plugins *pluginmgr.Manager
	handler http.Handler
	started time.Time
	legacy  legacyRefs // file hashes in element content (GET /api/v1/files)
	// live editing: the relay and its end (Close: shutdown)
	collab     *collab.Hub
	collabDone context.Context
	closeLive  context.CancelFunc
}

// New creates the server. web contains the built frontend (may be nil).
func New(a *app.App, web fs.FS, pm *pluginmgr.Manager) *Server {
	s := &Server{app: a, log: a.Log, web: web, plugins: pm, started: time.Now(), collab: collab.NewHub()}
	s.collabDone, s.closeLive = context.WithCancel(context.Background())
	s.routes()
	a.Mux.HandleFunc("/", s.serveFrontend)
	s.handler = s.wrap(a.Mux)
	return s
}

// Close ends the long-lived connections (live editing); http.Server.Shutdown
// does not wait for them.
func (s *Server) Close() { s.closeLive() }

// Handler returns the root handler.
func (s *Server) Handler() http.Handler { return s.handler }

// Run serves until ctx is cancelled, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.app.Config.Get().Server.Listen,
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	errc := make(chan error, 1)
	go s.app.Cache.Poll(ctx)
	go func() {
		s.log.Info("listening", "addr", srv.Addr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.log.Info("shutting down")
	s.Close()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// serveFrontend serves static files and falls back to index.html for SPA routes.
func (s *Server) serveFrontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.Error(w, `{"error":{"code":"not_found","message":"no such endpoint"}}`, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.web == nil {
		http.Error(w, "frontend not built (run scripts/build.sh)", http.StatusNotFound)
		return
	}
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p != "" && p != "index.html" {
		if st, err := fs.Stat(s.web, p); err == nil && !st.IsDir() {
			if strings.HasPrefix(p, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFileFS(w, r, s.web, p) // #nosec G703 -- embedded frontend (fs.FS cannot leave its root), path cleaned
			return
		}
		if strings.HasPrefix(p, "assets/") {
			http.NotFound(w, r)
			return
		}
	}
	// former readable paths and short URLs: permanent redirect to the current address
	if p != "" && !reservedPaths[strings.SplitN(p, "/", 2)[0]] && !strings.Contains(p, ".") {
		if res, err := s.app.Content.Resolve(r.Context(), p); err == nil && res.Redirect && res.Path != "" && res.Path != "/"+p {
			target := res.Path
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, auth.SafeNext(target), http.StatusMovedPermanently) // #nosec G710 -- SafeNext allows local paths only
			return
		}
	}
	b, err := fs.ReadFile(s.web, "index.html")
	if err != nil {
		http.Error(w, "frontend not built (run scripts/build.sh)", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if identity.From(r.Context()) == nil {
		// guests: the content as plain HTML (search engines, no JavaScript)
		if c := s.app.Config.Get(); c.UI.Prerender {
			if p := s.prerenderCached(r); p != nil {
				b = injectPrerender(b, p, c.UI.SiteName)
				w.Header().Set("Content-Language", p.Lang)
			}
		}
		// guests: embed the API responses this page needed before
		if pre := s.app.Cache.Preload(r); pre != nil {
			b = bytes.Replace(b, []byte("</head>"), append(append([]byte(`<script type="application/json" id="ew-preload">`), pre...), []byte("</script></head>")...), 1)
			w.Header().Set("X-Cache", "PRELOAD")
		}
	}
	_, _ = w.Write(b)
}
