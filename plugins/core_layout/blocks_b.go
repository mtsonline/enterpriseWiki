package core_layout

// Blocks of priority B, part 1 (plan of blocks and options, 3.1): progress
// bar, counter, timeline, a dialog opened by a button and a tooltip. Each
// one is readable without script; the script only adds the behaviour
// (counting up, the dialog, showing the tooltip). Motion follows "reduced
// motion" and never lasts longer than 5 seconds (WCAG 2.2.2).

import (
	"context"
	"fmt"
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/plugins/core_macros"
)

// number reads a decimal number as written ("1234.5", "1234,5", "-3"): the
// value and how many decimals it shows (at most 3).
func number(v string) (float64, int, error) {
	v = strings.TrimSpace(strings.ReplaceAll(v, ",", "."))
	if v == "" || len(v) > 24 {
		return 0, 0, fmt.Errorf("a number")
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) >= 1e15 {
		return 0, 0, fmt.Errorf("a number")
	}
	dec := 0
	if i := strings.IndexByte(v, '.'); i >= 0 {
		dec = len(v) - i - 1
	}
	if dec > 3 {
		return 0, 0, fmt.Errorf("at most 3 decimals")
	}
	return f, dec, nil
}

// formatNumber writes a number for a language: groups of thousands and the
// decimal mark (German: 1.234,5; English: 1,234.5).
func formatNumber(f float64, dec int, lang string) string {
	group, mark := ",", "."
	if strings.HasPrefix(lang, "de") {
		group, mark = ".", ","
	}
	s := strconv.FormatFloat(math.Abs(f), 'f', dec, 64)
	intPart, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	if f < 0 {
		b.WriteString("−")
	}
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteString(group)
		}
		b.WriteRune(r)
	}
	if frac != "" {
		b.WriteString(mark + frac)
	}
	return b.String()
}

// shortText checks a short text of a block (label, prefix …).
func shortText(name, v string, max int) (string, error) {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > max {
		return "", fmt.Errorf("%s: at most %d characters", name, max)
	}
	return v, nil
}

// ---------------------------------------------------------------- progress bar

type progress struct{}

func (progress) Name() string              { return "progress" }
func (progress) Labels() map[string]string { return lm("Progress bar", "Fortschrittsbalken") }
func (progress) Description() map[string]string {
	return lm("How far something is, as a bar with its name and value.", "Wie weit etwas ist, als Balken mit Namen und Wert.")
}
func (progress) HasBody() bool { return false }
func (progress) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "label", Labels: lm("Name", "Name"), Type: "text", Required: true},
		{Name: "value", Labels: lm("Value", "Wert"), Type: "number", Required: true},
		{Name: "max", Labels: lm("Maximum (100: percent)", "Höchstwert (100: Prozent)"), Type: "number", Default: "100"},
		{Name: "color", Labels: lm("Color", "Farbe"), Type: "select", Options: []string{"primary", "success", "warning", "danger"}, Default: "primary"},
	}
}

func (progress) Render(_ context.Context, mc content.MacroContext) (string, error) {
	label, err := shortText("label", mc.Params["label"], 120)
	if err != nil {
		return "", err
	}
	if label == "" {
		return "", fmt.Errorf("a progress bar needs a name (label)")
	}
	v, dec, err := number(mc.Params["value"])
	if err != nil {
		return "", fmt.Errorf("value: %v", err)
	}
	maxV, maxDec := 100.0, 0
	if m := strings.TrimSpace(mc.Params["max"]); m != "" {
		if maxV, maxDec, err = number(m); err != nil {
			return "", fmt.Errorf("max: %v", err)
		}
	}
	if maxV <= 0 || v < 0 || v > maxV {
		return "", fmt.Errorf("the value must lie between 0 and the maximum (%s)", formatNumber(maxV, maxDec, mc.Lang))
	}
	pct := v / maxV * 100
	text := formatNumber(math.Round(pct), 0, mc.Lang) + " %"
	if maxV != 100 {
		text = formatNumber(v, dec, mc.Lang) + " / " + formatNumber(maxV, maxDec, mc.Lang)
	}
	color := oneOf(mc.Params["color"], []string{"primary", "success", "warning", "danger"})
	// the value is always text beside the bar; the bar only shows it
	return fmt.Sprintf(`<div class="ewl-progress ewl-progress--%s"><p class="ewl-progress-head"><span class="ewl-progress-label">%s</span> <span class="ewl-progress-value">%s</span></p>`+
		`<div class="ewl-progress-track" role="progressbar" aria-label="%s" aria-valuemin="0" aria-valuemax="%s" aria-valuenow="%s" aria-valuetext="%s"><span class="ewl-progress-bar" style="width: %.2f%%"></span></div></div>`,
		color, esc(label), esc(text), esc(label), strconv.FormatFloat(maxV, 'f', -1, 64), strconv.FormatFloat(v, 'f', -1, 64), esc(text), pct), nil
}

