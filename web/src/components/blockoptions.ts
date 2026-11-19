// The options of a placed block: a form built from the schema the server
// checks with (GET /block-options), in groups to open, with values per view
// where it makes sense. Problems (contrast, room for columns …) are shown
// while choosing, with the same rules as the server; a sample shows the look.
import { api } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { optionsCSS, problems, SCHEMA, type BlockOptions, type OptionDef, type OptionValue, type ViewName } from '../core/blockoptions';
import { button, field, openDialog, select } from './ui';
import { mediaURL, pickMedium } from './media';

let schemaCheck: Promise<void> | null = null;

/** The label of an option or of one of its values. */
const label = (id: string, value?: string) => {
  const k = value === undefined ? `bo.${id}` : `bo.${id}.${value}`;
  const v = t(k);
  return v === k ? (value ?? id) : v;
};
const viewName = (v: ViewName) => t(`bo.view.${v}`);

/** A control for one value of an option: get() is undefined when not set. */
function control(def: OptionDef, value: unknown, name: string, onChange: () => void): { el: HTMLElement; get: () => OptionValue | undefined } {
  const id = uid('bo');
  const fire = () => onChange();
  switch (def.type) {
    case 'enum': {
      const s = select([{ value: '', label: t('bo.default') }, ...(def.values ?? []).map((v) => ({ value: v, label: label(def.id, v) }))], typeof value === 'string' ? value : '');
      s.id = id;
      s.addEventListener('change', fire);
      return { el: field(name, s), get: () => s.value || undefined };
    }
    case 'space': {
      const s = select([{ value: '', label: t('bo.default') }, ...SCHEMA.space_rem.map((r, i) => ({ value: String(i), label: `${r} rem` }))], typeof value === 'number' ? String(value) : '');
      s.id = id;
      s.addEventListener('change', fire);
      return { el: field(name, s), get: () => (s.value === '' ? undefined : Number(s.value)) };
    }
    case 'bool': {
      const c = h('input', { type: 'checkbox', id, checked: value === true }) as HTMLInputElement;
      c.addEventListener('change', fire);
      return { el: h('div', { class: 'ew-check' }, c, h('label', { for: id }, name)), get: () => (c.checked ? true : undefined) };
    }
    case 'int': case 'number': {
      const i = h('input', { type: 'number', class: 'ew-input', id, value: typeof value === 'number' ? String(value) : '', min: String(def.min), max: String(def.max), step: String(def.step ?? 1) }) as HTMLInputElement;
      i.addEventListener('input', fire);
      const unit = def.unit ? ` (${def.unit})` : '';
      return { el: field(`${name}${unit}`, i, { help: t('bo.range', { min: def.min ?? 0, max: def.max ?? 0 }) }), get: () => (i.value.trim() === '' ? undefined : Number(i.value)) };
    }
    case 'color': {
      const on = h('input', { type: 'checkbox', id: `${id}-on`, checked: typeof value === 'string' }) as HTMLInputElement;
      const c = h('input', { type: 'color', class: 'ew-bo-color', id, value: typeof value === 'string' ? value : '#000000', disabled: typeof value !== 'string', 'aria-label': name }) as HTMLInputElement;
      on.addEventListener('change', () => { c.disabled = !on.checked; fire(); });
      c.addEventListener('input', fire);
      return {
        el: h('div', { class: 'ew-bo-colorfield' }, h('div', { class: 'ew-check' }, on, h('label', { for: on.id }, t('bo.own_color', { name }))), c),
        get: () => (on.checked ? c.value.toLowerCase() : undefined),
      };
    }
    case 'file': {
      const i = h('input', { class: 'ew-input', id, value: typeof value === 'string' ? value : '', placeholder: '/api/v1/files/…' }) as HTMLInputElement;
      i.addEventListener('change', fire);
      const pick = button(t('bo.pick_image'), async () => {
        const m = await pickMedium({ type: 'image' });
        if (m) { i.value = mediaURL(m); fire(); }
      }, { variant: 'subtle' });
      return { el: h('div', null, field(name, i, { help: t('bo.file_help') }), pick), get: () => i.value.trim() || undefined };
    }
    case 'anchor': {
      const i = h('input', { class: 'ew-input', id, value: typeof value === 'string' ? value : '', maxlength: 40 }) as HTMLInputElement;
      i.addEventListener('input', fire);
      return { el: field(name, i, { help: t('bo.anchor_help') }), get: () => i.value.trim() || undefined };
    }
    case 'datetime': {
      const local = (iso: string) => {
        const d = new Date(iso);
        return Number.isNaN(d.getTime()) ? '' : new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
      };
      const i = h('input', { type: 'datetime-local', class: 'ew-input', id, value: typeof value === 'string' ? local(value) : '' }) as HTMLInputElement;
      i.addEventListener('change', fire);
      return { el: field(name, i), get: () => (i.value ? new Date(i.value).toISOString().replace(/\.\d{3}Z$/, 'Z') : undefined) };
    }
    case 'views': case 'langs': {
      const items = def.type === 'views' ? SCHEMA.views.map((v) => ({ value: v as string, label: viewName(v) })) : state.site.languages.map((l) => ({ value: l, label: t(`lang.${l}`) === `lang.${l}` ? l.toUpperCase() : t(`lang.${l}`) }));
      const have = Array.isArray(value) ? value as string[] : [];
      const boxes = items.map((it) => {
        const c = h('input', { type: 'checkbox', id: uid('bo'), checked: have.includes(it.value), value: it.value }) as HTMLInputElement;
        c.addEventListener('change', fire);
        return { c, el: h('div', { class: 'ew-check' }, c, h('label', { for: c.id }, it.label)) };
      });
      return {
        el: h('fieldset', { class: 'ew-bo-set' }, h('legend', null, name), ...boxes.map((b) => b.el)),
        get: () => { const v = boxes.filter((b) => b.c.checked).map((b) => b.c.value); return v.length ? v : undefined; },
      };
    }
  }
}

