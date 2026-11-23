package web

import (
	"embed"
	"encoding/json"
	"strings"
	"sync"
)

// The words of icon names in other languages, the same file the icon picker
// uses (src/icons/terms/<lang>.json, kept in the source, not built).
//
//go:embed src/icons/terms/*.json
var iconTerms embed.FS

var (
	termsOnce sync.Once
	termsByLg map[string]map[string][]string
)

func loadTerms() {
	termsByLg = map[string]map[string][]string{}
	ents, err := iconTerms.ReadDir("src/icons/terms")
	if err != nil {
		return
	}
	for _, e := range ents {
		raw, err := iconTerms.ReadFile("src/icons/terms/" + e.Name())
		if err != nil {
			continue
		}
		var f struct {
			Words map[string][]string `json:"words"`
		}
		if json.Unmarshal(raw, &f) == nil && len(f.Words) > 0 {
			termsByLg[strings.TrimSuffix(e.Name(), ".json")] = f.Words
		}
	}
}

// IconLabel is the name of an icon in words in the language lang, as screen
// readers read it: "Pfeil links" for arrow-left in German. Words without a
// translation (and every word in English) stay as they are.
func IconLabel(lang, name string) string {
	termsOnce.Do(loadTerms)
	words := termsByLg[strings.ToLower(strings.SplitN(lang, "-", 2)[0])]
	parts := strings.Split(name, "-")
	for i, w := range parts {
		if tr := words[w]; len(tr) > 0 && tr[0] != "" {
			parts[i] = tr[0]
		}
	}
	return strings.Join(parts, " ")
}
