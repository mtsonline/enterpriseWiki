package core_template

// Own CSS of a template: only when the administration
// allows it (ui.custom_css, default off). It is checked on saving, never
// changed silently: what is refused is named with its line. The rules are in
// limits.json (section css), the designer checks with the same file.

import (
	"regexp"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

var (
	cssForbidden = func() []struct {
		re   *regexp.Regexp
		what string
	} {
		out := []struct {
			re   *regexp.Regexp
			what string
		}{}
		for _, f := range limits.CSS.Forbidden {
			out = append(out, struct {
				re   *regexp.Regexp
				what string
			}{regexp.MustCompile(f[0]), f[1]})
		}
		return out
	}()
	cssURLAllowed = regexp.MustCompile(limits.CSS.URL)
	cssURLRe      = regexp.MustCompile(`url\s*\(\s*(['"]?)([^'")]*)(['"]?)\s*\)`)
	cssCommentRe  = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// cssProblems checks own CSS: size, no comment left open, nothing that
// loads or runs from elsewhere (@import, foreign url(), image-set() …), no
// escapes that could hide such a thing.
func cssProblems(css string, add func(where, format string, args ...any)) {
	if css == "" {
		return
	}
	if len(css) > limits.CSS.MaxBytes {
		add("css", "own CSS has at most %d bytes", limits.CSS.MaxBytes)
		return
	}
	// comments are removed first (they could split a word the browser joins);
	// a comment left open would hide the rest
	text := cssCommentRe.ReplaceAllStringFunc(css, func(c string) string {
		return strings.Repeat("\n", strings.Count(c, "\n")) // keep the lines for the messages
	})
	if strings.Contains(text, "/*") {
		add("css", "line %d: a comment is not closed", line(text, strings.Index(text, "/*")))
		return
	}
	low := strings.ToLower(text)
	for _, f := range cssForbidden {
		if loc := f.re.FindStringIndex(low); loc != nil {
			add("css", "line %d: %s is not allowed in own CSS", line(low, loc[0]), f.what)
		}
	}
	for _, m := range cssURLRe.FindAllStringSubmatchIndex(low, -1) {
		u := strings.TrimSpace(low[m[4]:m[5]])
		if low[m[2]:m[3]] != low[m[6]:m[7]] || !cssURLAllowed.MatchString(u) {
			add("css", "line %d: url() only to files of the wiki (/api/v1/files/… or /api/v1/media/ID/view), not %q", line(low, m[0]), u)
		}
	}
}

func line(s string, at int) int { return strings.Count(s[:at], "\n") + 1 }

// requireCSSAllowed refuses own CSS while the administration has not
// switched it on.
func requireCSSAllowed(d *Design, allowed bool) error {
	if d.CSS != "" && !allowed {
		return apperr.BadRequest("own CSS is switched off (setting ui.custom_css); remove it from the template or ask the administration")
	}
	return nil
}
