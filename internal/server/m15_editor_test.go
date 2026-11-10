package server_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Personal settings: only registered keys and values, per user, removable;
// the CLI (--as) shows and sets them.
func TestPrefs(t *testing.T) {
	e := setup(t)
	ctx := identity.With(context.Background(), identity.CLI())
	if _, err := e.app.Auth.CreateLocalUserUnchecked(ctx, auth.NewUser{Username: "carol", Password: "Carol-Passw0rd!", DisplayName: "Carol"}); err != nil {
		t.Fatal(err)
	}
	c := e.client()
	c.login("carol", "Carol-Passw0rd!")
	var p struct {
		Prefs   map[string]string   `json:"prefs"`
		Options map[string][]string `json:"options"`
	}
	c.must("GET", "/api/v1/me/prefs", nil, &p, 200)
	if len(p.Prefs) != 0 || len(p.Options["editor"]) != 2 {
		t.Fatalf("initial prefs %+v", p)
	}
	c.must("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"editor": "simple"}}, nil, 204)
	for _, bad := range []map[string]string{{"editor": "fancy"}, {"unknown": "x"}, {"editor": "<script>"}} {
		if got := c.do("PUT", "/api/v1/me", map[string]any{"prefs": bad}, nil); got != 400 {
			t.Errorf("prefs %v: status %d, want 400", bad, got)
		}
	}
	var me struct {
		User struct {
			Prefs map[string]string `json:"prefs"`
		} `json:"user"`
	}
	c.must("GET", "/api/v1/auth/me", nil, &me, 200)
	if me.User.Prefs["editor"] != "simple" {
		t.Fatalf("prefs after update: %v", me.User.Prefs)
	}
	// other users are not affected
	admin := e.client()
	admin.login("admin", adminPW)
	admin.must("GET", "/api/v1/me/prefs", nil, &p, 200)
	if p.Prefs["editor"] != "" {
		t.Fatalf("admin prefs changed: %v", p.Prefs)
	}
	// "" removes the setting
	c.must("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"editor": ""}}, nil, 204)
	c.must("GET", "/api/v1/me/prefs", nil, &p, 200)
	if _, ok := p.Prefs["editor"]; ok {
		t.Fatalf("editor not removed: %v", p.Prefs)
	}
	// guests have no settings
	if got := e.client().do("GET", "/api/v1/me/prefs", nil, nil); got != 401 {
		t.Errorf("guest prefs: %d", got)
	}
	if got := e.client().do("PUT", "/api/v1/me", map[string]any{"prefs": map[string]string{"editor": "simple"}}, nil); got != 401 {
		t.Errorf("guest PUT prefs: %d", got)
	}
	// CLI
	if code, out := runCLI(t, e.cfg, "pref", "set", "editor", "simple", "--as", "carol"); code != 0 {
		t.Fatalf("pref set: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "pref", "list", "--as", "carol"); code != 0 || !strings.Contains(out, `"simple"`) {
		t.Fatalf("pref list: %d %s", code, out)
	}
	if code, _ := runCLI(t, e.cfg, "pref", "list"); code == 0 {
		t.Error("pref without --as must fail")
	}
}

// The default editor of the site is part of the site info and validated.
func TestEditorDefault(t *testing.T) {
	e := setup(t)
	var site struct {
		Editor string `json:"editor"`
	}
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if site.Editor != "visual" {
		t.Fatalf("default editor %q", site.Editor)
	}
	if err := e.app.Config.Update(func(c *config.Config) error { c.UI.Editor = "fancy"; return nil }); err == nil {
		t.Fatal("invalid ui.editor accepted")
	}
	if err := e.app.Config.Update(func(c *config.Config) error { c.UI.Editor = "simple"; return nil }); err != nil {
		t.Fatal(err)
	}
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if site.Editor != "simple" {
		t.Fatalf("editor %q", site.Editor)
	}
}

// The conversions of the editors execute no macro and return sanitized HTML.
func TestEditorConversions(t *testing.T) {
	e := setup(t)
	if got := e.client().do("POST", "/api/v1/x/core.html/edit-html", map[string]string{"source": "x"}, nil); got != 401 {
		t.Errorf("guest conversion: %d", got)
	}
	c := e.client()
	c.login("admin", adminPW)
	var out struct {
		HTML     string `json:"html"`
		Markdown string `json:"markdown"`
	}
	src := "# T\n\n::toc{levels=2}\n\n:::panel{type=info}\nIn **panel**\n:::\n\n- [x] done\n\n<img src=x onerror=alert(1)><script>alert(2)</script>\n"
	c.must("POST", "/api/v1/x/core.html/edit-html", map[string]string{"source": src}, &out, 200)
	for _, want := range []string{`data-macro="toc"`, `data-macro="panel"`, "<strong>panel</strong>"} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("edit-html lacks %q: %s", want, out.HTML)
		}
	}
	for _, bad := range []string{"onerror", "<script", "ew-toc"} {
		if strings.Contains(out.HTML, bad) {
			t.Errorf("edit-html contains %q: %s", bad, out.HTML)
		}
	}
	c.must("POST", "/api/v1/x/core.html/to-markdown", map[string]string{"source": out.HTML}, &out, 200)
	for _, want := range []string{"::toc{levels=2}", ":::panel{type=info}", "- [x] done"} {
		if !strings.Contains(out.Markdown, want) {
			t.Errorf("to-markdown lacks %q: %s", want, out.Markdown)
		}
	}
	big, _ := json.Marshal(map[string]string{"source": strings.Repeat("a", 3<<20)})
	if got := c.do("POST", "/api/v1/x/core.html/edit-html", json.RawMessage(big), nil); got != 400 {
		t.Errorf("oversized conversion: %d", got)
	}
}

// Pages still stored as HTML (old editor) are converted to Markdown once by
// the background job; the history keeps the HTML.
func TestConvertHTMLPages(t *testing.T) {
	e := setup(t)
	c := e.client()
	c.login("admin", adminPW)
	var cat struct {
		ID int64 `json:"id"`
	}
	c.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Conv"}}, &cat, 201)
	var el struct {
		ID int64 `json:"id"`
	}
	c.must("POST", "/api/v1/elements", map[string]any{"category_id": cat.ID, "type": "core.html", "title": "Old", "body": htmlBody("<p>x</p>")}, &el, 201)
	// as written by an old version: HTML in the latest revision
	if _, err := e.app.DB.Exec(context.Background(), `UPDATE element_revisions SET body='{"format":"html","source":"<h2>Old</h2><p>Some <b>bold</b></p>"}' WHERE element_id=$1`, el.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Workers.RunOnce(context.Background(), "core.html.convert-markdown"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Body struct {
			Format string `json:"format"`
			Source string `json:"source"`
		} `json:"body"`
	}
	c.must("GET", "/api/v1/elements/"+strconv.FormatInt(el.ID, 10)+"?edit=1", nil, &got, 200)
	if got.Body.Format != "markdown" || !strings.Contains(got.Body.Source, "## Old") || !strings.Contains(got.Body.Source, "**bold**") {
		t.Fatalf("converted body: %+v", got.Body)
	}
}
