package core_layout

// Blocks of priority C, part 1 (plan of blocks, 3.1): price table, picture
// with hotspots, before and after, countdown. Each is readable content
// without script (plans one beside the other, numbered points with their
// texts, both pictures with captions, the date); the script only adds the
// slider of before and after and the time left. Nothing is loaded from other
// servers.

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/plugins/core_macros"
)

// tr picks the text of the language of the page.
func tr(lang, en, de string) string {
	if lang == "de" {
		return de
	}
	return en
}

// ---------------------------------------------------------------- price table

type pricing struct{}

func (pricing) Name() string              { return "pricing" }
func (pricing) Group() string             { return "layout" }
func (pricing) Labels() map[string]string { return lm("Price table", "Preis-Tabelle") }
func (pricing) Description() map[string]string {
	return lm("Plans side by side: per plan a heading (its name), the price, a list of what it includes and a button. One plan can be highlighted with a label (never by colour alone).",
		"Angebote nebeneinander: je Angebot eine Überschrift (sein Name), der Preis, eine Liste mit dem, was es enthält, und ein Knopf. Ein Angebot lässt sich mit einer Beschriftung hervorheben (nie nur mit Farbe).")
}
func (pricing) HasBody() bool { return true }
func (pricing) Parts() content.MacroParts {
	return content.MacroParts{Labels: lm("Plan", "Angebot"), Titled: true}
}
func (pricing) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "featured", Labels: lm("Highlighted plan", "Hervorgehobenes Angebot"), Type: "select", Options: []string{"none", "1", "2", "3", "4"}, Default: "none",
			OptionLabels: map[string]map[string]string{"none": lm("None", "Keines")}},
		{Name: "badge", Labels: lm("Label of the highlighted plan (e.g. Recommended)", "Beschriftung des hervorgehobenen Angebots (z. B. Empfohlen)"), Type: "text"},
	}
}

