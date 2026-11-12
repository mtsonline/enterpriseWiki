package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// InstanceOnly lists the settings that apply to the whole instance; a site
// cannot override them (database, license, listener).
var InstanceOnly = []string{"mode", "database", "license", "sites", "server.listen", "server.trusted_proxies", "server.client_ip_header", "server.max_body_mb", "limits"}

// IsInstanceOnly reports whether key (dotted path) is an instance setting.
func IsInstanceOnly(key string) bool {
	for _, k := range InstanceOnly {
		if key == k || strings.HasPrefix(key, k+".") {
			return true
		}
	}
	return false
}

// SiteFile returns the file with the settings of a further site.
func SiteFile(base *Store, site string) string {
	return filepath.Join(filepath.Dir(base.Path()), "sites", site+".yaml")
}

// OpenSite returns the settings of a further site: the instance settings
// (base) with the differences stored in the site's file. Instance-only
// settings always come from base. Files are kept in a sub directory of the
// instance storage unless the site sets storage.path.
func OpenSite(base *Store, site string) (*Store, error) {
	s := &Store{path: SiteFile(base, site), base: base, site: site}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	base.OnChange(func(*Config) {
		_ = s.Reload() // an invalid site file keeps the previous settings
	})
	return s, nil
}

// Site returns the site of a site store ("" for the instance settings).
func (s *Store) Site() string { return s.site }

// Base returns the instance settings of a site store (itself otherwise).
func (s *Store) Base() *Store {
	if s.base != nil {
		return s.base
	}
	return s
}

func (s *Store) readOverlay() (map[string]any, time.Time, error) {
	ov := map[string]any{}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return ov, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	if err := yaml.Unmarshal(b, &ov); err != nil {
		return nil, time.Time{}, fmt.Errorf("config %s: %w", s.path, err)
	}
	var mt time.Time
	if st, err := os.Stat(s.path); err == nil {
		mt = st.ModTime()
	}
	if ov == nil {
		ov = map[string]any{}
	}
	stripInstanceOnly(ov, "")
	return ov, mt, nil
}

func stripInstanceOnly(m map[string]any, prefix string) {
	for k, v := range m {
		key := prefix + k
		if IsInstanceOnly(key) {
			delete(m, k)
			continue
		}
		if sub, ok := v.(map[string]any); ok && prefix == "" {
			stripInstanceOnly(sub, key+".")
		}
	}
}

// merge copies over into base; nested maps are merged, other values replaced.
func merge(base, over map[string]any) {
	for k, v := range over {
		if sub, ok := v.(map[string]any); ok {
			if bsub, ok := base[k].(map[string]any); ok {
				merge(bsub, sub)
				continue
			}
		}
		base[k] = v
	}
}

// diff returns the values of c that differ from base.
func diff(c, base map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range c {
		bv, ok := base[k]
		if sub, isMap := v.(map[string]any); isMap && ok {
			if bsub, ok := bv.(map[string]any); ok {
				if d := diff(sub, bsub); len(d) > 0 {
					out[k] = d
				}
				continue
			}
		}
		if !ok || !reflect.DeepEqual(v, bv) {
			out[k] = v
		}
	}
	return out
}

func (s *Store) siteStorage(baseRaw *Config) string {
	return filepath.Join(baseRaw.Storage.Path, "sites", s.site)
}

func (s *Store) reloadSite() error {
	baseRaw := s.base.Raw()
	m, err := toMap(baseRaw)
	if err != nil {
		return err
	}
	ov, mt, err := s.readOverlay()
	if err != nil {
		return err
	}
	merge(m, ov)
	b, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	raw := Defaults()
	if err := yaml.Unmarshal(b, raw); err != nil {
		return fmt.Errorf("config %s: %w", s.path, err)
	}
	if st, _ := ov["storage"].(map[string]any); st == nil || st["path"] == nil {
		raw.Storage.Path = s.siteStorage(baseRaw)
	}
	if err := Validate(raw); err != nil {
		return fmt.Errorf("config %s: %w", s.path, err)
	}
	eff, err := clone(raw)
	if err != nil {
		return err
	}
	expandAll(reflect.ValueOf(eff))
	applyEnv(eff)
	s.resolvePaths(eff)
	s.mu.Lock()
	s.raw, s.effective, s.modTime = raw, eff, mt
	ls := append([]func(*Config){}, s.listeners...)
	s.mu.Unlock()
	for _, l := range ls {
		l(eff)
	}
	return nil
}

const siteHeader = `# Settings of one site - only the differences to the instance settings
# (config.yaml). Instance settings (database, license, server.listen)
# are ignored here. Reference: docs/admin/sites.md
`

func (s *Store) writeSite(c *Config) error {
	baseRaw := s.base.Raw()
	cm, err := toMap(c)
	if err != nil {
		return err
	}
	bm, err := toMap(baseRaw)
	if err != nil {
		return err
	}
	if c.Storage.Path == s.siteStorage(baseRaw) {
		if st, ok := cm["storage"].(map[string]any); ok {
			st["path"] = baseRaw.Storage.Path
		}
	}
	d := diff(cm, bm)
	stripInstanceOnly(d, "")
	b, err := yaml.Marshal(d)
	if err != nil {
		return err
	}
	if len(d) == 0 {
		b = nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append([]byte(siteHeader), b...), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// RemoveSite deletes the settings file of a site.
func RemoveSite(base *Store, site string) error {
	err := os.Remove(SiteFile(base, site))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
