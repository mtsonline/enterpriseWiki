// Sources of reading plugins and macros (blog, FAQ, news, carousel, lists,
// reports): an editor for the text form "category:12+sub; tag:hr; …" with
// the kinds of the core and those plugins register (other calendars,
// external feeds …). Registered as macro parameter control "sources".
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { button, checkbox, select, textInput } from './ui';
import { categorySelect } from './pickers';
import { registerMacroParamControl, type MacroParamContext } from '../editor/macros';

export interface SourceKind {
  kind: string;
  label: () => string;
  /** an input for the value after "kind:"; get returns "" for none */
  render: (value: string, ctx: { categoryId: number }) => Promise<{ el: HTMLElement; get: () => string }> | { el: HTMLElement; get: () => string };
}

const kinds: SourceKind[] = [
  {
    kind: 'category', label: () => t('sources.category'),
    render: async (v, ctx) => {
      const c = await categorySelect({ selected: Number(v.replace('+sub', '')) || ctx.categoryId || undefined });
      c.select.setAttribute('aria-label', t('sources.category'));
      const sub = checkbox(t('sources.sub'), v === '' || v.endsWith('+sub'));
      return { el: h('span', { class: 'ew-source-value' }, c.el, sub.el), get: () => (c.value() > 0 ? `${c.value()}${sub.input.checked ? '+sub' : ''}` : '') };
    },
  },
  {
    kind: 'tag', label: () => t('sources.tag'),
    render: (v) => { const i = textInput({ value: v, placeholder: 'hr' }); i.setAttribute('aria-label', t('sources.tag')); return { el: i, get: () => i.value.trim().toLowerCase() }; },
  },
  {
    kind: 'type', label: () => t('sources.type'),
    render: (v) => {
      const s = select(state.site.element_types.map((x) => ({ value: x.id, label: x.id })), v);
      s.setAttribute('aria-label', t('sources.type'));
      return { el: s, get: () => s.value };
    },
  },
  {
    kind: 'elements', label: () => t('sources.elements'),
    render: (v) => { const i = textInput({ value: v, placeholder: '12, 15' }); i.setAttribute('aria-label', t('sources.elements')); return { el: i, get: () => i.value.replace(/\s+/g, '') }; },
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

/** An editor of sources; only offers the kinds listed (default: the element kinds). */
export function sourcesEditor(label: string, value: string, opts: { categoryId?: number; kinds?: string[]; help?: string } = {}): { el: HTMLElement; get: () => string } {
  const allowed = () => kinds.filter((k) => (opts.kinds ? opts.kinds.includes(k.kind) : k.kind !== 'feed'));
  const list = h('ol', { class: 'ew-sources' });
  const rows: { get: () => string }[] = [];
  const add = async (src: string) => {
    const at = src.indexOf(':');
    const k0 = at > 0 ? src.slice(0, at) : allowed()[0]?.kind ?? 'category';
    const v0 = at > 0 ? src.slice(at + 1) : '';
    const kind = select(allowed().map((k) => ({ value: k.kind, label: k.label() })), k0);
    kind.setAttribute('aria-label', t('sources.kind'));
    const box = h('span', { class: 'ew-source-box' });
    let get = () => '';
    const draw = async (v: string) => {
      const k = kinds.find((x) => x.kind === kind.value);
      if (!k) return;
      const r = await k.render(v, { categoryId: opts.categoryId ?? 0 });
      box.replaceChildren(r.el);
      get = r.get;
    };
    kind.addEventListener('change', () => void draw(''));
    await draw(v0);
    const entry = { get: () => { const v = get(); return v ? `${kind.value}:${v}` : ''; } };
    const li: HTMLElement = h('li', null, kind, box, button(t('common.remove'), () => { li.remove(); rows.splice(rows.indexOf(entry), 1); }, { variant: 'subtle', ariaLabel: t('sources.remove') }));
    rows.push(entry);
    list.append(li);
  };
  for (const s of value.split(';').map((x) => x.trim()).filter(Boolean)) void add(s);
  const el = h('fieldset', { class: 'ew-fieldset' }, h('legend', null, label), h('p', { class: 'ew-help' }, opts.help ?? t('sources.help')), list,
    button(t('sources.add'), () => void add(''), { variant: 'secondary' }));
  return { el, get: () => rows.map((r) => r.get()).filter(Boolean).join('; ') };
}

/** Macro parameter controls: "sources" (elements) and "sources.feeds" (with external feeds). */
export function registerSourceControls(): void {
  registerMacroParamControl('sources', (value: string, ctx: MacroParamContext) => sourcesEditor(ctx.label, value, { categoryId: ctx.categoryId, kinds: ['category', 'tag', 'type', 'elements'] }));
  registerMacroParamControl('sources.feeds', (value: string, ctx: MacroParamContext) => sourcesEditor(ctx.label, value, { categoryId: ctx.categoryId, kinds: ['category', 'tag', 'type', 'elements', 'feed'] }));
}

