package identity

import (
	"context"
	"sync"
	"sync/atomic"
)

// Scope identifies one opened app (one site in one process). Requests,
// jobs and commands carry the scope of their app in the context, so
// process-wide registries find the instance of exactly that app, also when
// a process has two apps of the same site open (tests, a command line
// inside a server process).
type Scope uint64

var lastScope atomic.Uint64

// NewScope returns a scope no other app of this process has.
func NewScope() Scope { return Scope(lastScope.Add(1)) }

type scopeKey struct{}

// WithScope stores the scope of the app in ctx.
func WithScope(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, s)
}

// ScopeOf returns the scope of ctx (0 if none is set).
func ScopeOf(ctx context.Context) Scope {
	s, _ := ctx.Value(scopeKey{}).(Scope)
	return s
}

// PerScope keeps one value per app for process-wide state (registries of
// plugins, filters). A value is found by the scope of the context; a
// context without a known scope gets the value of the newest open app of
// its site. Remove takes the value of a closed app away, so a closed app
// never shadows an open one.
type PerScope[T any] struct {
	mu      sync.RWMutex
	entries []scopeEntry[T] // in the order of Set
}

type scopeEntry[T any] struct {
	scope Scope
	site  string
	v     T
}

// Set stores the value of the app with scope s on site.
func (p *PerScope[T]) Set(s Scope, site string, v T) {
	if site == "" {
		site = "main"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, e := range p.entries {
		if e.scope == s {
			p.entries = append(p.entries[:i], p.entries[i+1:]...)
			break
		}
	}
	p.entries = append(p.entries, scopeEntry[T]{scope: s, site: site, v: v})
}

// Remove takes the value of the app with scope s away (when it closes).
func (p *PerScope[T]) Remove(s Scope) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, e := range p.entries {
		if e.scope == s {
			p.entries = append(p.entries[:i], p.entries[i+1:]...)
			return
		}
	}
}

// Get returns the value for ctx and whether there is one.
func (p *PerScope[T]) Get(ctx context.Context) (T, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if s := ScopeOf(ctx); s != 0 {
		for _, e := range p.entries {
			if e.scope == s {
				return e.v, true
			}
		}
	}
	site := Site(ctx)
	for i := len(p.entries) - 1; i >= 0; i-- {
		if p.entries[i].site == site {
			return p.entries[i].v, true
		}
	}
	var zero T
	return zero, false
}
