// Package perm is the registry of permissions.
//
// There are two kinds:
//
//   - global permissions ("Funktionsrechte") are granted to roles (and to
//     all signed-in users) and allow functions of the whole system, e.g.
//     core.users.manage or workflow.process.admin;
//   - category permissions are granted in the permissions of a category to
//     users, groups and roles (optionally inherited by subcategories), e.g.
//     content.events.attendees. The five core category permissions view,
//     edit, create, delete and manage are bits of acl.Perm; all others are
//     named actions.
//
// The core and every plugin declare their permissions (plugins in
// plugin.json, "permissions"). Checking a permission that was not declared
// is a programming error: the check fails and logs an error, so a missing
// declaration is noticed at once.
package perm

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Scopes of a permission.
const (
	Global   = "global"
	Category = "category"
)

// Presets of category permissions (templates in the permission dialog).
const (
	Reader  = "reader"
	Editor  = "editor"
	Manager = "manager"
)

// Def declares a permission.
type Def struct {
	Key         string `json:"key"`   // <plugin id>.<name>, e.g. workflow.process.admin
	Scope       string `json:"scope"` // global or category
	Label       string `json:"label"` // English; translated with the UI key perm.<key>
	Description string `json:"description,omitempty"`
	// Presets lists the category presets that include it (reader, editor,
	// manager); for global permissions "authenticated" grants it to every
	// signed-in user by default.
	Presets []string `json:"presets,omitempty"`
	Group   string   `json:"group"` // plugin id or "core"
	// Implies lists permissions that are included (e.g. admin implies create).
	Implies []string `json:"implies,omitempty"`
}

var keyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,62}\.[a-z0-9_]{1,40}$`)

var (
	mu   sync.RWMutex
	defs = map[string]Def{}
)

// Register adds permissions of a group (the core or a plugin). Keys must
// start with the group id.
func Register(group string, list ...Def) error {
	mu.Lock()
	defer mu.Unlock()
	for _, d := range list {
		d.Group = group
		if !keyRe.MatchString(d.Key) || !strings.HasPrefix(d.Key, group+".") {
			return fmt.Errorf("permission %q: the key must be %s.<name> (lowercase letters, digits, _)", d.Key, group)
		}
		if d.Scope != Global && d.Scope != Category {
			return fmt.Errorf("permission %s: scope must be global or category", d.Key)
		}
		if d.Label == "" {
			return fmt.Errorf("permission %s: label is required", d.Key)
		}
		for _, p := range d.Presets {
			ok := p == Reader || p == Editor || p == Manager
			if d.Scope == Global {
				ok = p == "authenticated"
			}
			if !ok {
				return fmt.Errorf("permission %s: invalid preset %q", d.Key, p)
			}
		}
		if old, dup := defs[d.Key]; dup && old.Group != group {
			return fmt.Errorf("permission %s is already declared by %s", d.Key, old.Group)
		}
		defs[d.Key] = d
	}
	return nil
}

// Replace sets the permissions of a group whose keys start with prefix
// (e.g. one permission per configured language): existing ones with the
// prefix are removed first. Used for permissions that depend on settings.
func Replace(group, prefix string, list ...Def) error {
	mu.Lock()
	for k, d := range defs {
		if d.Group == group && strings.HasPrefix(k, prefix) {
			delete(defs, k)
		}
	}
	mu.Unlock()
	return Register(group, list...)
}

// MustRegister is Register for the core (panics on errors).
func MustRegister(group string, list ...Def) {
	if err := Register(group, list...); err != nil {
		panic(err)
	}
}

// Lookup returns a declared permission.
func Lookup(key string) (Def, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := defs[key]
	return d, ok
}

// All returns all declared permissions sorted by group and key.
func All() []Def {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Def, 0, len(defs))
	for _, d := range defs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			if out[i].Group == "core" || out[j].Group == "core" {
				return out[i].Group == "core"
			}
			return out[i].Group < out[j].Group
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Keys returns the keys of the declared permissions of a scope.
func Keys(scope string) []string {
	var out []string
	for _, d := range All() {
		if d.Scope == scope {
			out = append(out, d.Key)
		}
	}
	return out
}

// Validate checks that every key is a declared permission of scope.
func Validate(scope string, keys []string) error {
	for _, k := range keys {
		d, ok := Lookup(k)
		if !ok {
			return apperr.BadRequest("unknown permission %q", k)
		}
		if d.Scope != scope {
			return apperr.BadRequest("permission %q is a %s permission", k, d.Scope)
		}
	}
	return nil
}

// Expand adds the permissions implied by keys.
func Expand(keys []string) []string {
	out := slices.Clone(keys)
	for i := 0; i < len(out); i++ {
		if d, ok := Lookup(out[i]); ok {
			for _, imp := range d.Implies {
				if !slices.Contains(out, imp) {
					out = append(out, imp)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// Preset returns the named category actions of a preset (the core bits are
// handled by acl).
func Preset(name string) []string {
	var out []string
	for _, d := range All() {
		if d.Scope != Category {
			continue
		}
		for _, p := range d.Presets {
			if p == name || (name == Manager && p == Editor) || (name != Reader && p == Reader) {
				out = append(out, d.Key)
				break
			}
		}
	}
	return out
}

// DefaultAuthenticated returns the global permissions every signed-in user
// has unless changed in the administration.
func DefaultAuthenticated() []string {
	var out []string
	for _, d := range All() {
		if d.Scope == Global && slices.Contains(d.Presets, "authenticated") {
			out = append(out, d.Key)
		}
	}
	return out
}

// Has reports whether the caller has the global permission key.
// Administrators have all permissions. API keys only carry global
// permissions when created with allow_admin.
func Has(ctx context.Context, key string) bool {
	return HasP(identity.From(ctx), key)
}

// HasP is Has for a principal.
func HasP(p *identity.Principal, key string) bool {
	d, ok := Lookup(key)
	if !ok || d.Scope != Global {
		slog.Error("permission check for an undeclared global permission – declare it in plugin.json", "permission", key)
		return false
	}
	if p == nil {
		return false
	}
	if p.Admin() {
		return true
	}
	if p.Via == "apikey" && !p.KeyAllowAdmin {
		return false
	}
	return slices.Contains(p.Perms, key)
}

// Require returns 401 for guests and 403 unless the caller has key.
func Require(ctx context.Context, key string) error {
	p := identity.From(ctx)
	if p == nil {
		return apperr.ErrUnauthorized
	}
	if !HasP(p, key) {
		return apperr.ErrForbidden
	}
	return nil
}

// RequireAny is Require for any of the keys.
func RequireAny(ctx context.Context, keys ...string) error {
	p := identity.From(ctx)
	if p == nil {
		return apperr.ErrUnauthorized
	}
	for _, k := range keys {
		if HasP(p, k) {
			return nil
		}
	}
	return apperr.ErrForbidden
}
