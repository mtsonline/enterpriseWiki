package server_test

import (
	"strings"
	"testing"
)

// TestErrorLanguage: the server answers errors in the language the client
// asks for (the interface sends its own), English without a request or
// for a language without texts; the code stays the same.
func TestErrorLanguage(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	for lang, want := range map[string]string{
		"de-AT,de;q=0.9": `"message":"Element nicht gefunden"`,
		"":               `"message":"element not found"`,
		"fr":             `"message":"element not found"`,
	} {
		hdr := map[string]string{}
		if lang != "" {
			hdr["Accept-Language"] = lang
		}
		resp, body := admin.raw("/api/v1/elements/999999", hdr)
		if resp.StatusCode != 404 || !strings.Contains(body, want) || !strings.Contains(body, `"code":"not_found"`) {
			t.Errorf("%q: %d %s", lang, resp.StatusCode, body)
		}
	}
	// a message with values: the values stay, the text is German
	resp, body := admin.raw("/api/v1/elements/abc", map[string]string{"Accept-Language": "de"})
	if resp.StatusCode != 400 || !strings.Contains(body, `"message":"Ungültige ID"`) && !strings.Contains(body, `"message":"Ungültig: `) {
		t.Errorf("with values: %d %s", resp.StatusCode, body)
	}
	// an address of the API that does not exist
	resp, body = admin.raw("/api/v1/no-such-thing", map[string]string{"Accept-Language": "de"})
	if resp.StatusCode != 404 || !strings.Contains(body, "Diese Schnittstelle gibt es nicht") {
		t.Errorf("no endpoint: %d %s", resp.StatusCode, body)
	}
	// guests too (sign-in needed)
	resp, body = e.client().raw("/api/v1/admin/users", map[string]string{"Accept-Language": "de"})
	if resp.StatusCode != 401 || !strings.Contains(body, "Bitte anmelden") {
		t.Errorf("guest: %d %s", resp.StatusCode, body)
	}
	// notes of macros in the content are in the language of the page
	for lang, want := range map[string]string{"de": "Die Überschrift braucht einen Text", "en": "the heading needs a text"} {
		var res struct {
			HTML string `json:"html"`
		}
		admin.must("POST", "/api/v1/elements/preview", map[string]any{"type": "core.html", "lang": lang, "body": map[string]any{"format": "markdown", "source": "::heading{}"}}, &res, 200)
		if !strings.Contains(res.HTML, want) || !strings.Contains(res.HTML, "ew-macro-error") {
			t.Errorf("macro note in %s: %s", lang, res.HTML)
		}
	}
}
