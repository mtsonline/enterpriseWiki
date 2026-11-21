package core_layout

import (
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// TestEmbedTarget: watch pages become player addresses, everything else is
// used as it is; only https without user names.
func TestEmbedTarget(t *testing.T) {
	for in, want := range map[string][3]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ": {"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ", "https://www.youtube-nocookie.com", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		"https://youtu.be/dQw4w9WgXcQ?t=42":           {"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?start=42", "https://www.youtube-nocookie.com", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s"},
		"https://www.youtube.com/shorts/dQw4w9WgXcQ":  {"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ", "https://www.youtube-nocookie.com", ""},
		"https://vimeo.com/76979871":                  {"https://player.vimeo.com/video/76979871?dnt=1", "https://player.vimeo.com", "https://vimeo.com/76979871"},
		"https://player.vimeo.com/video/76979871":     {"https://player.vimeo.com/video/76979871?dnt=1", "https://player.vimeo.com", ""},
		"https://maps.example.com/embed?q=a%20b#frag": {"https://maps.example.com/embed?q=a%20b", "https://maps.example.com", ""},
		"https://Video.Example.com:8443/v/1":          {"https://Video.Example.com:8443/v/1", "https://video.example.com:8443", ""},
		"https://www.youtube.com/watch?v=short":       {"https://www.youtube.com/watch?v=short", "https://www.youtube.com", ""},
	} {
		got, err := EmbedTarget(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got.src != want[0] || got.origin != want[1] || (want[2] != "" && got.link != want[2]) {
			t.Errorf("%s: got %+v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"", "http://www.youtube.com/watch?v=dQw4w9WgXcQ", "javascript:alert(1)", "https://user:pw@video.example.com/x",
		"//video.example.com/x", "https:video.example.com", "data:text/html,x", "https://", strings.Repeat("https://a.example.com/", 200)} {
		if _, err := EmbedTarget(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// TestEmbed: only allowed services; nothing from the service in the HTML
// but the address in a data attribute (no frame before the click); texts
// escaped; the box names the service.
func TestEmbed(t *testing.T) {
	m := embed{allow: func() []string { return []string{"https://www.youtube-nocookie.com"} }}
	out, err := render(t, m, "de", map[string]string{"address": "https://youtu.be/dQw4w9WgXcQ", "title": `Einführung <b>"neu"</b>`, "ratio": "4x3", "caption": "Teil 1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="ewl-embed ewl-embed--4x3"`, `data-ew-widget="ewl.embed"`, `data-src="https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"`,
		`data-host="www.youtube-nocookie.com"`, `Einführung &lt;b&gt;&#34;neu&#34;&lt;/b&gt;`, "Ihr Browser verbindet sich erst mit www.youtube-nocookie.com",
		`href="https://www.youtube.com/watch?v=dQw4w9WgXcQ" rel="noopener noreferrer" target="_blank"`, `<figcaption>Teil 1</figcaption>`} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing in %s", want, out)
		}
	}
	for _, bad := range []string{"<iframe", "<b>", "<script"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q in %s", bad, out)
		}
	}
	if _, err := render(t, m, "en", map[string]string{"address": "https://vimeo.com/76979871", "title": "x"}, ""); err == nil || !strings.Contains(err.Error(), "https://player.vimeo.com") {
		t.Errorf("service not allowed: %v", err)
	}
	if _, err := render(t, embed{}, "en", map[string]string{"address": "https://youtu.be/dQw4w9WgXcQ", "title": "x"}, ""); err == nil {
		t.Error("nothing allowed, but accepted")
	}
	if _, err := render(t, m, "en", map[string]string{"address": "https://youtu.be/dQw4w9WgXcQ"}, ""); err == nil {
		t.Error("no title accepted")
	}
	// the attribute value of a plain address stays inside its quotes
	m2 := embed{allow: func() []string { return []string{"https://v.example.com"} }}
	out, err = render(t, m2, "en", map[string]string{"address": `https://v.example.com/a"onmouseover="x`, "title": "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"onmouseover`) || strings.Contains(out, `" onmouseover`) {
		t.Errorf("quote broke out: %s", out)
	}
}

// TestEmbedOrigin: what media.embed_allow accepts (it goes into the CSP).
func TestEmbedOrigin(t *testing.T) {
	for o, want := range map[string]bool{
		"https://www.youtube-nocookie.com": true, "https://player.vimeo.com": true, "https://video.example.com:8443": true,
		"http://video.example.com": false, "https://*.example.com": false, "https://video.example.com/": false, "https://Video.example.com": false,
		"https://localhost": false, "https://video.example.com; script-src *": false, "https://video.example.com:0": false, "https://[::1]": false,
		"https://-a.example.com": false, "https://a..example.com": false, "https://video.example.com:": false, "": false, "https://": false,
		"https://video.example.com 'unsafe-inline'": false,
	} {
		if got := config.EmbedOrigin(o); got != want {
			t.Errorf("EmbedOrigin(%q) = %v, want %v", o, got, want)
		}
	}
}