// ---------------------------------------------------------------- counter

type counter struct{}

func (counter) Name() string              { return "counter" }
func (counter) Labels() map[string]string { return lm("Counter", "Zähler") }
func (counter) Description() map[string]string {
	return lm("A large number with a name that counts up once when it comes into view (not with reduced motion). The number is always readable as text.",
		"Eine große Zahl mit Namen, die einmal hochzählt, wenn sie sichtbar wird (nicht bei weniger Bewegung). Die Zahl ist immer als Text lesbar.")
}
func (counter) HasBody() bool { return false }
func (counter) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "value", Labels: lm("Number", "Zahl"), Type: "number", Required: true},
		{Name: "label", Labels: lm("Name", "Name"), Type: "text", Required: true},
		{Name: "prefix", Labels: lm("Before the number (e.g. €)", "Vor der Zahl (z. B. €)"), Type: "text"},
		{Name: "suffix", Labels: lm("After the number (e.g. %, km)", "Nach der Zahl (z. B. %, km)"), Type: "text"},
		{Name: "count", Labels: lm("Count up when it comes into view", "Beim Sichtbarwerden hochzählen"), Type: "bool", Default: "true"},
	}
}

func (counter) Render(_ context.Context, mc content.MacroContext) (string, error) {
	v, dec, err := number(mc.Params["value"])
	if err != nil {
		return "", fmt.Errorf("value: %v", err)
	}
	label, err := shortText("label", mc.Params["label"], 120)
	if err != nil {
		return "", err
	}
	if label == "" {
		return "", fmt.Errorf("a counter needs a name (label)")
	}
	pre, err := shortText("prefix", mc.Params["prefix"], 12)
	if err != nil {
		return "", err
	}
	suf, err := shortText("suffix", mc.Params["suffix"], 12)
	if err != nil {
		return "", err
	}
	shown := formatNumber(v, dec, mc.Lang)
	if suf != "" {
		suf = " " + suf // with a space, also before % (as both languages write it)
	}
	widget := ""
	if mc.Params["count"] != "false" {
		widget = fmt.Sprintf(` data-ew-widget="ewl.counter" data-to="%s" data-decimals="%d"`, strconv.FormatFloat(v, 'f', -1, 64), dec)
	}
	// the whole number stays text for screen readers; only a copy hidden
	// from them counts up
	return fmt.Sprintf(`<p class="ewl-counter"%s><span class="ewl-counter-value"><span class="ewl-counter-pre">%s</span><span class="ewl-counter-num">%s</span><span class="ewl-counter-suf">%s</span></span> <span class="ewl-counter-label">%s</span></p>`,
		widget, esc(pre), esc(shown), esc(suf), esc(label)), nil
}

// ---------------------------------------------------------------- timeline

type timeline struct{}