func (pricing) Render(_ context.Context, mc content.MacroContext) (string, error) {
	parts := core_macros.SplitOnHR(mc.Body)
	if len(parts) < 1 || len(parts) > 4 {
		return "", apperr.BadRequest("1 to 4 plans, separated by a horizontal line")
	}
	badge, err := shortText("badge", mc.Params["badge"], 40)
	if err != nil {
		return "", err
	}
	featured := 0
	if f := oneOf(mc.Params["featured"], []string{"none", "1", "2", "3", "4"}); f != "none" {
		featured, _ = strconv.Atoi(f)
		if featured > len(parts) {
			return "", apperr.BadRequest("featured: there are only %d plans", len(parts))
		}
		if badge == "" {
			badge = tr(mc.Lang, "Recommended", "Empfohlen")
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewl-pricing ewl-pricing--%d">`, len(parts))
	for i, p := range parts {
		cls := "ewl-plan"
		if i+1 == featured {
			cls += " ewl-plan--featured"
		}
		b.WriteString(`<div class="` + cls + `">`)
		if i+1 == featured {
			// the highlight is a word, not only a colour (WCAG 1.4.1)
			b.WriteString(`<p class="ewl-plan-badge">` + esc(badge) + `</p>`)
		}
		b.WriteString(p + `</div>`)
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- hotspots

type hotspots struct{}

func (hotspots) Name() string              { return "hotspots" }
func (hotspots) Group() string             { return "media" }
func (hotspots) Labels() map[string]string { return lm("Picture with hotspots", "Bild mit Hotspots") }
func (hotspots) Description() map[string]string {
	return lm("Numbered points on a picture. Each entry of the list is \"x/y: text\" (x and y in percent from the top left corner). The texts always stand numbered below the picture; a point leads to its text.",
		"Nummerierte Punkte auf einem Bild. Jeder Eintrag der Liste ist „x/y: Text“ (x und y in Prozent von der linken oberen Ecke). Die Texte stehen immer nummeriert unter dem Bild; ein Punkt führt zu seinem Text.")
}
func (hotspots) HasBody() bool { return true }
func (hotspots) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "image", Labels: lm("Picture (from the wiki)", "Bild (aus dem Wiki)"), Type: "custom:image", Required: true},
		{Name: "alt", Labels: lm("Description of the picture", "Beschreibung des Bildes"), Type: "text", Required: true},
	}
}

var hotspotRe = regexp.MustCompile(`^\s*(?:<p>\s*)?(\d{1,3})\s*/\s*(\d{1,3})\s*:\s*`)

func (hotspots) Render(_ context.Context, mc content.MacroContext) (string, error) {
	img := strings.TrimSpace(mc.Params["image"])
	if !content.WikiFile(img) {
		return "", apperr.BadRequest("image: a file of the wiki (/api/v1/files/… or /api/v1/media/ID/view)")
	}
	alt, err := shortText("alt", mc.Params["alt"], 300)
	if err != nil {
		return "", err
	}
	if alt == "" {
		return "", apperr.BadRequest("a picture with hotspots needs a description (alt)")
	}
	items := listItemRe.FindAllString(mc.Body, -1)
	if len(items) == 0 {
		return "", apperr.BadRequest("put the points as a list into the block: x/y: text")
	}
	if len(items) > 20 {
		return "", apperr.BadRequest("at most 20 points")
	}
	point := tr(mc.Lang, "Point", "Punkt")
	var marks, list strings.Builder
	for i, it := range items {
		open := strings.Index(it, ">") + 1
		inner := it[open : len(it)-len("</li>")]
		m := hotspotRe.FindStringSubmatchIndex(inner)
		if m == nil {
			return "", apperr.BadRequest("point %d: write x/y: text (x and y from 0 to 100)", i+1)
		}
		x, _ := strconv.Atoi(inner[m[2]:m[3]])
		y, _ := strconv.Atoi(inner[m[4]:m[5]])
		if x > 100 || y > 100 {
			return "", apperr.BadRequest("point %d: x and y from 0 to 100", i+1)
		}
		// keep an opening <p> that the coordinates followed
		text := inner[m[1]:]
		if strings.HasPrefix(strings.TrimSpace(inner), "<p>") {
			text = "<p>" + text
		}
		id := fmt.Sprintf("ewl-hs-%d-%d", mc.Index, i+1)
		// a link with the number (and the word for screen readers) leads to the text
		fmt.Fprintf(&marks, `<a class="ewl-hotspot" href="#%s" style="left: %d%%; top: %d%%"><span class="sr-only">%s </span>%d</a>`, id, x, y, esc(point), i+1)
		fmt.Fprintf(&list, `<li id="%s">%s</li>`, id, text)
	}
	return fmt.Sprintf(`<figure class="ewl-hotspots"><div class="ewl-hotspots-pic"><img src="%s" alt="%s" loading="lazy">%s</div><figcaption><ol class="ewl-hotspots-list">%s</ol></figcaption></figure>`,
		esc(img), esc(alt), marks.String(), list.String()), nil
}

// ---------------------------------------------------------------- before and after

type compare struct{}

func (compare) Name() string              { return "compare" }
func (compare) Group() string             { return "media" }
func (compare) Labels() map[string]string { return lm("Before and after", "Vorher-Nachher-Bild") }
func (compare) Description() map[string]string {
	return lm("Two pictures of the same size over each other, with a slider between them (mouse, touch or arrow keys). Without script both stand side by side with their captions.",
		"Zwei gleich große Bilder übereinander, mit einem Regler dazwischen (Maus, Finger oder Pfeiltasten). Ohne Skript stehen beide mit ihrer Beschriftung nebeneinander.")
}
func (compare) HasBody() bool { return false }
func (compare) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "before", Labels: lm("Picture before (from the wiki)", "Bild vorher (aus dem Wiki)"), Type: "custom:image", Required: true},
		{Name: "before_alt", Labels: lm("Description of the picture before", "Beschreibung des Bildes vorher"), Type: "text", Required: true},
		{Name: "after", Labels: lm("Picture after (from the wiki)", "Bild nachher (aus dem Wiki)"), Type: "custom:image", Required: true},
		{Name: "after_alt", Labels: lm("Description of the picture after", "Beschreibung des Bildes nachher"), Type: "text", Required: true},
		{Name: "before_label", Labels: lm("Caption before (default: Before)", "Beschriftung vorher (Vorgabe: Vorher)"), Type: "text"},
		{Name: "after_label", Labels: lm("Caption after (default: After)", "Beschriftung nachher (Vorgabe: Nachher)"), Type: "text"},
		{Name: "start", Labels: lm("Start of the slider (percent)", "Start des Reglers (Prozent)"), Type: "number", Default: "50"},
	}
}

