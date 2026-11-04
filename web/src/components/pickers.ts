// Reusable pickers for categories and elements with filter/search and
// "create new" buttons. Used by menus, blog, FAQ, settings and other plugins
// (exported via the SDK).
import { api, qs } from '../core/api';
import { h, uid } from '../core/dom';
import { pick, t } from '../core/i18n';
import { isAdmin, hasPerm, state } from '../core/state';
import type { ElementView } from '../core/types';
import { toast } from '../core/announce';
import { button, field, formDialog, langFields, select, textInput } from './ui';
import { invalidateTree, langName, type TreeNode } from './widgets';
import { typeLabel } from '../sdk';

// the tree is shared by all pickers on a page for a few seconds
let tree: { at: number; p: Promise<TreeNode[]> } | null = null;
function getTree(): Promise<TreeNode[]> {
  if (!tree || Date.now() - tree.at > 5000) {
    const p = api.get<TreeNode[]>('categories/tree');
    tree = { at: Date.now(), p };
    p.catch(() => { tree = null; });
  }
  return tree.p;
}

const L = () => state.lang;
const D = () => state.site.default_language;
const norm = (s: string) => s.toLocaleLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');

/** Element types whose entries come from a category (they ask for the source). */
const SOURCE_TYPES = new Set(['content.blog', 'content.faq']);

export interface CategorySelect {
  /** filter, select and the "new category" button */
  el: HTMLElement;
  select: HTMLSelectElement;
  value(): number;
  set(id: number): void;
  /** reloads the categories (keeps the selection) */
  reload(): Promise<void>;
  /** a labelled form field (label bound to the select, filter and button next to it) */
  field(label: string, opts?: { help?: string; required?: boolean }): HTMLElement;
}

/**
 * A category select. perm is the permission needed to choose a category
 * ("view" to link to it, "create" to add things to it). With many
 * categories a filter field narrows the list (matches keep their parents).
 * With allowCreate a button creates a new category.
 */
export async function categorySelect(opts: { selected?: number; perm?: string; noneLabel?: string; allowCreate?: boolean; onChange?: (id: number) => void } = {}): Promise<CategorySelect> {
  const perm = opts.perm ?? 'view';
  const sel = h('select', { class: 'ew-input' }) as HTMLSelectElement;
  let nodes: TreeNode[] = [];
  let term = '';
  const draw = (selected: number) => {
    const words = norm(term).split(/\s+/).filter(Boolean);
    const title = (n: TreeNode) => pick(n.titles, L(), D());
    // keep matches and their ancestors (tree order: parents come first)
    const keep = new Set<number>();
    if (words.length) {
      const stack: TreeNode[] = [];
      for (const n of nodes) {
        while (stack.length && stack[stack.length - 1].depth >= n.depth) stack.pop();
        stack.push(n);
        if (words.every((w) => norm(title(n)).includes(w))) stack.forEach((x) => keep.add(x.id));
      }
    }
    sel.replaceChildren();
    if (opts.noneLabel !== undefined) sel.append(h('option', { value: '' }, opts.noneLabel));
    for (const n of nodes) {
      if (words.length && !keep.has(n.id) && n.id !== selected) continue;
      const ok = isAdmin() || n.permissions.includes(perm);
      sel.append(h('option', { value: String(n.id), selected: n.id === selected, disabled: !ok }, `${'— '.repeat(n.depth - 1)}${title(n)}`));
    }
    if (words.length && !selected) {
      const first = Array.from(sel.options).find((o) => o.value && !o.disabled && words.every((w) => norm(o.text).includes(w)));
      if (first) first.selected = true;
    } else if (!selected && opts.noneLabel === undefined) {
      const first = Array.from(sel.options).find((o) => !o.disabled);
      if (first) first.selected = true;
    }
  };
  const fill = async (selected: number) => {
    nodes = await getTree();
    draw(selected);
  };
  await fill(opts.selected ?? 0);
  sel.addEventListener('change', () => opts.onChange?.(Number(sel.value) || 0));
  const wrap = h('div', { class: 'ew-picker' });
  let filter: HTMLInputElement | null = null;
  if (nodes.length > 12) {
    filter = textInput({ type: 'search', placeholder: t('picker.filter') });
    filter.classList.add('ew-picker-filter');
    filter.setAttribute('aria-label', t('picker.filter_categories'));
    filter.addEventListener('input', () => {
      term = filter!.value;
      const before = sel.value;
      draw(0);
      if (sel.value !== before) opts.onChange?.(Number(sel.value) || 0);
    });
  }
  const api_: CategorySelect = {
    el: wrap,
    select: sel,
    value: () => Number(sel.value) || 0,
    set: (id) => { term = ''; if (filter) filter.value = ''; draw(id); },
    reload: () => fill(Number(sel.value) || 0),
    field: (label, o = {}) => {
      const f = field(label, sel, o);
      if (filter) sel.insertAdjacentElement('beforebegin', filter);
      sel.insertAdjacentElement('afterend', wrap); // wrap now holds only the buttons
      return f;
    },
  };
  if (filter) wrap.append(filter);
  wrap.append(sel);
  if (opts.allowCreate) {
    wrap.append(button(t('picker.new_category'), () => newCategory(api_.value(), async (id) => {
      await fill(id);
      opts.onChange?.(id);
      sel.focus();
    }), { variant: 'secondary' }));
  }
  return api_;
}

