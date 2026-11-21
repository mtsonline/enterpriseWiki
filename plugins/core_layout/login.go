package core_layout

import (
	"context"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

// ---------------------------------------------------------------- sign-in block

// login is a sign-in box for pages with a members' area. It does not sign
// anybody in itself: guests get the way to the sign-in page of the wiki
// (and, if chosen, the sign-in services the administrators set up), so the
// one sign-in with all its checks (second factor, passkey, lockout) stays
// the only one. Signed-in people see their name, a link to their dashboard
// and "Sign out". Without the script the link to the sign-in page remains.
type login struct{}

func (login) Name() string { return "login" }
func (login) Labels() map[string]string {
	return lm("Sign-in box", "Anmelde-Baustein")
}
func (login) Description() map[string]string {
	return lm("Guests find the way to sign in here; signed-in people see their name, their dashboard and Log out.",
		"Gäste finden hier den Weg zur Anmeldung; Angemeldete sehen ihren Namen, ihr Dashboard und Abmelden.")
}
func (login) HasBody() bool { return false }
func (login) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "title", Labels: lm("Title (empty: Log in)", "Titel (leer: Anmelden)"), Type: "text"},
		{Name: "text", Labels: lm("Text for guests", "Text für Gäste"), Type: "text"},
		{Name: "member", Labels: lm("Text for signed-in people", "Text für Angemeldete"), Type: "text"},
		{Name: "sso", Labels: lm("Buttons for the sign-in services", "Knöpfe für die Anmeldedienste"), Type: "bool", Default: "true"},
		{Name: "dashboard", Labels: lm("Link to the dashboard", "Link zum Dashboard"), Type: "bool", Default: "true"},
	}
}

func (login) Render(_ context.Context, mc content.MacroContext) (string, error) {
	title, err := shortText("title", mc.Params["title"], 120)
	if err != nil {
		return "", err
	}
	text, err := shortText("text", mc.Params["text"], 500)
	if err != nil {
		return "", err
	}
	member, err := shortText("member", mc.Params["member"], 500)
	if err != nil {
		return "", err
	}
	de := strings.HasPrefix(mc.Lang, "de")
	signIn := "Log in"
	if de {
		signIn = "Anmelden"
	}
	if title == "" {
		title = signIn
	}
	flag := func(name string) string {
		if mc.Params[name] == "false" {
			return "0"
		}
		return "1"
	}
	var b strings.Builder
	b.WriteString(`<section class="ewl-login" data-ew-widget="ewl.login" aria-label="` + esc(title) + `" data-sso="` + flag("sso") + `" data-dashboard="` + flag("dashboard") + `" data-member="` + esc(member) + `">`)
	b.WriteString(`<p class="ewl-login-title">` + esc(title) + `</p>`)
	if text != "" {
		b.WriteString(`<p class="ewl-login-text">` + esc(text) + `</p>`)
	}
	b.WriteString(`<p class="ewl-login-actions"><a class="ewl-cta ewl-cta--primary" href="/login">` + esc(signIn) + `</a></p></section>`)
	return b.String(), nil
}
