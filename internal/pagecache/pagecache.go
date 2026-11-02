// Package pagecache is the optional cache for guests: GET API responses of
// anonymous visitors are kept in memory and the page HTML embeds the API
// responses that page needed before ("preload"), so a returning page view
// is served with one request and without database access.
//
// Every change invalidates the whole cache (write requests, content hooks,
// user/group/permission changes). Macros make finer invalidation unsafe: a
// page may show a list of other pages, events or members. A generation
// counter in the database (polled every few seconds, independent of
// traffic) propagates invalidations between cluster nodes and from CLI
// processes. A TTL bounds time-dependent content (e.g. "upcoming events").
package pagecache

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// MaxBody is the largest response that is cached.
const MaxBody = 2 << 20

// MaxPreload is the largest preload block embedded into a page.
const MaxPreload = 3 << 20

type entry struct {
	key    string
	url    string
	header http.Header
	body   []byte
	at     time.Time
	el     *list.Element
}

// Cache is the guest cache.
type Cache struct {
	pool *pgxpool.Pool
	cfg  *config.Store
	log  *slog.Logger

	mu      sync.Mutex
	entries map[string]*entry
	lru     *list.List
	bytes   int64
	pages   map[string]map[string]bool // page (path?query) → API keys requested from it
	gen     uint64                     // local generation (bumped on every clear)
	dbGen   int64

	hits, misses, stores, clears atomic.Int64
}

// New creates the cache.
func New(pool *pgxpool.Pool, cfg *config.Store, log *slog.Logger) *Cache {
	c := &Cache{pool: pool, cfg: cfg, log: log}
	c.reset()
	return c
}

func (c *Cache) reset() {
	c.entries = map[string]*entry{}
	c.lru = list.New()
	c.pages = map[string]map[string]bool{}
	c.bytes = 0
	c.gen++
}

func (c *Cache) settings() config.Cache { return c.cfg.Get().Cache }

// Enabled reports whether the cache is switched on.
func (c *Cache) Enabled() bool { return c.settings().Enabled }

// Invalidate clears the cache here and (through the database) everywhere.
func (c *Cache) Invalidate(ctx context.Context, reason string) {
	if !c.Enabled() {
		return
	}
	c.clearLocal()
	if c.pool == nil {
		return
	}
	var g int64
	if err := c.pool.QueryRow(context.WithoutCancel(ctx), `UPDATE cache_state SET generation = generation + 1 WHERE id = 1 RETURNING generation`).Scan(&g); err != nil {
		c.log.Warn("cache: could not publish invalidation", "reason", reason, "err", err)
		return
	}
	c.mu.Lock()
	if g > c.dbGen {
		c.dbGen = g
	}
	c.mu.Unlock()
}

func (c *Cache) clearLocal() {
	c.mu.Lock()
	c.reset()
	c.mu.Unlock()
	c.clears.Add(1)
}

// Poll watches the database generation until ctx ends (server only).
func (c *Cache) Poll(ctx context.Context) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !c.Enabled() || c.pool == nil {
			continue
		}
		var g int64
		if err := c.pool.QueryRow(ctx, `SELECT generation FROM cache_state WHERE id = 1`).Scan(&g); err != nil {
			continue
		}
		c.mu.Lock()
		changed := g != c.dbGen
		c.dbGen = g
		c.mu.Unlock()
		if changed {
			c.clearLocal()
		}
	}
}

// Status describes the cache.
type Status struct {
	Enabled    bool    `json:"enabled"`
	TTLSeconds int     `json:"ttl_seconds"`
	Entries    int     `json:"entries"`
	Pages      int     `json:"pages"`
	Bytes      int64   `json:"bytes"`
	MaxEntries int     `json:"max_entries"`
	MaxBytes   int64   `json:"max_bytes"`
	Hits       int64   `json:"hits"`
	Misses     int64   `json:"misses"`
	HitRate    float64 `json:"hit_rate"`
	Stores     int64   `json:"stores"`
	Clears     int64   `json:"clears"`
	Generation int64   `json:"generation"`
}

// GetStatus returns statistics of this process.
func (c *Cache) GetStatus() Status {
	s := c.settings()
	c.mu.Lock()
	st := Status{Enabled: s.Enabled, TTLSeconds: s.TTLSeconds, Entries: len(c.entries), Pages: len(c.pages), Bytes: c.bytes,
		MaxEntries: s.MaxEntries, MaxBytes: int64(s.MaxMB) << 20, Generation: c.dbGen}
	c.mu.Unlock()
	st.Hits, st.Misses, st.Stores, st.Clears = c.hits.Load(), c.misses.Load(), c.stores.Load(), c.clears.Load()
	if n := st.Hits + st.Misses; n > 0 {
		st.HitRate = float64(st.Hits) / float64(n)
	}
	return st
}

// cacheable reports whether a guest GET to this API URL may be cached.
func cacheable(r *http.Request) bool {
	p := r.URL.Path
	if r.Method != http.MethodGet || !strings.HasPrefix(p, "/api/v1/") || r.URL.Query().Has("token") {
		return false
	}
	for _, x := range []string{"/api/v1/auth/", "/api/v1/me", "/api/v1/admin/", "/api/v1/files/", "/api/v1/health"} {
		if strings.HasPrefix(p, x) {
			return false
		}
	}
	return !strings.Contains(p, "/me/")
}

var cacheTypes = []string{"application/json", "application/atom+xml", "application/rss+xml", "text/calendar", "text/xml", "application/xml"}

