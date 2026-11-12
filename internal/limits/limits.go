// Package limits is the register of the technical limits of the wiki
// ("expert settings"): sizes of requests, parallel work, lengths of log
// values and the like. The core and every plugin declare their limits with
// a default and the allowed range; administrators see and change all of
// them in one place (Administration → Expert settings, section "limits" of
// config.yaml).
package limits

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// Def declares a limit.
type Def struct {
	// Key is unique and starts with the owner: "core.request.guest_kb",
	// "content.blog.post_kb".
	Key string `json:"key"`
	// Owner is "core" or the id of the plugin (set when registering).
	Owner string `json:"owner"`
	// Group sorts the limits in the administration: requests, database,
	// audit, live, search or a name of the plugin.
	Group string `json:"group"`
	// Label and Help are English; translations: UI keys limit.<key> and
	// limit.<key>.help.
	Label string `json:"label"`
	Help  string `json:"help"`
	// Unit: kb | mb | bytes | count | seconds | per_second | days
	Unit    string `json:"unit"`
	Default int64  `json:"default"`
	Min     int64  `json:"min"`
	Max     int64  `json:"max"`
	// Restart is true when a change takes effect after a restart only.
	Restart bool `json:"restart,omitempty"`
}

var (
	mu     sync.RWMutex
	defs   = map[string]Def{}
	routes = map[string]string{} // route pattern -> key of its body limit
	keyRe  = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)
	units  = map[string]bool{"kb": true, "mb": true, "bytes": true, "count": true, "seconds": true, "per_second": true, "days": true}
)

// Register declares a limit of owner ("core" or a plugin id). Declaring the
// same limit again (further sites share the process) is fine.
func Register(owner string, d Def) error {
	d.Owner = owner
	switch {
	case !keyRe.MatchString(d.Key) || !strings.HasPrefix(d.Key, owner+"."):
		return fmt.Errorf("limit %q: the key must start with %q and use a-z, 0-9, _ and dots", d.Key, owner+".")
	case !units[d.Unit]:
		return fmt.Errorf("limit %s: unknown unit %q", d.Key, d.Unit)
	case d.Label == "" || d.Group == "":
		return fmt.Errorf("limit %s: label and group are required", d.Key)
	case d.Min > d.Max || d.Default < d.Min || d.Default > d.Max:
		return fmt.Errorf("limit %s: default %d is outside of %d..%d", d.Key, d.Default, d.Min, d.Max)
	}
	mu.Lock()
	defer mu.Unlock()
	if old, ok := defs[d.Key]; ok && old != d {
		return fmt.Errorf("limit %s is declared twice with different values", d.Key)
	}
	defs[d.Key] = d
	return nil
}

// MustRegister is Register for limits known at compile time.
func MustRegister(owner string, ds ...Def) {
	for _, d := range ds {
		if err := Register(owner, d); err != nil {
			panic(err)
		}
	}
}

// Get returns the declaration of a limit.
func Get(key string) (Def, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := defs[key]
	return d, ok
}

// List returns all limits sorted by group and key.
func List() []Def {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Def, 0, len(defs))
	for _, d := range defs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Value returns the configured value of a limit, its default when nothing
// (or something outside of the allowed range) is configured, and 0 for an
// unknown key.
func Value(c *config.Config, key string) int64 {
	d, ok := Get(key)
	if !ok {
		return 0
	}
	if c != nil {
		if v, ok := c.Limits[key]; ok && v >= d.Min && v <= d.Max {
			return v
		}
	}
	return d.Default
}

// Check validates a value for key.
func Check(key string, v int64) error {
	d, ok := Get(key)
	if !ok {
		return fmt.Errorf("unknown limit %q", key)
	}
	if v < d.Min || v > d.Max {
		return fmt.Errorf("%s: %d is outside of %d..%d", key, v, d.Min, d.Max)
	}
	return nil
}

// RouteBody declares that the route (pattern as registered, e.g.
// "PUT /api/v1/elements/{id}") accepts bodies up to the limit key instead
// of the general limit of requests.
func RouteBody(pattern, key string) {
	mu.Lock()
	routes[pattern] = key
	mu.Unlock()
}

// BodyKey returns the limit declared for a route ("" for none).
func BodyKey(pattern string) string {
	mu.RLock()
	defer mu.RUnlock()
	return routes[pattern]
}

// Bytes converts the value of a size limit to bytes.
func Bytes(c *config.Config, key string) int64 {
	d, _ := Get(key)
	v := Value(c, key)
	switch d.Unit {
	case "kb":
		return v << 10
	case "mb":
		return v << 20
	}
	return v
}
