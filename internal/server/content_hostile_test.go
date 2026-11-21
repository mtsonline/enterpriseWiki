package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

// hostilePayloads try to break out of every place a parameter can end up
// in: element content, attribute values (quoted and unquoted), URLs, inline
// CSS, style elements, JSON in data attributes and scripts. "ewxss" marks
// an injected element or attribute: if the browser would see it, the value
// got out of its place.
var hostilePayloads = []string{
	`"><ewxss ewxss=1>`,
	`'><ewxss ewxss=1>`,
	`x" ewxss="1`,
	`x' ewxss='1`,
	`1 ewxss=1`,
	`<ewxss>`,
	`<script>alert(1)</script>`,
	`<img src=x onerror=alert(1)>`,
	`javascript:alert(1)`,
	"\tJaVa\nScRiPt:alert(1)",
	`vbscript:msgbox(1)`,
	`data:text/html,<ewxss>`,
	`//evil.example/x`,
	`red;background:url(https://evil.example/t)`,
	`#fff;}</style><ewxss>`,
	`expression(alert(1))`,
	`</script><ewxss>`,
	`"}],"ewxss":"<ewxss>`,
	"  <ewxss>",
	`&lt;ewxss&gt;`,
	`%3Cewxss%3E`,
	`{{constructor.constructor('alert(1)')()}}`,
	// valid beginnings with a tail (a check without an end anchor)
	`#0b3a6b;background:url(https://evil.example/t)`,
	`#0b3a6b"><ewxss>`,
	`/api/v1/files/a');background:url(https://evil.example/t);x:('`,
	`https://example.com/a"><ewxss>`,
	`3;background:url(https://evil.example/t)`,
	`intro"><ewxss>`,
}

// dangerousSamplesHTML: markup a foreign server could send.
const dangerousSamplesHTML = `<script>alert(1)</script><img src=x onerror=alert(1)><svg onload=alert(1)><a href="javascript:alert(1)">x</a>` +
	`<iframe src="https://evil.example/"></iframe><form action="https://evil.example/"><input name=p></form><style>body{background:url(https://evil.example/t)}</style>` +
	`<div style="background:url(https://evil.example/t)">x</div><base href="https://evil.example/"><meta http-equiv="refresh" content="0;url=https://evil.example/">` +
	`<math><mtext><table><mglyph><style><!--</style><img title="--&gt;&lt;img src=1 onerror=alert(1)&gt;">` + "\n"