/** Dialog: creates a category (parent preselected) and calls done with its id. */
export function newCategory(parent: number, done: (id: number) => void | Promise<void>): void {
  void (async () => {
    const parentSel = await categorySelect({ selected: parent, perm: 'manage', noneLabel: hasPerm('core.categories.admin') ? t('category.top_level') : undefined });
    const titles = langFields(t('common.title'), {}, { required: true });
    formDialog(t('category.new'), [parentSel.field(t('category.parent')), titles.el], t('common.create'), async () => {
      const c = await api.post<{ id: number }>('categories', { parent_id: parentSel.value() || null, titles: titles.get() });
      invalidateTree();
      tree = null;
      toast(t('category.created'));
      await done(c.id);
    }, { wide: true });
  })();
}

interface Found { id: number; title: string; type: string; category_id: number; category_path: string[]; short_url: string }

export interface ElementSelect {
  el: HTMLElement;
  value(): number;
}

/**
 * Selects an element of any category: search by title (or short URL),
 * optionally narrowed to a category. With allowCreate a button creates a new
 * element.
 */
export async function elementSelect(opts: { selected?: number; allowCreate?: boolean; type?: string; onChange?: (id: number) => void } = {}): Promise<ElementSelect> {
  let selected = opts.selected ?? 0;
  const searchId = uid('elsearch');
  const q = textInput({ type: 'search', placeholder: t('picker.search_placeholder'), id: searchId });
  const list = h('select', { class: 'ew-input ew-picker-list', size: 8 }) as HTMLSelectElement;
  const listId = uid('ellist');
  list.id = listId;
  const status = h('p', { class: 'ew-help', role: 'status' });
  const cats = await categorySelect({ perm: 'view', noneLabel: t('picker.all_categories'), onChange: () => void search() });
  let current: Found | null = null;
  if (selected) {
    try {
      const v = await api.get<ElementView>(`elements/${selected}`);
      current = { id: v.id, title: v.title, type: v.type, category_id: v.category_id, category_path: [], short_url: v.short_url };
    } catch { /* deleted or hidden */ }
  }
  const label = (x: Found) => `${x.title} – ${typeLabel(x.type)}${x.category_path.length ? ` · ${x.category_path.join(' › ')}` : ''}`;
  let seq = 0;
  const search = async () => {
    const my = ++seq;
    let items: Found[] = [];
    let total = 0;
    try {
      const r = await api.get<{ items: Found[]; total: number }>(`elements${qs({ q: q.value.trim(), category: cats.value() || undefined, type: opts.type, sort: q.value.trim() ? 'title' : 'updated', limit: 50, lang: L() })}`);
      items = r.items;
      total = r.total;
    } catch { /* keep empty */ }
    if (my !== seq) return;
    list.replaceChildren();
    if (current && !items.some((x) => x.id === current!.id)) list.append(h('option', { value: String(current.id), selected: true }, label(current)));
    for (const x of items) list.append(h('option', { value: String(x.id), selected: x.id === selected }, label(x)));
    status.textContent = items.length ? (total > items.length ? t('picker.more_results', { n: items.length, total }) : t('picker.results', { n: items.length })) : t('picker.no_results');
  };
  let timer = 0;
  q.addEventListener('input', () => { clearTimeout(timer); timer = window.setTimeout(() => void search(), 250); });
  list.addEventListener('change', () => {
    selected = Number(list.value) || 0;
    opts.onChange?.(selected);
  });
  await search();
  const wrap = h('div', { class: 'ew-picker ew-picker--element' },
    field(t('picker.search'), q),
    cats.field(t('picker.in_category')),
    field(t('picker.element'), list), status);
  if (opts.allowCreate) {
    wrap.append(h('div', null, button(t('picker.new_element'), () => newElement(cats.value(), async (id) => {
      selected = id;
      current = null;
      try {
        const v = await api.get<ElementView>(`elements/${id}`);
        current = { id: v.id, title: v.title, type: v.type, category_id: v.category_id, category_path: [], short_url: v.short_url };
      } catch { /* ignore */ }
      q.value = '';
      await search();
      opts.onChange?.(id);
      list.focus();
    }), { variant: 'secondary' })));
  }
  return { el: wrap, value: () => selected };
}