func storable(h http.Header) bool {
	if h.Get("Set-Cookie") != "" || h.Get("Content-Encoding") != "" {
		return false
	}
	ct := h.Get("Content-Type")
	for _, t := range cacheTypes {
		if strings.HasPrefix(ct, t) {
			return true
		}
	}
	return false
}

func keyOf(r *http.Request) string {
	return r.URL.Path + "?" + r.URL.RawQuery + "|" + r.Header.Get("Accept-Language")
}

// pageOf returns the page (path?query) that issued an API request.
func pageOf(r *http.Request) string {
	ref := r.Header.Get("Referer")
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || (u.Host != "" && u.Host != r.Host) {
		return ""
	}
	return u.Path + "?" + u.RawQuery
}

func (c *Cache) fresh(e *entry) bool {
	ttl := c.settings().TTLSeconds
	return ttl <= 0 || time.Since(e.at) < time.Duration(ttl)*time.Second
}

// recorder buffers a response for the cache while passing it through.
type recorder struct {
	http.ResponseWriter
	status int
	buf    bytes.Buffer
	over   bool
	header http.Header
}

func (w *recorder) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.header = w.ResponseWriter.Header().Clone()
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *recorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if !w.over {
		if w.buf.Len()+len(b) > MaxBody {
			w.over = true
			w.buf.Reset()
		} else {
			w.buf.Write(b)
		}
	}
	return w.ResponseWriter.Write(b)
}

// Flush lets streaming handlers flush.
func (w *recorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ServeAPI answers a guest GET API request from the cache or stores the
// response of next. It returns false when the request is not cacheable
// (next has not been called then).
func (c *Cache) ServeAPI(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
	if !c.Enabled() || !cacheable(r) {
		return false
	}
	key := keyOf(r)
	page := pageOf(r)
	c.mu.Lock()
	e := c.entries[key]
	if e != nil && !c.fresh(e) {
		c.remove(e)
		e = nil
	}
	if e != nil {
		c.lru.MoveToFront(e.el)
		c.notePage(page, key)
	}
	gen := c.gen
	c.mu.Unlock()
	if e != nil {
		c.hits.Add(1)
		h := w.Header()
		for k, v := range e.header {
			h[k] = v
		}
		h.Set("X-Cache", "HIT")
		if et := e.header.Get("ETag"); et != "" && strings.Contains(r.Header.Get("If-None-Match"), et) {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(e.body)
		return true
	}
	c.misses.Add(1)
	w.Header().Set("X-Cache", "MISS")
	rec := &recorder{ResponseWriter: w}
	next.ServeHTTP(rec, r)
	if rec.status != http.StatusOK || rec.over || !storable(rec.header) {
		return true
	}
	hdr := http.Header{}
	for _, k := range []string{"Content-Type", "ETag", "Last-Modified", "Content-Disposition", "Cache-Control"} {
		if v := rec.header.Get(k); v != "" {
			hdr.Set(k, v)
		}
	}
	c.store(gen, key, r.URL.Path+"?"+r.URL.RawQuery, page, hdr, append([]byte(nil), rec.buf.Bytes()...))
	return true
}

func (c *Cache) store(gen uint64, key, u, page string, hdr http.Header, body []byte) {
	s := c.settings()
	maxEntries := s.MaxEntries
	maxBytes := int64(s.MaxMB) << 20
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return // the cache was cleared while the response was built: it may be stale
	}
	if old := c.entries[key]; old != nil {
		c.remove(old)
	}
	e := &entry{key: key, url: u, header: hdr, body: body, at: time.Now()}
	e.el = c.lru.PushFront(e)
	c.entries[key] = e
	c.bytes += int64(len(body))
	c.notePage(page, key)
	for (maxEntries > 0 && len(c.entries) > maxEntries) || (maxBytes > 0 && c.bytes > maxBytes) {
		last := c.lru.Back()
		if last == nil {
			break
		}
		c.remove(last.Value.(*entry))
	}
	c.stores.Add(1)
}

func (c *Cache) remove(e *entry) {
	c.lru.Remove(e.el)
	delete(c.entries, e.key)
	c.bytes -= int64(len(e.body))
}

func (c *Cache) notePage(page, key string) {
	if page == "" {
		return
	}
	m := c.pages[page]
	if m == nil {
		if len(c.pages) > 20000 {
			return
		}
		m = map[string]bool{}
		c.pages[page] = m
	}
	if len(m) < 64 {
		m[key] = true
	}
}

// Preload returns the JSON responses a page needed before (for embedding
// into the page HTML), or nil.
func (c *Cache) Preload(r *http.Request) []byte {
	if !c.Enabled() || r.Method != http.MethodGet {
		return nil
	}
	page := r.URL.Path + "?" + r.URL.RawQuery
	lang := r.Header.Get("Accept-Language")
	out := map[string]json.RawMessage{}
	size := 0
	c.mu.Lock()
	for key := range c.pages[page] {
		e := c.entries[key]
		if e == nil || !c.fresh(e) || !strings.HasSuffix(key, "|"+lang) || !strings.HasPrefix(e.header.Get("Content-Type"), "application/json") {
			continue
		}
		if size+len(e.body) > MaxPreload {
			break
		}
		size += len(e.body)
		out[strings.TrimSuffix(e.url, "?")] = e.body
	}
	c.mu.Unlock()
	if len(out) == 0 {
		return nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	// safe inside <script type="application/json">
	return bytes.ReplaceAll(b, []byte("</"), []byte(`<\/`))
}
