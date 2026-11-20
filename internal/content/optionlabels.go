package content

// commonOptionLabels name options of select parameters that many macros
// share (sizes, alignments, colors …). A macro's own OptionLabels win; these
// fill the gaps, so the macro dialog never shows a bare value like "start".
var commonOptionLabels = map[string]map[string]string{
	"":           {"en": "Default", "de": "Standard"},
	"all":        {"en": "All", "de": "Alles"},
	"categories": {"en": "Categories", "de": "Kategorien"},
	"pages":      {"en": "Pages", "de": "Seiten"},
	"primary":    {"en": "Prominent", "de": "Auffällig"},
	"secondary":  {"en": "Subtle", "de": "Dezent"},
	"link":       {"en": "Link", "de": "Link"},
	"row":        {"en": "Side by side", "de": "Nebeneinander"},
	"full":       {"en": "Full width", "de": "Volle Breite"},
	"stack":      {"en": "One below the other", "de": "Untereinander"},
	"side":       {"en": "Side by side", "de": "Nebeneinander"},
	"start":      {"en": "At the start (left)", "de": "Am Anfang (links)"},
	"center":     {"en": "Centered", "de": "Mittig"},
	"end":        {"en": "At the end (right)", "de": "Am Ende (rechts)"},
	"left":       {"en": "Left", "de": "Links"},
	"top":        {"en": "Top", "de": "Oben"},
	"stretch":    {"en": "Same height", "de": "Gleich hoch"},
	"line":       {"en": "Line", "de": "Linie"},
	"dashed":     {"en": "Dashed", "de": "Gestrichelt"},
	"dotted":     {"en": "Dotted", "de": "Gepunktet"},
	"double":     {"en": "Double", "de": "Doppelt"},
	"fade":       {"en": "Fading", "de": "Verlaufend"},
	"space":      {"en": "Space only", "de": "Nur Abstand"},
	"none":       {"en": "None", "de": "Ohne"},
	"small":      {"en": "Small", "de": "Klein"},
	"medium":     {"en": "Medium", "de": "Mittel"},
	"large":      {"en": "Large", "de": "Groß"},
	"half":       {"en": "Half width", "de": "Halbe Breite"},
	"short":      {"en": "Short", "de": "Kurz"},
	"normal":     {"en": "Normal", "de": "Normal"},
	"wide":       {"en": "Wide", "de": "Breit"},
	"grid":       {"en": "Grid", "de": "Raster"},
	"masonry":    {"en": "Staggered (masonry)", "de": "Versetzt (Mauerwerk)"},
	"auto":       {"en": "Automatic", "de": "Automatisch"},
	"xl":         {"en": "Extra large", "de": "Sehr groß"},
	"l":          {"en": "Large", "de": "Groß"},
	"m":          {"en": "Medium", "de": "Mittel"},
	"s":          {"en": "Small", "de": "Klein"},
	"accent":     {"en": "Accent color", "de": "Akzentfarbe"},
	"dark":       {"en": "Dark", "de": "Dunkel"},
	"light":      {"en": "Light", "de": "Hell"},
	"text":       {"en": "Text", "de": "Text"},
	"html":       {"en": "HTML", "de": "HTML"},
	"json":       {"en": "JSON", "de": "JSON"},
	"outline":    {"en": "Outline", "de": "Umriss"},
	"filled":     {"en": "Filled", "de": "Gefüllt"},
	"info":       {"en": "Info", "de": "Info"},
	"note":       {"en": "Note", "de": "Notiz"},
	"tip":        {"en": "Tip", "de": "Tipp"},
	"warning":    {"en": "Warning", "de": "Warnung"},
	"success":    {"en": "Success", "de": "Erfolg"},
	"danger":     {"en": "Danger", "de": "Gefahr"},
	"grey":       {"en": "Grey", "de": "Grau"},
	"blue":       {"en": "Blue", "de": "Blau"},
	"green":      {"en": "Green", "de": "Grün"},
	"yellow":     {"en": "Yellow", "de": "Gelb"},
	"red":        {"en": "Red", "de": "Rot"},
	"orange":     {"en": "Orange", "de": "Orange"},
	"purple":     {"en": "Purple", "de": "Lila"},
	"serif":      {"en": "Serif", "de": "Serifen"},
	"mono":       {"en": "Monospace", "de": "Festbreite"},
	"slow":       {"en": "Slow", "de": "Langsam"},
	"fast":       {"en": "Fast", "de": "Schnell"},
}

// withOptionLabels returns the parameters with labels for the options of
// select parameters that have none (from commonOptionLabels); the
// parameters of the macro are not changed.
func withOptionLabels(ps []MacroParam) []MacroParam {
	out := make([]MacroParam, len(ps))
	for i, p := range ps {
		if p.Type == "select" {
			labels := make(map[string]map[string]string, len(p.Options))
			for _, o := range p.Options {
				if l := p.OptionLabels[o]; l != nil {
					labels[o] = l
				} else if l := commonOptionLabels[o]; l != nil {
					labels[o] = l
				}
			}
			if len(labels) > 0 {
				p.OptionLabels = labels
			}
		}
		out[i] = p
	}
	return out
}
