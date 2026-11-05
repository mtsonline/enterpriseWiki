package server

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
)

// Instance serves all sites of an instance on one listener; the host name
// of a request selects the site (unknown hosts: site "main").
type Instance struct {
	in      *app.Instance
	web     fs.FS
	pm      *pluginmgr.Manager
	mu      sync.RWMutex
	servers map[string]*Server
	// Background starts the background work of a site (bus, jobs); set
	// before Run. ctx ends when the site is closed or the server stops.
	Background func(ctx context.Context, a *app.App)
	ctx        context.Context
}

// NewInstance creates the servers of all open sites.
func NewInstance(in *app.Instance, web fs.FS, pm *pluginmgr.Manager) *Instance {
	si := &Instance{in: in, web: web, pm: pm, servers: map[string]*Server{}}
	for _, a := range in.Sites() {
		si.servers[a.Site] = New(a, web, pm)
	}
	in.OnSite = si.add
	return si
}

// add creates the server of a site opened while running.
func (si *Instance) add(a *app.App) {
	s := New(a, si.web, si.pm)
	si.mu.Lock()
	si.servers[a.Site] = s
	ctx := si.ctx
	si.mu.Unlock()
	if ctx != nil {
		si.start(ctx, s)
	}
}

func (si *Instance) start(ctx context.Context, s *Server) {
	sctx, cancel := context.WithCancel(ctx)
	s.app.SetStop(func() {
		cancel()
		si.mu.Lock()
		delete(si.servers, s.app.Site)
		si.mu.Unlock()
	})
	go s.app.Cache.Poll(sctx)
	if si.Background != nil {
		si.Background(sctx, s.app)
	}
}

// Server returns the server of a site.
func (si *Instance) Server(site string) *Server {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return si.servers[site]
}

// Handler dispatches by host name.
func (si *Instance) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := si.in.ForHost(r.Context(), r.Host)
		var s *Server
		if a != nil {
			s = si.Server(a.Site)
		}
		if s == nil {
			http.Error(w, "site not available", http.StatusServiceUnavailable)
			return
		}
		s.handler.ServeHTTP(w, r)
	})
}

// Run starts the background work of all sites and serves until ctx is
// cancelled, then shuts down gracefully.
func (si *Instance) Run(ctx context.Context) error {
	si.mu.Lock()
	si.ctx = ctx
	list := make([]*Server, 0, len(si.servers))
	for _, s := range si.servers {
		list = append(list, s)
	}
	si.mu.Unlock()
	for _, s := range list {
		si.start(ctx, s)
	}
	srv := &http.Server{
		Addr:              si.in.Config.Get().Server.Listen,
		Handler:           si.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	errc := make(chan error, 1)
	go func() {
		si.in.Log.Info("listening", "addr", srv.Addr, "sites", len(list))
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	si.in.Log.Info("shutting down")
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