// dangerousMarkup parses rendered content the way a browser does and lists
// what could run script, inject markup or load foreign styles. Unlike the
// pentest it knows the payloads: it flags any trace of "ewxss" or
// "evil.example" in a place where it would act.
func dangerousMarkup(doc string) []string {
	var out []string
	urlAttr := map[string]bool{"href": true, "src": true, "action": true, "formaction": true, "xlink:href": true, "data": true, "poster": true,
		"background": true, "srcset": true, "ping": true, "cite": true, "longdesc": true, "manifest": true, "codebase": true}
	z := html.NewTokenizer(strings.NewReader(doc))
	inStyle, inScript := false, false
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return out
		case html.TextToken:
			txt := strings.ToLower(string(z.Text()))
			if inStyle && (strings.Contains(txt, "evil.example") || strings.Contains(txt, "ewxss") || strings.Contains(txt, "expression(")) {
				out = append(out, "<style> with "+cut(txt, 60))
			}
			if inScript {
				out = append(out, "<script> content "+cut(txt, 60))
			}
			continue
		case html.EndTagToken:
			n, _ := z.TagName()
			switch string(n) {
			case "style":
				inStyle = false
			case "script":
				inScript = false
			}
			continue
		case html.StartTagToken, html.SelfClosingTagToken:
		default:
			continue
		}
		t := z.Token()
		switch t.Data {
		case "ewxss", "script", "base", "meta", "link", "object", "embed", "applet", "frame", "frameset", "portal":
			out = append(out, "<"+t.Data+">")
		case "style":
			inStyle = tt == html.StartTagToken
		}
		if t.Data == "script" {
			inScript = tt == html.StartTagToken
		}
		for _, a := range t.Attr {
			k := strings.ToLower(a.Key)
			v := strings.ToLower(strings.Map(func(r rune) rune {
				if r <= ' ' {
					return -1
				}
				return r
			}, a.Val))
			switch {
			case k == "ewxss" || strings.HasPrefix(k, "on") || k == "srcdoc" || k == "formaction" || strings.Contains(k, "ewxss"):
				out = append(out, fmt.Sprintf("<%s %s=%q>", t.Data, k, cut(a.Val, 40)))
			case urlAttr[k] && (strings.HasPrefix(v, "javascript:") || strings.HasPrefix(v, "vbscript:") || strings.HasPrefix(v, "data:text") || strings.HasPrefix(v, "data:image/svg")):
				out = append(out, fmt.Sprintf("<%s %s=%q>", t.Data, k, cut(a.Val, 40)))
			case urlAttr[k] && t.Data != "a" && t.Data != "img" && strings.Contains(v, "evil.example"):
				// links and pictures may point anywhere; frames, forms and
				// media only where the wiki allows (embeds come from the
				// script, after consent)
				out = append(out, fmt.Sprintf("<%s %s=%q>", t.Data, k, cut(a.Val, 40)))
			case k == "data-ewbo" && !safeOptions(a.Val):
				// the options become CSS in the browser: only the
				// characters of colors, numbers, files and times
				out = append(out, fmt.Sprintf("<%s data-ewbo=%q>", t.Data, cut(a.Val, 80)))
			case k == "style" && (strings.Contains(v, "evil.example") || strings.Contains(v, "expression(") || strings.Contains(v, "javascript:") ||
				strings.Contains(v, "@import") || strings.Contains(v, "ewxss") || strings.Contains(v, "behavior:") || strings.Contains(v, "-moz-binding")):
				out = append(out, fmt.Sprintf("<%s style=%q>", t.Data, cut(a.Val, 60)))
			}
		}
	}
}

var optionChars = regexp.MustCompile(`^[A-Za-z0-9#._:/+-]*$`)

// safeOptions: every text in the options of a block (data-ewbo) consists
// of harmless characters only.
func safeOptions(raw string) bool {
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return false
	}
	var walk func(any) bool
	walk = func(x any) bool {
		switch y := x.(type) {
		case string:
			return optionChars.MatchString(y)
		case []any:
			for _, z := range y {
				if !walk(z) {
					return false
				}
			}
		case map[string]any:
			for k, z := range y {
				if !optionChars.MatchString(k) || !walk(z) {
					return false
				}
			}
		}
		return true
	}
	return walk(v)
}

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// TestDangerousMarkup: the check itself finds what it must.
func TestDangerousMarkup(t *testing.T) {
	for _, s := range []string{`<p><ewxss>x</ewxss></p>`, `<p ewxss="1">`, `<a href=" java	script:x">`, `<div style="background:url(https://evil.example/t)">`,
		`<img src=x onerror=alert(1)>`, `<script>x</script>`, `<div data-ewbo="{&#34;bg_color&#34;:&#34;#0b3a6b;background:url(x)&#34;}">`, `<style>a{background:url(//evil.example)}</style>`, `<iframe src="https://evil.example/">`, `<form action="//evil.example">`} {
		if len(dangerousMarkup(s)) == 0 {
			t.Errorf("not found: %s", s)
		}
	}
	for _, s := range []string{`<p>&lt;ewxss&gt; &#34;&gt;&lt;ewxss ewxss=1&gt;</p>`, `<a href="https://evil.example/">x</a>`, `<p title="&quot;&gt;&lt;ewxss&gt;">x</p>`,
		`<div data-params="{&#34;a&#34;:&#34;&lt;ewxss&gt;&#34;}"></div>`,
		`<div class="ew-bo" data-ewbo="{&#34;bg_image&#34;:&#34;/api/v1/media/12/view&#34;,&#34;from&#34;:&#34;2026-12-01T08:00:00+01:00&#34;,&#34;padding_top&#34;:{&#34;desktop&#34;:4}}">`} {
		if d := dangerousMarkup(s); len(d) > 0 {
			t.Errorf("harmless markup flagged: %s → %v", s, d)
		}
	}
}

