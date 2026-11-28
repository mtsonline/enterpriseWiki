// Package tplsettings describes and checks the settings a website template
// offers (colours, fonts, logos, texts …; Administration → Templates →
// Settings). A template declares its settings when it is registered: a
// template of code in the manifest of its plugin (plugin.json,
// "template_settings"), a template of data in its design. The server checks
// every value against that description before it is stored, so what a
// template receives is always of the declared kind and within its limits,
// whatever the client sent. Only values changed from the defaults are
// stored; resetting a setting removes its value.
package tplsettings

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The kinds of settings.
const (
	KindColor  = "color"  // "#rrggbb"
	KindFont   = "font"   // one of Options (keys of FontStacks) or an own font ("own-12")
	KindImage  = "image"  // an uploaded PNG, WebP or JPEG image (with its size)
	KindNumber = "number" // a number within Min and Max
	KindSelect = "select" // one of Options
	KindBool   = "bool"   // true or false
	KindText   = "text"   // a line of text up to MaxLen characters
	KindLink   = "link"   // a path in the wiki ("/…") or an https:// address
)

// Kinds lists all kinds of settings.
var Kinds = []string{KindColor, KindFont, KindImage, KindNumber, KindSelect, KindBool, KindText, KindLink}

// Groups are the sections of the form, in this order; a template may use
// only some of them.
var Groups = []string{"colors", "fonts", "logos", "header", "content", "footer", "layout"}

// The roles of images that are logos. Other images (a hero image, a banner)
// have no role.
const (
	RoleLogo       = "logo"         // the wide logo in the header
	RoleLogoOnDark = "logo_on_dark" // the wide logo on a dark background
	RoleMark       = "mark"         // the mark alone (narrow screens, small places)
	RoleMarkOnDark = "mark_on_dark" // the mark on a dark background
)

// Roles lists the roles of logos.
var Roles = []string{RoleLogo, RoleLogoOnDark, RoleMark, RoleMarkOnDark}

// FontStacks are the fonts a template may offer (the keys of the options of
// a font setting) with their CSS font-family. The files of Inter and
// Poppins come with the core; Source Sans with the templates that use it.
var FontStacks = map[string]string{
	"system":      `system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`,
	"inter":       `"Inter Variable", Inter, system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`,
	"poppins":     `Poppins, "Inter Variable", system-ui, sans-serif`,
	"source-sans": `"Source Sans 3 Variable", system-ui, sans-serif`,
	"serif":       `Georgia, "Times New Roman", serif`,
	"mono":        `ui-monospace, SFMono-Regular, Menlo, Consolas, monospace`,
}

// The units of numbers written into CSS variables.
var Units = []string{"px", "rem", "em", "%"}

// Limits of the description and of values.
const (
	MaxSettings    = 100  // settings of one template
	MaxOptions     = 50   // options of a select
	DefaultTextLen = 200  // MaxLen of a text without one
	MaxTextLen     = 2000 // the largest MaxLen
	MaxLinkLen     = 500
	// MaxImagePixels bounds an image (width × height) so a logo cannot
	// make every page load a huge file.
	MaxImagePixels = 4096 * 4096
)

// Setting describes one setting of a template.
type Setting struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"`
	Group string `json:"group"`
	// Role of an image that is a logo (see Roles).
	Role string `json:"role,omitempty"`
	// Default is the value used while none is stored (a hash is not
	// allowed for images: their default is the image the template ships).
	Default any      `json:"default,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Step    float64  `json:"step,omitempty"`
	MaxLen  int      `json:"max_len,omitempty"`
	Options []string `json:"options,omitempty"`
	Label   Texts    `json:"label"`
	Help    Texts    `json:"help,omitempty"`
	// OptionLabels are the texts of the options, per option.
	OptionLabels map[string]Texts `json:"option_labels,omitempty"`
	// CSSVars are the CSS variables the value is written to (colours, fonts
	// and numbers): the core sets them for the template, only while the value
	// differs from the default, so the template keeps its own look by
	// default. Scheme limits them to the light or the dark colour scheme.
	CSSVars []string `json:"css_vars,omitempty"`
	Scheme  string   `json:"scheme,omitempty"`
	// Unit of a number written to CSS variables (see Units).
	Unit string `json:"unit,omitempty"`
	// DefaultSrc is the image the template shows without one of its own (a
	// file the template ships, or for a template of data an uploaded file
	// /api/v1/files/HASH; shown in the form as the default).
	DefaultSrc string `json:"default_src,omitempty"`
	// ContrastOn are the colours a colour is shown on (other colour
	// settings by key, or "#rrggbb"): the form warns while the contrast to
	// one of them is below 4.5:1 (WCAG AA for text).
	ContrastOn []string `json:"contrast_on,omitempty"`
}

// Texts are the texts of a setting per language ("en" is required).
type Texts map[string]string

// Image is the stored value of an image: the hash of the file and its size
// in pixels (read from the file when the value is saved, so pages can
// reserve the room of the image before it loads).
type Image struct {
	Hash   string `json:"hash"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// ImageInfo reads an uploaded file: its format ("png", "webp", "jpeg" or
