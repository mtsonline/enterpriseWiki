package core_template

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

func example(t *testing.T, name string) *Design {
	t.Helper()
	raw, err := os.ReadFile("examples/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := ParseDesign(raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return d
}

func TestExamplesAreValid(t *testing.T) {
	for _, n := range []string{"wiki", "cms"} {
		example(t, n)
	}
}

func problemsOf(t *testing.T, change func(d *Design)) string {
	t.Helper()
	d := example(t, "wiki")
	change(d)
	var out []string
	for _, p := range d.Problems() {
		out = append(out, p.Error())
	}
	return strings.Join(out, "\n")
}

func TestDesignProblems(t *testing.T) {
	cases := []struct {
		name   string
		change func(d *Design)
		want   string
	}{
		{"no content", func(d *Design) { d.Rows = d.Rows[3:] }, "exactly one area is the content"},
		{"two contents", func(d *Design) { d.Rows[0].Blocks[0].Kind = "content" }, "exactly one area is the content"},
		{"only content", func(d *Design) {
			d.Rows = []Row{d.Rows[2]}
			d.Rows[0].Blocks = d.Rows[0].Blocks[1:]
			d.Rows[0].Tablet, d.Rows[0].Mobile = nil, nil
		}, "at least one more area"},
		{"no room beside the content", func(d *Design) {
			b := d.Rows[2].Blocks[0]
			for i := 0; i < 3; i++ {
				c := b
				c.ID = "side" + string(rune('a'+i))
				c.Positions = []string{"extra-" + string(rune('a'+i))}
				c.Label = map[string]string{"en": "Side " + string(rune('a'+i))}
				d.Rows[2].Blocks = append(d.Rows[2].Blocks, c)
			}
		}, "the desktop has"},
		{"four narrow columns below the content fit", func(d *Design) {
			r := d.Rows[3]
			r.ID, r.Blocks = "cols", nil
			for i := 0; i < 4; i++ {
				r.Blocks = append(r.Blocks, Block{ID: "col" + string(rune('a'+i)), Kind: "area", Role: "none", Width: 12, Positions: []string{"col-" + string(rune('a'+i))}, Layout: "column", Align: "start"})
			}
			d.Rows = append(d.Rows, r)
		}, ""},
		{"tablet side by side without room", func(d *Design) { d.Rows[2].Tablet = &View{Stack: false} }, "tablet: the blocks need"},
		{"mobile side by side without room", func(d *Design) { d.Rows[2].Mobile = &View{Stack: false} }, "mobile: the blocks need"},
		{"content hidden", func(d *Design) { d.Rows[2].Mobile = &View{Stack: true, Hidden: []string{"content"}} }, "the content cannot be hidden"},
		{"unknown block in a view", func(d *Design) { d.Rows[2].Mobile = &View{Stack: true, Drawer: []string{"nope"}} }, `no block "nope"`},
		{"logo in a drawer", func(d *Design) { d.Rows[1].Mobile = &View{Drawer: []string{"brand"}} }, "the logo cannot"},
		{"the menu in a drawer on the desktop", func(d *Design) { d.Rows[1].Desktop = &View{Drawer: []string{"menu"}} }, ""},
		{"header shrinks without being sticky", func(d *Design) { d.Options.StickyHeader, d.Options.HeaderShrink = false, true }, "must be sticky"},
		{"header goes away without being sticky", func(d *Design) { d.Options.StickyHeader, d.Options.HeaderHide = false, true }, "must be sticky"},
		{"header shrinks and hides", func(d *Design) {
			d.Options.StickyHeader, d.Options.HeaderShrink, d.Options.HeaderHide, d.Options.PrintPlain = true, true, true, true
		}, ""},
		{"a sticky menu", func(d *Design) { d.Rows[1].Blocks[1].Sticky = true }, "only a side column"},
		{"a collapsible content", func(d *Design) { d.Rows[2].Blocks[1].Collapsible = true }, "only a side column"},
		{"a sticky, collapsible side column", func(d *Design) { d.Rows[2].Blocks[0].Sticky, d.Rows[2].Blocks[0].Collapsible = true, true }, ""},
		{"a collapsible side column in a desktop drawer", func(d *Design) {
			d.Rows[2].Blocks[0].Collapsible = true
			d.Rows[2].Desktop = &View{Drawer: []string{d.Rows[2].Blocks[0].ID}}
		}, "cannot also be sticky or collapsible"},
		{"a footer without names that collapses", func(d *Design) { d.Rows[3].CollapseMobile = true; d.Rows[3].Blocks[0].Label = nil }, "needs a name"},
		{"collapsing a row that is no footer", func(d *Design) { d.Rows[1].CollapseMobile = true }, "only a footer row"},
		{"the logo in a drawer on the desktop", func(d *Design) { d.Rows[1].Desktop = &View{Drawer: []string{"brand"}} }, "desktop: the logo cannot"},
		{"the content in a drawer on the desktop", func(d *Design) { d.Rows[2].Desktop = &View{Drawer: []string{"content"}} }, "desktop: the content cannot"},
		{"the desktop changes the layout", func(d *Design) { d.Rows[1].Desktop = &View{Stack: true} }, "desktop: only areas behind a button"},
		{"positions side by side without room", func(d *Design) {
			b := &d.Rows[1].Blocks[2]
			b.Fit, b.Width, b.Layout = false, 10, "row"
			b.Positions = []string{"tool-a", "tool-b", "tool-c"}
		}, "its 3 positions side by side need 13.0 rem, the area is 10 rem wide"},
		{"positions side by side fit", func(d *Design) {
			b := &d.Rows[1].Blocks[2]
			b.Fit, b.Width, b.Layout = false, 14, "row"
			b.Positions = []string{"tool-a", "tool-b", "tool-c"}
			d.Rows[1].Mobile = &View{Stack: true, Drawer: []string{"menu"}}
		}, ""},
		{"positions one below the other need no room side by side", func(d *Design) {
			b := &d.Rows[1].Blocks[2]
			b.Fit, b.Width, b.Layout = false, 10, "column"
			b.Positions = []string{"tool-a", "tool-b", "tool-c"}
			d.Rows[1].Mobile = &View{Stack: true, Drawer: []string{"menu"}}
		}, ""},
		{"positions side by side without room on the tablet", func(d *Design) {
			b := &d.Rows[1].Blocks[2]
			b.Fit, b.Width, b.Layout = false, 14, "row"
			b.Positions = []string{"tool-a", "tool-b", "tool-c"}
			d.Rows[1].Tablet = &View{Widths: map[string]int{b.ID: 10}}
		}, `tablet: the 3 positions of "tools" side by side need 13.0 rem`},
		{"a row of blocks with many positions has no room", func(d *Design) {
			for i := range d.Rows[1].Blocks {
				b := &d.Rows[1].Blocks[i]
				if b.Kind == "area" {
					b.Fit, b.Width, b.Layout = false, 0, "row"
					for j := 0; j < 7; j++ {
						b.Positions = append(b.Positions, b.ID+"-"+string(rune('a'+j)))
					}
				}
			}
		}, "the desktop has"},
		{"too narrow", func(d *Design) { d.Rows[1].Blocks[2].Width = 3 }, "the width is 8 to 40 rem"},
		{"too wide", func(d *Design) { d.Rows[1].Blocks[2].Width = 41 }, "the width is 8 to 40 rem"},
		{"position twice", func(d *Design) { d.Rows[0].Blocks[0].Positions = []string{"footer"} }, `the position "footer" is used twice`},
		{"reserved position", func(d *Design) { d.Rows[0].Blocks[0].Positions = []string{"element-header"} }, "belongs to the pages themselves"},
		{"bad position name", func(d *Design) { d.Rows[0].Blocks[0].Positions = []string{"Top Bar"} }, "must be lowercase"},
		{"id twice", func(d *Design) { d.Rows[3].Blocks[0].ID = "menu" }, `the id "menu" is used twice`},
		{"header in two parts", func(d *Design) { d.Rows[3].Role = "banner" }, "the rows of the header must follow each other"},
		{"content in the header", func(d *Design) { d.Rows[2].Role = "banner" }, "the content cannot be in the header"},
		{"footer above the content", func(d *Design) { d.Rows[0].Role = "contentinfo"; d.Rows[1].Role = "none"; d.Rows[3].Role = "none" }, "the footer must be below the content"},
		{"block role banner", func(d *Design) { d.Rows[0].Blocks[0].Role = "banner" }, "role: one of"},
		{"sticky without header", func(d *Design) { d.Rows[0].Role, d.Rows[1].Role = "none", "none" }, "no header"},
		{"header on the hero without header", func(d *Design) {
			d.Options.StickyHeader, d.Options.HeaderOverlay, d.Rows[0].Role, d.Rows[1].Role = false, true, "none", "none"
		}, "to lie on the hero banner"},
		{"header on the hero", func(d *Design) { d.Options.HeaderOverlay = true }, ""},
		{"two navigations without labels", func(d *Design) { d.Rows[0].Blocks[0].Role = "navigation" }, "needs a label"},
		{"two navigations with the same label", func(d *Design) {
			d.Rows[0].Blocks[0].Role = "navigation"
			d.Rows[0].Blocks[0].Label = d.Rows[1].Blocks[1].Label
		}, "need different labels"},
		{"region without label", func(d *Design) { d.Rows[0].Blocks[0].Role = "region" }, "needs a label"},
		{"label with markup", func(d *Design) { d.Rows[1].Blocks[1].Label = map[string]string{"en": "<b>x</b>"} }, "plain text"},
		{"low contrast text", func(d *Design) { d.Tokens.Text = "#aaaaaa" }, "text on bg has a contrast of"},
		{"low contrast focus", func(d *Design) { d.Tokens.Focus = "#f0f0f0" }, "focus on bg"},
		{"low contrast header", func(d *Design) { d.Tokens.HeaderText = "#777777"; d.Tokens.HeaderBg = "#666666" }, "header_text on header_bg"},
		{"color format", func(d *Design) { d.Tokens.Bg = "red" }, "a color as #rrggbb"},
		{"foreign font", func(d *Design) { d.Tokens.Font = "Comic Sans" }, "tokens.font"},
		{"logo elsewhere", func(d *Design) { d.Tokens.Logo = "https://example.com/x.png" }, "tokens.logo"},
		{"scale", func(d *Design) { d.Tokens.Scale = 80 }, "tokens.scale"},
		{"too many rows", func(d *Design) {
			for len(d.Rows) <= 20 {
				r := d.Rows[0]
				r.ID = "r" + strings.Repeat("x", len(d.Rows))
				r.Blocks = []Block{{ID: "b" + strings.Repeat("x", len(d.Rows)), Kind: "area", Role: "none", Layout: "row", Align: "start", Positions: []string{}}}
				d.Rows = append(d.Rows, r)
			}
		}, "1 to 20 rows"},
	}
	for _, c := range cases {
		got := problemsOf(t, c.change)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: want %q, got:\n%s", c.name, c.want, got)
		}
	}
}

