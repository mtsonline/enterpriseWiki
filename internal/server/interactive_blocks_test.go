package server_test

import (
	"strings"
	"testing"
)

// TestInteractiveBlocksOnPages: progress bar, counter, timeline, dialog and tooltip through
// the whole way of a page (markdown, macros, cleaning): what screen readers
// need stays, markup in parameters is text, wrong values say why.
func TestInteractiveBlocksOnPages(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var cat struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Blocks B"}}, &cat, 201)
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
	html := render(`::progress{label="Build <b>x</b>" value=40}

::counter{value=1234 label="Members" suffix="+"}

:::timeline{label="History" direction=horizontal}
### 2024
Founded
***
### 2025
Grown
:::

:::dialog{button="Hours" title="Open"}
Monday <script>alert(1)</script>
:::

Our :tooltip[API]{tip="a \"programming\" <i>interface</i>"} works.
`)
	for _, want := range []string{
		`role="progressbar" aria-label="Build &lt;b&gt;x&lt;/b&gt;"`, `aria-valuenow="40"`, `style="width: 40.00%"`,
		`data-ew-widget="ewl.counter" data-to="1234"`, `>1,234<`,
		`role="region" aria-label="History" tabindex="0"`, `<p class="ewl-timeline-when">2025</p>`,
		`<details class="ewl-dialog" data-ew-widget="ewl.dialog">`, `<p class="ewl-dialog-title">Open</p>`,
		`data-ew-widget="ewl.tip"`, `&lt;i&gt;interface&lt;/i&gt;`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in\n%s", want, html)
		}
	}
	if strings.Contains(html, "<script") || strings.Contains(html, "<b>x") {
		t.Fatalf("markup got through: %s", html)
	}
	// wrong values: the block says why instead of showing something wrong
	for src, want := range map[string]string{
		`::progress{label="x" value=140}`:                  "between 0 and the maximum",
		`::counter{value=abc label="x"}`:                   "value",
		"::: timeline{direction=horizontal}\n## A\nx\n:::": "needs a name",
		":::dialog{button=\"x\"}\ny\n:::":                  "needs a button text and a title",
	} {
		if got := render(src); !strings.Contains(got, want) {
			t.Errorf("%s: %s", src, got)
		}
	}
}
