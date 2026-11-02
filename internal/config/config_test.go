package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpand(t *testing.T) {
	t.Setenv("EW_A", "alpha")
	cases := map[string]string{
		"${EW_A}":            "alpha",
		"x-${EW_A}-y":        "x-alpha-y",
		"${EW_MISSING:-def}": "def",
		"${EW_MISSING}":      "",
		"plain":              "plain",
	}
	for in, want := range cases {
		if got := Expand(in); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStoreKeepsEnvReferences(t *testing.T) {
	t.Setenv("EW_DB", "postgres://u:secret@h/db")
	p := filepath.Join(t.TempDir(), "config.yaml")
	c := Defaults()
	c.Database.URL = "${EW_DB}"
	if err := WriteFile(p, c); err != nil {
		t.Fatal(err)
	}
	st, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Get().Database.URL != "postgres://u:secret@h/db" {
		t.Fatalf("effective url %q", st.Get().Database.URL)
	}
	if err := st.Update(func(c *Config) error { c.UI.SiteName = "X"; return nil }); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "secret") || !strings.Contains(string(b), "${EW_DB}") {
		t.Fatalf("secret written to config file:\n%s", b)
	}
}

func TestValidate(t *testing.T) {
	c := Defaults()
	c.I18n.DefaultLanguage = "fr"
	if Validate(c) == nil {
		t.Error("default language not in list accepted")
	}
	c = Defaults()
	c.Mode = "multi"
	if Validate(c) == nil {
		t.Error("invalid mode accepted")
	}
	c = Defaults()
	c.I18n.Languages = []string{"en", "EN"}
	if Validate(c) == nil {
		t.Error("invalid language code accepted")
	}
}

func TestSetGetKey(t *testing.T) {
	c := Defaults()
	if err := SetKey(c, "retention.trash_days", "60"); err != nil {
		t.Fatal(err)
	}
	if c.Retention.TrashDays != 60 {
		t.Fatal("not set")
	}
	if err := SetKey(c, "i18n.languages", "[en, de, fr]"); err != nil || len(c.I18n.Languages) != 3 {
		t.Fatal("list not set", err)
	}
	if err := SetKey(c, "nope.key", "1"); err == nil {
		t.Fatal("unknown key accepted")
	}
	if err := SetKey(c, "plugin_settings.my.plugin.flag", "true"); err != nil {
		t.Fatal(err)
	}
	if v, _ := GetKey(c, "retention.trash_days"); v != "60" {
		t.Fatalf("get %q", v)
	}
}

func TestDotEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("# c\nEW_X=1\nexport EW_Y=\"two words\"\n"), 0o600)
	os.Unsetenv("EW_X")
	os.Unsetenv("EW_Y")
	t.Cleanup(func() { os.Unsetenv("EW_X"); os.Unsetenv("EW_Y") })
	if err := LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("EW_X") != "1" || os.Getenv("EW_Y") != "two words" {
		t.Fatal("dotenv not loaded")
	}
}

func TestMigrateLegacyPluginIDs(t *testing.T) {
	c := Defaults()
	c.UI.Template = "theme.confluence.dark"
	c.Plugins.Disabled = []string{"theme.confluence", "example.notice"}
	c.Settings = map[string]map[string]any{"theme.confluence": {"x": 1}}
	migrateLegacy(c)
	if c.UI.Template != "theme.wiki.dark" || c.Plugins.Disabled[0] != "theme.wiki" || c.Plugins.Disabled[1] != "example.notice" || c.Settings["theme.wiki"]["x"] != 1 {
		t.Fatalf("migration: %+v %+v %+v", c.UI.Template, c.Plugins.Disabled, c.Settings)
	}
}
