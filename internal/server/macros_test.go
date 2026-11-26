package server_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

func TestMacros(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var macros []struct {
		Name    string `json:"name"`
		Plugin  string `json:"plugin"`
		HasBody bool   `json:"has_body"`
		Inline  bool   `json:"inline"`
		Picture bool   `json:"picture"`
		Group   string `json:"group"`
		Parts   *struct {
			Labels map[string]string `json:"labels"`
			Titled bool              `json:"titled"`
		} `json:"parts"`
		Params []struct {
			Name         string                       `json:"name"`
			Type         string                       `json:"type"`
			Options      []string                     `json:"options"`
			OptionLabels map[string]map[string]string `json:"option_labels"`
		} `json:"params"`
	}
	admin.must("GET", "/api/v1/macros", nil, &macros, 200)
	names := map[string]bool{}
	byName := map[string]int{}
	for i, m := range macros {
		names[m.Name] = true
		byName[m.Name] = i
		// the macro dialog never shows a bare value: every choice is named in both languages
		for _, p := range m.Params {
			for _, o := range p.Options {
				if l := p.OptionLabels[o]; !isNumber(o) && (l["en"] == "" || l["de"] == "") {
					t.Errorf("macro %s, parameter %s: option %q has no labels (%v)", m.Name, p.Name, o, l)
				}
			}
		}
		// every block of the wiki has its group in the menu of the editor
		if !slices.Contains(content.MacroGroups, m.Group) || m.Group == "other" {
			t.Errorf("macro %s: group %q", m.Name, m.Group)
		}
		if m.Picture && (m.HasBody || m.Inline) {
			t.Errorf("macro %s: a picture only for macros without a body", m.Name)
		}
	}
	// macros with parts tell the editor how to add one; pictures only of
	// macros without side effects
	if m := macros[byName["accordion"]]; m.Parts == nil || !m.Parts.Titled || m.Parts.Labels["de"] != "Teil" {
		t.Errorf("accordion parts: %+v", m.Parts)
	}
	if m := macros[byName["columns"]]; m.Parts == nil || m.Parts.Titled {
		t.Errorf("columns parts: %+v", m.Parts)
	}
	for n, want := range map[string]bool{"buttons": true, "pagetree": true, "shell": false, "http": false, "news": false, "panel": false} {
		if macros[byName[n]].Picture != want {
			t.Errorf("macro %s: picture %v, want %v", n, !want, want)
		}
	}
	for _, n := range []string{"toc", "children", "recent", "panel", "expand", "status", "columns", "section", "hero", "cards", "button", "spacer", "shell", "http"} {
		if !names[n] {
			t.Errorf("macro %s missing", n)
		}
	}
	var root, child cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Handbook"}}, &root, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": root.ID, "titles": map[string]string{"en": "Chapter <1>"}}, &child, 201)
	src := `<div data-macro="toc"></div><h2>Intro</h2><p>x</p><h2>Intro</h2>` +
		`<div data-macro="panel" data-params='{"type":"warning","title":"Careful <b>"}'><p>Hot <script>alert(1)</script></p>` +
		`<div data-macro="status" data-params='{"text":"done","color":"green"}'></div></div>` +
		`<div data-macro="children"></div><div data-macro="nope"></div>` +
		`<div data-macro="hero" data-params='{"title":"Welcome","button":"Go","link":"javascript:alert(1)","height":"half","phone_height":"100vh;x"}'></div>`
	var el elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Start", "body": htmlBody(src)}, &el, 201)
	for _, want := range []string{
		`<a href="#intro">Intro</a>`, `<a href="#intro-1">Intro</a>`, `id="intro-1"`,
		`ew-macro-panel--warning`, `Careful &lt;b&gt;`, `ew-status--green">DONE`,
		fmt.Sprintf(`href="/c/%d">Chapter &lt;1&gt;`, child.ID), `block &#34;nope&#34; is not available`,
		`ewl-hero`, `href="#">Go`, `ewl-hero--h-half ewl-hero--p-auto`,
	} {
		if !strings.Contains(el.HTML, want) {
			t.Errorf("rendered page lacks %q:\n%s", want, el.HTML)
		}
	}
	if strings.Contains(el.HTML, "<script") || strings.Contains(el.HTML, "javascript:") {
		t.Fatalf("unsafe output: %s", el.HTML)
	}
	// the hidden child category is not listed for a user without access
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", root.ID), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": false}, nil, 201)
	var bob idResp
	admin.must("POST", "/api/v1/admin/users", map[string]any{"username": "bob", "password": "Bob-Passw0rd!x", "display_name": "Bob"}, &bob, 201)
	bobc := e.client()
	bobc.login("bob", "Bob-Passw0rd!x")
	var bel elem
	bobc.must("GET", fmt.Sprintf("/api/v1/elements/%d", el.ID), nil, &bel, 200)
	if strings.Contains(bel.HTML, "Chapter") {
		t.Fatalf("children macro leaks hidden category: %s", bel.HTML)
	}
	// disabling the plugin disables its macros
	admin.must("POST", "/api/v1/admin/plugins/core.layout/disable", nil, nil, 204)
	admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", el.ID), nil, &el, 200)
	if !strings.Contains(el.HTML, `block &#34;hero&#34; is not available`) {
		t.Fatalf("disabled macro still rendered: %s", el.HTML)
	}
	// macro security settings cannot be changed through the API
	if got := admin.do("PUT", "/api/v1/admin/settings", map[string]any{"macros": map[string]any{"shell_enabled": true}}, nil); got != 400 {
		t.Fatalf("shell switched on through the API: %d", got)
	}
}

