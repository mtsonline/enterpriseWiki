package core_layout

// Blocks of priority B, part 3 (plan of blocks, 3.1): a card of a person,
// a heading with changing words, a ticker and a slider of content. Motion
// follows the rules of the catalogue (6.7): it starts only without "reduced
// motion", it stops while pointed at or focused, there is always a visible
// button to pause it (WCAG 2.2.2), and the whole text is always there for
// screen readers. Without script everything is plain, readable content.

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/plugins/core_macros"
)

// ---------------------------------------------------------------- person card

type personcard struct{}

func (personcard) Name() string              { return "personcard" }
func (personcard) Group() string             { return "media" }
func (personcard) Labels() map[string]string { return lm("Person card", "Personenkarte") }
func (personcard) Description() map[string]string {
	return lm("A person with picture, name, role and ways to reach them (e-mail, phone); the content is a short text about them.",
		"Eine Person mit Bild, Name, Funktion und Kontaktwegen (E-Mail, Telefon); der Inhalt ist ein kurzer Text über sie.")
}
func (personcard) HasBody() bool { return true }
func (personcard) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "name", Labels: lm("Name", "Name"), Type: "text", Required: true},
		{Name: "role", Labels: lm("Role", "Funktion"), Type: "text"},
		{Name: "image", Labels: lm("Picture (from the wiki)", "Bild (aus dem Wiki)"), Type: "custom:image"},
		{Name: "email", Labels: lm("E-mail", "E-Mail"), Type: "text"},
		{Name: "phone", Labels: lm("Phone", "Telefon"), Type: "text"},
		{Name: "link", Labels: lm("Link (e.g. the profile page)", "Link (z. B. die Profilseite)"), Type: "url"},
	}
}

func (personcard) Render(_ context.Context, mc content.MacroContext) (string, error) {
	name, err := shortText("name", mc.Params["name"], 120)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", apperr.BadRequest("a person card needs a name")
	}
	role, err := shortText("role", mc.Params["role"], 160)
	if err != nil {
		return "", err
	}
	img := strings.TrimSpace(mc.Params["image"])
	if img != "" && !content.WikiFile(img) {
		return "", apperr.BadRequest("image: a file of the wiki (/api/v1/files/… or /api/v1/media/ID/view)")
	}
	email := strings.TrimSpace(mc.Params["email"])
	if email != "" && !emailRe.MatchString(email) {
		return "", apperr.BadRequest("email: an e-mail address")
	}
	phone := strings.TrimSpace(mc.Params["phone"])
	if phone != "" && !phoneRe.MatchString(phone) {
		return "", apperr.BadRequest("phone: digits, spaces and + ( ) / - only")
	}
	var b strings.Builder
	b.WriteString(`<div class="ewl-person">`)
	if img != "" {
		// the name follows: the picture is decoration
		fmt.Fprintf(&b, `<img class="ewl-person-img" src="%s" alt="" loading="lazy" width="96" height="96">`, esc(img))
	}
	b.WriteString(`<div class="ewl-person-text"><p class="ewl-person-name">`)
	if link := strings.TrimSpace(mc.Params["link"]); link != "" {
		fmt.Fprintf(&b, `<a href="%s">%s</a>`, esc(SafeURL(link)), esc(name))
	} else {
		b.WriteString(esc(name))
	}
	b.WriteString(`</p>`)
	if role != "" {
		fmt.Fprintf(&b, `<p class="ewl-person-role">%s</p>`, esc(role))
	}
	if strings.TrimSpace(mc.Body) != "" {
		b.WriteString(`<div class="ewl-person-about">` + mc.Body + `</div>`)
	}
	if email != "" || phone != "" {
		b.WriteString(`<ul class="ewl-person-contact">`)
		if email != "" {
			fmt.Fprintf(&b, `<li><a href="mailto:%s">%s</a></li>`, esc(email), esc(email))
		}
		if phone != "" {
			fmt.Fprintf(&b, `<li><a href="tel:%s">%s</a></li>`, esc(strings.Map(telChar, phone)), esc(phone))
		}
		b.WriteString(`</ul>`)
	}
	b.WriteString(`</div></div>`)
	return b.String(), nil
}

