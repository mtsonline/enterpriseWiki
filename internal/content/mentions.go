package content

import (
	"regexp"
	"strings"
)

// mentionRe finds @name and @name@provider (after a space, a bracket, a
// quote or punctuation, so e-mail addresses do not count).
var mentionRe = regexp.MustCompile(`(?:^|[\s(\[{>"'„“,;:])@([\p{L}\p{N}][\p{L}\p{N}._-]{0,63}(?:@[\p{L}\p{N}._-]{1,64})?)`)

// Mentions returns the people mentioned in a text (@name, @name@provider;
// lower case, without duplicates, at most max).
func Mentions(text string, max int) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		h := strings.TrimRight(strings.ToLower(m[1]), ".-_")
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
		if len(out) >= max {
			break
		}
	}
	return out
}
