// Sources of reading plugins and macros (blog, FAQ, news, carousel, lists,
// reports): an editor for the text form "category:12+sub; tag:hr; …" with
// the kinds of the core and those plugins register (other calendars,
// external feeds …). Registered as macro parameter control "sources".
import { h } from '../core/dom';
import { api } from '../core/api';
import { announce } from '../core/announce';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { button, checkbox, select, textInput } from './ui';
import { categorySelect } from './pickers';
import { registerMacroParamControl, type MacroParamContext } from '../editor/macros';

export interface SourceKind {
  kind: string;
  label: () => string;
  /** resolved to elements (offered in every source editor); others only when asked for (feeds, calendars) */
  element?: boolean;
  /** an input for the value after "kind:"; get returns "" for none */
  render: (value: string, ctx: { categoryId: number }) => Promise<{ el: HTMLElement; get: () => string }> | { el: HTMLElement; get: () => string };
}

const kinds: SourceKind[] = [
  {
    kind: 'category', element: true, label: () => t('sources.category'),
    render: async (v, ctx) => {
      const c = await categorySelect({ selected: Number(v.replace('+sub', '')) || ctx.categoryId || undefined });
      c.select.setAttribute('aria-label', t('sources.category'));
      const sub = checkbox(t('sources.sub'), v === '' || v.endsWith('+sub'));
      return { el: h('span', { class: 'ew-source-value' }, c.el, sub.el), get: () => (c.value() > 0 ? `${c.value()}${sub.input.checked ? '+sub' : ''}` : '') };
    },
  },
  {
    kind: 'tag', element: true, label: () => t('sources.tag'),
    render: (v) => { const i = textInput({ value: v, placeholder: 'hr' }); i.setAttribute('aria-label', t('sources.tag')); return { el: i, get: () => i.value.trim().toLowerCase() }; },
  },
  {
    kind: 'type', element: true, label: () => t('sources.type'),
    render: (v) => {
      const s = select(state.site.element_types.map((x) => ({ value: x.id, label: x.id })), v);
      s.setAttribute('aria-label', t('sources.type'));
      return { el: s, get: () => s.value };
    },
  },
  {
    kind: 'elements', element: true, label: () => t('sources.elements'),
    render: (v) => { const i = textInput({ value: v, placeholder: '12, 15' }); i.setAttribute('aria-label', t('sources.elements')); return { el: i, get: () => i.value.replace(/\s+/g, '') }; },
  },
  {
    kind: 'search', element: true, label: () => t('sources.search'),
    render: (v) => { const i = textInput({ value: v, placeholder: t('sources.search_placeholder') }); i.setAttribute('aria-label', t('sources.search')); return { el: i, get: () => i.value.replace(/;/g, ' ').trim() }; },
  },
  {
    kind: 'feed', label: () => t('sources.feed'),
    render: (v) => {
      const i = textInput({ value: v, type: 'url', placeholder: 'https://example.org/feed.xml' });
      i.setAttribute('aria-label', t('sources.feed'));
      return { el: i, get: () => (/^https?:\/\//.test(i.value.trim()) ? i.value.trim() : '') };
    },
  },
];

/** Adds a kind of source (e.g. "feed" for external feeds). */
export function registerSourceKind(k: SourceKind): void {
  const i = kinds.findIndex((x) => x.kind === k.kind);
  if (i >= 0) kinds[i] = k;
  else kinds.push(k);
}

type Zone = 'include' | 'exclude' | 'require';
const ZONES: Zone[] = ['include', 'exclude', 'require'];
const PREFIX: Record<Zone, string> = { include: '', exclude: '-', require: '+' };
const DRAG = 'application/x-ewiki-source';

/**
 * The source builder: a palette of kinds that are added by a click (or
 * Enter) or dragged into the zones "include" (any is enough), "exclude"
 * (never these) and "require" (these as well); entries move between the
 * zones by dragging or with their zone menu (WCAG 2.5.7: nothing needs
 * dragging). A live preview counts the elements with the editor's rights.
 * With kinds: exactly these (e.g. calendars); otherwise all kinds resolved
 * to elements plus extra (e.g. feed).
 */
export function sourcesEditor(label: string, value: string, opts: { categoryId?: number; kinds?: string[]; extra?: string[]; help?: string } = {}): { el: HTMLElement; get: () => string } {
  const allowed = () => kinds.filter((k) => (opts.kinds ? opts.kinds.includes(k.kind) : k.element || (opts.extra?.includes(k.kind) ?? false)));
  const isElement = (kind: string) => kinds.find((k) => k.kind === kind)?.element ?? false;
  // exclusions and requirements only for kinds resolved to elements
  const zoned = allowed().some((k) => k.element);
  interface Row { zone: Zone; kind: string; get: () => string; li: HTMLElement }
  const rows: Row[] = [];
  const lists = {} as Record<Zone, HTMLOListElement>;
  const zoneBoxes = {} as Record<Zone, HTMLElement>;
  const preview = h('p', { class: 'ew-sources-preview', 'aria-live': 'polite' });
  let seq = 0;
  let timer = 0;

  const text = () => ZONES.flatMap((z) => rows.filter((r) => r.zone === z).map((r) => { const v = r.get(); return v ? `${PREFIX[z]}${r.kind}:${v}` : ''; })).filter(Boolean).join('; ');
  const refresh = () => {
    for (const z of ZONES) zoneBoxes[z]?.classList.toggle('is-empty', !rows.some((r) => r.zone === z));
    if (!zoned) return;
    window.clearTimeout(timer);
    timer = window.setTimeout(() => void runPreview(), 400);
  };
  const runPreview = async () => {
    // only kinds resolved to elements (feeds and calendars are not counted)
    const src = ZONES.flatMap((z) => rows.filter((r) => r.zone === z && isElement(r.kind)).map((r) => { const v = r.get(); return v ? `${PREFIX[z]}${r.kind}:${v}` : ''; })).filter(Boolean).join('; ');
    const my = ++seq;
    try {
      const r = await api.post<{ total: number; items: { id: number; title: string }[] }>('sources/preview', { sources: src, category_id: opts.categoryId ?? 0, lang: state.lang });
      if (my !== seq) return;
      preview.replaceChildren(h('strong', null, t('sources.preview_count', { count: r.total })));
      if (r.items.length) preview.append(h('span', null, ` – ${r.items.map((x) => x.title).join(' · ')}${r.total > r.items.length ? ' …' : ''}`));
    } catch (e) {
      if (my === seq) preview.textContent = e instanceof Error ? e.message : String(e);
    }
  };

  const move = (row: Row, zone: Zone, announceIt = true) => {
    if (row.zone === zone) return;
    row.zone = zone;
    lists[zone].append(row.li);
    const sel = row.li.querySelector<HTMLSelectElement>('select.ew-source-zone');
    if (sel) sel.value = zone;
    if (announceIt) announce(t('sources.moved', { zone: t(`sources.zone.${zone}`) }));
    refresh();
  };

  const add = async (kindName: string, v0: string, zone: Zone, focus = false) => {
    const k = kinds.find((x) => x.kind === kindName);
    if (!k) return;
    const id = `src-${++rowSeq}`;
    const li: HTMLElement = h('li', { class: 'ew-source', 'data-row': id });
    const row: Row = { zone, kind: k.kind, get: () => '', li };
    rows.push(row);
    lists[zone].append(li); // place first, so the order of the text form is kept
    const r = await k.render(v0, { categoryId: opts.categoryId ?? 0 });
    row.get = r.get;
    const zoneSel = select(ZONES.map((z) => ({ value: z, label: t(`sources.zone.${z}`) })), zone);
    zoneSel.classList.add('ew-source-zone');
    zoneSel.setAttribute('aria-label', t('sources.zone_of', { kind: k.label() }));
    zoneSel.addEventListener('change', () => move(row, zoneSel.value as Zone));
    const remove = button('×', () => {
      li.remove();
      rows.splice(rows.indexOf(row), 1);
      announce(t('sources.removed', { kind: k.label() }));
      refresh();
    }, { variant: 'subtle', ariaLabel: t('sources.remove_named', { kind: k.label() }) });
    // dragging only from the handle (text in the inputs stays selectable)
    const handle = h('span', { class: 'ew-source-handle', 'aria-hidden': 'true', title: t('sources.drag') }, '⠿');
    handle.addEventListener('pointerdown', () => { li.draggable = true; });
    li.addEventListener('pointerup', () => { li.draggable = false; });
    li.append(handle, h('span', { class: 'ew-source-kind' }, k.label()), h('span', { class: 'ew-source-box' }, r.el));
    if (zoned && isElement(k.kind)) li.append(zoneSel);
    li.append(remove);
    li.addEventListener('dragstart', (ev) => { ev.dataTransfer?.setData(DRAG, JSON.stringify({ row: id })); li.classList.add('is-dragging'); });
    li.addEventListener('dragend', () => { li.classList.remove('is-dragging'); li.draggable = false; });
    li.addEventListener('input', refresh);
    li.addEventListener('change', refresh);
    if (focus) li.querySelector<HTMLElement>('.ew-source-box input, .ew-source-box select')?.focus();
    refresh();
  };

  const zoneEl = (z: Zone) => {
    const list = h('ol', { class: 'ew-sources' });
    lists[z] = list;
    const box = h('fieldset', { class: ['ew-source-zonebox', `ew-source-zonebox--${z}`, 'is-empty'] },
      h('legend', null, t(`sources.zone.${z}`)), h('p', { class: 'ew-help' }, t(`sources.zone_help.${z}`)), list,
      h('p', { class: 'ew-source-drop', 'aria-hidden': 'true' }, t('sources.drop_here')));
    box.addEventListener('dragover', (ev) => { if (ev.dataTransfer?.types.includes(DRAG)) { ev.preventDefault(); box.classList.add('is-over'); } });
    box.addEventListener('dragleave', () => box.classList.remove('is-over'));
    box.addEventListener('drop', (ev) => {
      box.classList.remove('is-over');
      const raw = ev.dataTransfer?.getData(DRAG);
      if (!raw) return;
      ev.preventDefault();
      const d = JSON.parse(raw) as { kind?: string; row?: string };
      if (d.row) {
        const row = rows.find((r) => r.li.dataset.row === d.row);
        if (row && (z === 'include' || isElement(row.kind))) move(row, z);
      } else if (d.kind && (z === 'include' || isElement(d.kind))) {
        void add(d.kind, '', z, true);
        announce(t('sources.added', { kind: kinds.find((k) => k.kind === d.kind)?.label() ?? d.kind, zone: t(`sources.zone.${z}`) }));
      }
    });
    zoneBoxes[z] = box;
    return box;
  };

  // the palette: click adds to "include"; drag into any zone
  const palette = h('ul', { class: 'ew-source-palette', 'aria-label': t('sources.palette') },
    ...allowed().map((k) => {
      const b = button(h('span', null, h('span', { 'aria-hidden': 'true' }, '+ '), k.label()), () => {
        void add(k.kind, '', 'include', true);
        announce(t('sources.added', { kind: k.label(), zone: t('sources.zone.include') }));
      }, { variant: 'secondary', ariaLabel: t('sources.add_named', { kind: k.label() }) });
      b.draggable = true;
      b.addEventListener('dragstart', (ev) => ev.dataTransfer?.setData(DRAG, JSON.stringify({ kind: k.kind })));
      return h('li', null, b);
    }));
  const zonesEl = h('div', { class: 'ew-source-zones' }, zoneEl('include'), zoned ? zoneEl('exclude') : null, zoned ? zoneEl('require') : null);

  for (const part of value.split(';').map((x) => x.trim()).filter(Boolean)) {
    let zone: Zone = 'include';
    let p = part;
    if (p.startsWith('-')) { zone = 'exclude'; p = p.slice(1).trim(); } else if (p.startsWith('+')) { zone = 'require'; p = p.slice(1).trim(); }
    const at = p.indexOf(':');
    if (at <= 0) continue;
    void add(p.slice(0, at), p.slice(at + 1), zoned ? zone : 'include');
  }
  const el = h('fieldset', { class: ['ew-fieldset', 'ew-source-builder'] }, h('legend', null, label), h('p', { class: 'ew-help' }, opts.help ?? t('sources.help')),
    h('div', { class: 'ew-source-layout' }, h('div', { class: 'ew-source-palettebox' }, h('p', { class: 'ew-source-palette-title' }, t('sources.palette')), palette), zonesEl),
    zoned ? preview : null);
  refresh();
  return { el, get: text };
}

let rowSeq = 0;

/** Macro parameter controls: "sources" (elements) and "sources.feeds" (with external feeds). */
export function registerSourceControls(): void {
  registerMacroParamControl('sources', (value: string, ctx: MacroParamContext) => sourcesEditor(ctx.label, value, { categoryId: ctx.categoryId }));
  registerMacroParamControl('sources.feeds', (value: string, ctx: MacroParamContext) => sourcesEditor(ctx.label, value, { categoryId: ctx.categoryId, extra: ['feed'] }));
}

