package content

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

// Options of blocks: the same schema (blockoptions.json) is read by the
// browser for the form and the CSS. Every option is a checked value; the CSS
// is built from these values only (no free CSS here).

//go:embed blockoptions.json
var blockOptionsJSON []byte

// BlockOptionsSchema returns the schema of the options of blocks.
func BlockOptionsSchema() json.RawMessage { return blockOptionsJSON }

type optionDef struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Values     []string `json:"values"`
	Min        float64  `json:"min"`
	Max        float64  `json:"max"`
	Step       float64  `json:"step"`
	Responsive bool     `json:"responsive"`
}

type optionSchema struct {
	MaxBytes     int            `json:"max_bytes"`
	Views        []string       `json:"views"`
	LangsMax     int            `json:"langs_max"`
	SpaceRem     []float64      `json:"space_rem"`
	ContrastText float64        `json:"contrast_text"`
	ColumnMinRem float64        `json:"column_min_rem"`
	ViewMinPx    map[string]int `json:"view_min_px"`
	RemPx        int            `json:"rem_px"`
	Groups       []struct {
		ID      string      `json:"id"`
		Options []optionDef `json:"options"`
	} `json:"groups"`
}

var blockOptions = func() (s struct {
	optionSchema
	byID map[string]optionDef
}) {
	if err := json.Unmarshal(blockOptionsJSON, &s.optionSchema); err != nil {
		panic("content: blockoptions.json: " + err.Error())
	}
	s.byID = map[string]optionDef{}
	for _, g := range s.Groups {
		for _, o := range g.Options {
			s.byID[o.ID] = o
		}
	}
	return s
}()

// reservedAnchors are ids of the interface itself.
var reservedAnchors = []string{"main", "top", "content", "app", "search", "nav", "header", "footer"}

// generatedID is the form of ids the wiki makes itself (name-number:
// questions and sections of a FAQ, fields of forms f<page>-…); anchors must
// not take them.
var generatedID = regexp.MustCompile(`-[0-9]+$|^f[0-9]+-`)

// pluginID are the ids that element types write into content themselves
// (FAQ: q-<id>, s-<id>; forms: f<id>-…). Every other id of the interface
// starts with "ew".
var pluginID = regexp.MustCompile(`^[qs]-[0-9]+$|^f[0-9]+-`)

// interfaceID reports whether an id in content (a heading) would take an id
// of the interface: then it gets the prefix "h-".
func interfaceID(id string) bool {
	return slices.Contains(reservedAnchors, id) || strings.HasPrefix(id, "ew") || pluginID.MatchString(id)
}

// CheckAnchor checks the name of an anchor (block option and the anchor
// block): lowercase letters, digits and hyphens, starting with a letter,
// not an id of the interface (reserved names, the prefix "ew", names that
// end in -number). The same rule as anchorOK in web/src/core/blockoptions.ts.
func CheckAnchor(s string) error {
	if !optAnchorRe.MatchString(s) {
		return fmt.Errorf("lowercase letters, digits and hyphens, starting with a letter (at most 40)")
	}
	// names the interface uses itself (the skip link jumps to #main …)
	if slices.Contains(reservedAnchors, s) || strings.HasPrefix(s, "ew") || generatedID.MatchString(s) {
		return fmt.Errorf("%q is used by the wiki itself or looks like it (no \"ew\" at the start, no -number at the end); choose another name", s)
	}
	return nil
}

var (
	optColorRe  = regexp.MustCompile(`^#[0-9a-f]{6}$`)
	optAnchorRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	optFileRe   = regexp.MustCompile(`^/api/v1/(files/[A-Za-z0-9._-]{1,128}|media/[0-9]{1,18}/view)$`)
	optLangRe   = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)
)

// CheckBlockOptions checks the options of one placed block: known options,
// values of their type and range, and the rules every block must keep
// (contrast of its text on its background, room for its columns).
func CheckBlockOptions(raw map[string]any) error {
	if len(raw) == 0 {
		return nil
	}
	if b, _ := json.Marshal(raw); len(b) > blockOptions.MaxBytes {
		return apperr.BadRequest("options: at most %d bytes", blockOptions.MaxBytes)
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		def, ok := blockOptions.byID[k]
		if !ok {
			return apperr.BadRequest("options: unknown option %q", k)
		}
		v := raw[k]
		if m, isMap := v.(map[string]any); isMap && def.Responsive {
			if len(m) == 0 {
				return apperr.BadRequest("options: %s: a value per view, at least one", k)
			}
			for view, x := range m {
				if !slices.Contains(blockOptions.Views, view) {
					return apperr.BadRequest("options: %s: unknown view %q", k, view)
				}
				if err := checkOptionValue(def, x); err != nil {
					return apperr.BadRequest("options: %s (%s): %s", k, view, err)
				}
			}
			continue
		}
		if err := checkOptionValue(def, v); err != nil {
			return apperr.BadRequest("options: %s: %s", k, err)
		}
	}
	return checkOptionRules(raw)
}

