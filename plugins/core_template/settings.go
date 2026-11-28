package core_template

import (
	"context"
	"encoding/json"
	"io/fs"
	"maps"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/storage"
	"github.com/mtsonline/enterprisewiki/internal/tplsettings"
)

// The settings of a template of data (Administration → Templates →
// Settings) are its design values: every colour of the light and the dark
// scheme, the fonts, the size of the text, the corners, the content width
// and the logo, each with the value of the design as its default. A design
// declares them by what it is; nobody writes a description. Besides the
// values of the design there are the logos a design has no token for: the
// mark for narrow screens and the logos for the dark colour scheme.
//
// The values are checked twice: each one by its description (tplsettings)
// and the design with all of them like a saved design (contrast of every
// pair of colours, room in every row, own fonts that exist). So the
// settings can never make a template of data less accessible than the
// designer allows.

// Keys of the settings that are no token of the design.
const (
	keyLogo       = "logo"
	keyMark       = "mark"
	keyLogoOnDark = "logo_on_dark"
	keyMarkOnDark = "mark_on_dark"
	darkPrefix    = "dark_"
)

// paletteKeys are the colours of a palette in the order of the form (the
// JSON names of Palette).
var paletteKeys = []string{"bg", "bg_subtle", "text", "text_muted", "border", "primary", "on_primary", "link", "focus", "header_bg", "header_text", "footer_bg", "footer_text"}

// paletteGet and paletteSet read and write a colour of a palette by its key.
func paletteGet(p *Palette, key string) string { return *paletteField(p, key) }
func paletteSet(p *Palette, key, v string)     { *paletteField(p, key) = v }

func paletteField(p *Palette, key string) *string {
	switch key {
	case "bg":
		return &p.Bg
	case "bg_subtle":
		return &p.BgSubtle
	case "text":
		return &p.Text
	case "text_muted":
		return &p.TextMuted
	case "border":
		return &p.Border
	case "primary":
		return &p.Primary
	case "on_primary":
		return &p.OnPrimary
	case "link":
		return &p.Link
	case "focus":
		return &p.Focus
	case "header_bg":
		return &p.HeaderBg
	case "header_text":
		return &p.HeaderText
	case "footer_bg":
		return &p.FooterBg
	case "footer_text":
		return &p.FooterText
	}
	panic("core_template: unknown colour " + key)
}

