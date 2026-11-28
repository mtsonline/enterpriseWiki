package core_template

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/tplsettings"
)

// The settings of a design are a valid description, their defaults are
// the design itself, and every label is there in English and German.
func TestDesignSettingsDescribeTheDesign(t *testing.T) {
	for _, n := range []string{"wiki", "cms"} {
		d := example(t, n)
		desc := designSettings(d)
		if err := tplsettings.CheckDescription(desc); err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		for _, s := range desc {
			if s.Label["en"] == "" || s.Label["de"] == "" || strings.Contains(s.Label["en"], "{name}") {
				t.Errorf("%s: %s has no label in every language: %v", n, s.Key, s.Label)
			}
		}
		same := withValues(d, tplsettings.Effective(desc, nil))
		if !reflect.DeepEqual(same, d) {
			t.Errorf("%s: the defaults change the design:\n%+v\n%+v", n, same.Tokens, d.Tokens)
		}
		dark := false
		for _, s := range desc {
			dark = dark || strings.HasPrefix(s.Key, darkPrefix)
		}
		if dark != (d.Tokens.Dark != nil) {
			t.Errorf("%s: dark colours offered %v, the design has them %v", n, dark, d.Tokens.Dark != nil)
		}
	}
}

// The values of the settings become the tokens; the original stays as it
// is, and a design with too little contrast is refused by its check.
func TestDesignWithValues(t *testing.T) {
	d := example(t, "wiki")
	before := d.Tokens
	c := withValues(d, map[string]any{"primary": "#123456", "scale": float64(110), "font": "serif",
		"logo": tplsettings.Image{Hash: strings.Repeat("a", 64), Width: 10, Height: 5}})
	if c.Tokens.Primary != "#123456" || c.Tokens.Scale != 110 || c.Tokens.Font != "serif" || c.Tokens.Logo != "/api/v1/files/"+strings.Repeat("a", 64) {
		t.Fatalf("values not applied: %+v", c.Tokens)
	}
	if !reflect.DeepEqual(d.Tokens, before) {
		t.Fatal("the original design was changed")
	}
	bad := withValues(d, map[string]any{"text": d.Tokens.Bg})
	if len(bad.Problems()) == 0 {
		t.Fatal("text in the colour of the background passed the check")
	}
}

// A design with dark colours offers them and the logos for dark colours;
// their values go into the dark palette only.
func TestDesignSettingsDark(t *testing.T) {
	d := example(t, "wiki")
	dark := d.Tokens.Palette
	d.Tokens.Dark = &dark
	desc := designSettings(d)
	if err := tplsettings.CheckDescription(desc); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, s := range desc {
		keys[s.Key] = true
	}
	for _, k := range []string{"dark_primary", "dark_footer_text", keyLogoOnDark, keyMarkOnDark} {
		if !keys[k] {
			t.Errorf("%s is missing", k)
		}
	}
	c := withValues(d, map[string]any{"dark_primary": "#abcdef"})
	if c.Tokens.Dark.Primary != "#abcdef" || c.Tokens.Primary == "#abcdef" || d.Tokens.Dark.Primary == "#abcdef" {
		t.Fatalf("dark value in the wrong place: light %s dark %s", c.Tokens.Primary, c.Tokens.Dark.Primary)
	}
}
