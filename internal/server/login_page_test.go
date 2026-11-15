package server_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/limits"
)

type loginPageResp struct {
	PasskeyAutofill bool   `json:"passkey_autofill"`
	AutoRedirect    string `json:"auto_redirect"`
	Buttons         map[string]struct {
		Order  int    `json:"order"`
		Hidden bool   `json:"hidden"`
		Label  string `json:"label"`
	} `json:"buttons"`
	Providers []struct {
		ID, Name, Kind string
		Hidden         bool
	} `json:"providers"`
}

type siteLogin struct {
	AuthProviders []struct{ ID, Name, Kind string } `json:"auth_providers"`
	Login         struct {
		PasskeyAutofill bool   `json:"passkey_autofill"`
		AutoRedirect    string `json:"auto_redirect"`
	} `json:"login"`
}

func redirectIDs(s siteLogin) []string {
	var out []string
	for _, p := range s.AuthProviders {
		if p.Kind == "redirect" {
			out = append(out, p.ID+"="+p.Name)
		}
	}
	return out
}

// TestLoginPageSettings: order, text and hiding of the provider buttons,
// automatic redirect, passkeys in the user name field; validation, rights,
// audit, no loss through the other settings, and the CLI over the API.
func TestLoginPageSettings(t *testing.T) {
	e := setup(t)
	staff, partner := newFakeOIDC(t), newFakeOIDC(t)
	e.setPluginSettings(t, "auth.oidc", map[string]any{"name": "Staff", "issuer": staff.srv.URL, "client_id": "wiki", "client_secret": "s3cret",
		"instances": []any{map[string]any{"id": "partner", "name": "Partner", "issuer": partner.srv.URL, "client_id": "wiki", "client_secret": "p4rtner"}}})
	admin := e.client()
	admin.login("admin", adminPW)

	// defaults: passkeys in the user name field, no redirect, by name
	var site siteLogin
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if !site.Login.PasskeyAutofill || site.Login.AutoRedirect != "" {
		t.Fatalf("default login %+v", site.Login)
	}
	if got := strings.Join(redirectIDs(site), ","); got != "oidc-partner=Partner,oidc=Staff" {
		t.Fatalf("default order %s", got)
	}

	// order and text; hidden buttons leave the list of the sign-in page
	var lp loginPageResp
	admin.must("PUT", "/api/v1/admin/auth/login", map[string]any{"passkey_autofill": false, "auto_redirect": "oidc",
		"buttons": map[string]any{"oidc": map[string]any{"order": -5, "label": "  Mitarbeitende  "}, "oidc-partner": map[string]any{"order": 3}}}, nil, 200)
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if got := strings.Join(redirectIDs(site), ","); got != "oidc=Mitarbeitende,oidc-partner=Partner" {
		t.Fatalf("order %s", got)
	}
	if site.Login.PasskeyAutofill || site.Login.AutoRedirect != "oidc" {
		t.Fatalf("login %+v", site.Login)
	}
	admin.must("PUT", "/api/v1/admin/auth/login", map[string]any{"buttons": map[string]any{"oidc-partner": map[string]any{"hidden": true}, "oidc": map[string]any{}}}, nil, 200)
	e.client().must("GET", "/api/v1/site", nil, &site, 200)
	if got := strings.Join(redirectIDs(site), ","); got != "oidc=Staff" || !site.Login.PasskeyAutofill || site.Login.AutoRedirect != "" {
		t.Fatalf("hidden %s %+v", got, site.Login)
	}
	// a hidden provider can still be used (direct link /login?via=…)
	if loc := partner.loginAt(t, e, e.client(), "oidc-partner"); loc != "/" {
		t.Fatalf("hidden provider sign-in: %s", loc)
	}
	admin.must("GET", "/api/v1/admin/auth/login", nil, &lp, 200)
	if _, ok := lp.Buttons["oidc"]; ok || !lp.Buttons["oidc-partner"].Hidden || len(lp.Providers) == 0 {
		t.Fatalf("stored %+v", lp)
	}
	if c := e.app.Config.Get().Login; c.PasskeyAutofill != nil {
		t.Fatalf("default autofill stored explicitly: %v", *c.PasskeyAutofill)
	}

	// validation
	for name, body := range map[string]map[string]any{
		"unknown redirect":   {"auto_redirect": "oidc-nope"},
		"password provider":  {"auto_redirect": "local"},
		"unknown button":     {"buttons": map[string]any{"ldap": map[string]any{"order": 1}}},
		"order out of range": {"buttons": map[string]any{"oidc": map[string]any{"order": 1001}}},
		"long label":         {"buttons": map[string]any{"oidc": map[string]any{"label": strings.Repeat("ä", 81)}}},
		"control character":  {"buttons": map[string]any{"oidc": map[string]any{"label": "a\u0007b"}}},
		"unknown field":      {"auto_redirect": "", "extra": 1},
	} {
		if st, out := admin.rawJSON("PUT", "/api/v1/admin/auth/login", body); st != 400 {
			t.Errorf("%s: %d %v", name, st, out)
		}
	}
	// rights: only administrators
	u := e.client()
	staff.login(t, e, u)
	if got := u.do("GET", "/api/v1/admin/auth/login", nil, nil); got != 403 {
		t.Fatalf("user reads: %d", got)
	}
	if got := u.do("PUT", "/api/v1/admin/auth/login", map[string]any{}, nil); got != 403 {
		t.Fatalf("user writes: %d", got)
	}
	if got := e.client().do("GET", "/api/v1/admin/auth/login", nil, nil); got != 401 {
		t.Fatalf("guest reads: %d", got)
	}
	// audit
	var n int
	if err := e.app.DB.QueryRow(t.Context(), `SELECT count(*) FROM audit_log WHERE action='auth.login_page.update'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("audit %d %v", n, err)
	}

	// the other settings do not touch the sign-in page
	var cur map[string]any
	admin.must("GET", "/api/v1/admin/settings", nil, &cur, 200)
	auth := cur["config"].(map[string]any)["auth"]
	admin.must("PUT", "/api/v1/admin/settings", map[string]any{"auth": auth}, nil, 200)
	admin.must("GET", "/api/v1/admin/auth/login", nil, &lp, 200)
	if !lp.Buttons["oidc-partner"].Hidden {
		t.Fatalf("settings save lost the sign-in page: %+v", lp)
	}

	// a provider that goes away keeps its entries, and saving still works
	e.setPluginSettings(t, "auth.oidc", map[string]any{"name": "Staff", "issuer": staff.srv.URL, "client_id": "wiki", "client_secret": "s3cret"})
	admin.must("GET", "/api/v1/admin/auth/login", nil, &lp, 200)
	raw, _ := json.Marshal(map[string]any{"passkey_autofill": false, "auto_redirect": "", "buttons": lp.Buttons})
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	admin.must("PUT", "/api/v1/admin/auth/login", body, nil, 200)
	if b, ok := e.app.Config.Get().Login.Buttons["oidc-partner"]; !ok || !b.Hidden {
		t.Fatalf("entry of a removed provider lost: %+v", e.app.Config.Get().Login)
	}
	// ... but it cannot be changed while the provider is missing
	if st, _ := admin.rawJSON("PUT", "/api/v1/admin/auth/login", map[string]any{"buttons": map[string]any{"oidc-partner": map[string]any{"order": 9}}}); st != 400 {
		t.Fatalf("change for a missing provider: %d", st)
	}

	// CLI over the API, with the rights of --as
	if code, out := runCLI(t, e.cfg, "login-page", "button", "oidc", "--order", "7", "--label", "Staff sign-in"); code != 0 {
		t.Fatalf("cli button: %d %s", code, out)
	}
	if code, out := runCLI(t, e.cfg, "login-page", "set", "--passkey-autofill", "on", "--auto-redirect", "oidc"); code != 0 {
		t.Fatalf("cli set: %d %s", code, out)
	}
	// the CLI works on its own instance of the configuration: read back through it
	show := func() loginPageResp {
		t.Helper()
		code, out := runCLI(t, e.cfg, "login-page", "show")
		var r loginPageResp
		if code != 0 || json.Unmarshal([]byte(out), &r) != nil {
			t.Fatalf("cli show: %d %s", code, out)
		}
		return r
	}
	r := show()
	if r.Buttons["oidc"].Order != 7 || r.Buttons["oidc"].Label != "Staff sign-in" || !r.PasskeyAutofill || r.AutoRedirect != "oidc" || !r.Buttons["oidc-partner"].Hidden {
		t.Fatalf("after cli %+v", r)
	}
	if code, _ := runCLI(t, e.cfg, "login-page", "button", "oidc", "--reset"); code != 0 {
		t.Fatalf("cli reset: %d", code)
	}
	if r := show(); len(r.Buttons) != 1 {
		t.Fatalf("after reset %+v", r.Buttons)
	}
	if code, _ := runCLI(t, e.cfg, "login-page", "set", "--passkey-autofill", "maybe"); code == 0 {
		t.Fatal("cli accepted a wrong value")
	}
}

// TestPasskeyAutofillLimit: the request of the user name field has its own
// limit per address, so it does not use up the requests of the button.
func TestPasskeyAutofillLimit(t *testing.T) {
	e := setupWith(t, func(c *config.Config) {
		if c.Limits == nil {
			c.Limits = map[string]int64{}
		}
		c.Limits[limits.PasskeyAutofill] = 10
	})
	c := e.client()
	for i := range 10 {
		c.must("POST", "/api/v1/auth/passwordless/webauthn/begin", map[string]any{"autofill": true}, nil, 200)
		_ = i
	}
	if got := c.do("POST", "/api/v1/auth/passwordless/webauthn/begin", map[string]any{"autofill": true}, nil); got != 429 {
		t.Fatalf("autofill beyond the limit: %d", got)
	}
	c.must("POST", "/api/v1/auth/passwordless/webauthn/begin", nil, nil, 200)
	if got := c.do("POST", "/api/v1/auth/passwordless/webauthn/begin", map[string]any{"autofill": "yes"}, nil); got != 400 {
		t.Fatalf("wrong body: %d", got)
	}
}
