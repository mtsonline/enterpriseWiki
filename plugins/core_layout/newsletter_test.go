package core_layout

import (
	"strings"
	"testing"
)

func TestNewsletter(t *testing.T) {
	allowed := newsletter{allow: func() []string { return []string{"https://news.example.com"} }}
	p := map[string]string{"address": "https://news.example.com/subscribe?list=7#x", "title": "Our <news>", "hidden": "list_id=abc-7; u=12:34", "name_field": "FNAME",
		"privacy": "https://news.example.com/privacy", "button": "Join"}
	out, err := render(t, allowed, "de", p, "<p>Monthly.</p>")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<form class="ewl-newsletter-form" method="post" action="https://news.example.com/subscribe?list=7" accept-charset="utf-8">`,
		`<h2 class="ewl-newsletter-title" id="ewl-nl-1-t">Our &lt;news&gt;</h2>`, `<input type="hidden" name="list_id" value="abc-7">`, `<input type="hidden" name="u" value="12:34">`,
		`name="FNAME" type="text" autocomplete="name"`, `<label for="ewl-nl-1-e">E-Mail-Adresse</label><input class="ew-input" id="ewl-nl-1-e" name="email" type="email" autocomplete="email" required`,
		`an den Anbieter dieses Newsletters (news.example.com).`, `<a href="https://news.example.com/privacy" rel="noopener noreferrer">`, `>Join</button>`, `<div class="ewl-newsletter-text"><p>Monthly.</p></div>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	// not allowed, not https, hostile names and values: refused
	if _, err := render(t, newsletter{}, "en", map[string]string{"address": "https://news.example.com/s"}, ""); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("not allowed: %v", err)
	}
	for k, v := range map[string]string{
		"address": "http://news.example.com/s", "field": `email" onfocus="x`, "name_field": "a b", "hidden": `a=b"><script>`, "privacy": "javascript:alert(1)",
	} {
		q := map[string]string{"address": "https://news.example.com/s"}
		q[k] = v
		if _, err := render(t, allowed, "en", q, ""); err == nil {
			t.Errorf("%s=%q accepted", k, v)
		}
	}
	for _, a := range []string{"https://evil.example/s", "https://news.example.com.evil.example/s", "https://user:pw@news.example.com/s", "https://NEWS.example.com:444/s"} {
		if _, err := render(t, allowed, "en", map[string]string{"address": a}, ""); err == nil {
			t.Errorf("address %q accepted", a)
		}
	}
	out, _ = render(t, allowed, "en", map[string]string{"address": "https://NEWS.example.com/s"}, "")
	if !strings.Contains(out, `aria-label="Newsletter"`) || !strings.Contains(out, ">Subscribe</button>") {
		t.Errorf("defaults: %s", out)
	}
}
