package all

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

// TestPluginLocales checks that every plugin shipping UI strings ships
// English (the reference language) and that other languages only translate
// keys that exist in English. Plugin permissions need an English label.
func TestPluginLocales(t *testing.T) {
	for _, p := range sdk.Registered() {
		m := p.Manifest()
		for _, d := range m.Permissions {
			if strings.TrimSpace(d.Label) == "" {
				t.Errorf("%s: permission %s has no label", m.ID, d.Key)
			}
		}
		l, ok := p.(sdk.LocalesPlugin)
		if !ok {
			continue
		}
		files := l.Locales()
		entries, err := fs.ReadDir(files, ".")
		if err != nil {
			t.Fatalf("%s: %v", m.ID, err)
		}
		load := func(name string) map[string]string {
			b, err := fs.ReadFile(files, name)
			if err != nil {
				t.Fatalf("%s/%s: %v", m.ID, name, err)
			}
			v := map[string]string{}
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatalf("%s/%s: %v", m.ID, name, err)
			}
			return v
		}
		var en map[string]string
		for _, e := range entries {
			if e.Name() == "en.json" {
				en = load("en.json")
			}
		}
		if en == nil {
			t.Errorf("%s: locales without en.json", m.ID)
			continue
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == "en.json" || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			for k := range load(e.Name()) {
				if _, ok := en[k]; !ok {
					t.Errorf("%s/%s: key %q missing in en.json", m.ID, e.Name(), k)
				}
			}
		}
	}
}