func telChar(r rune) rune {
	if r == '+' || (r >= '0' && r <= '9') {
		return r
	}
	return -1
}

// ---------------------------------------------------------------- changing words

type rotating struct{}

func (rotating) Name() string  { return "rotating" }
func (rotating) Group() string { return "text" }
func (rotating) Labels() map[string]string {
	return lm("Heading with changing words", "Überschrift mit wechselnden Wörtern")
}
func (rotating) Description() map[string]string {
	return lm("A heading in which one word changes (\"We build wikis | sites | tools\"). Screen readers get all words at once; the change has a pause button and stops with reduced motion.",
		"Eine Überschrift, in der ein Wort wechselt („Wir bauen Wikis | Websites | Werkzeuge“). Screenreader bekommen alle Wörter auf einmal; der Wechsel hat einen Pause-Knopf und steht bei weniger Bewegung still.")
}
func (rotating) HasBody() bool { return false }
func (rotating) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "before", Labels: lm("Text before", "Text davor"), Type: "text"},
		{Name: "words", Labels: lm("Words, separated by |", "Wörter, getrennt durch |"), Type: "text", Required: true},
		{Name: "after", Labels: lm("Text after", "Text danach"), Type: "text"},
		{Name: "level", Labels: lm("Level", "Ebene"), Type: "select", Options: []string{"2", "1", "3", "4"}, Default: "2"},
		{Name: "effect", Labels: lm("Effect", "Effekt"), Type: "select", Options: []string{"fade", "type"}, Default: "fade",
			OptionLabels: map[string]map[string]string{"fade": lm("Fade", "Überblenden"), "type": lm("Typewriter", "Schreibmaschine")}},
	}
}

func (rotating) Render(_ context.Context, mc content.MacroContext) (string, error) {
	before, err := shortText("before", mc.Params["before"], 120)
	if err != nil {
		return "", err
	}
	after, err := shortText("after", mc.Params["after"], 120)
	if err != nil {
		return "", err
	}
	var words []string
	for _, w := range strings.Split(mc.Params["words"], "|") {
		if w = strings.TrimSpace(w); w != "" {
			if len([]rune(w)) > 40 {
				return "", apperr.BadRequest("a word has at most 40 characters")
			}
			words = append(words, w)
		}
	}
	if len(words) < 2 || len(words) > 10 {
		return "", apperr.BadRequest("2 to 10 words, separated by |")
	}
	level := oneOf(mc.Params["level"], []string{"2", "1", "3", "4"})
	effect := oneOf(mc.Params["effect"], []string{"fade", "type"})
	var list strings.Builder
	for i, w := range words {
		if i > 0 {
			list.WriteString(`<span class="ewl-rot-sep">, </span>`)
		}
		fmt.Fprintf(&list, `<span class="ewl-rot-word">%s</span>`, esc(w))
	}
	// the whole text is the heading (all words, readable without script);
	// the browser shows one word at a time and keeps the whole text for
	// screen readers
	return fmt.Sprintf(`<div class="ewl-rot ewl-rot--%s" data-ew-widget="ewl.rotating"><h%s class="ewl-rot-heading">%s <span class="ewl-rot-words">%s</span> %s</h%s></div>`,
		effect, level, esc(before), list.String(), esc(after), level), nil
}

// ---------------------------------------------------------------- ticker

type ticker struct{}

func (ticker) Name() string              { return "ticker" }
func (ticker) Group() string             { return "media" }
func (ticker) Labels() map[string]string { return lm("Ticker", "Laufband") }
func (ticker) Description() map[string]string {
	return lm("The entries of a list run through a band. It always has a pause button, stops while pointed at or focused, and stands still with reduced motion; the list itself stays readable.",
		"Die Einträge einer Liste laufen durch ein Band. Es hat immer einen Pause-Knopf, hält beim Zeigen und mit dem Fokus an und steht bei weniger Bewegung still; die Liste selbst bleibt lesbar.")
}
func (ticker) HasBody() bool { return true }
func (ticker) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "label", Labels: lm("Name for screen readers (e.g. News)", "Name für Screenreader (z. B. Meldungen)"), Type: "text", Required: true},
		{Name: "speed", Labels: lm("Speed", "Tempo"), Type: "select", Options: []string{"slow", "medium", "fast"}, Default: "slow"},
	}
}

