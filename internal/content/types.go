// Package content implements categories, elements, revisions, translations,
// trash, rollback, tags and full text search.
package content

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// RenderContext is passed to element types when rendering.
type RenderContext struct {
	ElementID  int64
	CategoryID int64
	Lang       string
	Principal  *identity.Principal
	// Service gives renderers (e.g. macros listing child pages) read access
	// with the permissions of the viewer.
	Service *Service
}

// ElementType is implemented by element type plugins (core.html, ...).
//
// Types can build on each other with Go struct embedding: a plugin embeds the
// base type and overrides only the methods it wants to change (see
// plugins/example_notice).
type ElementType interface {
	// TypeID is the unique id stored with each element, e.g. "core.html".
	TypeID() string
	// Labels returns the human readable name per language.
	Labels() map[string]string
	// Normalize validates and sanitizes a body before it is stored.
	Normalize(body json.RawMessage) (json.RawMessage, error)
	// Render returns safe HTML for displaying the body.
	Render(ctx context.Context, rc RenderContext, body json.RawMessage) (string, error)
	// Text extracts plain text for the full text index.
	Text(body json.RawMessage) string
	// EmptyBody returns the body of a new element.
	EmptyBody() json.RawMessage
}

// Preparer is implemented by element types that complete a body before it
// is validated, with the permissions of the caller: e.g. the blog creates a
// category for its posts when none was chosen. categoryID is the category
// of the element.
type Preparer interface {
	Prepare(ctx context.Context, svc *Service, categoryID int64, lang, title string, body json.RawMessage) (json.RawMessage, error)
}

// Guard is implemented by element types with own permissions (declared in
// the plugin.json of their plugin), in addition to the category permissions
// create, edit and delete.
type Guard interface {
	// CheckCreate is called before an element of the type is created or
	// duplicated into categoryID.
	CheckCreate(ctx context.Context, svc *Service, categoryID int64) error
	// CheckChange is called before a revision is saved (newBody set) or the
	// element is deleted or moved away (newBody nil). oldBody is the latest
	// body of the language (nil for a new translation).
	CheckChange(ctx context.Context, svc *Service, el *Element, oldBody, newBody json.RawMessage) error
}

// TypeInfo describes a registered element type.
type TypeInfo struct {
	ID       string            `json:"id"`
	PluginID string            `json:"plugin"`
	Labels   map[string]string `json:"labels"`
	Empty    json.RawMessage   `json:"empty_body"`
}

type typeRegistry struct {
	mu    sync.RWMutex
	types map[string]registeredType
}

type registeredType struct {
	pluginID string
	t        ElementType
}

// RegisterType registers an element type owned by pluginID.
func (s *Service) RegisterType(pluginID string, t ElementType) {
	s.types.mu.Lock()
	defer s.types.mu.Unlock()
	if s.types.types == nil {
		s.types.types = map[string]registeredType{}
	}
	s.types.types[t.TypeID()] = registeredType{pluginID, t}
}

// Type returns the active element type id (nil if unknown or its plugin is disabled).
func (s *Service) Type(id string) ElementType {
	s.types.mu.RLock()
	defer s.types.mu.RUnlock()
	r, ok := s.types.types[id]
	if !ok || !s.enabled(r.pluginID) {
		return nil
	}
	return r.t
}

// Listing is implemented by element types that show other elements (blog,
// FAQ, overviews); lists of several sources can leave them out.
type Listing interface{ ListsElements() bool }

// ListingTypes returns the ids of the registered types that list other elements.
func (s *Service) ListingTypes() []string {
	s.types.mu.RLock()
	defer s.types.mu.RUnlock()
	out := []string{}
	for id, r := range s.types.types {
		if l, ok := r.t.(Listing); ok && l.ListsElements() {
			out = append(out, id)
		}
	}
	return out
}

// Types lists active element types.
func (s *Service) Types() []TypeInfo {
	s.types.mu.RLock()
	defer s.types.mu.RUnlock()
	out := []TypeInfo{}
	for id, r := range s.types.types {
		if s.enabled(r.pluginID) {
			out = append(out, TypeInfo{ID: id, PluginID: r.pluginID, Labels: r.t.Labels(), Empty: r.t.EmptyBody()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
