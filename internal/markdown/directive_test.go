package markdown

import (
	"strings"
	"testing"
)

func TestDirectives(t *testing.T) {
	cases := []struct{ in, want string }{
		{"::toc{levels=3}", `<div data-macro="toc" data-params="{&#34;levels&#34;:&#34;3&#34;}"></div>`},
		{":::panel{type=info title=\"A \\\"note\\\"\"}\nText **bold**\n:::", `<div data-macro="panel" data-params="{&#34;title&#34;:&#34;A \&#34;note\&#34;&#34;,&#34;type&#34;:&#34;info&#34;}"><p>Text <strong>bold</strong></p>`},
		{"::::columns\n:::panel\ninner\n:::\nafter\n::::", `<div data-macro="columns"><div data-macro="panel"><p>inner</p>
</div>
<p>after</p>
</div>`},
		{"Status :status[DONE]{color=green} ok", `<p>Status <span data-macro="status" data-params="{&#34;color&#34;:&#34;green&#34;}">DONE</span> ok</p>`},
		{"at 10:30 see https://x.org/a:b and :smile:", `<p>at 10:30 see <a href="https://x.org/a:b">https://x.org/a:b</a> and :smile:</p>`},
		{"::bad{x=\"unclosed}", `<p>::bad{x=&quot;unclosed}</p>`},
		{"```\n:::panel\n```", "<pre><code>:::panel\n</code></pre>"},
	}
	for _, c := range cases {
		got, err := ToHTML(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%q\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestAttrsRoundTrip(t *testing.T) {
	p := map[string]string{"a": "1", "title": `He said "hi" \o/`, "empty": "", "sp": "two words"}
	f := FormatAttrs(p)
	got, ok := ParseAttrs(f[1 : len(f)-1])
	if !ok || len(got) != len(p) {
		t.Fatalf("%s -> %v", f, got)
	}
	for k, v := range p {
		if got[k] != v {
			t.Errorf("%s: %q != %q", k, got[k], v)
		}
	}
}