var (
	listItemRe = regexp.MustCompile(`(?is)<li\b[^>]*>.*?</li>`)
	emailRe    = regexp.MustCompile(`^[^\s@<>"']{1,64}@[^\s@<>"']{1,190}\.[A-Za-z]{2,24}$`)
	phoneRe    = regexp.MustCompile(`^\+?[0-9][0-9 ()/-]{2,30}$`)
)

func (ticker) Render(_ context.Context, mc content.MacroContext) (string, error) {
	label, err := shortText("label", mc.Params["label"], 120)
	if err != nil {
		return "", err
	}
	if label == "" {
		return "", apperr.BadRequest("a ticker needs a name for screen readers (label)")
	}
	items := listItemRe.FindAllString(mc.Body, -1)
	if len(items) == 0 {
		return "", apperr.BadRequest("put the entries as a list into the ticker")
	}
	if len(items) > 30 {
		return "", apperr.BadRequest("at most 30 entries")
	}
	speed := oneOf(mc.Params["speed"], []string{"slow", "medium", "fast"})
	return fmt.Sprintf(`<div class="ewl-ticker ewl-ticker--%s" role="region" aria-label="%s" data-ew-widget="ewl.ticker"><ul class="ewl-ticker-list">%s</ul></div>`,
		speed, esc(label), strings.Join(items, "")), nil
}

// ---------------------------------------------------------------- slider

type slider struct{}

func (slider) Name() string              { return "slider" }
func (slider) Group() string             { return "layout" }
func (slider) Labels() map[string]string { return lm("Content slider", "Inhalts-Slider") }
func (slider) Description() map[string]string {
	return lm("Parts shown one at a time, with arrows and a button per part; separate the parts with a horizontal line. Optionally it changes by itself, with a pause button, never with reduced motion.",
		"Teile, einer nach dem anderen, mit Pfeilen und einem Knopf je Teil; Teile mit einer Trennlinie trennen. Wahlweise wechselt er von selbst, mit Pause-Knopf, nie bei weniger Bewegung.")
}
func (slider) HasBody() bool { return true }
func (slider) Parts() content.MacroParts {
	return content.MacroParts{Labels: lm("Slide", "Folie"), Titled: true}
}
func (slider) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "label", Labels: lm("Name for screen readers", "Name für Screenreader"), Type: "text", Required: true},
		{Name: "auto", Labels: lm("Change by itself (every 7 seconds, with a pause button)", "Von selbst wechseln (alle 7 Sekunden, mit Pause-Knopf)"), Type: "bool"},
	}
}

func (slider) Render(_ context.Context, mc content.MacroContext) (string, error) {
	label, err := shortText("label", mc.Params["label"], 120)
	if err != nil {
		return "", err
	}
	if label == "" {
		return "", apperr.BadRequest("a slider needs a name for screen readers (label)")
	}
	parts := core_macros.SplitOnHR(mc.Body)
	if len(parts) < 2 {
		return "", apperr.BadRequest("a slider needs at least two parts, separated by a horizontal line")
	}
	if len(parts) > 20 {
		return "", apperr.BadRequest("at most 20 parts")
	}
	auto := ""
	if mc.Params["auto"] == "true" {
		auto = ` data-auto="true"`
	}
	var b strings.Builder
	// the carousel pattern of WAI-ARIA: without script the parts stand one below the other
	fmt.Fprintf(&b, `<section class="ewl-slider" aria-roledescription="carousel" aria-label="%s" data-ew-widget="ewl.slider"%s><div class="ewl-slides">`, esc(label), auto)
	for i, p := range parts {
		fmt.Fprintf(&b, `<div class="ewl-slide" role="group" aria-roledescription="slide" aria-label="%d / %d">%s</div>`, i+1, len(parts), p)
	}
	b.WriteString(`</div></section>`)
	return b.String(), nil
}