/** Opens the options of one placed block; done gets the new options. */
export function optionsDialog(blockLabel: string, current: BlockOptions, done: (o: BlockOptions) => void): void {
  // the schema of the server is the one this form is built from (same file)
  schemaCheck ??= api.get<{ version: number }>('block-options').then((s) => {
    if (s.version !== SCHEMA.version) console.warn('block options: the server has another version of the schema');
  }).catch(() => { schemaCheck = null; });
  const getters: { id: string; responsive: boolean; get: Partial<Record<ViewName, () => OptionValue | undefined>> }[] = [];
  const problemsBox = h('div', { class: 'ew-bo-problems', role: 'status', 'aria-live': 'polite' });
  const sample = h('div', { class: 'ew-bo', 'data-bo': 'sample' },
    h('div', { class: 'ew-bo-body' }, h('h3', null, t('bo.sample_title')), h('p', null, t('bo.sample_text'), ' ', h('a', { href: '#', onclick: (e: Event) => e.preventDefault() }, t('bo.sample_link')))));
  const sampleStyle = h('style');
  const sampleNote = h('p', { class: 'ew-help', hidden: true }, t('bo.sample_wait'));
  const collect = (): BlockOptions => {
    const o: BlockOptions = {};
    for (const g of getters) {
      if (!g.responsive) {
        const v = g.get.desktop!();
        if (v !== undefined) o[g.id] = v;
        continue;
      }
      const per: Partial<Record<ViewName, number | string>> = {};
      for (const view of SCHEMA.views) { const v = g.get[view]!(); if (v !== undefined) per[view] = v as number | string; }
      const keys = Object.keys(per);
      if (keys.length === 1 && per.desktop !== undefined) o[g.id] = per.desktop;
      else if (keys.length) o[g.id] = per;
    }
    return o;
  };
  const update = () => {
    const o = collect();
    const ps = problems(o);
    problemsBox.replaceChildren(ps.length
      ? h('div', { class: 'ew-form-alert' }, h('p', null, t('bo.problems', { n: ps.length })), h('ul', null, ...ps.map((p) => h('li', null, t(p.key, p.values ?? {})))))
      : h('p', { class: 'ew-ok' }, t('bo.no_problems')));
    // the sample, styled by the same CSS as the block (scoped to the sample);
    // while there are problems it keeps the look of the page, so nothing on
    // this page is ever shown with too little contrast
    sampleStyle.textContent = ps.length ? '' : optionsCSS('.ew-bo-sample-wrap .ew-bo[data-bo="sample"]', { ...o, hide: [], print: 'show' } as BlockOptions);
    sampleNote.hidden = !ps.length;
    return ps;
  };
  const groups = SCHEMA.groups.map((g, gi) => {
    const items = g.options.map((def) => {
      const v = current[def.id];
      if (!def.responsive) {
        const c = control(def, v, label(def.id), update);
        getters.push({ id: def.id, responsive: false, get: { desktop: c.get } });
        return c.el;
      }
      const per = v !== undefined && typeof v === 'object' && !Array.isArray(v) ? v as Partial<Record<ViewName, number | string>> : { desktop: v as number | string | undefined };
      const get: Partial<Record<ViewName, () => OptionValue | undefined>> = {};
      const els = SCHEMA.views.map((view) => {
        const c = control(def, per[view], `${label(def.id)} – ${viewName(view)}`, update);
        get[view] = c.get;
        return c.el;
      });
      getters.push({ id: def.id, responsive: true, get });
      return h('div', { class: 'ew-bo-views' }, ...els);
    });
    const help = t(`bo.group.${g.id}.help`);
    return h('details', { class: 'ew-bo-group', open: gi === 0 || g.options.some((d) => current[d.id] !== undefined) },
      h('summary', null, t(`bo.group.${g.id}`)),
      help === `bo.group.${g.id}.help` ? null : h('p', { class: 'ew-help' }, help),
      h('div', { class: 'ew-bo-grid' }, ...items));
  });
  update();
  const ok = button(t('bo.apply'), () => {
    const ps = update();
    if (ps.length) { problemsBox.focus(); return; }
    done(collect());
    dlg.close();
  }, { variant: 'primary' });
  const clear = button(t('bo.clear'), () => { done({}); dlg.close(); }, { variant: 'subtle' });
  problemsBox.tabIndex = -1;
  const dlg = openDialog(t('bo.title', { block: blockLabel }), h('div', { class: 'ew-bo-form' },
    h('p', { class: 'ew-help' }, t('bo.help')),
    problemsBox,
    h('div', { class: 'ew-bo-sample-wrap', 'aria-label': t('bo.sample'), role: 'group' }, sampleStyle, sampleNote, sample),
    ...groups), [clear, button(t('common.cancel'), () => dlg.close(), { variant: 'subtle' }), ok], { wide: true });
}

/** The control of the parameter "options" of every block in the content (in
 * the dialog of a macro): a button that opens the options; the value is
 * their JSON. */
export function blockOptionsControl(value: string, ctx: { label: string }): { el: HTMLElement; get: () => string } {
  let current: BlockOptions = {};
  try {
    const v = value ? JSON.parse(value) as unknown : {};
    if (v && typeof v === 'object' && !Array.isArray(v)) current = v as BlockOptions;
  } catch { /* an unreadable value is replaced by the options chosen */ }
  const summary = h('span', { class: 'ew-help' });
  const show = () => {
    const n = Object.keys(current).length;
    summary.textContent = n ? t('bo.set_count', { n }) : t('bo.none_set');
  };
  const open = button(t('bo.edit'), () => optionsDialog(ctx.label || t('bo.edit'), current, (o) => { current = o; show(); }), { variant: 'secondary' });
  show();
  return {
    el: h('div', { class: 'ew-field' }, h('span', { class: 'ew-label' }, ctx.label), h('div', { class: 'ew-actions' }, open, summary)),
    get: () => (Object.keys(current).length ? JSON.stringify(current) : ''),
  };
}
