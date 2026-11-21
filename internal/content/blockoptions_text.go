package content

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

// The options of a block in the content are written as the text value of
// its parameter "options", not as JSON: web application firewalls with the
// OWASP Core Rule Set take JSON inside a text field for an attack (CRS 4,
// rule 942550; CRS 3, 932105) and block the editor. The text form is
//
//	hide:mobile,tablet padding_top.desktop:4 padding_top.mobile:2 bg_color:%23ffffff
//
// pairs id:value separated by spaces; a value per view as id.view:value;
// lists separated by commas; every character outside [A-Za-z0-9._~/+#:-]
// as %XX (so no spaces, quotes, braces or separators of shells). The types
// come from the schema (blockoptions.json); the browser has the same code
// (encodeOptions/decodeOptions in core/blockoptions.ts, shared cases in
// testdata/blockoptions_text_cases.json).

func optionTextKeep(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("._~/+#:-", c) >= 0
}

func optionTextEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; optionTextKeep(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func optionTextUnescape(s string) (string, error) {
	if !strings.Contains(s, "%") {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", fmt.Errorf("broken %%XX")
		}
		n, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			return "", fmt.Errorf("broken %%XX")
		}
		b.WriteByte(byte(n))
		i += 2
	}
	return b.String(), nil
}

func optionTextValue(v any) string {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case string:
		return optionTextEscape(x)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = optionTextValue(e)
		}
		return strings.Join(parts, ",")
	case []string:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = optionTextEscape(e)
		}
		return strings.Join(parts, ",")
	}
	return optionTextEscape(fmt.Sprint(v))
}

// EncodeBlockOptions writes options (as checked by CheckBlockOptions) in
// the text form; ids in the order of the schema, views in their order.
func EncodeBlockOptions(o map[string]any) string {
	var parts []string
	for _, g := range blockOptions.Groups {
		for _, def := range g.Options {
			v, ok := o[def.ID]
			if !ok || v == nil {
				continue
			}
			if m, ok := v.(map[string]any); ok {
				for _, view := range blockOptions.Views {
					if x, ok := m[view]; ok && x != nil {
						parts = append(parts, def.ID+"."+view+":"+optionTextValue(x))
					}
				}
				continue
			}
			if l, ok := v.([]any); ok && len(l) == 0 {
				continue
			}
			parts = append(parts, def.ID+":"+optionTextValue(v))
		}
	}
	return strings.Join(parts, " ")
}

// DecodeBlockOptions reads the text form into options for
// CheckBlockOptions: unknown ids and views, a doubled id and values of the
// wrong type are refused.
func DecodeBlockOptions(s string) (map[string]any, error) {
	if len(s) > blockOptions.MaxBytes*3 {
		return nil, apperr.BadRequest("options: too long")
	}
	o := map[string]any{}
	for _, tok := range strings.Fields(s) {
		key, raw, ok := strings.Cut(tok, ":")
		if !ok {
			return nil, apperr.BadRequest("options: %q is not id:value", tok)
		}
		id, view, perView := strings.Cut(key, ".")
		def, known := blockOptions.byID[id]
		if !known {
			return nil, apperr.BadRequest("options: unknown option %q", id)
		}
		if perView && (!def.Responsive || !slices.Contains(blockOptions.Views, view)) {
			return nil, apperr.BadRequest("options: %q has no value per view %q", id, view)
		}
		var v any
		switch def.Type {
		case "bool":
			if raw != "true" && raw != "false" {
				return nil, apperr.BadRequest("options: %s: true or false", id)
			}
			v = raw == "true"
		case "int", "number", "space":
			n, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return nil, apperr.BadRequest("options: %s: a number", id)
			}
			v = n
		case "views", "langs":
			var list []any
			if raw != "" {
				for _, p := range strings.Split(raw, ",") {
					x, err := optionTextUnescape(p)
					if err != nil {
						return nil, apperr.BadRequest("options: %s: %v", id, err)
					}
					list = append(list, x)
				}
			}
			v = list
		default:
			x, err := optionTextUnescape(raw)
			if err != nil {
				return nil, apperr.BadRequest("options: %s: %v", id, err)
			}
			v = x
		}
		prev, had := o[id]
		switch {
		case !perView && had:
			return nil, apperr.BadRequest("options: %q twice", id)
		case perView:
			m, isMap := prev.(map[string]any)
			if had && !isMap {
				return nil, apperr.BadRequest("options: %q twice", id)
			}
			if m == nil {
				m = map[string]any{}
			}
			if _, dup := m[view]; dup {
				return nil, apperr.BadRequest("options: %q twice", key)
			}
			m[view] = v
			o[id] = m
		default:
			o[id] = v
		}
	}
	return o, nil
}
