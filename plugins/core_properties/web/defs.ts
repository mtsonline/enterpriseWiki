// Definitions of properties: the list (own, inherited, global) and the
// dialog to define one (type, options, choices, rights, element types).
import { api, button, checkbox, confirmDialog, errorText, field, formDialog, h, langFields, pageHeader, qs, select, state, subjectsEditor, t, textArea, textInput, toast, typeLabel as elementTypeLabel } from '@ewiki/sdk';
import { BASE, choiceLabel, loadTypes, plainValue, typeLabel, type Choice, type Def, type TypeDef } from './model';
import { valueInput } from './inputs';

interface DefsResponse { defs: Def[]; can_define: boolean; free: boolean; free_setting: string }

const label = (d: Def): string => d.labels[state.lang] || d.labels.en || Object.values(d.labels)[0] || d.key;

function choicesEditor(initial: Choice[], palette: string[], status: boolean): { el: HTMLElement; value: () => Choice[] } {
  const rows: { value: () => Choice | null }[] = [];
  const list = h('ol', { class: 'ewprop-choice-list' });
  const add = (c: Choice) => {
    const v = textInput({ value: c.value, maxlength: 100 });
    v.setAttribute('aria-label', t('props.choice_value'));
    const de = textInput({ value: c.labels?.de ?? '', maxlength: 100 });
    de.setAttribute('aria-label', t('props.choice_label_de'));
    const en = textInput({ value: c.labels?.en ?? '', maxlength: 100 });
    en.setAttribute('aria-label', t('props.choice_label_en'));
    const col = select(palette.map((p) => ({ value: p, label: t(`props.color.${p}`) })), c.color || 'grey');
    col.setAttribute('aria-label', t('props.choice_color'));
    const entry = {
      value: (): Choice | null => {
        const val = v.value.trim();
        if (!val) return null;
        const labels: Record<string, string> = {};
        if (de.value.trim()) labels.de = de.value.trim();
        if (en.value.trim()) labels.en = en.value.trim();
        return { value: val, labels, ...(status ? { color: col.value } : {}) };
      },
    };
    const li: HTMLElement = h('li', { class: 'ewprop-choice' }, v, de, en, status ? col : '',
      button(t('common.remove'), () => { li.remove(); rows.splice(rows.indexOf(entry), 1); }, { variant: 'subtle', ariaLabel: t('props.remove_choice') }));
    rows.push(entry);
    list.append(li);
    return v;
  };
  initial.forEach(add);
  return {
    el: h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('props.choices')), h('p', { class: 'ew-help' }, t('props.choices_help')),
      h('div', { class: 'ewprop-choice-head', 'aria-hidden': 'true' }, h('span', null, t('props.choice_value')), h('span', null, 'DE'), h('span', null, 'EN'), status ? h('span', null, t('props.choice_color')) : ''),
      list, button(t('props.add_choice'), () => add({ value: '' }).focus(), { variant: 'secondary' })),
    value: () => rows.map((r) => r.value()).filter((x): x is Choice => x !== null),
  };
}

