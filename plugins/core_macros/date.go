package core_macros

// A date in the text (plan of blocks, 3.2): a real date, written for the
// language of the page and readable by machines (<time datetime>).

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

type date struct{}

func (date) Name() string              { return "date" }
func (date) Group() string             { return "text" }
func (date) Labels() map[string]string { return lm("Date", "Datum") }
func (date) Description() map[string]string {
	return lm("A date in the text, written for the language of the page (for example 22 November 2026).",
		"Ein Datum im Text, geschrieben für die Sprache der Seite (zum Beispiel 22. November 2026).")
}
func (date) HasBody() bool { return false }
func (date) Inline() bool  { return true }
func (date) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "value", Labels: lm("Date", "Datum"), Type: "date", Required: true},
		{Name: "format", Labels: lm("Written as", "Schreibweise"), Type: "select", Options: []string{"long", "short", "weekday"}, Default: "long",
			OptionLabels: map[string]map[string]string{
				"long":    lm("22 November 2026", "22. November 2026"),
				"short":   lm("2026-11-22", "22.11.2026"),
				"weekday": lm("Sunday, 22 November 2026", "Sonntag, 22. November 2026"),
			}},
	}
}

var (
	monthsEN   = [...]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	monthsDE   = [...]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
	weekdaysEN = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	weekdaysDE = [...]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}
)

// FormatDate writes d for lang (de: German, else English) as long, short or
// weekday.
func FormatDate(d time.Time, lang, format string) string {
	de := strings.HasPrefix(lang, "de")
	switch format {
	case "short":
		if de {
			return d.Format("02.01.2006")
		}
		return d.Format("2006-01-02")
	case "weekday":
		if de {
			return fmt.Sprintf("%s, %d. %s %d", weekdaysDE[d.Weekday()], d.Day(), monthsDE[d.Month()-1], d.Year())
		}
		return fmt.Sprintf("%s, %d %s %d", weekdaysEN[d.Weekday()], d.Day(), monthsEN[d.Month()-1], d.Year())
	}
	if de {
		return fmt.Sprintf("%d. %s %d", d.Day(), monthsDE[d.Month()-1], d.Year())
	}
	return fmt.Sprintf("%d %s %d", d.Day(), monthsEN[d.Month()-1], d.Year())
}

func (date) Render(_ context.Context, mc content.MacroContext) (string, error) {
	v := strings.TrimSpace(mc.Params["value"])
	d, err := time.Parse("2006-01-02", v)
	if err != nil || d.Year() < 1 || d.Year() > 9999 {
		return "", apperr.BadRequest("the date: a day as YYYY-MM-DD, for example 2026-11-22")
	}
	f := mc.Params["format"]
	if f != "short" && f != "weekday" {
		f = "long"
	}
	return fmt.Sprintf(`<time class="ew-date" datetime="%s">%s</time>`, d.Format("2006-01-02"), esc(FormatDate(d, mc.Lang, f))), nil
}
