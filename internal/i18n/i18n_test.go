package i18n

import (
	"slices"
	"strings"
	"testing"
)

func TestPluralForms(t *testing.T) {
	cases := map[string][]string{
		"en": {"one", "other"},
		"de": {"one", "other"},
		"pl": {"one", "few", "many", "other"},
		"ar": {"zero", "one", "two", "few", "many", "other"},
		"ja": {"other"},
	}
	for lang, want := range cases {
		if got := PluralForms(lang); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", lang, got, want)
		}
	}
	for _, c := range []struct{ lang, n, form string }{
		{"en", "1", "one"}, {"en", "0", "other"}, {"en", "1.5", "other"}, {"de", "2", "other"},
		{"pl", "1", "one"}, {"pl", "3", "few"}, {"pl", "5", "many"}, {"pl", "22", "few"}, {"pl", "1.5", "other"},
		{"fr", "0", "one"}, {"fr", "1", "one"}, {"fr", "2", "other"},
	} {
		if got := PluralForm(c.lang, c.n); got != c.form {
			t.Errorf("%s %s: %s, want %s", c.lang, c.n, got, c.form)
		}
	}
}

func TestFormat(t *testing.T) {
	msgs := map[string]string{
		"x_one": "{n} file", "x_other": "{n} files", "x_few": "{count} pliki",
		"plain": "Hello {name}",
	}
	for _, c := range []struct {
		lang, key string
		params    map[string]string
		want      string
	}{
		{"en", "x", map[string]string{"n": "1"}, "1 file"},
		{"en", "x", map[string]string{"n": "7"}, "7 files"},
		{"pl", "x", map[string]string{"count": "3"}, "3 pliki"},
		{"pl", "x", map[string]string{"n": "5"}, "5 files"}, // "many" missing: falls back to "other"
		{"en", "x", nil, "{n} files"},
		{"en", "plain", map[string]string{"name": "Ada"}, "Hello Ada"},
		{"en", "missing", nil, "missing"},
	} {
		if got := Format(msgs, c.lang, c.key, c.params); got != c.want {
			t.Errorf("%s %s %v: %q, want %q", c.lang, c.key, c.params, got, c.want)
		}
	}
}

func TestExchangeFormats(t *testing.T) {
	es := []entry{
		{key: "a.b", source: "core", ref: "Hello \"you\"\nline 2", value: "Hallo „du“\nZeile 2", status: "custom"},
		{key: "a.n_one", source: "core", ref: "{n} file", value: "{n} Datei", form: "one", status: "file"},
		{key: "a.n_other", source: "core", ref: "{n} files", value: "", form: "other", status: "missing"},
		{key: "p.x", source: "acme.kanban", ref: "Board", value: "Tafel", status: "outdated"},
	}
	po := exportPO("de", es)
	if !strings.Contains(string(po), "#, fuzzy") || !strings.Contains(string(po), "plural form: one (e.g. 1)") {
		t.Fatalf("po:\n%s", po)
	}
	got, err := parsePO(po)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a.b": "Hallo „du“\nZeile 2", "a.n_one": "{n} Datei"} // missing and fuzzy skipped
	if len(got) != len(want) || got["a.b"] != want["a.b"] || got["a.n_one"] != want["a.n_one"] {
		t.Fatalf("po import: %#v", got)
	}

	x, err := exportXLIFF("de", es)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(x), `trgLang="de"`) || !strings.Contains(string(x), `<file id="acme.kanban">`) {
		t.Fatalf("xliff:\n%s", x)
	}
	got, err = parseXLIFF("de", x)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["a.b"] != want["a.b"] || got["a.n_one"] != "{n} Datei" {
		t.Fatalf("xliff import: %#v", got)
	}
	if _, err := parseXLIFF("fr", x); err == nil {
		t.Fatal("xliff for another language accepted")
	}
	for in, f := range map[string]string{`{"a":"b"}`: FormatJSON, "<?xml": FormatXLIFF, "msgid \"\"": FormatPO, "\xef\xbb\xbf{}": FormatJSON} {
		if DetectFormat([]byte(in)) != f {
			t.Errorf("detect %q", in)
		}
	}
}

func TestPOWithoutBlankLines(t *testing.T) {
	src := `msgid ""
msgstr "Language: de\n"
#, fuzzy
msgctxt "k1"
msgid "One"
msgstr "Eins"
msgctxt "k2"
msgid "Two"
msgstr ""
"Zw"
"ei"
msgctxt "k3"
msgid "x"
msgid_plural "xs"
msgstr[0] "a"
msgstr[1] "b"
`
	got, err := parsePO([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["k2"] != "Zwei" {
		t.Fatalf("%#v", got)
	}
}
