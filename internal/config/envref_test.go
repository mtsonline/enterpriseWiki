package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A value the wiki writes (API, interface, plugins) must not read the
// environment of the server; references from the operator stay.
func TestUpdateRefusesNewEnvRefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
database:
  url: ${EWIKI_DATABASE_URL}
mail:
  smtp:
    password: ${SMTP_PASSWORD}
plugins:
  trusted_sources:
    - prefix: https://github.com/acme
      note: ${ACME_TOKEN}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACME_TOKEN", "acme-secret")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var refused *EnvRefError
	for name, fn := range map[string]func(c *Config){
		"site name":      func(c *Config) { c.UI.SiteName = "${EWIKI_DATABASE_URL}" },
		"smtp password":  func(c *Config) { c.Mail.SMTP.Password = "${OTHER_SECRET}" },
		"plugin setting": func(c *Config) { c.Settings = map[string]map[string]any{"x.y": {"key": "a${HOME}b"}} },
		"nested setting": func(c *Config) {
			c.Settings = map[string]map[string]any{"x.y": {"list": []any{map[string]any{"k": "${HOME}"}}}}
		},
		"copied in a list": func(c *Config) {
			c.Plugins.TrustedSources = append(c.Plugins.TrustedSources, TrustedSource{Prefix: "https://evil.example/x", Note: "${ACME_TOKEN}"})
		},
		"moved reference": func(c *Config) { c.UI.SiteName = "${SMTP_PASSWORD}" },
		"default value":   func(c *Config) { c.UI.SiteName = "${NOT_SET:-x}" },
	} {
		err := st.Update(func(c *Config) error { fn(c); return nil })
		if !errors.As(err, &refused) || !refused.ClientError() {
			t.Errorf("%s: %v", name, err)
		}
	}
	// unchanged references (a form saved again) and a reordered list stay
	if err := st.Update(func(c *Config) error {
		c.UI.SiteName = "Wiki"
		c.Plugins.TrustedSources = append([]TrustedSource{{Prefix: "https://git.example.com/team", Note: "plain"}}, c.Plugins.TrustedSources...)
		return nil
	}); err != nil {
		t.Fatalf("unchanged references: %v", err)
	}
	if got := st.Get().Plugins.TrustedSources[1].Note; got != "acme-secret" {
		t.Fatalf("reference not expanded: %q", got)
	}
	// the operator on the server may write references
	if err := st.UpdateTrusted(func(c *Config) error { c.Mail.SMTP.Password = "${OTHER_SECRET}"; return nil }); err != nil {
		t.Fatalf("operator: %v", err)
	}
	// text without a reference is fine, also with a dollar sign
	if err := st.Update(func(c *Config) error { c.UI.SiteName = "Costs in $ {x} and $HOME"; return nil }); err != nil {
		t.Fatalf("plain text: %v", err)
	}
}
