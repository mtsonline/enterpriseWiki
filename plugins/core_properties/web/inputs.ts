// An input for a value of each type (with its label, help and required mark).
import { checkbox, elementSelect, field, groupPicker, h, select, state, t, textArea, textInput, userPicker, categorySelect, pickMedium, button, mediaURL } from '@ewiki/sdk';
import { choiceLabel, formatDuration, type Options, type Ref } from './model';

export interface ValueInput {
  el: HTMLElement;
  /** the value to send (null: none) */
  get: () => unknown;
  /** a problem to show (the input focuses itself) */
  validate: () => string | null;
}

interface Spec { key: string; label: string; type: string; options?: Options; required?: boolean; help?: string; readonly?: boolean }

const COUNTRIES = 'AT DE CH IT FR NL BE LU LI CZ SK SI HU PL HR GB IE ES PT DK SE NO FI US CA'.split(' ');
const LANGS = 'de en fr it es pt nl pl cs sk sl hu hr tr ru uk ar zh ja'.split(' ');

function nameOf(kind: 'region' | 'language', code: string): string {
  try { return new Intl.DisplayNames([state.lang], { type: kind }).of(code) ?? code; } catch { return code; }
}

export async function valueInput(s: Spec, value: unknown, refs: Record<string, Ref>): Promise<ValueInput> {
  const o = s.options ?? {};
  const opt = { required: s.required, help: s.help };
  const simple = (input: HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement, read: () => unknown, check?: () => string | null): ValueInput => {
    if (s.readonly) input.disabled = true;
    return {
      el: field(s.label, input, opt),
      get: read,
      validate: () => {
        if (s.required && (read() === null || read() === '')) { input.focus(); return t('props.required', { name: s.label }); }
        if ('checkValidity' in input && !input.checkValidity()) { input.focus(); return `${s.label}: ${input.validationMessage}`; }
        const e = check?.() ?? null;
        if (e) input.focus();
        return e;
      },
    };
  };
  const str = (v: unknown) => (v === null || v === undefined ? '' : String(v));
  const orNull = (x: string) => (x.trim() === '' ? null : x.trim());
  switch (s.type) {
    case 'textarea': {
      const ta = textArea({ value: str(value), rows: 3 });
      ta.maxLength = o.max_len || 4000;
      return simple(ta, () => orNull(ta.value));
    }
    case 'integer': case 'decimal': case 'percent': case 'currency': case 'unit': {
      const i = textInput({ value: value === null || value === undefined ? '' : String(value) });
      i.inputMode = s.type === 'integer' ? 'numeric' : 'decimal';
      i.setAttribute('autocomplete', 'off');
      const suffix = s.type === 'percent' ? '%' : s.type === 'currency' ? (o.currency || 'EUR') : o.unit;
      const read = () => {
        const x = i.value.trim().replace(',', '.');
        return x === '' ? null : Number(x);
      };
      const w = simple(i, read, () => {
        const n = read();
        if (n === null) return null;
        if (Number.isNaN(n) || (s.type === 'integer' && !Number.isInteger(n))) return t('props.invalid_number', { name: s.label });
        if (o.min !== undefined && (n as number) < o.min) return t('props.min', { name: s.label, n: o.min });
        if (o.max !== undefined && (n as number) > o.max) return t('props.max', { name: s.label, n: o.max });
        return null;
      });
      if (suffix) i.after(h('span', { class: 'ewprop-suffix', 'aria-hidden': 'true' }, suffix));
      if (suffix) i.setAttribute('aria-description', suffix);
      return w;
    }
    case 'rating': {
      const max = o.max ?? 5;
      const sel = select([{ value: '', label: '—' }, ...Array.from({ length: max + 1 }, (_, n) => ({ value: String(n), label: `${'★'.repeat(n)}${'☆'.repeat(max - n)} (${n})` }))], str(value));
      return simple(sel, () => (sel.value === '' ? null : Number(sel.value)));
    }
    case 'progress': {
      const r = h('input', { type: 'range', min: '0', max: '100', step: '5', value: str(value ?? 0), class: 'ewprop-range' }) as HTMLInputElement;
      const out = h('output', null, `${r.value} %`);
      r.addEventListener('input', () => { out.textContent = `${r.value} %`; });
      const w = simple(r, () => Number(r.value));
      r.after(out);
      return w;
    }
    case 'duration': {
      const i = textInput({ value: typeof value === 'number' ? formatDuration(value).replace(/ (d|h|min)/g, '$1') : '', placeholder: '1h 30m' });
      return simple(i, () => orNull(i.value));
    }
    case 'boolean': {
      const c = checkbox(s.label, value === true, { help: s.help });
      if (s.readonly) c.input.disabled = true;
      return { el: c.el, get: () => c.input.checked, validate: () => null };
    }
    case 'date': case 'datetime': case 'time': {
      const i = textInput({ type: s.type === 'datetime' ? 'datetime-local' : s.type, value: s.type === 'datetime' && value ? String(value).slice(0, 16) : str(value) });
      return simple(i, () => {
        if (!i.value) return null;
        return s.type === 'datetime' ? new Date(i.value).toISOString().replace(/\.\d{3}Z$/, 'Z') : i.value;
      });
    }
    case 'daterange': {
      const r = (value ?? {}) as { from?: string; to?: string };
      const from = textInput({ type: 'date', value: r.from ?? '' });
      const to = textInput({ type: 'date', value: r.to ?? '' });
      from.setAttribute('aria-label', `${s.label}: ${t('props.from')}`);
      to.setAttribute('aria-label', `${s.label}: ${t('props.to')}`);
      const box = h('fieldset', { class: 'ew-fieldset ewprop-range-fields' }, h('legend', null, s.label + (s.required ? ' *' : '')),
        h('label', null, t('props.from'), ' ', from), h('label', null, t('props.to'), ' ', to), s.help ? h('p', { class: 'ew-help' }, s.help) : '');
      const get = () => (from.value || to.value ? { from: from.value, to: to.value || from.value } : null);
      return {
        el: box, get,
        validate: () => {
          if (s.required && !get()) { from.focus(); return t('props.required', { name: s.label }); }
          if (from.value && to.value && to.value < from.value) { to.focus(); return t('props.range_order', { name: s.label }); }
          return null;
        },
      };
    }
    case 'select': case 'status': {
      if (!o.choices?.length) {
        const i = textInput({ value: str(value), maxlength: 100 });
        return simple(i, () => orNull(i.value));
      }
      const sel = select([{ value: '', label: '—' }, ...o.choices.map((c) => ({ value: c.value, label: choiceLabel(c, c.value) }))], str(value));
      return simple(sel, () => (sel.value === '' ? null : sel.value));
    }
    case 'multiselect': {
      const cur = Array.isArray(value) ? (value as string[]) : [];
      if (!o.choices?.length) {
        const i = textInput({ value: cur.join(', '), placeholder: t('props.comma_list') });
        return simple(i, () => { const l = i.value.split(',').map((x) => x.trim()).filter(Boolean); return l.length ? l : null; });
      }
      const boxes = o.choices.map((c) => checkbox(choiceLabel(c, c.value), cur.includes(c.value)));
      const fs = h('fieldset', { class: 'ew-fieldset ewprop-choices' }, h('legend', null, s.label + (s.required ? ' *' : '')), ...boxes.map((b) => b.el),
        s.help ? h('p', { class: 'ew-help' }, s.help) : '');
      const get = () => { const l = o.choices!.filter((_, i) => boxes[i].input.checked).map((c) => c.value); return l.length ? l : null; };
      return { el: fs, get, validate: () => (s.required && !get() ? (boxes[0]?.input.focus(), t('props.required', { name: s.label })) : null) };
    }
    case 'user': {
      const ids = Array.isArray(value) ? (value as number[]) : [];
      const p = userPicker({ label: s.label, multiple: !!o.multiple, help: s.help, required: s.required, initial: ids.map((id) => ({ id, name: refs[`user:${id}`]?.name ?? `#${id}` })) });
      return { el: p.el, get: () => (p.value().length ? p.value() : null), validate: () => (s.required && !p.value().length ? (p.focus(), t('props.required', { name: s.label })) : null) };
    }
    case 'group': {
      const p = await groupPicker({ label: s.label, multiple: !!o.multiple, help: s.help, initial: Array.isArray(value) ? (value as number[]) : [] });
      return { el: p.el, get: () => (p.value().length ? p.value() : null), validate: () => (s.required && !p.value().length ? t('props.required', { name: s.label }) : null) };
    }
    case 'element': {
      // one element per picker; several: one picker per value
      const ids = Array.isArray(value) ? (value as number[]) : [];
      const list = h('div', { class: 'ewprop-elements' });
      const pickers: { value: () => number }[] = [];
      const add = async (id?: number) => {
        const p = await elementSelect({ selected: id });
        pickers.push(p);
        list.append(p.el);
      };
      for (const id of ids) await add(id);
      if (!ids.length) await add();
      const more = o.multiple ? button(t('props.add_element'), () => void add(), { variant: 'subtle' }) : '';
      const fs = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, s.label + (s.required ? ' *' : '')), list, more, s.help ? h('p', { class: 'ew-help' }, s.help) : '');
      const get = () => { const l = pickers.map((p) => p.value()).filter((x) => x > 0); return l.length ? l : null; };
      return { el: fs, get, validate: () => (s.required && !get() ? t('props.required', { name: s.label }) : null) };
    }
    case 'category': {
      const ids = Array.isArray(value) ? (value as number[]) : [];
      const c = await categorySelect({ selected: ids[0], noneLabel: '—' });
      return { el: c.field(s.label, opt), get: () => (c.value() > 0 ? [c.value()] : null), validate: () => (s.required && !(c.value() > 0) ? t('props.required', { name: s.label }) : null) };
    }
    case 'media': {
      let cur = str(value);
      const shown = h('span', { class: 'ewprop-media' }, cur ? cur.split('/').pop() : t('props.none'));
      const pick = button(t('props.choose_file'), async () => {
        const m = await pickMedium({});
        if (m) { cur = mediaURL(m); shown.textContent = m.title || cur.split('/').pop() || cur; }
      }, { variant: 'secondary', ariaLabel: `${t('props.choose_file')}: ${s.label}` });
      const clear = button(t('common.remove'), () => { cur = ''; shown.textContent = t('props.none'); }, { variant: 'subtle', ariaLabel: `${t('common.remove')}: ${s.label}` });
      const fs = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, s.label), shown, ' ', pick, ' ', clear);
      return { el: fs, get: () => (cur ? cur : null), validate: () => (s.required && !cur ? t('props.required', { name: s.label }) : null) };
    }
    case 'location': {
      const l = (value ?? {}) as { address?: string; lat?: number; lng?: number };
      const addr = textInput({ value: l.address ?? '', maxlength: 300 });
      const lat = textInput({ value: l.lat?.toString() ?? '' });
      const lng = textInput({ value: l.lng?.toString() ?? '' });
      lat.inputMode = lng.inputMode = 'decimal';
      const fs = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, s.label + (s.required ? ' *' : '')),
        field(t('props.address'), addr), h('div', { class: 'ewprop-latlng' }, field(t('props.lat'), lat), field(t('props.lng'), lng)));
      const get = () => {
        const a = addr.value.trim(), la = lat.value.trim(), lo = lng.value.trim();
        if (!a && !la) return null;
        return { address: a, ...(la && lo ? { lat: Number(la.replace(',', '.')), lng: Number(lo.replace(',', '.')) } : {}) };
      };
      return { el: fs, get, validate: () => (s.required && !get() ? (addr.focus(), t('props.required', { name: s.label })) : null) };
    }
    case 'country': case 'language': {
      const codes = s.type === 'country' ? COUNTRIES : LANGS;
      const kind = s.type === 'country' ? 'region' : 'language';
      const all = [...new Set([...(value ? [String(value)] : []), ...codes])];
      const sel = select([{ value: '', label: '—' }, ...all.map((c) => ({ value: c, label: `${nameOf(kind, c)} (${c})` }))], str(value));
      return simple(sel, () => (sel.value === '' ? null : sel.value));
    }
    case 'color': {
      const i = h('input', { type: 'color', class: 'ew-input ewprop-colorinput', value: str(value) || '#000000' }) as HTMLInputElement;
      const none = checkbox(t('props.no_color'), !value);
      const w = simple(i, () => (none.input.checked ? null : i.value));
      w.el.append(none.el);
      return w;
    }
    case 'autonumber': case 'formula': {
      const i = textInput({ value: str(value) });
      i.readOnly = true;
      return { el: field(s.label, i, { help: s.type === 'formula' ? t('props.computed_formula') : t('props.computed_number') }), get: () => null, validate: () => null };
    }
  }
  // text, email, phone, url, code, version
  const type = s.type === 'email' ? 'email' : s.type === 'url' ? 'url' : s.type === 'phone' ? 'tel' : 'text';
  const i = textInput({ value: str(value), type, maxlength: o.max_len || (s.type === 'code' ? 100 : 4000) });
  if (o.pattern) i.pattern = o.pattern;
  if (s.type === 'version') i.placeholder = '1.0.0';
  return simple(i, () => orNull(i.value));
}