func checkOptionValue(def optionDef, v any) error {
	switch def.Type {
	case "enum":
		s, ok := v.(string)
		if !ok || !slices.Contains(def.Values, s) {
			return fmt.Errorf("one of %v", def.Values)
		}
	case "bool":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("true or false")
		}
	case "int", "number", "space":
		n, ok := v.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Errorf("a number")
		}
		lo, hi := def.Min, def.Max
		if def.Type == "space" {
			lo, hi = 0, float64(len(blockOptions.SpaceRem)-1)
		}
		if (def.Type == "int" || def.Type == "space") && n != math.Trunc(n) {
			return fmt.Errorf("a whole number")
		}
		if n < lo || n > hi {
			return fmt.Errorf("from %s to %s", strconv.FormatFloat(lo, 'f', -1, 64), strconv.FormatFloat(hi, 'f', -1, 64))
		}
	case "color":
		if s, ok := v.(string); !ok || !optColorRe.MatchString(s) {
			return fmt.Errorf("a color #rrggbb (lowercase)")
		}
	case "file":
		if s, ok := v.(string); !ok || !optFileRe.MatchString(s) {
			return fmt.Errorf("a file of the wiki (/api/v1/files/… or /api/v1/media/ID/view)")
		}
	case "anchor":
		s, _ := v.(string)
		if err := CheckAnchor(s); err != nil {
			return err
		}
	case "datetime":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("a time (RFC 3339)")
		}
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			return fmt.Errorf("a time (RFC 3339)")
		}
	case "views", "langs":
		list, ok := v.([]any)
		if !ok {
			return fmt.Errorf("a list")
		}
		max := len(blockOptions.Views)
		if def.Type == "langs" {
			max = blockOptions.LangsMax
		}
		if len(list) > max {
			return fmt.Errorf("at most %d entries", max)
		}
		seen := map[string]bool{}
		for _, x := range list {
			s, ok := x.(string)
			if def.Type == "views" && (!ok || seen[s] || !slices.Contains(blockOptions.Views, s)) {
				return fmt.Errorf("entries of %v, each once", blockOptions.Views)
			}
			if def.Type == "langs" && (!ok || seen[s] || !optLangRe.MatchString(s)) {
				return fmt.Errorf("language codes, each once")
			}
			seen[s] = true
		}
	default:
		return fmt.Errorf("unknown type %q", def.Type)
	}
	return nil
}

func optStr(o map[string]any, k string) string {
	s, _ := o[k].(string)
	return s
}

func optNum(o map[string]any, k string, def float64) float64 {
	if n, ok := o[k].(float64); ok {
		return n
	}
	return def
}

