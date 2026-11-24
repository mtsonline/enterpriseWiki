package core_layout

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

// ---------------------------------------------------------------- newsletter (external)

// newsletter is a sign-up form for the newsletter of an external provider:
// the reader's browser sends the address straight to that provider, the wiki
// stores nothing. Only providers an administrator allowed (media.form_allow)
// are accepted, and the same list opens the Content-Security-Policy
// (form-action) for them. The form says where the address goes. The own
// newsletter of the wiki (with sign-up in the wiki) is a plugin of its own.
type newsletter struct{ allow func() []string }

func (newsletter) Name() string  { return "newsletter" }
func (newsletter) Group() string { return "forms" }
func (newsletter) Labels() map[string]string {
	return lm("Newsletter sign-up (external)", "Newsletter-Anmeldung (extern)")
}
func (newsletter) Description() map[string]string {
	return lm("A sign-up form for the newsletter of an allowed external provider; the address goes straight from the browser to the provider, the wiki stores nothing. The content is the text above the form.",
		"Ein Anmeldeformular für den Newsletter eines freigegebenen externen Anbieters; die Adresse geht direkt vom Browser zum Anbieter, das Wiki speichert nichts. Der Inhalt ist der Text über dem Formular.")
}
func (newsletter) HasBody() bool { return true }
func (newsletter) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "address", Labels: lm("Address of the sign-up (https, from the provider)", "Adresse der Anmeldung (https, vom Anbieter)"), Type: "url", Required: true},
		{Name: "title", Labels: lm("Title", "Titel"), Type: "text"},
		{Name: "field", Labels: lm("Name of the field for the e-mail address (from the provider)", "Name des Felds für die E-Mail-Adresse (vom Anbieter)"), Type: "text", Default: "email"},
		{Name: "name_field", Labels: lm("Name of a field for the name (optional, from the provider)", "Name eines Felds für den Namen (optional, vom Anbieter)"), Type: "text"},
		{Name: "hidden", Labels: lm("Fixed values of the provider (name=value, separated by ;)", "Feste Werte des Anbieters (Name=Wert, getrennt durch ;)"), Type: "text"},
		{Name: "button", Labels: lm("Text of the button", "Text des Knopfs"), Type: "text"},
		{Name: "privacy", Labels: lm("Privacy policy of the provider (link)", "Datenschutzerklärung des Anbieters (Link)"), Type: "url"},
	}
}

