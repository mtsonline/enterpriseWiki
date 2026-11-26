package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPluginNames: every plugin has a name in German and English
// (plugin.name.<id>, shown in the administration), in its own texts or, for
// plugins without texts, in those of the core; the English name is the one
// of its manifest, so the two cannot drift apart.
func TestPluginNames(t *testing.T) {
	read := func(path string) map[string]string {
		out := map[string]string{}
		if b, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
		}
		return out
	}
	coreEN, coreDE := read("locales/en.json"), read("locales/de.json")
	manifests, err := filepath.Glob("../../plugins/*/plugin.json")
	if err != nil || len(manifests) == 0 {
		t.Fatalf("no plugins found: %v", err)
	}
	for _, mf := range manifests {
		var m struct{ ID, Name string }
		b, err := os.ReadFile(mf)
		if err != nil || json.Unmarshal(b, &m) != nil {
			t.Fatalf("%s: unreadable manifest", mf)
		}
		key := "plugin.name." + m.ID
		dir := filepath.Join(filepath.Dir(mf), "locales")
		en, de := read(filepath.Join(dir, "en.json")), read(filepath.Join(dir, "de.json"))
		if en[key] == "" {
			en, de = coreEN, coreDE
		}
		if de[key] == "" {
			t.Errorf("%s: no German name (%s)", m.ID, key)
		}
		// the templates have shorter names of their own in the interface
		if en[key] != m.Name && !strings.HasPrefix(filepath.Base(filepath.Dir(mf)), "theme_") {
			t.Errorf("%s: English name %q differs from the manifest %q", m.ID, en[key], m.Name)
		}
	}
}