// anything else) and its size. It returns an error if no such file exists.
type ImageInfo func(hash string) (format string, width, height int, err error)

var (
	keyRe   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
	colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	hashRe  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	optRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)
	cssVar  = regexp.MustCompile(`^--[a-z][a-z0-9-]{0,62}$`)
	ownFont = regexp.MustCompile(`^own-[1-9][0-9]{0,17}$`)
	srcRe   = regexp.MustCompile(`^/[A-Za-z0-9_./-]{1,200}\.(png|webp|jpe?g)$|^/api/v1/files/[0-9a-f]{64}$`)
)

// CheckDescription checks the description of the settings of a template
// (done when it is registered): keys, kinds, groups, roles, limits and
// that every default is a valid value.
func CheckDescription(list []Setting) error {
	if len(list) > MaxSettings {
		return fmt.Errorf("at most %d settings", MaxSettings)
	}
	colors := map[string]bool{}
	for _, s := range list {
		if s.Kind == KindColor {
			colors[s.Key] = true
		}
	}
	seen := map[string]bool{}
	for _, s := range list {
		if !keyRe.MatchString(s.Key) {
			return fmt.Errorf("setting %q: key must be lower case letters, digits and _", s.Key)
		}
		if seen[s.Key] {
			return fmt.Errorf("setting %q: declared twice", s.Key)
		}
		seen[s.Key] = true
		if !slices.Contains(Kinds, s.Kind) {
			return fmt.Errorf("setting %q: unknown kind %q", s.Key, s.Kind)
		}
		if !slices.Contains(Groups, s.Group) {
			return fmt.Errorf("setting %q: unknown group %q", s.Key, s.Group)
		}
		if s.Role != "" && (s.Kind != KindImage || !slices.Contains(Roles, s.Role)) {
			return fmt.Errorf("setting %q: role %q is only for images: %v", s.Key, s.Role, Roles)
		}
		if strings.TrimSpace(s.Label["en"]) == "" {
			return fmt.Errorf("setting %q: an English label is needed", s.Key)
		}
		if s.MaxLen < 0 || s.MaxLen > MaxTextLen {
			return fmt.Errorf("setting %q: max_len must be 0–%d", s.Key, MaxTextLen)
		}
		if s.Kind == KindNumber && (s.Min == nil || s.Max == nil || *s.Min > *s.Max) {
			return fmt.Errorf("setting %q: a number needs min ≤ max", s.Key)
		}
		if s.Kind == KindSelect {
			if len(s.Options) == 0 || len(s.Options) > MaxOptions {
				return fmt.Errorf("setting %q: a select needs 1–%d options", s.Key, MaxOptions)
			}
			for _, o := range s.Options {
				if !optRe.MatchString(o) {
					return fmt.Errorf("setting %q: option %q must be lower case letters, digits, _ and -", s.Key, o)
				}
			}
		}
		if len(s.CSSVars) > 0 && s.Kind != KindColor && s.Kind != KindFont && s.Kind != KindNumber {
			return fmt.Errorf("setting %q: css_vars are only for colours, fonts and numbers", s.Key)
		}
		for _, v := range s.CSSVars {
			if !cssVar.MatchString(v) {
				return fmt.Errorf("setting %q: css variable %q must look like --name", s.Key, v)
			}
		}
		if s.Scheme != "" && s.Scheme != "light" && s.Scheme != "dark" {
			return fmt.Errorf("setting %q: scheme is light, dark or empty", s.Key)
		}
		if s.Unit != "" && (s.Kind != KindNumber || !slices.Contains(Units, s.Unit)) {
			return fmt.Errorf("setting %q: unit is for numbers, one of %v", s.Key, Units)
		}
		if s.DefaultSrc != "" && (s.Kind != KindImage || !srcRe.MatchString(s.DefaultSrc) || strings.Contains(s.DefaultSrc, "..") || strings.HasPrefix(s.DefaultSrc, "//")) {
			return fmt.Errorf("setting %q: default_src is a path of a PNG, WebP or JPEG file the template ships or /api/v1/files/HASH", s.Key)
		}
		if s.Kind == KindFont {
			if len(s.Options) == 0 {
				return fmt.Errorf("setting %q: a font needs options (keys of the fonts offered)", s.Key)
			}
			for _, o := range s.Options {
				if _, ok := FontStacks[o]; !ok {
					return fmt.Errorf("setting %q: unknown font %q", s.Key, o)
				}
			}
		}
		if len(s.ContrastOn) > 0 && s.Kind != KindColor {
			return fmt.Errorf("setting %q: contrast_on is only for colours", s.Key)
		}
		for _, c := range s.ContrastOn {
			if !colorRe.MatchString(c) && (!colors[c] || c == s.Key) {
				return fmt.Errorf("setting %q: contrast_on %q is neither #rrggbb nor another colour setting", s.Key, c)
			}
		}
		if s.Kind == KindImage {
			if s.Default != nil {
				return fmt.Errorf("setting %q: an image has no default value (the template ships its own image)", s.Key)
			}
			continue
		}
		if s.Default == nil {
			return fmt.Errorf("setting %q: a default value is needed", s.Key)
		}
		if _, err := checkValue(s, s.Default, nil); err != nil {
			return fmt.Errorf("setting %q: default: %w", s.Key, err)
		}
	}
	return nil
}

