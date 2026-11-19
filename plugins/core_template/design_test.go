package core_template

import (
	"encoding/json"
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
		{"no content", func(d *Design) { d.Rows = d.Rows[3:] }, "exactly one block is the content"},
		{"two contents", func(d *Design) { d.Rows[0].Blocks[0].Kind = "content" }, "exactly one block is the content"},
		{"only content", func(d *Design) {
			d.Rows = []Row{d.Rows[2]}
			d.Rows[0].Blocks = d.Rows[0].Blocks[1:]
			d.Rows[0].Tablet, d.Rows[0].Mobile = nil, nil
		}, "at least one more block"},
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