func (timeline) Name() string              { return "timeline" }
func (timeline) Labels() map[string]string { return lm("Timeline", "Zeitleiste") }
func (timeline) Description() map[string]string {
	return lm("Entries in order, each with a date or step as its first heading. Separate the entries with a horizontal line.",
		"Einträge der Reihe nach, jeder mit einem Datum oder Schritt als erster Überschrift. Einträge mit einer Trennlinie trennen.")
}
func (timeline) HasBody() bool { return true }
func (timeline) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "label", Labels: lm("Name for screen readers", "Name für Screenreader"), Type: "text"},
		{Name: "direction", Labels: lm("Direction on the desktop", "Richtung auf dem Desktop"), Type: "select", Options: []string{"vertical", "horizontal"}, Default: "vertical",
			OptionLabels: map[string]map[string]string{"vertical": lm("Top to bottom", "Von oben nach unten"), "horizontal": lm("Left to right (on the phone top to bottom)", "Von links nach rechts (auf dem Handy von oben nach unten)")}},
	}
}

func (timeline) Render(_ context.Context, mc content.MacroContext) (string, error) {
	parts := core_macros.SplitOnHR(mc.Body)
	if len(parts) == 0 {
		return "", fmt.Errorf("separate the entries with a horizontal line")
	}
	if len(parts) > 50 {
		return "", fmt.Errorf("at most 50 entries")
	}
	label, err := shortText("label", mc.Params["label"], 120)
	if err != nil {
		return "", err
	}
	dir := oneOf(mc.Params["direction"], []string{"vertical", "horizontal"})
	var b strings.Builder
	// a horizontal timeline can scroll in itself: then it is a region the
	// keyboard reaches
	if dir == "horizontal" {
		if label == "" {
			return "", fmt.Errorf("a horizontal timeline needs a name for screen readers (label): it can scroll in itself")
		}
		fmt.Fprintf(&b, `<div class="ewl-timeline-scroll" role="region" aria-label="%s" tabindex="0">`, esc(label))
	}
	attr := ""
	if label != "" && dir == "vertical" {
		attr = fmt.Sprintf(` aria-label="%s"`, esc(label))
	}
	fmt.Fprintf(&b, `<ol class="ewl-timeline ewl-timeline--%s"%s>`, dir, attr)
	for _, p := range parts {
		when, rest := firstHeading(p)
		b.WriteString(`<li class="ewl-timeline-entry">`)
		if when != "" {
			fmt.Fprintf(&b, `<p class="ewl-timeline-when">%s</p>`, esc(when))
		}
		b.WriteString(`<div class="ewl-timeline-body">` + rest + `</div></li>`)
	}
	b.WriteString(`</ol>`)
	if dir == "horizontal" {
		b.WriteString(`</div>`)
	}
	return b.String(), nil
}

// ---------------------------------------------------------------- dialog

type dialog struct{}

func (dialog) Name() string              { return "dialog" }
func (dialog) Labels() map[string]string { return lm("Dialog (button)", "Dialog (Knopf)") }
func (dialog) Description() map[string]string {
	return lm("A button that opens the content in a dialog. It never opens by itself; Escape closes it and the focus returns to the button.",
		"Ein Knopf, der den Inhalt in einem Dialog öffnet. Er öffnet sich nie von selbst; Escape schließt ihn, der Fokus kehrt zum Knopf zurück.")
}
func (dialog) HasBody() bool { return true }
func (dialog) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "button", Labels: lm("Button text", "Schaltflächentext"), Type: "text", Required: true},
		{Name: "title", Labels: lm("Title of the dialog", "Titel des Dialogs"), Type: "text", Required: true},
		{Name: "icon", Labels: lm("Icon of the button", "Symbol der Schaltfläche"), Type: "custom:icon"},
		{Name: "style", Labels: lm("Style of the button", "Stil der Schaltfläche"), Type: "select", Options: []string{"primary", "secondary", "link"}, Default: "secondary"},
	}
}