/** Opens the dialog to define (or change) a property. */
export async function defDialog(categoryId: number | null, existing: Def | null, done: () => void): Promise<void> {
  const { types, palette } = await loadTypes();
  const key = textInput({ value: existing?.key ?? '', maxlength: 63, required: true });
  key.pattern = '[a-z][a-z0-9_]{0,62}';
  if (existing) key.readOnly = true;
  const labels = langFields(t('props.label'), existing?.labels ?? {}, { required: true });
  const help = langFields(t('props.help_text'), existing?.help ?? {});
  const groups = ['text', 'number', 'time', 'choice', 'reference', 'auto'];
  const typeSel = h('select', { class: 'ew-input' }, ...groups.map((g) => h('optgroup', { label: t(`props.group.${g}`) },
    ...types.filter((x) => x.group === g).map((x) => h('option', { value: x.id, selected: (existing?.type ?? 'text') === x.id }, typeLabel(x.id)))))) as HTMLSelectElement;
  const optBox = h('div', { class: 'ewprop-options' });
  const o = existing?.options ?? {};
  const num = (v?: number) => textInput({ value: v === undefined ? '' : String(v) });
  const min = num(o.min), max = num(o.max), decimals = num(o.decimals), digits = num(o.digits);
  const unit = textInput({ value: o.unit ?? '', maxlength: 16 });
  const currency = textInput({ value: o.currency ?? 'EUR', maxlength: 3 });
  const maxLen = num(o.max_len || undefined);
  const pattern = textInput({ value: o.pattern ?? '', maxlength: 200 });
  const prefix = textInput({ value: o.prefix ?? '', maxlength: 12 });
  const formula = textArea({ value: o.formula ?? '', rows: 2 });
  formula.classList.add('ewe-mono');
  const multiple = checkbox(t('props.opt.multiple'), !!o.multiple);
  let choices = choicesEditor(o.choices ?? [], palette, (existing?.type ?? '') === 'status');
  const required = checkbox(t('props.required_label'), !!existing?.required, { help: t('props.required_help') });
  const onPage = checkbox(t('props.show_on_page'), existing?.show_on_page ?? true, { help: t('props.show_on_page_help') });
  const group = textInput({ value: existing?.group ?? '', maxlength: 60 });
  const position = num(existing?.position ?? 0);
  const etypes = state.site.element_types.map((x) => checkbox(elementTypeLabel(x.id), !!existing?.element_types.includes(x.id)));
  // names of the people among the subjects
  const uids = [...(existing?.view_subjects ?? []), ...(existing?.edit_subjects ?? [])].filter((x) => x.type === 'user' && x.id).map((x) => x.id);
  const names: Record<string, { name: string }> = uids.length ? await api.get<Record<string, { name: string }>>(`${BASE}/refs${qs({ user: uids.join(','), lang: state.lang })}`).catch(() => ({})) : {};
  const nameMap = Object.fromEntries(Object.entries(names).map(([k, v]) => [k, v.name]));
  const viewS = await subjectsEditor(t('props.view_subjects'), t('props.view_subjects_help'), existing?.view_subjects ?? [], nameMap);
  const editS = await subjectsEditor(t('props.edit_subjects'), t('props.edit_subjects_help'), existing?.edit_subjects ?? [], nameMap);
  const defBox = h('div');
  let defaultInput: Awaited<ReturnType<typeof valueInput>> | null = null;
  const typeOf = (): TypeDef => types.find((x) => x.id === typeSel.value)!;
  const drawOptions = async () => {
    const td = typeOf();
    const has = (k: string) => td.options?.includes(k);
    if (has('choices')) {
      const cur = choices.value();
      choices = choicesEditor(cur.length ? cur : o.choices ?? [], palette, td.id === 'status');
    }
    optBox.replaceChildren(
      h('p', { class: 'ew-help' }, t(`props.type_help.${td.id}`)),
      has('choices') ? choices.el : '',
      has('min') ? field(t('props.opt.min'), min) : '', has('max') ? field(td.id === 'rating' ? t('props.opt.stars') : t('props.opt.max'), max) : '',
      has('decimals') ? field(t('props.opt.decimals'), decimals) : '', has('unit') ? field(t('props.opt.unit'), unit, { help: t('props.opt.unit_help') }) : '',
      has('currency') ? field(t('props.opt.currency'), currency, { help: t('props.opt.currency_help') }) : '',
      has('max_len') ? field(t('props.opt.max_len'), maxLen) : '', has('pattern') ? field(t('props.opt.pattern'), pattern, { help: t('props.opt.pattern_help') }) : '',
      has('multiple') ? multiple.el : '', has('prefix') ? field(t('props.opt.prefix'), prefix, { help: t('props.opt.prefix_help') }) : '',
      has('digits') ? field(t('props.opt.digits'), digits) : '', has('formula') ? field(t('props.opt.formula'), formula, { help: t('props.opt.formula_help'), required: true }) : '');
    required.el.hidden = !!td.computed;
    defBox.replaceChildren();
    defaultInput = null;
    if (!td.computed && !['element', 'media', 'category'].includes(td.id)) {
      defaultInput = await valueInput({ key: 'default', label: t('props.default'), type: td.id, options: optionsValue(), help: t('props.default_help') }, existing?.type === td.id ? existing?.default : null, {});
      defBox.append(defaultInput.el);
    }
  };
  const n = (i: HTMLInputElement) => (i.value.trim() === '' ? undefined : Number(i.value.replace(',', '.')));
  const optionsValue = () => {
    const td = typeOf();
    const has = (k: string) => td.options?.includes(k);
    return {
      ...(has('choices') ? { choices: choices.value() } : {}), ...(has('min') ? { min: n(min) } : {}), ...(has('max') ? { max: n(max) } : {}),
      ...(has('decimals') ? { decimals: n(decimals) } : {}), ...(has('unit') ? { unit: unit.value.trim() } : {}), ...(has('currency') ? { currency: currency.value.trim().toUpperCase() } : {}),
      ...(has('max_len') ? { max_len: n(maxLen) } : {}), ...(has('pattern') ? { pattern: pattern.value.trim() } : {}), ...(has('multiple') ? { multiple: multiple.input.checked } : {}),
      ...(has('prefix') ? { prefix: prefix.value.trim(), digits: n(digits) ?? 0 } : {}), ...(has('formula') ? { formula: formula.value.trim() } : {}),
    };
  };
  typeSel.addEventListener('change', () => void drawOptions());
  await drawOptions();
  if (existing) {
    // only types stored the same way (e.g. text and e-mail) can be changed into each other
    const st = types.find((x) => x.id === existing.type)?.storage;
    for (const opt of Array.from(typeSel.options)) opt.disabled = types.find((x) => x.id === opt.value)?.storage !== st;
  }
  const where = categoryId ? t('props.def_where_category') : t('props.def_where_global');
  formDialog(existing ? t('props.def_edit', { name: label(existing) }) : t('props.def_new'), [
    h('p', { class: 'ew-help' }, where),
    field(t('props.key'), key, { required: true, help: t('props.key_help') }), labels.el,
    field(t('props.type_label'), typeSel), optBox, defBox, required.el, onPage.el,
    h('details', { class: 'ewprop-more' }, h('summary', null, t('props.more_settings')),
      help.el, field(t('props.group_label'), group, { help: t('props.group_help') }), field(t('props.position'), position),
      h('fieldset', { class: 'ew-fieldset' }, h('legend', null, t('props.element_types')), h('p', { class: 'ew-help' }, t('props.element_types_help')), ...etypes.map((x) => x.el)),
      viewS.el, editS.el),
  ], existing ? t('common.save') : t('props.define'), async () => {
    const def = defaultInput?.validate();
    if (def) throw new Error(def);
    const body = {
      category_id: categoryId, key: key.value.trim(), labels: labels.get(), help: help.get(), type: typeSel.value, options: optionsValue(),
      required: required.input.checked, default: defaultInput?.get() ?? null, element_types: state.site.element_types.filter((_, i) => etypes[i].input.checked).map((x) => x.id),
      group: group.value.trim(), position: Number(position.value) || 0, show_on_page: onPage.input.checked,
      view_subjects: viewS.value(), edit_subjects: editS.value(),
    };
    if (existing) await api.put(`${BASE}/defs/${existing.id}`, body);
    else await api.post(`${BASE}/defs`, body);
    toast(t('props.def_saved'));
    done();
  }, { wide: true });
}