// ErrUnknown is returned for a value of a setting the template does not declare.
var ErrUnknown = errors.New("not a setting of this template")

// Check checks values sent for a template against its description and
// returns them as they are stored: images as Image, numbers as float64,
// texts trimmed. A value of nil resets the setting (it is left out).
func Check(list []Setting, values map[string]any, info ImageInfo) (map[string]any, error) {
	out := map[string]any{}
	for k, v := range values {
		i := slices.IndexFunc(list, func(s Setting) bool { return s.Key == k })
		if i < 0 {
			return nil, fmt.Errorf("%q: %w", k, ErrUnknown)
		}
		if v == nil {
			continue
		}
		c, err := checkValue(list[i], v, info)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", k, err)
		}
		out[k] = c
	}
	return out, nil
}

func checkValue(s Setting, v any, info ImageInfo) (any, error) {
	switch s.Kind {
	case KindColor:
		x, ok := v.(string)
		if !ok || !colorRe.MatchString(x) {
			return nil, errors.New("a colour must be #rrggbb")
		}
		return strings.ToLower(x), nil
	case KindFont:
		x, ok := v.(string)
		if !ok || !(slices.Contains(s.Options, x) || ownFont.MatchString(x)) {
			return nil, fmt.Errorf("a font of %v or an own font", s.Options)
		}
		return x, nil
	case KindNumber:
		x, ok := v.(float64)
		if n, isInt := v.(int); isInt {
			x, ok = float64(n), true
		}
		if !ok || math.IsNaN(x) || math.IsInf(x, 0) || x < *s.Min || x > *s.Max {
			return nil, fmt.Errorf("a number from %g to %g", *s.Min, *s.Max)
		}
		return x, nil
	case KindSelect:
		x, ok := v.(string)
		if !ok || !slices.Contains(s.Options, x) {
			return nil, fmt.Errorf("one of %v", s.Options)
		}
		return x, nil
	case KindBool:
		x, ok := v.(bool)
		if !ok {
			return nil, errors.New("true or false")
		}
		return x, nil
	case KindText:
		x, ok := v.(string)
		if !ok {
			return nil, errors.New("a text")
		}
		x = strings.TrimSpace(x)
		most := s.MaxLen
		if most == 0 {
			most = DefaultTextLen
		}
		if !utf8.ValidString(x) || utf8.RuneCountInString(x) > most || strings.ContainsFunc(x, unicode.IsControl) {
			return nil, fmt.Errorf("a line of at most %d characters", most)
		}
		return x, nil
	case KindLink:
		x, ok := v.(string)
		if !ok || !LinkOK(x) {
			return nil, errors.New("a path in the wiki (/…) or an https:// address")
		}
		return x, nil
	case KindImage:
		return checkImage(v, info)
	}
	return nil, fmt.Errorf("unknown kind %q", s.Kind)
}

// LinkOK reports whether x may be the target of a link a template shows:
// empty, a path in the wiki ("/…", not "//…") or an https:// address,
// without spaces, quotes or angle brackets.
func LinkOK(x string) bool {
	if x == "" {
		return true
	}
	if len(x) > MaxLinkLen || strings.ContainsAny(x, " \t\r\n\"'<>\\`") || strings.ContainsFunc(x, unicode.IsControl) {
		return false
	}
	if strings.HasPrefix(x, "/") {
		return !strings.HasPrefix(x, "//")
	}
	rest, ok := strings.CutPrefix(x, "https://")
	return ok && rest != "" && !strings.HasPrefix(rest, "/")
}

