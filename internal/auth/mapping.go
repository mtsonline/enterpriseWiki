package auth

import (
	"fmt"
	"sort"
	"strings"
)

// Mapping translates groups and attributes of an external identity provider
// into local groups and roles. Every provider plugin reads it from its
// plugin_settings section, so the rules work the same for all providers:
//
//	group_mapping:  {"cn=hr,ou=groups,dc=example,dc=com": "HR"}   # external -> local group
//	role_mapping:   {"wiki-admins": "Administrators"}            # external group -> local role
//	default_groups: [Staff]                                      # always added
//	default_roles:  []
//	keep_unmapped_groups: true                                   # unmapped external groups are created as local groups
//
// Group matching is case-insensitive. Local memberships (assigned in the
// admin UI) are never touched by the synchronization.
type Mapping struct {
	GroupMapping       map[string]string
	RoleMapping        map[string]string
	DefaultGroups      []string
	DefaultRoles       []string
	KeepUnmappedGroups bool
}

// MappingFromSettings reads the mapping keys of a plugin_settings section.
func MappingFromSettings(s map[string]any) Mapping {
	m := Mapping{GroupMapping: StringMap(s["group_mapping"]), RoleMapping: StringMap(s["role_mapping"]),
		DefaultGroups: StringList(s["default_groups"]), DefaultRoles: StringList(s["default_roles"]), KeepUnmappedGroups: true}
	if v, ok := s["keep_unmapped_groups"].(bool); ok {
		m.KeepUnmappedGroups = v
	}
	return m
}

// Apply maps the external groups of id (id.Groups) to local groups and roles.
func (m Mapping) Apply(id *ExternalIdentity) {
	groups := map[string]bool{}
	roles := map[string]bool{}
	lower := func(mm map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range mm {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
		return out
	}
	gm, rm := lower(m.GroupMapping), lower(m.RoleMapping)
	for _, g := range id.Groups {
		k := strings.ToLower(strings.TrimSpace(g))
		if k == "" {
			continue
		}
		short := strings.ToLower(groupName(g)) // "cn=HR,ou=..." also matches the key "hr"
		lookup := func(m map[string]string) (string, bool) {
			if v, ok := m[k]; ok {
				return v, true
			}
			v, ok := m[short]
			return v, ok
		}
		if r, ok := lookup(rm); ok && r != "" {
			roles[r] = true
		}
		if lg, ok := lookup(gm); ok {
			if lg != "" {
				groups[lg] = true
			}
		} else if m.KeepUnmappedGroups {
			groups[groupName(g)] = true
		}
	}
	for _, g := range m.DefaultGroups {
		groups[g] = true
	}
	for _, r := range m.DefaultRoles {
		roles[r] = true
	}
	id.Groups, id.Roles = keys(groups), keys(roles)
}

// groupName shortens a distinguished name (cn=HR,ou=groups,...) to its first value.
func groupName(g string) string {
	g = strings.TrimSpace(g)
	if i := strings.Index(g, "="); i > 0 && strings.Contains(g, ",") {
		first := strings.SplitN(g, ",", 2)[0]
		return strings.TrimSpace(first[i+1:])
	}
	return g
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// StringMap converts a YAML map value to map[string]string.
func StringMap(v any) map[string]string {
	out := map[string]string{}
	switch m := v.(type) {
	case map[string]any:
		for k, x := range m {
			out[k] = fmt.Sprint(x)
		}
	case map[string]string:
		for k, x := range m {
			out[k] = x
		}
	}
	return out
}

// StringList converts a YAML list (or comma separated string) to []string.
func StringList(v any) []string {
	var out []string
	switch l := v.(type) {
	case []any:
		for _, x := range l {
			if s := strings.TrimSpace(fmt.Sprint(x)); s != "" {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, l...)
	case string:
		for _, s := range strings.Split(l, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// Setting returns a string setting with a default.
func Setting(s map[string]any, key, def string) string {
	if v, ok := s[key]; ok && v != nil {
		if str := strings.TrimSpace(fmt.Sprint(v)); str != "" {
			return str
		}
	}
	return def
}

// BoolSetting returns a boolean setting with a default.
func BoolSetting(s map[string]any, key string, def bool) bool {
	if v, ok := s[key].(bool); ok {
		return v
	}
	return def
}