/** The definitions of a category (or the global ones) with the setting for free properties. */
export async function defsManager(panel: HTMLElement, categoryId: number | null): Promise<void> {
  const data = await api.get<DefsResponse>(`${BASE}/defs${qs({ category: categoryId ?? undefined, lang: state.lang })}`);
  const reload = () => void defsManager(panel, categoryId);
  const rows = data.defs.map((d) => {
    const from = d.own ? t('props.from_here') : d.inherited_from === 'global' ? t('props.from_global') : t('props.from_parent');
    const opts = d.options.choices?.length ? d.options.choices.map((c) => choiceLabel(c, c.value)).join(', ') : d.options.formula ?? '';
    return h('tr', null,
      h('th', { scope: 'row' }, label(d), h('br'), h('code', null, d.key)),
      h('td', null, typeLabel(d.type), opts ? h('div', { class: 'ew-help' }, opts) : ''),
      h('td', null, [d.required ? t('props.required_short') : '', d.view_subjects.length ? t('props.restricted_view') : '', d.edit_subjects.length ? t('props.restricted_edit') : '',
        d.default !== null && d.default !== undefined ? t('props.default_short', { v: plainValue(d.type, d.default, d.options, {}) }) : ''].filter(Boolean).join(' · ') || '—'),
      h('td', null, from),
      h('td', null, data.can_define && d.own ? h('span', { class: 'ew-actions' },
        button(t('common.edit'), () => void defDialog(categoryId, d, reload), { variant: 'link', ariaLabel: `${t('common.edit')}: ${label(d)}` }),
        button(t('common.delete'), async () => {
          const keep = await confirmDialog(t('props.def_delete_confirm', { name: label(d) }), t('props.def_delete_keep'), true);
          if (!keep) return;
          try { await api.del(`${BASE}/defs/${d.id}?values=keep`); toast(t('props.def_deleted')); reload(); } catch (e) { toast(errorText(e), 'error'); }
        }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${label(d)}` })) : ''));
  });
  const headers = [t('props.property'), t('props.type_label'), t('props.rules'), t('props.defined_in'), t('common.actions')];
  const free = categoryId ? (() => {
    const sel = select(['inherit', 'allow', 'deny'].map((x) => ({ value: x, label: t(`props.free.${x}`) })), data.free_setting);
    sel.disabled = !data.can_define;
    sel.addEventListener('change', async () => {
      try { await api.put(`${BASE}/settings/${categoryId}`, { free: sel.value }); toast(t('common.saved')); } catch (e) { toast(errorText(e), 'error'); }
    });
    return field(t('props.free_label'), sel, { help: t('props.free_help', { now: data.free ? t('props.free.allow') : t('props.free.deny') }) });
  })() : h('p', { class: 'ew-help' }, t('props.free_global_help'));
  panel.replaceChildren(
    ...(categoryId ? [h('h2', null, t('props.title'))] : [pageHeader(t('props.admin_title'))]),
    h('p', null, categoryId ? t('props.category_help') : t('props.global_help')),
    h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('props.defs')),
      h('thead', null, h('tr', null, ...headers.map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: String(headers.length) }, t('props.no_defs')))]))),
    data.can_define ? h('div', { class: 'ew-form-actions' }, button(t('props.def_new'), () => void defDialog(categoryId, null, reload), { variant: 'primary' })) : '',
    free);
}