func TestShellMacro(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell")
	}
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var root cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Ops"}}, &root, 201)
	body := htmlBody(`<div data-macro="shell" data-params='{"command":"echo \"<b>hello</b> $((40+2))\""}'></div>`)
	var el elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "Status", "body": body}, &el, 201)
	if !strings.Contains(el.HTML, "Shell commands are switched off on this server.") {
		t.Fatalf("shell ran while disabled: %s", el.HTML)
	}
	if err := e.app.Config.Update(func(c *config.Config) error { c.Plugins.Macros.ShellEnabled = true; return nil }); err != nil {
		t.Fatal(err)
	}
	get := func() string {
		var x elem
		admin.must("GET", fmt.Sprintf("/api/v1/elements/%d", el.ID), nil, &x, 200)
		return x.HTML
	}
	if h := get(); !strings.Contains(h, "waiting for approval") {
		t.Fatalf("unapproved command: %s", h)
	}
	var list struct {
		Commands []struct {
			Hash    string `json:"hash"`
			Command string `json:"command"`
			Status  string `json:"status"`
		} `json:"commands"`
		CanApprove bool `json:"can_approve"`
	}
	admin.must("GET", "/api/v1/x/macro.shell/commands", nil, &list, 200)
	if len(list.Commands) != 1 || list.Commands[0].Status != "pending" || list.CanApprove {
		t.Fatalf("commands %+v", list)
	}
	hash := list.Commands[0].Hash
	// an admin who is not listed as approver cannot approve
	if got := admin.do("POST", "/api/v1/x/macro.shell/commands/"+hash+"/approve", nil, nil); got != 403 {
		t.Fatalf("approve without being superadmin: %d", got)
	}
	if err := e.app.Config.Update(func(c *config.Config) error { c.Plugins.Macros.ShellApprovers = []string{"admin@local"}; return nil }); err != nil {
		t.Fatal(err)
	}
	admin.must("POST", "/api/v1/x/macro.shell/commands/"+hash+"/approve", nil, nil, 204)
	if h := get(); !strings.Contains(h, "&lt;b&gt;hello&lt;/b&gt; 42") {
		t.Fatalf("approved command output: %s", h)
	}
	// changing the command needs a new approval
	body = htmlBody(`<div data-macro="shell" data-params='{"command":"echo changed"}'></div>`)
	admin.must("PUT", fmt.Sprintf("/api/v1/elements/%d", el.ID), map[string]any{"lang": "en", "title": "Status", "body": body}, nil, 200)
	if h := get(); !strings.Contains(h, "waiting for approval") {
		t.Fatalf("changed command ran without approval: %s", h)
	}
	admin.must("POST", "/api/v1/x/macro.shell/commands/"+hash+"/revoke", nil, nil, 204)
	admin.must("DELETE", "/api/v1/x/macro.shell/commands/"+hash, nil, nil, 204)
	var created struct {
		Hash string `json:"hash"`
	}
	admin.must("POST", "/api/v1/x/macro.shell/commands", map[string]any{"command": "echo pre", "approve": true}, &created, 200)
	if len(created.Hash) != 64 {
		t.Fatalf("register command: %+v", created)
	}
}

