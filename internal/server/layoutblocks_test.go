package server_test

import (
	"strings"
	"testing"
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
	html := render(`:::grid{desktop=3 tablet=2 options="{\"bg_color\":\"#102040\",\"text_color\":\"#ffffff\",\"padding_top\":4}"}
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

:::accordion{one=true first=true}
### Question
Answer
***
### Other
More
:::

::buttons{text1="Go" link1="/c/1" text2="Bad" link2="javascript:alert(1)"}

::divider{style=dotted}
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
	html = render(`:::section{options="{\"bg_color\":\"#ffffff\",\"text_color\":\"#bbbbbb\"}"}
Text
:::

:::section{options="{\"css\":\"body{}\"}"}
Text
:::

:::section{options="not json"}
Text
:::`)
	if strings.Contains(html, "data-ewbo") {
		t.Fatalf("refused options were used: %s", html)
	}
	for _, want := range []string{"has a contrast of", "unknown option", "not a valid list of options"} {
		if !strings.Contains(html, want) {
			t.Errorf("no note %q: %s", want, html)
		}
	}
	// room: more columns on the tablet than have room
	html = render(":::grid{desktop=5 tablet=5}\nA\n:::")
	if !strings.Contains(html, "at most 3 columns have room") || strings.Contains(html, "ewl-grid--d5") {
		t.Fatalf("grid without room: %s", html)
	}
}
