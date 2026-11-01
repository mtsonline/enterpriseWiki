package content

import (
	"context"
	"sync"
)

// SearchQuery is a full text query for a search backend.
type SearchQuery struct {
	Q      string
	Tags   []string
	Lang   string // preferred language
	Limit  int
	Offset int
}

// BackendHit is a document found by a search backend. Snippet and Title are
// plain text with matches between \x01 and \x02 (escaped by the core).
type BackendHit struct {
	ElementID int64
	Lang      string
	Title     string
	Snippet   string
	Score     float64
}

// SearchBackend is an external full text engine (OpenSearch, Meilisearch,
// ...) provided by a plugin. The PostgreSQL index stays the source of truth;
// backends follow the search_changes log. Permissions are always checked by
// the core after the search.
type SearchBackend interface {
	Name() string
	// Ready reports whether the backend is configured and fully indexed.
	Ready(ctx context.Context) bool
	Search(ctx context.Context, q SearchQuery) ([]BackendHit, error)
}

type backendRegistry struct {
	mu       sync.RWMutex
	pluginID string
	b        SearchBackend
}

// RegisterSearchBackend sets the external search backend of a plugin. Only
// one backend is used; the last registered active one wins.
func (s *Service) RegisterSearchBackend(pluginID string, b SearchBackend) {
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()
	s.backend.pluginID, s.backend.b = pluginID, b
}

// searchBackend returns the active, ready backend or nil.
func (s *Service) searchBackend(ctx context.Context) SearchBackend {
	s.backend.mu.RLock()
	id, b := s.backend.pluginID, s.backend.b
	s.backend.mu.RUnlock()
	if b == nil || !s.enabled(id) || !b.Ready(ctx) {
		return nil
	}
	return b
}