func TestHTTPMacro(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/users":
			w.Write([]byte(`{"data":{"users":[{"name":"Ann","age":30},{"name":"<Bob>","age":41}]}}`))
		case "/redirect":
			http.Redirect(w, r, "http://example.invalid/", http.StatusFound)
		case "/down":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.Write([]byte("plain <text>"))
		}
	}))
	defer upstream.Close()
	var root cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Data"}}, &root, 201)
	page := func(params string) string {
		var el elem
		admin.must("POST", "/api/v1/elements", map[string]any{"category_id": root.ID, "type": "core.html", "title": "x",
			"body": htmlBody(`<div data-macro="http" data-params='` + params + `'></div>`)}, &el, 201)
		return el.HTML
	}
	if h := page(`{"address":"` + upstream.URL + `/x"}`); !strings.Contains(h, "not on the allow list") {
		t.Fatalf("not allowed URL fetched: %s", h)
	}
	if err := e.app.Config.Update(func(c *config.Config) error { c.Plugins.Macros.HTTPAllow = []string{upstream.URL + "/"}; return nil }); err != nil {
		t.Fatal(err)
	}
	if h := page(`{"address":"` + upstream.URL + `/x"}`); !strings.Contains(h, "HTTP request: the address is internal; only plugins.macros.http_allow_private in config.yaml allows it") {
		t.Fatalf("loopback address fetched: %s", h)
	}
	// the notes in the language of the page
	german := func(params string) string {
		var res struct {
			HTML string `json:"html"`
		}
		admin.must("POST", "/api/v1/elements/preview", map[string]any{"type": "core.html", "lang": "de",
			"body": htmlBody(`<div data-macro="http" data-params='` + params + `'></div>`)}, &res, 200)
		return res.HTML
	}
	if h := german(`{"address":"` + upstream.URL + `/x"}`); !strings.Contains(h, "HTTP-Abruf: die Adresse ist intern; nur plugins.macros.http_allow_private in config.yaml erlaubt sie") {
		t.Fatalf("German note for an internal address: %s", h)
	}
	if err := e.app.Config.Update(func(c *config.Config) error { c.Plugins.Macros.HTTPAllowPrivate = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if h := page(`{"address":"` + upstream.URL + `/x","cache":"0"}`); !strings.Contains(h, "plain &lt;text&gt;") {
		t.Fatalf("text: %s", h)
	}
	if h := page(`{"address":"` + upstream.URL + `/api/users","format":"json","path":"data.users"}`); !strings.Contains(h, "<th scope=\"col\">name</th>") || !strings.Contains(h, "&lt;Bob&gt;") {
		t.Fatalf("json table: %s", h)
	}
	if h := german(`{"address":"` + upstream.URL + `/down","cache":"0"}`); !strings.Contains(h, "HTTP-Abruf: der Server antwortete mit dem Status 503") {
		t.Fatalf("German note for a failing server: %s", h)
	}
	if h := german(`{"address":"` + upstream.URL + `/x","cache":"0","format":"json"}`); !strings.Contains(h, "HTTP-Abruf: die Antwort ist kein gültiges JSON") {
		t.Fatalf("German note for invalid JSON: %s", h)
	}
	if h := page(`{"address":"` + upstream.URL + `/down","cache":"0"}`); !strings.Contains(h, "HTTP request: the server answered with the status 503") {
		t.Fatalf("note for a failing server: %s", h)
	}
	if h := page(`{"address":"` + upstream.URL + `/redirect"}`); !strings.Contains(h, "not on the allow list") {
		t.Fatalf("redirect outside the allow list followed: %s", h)
	}
}
