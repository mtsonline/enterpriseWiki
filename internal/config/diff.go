package config

import (
	"reflect"
	"sort"

	"gopkg.in/yaml.v3"
)

// ChangedKeys lists the keys (dotted, as in "ewiki config get") whose values
// differ between a and b. Only keys, never values: values can be secrets.
func ChangedKeys(a, b *Config) []string {
	fa, fb := flatten(a), flatten(b)
	set := map[string]bool{}
	for k, v := range fa {
		if w, ok := fb[k]; !ok || !reflect.DeepEqual(v, w) {
			set[k] = true
		}
	}
	for k := range fb {
		if _, ok := fa[k]; !ok {
			set[k] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// flatten turns c into dotted keys and their values (lists stay one value).
func flatten(c *Config) map[string]any {
	out := map[string]any{}
	if c == nil {
		return out
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return out
	}
	var m map[string]any
	if yaml.Unmarshal(b, &m) != nil {
		return out
	}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		if mm, ok := v.(map[string]any); ok && len(mm) > 0 {
			for k, x := range mm {
				key := k
				if prefix != "" {
					key = prefix + "." + k
				}
				walk(key, x)
			}
			return
		}
		out[prefix] = v
	}
	walk("", m)
	return out
}