// texts are the texts of the plugin per language (locales/*.json), read
// once: the labels of the settings come from the same texts as the
// designer, so both always say the same.
var texts = func() map[string]map[string]string {
	out := map[string]map[string]string{}
	files, err := fs.Glob(locales, "locales/*.json")
	if err != nil {
		panic(err)
	}
	for _, f := range files {
		raw, err := locales.ReadFile(f)
		if err != nil {
			panic(err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(raw, &m); err != nil {
			panic("core_template: " + f + ": " + err.Error())
		}
		out[strings.TrimSuffix(strings.TrimPrefix(f, "locales/"), ".json")] = m
	}
	return out
}()

// text is a text of the plugin in every language; {name} in it is replaced
// by the text of inner in the same language.
func text(key, inner string) tplsettings.Texts {
	out := tplsettings.Texts{}
	for lang, m := range texts {
		v := m[key]
		if inner != "" {
			v = strings.ReplaceAll(v, "{name}", m[inner])
		}
		if v != "" {
			out[lang] = v
		}
	}
	return out
}

func num(v int) *float64 { f := float64(v); return &f }

// designSettings describes the settings of a design (see above).
func designSettings(d *Design) []tplsettings.Setting {
	t := d.Tokens
	var out []tplsettings.Setting
	// the text colours are checked against their backgrounds (the pairs of
	// the designer); the form warns, the check of the design refuses
	contrast := func(prefix, fore string) []string {
		var on []string
		for _, p := range ContrastPairs {
			if p.Fore == fore && !p.UI {
				on = append(on, prefix+p.Back)
			}
		}
		return on
	}
	colors := func(p *Palette, prefix string) {
		for _, k := range paletteKeys {
			label := text("template.d_color_"+k, "")
			if prefix != "" {
				label = text("template.s_dark_color", "template.d_color_"+k)
			}
			out = append(out, tplsettings.Setting{Key: prefix + k, Kind: tplsettings.KindColor, Group: "colors", Default: paletteGet(p, k),
				Label: label, ContrastOn: contrast(prefix, k)})
		}
	}
	colors(&t.Palette, "")
	if t.Dark != nil {
		colors(t.Dark, darkPrefix)
	}
	fonts := append([]string(nil), limits.Fonts...)
	optionLabels := map[string]tplsettings.Texts{}
	for _, f := range fonts {
		optionLabels[f] = text("template.d_font_"+f, "")
	}
	out = append(out,
		tplsettings.Setting{Key: "font", Kind: tplsettings.KindFont, Group: "fonts", Default: t.Font, Options: fonts, OptionLabels: optionLabels,
			Label: text("template.d_font", ""), Help: text("template.d_font_help", "")},
		tplsettings.Setting{Key: "heading_font", Kind: tplsettings.KindFont, Group: "fonts", Default: t.HeadingFont, Options: fonts, OptionLabels: optionLabels,
			Label: text("template.d_heading_font", "")},
		tplsettings.Setting{Key: "scale", Kind: tplsettings.KindNumber, Group: "fonts", Default: float64(t.Scale), Step: 1,
			Min: num(limits.ScalePercent[0]), Max: num(limits.ScalePercent[1]), Label: text("template.d_scale", "")},
		tplsettings.Setting{Key: "radius", Kind: tplsettings.KindNumber, Group: "layout", Default: float64(t.Radius), Step: 1,
			Min: num(limits.RadiusPx[0]), Max: num(limits.RadiusPx[1]), Label: text("template.d_radius", "")},
		tplsettings.Setting{Key: "content_width", Kind: tplsettings.KindNumber, Group: "layout", Default: float64(t.ContentWidth), Step: 1,
			Min: num(limits.ContentWidthRem[0]), Max: num(limits.ContentWidthRem[1]), Label: text("template.d_content_width", ""), Help: text("template.s_content_width_help", "")},
	)
	logo := tplsettings.Setting{Key: keyLogo, Kind: tplsettings.KindImage, Group: "logos", Role: tplsettings.RoleLogo,
		Label: text("template.s_logo", ""), Help: text("template.s_logo_help", "")}
	// the logo of the design is the default (a file of the wiki; the form
	// shows it while no other is chosen)
	if h, ok := logoHash(t.Logo); ok {
		logo.DefaultSrc = "/api/v1/files/" + h
	}
	out = append(out, logo,
		tplsettings.Setting{Key: keyMark, Kind: tplsettings.KindImage, Group: "logos", Role: tplsettings.RoleMark,
			Label: text("template.s_mark", ""), Help: text("template.s_mark_help", "")})
	if t.Dark != nil {
		out = append(out,
			tplsettings.Setting{Key: keyLogoOnDark, Kind: tplsettings.KindImage, Group: "logos", Role: tplsettings.RoleLogoOnDark,
				Label: text("template.s_logo_on_dark", ""), Help: text("template.s_on_dark_help", "")},
			tplsettings.Setting{Key: keyMarkOnDark, Kind: tplsettings.KindImage, Group: "logos", Role: tplsettings.RoleMarkOnDark,
				Label: text("template.s_mark_on_dark", ""), Help: text("template.s_on_dark_help", "")})
	}
	return out
}

// logoHash is the hash of the file of a logo of the design
// (/api/v1/files/HASH).
func logoHash(logo string) (string, bool) {
	h, ok := strings.CutPrefix(logo, "/api/v1/files/")
	return h, ok && storage.ValidHash(h)
}

// withValues returns a copy of the design with the effective values of its
// settings as its tokens (values not given keep the value of the design).
// The values were checked against designSettings before.
func withValues(d *Design, values map[string]any) *Design {
	c := *d
	c.Tokens = d.Tokens
	if d.Tokens.Dark != nil {
		dark := *d.Tokens.Dark
		c.Tokens.Dark = &dark
	}
	for _, k := range paletteKeys {
		if v, ok := values[k].(string); ok {
			paletteSet(&c.Tokens.Palette, k, v)
		}
		if v, ok := values[darkPrefix+k].(string); ok && c.Tokens.Dark != nil {
			paletteSet(c.Tokens.Dark, k, v)
		}
	}
	if v, ok := values["font"].(string); ok {
		c.Tokens.Font = v
	}
	if v, ok := values["heading_font"].(string); ok {
		c.Tokens.HeadingFont = v
	}
	for k, dst := range map[string]*int{"scale": &c.Tokens.Scale, "radius": &c.Tokens.Radius, "content_width": &c.Tokens.ContentWidth} {
		if v, ok := values[k].(float64); ok {
			*dst = int(v)
		}
	}
	if img, ok := imageOf(values[keyLogo]); ok {
		c.Tokens.Logo = "/api/v1/files/" + img.Hash
	}
	return &c
}

// imageOf reads an image value (checked before: tplsettings.Image).
func imageOf(v any) (tplsettings.Image, bool) {
	img, ok := v.(tplsettings.Image)
	return img, ok && storage.ValidHash(img.Hash)
}

// TemplateSettings implements content.TemplateSettingsSource.
func (p *Plugin) TemplateSettings(ctx context.Context, id string) ([]tplsettings.Setting, func(context.Context, map[string]any) error, bool) {
	if !strings.HasPrefix(id, ID+"/") {
		return nil, nil, false
	}
	t, err := p.get(ctx, id, false)
	if err != nil || len(t.Design) == 0 {
		return nil, nil, false
	}
	var d Design
	if json.Unmarshal(t.Design, &d) != nil {
		return nil, nil, false
	}
	check := func(ctx context.Context, values map[string]any) error {
		c := withValues(&d, values)
		if ps := c.Problems(); len(ps) > 0 {
			return apperr.BadRequest("%s", ps[0].Error())
		}
		ps, err := p.fontProblems(ctx, c)
		if err != nil {
			return err
		}
		if len(ps) > 0 {
			return apperr.BadRequest("%s", ps[0].Error())
		}
		return nil
	}
	return designSettings(&d), check, true
}

var _ content.TemplateSettingsSource = (*Plugin)(nil)

// copySettings is what a copy of the template id takes from its settings:
// the design with the effective values as its tokens (they become the
// defaults of the copy) and the stored values that are no token (the mark,
// the logos for dark colours), which the copy keeps as its own settings.
func (p *Plugin) copySettings(id string, raw json.RawMessage) (json.RawMessage, map[string]any, error) {
	stored := p.host.Config.Get().TemplateSettings[id]
	if len(stored) == 0 {
		return raw, nil, nil
	}
	var d Design
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, nil, err
	}
	desc := designSettings(&d)
	c := withValues(&d, tplsettings.Effective(desc, stored))
	norm, err := json.Marshal(c)
	if err != nil {
		return nil, nil, err
	}
	var rest map[string]any
	for _, k := range []string{keyMark, keyLogoOnDark, keyMarkOnDark} {
		if v, ok := stored[k]; ok {
			if rest == nil {
				rest = map[string]any{}
			}
			rest[k] = v
		}
	}
	return norm, rest, nil
}

// setStoredSettings stores the settings of a template (nil: none, e.g.
// when it is deleted for good). The settings live in the configuration,
// not in the database, so this follows the transaction of the template; a
// failure leaves only values nobody uses, which the next copy or purge
// does not need.
func (p *Plugin) setStoredSettings(id string, values map[string]any) error {
	return p.host.Config.Update(func(c *config.Config) error {
		if len(values) == 0 {
			if _, ok := c.TemplateSettings[id]; !ok {
				return nil
			}
			delete(c.TemplateSettings, id)
			if len(c.TemplateSettings) == 0 {
				c.TemplateSettings = nil
			}
			return nil
		}
		if c.TemplateSettings == nil {
			c.TemplateSettings = map[string]map[string]any{}
		}
		c.TemplateSettings[id] = maps.Clone(values)
		return nil
	})
}

// checkLogo makes sure the logo of a design is an uploaded PNG, WebP or
// JPEG file: no SVG or other format (like every image of the settings).
func (p *Plugin) checkLogo(ctx context.Context, d *Design) error {
	if d.Tokens.Logo == "" {
		return nil
	}
	h, ok := logoHash(d.Tokens.Logo)
	if !ok {
		return apperr.BadRequest("tokens.logo: an uploaded file of the wiki (/api/v1/files/…)")
	}
	format, _, _, err := p.host.Files.ImageInfo(ctx, h)
	if err != nil {
		return apperr.BadRequest("tokens.logo: the file does not exist")
	}
	switch format {
	case "png", "webp", "jpeg":
		return nil
	}
	return apperr.BadRequest("tokens.logo: only PNG, WebP or JPEG images, not %q", format)
}