// checkImage takes the hash of an uploaded file and returns it with its
// size, if the file is a PNG, WebP or JPEG image. Without info (a value
// read back from the configuration) the stored size is taken as stored:
// it was read from the file when the value was saved. A value sent by a
// client is always checked with info, so its own width and height count
// for nothing.
func checkImage(v any, info ImageInfo) (any, error) {
	img := Image{}
	switch x := v.(type) {
	case string:
		img.Hash = x
	case map[string]any:
		img.Hash, _ = x["hash"].(string)
		img.Width, img.Height = toInt(x["width"]), toInt(x["height"])
	case Image:
		img = x
	}
	if !hashRe.MatchString(img.Hash) {
		return nil, errors.New("an image is the hash of an uploaded file")
	}
	if info != nil {
		format, w, h, err := info(img.Hash)
		if err != nil {
			return nil, errors.New("no uploaded file with this hash")
		}
		if format != "png" && format != "webp" && format != "jpeg" {
			return nil, errors.New("only PNG, WebP and JPEG images (no SVG: it can contain scripts)")
		}
		img.Width, img.Height = w, h
	}
	if img.Width <= 0 || img.Height <= 0 || img.Width*img.Height > MaxImagePixels {
		return nil, fmt.Errorf("an image with at most %d pixels", MaxImagePixels)
	}
	return img, nil
}

// Effective merges the stored values over the defaults: every declared
// setting except images without a value. Stored values a template no
// longer declares, or that no longer fit its description (a smaller
// maximum, an option removed), are left out, so a template only ever gets
// values of its current description.
func Effective(list []Setting, stored map[string]any) map[string]any {
	out := map[string]any{}
	for _, s := range list {
		if v, ok := stored[s.Key]; ok {
			if c, err := checkValue(s, v, nil); err == nil {
				out[s.Key] = c
				continue
			}
		}
		if s.Default != nil {
			out[s.Key] = s.Default
		}
	}
	return out
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

// Look is what the core writes into CSS for a template: the CSS variables
// of the settings changed from their defaults, for both colour schemes, the
// light and the dark one, and the own fonts they use.
type Look struct {
	All   map[string]string `json:"all"`
	Light map[string]string `json:"light"`
	Dark  map[string]string `json:"dark"`
	// Fonts are the ids of own fonts used (their @font-face rules are loaded).
	Fonts []string `json:"fonts"`
}

// LookOf returns the CSS variables of the stored values that differ from
// their defaults. Every value is of a checked form (#rrggbb, a number with
// its unit, a font-family from FontStacks or of an own font), so nothing
// else can get into the CSS.
func LookOf(list []Setting, stored map[string]any) Look {
	l := Look{All: map[string]string{}, Light: map[string]string{}, Dark: map[string]string{}, Fonts: []string{}}
	for _, s := range list {
		v, ok := stored[s.Key]
		if !ok || len(s.CSSVars) == 0 {
			continue
		}
		c, err := checkValue(s, v, nil)
		if err != nil || c == s.Default || (s.Kind == KindNumber && s.Default != nil && toFloat(s.Default) == c) {
			continue
		}
		css := ""
		switch x := c.(type) {
		case string:
			css = x
			if s.Kind == KindFont {
				if stack, ok := FontStacks[x]; ok {
					css = stack
				} else {
					css = `"ewd-` + x + `", ` + FontStacks["system"]
					if !slices.Contains(l.Fonts, strings.TrimPrefix(x, "own-")) {
						l.Fonts = append(l.Fonts, strings.TrimPrefix(x, "own-"))
					}
				}
			}
		case float64:
			css = strconv.FormatFloat(x, 'f', -1, 64) + s.Unit
		default:
			continue
		}
		into := l.All
		switch s.Scheme {
		case "light":
			into = l.Light
		case "dark":
			into = l.Dark
		}
		for _, name := range s.CSSVars {
			into[name] = css
		}
	}
	// the core derives the colour of a hovered button from the primary colour
	for _, x := range []struct {
		m   map[string]string
		mix string
	}{{l.Light, "black"}, {l.Dark, "white"}} {
		if p, ok := x.m["--ew-primary"]; ok {
			if _, has := x.m["--ew-primary-hover"]; !has {
				x.m["--ew-primary-hover"] = "color-mix(in srgb, " + p + " 82%, " + x.mix + ")"
			}
		}
	}
	return l
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	}
	return math.NaN()
}
