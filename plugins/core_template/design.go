package core_template

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

// Design is a data template: rows of blocks with positions, design values
// and per view (tablet, mobile) changes. It is only data; the page skeleton
// and its CSS are built from it by the same code in the designer and in the
// wiki (web/engine.ts), so what is designed is what is shown.
type Design struct {
	Version int     `json:"version"`
	Tokens  Tokens  `json:"tokens"`
	Rows    []Row   `json:"rows"`
	Options Options `json:"options"`
	// CSS: own CSS of the template (only with ui.custom_css; see css.go)
	CSS string `json:"css,omitempty"`
}

// Tokens are the design values (CSS custom properties of the template).
type Tokens struct {
	Bg         string `json:"bg"`
	BgSubtle   string `json:"bg_subtle"`
	Text       string `json:"text"`
	TextMuted  string `json:"text_muted"`
	Border     string `json:"border"`
	Primary    string `json:"primary"`
	OnPrimary  string `json:"on_primary"`
	Link       string `json:"link"`
	Focus      string `json:"focus"`
	HeaderBg   string `json:"header_bg"`
	HeaderText string `json:"header_text"`
	FooterBg   string `json:"footer_bg"`
	FooterText string `json:"footer_text"`
	// Font and HeadingFont: one of limits.fonts (only fonts of the wiki itself)
	Font        string `json:"font"`
	HeadingFont string `json:"heading_font"`
	// Scale: the size of the text in percent
	Scale int `json:"scale"`
	// Radius: rounded corners in px
	Radius int `json:"radius"`
	// ContentWidth: the width of rows with width "content", in rem
	ContentWidth int `json:"content_width"`
	// Logo: a picture of the wiki (/api/v1/files/…), empty: none
	Logo string `json:"logo"`
}

// Row is a row of the page with blocks side by side.
type Row struct {
	ID string `json:"id"`
	// Role: banner (the header of the page), contentinfo (the footer) or
	// none; adjacent rows of the same role form one landmark
	Role string `json:"role"`
	// Width: "full" (the whole window) or "content" (limited to ContentWidth)
	Width string `json:"width"`
	// Background: page, subtle, header or footer (colors of the tokens)
	Background string  `json:"background"`
	Blocks     []Block `json:"blocks"`
	// Desktop: only the drawer (blocks behind a button, a menu as "burger"
	// also on the desktop); what is in it is behind a button in every view
	Desktop *View `json:"desktop,omitempty"`
	// Tablet and Mobile change the row for these views; nil: automatic
	Tablet *View `json:"tablet,omitempty"`
	Mobile *View `json:"mobile,omitempty"`
	// CollapseMobile: on the phone each block of this footer row opens and
	// closes under its name (footer rows only; every block needs a label)
	CollapseMobile bool `json:"collapse_mobile,omitempty"`
}

// Block is an area of a row.
type Block struct {
	ID string `json:"id"`
	// Kind: content (the content of the page, exactly once), area (with
	// positions) or brand (logo and name of the wiki, linked to the start)
	Kind string `json:"kind"`
	// Role for screen readers: navigation, complementary, region or none
	// (the content is always main; header and footer are roles of rows)
	Role string `json:"role"`
	// Label names the block for screen readers, per language
	Label map[string]string `json:"label,omitempty"`
	// Width in rem; 0 takes the rest of the row
	Width int `json:"width"`
	// Fit: as wide as its content (at least the minimum of its kind; it
	// may get narrower down to it when the row is full); Width must be 0
	Fit bool `json:"fit,omitempty"`
	// Positions offered in the block (in order); for the content those
	// before the content of the page
	Positions []string `json:"positions"`
	// After: positions after the content of the page (content only)
	After []string `json:"after,omitempty"`
	// Layout of the positions inside: row (side by side) or column
	Layout string `json:"layout"`
	// Align of the positions inside: start, center, end or between
	Align string `json:"align"`
	// Sticky: a side column stays in view while the page scrolls (desktop;
	// scrolls in itself when higher than the window); complementary only
	Sticky bool `json:"sticky,omitempty"`
	// Collapsible: a side column can be hidden with a button; the choice is
	// kept in the browser (desktop); complementary only
	Collapsible bool `json:"collapsible,omitempty"`
}

