// Controls of the macro dialog for the property report: sources, filters
// with operators fitting the type, columns, sorting and keys. They write the
// short text parameters of the macro (readable in Markdown).
import { api, button, checkbox, field, h, qs, registerMacroParamControl, registerSourceKind, select, sourcesEditor, state, t, textArea, textInput, uid, type MacroParamContext } from '@ewiki/sdk';
import { BASE, builtinLabel, type Def } from './model';

interface Key { key: string; label: string; type: string }

const cache = new Map<number, Promise<Key[]>>();
function keys(categoryId: number): Promise<Key[]> {
  let p = cache.get(categoryId);
  if (!p) {
    p = (async () => {
      const [used, defs] = await Promise.all([
        api.get<Key[]>(`${BASE}/keys${qs({ category: categoryId || undefined, lang: state.lang })}`).catch(() => [] as Key[]),
        api.get<{ defs: Def[] }>(`${BASE}/defs${qs({ category: categoryId || undefined, lang: state.lang })}`).catch(() => ({ defs: [] as Def[] })),
      ]);
      const out = new Map<string, Key>();
      for (const d of defs.defs) out.set(d.key, { key: d.key, label: d.labels[state.lang] || d.labels.en || d.key, type: d.type });
      for (const u of used) if (!out.has(u.key)) out.set(u.key, u);
      const builtins: Key[] = ['title', 'tags', 'type', 'category', 'created_at', 'updated_at', 'created_by', 'updated_by'].map((k) => ({
        key: k, label: builtinLabel(k), type: { tags: 'multiselect', category: 'category', created_at: 'datetime', updated_at: 'datetime', created_by: 'user', updated_by: 'user' }[k] ?? 'text',
      }));
      return [...builtins, ...[...out.values()].sort((a, b) => a.label.localeCompare(b.label))];
    })();
    cache.set(categoryId, p);
  }
  return p;
}

const keyOptions = (list: Key[], empty?: string) => [...(empty !== undefined ? [{ value: '', label: empty }] : []), ...list.map((k) => ({ value: k.key, label: k.key === k.label ? k.key : `${k.label} (${k.key})` }))];

/** Operators by the kind of type. */
function ops(type: string): string[] {
  if (['integer', 'decimal', 'percent', 'currency', 'unit', 'rating', 'progress', 'duration', 'formula'].includes(type)) return ['eq', 'ne', 'lt', 'le', 'gt', 'ge', 'between', 'empty', 'notempty'];
  if (['date', 'datetime', 'daterange', 'created_at', 'updated_at'].includes(type)) return ['eq', 'lt', 'le', 'gt', 'ge', 'between', 'empty', 'notempty'];
  if (['multiselect'].includes(type)) return ['has', 'hasall', 'hasnot', 'empty', 'notempty'];
  if (['user', 'group', 'element', 'category'].includes(type)) return ['me', 'has', 'hasnot', 'empty', 'notempty'];
  if (type === 'boolean') return ['eq', 'empty', 'notempty'];
  return ['eq', 'ne', 'in', 'nin', 'contains', 'starts', 'empty', 'notempty'];
}
const opText: Record<string, string> = { eq: '=', ne: '!=', lt: '<', le: '<=', gt: '>', ge: '>=', in: 'in', nin: 'not in', has: 'has', hasall: 'hasall', hasnot: 'hasnot', contains: 'contains', starts: 'starts', between: 'between', empty: 'empty', notempty: 'notempty', me: '=' };

function parseFilter(s: string): { key: string; op: string; value: string } | null {
  const m = /^\s*([a-z][a-z0-9_]*)\s*(!=|<=|>=|=|<|>| not in | in | hasall | hasnot | has | contains | starts | between | empty$| notempty$)\s*(.*)$/.exec(` ${s} `.replace(/^ /, ''));
  if (!m) return null;
  let op = Object.entries(opText).find(([, v]) => v === m[2].trim())?.[0] ?? 'eq';
  if (op === 'eq' && m[3].trim() === 'me') op = 'me';
  return { key: m[1], op, value: op === 'me' ? '' : m[3].trim() };
}

