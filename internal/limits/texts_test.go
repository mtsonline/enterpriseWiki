package limits_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/limits"
	_ "github.com/mtsonline/enterprisewiki/plugins/all" // limits of the core plugins
)

// Every limit and every group of limits (core and plugins) has its texts in German and English
// (regression: the group "mail" showed as the raw word in the interface).
func TestLimitTexts(t *testing.T) {
	for _, lang := range []string{"en", "de"} {
		b, err := os.ReadFile("../i18n/locales/" + lang + ".json")
		if err != nil {
			t.Fatal(err)
		}
		texts := map[string]string{}
		if err := json.Unmarshal(b, &texts); err != nil {
			t.Fatal(err)
		}
		for _, d := range limits.List() {
			if strings.HasPrefix(d.Key, "acme.") {
				continue // registered by the unit tests of this package
			}
			for _, k := range []string{"limit." + d.Key, "limit." + d.Key + ".help", "limits.group." + d.Group} {
				if texts[k] == "" {
					t.Errorf("%s: no text %q", lang, k)
				}
			}
		}
	}
}
