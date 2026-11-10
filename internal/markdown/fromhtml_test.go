package markdown

import (
	"strings"
	"testing"
)

func TestFromHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{`<h2>Title</h2><p>Hello <strong>bold</strong> and <em>it</em>, <a href="/x" title="T">link</a>.</p>`, "## Title\n\nHello **bold** and *it*, [link](/x \"T\").\n"},
		{`<ul><li>one</li><li>two<ul><li>nested</li></ul></li></ul>`, "- one\n- two\n  - nested\n"},
		{`<ol start="3"><li>a</li><li>b</li></ol>`, "3. a\n4. b\n"},
		{`<blockquote><p>q1</p><p>q2</p></blockquote>`, "> q1\n>\n> q2\n"},
		{`<pre><code class="language-go">x := "` + "`" + `"</code></pre>`, "```go\nx := \"`\"\n```\n"},
		{`<table><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td>1</td><td>x|y</td></tr></tbody></table>`, "| A | B |\n| --- | --- |\n| 1 | x\\|y |\n"},
		{`<table><tr><td colspan="2">merged</td></tr></table>`, `<table><tbody><tr><td colspan="2">merged</td></tr></tbody></table>`},
		{`<div data-macro="toc" data-params='{"levels":"3"}'></div>`, "::toc{levels=3}\n"},
		{`<div data-macro="columns"><div data-macro="panel" data-params='{"type":"info"}'><p>in</p></div></div>`, "::::columns\n:::panel{type=info}\nin\n:::\n::::\n"},
		{`<p>Status <span data-macro="status" data-params='{"color":"green"}'>DONE</span></p>`, "Status :status[DONE]{color=green}\n"},
		{`<p>5 * 3 = 15 [x] #tag :note</p>`, "5 \\* 3 = 15 \\[x\\] #tag \\:note\n"},
		{`<p># not a heading</p><p>- not a list</p>`, "\\# not a heading\n\n\\- not a list\n"},
		{`<p>line<br>break <u>under</u></p>`, "line\\\nbreak <u>under</u>\n"},
		{`<ul><li data-checked="true">done</li><li data-checked="false">open</li></ul>`, "- [x] done\n- [ ] open\n"},
		{`<ul><li><input checked="" disabled="" type="checkbox"> done</li><li><input disabled="" type="checkbox"> open</li></ul>`, "- [x] done\n- [ ] open\n"},
		{`<figure><img src="/a.png" alt="A"><figcaption>Cap</figcaption></figure>`, `<figure><img src="/a.png" alt="A"/><figcaption>Cap</figcaption></figure>`},
	}
	for _, c := range cases {
		got := FromHTML(c.in)
		if !strings.Contains(got, c.want) {
			t.Errorf("%s\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

// Converting and rendering again keeps the content.
func TestRoundTrip(t *testing.T) {
	in := `<h2>Hi</h2><p>A <strong>b</strong> <code>c</code> <a href="https://x.org">d</a> 10:30 *star*</p><ul><li>x</li></ul><div data-macro="panel" data-params='{"title":"T \"q\""}'><p>body</p><div data-macro="toc"></div></div><p><span data-macro="status" data-params='{"color":"red"}'>NO</span></p>`
	md := FromHTML(in)
	out, err := ToHTML(md)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<h2 id="hi">Hi</h2>`, `<strong>b</strong>`, `<code>c</code>`, `<a href="https://x.org">d</a>`, `10:30 *star*`, `<li>x</li>`,
		`data-macro="panel" data-params="{&#34;title&#34;:&#34;T \&#34;q\&#34;&#34;}"`, `<div data-macro="toc"></div>`, `<span data-macro="status" data-params="{&#34;color&#34;:&#34;red&#34;}">NO</span>`} {
		if !strings.Contains(out, want) {
			t.Errorf("round trip lost %q\nmd: %s\nhtml: %s", want, md, out)
		}
	}
}