func (dialog) Render(_ context.Context, mc content.MacroContext) (string, error) {
	text, err := shortText("button", mc.Params["button"], 80)
	if err != nil {
		return "", err
	}
	title, err := shortText("title", mc.Params["title"], 120)
	if err != nil {
		return "", err
	}
	if text == "" || title == "" {
		return "", fmt.Errorf("a dialog needs a button text and a title")
	}
	if strings.TrimSpace(mc.Body) == "" {
		return "", fmt.Errorf("a dialog needs content")
	}
	icon, err := buttonIcon(mc.Params["icon"])
	if err != nil {
		return "", err
	}
	style := oneOf(mc.Params["style"], []string{"secondary", "primary", "link"})
	// without script: content to open and close in place; the browser makes
	// a button and a modal dialog of it
	return fmt.Sprintf(`<details class="ewl-dialog" data-ew-widget="ewl.dialog"><summary class="ewl-cta ewl-cta--%s">%s%s</summary><div class="ewl-dialog-content"><p class="ewl-dialog-title">%s</p><div class="ewl-dialog-body">%s</div></div></details>`,
		style, icon, esc(text), esc(title), mc.Body), nil
}

// ---------------------------------------------------------------- tooltip

type tooltip struct{}

func (tooltip) Name() string              { return "tooltip" }
func (tooltip) Labels() map[string]string { return lm("Tooltip", "Hinweisblase") }
func (tooltip) Description() map[string]string {
	return lm("A word with an explanation that shows on pointing, focus and tapping; Escape hides it (WCAG 1.4.13). Without script the explanation stands in brackets.",
		"Ein Wort mit einer Erklärung, die beim Zeigen, beim Fokus und beim Tippen erscheint; Escape blendet sie aus (WCAG 1.4.13). Ohne Skript steht die Erklärung in Klammern.")
}
func (tooltip) HasBody() bool   { return false }
func (tooltip) Inline() bool    { return true }
func (tooltip) Toolbar() string { return "ⓘ" }
func (tooltip) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "text", Labels: lm("Word", "Wort"), Type: "text", Required: true},
		{Name: "tip", Labels: lm("Explanation", "Erklärung"), Type: "textarea", Required: true},
	}
}

func (tooltip) Render(_ context.Context, mc content.MacroContext) (string, error) {
	word, err := shortText("text", mc.Params["text"], 80)
	if err != nil {
		return "", err
	}
	tip, err := shortText("tip", mc.Params["tip"], 500)
	if err != nil {
		return "", err
	}
	if word == "" || tip == "" {
		return "", fmt.Errorf("a tooltip needs a word and an explanation")
	}
	return fmt.Sprintf(`<span class="ewl-tip" data-ew-widget="ewl.tip"><span class="ewl-tip-term">%s</span><span class="ewl-tip-text"> (%s)</span></span>`, esc(word), esc(tip)), nil
}

// ---------------------------------------------------------------- quote (testimonial)

type testimonial struct{}

func (testimonial) Name() string              { return "testimonial" }
func (testimonial) Labels() map[string]string { return lm("Quote of a person", "Zitat einer Person") }
func (testimonial) Description() map[string]string {
	return lm("What someone says, with their name, role and optionally a picture of the wiki (a testimonial).",
		"Was jemand sagt, mit Name, Funktion und wahlweise einem Bild aus dem Wiki (Kundenstimme).")
}
func (testimonial) HasBody() bool { return true }
func (testimonial) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "name", Labels: lm("Name", "Name"), Type: "text", Required: true},
		{Name: "role", Labels: lm("Role or organisation", "Funktion oder Organisation"), Type: "text"},
		{Name: "image", Labels: lm("Picture (from the wiki)", "Bild (aus dem Wiki)"), Type: "custom:image"},
	}
}

func (testimonial) Render(_ context.Context, mc content.MacroContext) (string, error) {
	name, err := shortText("name", mc.Params["name"], 120)
	if err != nil {
		return "", err
	}
	role, err := shortText("role", mc.Params["role"], 160)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", fmt.Errorf("a quote needs the name of the person")
	}
	if strings.TrimSpace(mc.Body) == "" {
		return "", fmt.Errorf("a quote needs a text")
	}
	img := strings.TrimSpace(mc.Params["image"])
	if img != "" && !content.WikiFile(img) {
		return "", fmt.Errorf("image: a file of the wiki (/api/v1/files/… or /api/v1/media/ID/view)")
	}
	var b strings.Builder
	b.WriteString(`<figure class="ewl-quote"><blockquote class="ewl-quote-text">` + mc.Body + `</blockquote><figcaption class="ewl-quote-who">`)
	if img != "" {
		// the name stands beside it: the picture is decoration
		fmt.Fprintf(&b, `<img class="ewl-quote-img" src="%s" alt="" loading="lazy" width="56" height="56">`, esc(img))
	}
	fmt.Fprintf(&b, `<span><span class="ewl-quote-name">%s</span>`, esc(name))
	if role != "" {
		fmt.Fprintf(&b, `<span class="ewl-quote-role">%s</span>`, esc(role))
	}
	b.WriteString(`</span></figcaption></figure>`)
	return b.String(), nil
}