/** The category new elements go to when nothing else is known. */
async function defaultCategory(): Promise<number> {
  const nodes = await getTree();
  const can = (n: TreeNode) => isAdmin() || n.permissions.includes('create');
  const home = nodes.find((n) => n.id === state.site.home_category && can(n));
  return (home ?? nodes.find(can))?.id ?? 0;
}

/**
 * Dialog: creates an element and calls done with its id. Only what the type
 * needs is asked: type, title, the category it is stored in (preselected;
 * it decides who may see and edit it) and, for blog and FAQ, the source of
 * the entries (a new category by default).
 */
export function newElement(category: number, done: (id: number, category: number) => void | Promise<void>, opts: { type?: string } = {}): void {
  void (async () => {
    const cats = await categorySelect({ selected: category || await defaultCategory(), perm: 'create', allowCreate: true });
    const types = state.site.element_types;
    const typeSel = select(types.map((x) => ({ value: x.id, label: typeLabel(x.id) })), opts.type ?? 'core.html');
    const title = textInput({ required: true, maxlength: 250 });
    const srcMode = select([{ value: 'new', label: t('picker.source_new') }, { value: 'existing', label: t('picker.source_existing') }], 'new');
    const srcCat = await categorySelect({ perm: 'view', allowCreate: true });
    const fSrcMode = field(t('picker.source'), srcMode, { help: t('picker.source_help') });
    const fSrcCat = srcCat.field(t('picker.source_category'));
    const help = h('p', { class: 'ew-help' });
    const sync = () => {
      const source = SOURCE_TYPES.has(typeSel.value);
      fSrcMode.hidden = !source;
      fSrcCat.hidden = !source || srcMode.value !== 'existing';
      help.textContent = source ? t('picker.new_source_help') : t('picker.new_element_help', { lang: langName(L()) });
    };
    typeSel.addEventListener('change', sync);
    srcMode.addEventListener('change', sync);
    sync();
    formDialog(t('picker.new_element'), [field(t('element.type'), typeSel), field(t('common.title'), title, { required: true }), fSrcMode, fSrcCat,
      cats.field(t('picker.store_in'), { help: t('picker.store_in_help') }), help], t('common.create'), async () => {
      let body: unknown;
      if (SOURCE_TYPES.has(typeSel.value)) {
        const empty = (types.find((x) => x.id === typeSel.value)?.empty_body ?? {}) as Record<string, unknown>;
        // category_id 0: the server creates a category named like the element
        body = { ...empty, category_id: srcMode.value === 'existing' ? srcCat.value() : 0 };
      }
      const el = await api.post<ElementView>('elements', { category_id: cats.value(), type: typeSel.value, lang: L(), title: title.value, body });
      invalidateTree();
      tree = null;
      toast(t('element.created'));
      await done(el.id, cats.value());
    }, { wide: true });
  })();
}
