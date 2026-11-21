package core_layout

import (
	"strings"
	"testing"
)

// TestLogin: the box links to the sign-in page of the wiki and never has a
// form of its own; texts are escaped; options become data attributes.
func TestLogin(t *testing.T) {
	out, err := render(t, login{}, "de", map[string]string{"text": `Für <b>Mitglieder</b>`, "member": `Willkommen "zurück"`, "sso": "false"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-ew-widget="ewl.login"`, `aria-label="Anmelden"`, `<p class="ewl-login-title">Anmelden</p>`,
		`Für &lt;b&gt;Mitglieder&lt;/b&gt;`, `data-member="Willkommen &#34;zurück&#34;"`, `data-sso="0"`, `data-dashboard="1"`, `href="/login"`} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing in %s", want, out)
		}
	}
	for _, bad := range []string{"<form", "<input", "password", "<b>"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q in %s", bad, out)
		}
	}
	out, err = render(t, login{}, "en", map[string]string{"title": "Members"}, "")
	if err != nil || !strings.Contains(out, `aria-label="Members"`) || !strings.Contains(out, ">Log in</a>") {
		t.Errorf("%v %s", err, out)
	}
	if _, err := render(t, login{}, "en", map[string]string{"text": strings.Repeat("x", 501)}, ""); err == nil {
		t.Error("long text accepted")
	}
}