// checkOptionRules: the rules a block keeps whatever its options are.
func checkOptionRules(o map[string]any) error {
	min := blockOptions.ContrastText
	text := optStr(o, "text_color")
	bg := optStr(o, "bg_color")
	gradient := optStr(o, "bg_gradient")
	if gradient == "none" {
		gradient = ""
	}
	// a video in the background has unknown colors like a picture
	image := optStr(o, "bg_image") + optStr(o, "bg_video")
	pattern := optStr(o, "pattern")
	if pattern == "none" {
		pattern = ""
	}
	changesBackground := bg != "" || gradient != "" || image != "" || pattern != ""
	// a new background needs a text color, so the contrast is known
	if changesBackground && text == "" {
		return apperr.BadRequest("options: a block with its own background needs its own text color (text_color), so its contrast can be checked")
	}
	if gradient != "" && (optStr(o, "bg_gradient_from") == "" || optStr(o, "bg_gradient_to") == "") {
		return apperr.BadRequest("options: a gradient needs its two colors (bg_gradient_from, bg_gradient_to)")
	}
	overlay := optStr(o, "overlay_color")
	alpha := optNum(o, "overlay_opacity", 0) / 100
	if overlay == "" {
		alpha = 0
	}
	// the colors the text can stand on: the solid color, the colors of the
	// gradient (ends and between), and for a picture (unknown colors) the
	// overlay over black and over white; each under the overlay
	var backs []string
	switch {
	case image != "":
		if overlay == "" || alpha == 0 {
			return apperr.BadRequest("options: text on a picture needs an overlay (overlay_color, overlay_opacity) so it can be read on every part of the picture")
		}
		backs = []string{"#000000", "#ffffff"}
	case gradient != "":
		from, to := optStr(o, "bg_gradient_from"), optStr(o, "bg_gradient_to")
		for i := 0; i <= 4; i++ {
			backs = append(backs, mixHex(from, to, float64(i)/4))
		}
	case bg != "":
		backs = []string{bg}
	}
	if text != "" && len(backs) == 0 && optBool(o, "gradient_text") {
		return apperr.BadRequest("options: text in a gradient needs a background color of its own (bg_color)")
	}
	for i := range backs {
		if alpha > 0 {
			backs[i] = mixHex(backs[i], overlay, alpha)
		}
	}
	// a picture or a gradient has every brightness between its ends: the
	// text must be brighter or darker than all of them, not in between
	if len(backs) > 1 && text != "" && !outsideRange(text, backs) {
		return apperr.BadRequest("options: the text color %s lies within the brightness of its background (%s to %s); choose a much lighter or darker text color or a stronger overlay", text, backs[0], backs[len(backs)-1])
	}
	for _, b := range backs {
		if c := contrastHex(text, b); c < min {
			return apperr.BadRequest("options: the text color %s has a contrast of %.2f:1 on %s, at least %.1f:1 is needed (WCAG 1.4.3)", text, c, b, min)
		}
		if optBool(o, "gradient_text") {
			for _, g := range []string{optStr(o, "bg_gradient_from"), optStr(o, "bg_gradient_to")} {
				if g == "" {
					return apperr.BadRequest("options: text in a gradient needs the colors of the gradient")
				}
				if c := contrastHex(g, b); c < min {
					return apperr.BadRequest("options: the text gradient color %s has a contrast of %.2f:1 on %s, at least %.1f:1 is needed (WCAG 1.4.3)", g, c, b, min)
				}
			}
		}
	}
	// columns: each at least column_min_rem wide in the narrowest window of a view
	if err := checkColumns(o["columns"]); err != nil {
		return err
	}
	if optStr(o, "from") != "" && optStr(o, "until") != "" {
		a, _ := time.Parse(time.RFC3339, optStr(o, "from"))
		b, _ := time.Parse(time.RFC3339, optStr(o, "until"))
		if !b.After(a) {
			return apperr.BadRequest("options: until must be after from")
		}
	}
	return nil
}

func optBool(o map[string]any, k string) bool {
	b, _ := o[k].(bool)
	return b
}

// MaxColumns is how many columns of text have room side by side in the
// narrowest window of a view.
func MaxColumns(view string) int {
	w := float64(blockOptions.ViewMinPx[view])/float64(blockOptions.RemPx) - 2
	return max(1, int(w/blockOptions.ColumnMinRem))
}

func checkColumns(v any) error {
	per := map[string]float64{}
	switch x := v.(type) {
	case nil:
		return nil
	case float64:
		// one value for all views: the desktop, the others inherit it
		for _, view := range blockOptions.Views {
			per[view] = x
		}
	case map[string]any:
		last := 0.0
		for _, view := range blockOptions.Views {
			if n, ok := x[view].(float64); ok {
				last = n
			}
			if last > 0 {
				per[view] = last
			}
		}
	}
	for _, view := range blockOptions.Views {
		if n, ok := per[view]; ok && int(n) > MaxColumns(view) {
			return apperr.BadRequest("options: columns: on the %s at most %d columns have room side by side (each at least %.0f rem); set fewer for this view", view, MaxColumns(view), blockOptions.ColumnMinRem)
		}
	}
	return nil
}

func hexRGB(h string) (r, g, b float64) {
	x, _ := strconv.ParseUint(h[1:], 16, 32)
	return float64(x >> 16 & 0xff), float64(x >> 8 & 0xff), float64(x & 0xff)
}

// mixHex is a over b … with t: (1-t)*a + t*b.
func mixHex(a, b string, t float64) string {
	ar, ag, ab := hexRGB(a)
	br, bg, bb := hexRGB(b)
	m := func(x, y float64) int { return int(math.Round(x*(1-t) + y*t)) }
	return fmt.Sprintf("#%02x%02x%02x", m(ar, br), m(ag, bg), m(ab, bb))
}

func luminanceHex(h string) float64 {
	r, g, b := hexRGB(h)
	lin := func(v float64) float64 {
		c := v / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrastHex is the WCAG contrast ratio of two colors #rrggbb.
func contrastHex(a, b string) float64 {
	la, lb := luminanceHex(a), luminanceHex(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// outsideRange reports whether the color is brighter or darker than every
// color of the background (a picture under its overlay, a gradient).
func outsideRange(c string, backs []string) bool {
	l := luminanceHex(c)
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, b := range backs {
		v := luminanceHex(b)
		lo, hi = min(lo, v), max(hi, v)
	}
	return l < lo || l > hi
}
