package i18n

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/limits"
)

// The translations of the core and of all plugins: English and German have
// the same keys, and every permission and limit (expert setting) has its
// label in both languages.

func readLocale(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- files of the repository
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

func TestLocaleParity(t *testing.T) {
	all := map[string]map[string]string{"en": {}, "de": {}}
	dirs, _ := filepath.Glob("../../plugins/*/locales")
	dirs = append(dirs, "locales")
	for _, dir := range dirs {
		en, de := readLocale(t, filepath.Join(dir, "en.json")), readLocale(t, filepath.Join(dir, "de.json"))
		var missing []string
		for k := range en {
			if _, ok := de[k]; !ok {
				missing = append(missing, "de:"+k)
			}
		}
		for k := range de {
			if _, ok := en[k]; !ok {
				missing = append(missing, "en:"+k)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s: missing translations %v", dir, missing)
		}
		for k, v := range en {
			all["en"][k] = v
		}
		for k, v := range de {
			all["de"][k] = v
		}
	}
	need := map[string]bool{}
	manifests, _ := filepath.Glob("../../plugins/*/plugin.json")
	for _, f := range manifests {
		raw, err := os.ReadFile(f) // #nosec G304 -- files of the repository
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			Permissions []struct{ Key string } `json:"permissions"`
			Limits      []limits.Def          `json:"limits"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, p := range m.Permissions {
			need["perm."+p.Key], need["perm."+p.Key+"_help"] = true, true
		}
		for _, d := range m.Limits {
			need["limit."+d.Key], need["limit."+d.Key+".help"], need["limits.group."+d.Group] = true, true, true
		}
	}
	for _, d := range limits.List() { // the limits of the core
		need["limit."+d.Key], need["limit."+d.Key+".help"], need["limits.group."+d.Group] = true, true, true
	}
	// keys written literally in the sources: t('key'), tr("key")
	call := regexp.MustCompile("\\b(?:tr|t)\\(\\s*['\"`]([a-z][a-zA-Z0-9_\\-]*(?:\\.[a-zA-Z0-9_\\-]+)+)['\"`]")
	for _, root := range []string{"../../web/src", "../../plugins", "../../internal"} {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				if d != nil && d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(path)
			if (ext != ".ts" && ext != ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(path, ".test.") {
				return nil
			}
			raw, err := os.ReadFile(path) // #nosec G304 -- files of the repository
			if err != nil {
				return err
			}
			for _, m := range call.FindAllSubmatch(raw, -1) {
				k := string(m[1])
				if _, ok := all["en"][k+"_other"]; ok { // plural forms
					continue
				}
				need[k] = true
			}
			return nil
		})
	}
	for lang, keys := range all {
		for k := range need {
			if _, ok := keys[k]; !ok {
				t.Errorf("%s: no translation for %s", lang, k)
			}
		}
	}
}
