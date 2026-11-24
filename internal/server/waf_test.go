package server_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	coreruleset "github.com/corazawaf/coraza-coreruleset/v4"
	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/types"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

// Web application firewalls in front of the wiki (ModSecurity, CrowdSec
// AppSec, Coraza and others with the OWASP Core Rule Set) must not block
// the wiki's own syntax. These tests run the Core Rule Set (paranoia level
// 1, as the defaults of those firewalls) on requests the editor sends.
// Content of the people (code samples, SQL, HTML) can still trigger the
// rules; for that the documented exclusions exist (docker/waf), and they
// are tested here too.

// wafFor builds a Coraza WAF with the Core Rule Set in blocking mode and
// the given extra directives (between the setup and the rules, where the
// exclusions "before CRS" go). crs is "3" (3.3.8, as in many Linux
// distributions, testdata/crs3) or "4" (the current one).
func wafFor(t *testing.T, crs, extra string) coraza.WAF {
	t.Helper()
	base := `
SecRuleEngine On
SecRequestBodyAccess On
SecRequestBodyLimit 13107200
SecRequestBodyNoFilesLimit 1048576
SecRule REQUEST_HEADERS:Content-Type "@rx ^application/json" "id:200001,phase:1,t:none,t:lowercase,pass,nolog,ctl:requestBodyProcessor=JSON"
`
	var cfg coraza.WAFConfig
	switch crs {
	case "3":
		// Coraza cannot load some files of CRS 3: counters per IP address
		// (910, 912, 913, 949: no persistent collections) and regular
		// expressions with look-ahead (920, 942, 953: Go has RE2). The
		// XSS, command, file and PHP rules (930-941) load; a request counts
		// as blocked when one of them matches (CRS 3 blocks at 5 points,
		// one critical match). SQL is checked with CRS 4.
		files, err := filepath.Glob("testdata/crs3/rules/*.conf")
		if err != nil {
			t.Fatal(err)
		}
		rules := ""
		for _, f := range files {
			b := filepath.Base(f)
			skip := false
			for _, p := range []string{"REQUEST-910-", "REQUEST-912-", "REQUEST-913-", "REQUEST-920-", "REQUEST-942-", "REQUEST-949-", "RESPONSE-953-"} {
				skip = skip || strings.HasPrefix(b, p)
			}
			if !skip {
				rules += "Include rules/" + b + "\n"
			}
		}
		cfg = coraza.NewWAFConfig().WithRootFS(os.DirFS("testdata/crs3")).
			WithDirectives(base + "Include crs-setup.conf.example\n" + extra + "\n" + rules)
	default:
		cfg = coraza.NewWAFConfig().WithRootFS(coreruleset.FS).
			WithDirectives(base + "Include @crs-setup.conf.example\n" + extra + "\nInclude @owasp_crs/*.conf\n")
	}
	waf, err := coraza.NewWAF(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return waf
}

// wafCheck sends one request through the WAF; it returns the IDs of the
// matched rules that add to the score (empty: nothing found) and whether
// the request was blocked.
func wafCheck(t *testing.T, waf coraza.WAF, method, uri, body string) (rules []string, blocked bool) {
	t.Helper()
	return wafCheckType(t, waf, method, uri, "application/json", body)
}

// wafCheckType is wafCheck with a body of the given content type.
func wafCheckType(t *testing.T, waf coraza.WAF, method, uri, ctype, body string) (rules []string, blocked bool) {
	t.Helper()
	tx := waf.NewTransaction()
	defer func() { _ = tx.Close() }()
	tx.ProcessConnection("192.0.2.10", 50000, "192.0.2.1", 443)
	tx.ProcessURI(uri, method, "HTTP/1.1")
	for k, v := range map[string]string{
		"Host": "wiki.example.com", "User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:160.0) Gecko/20100101 Firefox/160.0",
		"Accept": "application/json", "Accept-Language": "de", "Accept-Encoding": "gzip, deflate, br",
		"Origin": "https://wiki.example.com", "X-Requested-With": "ewiki", "Cookie": "ewiki_session=abcdefghijklmnopqrstuvwxyz0123456789ABCDEF",
	} {
		tx.AddRequestHeader(k, v)
	}
	if body != "" {
		tx.AddRequestHeader("Content-Type", ctype)
		tx.AddRequestHeader("Content-Length", fmt.Sprint(len(body)))
	}
	var it *types.Interruption
	if it = tx.ProcessRequestHeaders(); it == nil {
		if body != "" {
			if _, _, err := tx.WriteRequestBody([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
		// phase 2 (most attack rules) runs also without a body
		var err error
		if it, err = tx.ProcessRequestBody(); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range tx.MatchedRules() {
		id := m.Rule().ID()
		if id >= 911000 && id < 949000 && m.Message() != "" {
			data := ""
			for _, md := range m.MatchedDatas() {
				data += " [" + md.Variable().Name() + ":" + md.Key() + "=" + md.Value() + "]"
			}
			rules = append(rules, fmt.Sprintf("%d %s%s", id, m.Message(), data))
		}
	}
	// CRS 3 in Coraza has no blocking evaluation (949): any attack rule
	// that matches counts (one critical match blocks there)
	return rules, it != nil || len(rules) > 0
}

// draftBody is what the editor sends when it keeps a draft (as in the log
// of the owner, 21.11.2026).
func draftBody(source string) string {
	b, _ := json.Marshal(map[string]any{"element_id": nil, "category_id": 2, "type": "core.html", "lang": "de", "title": "Testseite",
		"body": map[string]any{"format": "markdown", "source": source}, "tags": []string{}, "comment": "", "base_rev": 0})
	return string(b)
}

// TestWAFMacros: every macro with every parameter and every choice, as the
// editor writes it, passes the Core Rule Set without exclusions.
func TestWAFMacros(t *testing.T) {
	// every option of the schema has a sample
	var schema struct {
		Groups []struct {
			Options []struct {
				ID string `json:"id"`
			} `json:"options"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(content.BlockOptionsSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	for _, g := range schema.Groups {
		for _, o := range g.Options {
			if _, ok := optionSamples[o.ID]; !ok {
				t.Errorf("option %s has no sample in optionSamples", o.ID)
			}
		}
	}
	if os.Getenv("EWIKI_TEST_DATABASE_URL") == "" {
		t.Skip("needs the list of macros (EWIKI_TEST_DATABASE_URL)")
	}
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
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
		"textarea": "Text", "icon": "star", "json": `{"a":1}`}
	var bad []string
	// every parameter with every choice: alone (right after the brace,
	// where a shell separator counts), after and before another parameter,
	// and all together (sorted by name, as the editor writes them)
	type attr struct{ name, val string }
	for _, crs := range []string{"3", "4"} {
		waf := wafFor(t, crs, "")
		for _, m := range macros {
			var all, each, alone []attr
			for _, p := range m.Params {
				v, ok := sample[p.Type]
				if !ok {
					v = "1"
				}
				if p.Name == "options" {
					v = content.EncodeBlockOptions(map[string]any{"hide": []any{"mobile"}, "padding_top": map[string]any{"desktop": 4.0, "mobile": 2.0},
						"bg_color": "#0b3a6b", "text_color": "#ffffff", "anchor": "intro", "from": "2026-12-01T08:00:00Z", "langs": []any{"de", "en"}, "print": "hide"})
				}
				vals := []string{v}
				if len(p.Options) > 0 {
					vals = p.Options
				}
				if p.Name == "options" && m.Name == "section" {
					// every option alone, too (once: the options are the
					// same for every block)
					for k, x := range optionSamples {
						enc := content.EncodeBlockOptions(map[string]any{k: x})
						if enc == "" {
							t.Errorf("option %s: not in the schema (optionSamples is out of date)", k)
						}
						alone = append(alone, attr{p.Name, enc})
					}
				}
				for _, x := range vals {
					if x != "" {
						each = append(each, attr{p.Name, x})
					}
				}
				all = append(all, attr{p.Name, vals[0]})
			}
			fmtAttrs := func(as ...attr) string {
				sort.Slice(as, func(i, j int) bool { return as[i].name < as[j].name })
				parts := make([]string, len(as))
				for i, a := range as {
					parts[i] = a.name + "=" + quoteAttr(a.val)
				}
				return strings.Join(parts, " ")
			}
			sources := []string{wrapMacro(m.Name, m.HasBody, m.Inline, fmtAttrs(all...))}
			for _, a := range alone {
				sources = append(sources, wrapMacro(m.Name, m.HasBody, m.Inline, "a=1 "+fmtAttrs(a)), wrapMacro(m.Name, m.HasBody, m.Inline, fmtAttrs(a)+" z=1"))
			}
			// a value right after the brace, after another value and before
			// one (what precedes or follows matters to the rules, not which)
			for _, a := range each {
				sources = append(sources, wrapMacro(m.Name, m.HasBody, m.Inline, fmtAttrs(a)),
					wrapMacro(m.Name, m.HasBody, m.Inline, "a=1 "+fmtAttrs(a)), wrapMacro(m.Name, m.HasBody, m.Inline, fmtAttrs(a)+" z=1"))
			}
			seen := map[string]bool{}
			for _, src := range sources {
				if seen[src] {
					continue
				}
				seen[src] = true
				rules, blocked := wafCheck(t, waf, "POST", "/api/v1/drafts", draftBody(src))
				if blocked || len(rules) > 0 {
					bad = append(bad, fmt.Sprintf("CRS %s, %s: %q → %v", crs, m.Name, src, rules))
				}
			}
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	t.Logf("%d macros checked", len(macros))
}

// optionSamples: one valid value per block option (blockoptions.json).
var optionSamples = map[string]any{
	"hide": []any{"mobile", "tablet"}, "audience": "signed_in", "from": "2026-12-01T08:00:00Z", "until": "2026-12-24T18:00:00+01:00",
	"langs": []any{"de", "en"}, "print": "hide", "break_before": true, "break_inside_avoid": true, "anchor": "intro", "closable": true,
	"order": map[string]any{"mobile": -1.0}, "max_width": map[string]any{"desktop": 40.0}, "min_height": 20.0, "aspect": "16-9",
	"padding_top": map[string]any{"desktop": 4.0, "mobile": 2.0}, "padding_right": 3.0, "padding_bottom": 3.0, "padding_left": 3.0,
	"margin_top": 2.0, "margin_bottom": 2.0, "text_align": map[string]any{"tablet": "center"}, "bg_color": "#0b3a6b",
	"bg_gradient": "linear", "bg_gradient_angle": 45.0, "bg_gradient_from": "#0b3a6b", "bg_gradient_to": "#102040",
	"bg_image": "/api/v1/media/12/view", "bg_video": "/api/v1/media/13/view", "bg_focus_x": 50.0, "bg_focus_y": 50.0,
	"bg_size": "cover", "bg_repeat": "no-repeat", "bg_fixed": true, "overlay_color": "#000000", "overlay_opacity": 60.0,
	"blend": "multiply", "texture": "dots", "texture_color": "#ffffff", "glass": true, "border_style": "solid",
	"border_width": 2.0, "border_color": "#767676", "radius": 8.0, "shadow": "md", "glow": "#0b3a6b", "divider_top": "wave",
	"divider_bottom": "zigzag", "divider_height": 60.0, "divider_color": "#ffffff", "divider_flip": true, "fade_edges": "both",
	"font": "serif", "font_size": map[string]any{"desktop": 120.0}, "weight": "600", "line_height": 1.5, "letter_spacing": 0.05,
	"transform": "uppercase", "text_color": "#ffffff", "columns": map[string]any{"desktop": 2.0, "mobile": 1.0}, "hyphens": true, "wrap": "balance",
	"drop_cap": true, "text_shadow": "soft", "line_clamp": 3.0, "gradient_text": true, "link_style": "thick", "appear": "slide_start",
}

func wrapMacro(name string, body, inline bool, attrs string) string {
	switch {
	case inline:
		return "Text :" + name + "[Wort]{" + attrs + "} Text\n"
	case body:
		return ":::" + name + "{" + attrs + "}\nInhalt\n\n---\n\nInhalt 2\n:::\n"
	}
	return "::" + name + "{" + attrs + "}\n"
}

// quoteAttr writes a value as the editor does (formatAttrs in attrs.ts).
func quoteAttr(v string) string {
	if v != "" && !strings.ContainsAny(v, " \t\n\"}\\") {
		return v
	}
	v = strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`)
	return `"` + strings.ReplaceAll(v, "\n", " ") + `"`
}

// TestWAFExclusions: the documented exclusions (docker/waf) let the wiki's
// own requests with everyday content of an IT wiki through CRS 3 and 4;
// the same content outside the API's JSON writes is still blocked.
func TestWAFExclusions(t *testing.T) {
	excl, err := os.ReadFile("../../docker/waf/ewiki-crs-exclusions.conf")
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{
		"lines like shell commands": "Sort the list by date.\nset the value to 3\ntype your name",
		"HTML in a code sample":     "```html\n<script src=\"app.js\"></script>\n<button onclick=\"go()\">Go</button>\n```",
		"SQL in a manual":           "```sql\nSELECT * FROM users WHERE id = 1 OR 1=1; DROP TABLE logs; --\n```",
		"paths":                     "Die Datei liegt unter ../../etc/hosts und C:\\Windows\\System32\\drivers\\etc\\hosts.",
		"PHP and Java":              "`<?php system($_GET['cmd']); ?>` und `Runtime.getRuntime().exec(\"ls\")`",
		"template syntax":           "{{7*7}} ${7*7} <%= 7*7 %>",
	}
	type req struct{ method, uri, body string }
	wiki := func(text string) []req {
		return []req{
			{"POST", "/api/v1/drafts", draftBody(text)},
			{"PUT", "/api/v1/elements/12", draftBody(text)},
			{"POST", "/api/v1/elements/12/comments", `{"body":` + jsonString(text) + `}`},
			{"POST", "/api/v1/auth/login", `{"username":"admin","password":` + jsonString(text) + `}`},
		}
	}
	for _, crs := range []string{"3", "4"} {
		plain, withExcl := wafFor(t, crs, ""), wafFor(t, crs, string(excl))
		blockedWithout := 0
		for name, text := range contents {
			for _, r := range wiki(text) {
				if _, b := wafCheck(t, plain, r.method, r.uri, r.body); b {
					blockedWithout++
				}
				if rules, b := wafCheck(t, withExcl, r.method, r.uri, r.body); b || len(rules) > 0 {
					t.Errorf("CRS %s, %s, %s %s: still blocked: %v", crs, name, r.method, r.uri, rules)
				}
			}
			// searching for it
			if rules, b := wafCheck(t, withExcl, "GET", "/api/v1/search?q="+urlQuery(text), ""); b || len(rules) > 0 {
				t.Errorf("CRS %s, %s, search: still blocked: %v", crs, name, rules)
			}
		}
		if blockedWithout == 0 {
			t.Errorf("CRS %s: nothing blocked without the exclusions - the test proves nothing", crs)
		}
		// still blocked: attacks in the address, in headers, in non-JSON
		// bodies and outside the API
		attack := "<script>alert(document.cookie)</script>"
		for _, r := range []req{
			{"GET", "/api/v1/elements?x=" + urlQuery(attack), ""},
			{"GET", "/wiki/page?x=" + urlQuery(attack), ""},
			{"GET", "/api/v1/search?q=a&x=" + urlQuery(attack), ""},
		} {
			if _, b := wafCheck(t, withExcl, r.method, r.uri, r.body); !b {
				t.Errorf("CRS %s: %s %s passes with the exclusions", crs, r.method, r.uri)
			}
		}
		// SCIM: the identity provider creates, changes and removes accounts
		// with application/scim+json and PUT, PATCH, DELETE
		user := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"anna.obrien@example.com","displayName":"Anna O'Brien","name":{"givenName":"Anna","familyName":"O'Brien"},"emails":[{"value":"anna.obrien@example.com","primary":true}],"active":true,"externalId":"5f1c-77aa"}`
		patch := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"active","value":false},{"op":"add","path":"members","value":[{"value":"12"}]}]}`
		scimBlockedWithout := false
		for _, r := range []req{
			{"POST", "/scim/v2/Users", user},
			{"PUT", "/scim/v2/Users/7", user},
			{"PATCH", "/scim/v2/Users/7", patch},
			{"PATCH", "/scim/v2/Groups/3", patch},
			{"DELETE", "/scim/v2/Users/7", ""},
			{"GET", "/scim/v2/Users?filter=" + urlQuery(`userName eq "anna.obrien@example.com"`), ""},
		} {
			if _, b := wafCheckType(t, plain, r.method, r.uri, "application/scim+json", r.body); b {
				scimBlockedWithout = true
			}
			if rules, b := wafCheckType(t, withExcl, r.method, r.uri, "application/scim+json", r.body); b || len(rules) > 0 {
				t.Errorf("CRS %s, SCIM %s %s: still blocked: %v", crs, r.method, r.uri, rules)
			}
		}
		if crs == "4" && !scimBlockedWithout {
			t.Error("CRS 4: SCIM was not blocked without the exclusions - the test proves nothing")
		}
		// files as they are: an office server stores a document, the
		// administration imports a category or a translation; larger than
		// SecRequestBodyNoFilesLimit, with bytes that look like attacks
		big := strings.Repeat("<script>alert(1)</script> SELECT * FROM users WHERE 1=1; ../../etc/passwd\n", 20000)
		for _, r := range []struct{ uri, ctype string }{
			{"/api/v1/wopi/files/17/contents?access_token=" + urlQuery("eyJhbGciOiJFZERTQSJ9.eyJmIjoxN30.c2lnbmF0dXJl") + "&access_token_ttl=0", "application/octet-stream"},
			{"/api/v1/categories/import?parent=4", "application/x-ndjson"},
			{"/api/v1/admin/i18n/de/import", "application/octet-stream"},
		} {
			if crs == "4" {
				if _, b := wafCheckType(t, plain, "POST", r.uri, r.ctype, "x"); !b {
					t.Errorf("CRS 4: %s with %s passes without the exclusions - the test proves nothing", r.uri, r.ctype)
				}
			}
			if rules, b := wafCheckType(t, withExcl, "POST", r.uri, r.ctype, big); b || len(rules) > 0 {
				t.Errorf("CRS %s, POST %s (%s): still blocked: %v", crs, r.uri, r.ctype, rules)
			}
		}
		// other addresses keep the types closed and the body checked
		if _, b := wafCheckType(t, withExcl, "POST", "/api/v1/elements", "application/octet-stream", "x"); !b && crs == "4" {
			t.Errorf("CRS %s: application/octet-stream to /api/v1/elements passes", crs)
		}
		if _, b := wafCheckType(t, withExcl, "POST", "/api/v1/wopi/files/17", "application/octet-stream", "x"); !b && crs == "4" {
			t.Errorf("CRS %s: application/octet-stream to the WOPI file (not its contents) passes", crs)
		}
		// the exclusions for SCIM do not open the rest of the wiki to other
		// content types or methods
		if _, b := wafCheckType(t, withExcl, "PUT", "/wiki/page", "application/scim+json", user); !b && crs == "4" {
			t.Errorf("CRS %s: PUT with application/scim+json outside /scim/v2/ passes", crs)
		}
		if _, b := wafCheckForm(t, withExcl, "/api/v1/drafts", "source="+urlQuery(attack)); !b {
			t.Errorf("CRS %s: a form body (not JSON) passes with the exclusions", crs)
		}
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func urlQuery(s string) string { return url.QueryEscape(s) }

// wafCheckForm sends a form body (application/x-www-form-urlencoded).
func wafCheckForm(t *testing.T, waf coraza.WAF, uri, body string) ([]string, bool) {
	t.Helper()
	tx := waf.NewTransaction()
	defer func() { _ = tx.Close() }()
	tx.ProcessConnection("192.0.2.10", 50000, "192.0.2.1", 443)
	tx.ProcessURI(uri, "POST", "HTTP/1.1")
	for k, v := range map[string]string{"Host": "wiki.example.com", "User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:160.0) Gecko/20100101 Firefox/160.0",
		"Accept": "*/*", "Content-Type": "application/x-www-form-urlencoded", "Content-Length": fmt.Sprint(len(body))} {
		tx.AddRequestHeader(k, v)
	}
	if it := tx.ProcessRequestHeaders(); it != nil {
		return nil, true
	}
	if _, _, err := tx.WriteRequestBody([]byte(body)); err != nil {
		t.Fatal(err)
	}
	it, err := tx.ProcessRequestBody()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range tx.MatchedRules() {
		if id := m.Rule().ID(); id >= 911000 && id < 949000 && m.Message() != "" {
			n++
		}
	}
	return nil, it != nil || n > 0
}
