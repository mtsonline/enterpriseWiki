package content_forms

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

// Render implements content.ElementType: an accessible HTML form (labels,
// help and error texts linked with aria-describedby, groups as fieldset,
// required marked in text). The widget "form" adds conditions, checks,
// uploads by drag and drop and the submission; without it nothing is sent.
func (Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	f, err := Decode(raw)
	if err != nil {
		return "", err
	}
	tr := func(key string) string {
		if gh := hostOf(ctx); gh != nil {
			return gh.T(ctx, rc.Lang, key, nil)
		}
		return key
	}
	var b strings.Builder
	pre := fmt.Sprintf("f%d-", rc.ElementID)
	fmt.Fprintf(&b, `<div class="ewfm" data-ew-widget="form" data-element="%d" data-layout="%s">`, rc.ElementID, esc(f.Layout))
	for _, p := range regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(f.Intro), -1) {
		if p = strings.TrimSpace(p); p != "" {
			b.WriteString(`<p class="ewfm-intro">` + strings.ReplaceAll(esc(p), "\n", "<br>") + `</p>`)
		}
	}
	style := ""
	if f.Layout == "free" && f.Height > 0 {
		style = fmt.Sprintf(` style="--ewfm-h:%dpx"`, f.Height)
	}
	fmt.Fprintf(&b, `<form class="ewfm-form ewfm-form--%s" novalidate%s>`, esc(f.Layout), style)
	fmt.Fprintf(&b, `<div class="ewfm-summary" role="alert" tabindex="-1" hidden></div>`)
	hasRequired := false
	for _, x := range f.Fields {
		hasRequired = hasRequired || x.Required
	}
	if hasRequired {
		b.WriteString(`<p class="ewfm-note">` + esc(tr("forms.required_note")) + `</p>`)
	}
	b.WriteString(`<div class="ewfm-fields">`)
	for i := range f.Fields {
		renderField(&b, &f.Fields[i], pre, f.Layout, tr)
	}
	b.WriteString(`</div>`)
	// hidden field for bots (not reachable with the keyboard or screen readers)
	fmt.Fprintf(&b, `<div class="ewfm-hp" aria-hidden="true"><label for="%swebsite">Website</label><input id="%swebsite" name="website" type="text" tabindex="-1" autocomplete="off"></div>`, pre, pre)
	label := f.Submit
	if label == "" {
		label = tr("forms.submit")
	}
	fmt.Fprintf(&b, `<div class="ewfm-actions"><button type="submit" class="ew-btn ew-btn--primary" disabled>%s</button></div>`, esc(label))
	b.WriteString(`<div class="ewfm-status" role="status" aria-live="polite"></div>`)
	b.WriteString(`</form></div>`)
	return b.String(), nil
}

