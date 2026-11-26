package i18n

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/readability"
)

// Short, clear sentences in plain language (CLAUDE.md, section 9): the texts
// of the interface and the messages of the server, German and English. The
// counting is that of internal/readability (also the hint in the editors).
//
//   - a sentence has at most maxSentenceWords words (a sentence ends with
//     . ! ? ; or : before a space, or with a line break; placeholders, code in
//     backticks, addresses and paths count as one word);
//   - a word has at most maxWordLetters letters (German compounds: a longer
//     one reads better split, e.g. "Stand der Übersetzung" for
//     "Übersetzungsfortschritt"; a hyphen splits a word);
//   - German addresses the reader with "Sie" (TestGermanFormal).
//
// The messages of the server (internal/apperr/locales) count too; their
// English text is the key.
//
// The long sentences that existed when the rule came are listed in
// testdata/readability-baseline.json. The list may only get shorter: a new
// long sentence fails, and so does an entry that is no longer too long
// (remove it). Rewrite: go test ./internal/i18n -run TestReadability -update-readability
const (
	maxSentenceWords = readability.DefaultSentenceWords
	maxWordLetters   = readability.DefaultWordLetters
)

var updateReadability = flag.Bool("update-readability", false, "rewrite testdata/readability-baseline.json (only to remove entries)")

// sentencesOf splits a text into sentences (code, addresses and paths are
// one word); the rules are those of the hint in the editors.
func sentencesOf(text string) []string { return readability.Sentences(text) }

// readabilityFindings lists the long sentences ("lang key") and the long
// words of all locale files of a language.
func readabilityFindings(t *testing.T, lang string) (sentences map[string]string, words []string) {
	t.Helper()
	files, _ := filepath.Glob("../../plugins/*/locales/" + lang + ".json")
	files = append(files, "locales/"+lang+".json", "../apperr/locales/de.json")
	sentences = map[string]string{}
	for _, f := range files {
		texts := readLocale(t, f)
		if lang == "en" && strings.HasPrefix(f, "../apperr/") {
			// the messages of the server: the English text is the key
			en := map[string]string{}
			for k := range texts {
				en[k] = k
			}
			texts = en
		}
		for k, v := range texts {
			if strings.HasPrefix(k, "_") {
				continue // a note on the file, not a text of the interface
			}
			for _, s := range sentencesOf(v) {
				if n := readability.Words(s); n > maxSentenceWords {
					sentences[lang+" "+k] = fmt.Sprintf("%d words: %s", n, strings.TrimSpace(s))
				}
			}
			for _, w := range readability.LongWords(v, maxWordLetters) {
				words = append(words, fmt.Sprintf("%s %s: %q in %q", lang, k, w, v))
			}
		}
	}
	return sentences, words
}

func TestReadability(t *testing.T) {
	const base = "testdata/readability-baseline.json"
	var known []string
	raw, readErr := os.ReadFile(base)
	if readErr == nil {
		if err := json.Unmarshal(raw, &known); err != nil {
			t.Fatalf("%s: %v", base, err)
		}
	}
	isKnown := map[string]bool{}
	for _, k := range known {
		isKnown[k] = true
	}
	found := map[string]string{}
	for _, lang := range []string{"de", "en"} {
		s, w := readabilityFindings(t, lang)
		for k, v := range s {
			found[k] = v
		}
		for _, x := range w {
			t.Errorf("word longer than %d letters (split it or say it in other words): %s", maxWordLetters, x)
		}
	}
	var keys []string
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if *updateReadability {
		var still []string
		for _, k := range keys {
			// a new list starts with all; after that it only gets shorter
			if isKnown[k] || os.IsNotExist(readErr) {
				still = append(still, k)
			}
		}
		raw, _ := json.MarshalIndent(still, "", " ")
		if err := os.WriteFile(base, append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, k := range keys {
		if !isKnown[k] {
			t.Errorf("sentence longer than %d words (split it, say less): %s: %s", maxSentenceWords, k, found[k])
		}
	}
	for _, k := range known {
		if _, ok := found[k]; !ok {
			t.Errorf("%s is no longer too long: remove it from %s (the list only gets shorter)", k, base)
		}
	}
	t.Logf("%d long sentences left in the baseline", len(known))
}

func TestReadabilityRules(t *testing.T) {
	if got := len(sentencesOf("Erster Satz. Zweiter Satz: mit Doppelpunkt; dritter Teil\nvierte Zeile")); got != 5 {
		t.Errorf("sentences: %d", got)
	}
	s := sentencesOf("Siehe https://news.example.com/a.b und scripts/update.sh --enable-timer sowie `x y z` und {count} Dinge.")
	if len(s) != 1 || readability.Words(s[0]) != 11 {
		t.Errorf("addresses, paths and code count as one word: %q", s)
	}
}
