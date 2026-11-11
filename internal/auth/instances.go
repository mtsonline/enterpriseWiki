package auth

import (
	"regexp"
	"sync"
)

// Provider instances: a sign-in plugin can serve several configured
// providers of its kind, e.g. two OpenID Connect providers. The plugin's
// top-level settings are the first instance (provider id = base id, e.g.
// "oidc"); each entry of the list "instances" is a further one with its
// own complete settings, mapping and JIT rules (provider id = base-id,
// e.g. "oidc-entra"):
//
//	plugin_settings:
//	  auth.oidc:
//	    issuer: https://keycloak.example.com/realms/staff
//	    ...
//	    instances:
//	      - id: entra
//	        name: Partner login
//	        issuer: https://login.microsoftonline.com/<tenant>/v2.0
//	        jit: false
//
// The provider id ends up in users.provider, user identities and sessions,
// so an instance id must not change once people signed in with it.

// InstanceKey is the settings key holding the further instances.
const InstanceKey = "instances"

var instanceIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,23}$`)

// ValidInstanceID reports whether id is a valid instance id (without the
// base id): 1-24 lower-case letters, digits and hyphens.
func ValidInstanceID(id string) bool { return instanceIDRe.MatchString(id) }

// InstanceIDs returns the provider ids configured in s for base: base
// itself and base-<id> for each valid, unique instance.
func InstanceIDs(base string, s map[string]any) []string {
	out := []string{base}
	seen := map[string]bool{}
	for _, m := range instanceList(s) {
		id, _ := m["id"].(string)
		if !ValidInstanceID(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, base+"-"+id)
	}
	return out
}

// InstanceSettings returns the settings of provider id (see InstanceIDs)
// within the plugin settings s; nil if there is no such instance.
func InstanceSettings(base, id string, s map[string]any) map[string]any {
	if id == base {
		out := make(map[string]any, len(s))
		for k, v := range s {
			if k != InstanceKey {
				out[k] = v
			}
		}
		return out
	}
	for _, m := range instanceList(s) {
		if iid, _ := m["id"].(string); ValidInstanceID(iid) && base+"-"+iid == id {
			return m
		}
	}
	return nil
}

func instanceList(s map[string]any) []map[string]any {
	var out []map[string]any
	switch l := s[InstanceKey].(type) {
	case []any:
		for _, x := range l {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
	case []map[string]any:
		out = l
	}
	return out
}

// InstanceSet keeps one provider object per configured instance of a
// plugin, so per-instance state (discovery documents, metadata) survives
// between requests. Instances follow the current settings: new ones
// appear, removed ones disappear, changed settings apply at once (each
// provider reads its settings through the function it was built with).
type InstanceSet[T any] struct {
	base     string
	settings func() map[string]any
	mk       func(id string, settings func() map[string]any) T

	mu    sync.Mutex
	cache map[string]T
}

// NewInstanceSet creates the set of plugin settings settings for base.
func NewInstanceSet[T any](base string, settings func() map[string]any, mk func(id string, settings func() map[string]any) T) *InstanceSet[T] {
	return &InstanceSet[T]{base: base, settings: settings, mk: mk, cache: map[string]T{}}
}

// All returns the providers of all configured instances, base first.
func (s *InstanceSet[T]) All() []T {
	ids := InstanceIDs(s.base, s.settings())
	out := make([]T, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.get(id))
	}
	return out
}

// Get returns the provider of instance id ("" = base); false if there is
// no such instance.
func (s *InstanceSet[T]) Get(id string) (T, bool) {
	if id == "" {
		id = s.base
	}
	for _, x := range InstanceIDs(s.base, s.settings()) {
		if x == id {
			return s.get(id), true
		}
	}
	var zero T
	return zero, false
}

func (s *InstanceSet[T]) get(id string) T {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.cache[id]; ok {
		return p
	}
	p := s.mk(id, func() map[string]any {
		if m := InstanceSettings(s.base, id, s.settings()); m != nil {
			return m
		}
		return map[string]any{} // removed meanwhile: not configured
	})
	s.cache[id] = p
	return p
}

// SettingsProvider is implemented by providers that expose their current
// settings (used for the mapping preview).
type SettingsProvider interface {
	Settings() map[string]any
}