// View changes a row for a view.
type View struct {
	// Stack: the blocks one below the other
	Stack bool `json:"stack"`
	// Hidden: blocks not shown in this view
	Hidden []string `json:"hidden,omitempty"`
	// Drawer: blocks behind a button that opens them (menu, side column)
	Drawer []string `json:"drawer,omitempty"`
	// Widths: the width of blocks in this view in rem (0: the rest of the
	// row); blocks not named keep their width
	Widths map[string]int `json:"widths,omitempty"`
}

// Options of the whole template.
type Options struct {
	// BackToTop: a button "to the top" at long pages
	BackToTop bool `json:"back_to_top"`
	// StickyHeader: the header (rows of the role banner) stays at the top
	StickyHeader bool `json:"sticky_header"`
	// HeaderShrink: the sticky header gets lower once the page is scrolled
	HeaderShrink bool `json:"header_shrink,omitempty"`
	// HeaderHide: the sticky header goes away while scrolling down and comes
	// back when scrolling up or when the keyboard focus is in it
	HeaderHide bool `json:"header_hide,omitempty"`
	// PrintPlain: printing shows only the content (no header, footer, side
	// columns, drawer buttons)
	PrintPlain bool `json:"print_plain,omitempty"`
}

// Limits are the limits of data templates (limits.json, the same file the
// designer reads).
type Limits struct {
	RemPx int `json:"rem_px"`
	Views map[string]struct {
		MinPx         int `json:"min_px"`
		ContentMinRem int `json:"content_min_rem"`
	} `json:"views"`
	GapRem               float64        `json:"gap_rem"`
	PaddingRem           float64        `json:"padding_rem"`
	BlockMinRem          map[string]int `json:"block_min_rem"`
	BlockMaxRem          int            `json:"block_max_rem"`
	PositionMinRem       float64        `json:"position_min_rem"`
	PositionGapRem       float64        `json:"position_gap_rem"`
	StickyMaxVh          int            `json:"sticky_max_vh"`
	MaxRows              int            `json:"max_rows"`
	MaxBlocksPerRow      int            `json:"max_blocks_per_row"`
	MaxBlocks            int            `json:"max_blocks"`
	MaxPositionsPerBlock int            `json:"max_positions_per_block"`
	MaxPositions         int            `json:"max_positions"`
	LabelMax             int            `json:"label_max"`
	NameMax              int            `json:"name_max"`
	ScalePercent         [2]int         `json:"scale_percent"`
	RadiusPx             [2]int         `json:"radius_px"`
	ContentWidthRem      [2]int         `json:"content_width_rem"`
	ContrastText         float64        `json:"contrast_text"`
	ContrastUI           float64        `json:"contrast_ui"`
	ContrastPairs        []ContrastPair `json:"contrast_pairs"`
	Fonts                []string       `json:"fonts"`
	MaxBytes             int            `json:"max_bytes"`
	CSS                  struct {
		MaxBytes  int         `json:"max_bytes"`
		Forbidden [][2]string `json:"forbidden"`
		URL       string      `json:"url"`
	} `json:"css"`
}

//go:embed limits.json
var limitsJSON []byte

// limits are the parsed limits (the file is part of the program).
var limits = func() Limits {
	var l Limits
	if err := json.Unmarshal(limitsJSON, &l); err != nil {
		panic("core_template: limits.json: " + err.Error())
	}
	return l
}()

var (
	idRe    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	posRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	colorRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)
	logoRe  = regexp.MustCompile(`^/api/v1/files/[A-Za-z0-9._-]{1,128}$`)
	langRe  = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)
)

// reservedPositions start the positions the pages create themselves.
var reservedPositions = []string{"element-", "category-", "editor-", "dashboard-"}

var (
	roles       = []string{"navigation", "complementary", "region", "none"}
	rowRoles    = []string{"banner", "contentinfo", "none"}
	kinds       = []string{"content", "area", "brand"}
	widths      = []string{"full", "content"}
	backgrounds = []string{"page", "subtle", "header", "footer"}
	layouts     = []string{"row", "column"}
	aligns      = []string{"start", "center", "end", "between"}
)

// Problem is a reason a design cannot be saved: where and why, in words a
// person understands (the designer shows the same).
type Problem struct {
	Where string `json:"where"`
	Text  string `json:"text"`
}

func (p Problem) Error() string { return p.Where + ": " + p.Text }

