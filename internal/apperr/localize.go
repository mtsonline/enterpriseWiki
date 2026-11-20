package apperr

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
)

// The messages of the server in other languages: locales/<lang>.json maps
// the English format of a message (as written in the code) to the format
// in that language, with the same verbs. go test ./internal/apperr checks
// that every message of the code has a text in every language.
//
//go:embed locales/*.json
var localeFS embed.FS

var (
	catalogOnce sync.Once
	catalogs    map[string]map[string]string
)

func loadCatalogs() map[string]map[string]string {
	catalogOnce.Do(func() {
		catalogs = map[string]map[string]string{}
		files, _ := localeFS.ReadDir("locales")
		for _, f := range files {
			b, err := localeFS.ReadFile(path.Join("locales", f.Name()))
			if err != nil {
				continue
			}
			var m map[string]string
			if json.Unmarshal(b, &m) != nil {
				continue
			}
			delete(m, "_about")
			catalogs[strings.TrimSuffix(f.Name(), ".json")] = m
		}
	})
	return catalogs
}

// Languages are the languages with texts for the messages (besides English).
func Languages() []string {
	var out []string
	for l := range loadCatalogs() {
		out = append(out, l)
	}
	return out
}

// Catalog is the texts of one language (for tests and tools; do not change).
func Catalog(lang string) map[string]string { return loadCatalogs()[lang] }

// Localized is the message of err in lang: the text of the language with
// the same values, or the English message if there is none. Values that are
// texts of their own (a field name, an error from elsewhere) stay as they are.
func Localized(err error, lang string) string {
	_, _, msg := Status(err)
	cat := loadCatalogs()[lang]
	if cat == nil {
		return msg
	}
	format, args := strings.ReplaceAll(msg, "%", "%%"), []any(nil)
	var e *Error
	if errors.As(err, &e) && e.format != "" {
		format, args = e.format, e.args
	}
	tr, ok := cat[format]
	if !ok {
		return msg
	}
	out := fmt.Sprintf(tr, args...)
	if strings.Contains(out, "%!") { // a broken text never hides the message
		return msg
	}
	return out
}

// RequestLanguage is the language the client asks for (the first entry of
// Accept-Language, e.g. "de" for "de-AT,de;q=0.9"), if there are texts in
// it; otherwise "en".
func RequestLanguage(r *http.Request) string {
	al := r.Header.Get("Accept-Language")
	first, _, _ := strings.Cut(al, ",")
	first, _, _ = strings.Cut(first, ";")
	lang, _, _ := strings.Cut(strings.TrimSpace(first), "-")
	lang = strings.ToLower(lang)
	if _, ok := loadCatalogs()[lang]; ok {
		return lang
	}
	return "en"
}