// TestContentHostileParams: every parameter of every block and every block
// option, set to values that try to break out, renders to markup without
// anything that could run, inject or load from elsewhere. A refused value
// (the page cannot be saved) is fine as well.
func TestContentHostileParams(t *testing.T) {
	if os.Getenv("EWIKI_TEST_DATABASE_URL") == "" {
		t.Skip("needs EWIKI_TEST_DATABASE_URL")
	}
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var cat struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Hostile"}}, &cat, 201)
	// a server for the HTTP block that answers with hostile content in
	// every format
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		switch r.URL.Query().Get("f") {
		case "rss":
			w.Header().Set("Content-Type", "application/rss+xml")
			b.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel><title>T</title>`)
			for i, p := range hostilePayloads {
				x := stdhtml.EscapeString(p)
				fmt.Fprintf(&b, `<item><title>Feed item %d %s</title><link>%s</link><guid>g%d</guid><description>%s %s</description>`+
					`<enclosure url="%s" type="image/png"/><pubDate>Sat, 21 Nov 2026 10:%02d:00 +0000</pubDate></item>`,
					i, x, x, i, stdhtml.EscapeString(dangerousSamplesHTML), x, x, i%60)
			}
			b.WriteString(`<item><title>Feed item js</title><link>javascript:alert(1)</link><description><![CDATA[` + dangerousSamplesHTML + `]]></description>` +
				`<enclosure url="javascript:alert(1)" type="image/png"/></item></channel></rss>`)
		case "json":
			m := map[string]any{}
			for i, p := range hostilePayloads {
				m[p] = p
				m[fmt.Sprint("k", i)] = []any{p, map[string]any{p: p}}
			}
			_ = json.NewEncoder(&b).Encode([]any{m, m})
		default:
			b.WriteString(dangerousSamplesHTML)
			for _, p := range hostilePayloads {
				b.WriteString(p + "\n")
			}
		}
		_, _ = w.Write([]byte(b.String()))
	}))
	defer upstream.Close()
	if err := e.app.Config.Update(func(c *config.Config) error {
		c.Plugins.Macros.HTTPAllow = []string{upstream.URL + "/"}
		c.Plugins.Macros.HTTPAllowPrivate = true
		c.Media.EmbedAllow = []string{"https://example.com"}
		c.Feeds.AllowPrivate = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var macros []struct {
		Name    string `json:"name"`
		HasBody bool   `json:"has_body"`
		Inline  bool   `json:"inline"`
		Params  []struct {
			Name    string   `json:"name"`
			Type    string   `json:"type"`
			Options []string `json:"options"`
		} `json:"params"`
	}
	admin.must("GET", "/api/v1/macros", nil, &macros, 200)
	sample := map[string]string{"text": "Text", "number": "3", "url": "https://example.com/a", "bool": "true", "color": "#0b3a6b", "date": "2026-11-21",
		"textarea": "Text", "icon": "star", "custom:icon": "star", "json": `{"a":1}`}
	saved, refused := 0, 0
	render := func(src string) (string, bool) {
		t.Helper()
		var el struct {
			ID   int64  `json:"id"`
			HTML string `json:"html"`
		}
		if st := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "H",
			"body": map[string]any{"format": "markdown", "source": src}}, &el); st != 201 {
			refused++
			return "", false
		}
		saved++
		admin.must("GET", "/api/v1/elements/"+itoa(el.ID), nil, &el, 200)
		return el.HTML, true
	}
	var bad []string
	check := func(what, src string) {
		html, ok := render(src)
		if !ok {
			return
		}
		if d := dangerousMarkup(html); len(d) > 0 {
			bad = append(bad, fmt.Sprintf("%s: %v\nsource: %q", what, d, src))
		}
	}
	// first every block with valid values only: it must render, or the
	// hostile values below would only test the error message
	valid := func(m int, p string) string {
		q := macros[m].Params
		for _, x := range q {
			if x.Name != p {
				continue
			}
			if v, ok := hostileSamples[macros[m].Name+"."+p]; ok {
				return v
			}
			if len(x.Options) > 0 {
				return x.Options[0]
			}
			if v, ok := sample[x.Type]; ok {
				return v
			}
			return "1"
		}
		return ""
	}
	hostileSamples["http.address"] = upstream.URL + "/x"
	for mi, m := range macros {
		if m.Name == "shell" {
			continue // runs only approved commands; its output: TestShellMacro
		}
		var attrs []string
		for _, q := range m.Params {
			if v := valid(mi, q.Name); v != "" && q.Name != "options" {
				attrs = append(attrs, q.Name+"="+quoteAttr(v))
			}
		}
		src := wrapMacroBody(m.Name, m.HasBody, m.Inline, strings.Join(attrs, " "), bodyFor(m.Name, "Text"))
		if html, ok := render(src); !ok || strings.Contains(html, "ew-macro-error") {
			t.Errorf("block %s does not render with valid values (hostileSamples): %q\n%s", m.Name, src, html)
		}
	}
	// a body that tries the same (the sanitizer must hold inside blocks)
	hostileBody := "<img src=x onerror=alert(1)> <ewxss>x</ewxss> [x](javascript:alert(1))"
	rendered := 0
	// the HTTP block in every format with hostile answers
	for _, f := range []string{"text", "html", "json"} {
		check("http "+f, fmt.Sprintf("::http{address=%s cache=0 format=%s}", quoteAttr(upstream.URL+"/x?f="+f), f))
		if html, _ := render(fmt.Sprintf("::http{address=%s cache=0 format=%s}", quoteAttr(upstream.URL+"/x?f="+f), f)); strings.Contains(html, "ew-macro-error") || !strings.Contains(html, "ewxss") {
			t.Errorf("http %s: the answer was not shown: %s", f, cut(html, 300))
		}
	}
	// news from a hostile feed, in every layout
	feed := upstream.URL + "/x?f=rss"
	// the first use registers the feed; then fetch it now (not in the background)
	render(fmt.Sprintf("::news{source=%s}", quoteAttr("feed:"+feed)))
	if err := e.app.Feeds.Refresh(context.Background(), "rss", feed); err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{"cards", "list", "headlines", "tiles"} {
		src := fmt.Sprintf("::news{source=%s layout=%s limit=50}", quoteAttr("feed:"+feed), l)
		check("news "+l, src)
		if html, _ := render(src); !strings.Contains(html, "Feed item 3") {
			t.Errorf("news %s: the feed was not shown: %s", l, cut(html, 300))
		}
	}
	for mi, m := range macros {
		if m.Name == "shell" {
			continue
		}
		// one page per payload with one instance per parameter: the
		// parameter is hostile, the others hold a valid sample
		for _, pay := range hostilePayloads {
			var parts []string
			for i := range m.Params {
				var attrs []string
				for j, q := range m.Params {
					v := valid(mi, q.Name)
					if q.Name == "options" {
						v = ""
					}
					if i == j {
						v = pay
					}
					if v != "" {
						attrs = append(attrs, q.Name+"="+quoteAttr(v))
					}
				}
				parts = append(parts, wrapMacroBody(m.Name, m.HasBody, m.Inline, strings.Join(attrs, " "), bodyFor(m.Name, hostileBody)))
			}
			if len(parts) == 0 {
				parts = append(parts, wrapMacroBody(m.Name, m.HasBody, m.Inline, "", bodyFor(m.Name, hostileBody)))
			}
			src := strings.Join(parts, "\n\n")
			html, ok := render(src)
			rendered++
			// refused as a whole (one value is refused when saving): each
			// instance on its own, so the others are checked as well
			if d := dangerousMarkup(html); !ok || len(d) > 0 {
				// find the parameter for the message
				for _, part := range parts {
					check(m.Name+" with "+fmt.Sprintf("%q", pay), part)
				}
			}
		}
	}
	// every block option with every payload, written by hand (not through
	// the encoder, which an attacker does not have to use)
	var schema struct {
		Groups []struct {
			Options []struct {
				ID string `json:"id"`
			} `json:"options"`
		} `json:"groups"`
	}
	if err := jsonUnmarshal(content.BlockOptionsSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, g := range schema.Groups {
		for _, o := range g.Options {
			ids = append(ids, o.ID)
		}
	}
	sort.Strings(ids)
	// first every option with a valid value: it must take effect, or the
	// hostile values below would only test the error message
	for _, id := range ids {
		v, ok := optionSamples[id]
		if !ok {
			t.Errorf("option %s has no sample in optionSamples", id)
			continue
		}
		o := content.EncodeBlockOptions(map[string]any{id: v})
		src := wrapMacroBody("section", true, false, "options="+quoteAttr(optionContext(id)+o), "x")
		if html, ok := render(src); !ok || strings.Contains(html, "ew-macro-error") || !strings.Contains(html, "data-ewbo") {
			t.Errorf("option %s does not take effect with a valid value: %q\n%s", id, src, cut(html, 300))
		}
	}
	for _, pay := range hostilePayloads {
		// percent-encoded as the codec writes it (everything outside
		// A-Z a-z 0-9 . _ ~ / + # : -)
		var eb strings.Builder
		for _, c := range []byte(pay) {
			if c < 0x80 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("._~/+#:-", c) >= 0) {
				eb.WriteByte(c)
			} else {
				fmt.Fprintf(&eb, "%%%02X", c)
			}
		}
		enc := eb.String()
		var parts []string
		for _, id := range ids {
			for _, key := range []string{id, id + ".mobile"} {
				// raw (as typed), and percent-encoded (as the codec writes)
				ctx := optionContext(id)
				parts = append(parts, wrapMacroBody("section", true, false, "options="+quoteAttr(ctx+key+":"+pay), "x"),
					wrapMacroBody("section", true, false, "options="+quoteAttr(ctx+key+":"+enc), "x"))
			}
		}
		for i := 0; i < len(parts); i += 40 {
			j := min(i+40, len(parts))
			src := strings.Join(parts[i:j], "\n\n")
			html, ok := render(src)
			rendered++
			if !ok {
				for _, part := range parts[i:j] {
					check("option with "+fmt.Sprintf("%q", pay), part)
				}
				continue
			}
			if d := dangerousMarkup(html); len(d) > 0 {
				for _, part := range parts[i:j] {
					check("option with "+fmt.Sprintf("%q", pay), part)
				}
			}
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if saved < refused*4 {
		t.Errorf("too few pages saved (%d of %d): the check would test little", saved, saved+refused)
	}
	t.Logf("%d macros, %d options, %d pages: %d saved, %d refused", len(macros), len(ids), rendered, saved, refused)
}

// optionContext: valid options that the rules of an option need next to
// it (a background needs a text color, a picture an overlay, a gradient
// its colors), so the option itself is used.
func optionContext(id string) string {
	if id == "gradient_text" {
		return "bg_color:#000000 text_color:#ffffff bg_gradient_from:#ffffff bg_gradient_to:#eeeeee "
	}
	ctx := map[string]string{"text_color": "#ffffff", "bg_color": "#0b3a6b", "overlay_color": "#000000", "overlay_opacity": "60",
		"bg_gradient": "linear", "bg_gradient_from": "#0b3a6b", "bg_gradient_to": "#102040"}
	var b strings.Builder
	for _, k := range []string{"text_color", "bg_color", "overlay_color", "overlay_opacity", "bg_gradient", "bg_gradient_from", "bg_gradient_to"} {
		if k != id {
			b.WriteString(k + ":" + ctx[k] + " ")
		}
	}
	return b.String()
}

// hostileSamples: valid values of single parameters (block.param) where
// the sample of the type does not fit.
var hostileSamples = map[string]string{
	"anchor.name": "intro", "excerpt.name": "intro", "include.excerpt": "false", "include.name": "", "grid.widths": "",
	"personcard.image": "/api/v1/media/1/view", "testimonial.image": "/api/v1/media/1/view", "rotating.words": "eins|zwei|drei", "personcard.email": "anna@example.com", "personcard.phone": "+49 30 123456",
}

// hostileBodies: the content of blocks that need a certain body; %s is
// the hostile body.
var hostileBodies = map[string]string{
	"chart":  "| Name | Wert |\n|---|---|\n| a | 1 |\n| %s | 2 |",
	"logos":  "![%s](/api/v1/media/1/view)\n\n%s",
	"ticker": "- %s\n- b",
}

func bodyFor(macro, inner string) string {
	if f, ok := hostileBodies[macro]; ok {
		return strings.ReplaceAll(f, "%s", inner)
	}
	return inner
}

// wrapMacroBody is wrapMacro with a chosen body.
func wrapMacroBody(name string, body, inline bool, attrs, inner string) string {
	switch {
	case inline:
		return "Text :" + name + "[" + inner + "]{" + attrs + "} Text"
	case body:
		if _, ok := hostileBodies[name]; ok {
			return ":::" + name + "{" + attrs + "}\n" + inner + "\n:::"
		}
		return ":::" + name + "{" + attrs + "}\n" + inner + "\n\n---\n\n" + inner + "\n:::"
	}
	return "::" + name + "{" + attrs + "}"
}

// TestSearchSnippetMarks: text on a page cannot become markup in the
// snippets of the search (the marks of hits are replaced when indexing),
// and a long text cut for the index keeps whole characters.
func TestSearchSnippetMarks(t *testing.T) {
	if os.Getenv("EWIKI_TEST_DATABASE_URL") == "" {
		t.Skip("needs EWIKI_TEST_DATABASE_URL")
	}
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var cat struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Marks"}}, &cat, 201)
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Plan ⟦t⟧",
		"body": map[string]any{"format": "markdown", "source": "⟦fake⟧ quokka \x01also\x02 <ewxss> &lt;script&gt;"}}, nil, 201)
	var res struct {
		Elements []struct {
			Title   string `json:"title"`
			Snippet string `json:"snippet"`
		} `json:"elements"`
	}
	admin.must("GET", "/api/v1/search?q=quokka", nil, &res, 200)
	if len(res.Elements) != 1 {
		t.Fatalf("search: %+v", res)
	}
	sn := res.Elements[0].Snippet
	if !strings.Contains(sn, "<mark>quokka</mark>") || strings.Contains(sn, "<mark>fake") || strings.Contains(sn, "<mark>also") ||
		strings.Count(sn, "<mark>") != 1 || strings.Count(sn, "</mark>") != 1 || strings.ContainsAny(sn, "\x01\x02⟦⟧") || len(dangerousMarkup(sn)) > 0 {
		t.Fatalf("snippet %q", sn)
	}
	// a multi-byte character right at the limit of the index (1 MiB)
	long := strings.Repeat("a", 1<<20-1) + "ä wombat"
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Long",
		"body": map[string]any{"format": "markdown", "source": long}}, nil, 201)
}

// uaTransport sends a fixed browser name.
type uaTransport struct{ ua string }

func (u uaTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("User-Agent", u.ua)
	return http.DefaultTransport.RoundTrip(r)
}

// TestCutKeepsCharacters: values cut to their limit keep whole characters,
// so storing them cannot fail (a browser name with a multi-byte character
// at the limit must neither block the sign-in nor the audit log).
func TestCutKeepsCharacters(t *testing.T) {
	if os.Getenv("EWIKI_TEST_DATABASE_URL") == "" {
		t.Skip("needs EWIKI_TEST_DATABASE_URL")
	}
	e := setup(t)
	c := e.client()
	c.http.Transport = uaTransport{strings.Repeat("a", 299) + "ä" + strings.Repeat("b", 50)}
	c.login("admin", adminPW)
	var cat struct {
		ID int64 `json:"id"`
	}
	c.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "UA"}}, &cat, 201)
	// a file name with a multi-byte character at the limit
	var folder struct {
		ID int64 `json:"id"`
	}
	c.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Media"}, "hidden": true}, &folder, 201)
	name := strings.Repeat("n", 199) + "ö" + ".png"
	if st, _ := c.uploadMedium(t, "/api/v1/media", map[string]any{"category_id": folder.ID, "alt": "x"}, name, testPNG(4, 4)); st != 201 {
		t.Fatalf("upload with a long name: %d", st)
	}
}