// ParseDesign reads and checks a design (unknown fields are refused, so a
// typo is never silently ignored).
func ParseDesign(raw []byte) (*Design, error) {
	if len(raw) > limits.MaxBytes {
		return nil, apperr.BadRequest("a template has at most %d bytes", limits.MaxBytes)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var d Design
	if err := dec.Decode(&d); err != nil {
		return nil, apperr.BadRequest("the template is no valid JSON of a template: %v", err)
	}
	if dec.More() {
		return nil, apperr.BadRequest("the template is no valid JSON of a template: more than one value")
	}
	if err := d.Check(); err != nil {
		return nil, err
	}
	return &d, nil
}

// Check checks a design against the limits; the first problem is returned
// as a bad request with where and why.
func (d *Design) Check() error {
	if ps := d.Problems(); len(ps) > 0 {
		return apperr.BadRequest("%s", ps[0].Error())
	}
	return nil
}

// Problems lists all problems of a design (empty: it can be saved).
func (d *Design) Problems() []Problem {
	var out []Problem
	add := func(where, format string, args ...any) {
		out = append(out, Problem{where, fmt.Sprintf(format, args...)})
	}
	if d.Version != 1 {
		add("version", "must be 1")
	}
	d.Tokens.problems(add)
	cssProblems(d.CSS, add)
	if (d.Options.HeaderShrink || d.Options.HeaderHide) && !d.Options.StickyHeader {
		add("options", "a header that gets lower or goes away while scrolling must be sticky (sticky_header)")
	}
	if len(d.Rows) == 0 || len(d.Rows) > limits.MaxRows {
		add("rows", "1 to %d rows", limits.MaxRows)
	}
	ids := map[string]bool{}
	positions := map[string]bool{}
	nBlocks, contents, others := 0, 0, 0
	contentRow := -1
	roleCount := map[string]int{}
	roleLabels := map[string][]map[string]string{}
	for ri, r := range d.Rows {
		where := fmt.Sprintf("row %d", ri+1)
		if !idRe.MatchString(r.ID) {
			add(where, "the id %q must be lowercase letters, digits and dashes, starting with a letter", r.ID)
		} else if ids[r.ID] {
			add(where, "the id %q is used twice", r.ID)
		}
		ids[r.ID] = true
		if !slices.Contains(rowRoles, r.Role) {
			add(where, "role: one of %v", rowRoles)
		}
		if !slices.Contains(widths, r.Width) {
			add(where, "width: one of %v", widths)
		}
		if !slices.Contains(backgrounds, r.Background) {
			add(where, "background: one of %v", backgrounds)
		}
		if r.CollapseMobile && r.Role != "contentinfo" {
			add(where, "only a footer row (role contentinfo) opens and closes on the phone")
		}
		if len(r.Blocks) == 0 || len(r.Blocks) > limits.MaxBlocksPerRow {
			add(where, "1 to %d blocks side by side", limits.MaxBlocksPerRow)
		}
		nBlocks += len(r.Blocks)
		for bi, b := range r.Blocks {
			bw := fmt.Sprintf("%s, block %d", where, bi+1)
			if !idRe.MatchString(b.ID) {
				add(bw, "the id %q must be lowercase letters, digits and dashes, starting with a letter", b.ID)
			} else if ids[b.ID] {
				add(bw, "the id %q is used twice", b.ID)
			}
			ids[b.ID] = true
			if !slices.Contains(kinds, b.Kind) {
				add(bw, "kind: one of %v", kinds)
				continue
			}
			if b.Kind == "content" {
				contents++
				contentRow = ri
				if b.Width != 0 || b.Fit {
					add(bw, "the content takes the rest of the row (width 0)")
				}
			} else {
				others++
				if !slices.Contains(roles, b.Role) {
					add(bw, "role: one of %v", roles)
				}
				roleCount[b.Role]++
				roleLabels[b.Role] = append(roleLabels[b.Role], b.Label)
				min := limits.BlockMinRem[b.Kind]
				if b.Fit && b.Width != 0 {
					add(bw, "a block as wide as its content has no width of its own (width 0)")
				}
				if b.Width != 0 && (b.Width < min || b.Width > limits.BlockMaxRem) {
					add(bw, "the width is %d to %d rem (or 0: the rest of the row)", min, limits.BlockMaxRem)
				} else if need := positionsRoom(b); b.Width != 0 && need > float64(b.Width) {
					add(bw, "its %d positions side by side need %.1f rem, the block is %d rem wide; make it wider or put the positions one below the other (layout column)", len(b.Positions), need, b.Width)
				}
				if len(b.After) > 0 {
					add(bw, "only the content has positions after it")
				}
			}
			if (b.Sticky || b.Collapsible) && (b.Kind == "content" || b.Role != "complementary") {
				add(bw, "only a side column (role complementary) can be sticky or collapsible")
			}
			if (b.Sticky || b.Collapsible) && r.Desktop != nil && slices.Contains(r.Desktop.Drawer, b.ID) {
				add(bw, "a side column behind a button on the desktop (drawer) cannot also be sticky or collapsible")
			}
			if r.CollapseMobile && len(b.Label) == 0 {
				add(bw, "a footer that opens and closes on the phone needs a name for every block (label)")
			}
			if !slices.Contains(layouts, b.Layout) {
				add(bw, "layout: one of %v", layouts)
			}
			if !slices.Contains(aligns, b.Align) {
				add(bw, "align: one of %v", aligns)
			}
			for l, v := range b.Label {
				if !langRe.MatchString(l) || strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > limits.LabelMax || strings.ContainsAny(v, "<>") {
					add(bw, "a label is plain text of 1 to %d characters per language", limits.LabelMax)
				}
			}
			all := append(slices.Clone(b.Positions), b.After...)
			if len(all) > limits.MaxPositionsPerBlock {
				add(bw, "at most %d positions in a block", limits.MaxPositionsPerBlock)
			}
			for _, p := range all {
				switch {
				case !posRe.MatchString(p):
					add(bw, "the position %q must be lowercase letters, digits and dashes, starting with a letter", p)
				case slices.ContainsFunc(reservedPositions, func(x string) bool { return strings.HasPrefix(p, x) }):
					add(bw, "the position %q belongs to the pages themselves (%v)", p, reservedPositions)
				case positions[p]:
					add(bw, "the position %q is used twice", p)
				}
				positions[p] = true
			}
		}
		d.fitProblems(ri, r, add)
	}
	if nBlocks > limits.MaxBlocks {
		add("rows", "at most %d blocks in all", limits.MaxBlocks)
	}
	if len(positions) > limits.MaxPositions {
		add("rows", "at most %d positions in all", limits.MaxPositions)
	}
	if contents != 1 {
		add("rows", "exactly one block is the content of the page (%d now)", contents)
	}
	if others == 0 {
		add("rows", "besides the content at least one more block is needed (for the user menu and signing in)")
	}
	// header and footer: adjacent rows (one landmark each), the header
	// above the content, the footer below it; the content is in neither
	for _, role := range []string{"banner", "contentinfo"} {
		first, last, n := -1, -1, 0
		for ri, r := range d.Rows {
			if r.Role == role {
				if first < 0 {
					first = ri
				}
				last = ri
				n++
			}
		}
		name := map[string]string{"banner": "header", "contentinfo": "footer"}[role]
		switch {
		case n == 0:
		case last-first+1 != n:
			add("rows", "the rows of the %s must follow each other (they form one %s)", name, name)
		case contentRow >= 0 && first <= contentRow && contentRow <= last:
			add("rows", "the content cannot be in the %s", name)
		case role == "banner" && contentRow >= 0 && first > contentRow:
			add("rows", "the header must be above the content")
		case role == "contentinfo" && contentRow >= 0 && last < contentRow:
			add("rows", "the footer must be below the content")
		}
	}
	if d.Options.StickyHeader && !slices.ContainsFunc(d.Rows, func(r Row) bool { return r.Role == "banner" }) {
		add("options.sticky_header", "there is no header (a row with the role banner) to stay at the top")
	}
	// landmarks: several of a role need different labels, a region always
	// one (WCAG 1.3.1, 2.4.1)
	for role, labels := range roleLabels {
		if role == "none" {
			continue
		}
		if role == "region" || len(labels) > 1 {
			seen := map[string]bool{}
			for _, l := range labels {
				name := fmt.Sprint(l)
				if len(l) == 0 {
					add("rows", "every block with the role %s needs a label (it is named for screen readers)", role)
					break
				}
				if seen[name] {
					add("rows", "the blocks with the role %s need different labels", role)
					break
				}
				seen[name] = true
			}
		}
	}
	return out
}

// fitProblems checks that the blocks of a row have room side by side: on
// the desktop always, on tablet and mobile when the view keeps them side by
// side; hidden and drawer blocks of the view must exist in the row.
func (d *Design) fitProblems(ri int, r Row, add func(where, format string, args ...any)) {
	where := fmt.Sprintf("row %d", ri+1)
	ids := map[string]Block{}
	for _, b := range r.Blocks {
		ids[b.ID] = b
	}
	if r.Desktop != nil && (r.Desktop.Stack || len(r.Desktop.Hidden) > 0 || len(r.Desktop.Widths) > 0) {
		add(where, "desktop: only blocks behind a button can be chosen; the layout of the desktop is the row itself")
	}
	if need, room := d.needRoom(r, "desktop", r.Desktop), d.room(r, "desktop"); need > room {
		add(where, "the blocks need %.0f rem side by side, the desktop has %.0f rem here; make blocks narrower or move one to another row", need, room)
	}
	for _, v := range []struct {
		name string
		view *View
	}{{"desktop", r.Desktop}, {"tablet", r.Tablet}, {"mobile", r.Mobile}} {
		if v.view == nil {
			continue
		}
		for _, id := range append(slices.Clone(v.view.Hidden), v.view.Drawer...) {
			b, ok := ids[id]
			switch {
			case !ok:
				add(where, "%s: there is no block %q in this row", v.name, id)
			case b.Kind == "content":
				add(where, "%s: the content cannot be hidden or put in a drawer", v.name)
			}
		}
		for _, id := range v.view.Drawer {
			if b, ok := ids[id]; ok && b.Kind == "brand" {
				add(where, "%s: the logo cannot be put in a drawer", v.name)
			}
		}
		for id, w := range v.view.Widths {
			b, ok := ids[id]
			min := limits.BlockMinRem[b.Kind]
			switch {
			case !ok:
				add(where, "%s: there is no block %q in this row", v.name, id)
			case b.Kind == "content" && w != 0:
				add(where, "%s: the content takes the rest of the row (width 0)", v.name)
			case w != 0 && (w < min || w > limits.BlockMaxRem):
				add(where, "%s: the width of %q is %d to %d rem (or 0: the rest of the row)", v.name, id, min, limits.BlockMaxRem)
			case w != 0 && positionsRoom(b) > float64(w):
				add(where, "%s: the %d positions of %q side by side need %.1f rem, it is %d rem wide; make it wider or put the positions one below the other", v.name, len(b.Positions), id, positionsRoom(b), w)
			}
		}
		if !v.view.Stack && v.name != "desktop" {
			if need, room := d.needRoom(r, v.name, withDesktopDrawer(r, v.view)), d.room(r, v.name); need > room {
				add(where, "%s: the blocks need %.0f rem side by side, but there are only %.0f rem; let them go one below the other", v.name, need, room)
			}
		}
	}
}

// positionsRoom is the width (rem) the positions of a block need side by
// side (layout row): each at least position_min_rem, with the gap between
// them. A block one below the other (layout column) needs no more than one.
func positionsRoom(b Block) float64 {
	n := len(b.Positions)
	if b.Layout != "row" || n < 2 {
		return 0
	}
	return float64(n)*limits.PositionMinRem + float64(n-1)*limits.PositionGapRem
}

// minWidth is the narrowest a block without a width of its own can be: the
// minimum of its kind, or more when its positions stand side by side.
func minWidth(b Block) float64 {
	return max(float64(limits.BlockMinRem[b.Kind]), positionsRoom(b))
}

// withDesktopDrawer adds the drawer of the desktop to a view: what is behind
// a button on the desktop is behind one in every view.
func withDesktopDrawer(r Row, v *View) *View {
	if r.Desktop == nil || len(r.Desktop.Drawer) == 0 {
		return v
	}
	x := View{}
	if v != nil {
		x = *v
	}
	x.Drawer = slices.Clone(x.Drawer)
	for _, id := range r.Desktop.Drawer {
		if !slices.Contains(x.Drawer, id) {
			x.Drawer = append(x.Drawer, id)
		}
	}
	return &x
}

// needRoom is the width the blocks of a row need side by side in a view.
func (d *Design) needRoom(r Row, view string, v *View) float64 {
	sum, n := 0.0, 0
	for _, b := range r.Blocks {
		if v != nil && (slices.Contains(v.Hidden, b.ID) || slices.Contains(v.Drawer, b.ID)) {
			continue
		}
		n++
		w := b.Width
		if v != nil {
			if x, ok := v.Widths[b.ID]; ok {
				w = x
			}
		}
		if b.Kind == "content" {
			sum += float64(limits.Views[view].ContentMinRem)
		} else if w > 0 {
			sum += float64(w)
		} else {
			sum += minWidth(b)
		}
	}
	if n > 1 {
		sum += limits.GapRem * float64(n-1)
	}
	return sum
}

// room is the width a row has in the narrowest window of a view.
func (d *Design) room(r Row, view string) float64 {
	w := float64(limits.Views[view].MinPx) / float64(limits.RemPx)
	if r.Width == "content" && float64(d.Tokens.ContentWidth) < w {
		w = float64(d.Tokens.ContentWidth)
	}
	return w - 2*limits.PaddingRem
}

func (t *Tokens) problems(add func(where, format string, args ...any)) {
	colors := map[string]string{"bg": t.Bg, "bg_subtle": t.BgSubtle, "text": t.Text, "text_muted": t.TextMuted, "border": t.Border, "primary": t.Primary,
		"on_primary": t.OnPrimary, "link": t.Link, "focus": t.Focus, "header_bg": t.HeaderBg, "header_text": t.HeaderText, "footer_bg": t.FooterBg, "footer_text": t.FooterText}
	ok := true
	for _, k := range []string{"bg", "bg_subtle", "text", "text_muted", "border", "primary", "on_primary", "link", "focus", "header_bg", "header_text", "footer_bg", "footer_text"} {
		if !colorRe.MatchString(colors[k]) {
			add("tokens."+k, "a color as #rrggbb (lowercase)")
			ok = false
		}
	}
	if ok {
		for _, p := range ContrastPairs {
			if c := Contrast(colors[p.Fore], colors[p.Back]); c < p.Min() {
				add("tokens."+p.Fore, "%s on %s has a contrast of %.2f:1, at least %.1f:1 is needed (WCAG %s)", p.Fore, p.Back, c, p.Min(), p.Rule)
			}
		}
	}
	if !slices.Contains(limits.Fonts, t.Font) {
		add("tokens.font", "one of %v", limits.Fonts)
	}
	if !slices.Contains(limits.Fonts, t.HeadingFont) {
		add("tokens.heading_font", "one of %v", limits.Fonts)
	}
	if t.Scale < limits.ScalePercent[0] || t.Scale > limits.ScalePercent[1] {
		add("tokens.scale", "%d to %d percent", limits.ScalePercent[0], limits.ScalePercent[1])
	}
	if t.Radius < limits.RadiusPx[0] || t.Radius > limits.RadiusPx[1] {
		add("tokens.radius", "%d to %d px", limits.RadiusPx[0], limits.RadiusPx[1])
	}
	if t.ContentWidth < limits.ContentWidthRem[0] || t.ContentWidth > limits.ContentWidthRem[1] {
		add("tokens.content_width", "%d to %d rem", limits.ContentWidthRem[0], limits.ContentWidthRem[1])
	}
	if t.Logo != "" && !logoRe.MatchString(t.Logo) {
		add("tokens.logo", "a picture of the wiki (/api/v1/files/…) or empty")
	}
}

// ContrastPair is a pair of colors whose contrast is checked.
type ContrastPair struct {
	Fore string `json:"fore"`
	Back string `json:"back"`
	// UI: a control or border (3:1, WCAG 1.4.11), else text (4.5:1, WCAG 1.4.3)
	UI   bool   `json:"ui"`
	Rule string `json:"rule"`
}

// Min is the contrast the pair needs.
func (p ContrastPair) Min() float64 {
	if p.UI {
		return limits.ContrastUI
	}
	return limits.ContrastText
}

// ContrastPairs are all pairs of the tokens that are shown together (from
// limits.json, so the designer checks the same pairs).
var ContrastPairs = limits.ContrastPairs

// Contrast is the contrast ratio of two colors #rrggbb (WCAG 2.2).
func Contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(hex string) float64 {
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return 0
	}
	ch := func(v int) float64 {
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(r) + 0.7152*ch(g) + 0.0722*ch(b)
}