// ---------------------------------------------------------------- logo bar

type logos struct{}

func (logos) Name() string              { return "logos" }
func (logos) Labels() map[string]string { return lm("Logo bar", "Logo-Leiste") }
func (logos) Description() map[string]string {
	return lm("Logos side by side (for example partners): put the pictures in the content, each with its name as text alternative and optionally as a link.",
		"Logos nebeneinander (zum Beispiel Partner): Bilder in den Inhalt setzen, jedes mit seinem Namen als Alternativtext und wahlweise als Link.")
}
func (logos) HasBody() bool { return true }
func (logos) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "label", Labels: lm("Name for screen readers (e.g. Our partners)", "Name für Screenreader (z. B. Unsere Partner)"), Type: "text", Required: true},
		{Name: "grey", Labels: lm("In grey, in colour when pointed at or focused", "In Grau, farbig beim Zeigen oder mit dem Fokus"), Type: "bool"},
		{Name: "size", Labels: lm("Height", "Höhe"), Type: "select", Options: []string{"small", "medium", "large"}, Default: "medium"},
	}
}

func (logos) Render(_ context.Context, mc content.MacroContext) (string, error) {
	label, err := shortText("label", mc.Params["label"], 120)
	if err != nil {
		return "", err
	}
	if label == "" {
		return "", fmt.Errorf("a logo bar needs a name for screen readers (label)")
	}
	items := logoItems(mc.Body)
	if len(items) == 0 {
		return "", fmt.Errorf("put the logos as pictures into the logo bar")
	}
	if len(items) > 40 {
		return "", fmt.Errorf("at most 40 logos")
	}
	for _, it := range items {
		if !it.hasAlt {
			return "", fmt.Errorf("every logo needs its name as text alternative")
		}
		if !content.WikiFile(it.src) {
			return "", fmt.Errorf("logos come from the wiki itself (upload them to the media library)")
		}
	}
	cls := "ewl-logos ewl-logos--" + oneOf(mc.Params["size"], []string{"medium", "small", "large"})
	if mc.Params["grey"] == "true" {
		cls += " ewl-logos--grey"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<ul class="%s" aria-label="%s">`, cls, esc(label))
	for _, it := range items {
		b.WriteString(`<li>` + it.html + `</li>`)
	}
	b.WriteString(`</ul>`)
	return b.String(), nil
}

type logoItem struct {
	html   string
	src    string
	hasAlt bool
}

var (
	logoRe = regexp.MustCompile(`(?is)<a\b[^>]*>\s*<img\b[^>]*>\s*</a>|<img\b[^>]*>`)
	altRe  = regexp.MustCompile(`(?is)\balt\s*=\s*"\s*[^"\s][^"]*"`)
	srcRe  = regexp.MustCompile(`(?is)<img\b[^>]*\bsrc\s*=\s*"([^"]*)"`)
)

// logoItems: the pictures (alone or as a link) of the cleaned content of
// the block, in order; everything else is left out.
func logoItems(body string) []logoItem {
	var out []logoItem
	for _, m := range logoRe.FindAllString(body, -1) {
		it := logoItem{html: m, hasAlt: altRe.MatchString(m)}
		if sm := srcRe.FindStringSubmatch(m); sm != nil {
			it.src = html.UnescapeString(sm[1])
		}
		out = append(out, it)
	}
	return out
}
