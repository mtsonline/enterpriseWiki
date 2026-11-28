package tplsettings

import (
	"errors"
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

var desc = []Setting{
	{Key: "primary", Kind: KindColor, Group: "colors", Default: "#0a3760", ContrastOn: []string{"#ffffff"}, Label: Texts{"en": "Primary colour"}},
	{Key: "body_font", Kind: KindFont, Group: "fonts", Default: "inter", Options: []string{"inter", "serif"}, CSSVars: []string{"--ew-font"}, Label: Texts{"en": "Text"}},
	{Key: "bg_dark", Kind: KindColor, Group: "colors", Default: "#1d2125", Scheme: "dark", CSSVars: []string{"--ew-bg"}, Label: Texts{"en": "Background (dark)"}},
	{Key: "primary_light", Kind: KindColor, Group: "colors", Default: "#0b5cad", Scheme: "light", CSSVars: []string{"--ew-primary", "--ew-link"}, Label: Texts{"en": "Primary (light)"}},
	{Key: "sidebar", Kind: KindNumber, Group: "layout", Default: 248.0, Min: f(160), Max: f(400), Unit: "px", CSSVars: []string{"--ew-sidebar-w"}, Label: Texts{"en": "Sidebar"}},
	{Key: "logo", Kind: KindImage, Group: "logos", Role: RoleLogo, Label: Texts{"en": "Logo"}},
	{Key: "logo_height", Kind: KindNumber, Group: "logos", Default: 44.0, Min: f(24), Max: f(120), Label: Texts{"en": "Height"}},
	{Key: "header", Kind: KindSelect, Group: "header", Default: "solid", Options: []string{"solid", "overlay"}, Label: Texts{"en": "Header"}},
	{Key: "back_to_top", Kind: KindBool, Group: "layout", Default: true, Label: Texts{"en": "Back to top"}},
	{Key: "copyright", Kind: KindText, Group: "footer", Default: "", MaxLen: 20, Label: Texts{"en": "Copyright"}},
	{Key: "hero_link", Kind: KindLink, Group: "content", Default: "", Label: Texts{"en": "Link"}},
}

const png = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const svg = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const huge = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

// files: the uploaded files the checks see
func files(hash string) (string, int, int, error) {
	switch hash {
	case png:
		return "png", 300, 120, nil
	case svg:
		return "svg", 300, 120, nil
	case huge:
		return "png", 10000, 10000, nil
	}
	return "", 0, 0, errors.New("no file")
}

func TestDescription(t *testing.T) {
	if err := CheckDescription(desc); err != nil {
		t.Fatal(err)
	}
	bad := map[string]Setting{
		"key":           {Key: "Bad-Key", Kind: KindBool, Group: "layout", Default: true, Label: Texts{"en": "x"}},
		"kind":          {Key: "x", Kind: "script", Group: "layout", Default: true, Label: Texts{"en": "x"}},
		"group":         {Key: "x", Kind: KindBool, Group: "elsewhere", Default: true, Label: Texts{"en": "x"}},
		"role on text":  {Key: "x", Kind: KindText, Group: "logos", Role: RoleLogo, Default: "", Label: Texts{"en": "x"}},
		"no label":      {Key: "x", Kind: KindBool, Group: "layout", Default: true},
		"no default":    {Key: "x", Kind: KindBool, Group: "layout", Label: Texts{"en": "x"}},
		"bad default":   {Key: "x", Kind: KindColor, Group: "colors", Default: "red", Label: Texts{"en": "x"}},
		"image default": {Key: "x", Kind: KindImage, Group: "logos", Default: png, Label: Texts{"en": "x"}},
		"css on text":   {Key: "x", Kind: KindText, Group: "layout", Default: "", CSSVars: []string{"--x"}, Label: Texts{"en": "x"}},
		"css var name":  {Key: "x", Kind: KindColor, Group: "colors", Default: "#000000", CSSVars: []string{"color: red"}, Label: Texts{"en": "x"}},
		"unit":          {Key: "x", Kind: KindNumber, Group: "layout", Default: 1.0, Min: f(0), Max: f(2), Unit: "vw;x", Label: Texts{"en": "x"}},
		"font option":   {Key: "x", Kind: KindFont, Group: "fonts", Default: "comic", Options: []string{"comic"}, Label: Texts{"en": "x"}},
		"font no opts":  {Key: "x", Kind: KindFont, Group: "fonts", Default: "system", Label: Texts{"en": "x"}},
		"default src":   {Key: "x", Kind: KindImage, Group: "logos", DefaultSrc: "javascript:alert(1).png", Label: Texts{"en": "x"}},
		"scheme":        {Key: "x", Kind: KindColor, Group: "colors", Default: "#000000", Scheme: "dim", Label: Texts{"en": "x"}},
		"number limits": {Key: "x", Kind: KindNumber, Group: "layout", Default: 1.0, Min: f(2), Max: f(1), Label: Texts{"en": "x"}},
		"select empty":  {Key: "x", Kind: KindSelect, Group: "layout", Default: "a", Label: Texts{"en": "x"}},
		"contrast":      {Key: "x", Kind: KindColor, Group: "colors", Default: "#000000", ContrastOn: []string{"nothing"}, Label: Texts{"en": "x"}},
	}
	for name, s := range bad {
		if CheckDescription([]Setting{s}) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if CheckDescription([]Setting{desc[0], desc[0]}) == nil {
		t.Error("a key declared twice was accepted")
	}
}

func TestCheckValues(t *testing.T) {
	ok := map[string]any{
		"primary": "#AABBCC", "body_font": "own-12", "logo": png, "logo_height": 60.0,
		"header": "overlay", "back_to_top": false, "copyright": "  © Example  ", "hero_link": "https://example.com/x",
	}
	got, err := Check(desc, ok, files)
	if err != nil {
		t.Fatal(err)
	}
	if got["primary"] != "#aabbcc" || got["copyright"] != "© Example" {
		t.Errorf("not normalised: %v", got)
	}
	if img, _ := got["logo"].(Image); img != (Image{Hash: png, Width: 300, Height: 120}) {
		t.Errorf("image: %#v", got["logo"])
	}
	bad := map[string]any{
		"primary":     "red; background:url(x)",
		"body_font":   "x'; } body { display:none",
		"sidebar":     100.0,
		"logo_height": 500.0,
		"header":      "fixed",
		"back_to_top": "yes",
		"copyright":   "far too long for twenty characters",
		"unknown":     "x",
	}
	for k, v := range bad {
		if _, err := Check(desc, map[string]any{k: v}, files); err == nil {
			t.Errorf("%s=%v accepted", k, v)
		}
	}
	if _, err := Check(desc, map[string]any{"copyright": "a\u0000b"}, files); err == nil {
		t.Error("control character accepted")
	}
	// links: only paths in the wiki and https://
	for _, l := range []string{"javascript:alert(1)", "//evil.example.com", "http://example.com", "data:text/html,x", "https://", "/x y", `/x"onmouseover="a`, "https:///x"} {
		if _, err := Check(desc, map[string]any{"hero_link": l}, files); err == nil {
			t.Errorf("link %q accepted", l)
		}
	}
	for _, l := range []string{"", "/studium", "/e/12?x=1#a", "https://example.com"} {
		if _, err := Check(desc, map[string]any{"hero_link": l}, files); err != nil {
			t.Errorf("link %q refused: %v", l, err)
		}
	}
	// images: only PNG, WebP and JPEG of uploaded files, not too large; a
	// size sent along counts for nothing
	for _, v := range []any{svg, huge, "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "../etc/passwd", map[string]any{"hash": svg, "width": 1, "height": 1}} {
		if _, err := Check(desc, map[string]any{"logo": v}, files); err == nil {
			t.Errorf("image %v accepted", v)
		}
	}
	got, err = Check(desc, map[string]any{"logo": map[string]any{"hash": png, "width": 1, "height": 1}}, files)
	if err != nil || got["logo"].(Image).Width != 300 {
		t.Errorf("the size of the file must count: %v %v", got, err)
	}
	// nil resets
	got, _ = Check(desc, map[string]any{"primary": nil}, files)
	if _, has := got["primary"]; has {
		t.Error("nil must reset")
	}
	if _, err := Check(desc, map[string]any{"unknown": 1}, files); !errors.Is(err, ErrUnknown) {
		t.Errorf("unknown: %v", err)
	}
}

func TestEffective(t *testing.T) {
	stored := map[string]any{
		"primary":     "#112233",
		"logo":        map[string]any{"hash": png, "width": 300, "height": 120},
		"logo_height": 999.0,     // no longer within the limits: the default counts
		"removed":     "#000000", // no longer declared: left out
	}
	got := Effective(desc, stored)
	if got["primary"] != "#112233" || got["logo_height"] != 44.0 || got["header"] != "solid" {
		t.Errorf("effective: %v", got)
	}
	if _, has := got["removed"]; has {
		t.Error("an undeclared value was given to the template")
	}
	if img, _ := got["logo"].(Image); img.Hash != png {
		t.Errorf("stored image: %#v", got["logo"])
	}
	if _, has := Effective(desc, nil)["logo"]; has {
		t.Error("an image without a value has no effective value")
	}
	if !strings.Contains(strings.Join(Groups, ","), "logos") {
		t.Error("groups")
	}
}

func TestLook(t *testing.T) {
	// nothing changed: the template keeps its own look
	if l := LookOf(desc, map[string]any{"body_font": "inter", "sidebar": 248.0}); len(l.All)+len(l.Light)+len(l.Dark)+len(l.Fonts) != 0 {
		t.Fatalf("defaults written: %+v", l)
	}
	l := LookOf(desc, map[string]any{
		"body_font": "own-7", "bg_dark": "#101010", "primary_light": "#7A1F1F", "sidebar": 300.0,
		"primary": "#123456", // no css_vars: the template uses it itself
		"bad":     "x",
	})
	if l.All["--ew-font"] != `"ewd-own-7", `+FontStacks["system"] || l.All["--ew-sidebar-w"] != "300px" {
		t.Errorf("all: %v", l.All)
	}
	if l.Dark["--ew-bg"] != "#101010" || len(l.Dark) != 1 {
		t.Errorf("dark: %v", l.Dark)
	}
	if l.Light["--ew-primary"] != "#7a1f1f" || l.Light["--ew-link"] != "#7a1f1f" || l.Light["--ew-primary-hover"] != "color-mix(in srgb, #7a1f1f 82%, black)" {
		t.Errorf("light: %v", l.Light)
	}
	if len(l.Fonts) != 1 || l.Fonts[0] != "7" {
		t.Errorf("fonts: %v", l.Fonts)
	}
	// a stored value that no longer passes the checks is not written
	if l := LookOf(desc, map[string]any{"bg_dark": "red;}", "sidebar": 9999.0, "body_font": "Comic Sans"}); len(l.All)+len(l.Dark) != 0 {
		t.Errorf("unchecked value written: %+v", l)
	}
}

// The colour scheme by default is added by the core: a template cannot
// declare it, and its value is used only for templates with dark colours.
func TestScheme(t *testing.T) {
	if CheckDescription([]Setting{SchemeSetting("system")}) == nil {
		t.Fatal("a template declared the colour scheme itself")
	}
	cases := []struct {
		def    string
		stored map[string]any
		want   string
	}{
		{"system", nil, "system"},
		{"system", map[string]any{KeyColorScheme: "dark"}, "dark"},
		{"dark", map[string]any{KeyColorScheme: "purple"}, "dark"},
		{"dark", map[string]any{KeyColorScheme: 1}, "dark"},
		{"", map[string]any{KeyColorScheme: "dark"}, ""},
	}
	for _, c := range cases {
		if got := Scheme(c.def, c.stored); got != c.want {
			t.Errorf("Scheme(%q, %v) = %q, want %q", c.def, c.stored, got, c.want)
		}
	}
	s := SchemeSetting("light")
	if _, err := Check([]Setting{s}, map[string]any{KeyColorScheme: "dark"}, nil); err != nil {
		t.Fatalf("a valid scheme: %v", err)
	}
	if _, err := Check([]Setting{s}, map[string]any{KeyColorScheme: "sepia"}, nil); err == nil {
		t.Fatal("an unknown scheme was accepted")
	}
}
