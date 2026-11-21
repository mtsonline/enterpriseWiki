package server_test

import (
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/web"
)

// TestLayoutBlocks: the layout blocks render readable HTML without script,
// every block in the content has the options of blocks (checked by the
// server, refused ones are not used and say so), and what stands side by
// side needs room.
func TestLayoutBlocks(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var cat struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Blocks"}}, &cat, 201)
	render := func(src string) string {
		t.Helper()
		var el struct {
			ID   int64  `json:"id"`
			HTML string `json:"html"`
		}
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "B", "body": map[string]any{"format": "markdown", "source": src}}, &el, 201)
		admin.must("GET", "/api/v1/elements/"+itoa(el.ID), nil, &el, 200)
		return el.HTML
	}
	// every block in the content offers the options
	var macros []struct {
		Name   string `json:"name"`
		Inline bool   `json:"inline"`
		Params []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"params"`
	}
	admin.must("GET", "/api/v1/macros", nil, &macros, 200)
	seen := map[string]bool{}
	for _, m := range macros {
		seen[m.Name] = true
		has := false
		for _, p := range m.Params {
			has = has || (p.Name == "options" && p.Type == "custom:blockoptions")
		}
		if has == m.Inline {
			t.Errorf("macro %s (inline %v): options offered %v", m.Name, m.Inline, has)
		}
	}
	for _, n := range []string{"grid", "heading", "divider", "buttons", "cta", "tabs", "accordion"} {
		if !seen[n] {
			t.Errorf("block %s missing", n)
		}
	}
	html := render(`:::grid{desktop=3 tablet=2 options="bg_color:#102040 padding_top:4 text_color:#ffffff"}
One
***
Two
***
Three
:::

::heading{text="Big title" level=3 size=xl line=true}

:::tabs{label="Steps"}
## First
A
***
## Second
B
:::

:::accordion{single=true first=true}
### Question
Answer
***
### Other
More
:::

::buttons{text1="Go" link1="/c/1" text2="Bad" link2="javascript:alert(1)"}

::divider{look=dotted}
`)
	for _, want := range []string{
		`class="ewl-grid ewl-grid--d3 ewl-grid--t2 ewl-grid--m1`, `<div class="ewl-cell">`,
		`data-ewbo="{&#34;bg_color&#34;:&#34;#102040&#34;,&#34;padding_top&#34;:4,&#34;text_color&#34;:&#34;#ffffff&#34;}"`,
		`<h3>Big title</h3>`, `ewl-heading--xl`, `ewl-heading--line`,
		`data-ew-widget="ewl.tabs"`, `<h3 class="ewl-tab-name">First</h3>`, `<h3 class="ewl-tab-name">Second</h3>`,
		`<details class="ewl-acc" name="ewl-acc-`, ` open=""><summary>Question</summary>`, `<summary>Other</summary>`,
		`href="/c/1">Go</a>`, `href="#">Bad</a>`, `ewl-divider--dotted`, `aria-hidden="true"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in\n%s", want, html)
		}
	}
	if strings.Contains(html, "javascript:") {
		t.Fatal("unsafe link rendered")
	}
	// options the server does not accept: not used, and the block says so
	html = render(`:::section{options="bg_color:#ffffff text_color:#bbbbbb"}
Text
:::

:::section{options="css:body%7B%7D"}
Text
:::

:::section{options="not:options"}
Text
:::`)
	if strings.Contains(html, "data-ewbo") {
		t.Fatalf("refused options were used: %s", html)
	}
	for _, want := range []string{"has a contrast of", "unknown option"} {
		if !strings.Contains(html, want) {
			t.Errorf("no note %q: %s", want, html)
		}
	}
	if n := strings.Count(html, "unknown option"); n != 2 {
		t.Errorf("%d notes of unknown options, want 2 (css, not): %s", n, html)
	}
	// icons: only the name in the HTML (the browser shows its file), decoration
	// unless labelled; the whole icon box is one link
	html = render(`::iconbox{name=home title="Start" link="/c/1"}

::iconbox{name=home title="Evil" link="javascript:alert(1)"}

Text :icon[Phone]{name=phone} and :icon{name=star look=filled} and :icon{name=arrow-right decorative=true}

:::iconlist{name=check}
- one
- two
:::

::icon{name="bad name"}

::gallery{category=1 layout=masonry tablet=2 mobile=1}
`)
	for _, want := range []string{
		// without a label screen readers read the name of the icon
		`<span class="ew-icon ew-icon--m" data-icon="home" data-icon-style="outline" role="img" aria-label="home"></span>`,
		`<a class="ewl-boxlink" href="/c/1">Start</a>`,
		`data-icon="phone" data-icon-style="outline" role="img" aria-label="Phone"`,
		`data-icon="star" data-icon-style="filled" role="img" aria-label="star"`,
		`data-icon="arrow-right" data-icon-style="outline" aria-hidden="true"`,
		`<div class="ewl-iconlist ewl-iconlist--m" data-icon="check" data-icon-style="outline"><ul>`,
		"choose an icon",
		`<a class="ewl-boxlink" href="#">Evil</a>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("icons: missing %q in\n%s", want, html)
		}
	}
	if _, built := web.HasIcon("outline", "home"); built {
		// with the icons in the program: a name that does not exist is refused
		if h := render("::icon{name=no-such-icon-xyz}"); !strings.Contains(h, `there is no icon \"no-such-icon-xyz\"`) && !strings.Contains(h, "there is no icon &#34;no-such-icon-xyz&#34;") {
			t.Errorf("unknown icon: %s", h)
		}
	}
	// room: more columns on the tablet than have room
	html = render(":::grid{desktop=5 tablet=5}\nA\n:::")
	if !strings.Contains(html, "at most 3 columns have room") || strings.Contains(html, "ewl-grid--d5") {
		t.Fatalf("grid without room: %s", html)
	}
}
