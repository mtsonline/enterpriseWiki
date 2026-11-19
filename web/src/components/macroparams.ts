// Controls for macro parameters that name a page, a category or a page
// template (custom:page, custom:category, custom:template): people choose by
// title instead of typing numbers. The value stays the number.
import { api } from '../core/api';
import { h } from '../core/dom';
import { t } from '../core/i18n';
import type { MacroParamControl } from '../editor/macros';
import { errorText, field, select } from './ui';
import { categorySelect, elementSelect } from './pickers';
import type { Template } from './publishing';

const num = (v: string): number => (/^[1-9][0-9]{0,17}$/.test(v.trim()) ? Number(v.trim()) : 0);

/** A box filled once the picker is loaded; until then the old value counts. */
function later(value: string, label: string, make: () => Promise<{ el: HTMLElement; get: () => string }>): { el: HTMLElement; get: () => string } {
  const box = h('div', { class: 'ew-macro-param', role: 'group', 'aria-label': label }, h('p', { class: 'ew-help' }, t('common.loading')));
  let get = () => value;
  make().then((c) => { box.replaceChildren(c.el); get = c.get; })
    .catch((e: unknown) => { box.replaceChildren(h('p', { role: 'alert' }, errorText(e))); });
  return { el: box, get: () => get() };
}

export const pageControl: MacroParamControl = (value, ctx) => later(value, ctx.label, async () => {
  const s = await elementSelect({ selected: num(value) });
  return { el: h('fieldset', { class: 'ew-fieldset' }, h('legend', null, ctx.label), s.el), get: () => (s.value() ? String(s.value()) : '') };
});

export const categoryControl: MacroParamControl = (value, ctx) => later(value, ctx.label, async () => {
  const s = await categorySelect({ selected: num(value), noneLabel: t('macro.this_category') });
  return { el: s.field(ctx.label, { required: ctx.required }), get: () => (s.value() ? String(s.value()) : '') };
});

export const templateControl: MacroParamControl = (value, ctx) => later(value, ctx.label, async () => {
  const list = await api.get<Template[]>(`templates${ctx.categoryId ? `?category=${ctx.categoryId}` : ''}`);
  const opts = [{ value: '', label: t('template.none') }, ...list.map((x) => ({ value: String(x.id), label: x.name }))];
  if (num(value) && !list.some((x) => String(x.id) === value.trim())) opts.push({ value: value.trim(), label: `#${value.trim()}` });
  const sel = select(opts, value.trim());
  return { el: field(ctx.label, sel, { required: ctx.required, help: t('macro.template_help') }), get: () => sel.value };
});
