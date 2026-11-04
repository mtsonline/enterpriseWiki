package i18n

import (
	"strconv"
	"strings"
	"sync"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// Plural forms (CLDR categories) in their canonical order. A plural text has
// one key per form: "inbox.new_one", "inbox.new_other" (English), Polish
// also needs "_few" and "_many". "_other" is required in every language.
var Forms = []string{"zero", "one", "two", "few", "many", "other"}

var formOf = map[plural.Form]string{
	plural.Other: "other", plural.Zero: "zero", plural.One: "one",
	plural.Two: "two", plural.Few: "few", plural.Many: "many",
}

var (
	catMu    sync.Mutex
	catCache = map[string][]string{}
)

// PluralForms returns the plural forms a language uses (CLDR order).
func PluralForms(lang string) []string {
	catMu.Lock()
	defer catMu.Unlock()
	if c, ok := catCache[lang]; ok {
		return c
	}
	tag, err := language.Parse(lang)
	if err != nil {
		tag = language.English
	}
	seen := map[string]bool{"other": true}
	// integers and numbers with one decimal cover every category of CLDR
	for i := 0; i <= 1000; i++ {
		seen[formOf[plural.Cardinal.MatchPlural(tag, i, 0, 0, 0, 0)]] = true
		if i <= 20 {
			for f := 1; f <= 9; f++ {
				seen[formOf[plural.Cardinal.MatchPlural(tag, i, 1, 1, f, f)]] = true
			}
		}
	}
	var out []string
	for _, f := range Forms {
		if seen[f] {
			out = append(out, f)
		}
	}
	catCache[lang] = out
	return out
}

// PluralForm returns the form of number n (e.g. "3", "1.5") in lang.
func PluralForm(lang, n string) string {
	tag, err := language.Parse(lang)
	if err != nil {
		tag = language.English
	}
	n = strings.TrimPrefix(strings.TrimSpace(n), "-")
	ip, fp, _ := strings.Cut(n, ".")
	i, err := strconv.Atoi(ip)
	if err != nil {
		return "other"
	}
	v, f, w, t := len(fp), 0, 0, 0
	if v > 0 {
		if f, err = strconv.Atoi(fp); err != nil {
			return "other"
		}
		tr := strings.TrimRight(fp, "0")
		w = len(tr)
		if w > 0 {
			t, _ = strconv.Atoi(tr)
		}
	}
	return formOf[plural.Cardinal.MatchPlural(tag, i, v, w, f, t)]
}

// splitForm splits "inbox.new_one" into ("inbox.new", "one").
func splitForm(key string) (string, string, bool) {
	i := strings.LastIndexByte(key, '_')
	if i <= 0 {
		return key, "", false
	}
	for _, f := range Forms {
		if key[i+1:] == f {
			return key[:i], f, true
		}
	}
	return key, "", false
}

// countParam returns the number that selects the plural form.
func countParam(params map[string]string) (string, bool) {
	if v, ok := params["count"]; ok {
		return v, true
	}
	v, ok := params["n"]
	return v, ok
}
