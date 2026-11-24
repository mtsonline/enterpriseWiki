package core_macros

// A date in the text (plan of blocks, 3.2): a real date, written for the
// language of the page and readable by machines (<time datetime>). With a
// title it is an appointment (plan "Aufgaben-Bericht", step 2): kept as
// data of the kind core.macros.date in the common fields of points in time
// (content.TimeFields), shown by the report of dates and by calendars that
// take the page or its category as a source. A time is the wall clock of
// the wiki's time zone (the one of the e-mail digests).

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

// DateKind is the kind of data of appointments in the text.
const DateKind = "core.macros.date"

// DateStates are the states of an appointment.
var DateStates = []string{"planned", "cancelled"}

// DateDataKind is the schema of appointments in the text.
var DateDataKind = content.DataKind{
	Kind: DateKind, Version: 1, Labels: lm("Dates in pages", "Termine in Seiten"),
	Fields: content.TimeFields(DateStates, map[string]map[string]string{
		"planned":   lm("Planned", "Geplant"),
		"cancelled": lm("Cancelled", "Abgesagt"),
	}),
	MaxPerElement: 500,
}

type date struct {
	// loc is the time zone of the wiki (nil: the server's)
	loc func() *time.Location
}

func (d date) location() *time.Location {
	if d.loc != nil {
		if l := d.loc(); l != nil {
			return l
		}
	}
	return time.Local
}

var clockRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

// appointment checks the parameters of a date: the day, and – for an
// appointment – its title, start and end (end without start is refused).
func appointment(p map[string]string) (day time.Time, title, start, end string, err error) {
	v := strings.TrimSpace(p["value"])
	day, err = time.Parse("2006-01-02", v)
	if err != nil || day.Year() < 1 || day.Year() > 9999 {
		return day, "", "", "", apperr.BadRequest("the date: a day as YYYY-MM-DD, for example 2026-11-22")
	}
	title = strings.Join(strings.Fields(p["label"]), " ")
	if utf8.RuneCountInString(title) > 300 {
		return day, "", "", "", apperr.BadRequest("the title of the date: at most %d characters", 300)
	}
	start, end = strings.TrimSpace(p["time"]), strings.TrimSpace(p["end"])
	if start != "" && !clockRe.MatchString(start) || end != "" && !clockRe.MatchString(end) {
		return day, "", "", "", apperr.BadRequest("times: HH:MM, for example 09:30")
	}
	if end != "" && (start == "" || end <= start) {
		return day, "", "", "", apperr.BadRequest("the end: after the start on the same day")
	}
	return day, title, start, end, nil
}

// DataKind implements content.DataMacro.
func (date) DataKind() string { return DateKind }

// Data implements content.DataMacro: a date with a title is an appointment.
func (d date) Data(_ context.Context, u content.DataUse) (map[string]any, error) {
	day, title, start, end, err := appointment(u.Params)
	if err != nil || title == "" {
		return nil, err
	}
	out := map[string]any{"title": title, "day": day.Format("2006-01-02"), "state": "planned"}
	at := func(clock string) string {
		t, _ := time.ParseInLocation("2006-01-02 15:04", day.Format("2006-01-02")+" "+clock, d.location())
		return t.UTC().Format(time.RFC3339)
	}
	if start != "" {
		out["at"] = at(start)
	}
	if end != "" {
		out["end"] = at(end)
	}
	return out, nil
}

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
		{Name: "label", Labels: lm("Title of the appointment (makes it an appointment for the report of dates and calendars)", "Titel des Termins (macht ihn zu einem Termin für den Termin-Bericht und Kalender)"), Type: "text"},
		{Name: "time", Labels: lm("Starts at", "Beginn"), Type: "time"},
		{Name: "end", Labels: lm("Ends at", "Ende"), Type: "time"},
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

func (d date) Render(_ context.Context, mc content.MacroContext) (string, error) {
	day, _, start, end, err := appointment(mc.Params)
	if err != nil {
		return "", err
	}
	f := mc.Params["format"]
	if f != "short" && f != "weekday" {
		f = "long"
	}
	text, machine := FormatDate(day, mc.Lang, f), day.Format("2006-01-02")
	if start != "" {
		// the time of the wiki, also machine-readable with its offset
		t, _ := time.ParseInLocation("2006-01-02 15:04", machine+" "+start, d.location())
		machine = t.Format("2006-01-02T15:04-07:00")
		text += ", " + start
		if end != "" {
			text += "–" + end
		}
		if strings.HasPrefix(mc.Lang, "de") {
			text += " Uhr"
		}
	}
	return fmt.Sprintf(`<time class="ew-date" datetime="%s">%s</time>`, machine, esc(text)), nil
}