function filterControl(value: string, ctx: MacroParamContext): { el: HTMLElement; get: () => string } {
  const list = h('ol', { class: 'ewprop-cfg-list' });
  const rows: { get: () => string }[] = [];
  const raw = textArea({ value, rows: 2 });
  raw.classList.add('ewe-mono');
  const rawBox = h('details', { class: 'ewprop-cfg-raw' }, h('summary', null, t('props.flt.as_text')), field(t('props.flt.text'), raw, { help: t('props.flt.text_help') }));
  let useRaw = false;
  raw.addEventListener('input', () => { useRaw = true; });
  const add = async (f: { key: string; op: string; value: string } | null) => {
    const all = await keys(ctx.categoryId);
    const key = select(keyOptions(all), f?.key ?? all[0]?.key);
    key.setAttribute('aria-label', t('props.flt.key'));
    const op = select([]);
    op.setAttribute('aria-label', t('props.flt.op'));
    const val = textInput({ value: f?.value ?? '' });
    val.setAttribute('aria-label', t('props.flt.value'));
    const hint = h('span', { class: 'ew-help' });
    const sync = (keep?: string) => {
      const type = all.find((k) => k.key === key.value)?.type ?? 'text';
      op.replaceChildren(...ops(type).map((o) => h('option', { value: o, selected: o === (keep ?? op.value) }, t(`props.op.${o}`))));
      val.hidden = ['empty', 'notempty', 'me'].includes(op.value);
      hint.textContent = ['date', 'datetime', 'daterange', 'created_at', 'updated_at'].includes(type) ? t('props.flt.date_hint') : op.value === 'between' ? t('props.flt.between_hint') : ['in', 'nin', 'has', 'hasall', 'hasnot'].includes(op.value) ? t('props.flt.list_hint') : '';
    };
    key.addEventListener('change', () => sync());
    op.addEventListener('change', () => sync(op.value));
    sync(f?.op ?? 'eq');
    const entry = {
      get: () => {
        if (['empty', 'notempty'].includes(op.value)) return `${key.value} ${op.value}`;
        if (op.value === 'me') return `${key.value} = me`;
        if (!val.value.trim()) return '';
        const o = opText[op.value];
        return /^[a-z]/.test(o) ? `${key.value} ${o} ${val.value.trim()}` : `${key.value} ${o} ${val.value.trim()}`;
      },
    };
    const li: HTMLElement = h('li', null, key, op, val, hint, button(t('common.remove'), () => { li.remove(); rows.splice(rows.indexOf(entry), 1); }, { variant: 'subtle', ariaLabel: t('props.flt.remove') }));
    rows.push(entry);
    list.append(li);
  };
  const parsed = value.split(';').map((x) => x.trim()).filter(Boolean).map(parseFilter);
  if (parsed.some((x) => !x)) useRaw = true;
  else for (const f of parsed) void add(f);
  const el = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, ctx.label), list, button(t('props.flt.add'), () => void add(null), { variant: 'secondary' }), rawBox);
  return { el, get: () => (useRaw ? raw.value.trim() : rows.map((r) => r.get()).filter(Boolean).join('; ')) };
}

function columnsControl(value: string, ctx: MacroParamContext): { el: HTMLElement; get: () => string } {
  const chosen = value.split(',').map((x) => x.trim()).filter(Boolean);
  const box = h('div', { class: 'ewprop-cfg-cols' }, h('p', { class: 'ew-help' }, t('common.loading')));
  let boxes: { key: string; c: ReturnType<typeof checkbox> }[] = [];
  void keys(ctx.categoryId).then((all) => {
    const ordered = [...chosen.map((k) => all.find((x) => x.key === k)).filter((x): x is Key => !!x), ...all.filter((x) => !chosen.includes(x.key))];
    boxes = ordered.map((k) => ({ key: k.key, c: checkbox(k.key === k.label ? k.key : `${k.label} (${k.key})`, chosen.includes(k.key)) }));
    box.replaceChildren(...boxes.map((b) => b.c.el));
  });
  const id = uid('cols');
  return {
    el: h('fieldset', { class: 'ew-fieldset', id }, h('legend', null, ctx.label), h('p', { class: 'ew-help' }, t('props.cols_help')), box),
    get: () => (boxes.length ? boxes.filter((b) => b.c.input.checked).map((b) => b.key).join(',') : value),
  };
}

function sortControl(value: string, ctx: MacroParamContext): { el: HTMLElement; get: () => string } {
  const parts = value.split(',').map((x) => x.trim()).filter(Boolean);
  const box = h('div', { class: 'ewprop-cfg-sort' });
  const sels: { key: HTMLSelectElement; dir: HTMLSelectElement }[] = [];
  void keys(ctx.categoryId).then((all) => {
    for (let i = 0; i < 2; i++) {
      const cur = parts[i] ?? '';
      const key = select(keyOptions(all, '—'), cur.replace(/^-/, ''));
      key.setAttribute('aria-label', i ? t('props.sort_then') : t('props.sort_first'));
      const dir = select([{ value: '', label: t('props.asc') }, { value: '-', label: t('props.desc') }], cur.startsWith('-') ? '-' : '');
      dir.setAttribute('aria-label', t('props.direction'));
      sels.push({ key, dir });
      box.append(h('div', { class: 'ewprop-cfg-row' }, key, dir));
    }
  });
  return {
    el: h('fieldset', { class: 'ew-fieldset' }, h('legend', null, ctx.label), box),
    get: () => (sels.length ? sels.filter((s) => s.key.value).map((s) => s.dir.value + s.key.value).join(',') : value),
  };
}

function keyControl(value: string, ctx: MacroParamContext): { el: HTMLElement; get: () => string } {
  const sel = select([{ value, label: value || '—' }], value);
  void keys(ctx.categoryId).then((all) => {
    sel.replaceChildren(...keyOptions(all, '—').map((o) => h('option', { value: o.value, selected: o.value === value }, o.label)));
  });
  return { el: field(ctx.label, sel), get: () => sel.value };
}

export function registerControls(): void {
  registerMacroParamControl('properties.source', (value, ctx) => sourcesEditor(ctx.label, value, { categoryId: ctx.categoryId }));
  registerMacroParamControl('properties.filter', filterControl);
  registerMacroParamControl('properties.columns', columnsControl);
  registerMacroParamControl('properties.sort', sortControl);
  registerMacroParamControl('properties.key', keyControl);
}

// source "props:<filter>": elements whose properties match (for every list of several sources)
registerSourceKind({
  kind: 'props', element: true, label: () => t('props.source_kind'),
  render: (v) => {
    // shown with ";" as in reports; stored with "&&" (";" separates the sources)
    const i = textInput({ value: v.split('&&').map((x) => x.trim()).join('; '), placeholder: 'status = open; due <= today+7d' });
    i.setAttribute('aria-label', t('props.source_kind'));
    return { el: i, get: () => i.value.split(';').map((x) => x.trim()).filter(Boolean).join(' && ') };
  },
});