func TestParseDesignRefusesUnknownFields(t *testing.T) {
	raw, _ := os.ReadFile("examples/wiki.json")
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["script"] = "alert(1)"
	b, _ := json.Marshal(m)
	if _, err := ParseDesign(b); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field: %v", err)
	}
	if _, err := ParseDesign([]byte(strings.Repeat(" ", 70000))); err == nil {
		t.Fatal("too large")
	}
}

func TestContrast(t *testing.T) {
	if c := Contrast("#000000", "#ffffff"); c < 20.9 || c > 21.1 {
		t.Fatalf("black on white: %v", c)
	}
	if c := Contrast("#767676", "#ffffff"); c < 4.5 || c > 4.6 {
		t.Fatalf("#767676 on white: %v", c)
	}
}

var update = flag.Bool("update", false, "write testdata/room.json")

// TestRoomShared: the numbers of room the server computes for the shipped
// examples are in testdata/room.json; the unit test of the browser checks
// the engine against the same file, so designer and server always agree.
// go test ./plugins/core_template -run TestRoomShared -update writes it.
func TestRoomShared(t *testing.T) {
	type rowRoom struct {
		Row       string             `json:"row"`
		View      string             `json:"view"`
		Need      float64            `json:"need"`
		Room      float64            `json:"room"`
		Positions map[string]float64 `json:"positions"`
	}
	got := map[string][]rowRoom{}
	for _, name := range []string{"wiki", "cms", "cms_boxed"} {
		raw, err := os.ReadFile("examples/" + strings.TrimSuffix(name, "_boxed") + ".json")
		if err != nil {
			t.Fatal(err)
		}
		d, err := ParseDesign(raw)
		if err != nil {
			t.Fatal(err)
		}
		// boxed: every row has at most the content width
		if strings.HasSuffix(name, "_boxed") {
			d.Options.Boxed, d.Tokens.ContentWidth = true, 50
		}
		for _, r := range d.Rows {
			for _, view := range []string{"desktop", "tablet", "mobile"} {
				var v *View
				switch view {
				case "tablet":
					v = r.Tablet
				case "mobile":
					v = r.Mobile
				}
				pos := map[string]float64{}
				for _, b := range r.Blocks {
					pos[b.ID] = positionsRoom(b)
				}
				got[name] = append(got[name], rowRoom{r.ID, view, d.needRoom(r, view, v), d.room(r, view), pos})
			}
		}
	}
	out, _ := json.MarshalIndent(got, "", " ")
	out = append(out, '\n')
	if *update {
		if err := os.WriteFile("testdata/room.json", out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/room.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(out) {
		t.Fatalf("testdata/room.json is not what the server computes; run with -update and check the browser unit test\n%s", out)
	}
}

// TestContrastShared: the contrast the server computes for the pairs of the
// shipped examples and a few fixed colors is in testdata/contrast.json; the
// unit test of the browser checks the designer against the same file.
func TestContrastShared(t *testing.T) {
	type c struct {
		A     string  `json:"a"`
		B     string  `json:"b"`
		Ratio float64 `json:"ratio"`
	}
	var got []c
	add := func(a, b string) { got = append(got, c{a, b, math.Round(Contrast(a, b)*1e6) / 1e6}) }
	for _, p := range [][2]string{{"#000000", "#ffffff"}, {"#777777", "#ffffff"}, {"#bbbbbb", "#ffffff"}, {"#7a1f5c", "#f6f7f9"}, {"#ff0000", "#00ff00"}} {
		add(p[0], p[1])
	}
	for _, name := range []string{"wiki", "cms"} {
		raw, err := os.ReadFile("examples/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var d struct {
			Tokens map[string]any `json:"tokens"`
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatal(err)
		}
		for _, p := range ContrastPairs {
			add(d.Tokens[p.Fore].(string), d.Tokens[p.Back].(string))
		}
	}
	out, _ := json.MarshalIndent(got, "", " ")
	out = append(out, '\n')
	if *update {
		if err := os.WriteFile("testdata/contrast.json", out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/contrast.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(out) {
		t.Fatalf("testdata/contrast.json is not what the server computes; run with -update\n%s", out)
	}
	if len(ContrastPairs) < 10 {
		t.Fatalf("contrast pairs from limits.json: %d", len(ContrastPairs))
	}
}

// TestCSSCases: own CSS is checked by the rules of limits.json; the browser
// unit test checks the designer with the same cases.
func TestCSSCases(t *testing.T) {
	raw, err := os.ReadFile("testdata/css_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		CSS string `json:"css"`
		OK  bool   `json:"ok"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		var got []string
		cssProblems(c.CSS, func(where, format string, args ...any) { got = append(got, fmt.Sprintf(format, args...)) })
		if (len(got) == 0) != c.OK {
			t.Errorf("%q: ok=%v, problems %v", c.CSS, c.OK, got)
		}
	}
	// the size
	var got []string
	cssProblems(strings.Repeat("a", limits.CSS.MaxBytes+1), func(where, format string, args ...any) { got = append(got, format) })
	if len(got) == 0 {
		t.Error("too large CSS accepted")
	}
}
