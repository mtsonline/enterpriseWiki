package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func toMap(c *Config) (map[string]any, error) {
	b, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	return m, yaml.Unmarshal(b, &m)
}

// GetKey returns the YAML representation of a dotted key (e.g. "ui.site_name").
func GetKey(c *Config, key string) (string, error) {
	m, err := toMap(c)
	if err != nil {
		return "", err
	}
	var cur any = m
	for _, part := range strings.Split(key, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("unknown key %q", key)
		}
		if cur, ok = mm[part]; !ok {
			return "", fmt.Errorf("unknown key %q", key)
		}
	}
	b, err := yaml.Marshal(cur)
	return strings.TrimSpace(string(b)), err
}

// SetKey sets a dotted key. The value is parsed as YAML, so lists can be
// given as "[en, de]" and numbers / booleans are typed automatically.
// Keys below plugin_settings may be created; all other keys must exist.
func SetKey(c *Config, key, value string) error {
	m, err := toMap(c)
	if err != nil {
		return err
	}
	var v any
	if err := yaml.Unmarshal([]byte(value), &v); err != nil {
		return fmt.Errorf("invalid value: %w", err)
	}
	parts := strings.Split(key, ".")
	cur := m
	for i, part := range parts {
		last := i == len(parts)-1
		if last {
			if _, ok := cur[part]; !ok && parts[0] != "plugin_settings" {
				return fmt.Errorf("unknown key %q", key)
			}
			cur[part] = v
			break
		}
		next, ok := cur[part].(map[string]any)
		if !ok {
			if parts[0] != "plugin_settings" {
				return fmt.Errorf("unknown key %q", key)
			}
			next = map[string]any{}
			cur[part] = next
		}
		cur = next
	}
	b, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	nc := Defaults()
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(nc); err != nil {
		return fmt.Errorf("invalid value for %s: %w", key, err)
	}
	*c = *nc
	return nil
}