func (compare) Render(_ context.Context, mc content.MacroContext) (string, error) {
	var pics [2]struct{ src, alt, label string }
	for i, k := range []string{"before", "after"} {
		src := strings.TrimSpace(mc.Params[k])
		if !content.WikiFile(src) {
			return "", apperr.BadRequest("%s: a file of the wiki (/api/v1/files/… or /api/v1/media/ID/view)", k)
		}
		alt, err := shortText(k+"_alt", mc.Params[k+"_alt"], 300)
		if err != nil {
			return "", err
		}
		if alt == "" {
			return "", apperr.BadRequest("%s_alt: each picture needs a description", k)
		}
		label, err := shortText(k+"_label", mc.Params[k+"_label"], 40)
		if err != nil {
			return "", err
		}
		if label == "" {
			label = [2]string{tr(mc.Lang, "Before", "Vorher"), tr(mc.Lang, "After", "Nachher")}[i]
		}
		pics[i].src, pics[i].alt, pics[i].label = src, alt, label
	}
	start := 50
	if s := strings.TrimSpace(mc.Params["start"]); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 || n > 100 {
			return "", apperr.BadRequest("start: a whole number from 0 to 100")
		}
		start = n
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewl-compare" data-ew-widget="ewl.compare" data-start="%d">`, start)
	for i, p := range pics {
		fmt.Fprintf(&b, `<figure class="ewl-compare-%s"><img src="%s" alt="%s" loading="lazy"><figcaption>%s</figcaption></figure>`,
			[2]string{"before", "after"}[i], esc(p.src), esc(p.alt), esc(p.label))
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- countdown

type countdown struct {
	// loc is the time zone of the wiki (nil: the server's)
	loc func() *time.Location
}

func (countdown) Name() string              { return "countdown" }
func (countdown) Group() string             { return "text" }
func (countdown) Labels() map[string]string { return lm("Countdown", "Countdown") }
func (countdown) Description() map[string]string {
	return lm("The time left until a day and time of the wiki's time zone, in days, hours and minutes (updated each minute, with a pause button). The date itself always stands there; once it is reached, the text for afterwards is shown.",
		"Die Zeit bis zu einem Tag und einer Uhrzeit in der Zeitzone des Wikis, in Tagen, Stunden und Minuten (jede Minute neu, mit Pause-Knopf). Das Datum selbst steht immer da; ist es erreicht, erscheint der Text für danach.")
}
func (countdown) HasBody() bool { return false }
func (countdown) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "date", Labels: lm("Day", "Tag"), Type: "date", Required: true},
		{Name: "time", Labels: lm("Time", "Uhrzeit"), Type: "time"},
		{Name: "label", Labels: lm("Text before (e.g. Until the opening)", "Text davor (z. B. Bis zur Eröffnung)"), Type: "text"},
		{Name: "reached", Labels: lm("Text once reached", "Text, wenn erreicht"), Type: "text"},
	}
}

var monthsEN = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}

func (c countdown) Render(_ context.Context, mc content.MacroContext) (string, error) {
	loc := time.Local
	if c.loc != nil {
		if l := c.loc(); l != nil {
			loc = l
		}
	}
	day := strings.TrimSpace(mc.Params["date"])
	clock := strings.TrimSpace(mc.Params["time"])
	if clock == "" {
		clock = "00:00"
	}
	when, err := time.ParseInLocation("2006-01-02 15:04", day+" "+clock, loc)
	if err != nil || when.Year() < 1970 || when.Year() > 9999 {
		return "", apperr.BadRequest("a countdown needs a day (YYYY-MM-DD) and optionally a time (HH:MM)")
	}
	label, err := shortText("label", mc.Params["label"], 120)
	if err != nil {
		return "", err
	}
	reached, err := shortText("reached", mc.Params["reached"], 200)
	if err != nil {
		return "", err
	}
	shown := fmt.Sprintf("%d %s %d, %s", when.Day(), monthsEN[when.Month()-1], when.Year(), when.Format("15:04"))
	if mc.Lang == "de" {
		shown = fmt.Sprintf("%d.%d.%d, %s Uhr", when.Day(), int(when.Month()), when.Year(), when.Format("15:04"))
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewl-countdown" data-ew-widget="ewl.countdown" data-to="%s"`, when.UTC().Format(time.RFC3339))
	if reached != "" {
		fmt.Fprintf(&b, ` data-reached="%s"`, esc(reached))
	}
	b.WriteString(`>`)
	if label != "" {
		b.WriteString(`<p class="ewl-countdown-label">` + esc(label) + `</p>`)
	}
	fmt.Fprintf(&b, `<p class="ewl-countdown-when"><time datetime="%s">%s</time></p></div>`, when.Format(time.RFC3339), esc(shown))
	return b.String(), nil
}
