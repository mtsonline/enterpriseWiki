package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
)

const loginPageUsage = `usage: ewiki login-page show [--as USER]
       ewiki login-page set [--passkey-autofill on|off] [--auto-redirect PROVIDER|none]
                            [--password-reset on|off] [--registration on|off] [--registration-domains D,D|none] [--registration-groups ID,ID|none] [--as USER]
       ewiki login-page button PROVIDER [--order N] [--label TEXT] [--hidden|--visible] [--reset] [--as USER]`

// loginPageSettings mirrors the answer and body of /api/v1/admin/auth/login.
type loginPageSettings struct {
	PasskeyAutofill bool                       `json:"passkey_autofill"`
	AutoRedirect    string                     `json:"auto_redirect"`
	Buttons         map[string]loginPageButton `json:"buttons"`
	PasswordReset   bool                       `json:"password_reset"`
	Registration    struct {
		Enabled bool     `json:"enabled"`
		Domains []string `json:"domains"`
		Groups  []int64  `json:"groups"`
	} `json:"registration"`
}

type loginPageButton struct {
	Order  int    `json:"order"`
	Hidden bool   `json:"hidden"`
	Label  string `json:"label"`
}

// loginPage shows and changes the sign-in page settings through the API.
func (e *env) loginPage(args []string) error {
	if len(args) == 0 {
		args = []string{"show"}
	}
	sub, args := args[0], args[1:]
	var provider string
	if sub == "button" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		provider, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("login-page", flag.ContinueOnError)
	autofill := fs.String("passkey-autofill", "", "on or off: passkeys in the suggestions of the user name field")
	redirect := fs.String("auto-redirect", "", "send the sign-in page straight to this provider, or none")
	order := fs.Int("order", 0, "position of the button (lower first)")
	label := fs.String("label", "", "text of the button instead of the provider name")
	hidden := fs.Bool("hidden", false, "no button; the provider stays usable at /login?via=PROVIDER")
	visible := fs.Bool("visible", false, "show the button again")
	reset := fs.Bool("reset", false, "back to the defaults for this button")
	pwReset := fs.String("password-reset", "", "on or off: \"Forgot your password?\" for local accounts")
	reg := fs.String("registration", "", "on or off: people may create a local account themselves")
	regDomains := fs.String("registration-domains", "", "only addresses of these domains (comma separated, *.example.com for subdomains), or none")
	regGroups := fs.String("registration-groups", "", "IDs of the groups new accounts join (comma separated), or none")
	as := fs.String("as", "", "act as this user (never more than the rights of the signed-in user)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	const path = "/api/v1/admin/auth/login"
	switch {
	case sub == "show":
		return e.apiBody("GET", path, nil, *as)
	case sub == "set" || (sub == "button" && provider != ""):
	default:
		return errors.New(loginPageUsage)
	}
	var cur loginPageSettings
	if err := e.apiJSON("GET", path, *as, func(b []byte) error { return json.Unmarshal(b, &cur) }); err != nil {
		return err
	}
	if cur.Buttons == nil {
		cur.Buttons = map[string]loginPageButton{}
	}
	if sub == "set" {
		switch {
		case !set["passkey-autofill"]:
		case *autofill == "on":
			cur.PasskeyAutofill = true
		case *autofill == "off":
			cur.PasskeyAutofill = false
		default:
			return errors.New("--passkey-autofill: on or off")
		}
		switch {
		case !set["password-reset"]:
		case *pwReset == "on":
			cur.PasswordReset = true
		case *pwReset == "off":
			cur.PasswordReset = false
		default:
			return errors.New("--password-reset: on or off")
		}
		switch {
		case !set["registration"]:
		case *reg == "on":
			cur.Registration.Enabled = true
		case *reg == "off":
			cur.Registration.Enabled = false
		default:
			return errors.New("--registration: on or off")
		}
		if set["registration-domains"] {
			cur.Registration.Domains = list(*regDomains)
		}
		if set["registration-groups"] {
			cur.Registration.Groups = []int64{}
			for _, g := range list(*regGroups) {
				id, err := strconv.ParseInt(g, 10, 64)
				if err != nil {
					return fmt.Errorf("--registration-groups: %q is not a group ID", g)
				}
				cur.Registration.Groups = append(cur.Registration.Groups, id)
			}
		}
		if set["auto-redirect"] {
			cur.AutoRedirect = strings.TrimSpace(*redirect)
			if cur.AutoRedirect == "none" {
				cur.AutoRedirect = ""
			}
		}
	} else {
		if *hidden && *visible {
			return errors.New("--hidden and --visible exclude each other")
		}
		b := cur.Buttons[provider]
		if *reset {
			b = loginPageButton{}
		}
		if set["order"] {
			b.Order = *order
		}
		if set["label"] {
			b.Label = *label
		}
		if *hidden {
			b.Hidden = true
		}
		if *visible {
			b.Hidden = false
		}
		cur.Buttons[provider] = b
	}
	body, _ := json.Marshal(cur)
	return e.apiBody("PUT", path, body, *as)
}

// list splits a comma separated value; "none" or "" is the empty list.
func list(v string) []string {
	out := []string{}
	if strings.TrimSpace(v) == "none" {
		return out
	}
	for _, x := range strings.Split(v, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
