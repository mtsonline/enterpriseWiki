// Package readability finds long sentences and long words (plain language,
// CLAUDE.md section 9). The test of the interface texts (internal/i18n) uses
// it, and the hint while writing in the editors counts the same way in the
// browser (web/src/core/readability.ts). Both run the shared cases of
// testdata/cases.json.
//
//   - a sentence ends with . ! ? ; or : before a space, or with a line break;
//   - code in backticks, placeholders in braces, addresses (https://…) and
//     paths count as one word;
//   - a word of a sentence is a run of letters and digits;
//   - a long word is a run of letters (a hyphen or a digit splits it).
package readability

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// The defaults and the allowed ranges of the limits (site setting
// ui.readability).
const (
	DefaultSentenceWords = 20
	DefaultWordLetters   = 22
	MinSentenceWords     = 5
	MaxSentenceWords     = 100
	MinWordLetters       = 8
	MaxWordLetters       = 60
)

// The modes of the site (ui.readability.mode) and the personal choice
// (preference "readability").
const (
	ModeOff     = "off"     // nobody sees the hint (the default, also "")
	ModeOffered = "offered" // a person may switch it on in the profile
	ModeOn      = "on"      // on for everybody; a person may switch it off
	PrefOn      = "on"
	PrefOff     = "off"
)

// Modes are the valid modes of the site ("" means off).
var Modes = []string{ModeOff, ModeOffered, ModeOn}

// Active reports whether a person sees the hint: the site decides whether it
// is offered at all, the person within that ("" follows the site).
func Active(siteMode, pref string) bool {
	switch siteMode {
	case ModeOn:
		return pref != PrefOff
	case ModeOffered:
		return pref == PrefOn
	}
	return false
}

var (
	reCode     = regexp.MustCompile("`[^`]*`|\\{[^}]*\\}")
	reURL      = regexp.MustCompile(`https?://[^\s)]*[^\s).,;:]`)
	rePath     = regexp.MustCompile(`[\w.-]*[\w-]/[\w./-]*[\w-]|[\w-]+\.(sh|yaml|json|go|ts|md)\b`)
	reSentence = regexp.MustCompile(`[.!?;:](\s+)|\n+`)
	reWord     = regexp.MustCompile(`[\pL\pN]+`)
	reLetters  = regexp.MustCompile(`\pL+`)
)

// Mask replaces code, placeholders, addresses and paths by "X" (one word).
func Mask(text string) string {
	s := reCode.ReplaceAllString(text, "X")
	s = reURL.ReplaceAllString(s, "X")
	return rePath.ReplaceAllString(s, "X")
}

// Sentences splits a text into its sentences (masked, without the
// punctuation that ends them; empty ones left out).
func Sentences(text string) []string {
	var out []string
	for _, part := range reSentence.Split(Mask(text), -1) {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

// Words counts the words of a sentence (of Sentences).
func Words(sentence string) int { return len(reWord.FindAllString(sentence, -1)) }

// LongWords lists the words of text with more than max letters.
func LongWords(text string, max int) []string {
	var out []string
	for _, w := range reLetters.FindAllString(Mask(text), -1) {
		if utf8.RuneCountInString(w) > max {
			out = append(out, w)
		}
	}
	return out
}