func renderField(b *strings.Builder, x *Field, pre, layout string, tr func(string) string) {
	id := pre + x.ID
	cond := ""
	if x.ShowIf != nil {
		c, _ := json.Marshal(x.ShowIf)
		cond = ` data-show-if="` + esc(string(c)) + `"`
	}
	pos := fmt.Sprintf(` style="--ewfm-w:%d"`, x.Width)
	if layout == "free" && x.W > 0 {
		pos = fmt.Sprintf(` style="--ewfm-w:%d;--ewfm-x:%dpx;--ewfm-y:%dpx;--ewfm-fw:%dpx;--ewfm-fh:%dpx"`, x.Width, x.X, x.Y, x.W, x.H)
	}
	fmt.Fprintf(b, `<div class="ewfm-field ewfm-field--%s" data-field="%s" data-type="%s"%s%s>`, esc(x.Type), esc(x.ID), esc(x.Type), cond, pos)
	defer b.WriteString(`</div>`)
	switch x.Type {
	case "heading":
		b.WriteString(`<h2 class="ewfm-heading">` + esc(x.Label) + `</h2>`)
		if x.Help != "" {
			b.WriteString(`<p class="ewfm-help">` + esc(x.Help) + `</p>`)
		}
		return
	case "paragraph":
		b.WriteString(`<p class="ewfm-text">` + strings.ReplaceAll(esc(x.Label), "\n", "<br>") + `</p>`)
		return
	case "divider":
		b.WriteString(`<hr>`)
		return
	case "hidden":
		fmt.Fprintf(b, `<input type="hidden" id="%s" name="%s" value="%s"%s>`, id, esc(x.ID), esc(x.Default), prefillAttr(x))
		return
	}
	req := ""
	mark := ""
	if x.Required {
		req = ` required aria-required="true"`
		mark = `<span class="ewfm-req" aria-hidden="true"> *</span><span class="sr-only"> (` + esc(tr("forms.required")) + `)</span>`
	}
	described := []string{}
	help := ""
	if x.Help != "" {
		help = fmt.Sprintf(`<p class="ewfm-help" id="%s-help">%s</p>`, id, esc(x.Help))
		described = append(described, id+"-help")
	}
	errID := id + "-error"
	described = append(described, errID)
	desc := ` aria-describedby="` + strings.Join(described, " ") + `"`
	errP := fmt.Sprintf(`<p class="ewfm-error" id="%s" hidden></p>`, errID)
	common := fmt.Sprintf(` id="%s" name="%s"%s%s`, id, esc(x.ID), req, desc)
	if x.Autocomplete != "" {
		common += ` autocomplete="` + esc(x.Autocomplete) + `"`
	}
	group := func(kind string) {
		fmt.Fprintf(b, `<fieldset class="ewfm-group" id="%s" aria-describedby="%s"%s><legend>%s%s</legend>%s<div class="ewfm-options">`,
			id, strings.Join(described, " "), groupReq(x), esc(x.Label), mark, help)
		for i, o := range x.Options {
			oid := fmt.Sprintf("%s-%d", id, i)
			checked := ""
			if o.Value == x.Default {
				checked = " checked"
			}
			fmt.Fprintf(b, `<div class="ewfm-option"><input type="%s" id="%s" name="%s" value="%s"%s><label for="%s">%s</label></div>`,
				kind, oid, esc(x.ID), esc(o.Value), checked, oid, esc(o.Label))
		}
		b.WriteString(`</div>` + errP + `</fieldset>`)
	}
	label := fmt.Sprintf(`<label class="ewfm-label" for="%s">%s%s</label>`, id, esc(x.Label), mark)
	switch x.Type {
	case "radio":
		group("radio")
	case "checkboxes":
		group("checkbox")
	case "rating":
		// stars as a group of radio buttons (keyboard: arrows)
		n := 5
		if x.Max != nil && *x.Max >= 2 && *x.Max <= 10 {
			n = int(*x.Max)
		}
		fmt.Fprintf(b, `<fieldset class="ewfm-group ewfm-rating" id="%s" aria-describedby="%s"%s><legend>%s%s</legend>%s<div class="ewfm-stars">`,
			id, strings.Join(described, " "), groupReq(x), esc(x.Label), mark, help)
		for i := 1; i <= n; i++ {
			oid := fmt.Sprintf("%s-%d", id, i)
			fmt.Fprintf(b, `<input type="radio" id="%s" name="%s" value="%d"><label for="%s"><span aria-hidden="true">★</span><span class="sr-only">%s</span></label>`,
				oid, esc(x.ID), i, oid, esc(strings.ReplaceAll(tr("forms.stars"), "{n}", strconv.Itoa(i))))
		}
		b.WriteString(`</div>` + errP + `</fieldset>`)
	case "checkbox", "consent":
		checked := ""
		if x.Default == "true" {
			checked = " checked"
		}
		fmt.Fprintf(b, `<div class="ewfm-check"><input type="checkbox" value="true"%s%s><label for="%s">%s%s</label></div>%s%s`, common, checked, id, esc(x.Label), mark, help, errP)
	case "select", "multiselect":
		multi := ""
		if x.Type == "multiselect" {
			multi = " multiple"
		}
		fmt.Fprintf(b, `%s%s<select%s%s>`, label, help, common, multi)
		if x.Type == "select" {
			fmt.Fprintf(b, `<option value="">%s</option>`, esc(tr("forms.choose")))
		}
		for _, o := range x.Options {
			sel := ""
			if o.Value == x.Default {
				sel = " selected"
			}
			fmt.Fprintf(b, `<option value="%s"%s>%s</option>`, esc(o.Value), sel, esc(o.Label))
		}
		b.WriteString(`</select>` + errP)
	case "textarea":
		rows := x.Rows
		if rows == 0 {
			rows = 5
		}
		fmt.Fprintf(b, `%s%s<textarea%s rows="%d" maxlength="%d"%s%s>%s</textarea>%s`, label, help, common, rows, x.MaxLen, phAttr(x), prefillAttr(x), esc(x.Default), errP)
	case "file":
		accept := []string{}
		for _, a := range x.Accept {
			if exts, ok := FileKinds[a]; ok {
				accept = append(accept, exts...)
			} else {
				accept = append(accept, a)
			}
		}
		multi := ""
		if x.MaxFiles > 1 {
			multi = " multiple"
		}
		limits := strings.NewReplacer("{types}", strings.Join(accept, ", "), "{n}", strconv.Itoa(x.MaxFiles), "{mb}", strconv.Itoa(x.MaxSizeMB)).Replace(tr("forms.file_limits"))
		fmt.Fprintf(b, `%s%s<p class="ewfm-help" id="%s-limits">%s</p><div class="ewfm-drop"><input type="file"%s%s accept="%s" data-max-files="%d" data-max-mb="%d" data-comment="%t"></div><ul class="ewfm-files" aria-live="polite"></ul>%s`,
			label, help, id, esc(limits), strings.Replace(common, `aria-describedby="`, `aria-describedby="`+id+`-limits `, 1), multi, esc(strings.Join(accept, ",")), x.MaxFiles, x.MaxSizeMB, x.FileComment, errP)
	default:
		typ := map[string]string{"text": "text", "email": "email", "tel": "tel", "url": "url", "number": "number", "date": "date",
			"time": "time", "datetime": "datetime-local", "range": "range"}[x.Type]
		attrs := phAttr(x) + prefillAttr(x)
		if x.Type == "text" || x.Type == "email" || x.Type == "tel" || x.Type == "url" {
			attrs += fmt.Sprintf(` maxlength="%d"`, x.MaxLen)
			if x.MinLen > 0 {
				attrs += fmt.Sprintf(` minlength="%d"`, x.MinLen)
			}
			if x.Pattern != "" {
				attrs += ` pattern="` + esc(x.Pattern) + `"`
			}
		}
		if x.Min != nil {
			attrs += ` min="` + strconv.FormatFloat(*x.Min, 'f', -1, 64) + `"`
		}
		if x.Max != nil {
			attrs += ` max="` + strconv.FormatFloat(*x.Max, 'f', -1, 64) + `"`
		}
		if x.Step > 0 {
			attrs += ` step="` + strconv.FormatFloat(x.Step, 'f', -1, 64) + `"`
		}
		if x.Type == "number" {
			attrs += ` inputmode="decimal"`
		}
		fmt.Fprintf(b, `%s%s<input type="%s"%s value="%s"%s>`, label, help, typ, common, esc(x.Default), attrs)
		if x.Type == "range" {
			fmt.Fprintf(b, `<output for="%s" class="ewfm-range-value"></output>`, id)
		}
		b.WriteString(errP)
	}
}

func groupReq(x *Field) string {
	if x.Required {
		return ` data-required="true"`
	}
	return ""
}

func phAttr(x *Field) string {
	if x.Placeholder == "" {
		return ""
	}
	return ` placeholder="` + esc(x.Placeholder) + `"`
}

func prefillAttr(x *Field) string {
	if x.Prefill == "" {
		return ""
	}
	return ` data-prefill="` + esc(x.Prefill) + `"`
}
