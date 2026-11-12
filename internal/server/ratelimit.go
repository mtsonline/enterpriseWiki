package server

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/limits"
)

// Limits of API requests per client (S-04): a token bucket per account
// (signed in) or per address (guests), and a stricter one for expensive
// requests (search, previews, exports, uploads). Behind a reverse proxy
// the address comes from server.trusted_proxies.

type bucket struct {
	tokens float64
	last   time.Time
}

type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	sweep   time.Time
}

func newRateLimiter() *limiter { return &limiter{buckets: map[string]*bucket{}} }

// take removes one token of key; rate per second, burst tokens at most.
// It returns how long to wait when no token is left.
func (l *limiter) take(key string, rate float64, burst int, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.sweep) > time.Minute || len(l.buckets) > 200000 {
		// forget full buckets (memory)
		for k, b := range l.buckets {
			if b.tokens+now.Sub(b.last).Seconds()*rate >= float64(burst) || len(l.buckets) > 200000 {
				delete(l.buckets, k)
			}
		}
		l.sweep = now
	}
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: float64(burst), last: now}
		l.buckets[key] = b
	}
	b.tokens = min(float64(burst), b.tokens+now.Sub(b.last).Seconds()*rate)
	b.last = now
	if b.tokens < 1 {
		return false, time.Duration((1 - b.tokens) / rate * float64(time.Second))
	}
	b.tokens--
	return true, 0
}

// expensive reports requests with their own, stricter limit.
func expensive(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/api/v1/search"), strings.HasSuffix(p, "/preview"),
		strings.Contains(p, "/export"), strings.HasSuffix(p, ".csv"), strings.HasSuffix(p, ".xlsx"):
		return true
	case r.Method == http.MethodPost && (p == "/api/v1/files" || strings.HasSuffix(p, "/media") || strings.HasSuffix(p, "/versions")):
		return true
	}
	return false
}

// rateLimited applies the limits of the setting server.rate_limit; true:
// the answer 429 is written.
func (s *Server) rateLimited(w http.ResponseWriter, r *http.Request, user int64, cfg *config.Config) bool {
	rl := cfg.Server.RateLimit
	if rl.Disabled || !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	key := "ip:" + s.clientIP(r)
	rate, burst := float64(limits.Value(cfg, limits.RateGuest)), int(limits.Value(cfg, limits.RateGuestBurst))
	if user > 0 {
		key = "u:" + strconv.FormatInt(user, 10)
		rate, burst = float64(limits.Value(cfg, limits.RateUser)), int(limits.Value(cfg, limits.RateUserBurst))
	}
	now := time.Now()
	ok, wait := s.limits.take(key, rate, burst, now)
	if ok && expensive(r) {
		ok, wait = s.limits.take("x"+key, float64(limits.Value(cfg, limits.RateExpensive)), int(limits.Value(cfg, limits.RateExpensiveBurst)), now)
	}
	if ok {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(wait/time.Second)+1))
	httpx.WriteError(w, r, apperr.TooMany("too many requests, try again shortly"))
	return true
}