var (
	formFieldRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.\[\]-]{0,63}$`)
	formValueRe = regexp.MustCompile(`^[A-Za-z0-9_.:@/+-]{0,200}$`)
)

// FormTarget checks the address of a sign-up and returns it rebuilt from
// its parts, its origin (for the allow list) and its host.
func FormTarget(raw string) (target, origin, host string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2000 {
		return "", "", "", apperr.BadRequest("the sign-up needs the address of the provider (at most %d characters)", 2000)
	}
	u, perr := url.Parse(raw)
	if perr != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" {
		return "", "", "", apperr.BadRequest("the address must start with https:// and name a server")
	}
	u.Fragment, u.RawFragment = "", ""
	return u.String(), "https://" + strings.ToLower(u.Host), strings.ToLower(u.Hostname()), nil
}

func (n newsletter) Render(_ context.Context, mc content.MacroContext) (string, error) {
	target, origin, host, err := FormTarget(mc.Params["address"])
	if err != nil {
		return "", err
	}
	allowed := false
	if n.allow != nil {
		for _, o := range n.allow() {
			if strings.EqualFold(o, origin) {
				allowed = true
			}
		}
	}
	if !allowed {
		return "", apperr.BadRequest("sending to %s is not allowed in this wiki; an administrator can allow %s under Administration → Settings → Media", host, origin)
	}
	field := strings.TrimSpace(mc.Params["field"])
	if field == "" {
		field = "email"
	}
	nameField := strings.TrimSpace(mc.Params["name_field"])
	for _, f := range []string{field, nameField} {
		if f != "" && !formFieldRe.MatchString(f) {
			return "", apperr.BadRequest("a field name has letters, digits and _ . - [ ] only (at most 64)")
		}
	}
	type pair struct{ k, v string }
	var hidden []pair
	if h := strings.TrimSpace(mc.Params["hidden"]); h != "" {
		for _, p := range strings.Split(h, ";") {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			k, v, ok := strings.Cut(p, "=")
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			if !ok || !formFieldRe.MatchString(k) || !formValueRe.MatchString(v) || k == field || k == nameField {
				return "", apperr.BadRequest("fixed values: name=value, separated by ; (letters, digits and _ . : @ / + - only)")
			}
			hidden = append(hidden, pair{k, v})
		}
		if len(hidden) > 10 {
			return "", apperr.BadRequest("at most 10 fixed values")
		}
	}
	title, err := shortText("title", mc.Params["title"], 120)
	if err != nil {
		return "", err
	}
	button, err := shortText("button", mc.Params["button"], 60)
	if err != nil {
		return "", err
	}
	if button == "" {
		button = tr(mc.Lang, "Subscribe", "Anmelden")
	}
	privacy := strings.TrimSpace(mc.Params["privacy"])
	if privacy != "" {
		if u, err := url.Parse(privacy); err != nil || (u.Scheme != "https" && u.Scheme != "") || u.User != nil {
			return "", apperr.BadRequest("privacy: a link (https or a page of the wiki)")
		}
	}
	id := fmt.Sprintf("ewl-nl-%d", mc.Index)
	var b strings.Builder
	b.WriteString(`<section class="ewl-newsletter"`)
	if title != "" {
		fmt.Fprintf(&b, ` aria-labelledby="%s-t"><h2 class="ewl-newsletter-title" id="%s-t">%s</h2>`, id, id, esc(title))
	} else {
		fmt.Fprintf(&b, ` aria-label="%s">`, esc(tr(mc.Lang, "Newsletter", "Newsletter")))
	}
	if strings.TrimSpace(mc.Body) != "" {
		b.WriteString(`<div class="ewl-newsletter-text">` + mc.Body + `</div>`)
	}
	// a plain form to the provider: the browser sends it there itself
	fmt.Fprintf(&b, `<form class="ewl-newsletter-form" method="post" action="%s" accept-charset="utf-8">`, esc(target))
	for _, p := range hidden {
		fmt.Fprintf(&b, `<input type="hidden" name="%s" value="%s">`, esc(p.k), esc(p.v))
	}
	if nameField != "" {
		fmt.Fprintf(&b, `<p class="ewl-newsletter-field"><label for="%s-n">%s</label><input class="ew-input" id="%s-n" name="%s" type="text" autocomplete="name" maxlength="200"></p>`,
			id, esc(tr(mc.Lang, "Name", "Name")), id, esc(nameField))
	}
	fmt.Fprintf(&b, `<p class="ewl-newsletter-field"><label for="%s-e">%s</label><input class="ew-input" id="%s-e" name="%s" type="email" autocomplete="email" required maxlength="254" aria-describedby="%s-p"></p>`,
		id, esc(tr(mc.Lang, "E-mail address", "E-Mail-Adresse")), id, esc(field), id)
	// where the address goes, before it goes there
	fmt.Fprintf(&b, `<p class="ewl-newsletter-privacy" id="%s-p">%s`, id, esc(fmt.Sprintf(tr(mc.Lang,
		"When you subscribe, your browser sends your e-mail address to the provider of this newsletter (%s).",
		"Wenn Sie sich anmelden, sendet Ihr Browser Ihre E-Mail-Adresse an den Anbieter dieses Newsletters (%s)."), host)))
	if privacy != "" {
		fmt.Fprintf(&b, ` <a href="%s" rel="noopener noreferrer">%s</a>`, esc(SafeURL(privacy)), esc(tr(mc.Lang, "Privacy policy of the provider", "Datenschutzerklärung des Anbieters")))
	}
	fmt.Fprintf(&b, `</p><p class="ewl-newsletter-action"><button type="submit" class="ewl-cta ewl-cta--primary">%s</button></p></form></section>`, esc(button))
	return b.String(), nil
}
