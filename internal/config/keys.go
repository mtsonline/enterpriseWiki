package config

import (
	"fmt"
	"reflect"
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

// lookup follows a dotted key through the structure of the configuration
// by the YAML names of the fields (also fields that are empty and left out
// when written), and through maps by their keys. ok is false for a key
// the configuration does not have.
func lookup(c *Config, key string) (v reflect.Value, ok bool) {
	v = reflect.ValueOf(c).Elem()
	for _, part := range strings.Split(key, ".") {
		for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
			if v.IsNil() {
				return reflect.Value{}, v.Kind() == reflect.Interface
			}
			v = v.Elem()
		}
		switch v.Kind() {
		case reflect.Struct:
			found := false
			for i := range v.NumField() {
				f := v.Type().Field(i)
				name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
				if name == "" || name == "-" || !f.IsExported() {
					continue
				}
				if name == part {
					v, found = v.Field(i), true
					break
				}
			}
			if !found {
				return reflect.Value{}, false
			}
		case reflect.Map:
			if v.Type().Key().Kind() != reflect.String {
				return reflect.Value{}, false
			}
			e := v.MapIndex(reflect.ValueOf(part))
			if !e.IsValid() {
				// a new entry of a map (e.g. plugin settings): valid, empty
				return reflect.Zero(v.Type().Elem()), true
			}
			v = e
		default:
			return reflect.Value{}, false
		}
	}
	return v, true
}

// GetKey returns the YAML representation of a dotted key (e.g. "ui.site_name").
func GetKey(c *Config, key string) (string, error) {
	v, ok := lookup(c, key)
	if !ok {
		return "", fmt.Errorf("unknown key %q", key)
	}
	var out any
	if v.IsValid() && v.CanInterface() {
		out = v.Interface()
	}
	b, err := yaml.Marshal(out)
	return strings.TrimSpace(string(b)), err
}

// SetKey sets a dotted key. The value is parsed as YAML, so lists can be
// given as "[en, de]" and numbers / booleans are typed automatically. Every
// key of the configuration can be set, also one that is empty now; keys
// below plugin_settings may be created.
func SetKey(c *Config, key, value string) error {
	if _, ok := lookup(c, key); !ok {
		return fmt.Errorf("unknown key %q", key)
	}
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
		if i == len(parts)-1 {
			cur[part] = v
			break
		}
		next, ok := cur[part].(map[string]any)
		if !ok {
			if cur[part] != nil {
				return fmt.Errorf("unknown key %q", key)
			}
			// a section that is empty now (left out when written)
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
