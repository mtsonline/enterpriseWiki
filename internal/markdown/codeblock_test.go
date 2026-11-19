package markdown

import "testing"

// Fenced code keeps its language, "lines" asks for line numbers, and the
// code is escaped.
func TestCodeBlocks(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"```go\nx := 1 < 2\n```\n", "<pre><code class=\"language-go\">x := 1 &lt; 2\n</code></pre>\n"},
		{"```go lines\na\nb\n```\n", "<pre data-lines=\"\"><code class=\"language-go\">a\nb\n</code></pre>\n"},
		{"```\n<b>\n```\n", "<pre><code>&lt;b&gt;\n</code></pre>\n"},
		{"```\"><script> lines\nx\n```\n", "<pre data-lines=\"\"><code class=\"language-&#34;&gt;&lt;script&gt;\">x\n</code></pre>\n"},
		{"    indented\n", "<pre><code>indented\n</code></pre>\n"},
	} {
		got, err := ToHTML(c.src)
		if err != nil || got != c.want {
			t.Errorf("%q:\n got %q\nwant %q (%v)", c.src, got, c.want, err)
		}
	}
}
