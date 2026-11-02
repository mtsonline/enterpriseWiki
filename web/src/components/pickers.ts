// Reusable pickers for categories and elements with "create new" buttons.
// Used by menus, blog, FAQ and other plugins (exported via the SDK).
import { api } from '../core/api';
import { h } from '../core/dom';
import { pick, t } from '../core/i18n';
import { isAdmin, state } from '../core/state';
import type { ElementSummary, ElementView } from '../core/types';
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

export interface CategorySelect {
  /** the select plus the "new category" button */
  el: HTMLElement;
  select: HTMLSelectElement;
  value(): number;
  set(id: number): void;
  /** reloads the categories (keeps the selection) */
  reload(): Promise<void>;
  /** a labelled form field (label bound to the select, button next to it) */
  field(label: string, opts?: { help?: string; required?: boolean }): HTMLElement;
}

/**
 * A category select. perm is the permission needed to choose a category
 * ("view" to link to it, "create" to add things to it). With allowCreate a
 * button creates a new category (below the selected one if wanted).
 */
export async function categorySelect(opts: { selected?: number; perm?: string; noneLabel?: string; allowCreate?: boolean; label?: string; onChange?: (id: number) => void } = {}): Promise<CategorySelect> {
  const perm = opts.perm ?? 'view';
  const sel = h('select', { class: 'ew-input' }) as HTMLSelectElement;
  const fill = async (selected: number) => {
    const nodes = await getTree();
    sel.replaceChildren();
    if (opts.noneLabel !== undefined) sel.append(h('option', { value: '' }, opts.noneLabel));
    for (const n of nodes) {
      const ok = isAdmin() || n.permissions.includes(perm);
      sel.append(h('option', { value: String(n.id), selected: n.id === selected, disabled: !ok },
        `${'— '.repeat(n.depth - 1)}${pick(n.titles, L(), D())}`));
    }
    if (!selected && opts.noneLabel === undefined) {
      const first = Array.from(sel.options).find((o) => !o.disabled);
      if (first) first.selected = true;
    }
  };
  await fill(opts.selected ?? 0);
  sel.addEventListener('change', () => opts.onChange?.(Number(sel.value) || 0));
  const api_: CategorySelect = {
    el: h('div', { class: 'ew-picker' }, sel),
    select: sel,
    value: () => Number(sel.value) || 0,
    set: (id) => { sel.value = String(id); },
    reload: () => fill(Number(sel.value) || 0),
    field: (label, o = {}) => {
      const f = field(label, sel, o);
      sel.insertAdjacentElement('afterend', api_.el); // el now holds only the buttons
      return f;
    },
  };
  if (opts.allowCreate) {
    api_.el.append(button(t('picker.new_category'), () => newCategory(api_.value(), async (id) => {
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
    const parentSel = await categorySelect({ selected: parent, perm: 'create', noneLabel: isAdmin() ? t('category.top_level') : undefined });
    const titles = langFields(t('common.title'), {}, { required: true });
    formDialog(t('category.new'), [field(t('category.parent'), parentSel.select), titles.el], t('common.create'), async () => {
      const c = await api.post<{ id: number }>('categories', { parent_id: parentSel.value() || null, titles: titles.get() });
      invalidateTree();
      tree = null;
      toast(t('category.created'));
      await done(c.id);
    }, { wide: true });
  })();
}

export interface ElementSelect {
  el: HTMLElement;
  value(): number;
  category(): number;
}

/**
 * Selects an element: first the category, then the element in it. With
 * allowCreate a button creates a new element in the chosen category.
 */
export async function elementSelect(opts: { selected?: number; categoryId?: number; allowCreate?: boolean; onChange?: (id: number) => void } = {}): Promise<ElementSelect> {
  let catId = opts.categoryId ?? 0;
  if (opts.selected && !catId) {
    try { catId = (await api.get<ElementView>(`elements/${opts.selected}`)).category_id; } catch { /* deleted or hidden */ }
  }
  const elSel = h('select', { class: 'ew-input' }) as HTMLSelectElement;
  const fillElements = async (cat: number, selected: number) => {
    elSel.replaceChildren(h('option', { value: '' }, t('picker.choose_element')));
    if (!cat) return;
    try {
      const r = await api.get<{ elements: ElementSummary[] }>(`categories/${cat}`);
      for (const e of r.elements) {
        elSel.append(h('option', { value: String(e.id), selected: e.id === selected }, `${pick(e.titles, L(), D())} (${typeLabel(e.type)})`));
      }
    } catch { /* no access */ }
  };
  const cats = await categorySelect({ selected: catId, perm: 'view', noneLabel: t('picker.choose_category'), onChange: (id) => void fillElements(id, 0) });
  await fillElements(catId, opts.selected ?? 0);
  elSel.addEventListener('change', () => opts.onChange?.(Number(elSel.value) || 0));
  const wrap = h('div', { class: 'ew-picker ew-picker--element' },
    field(t('picker.category'), cats.select), field(t('picker.element'), elSel));
  if (opts.allowCreate) {
    wrap.append(button(t('picker.new_element'), () => newElement(cats.value(), async (id, cat) => {
      cats.set(cat);
      await fillElements(cat, id);
      opts.onChange?.(id);
      elSel.focus();
    }), { variant: 'secondary' }));
  }
  return { el: wrap, value: () => Number(elSel.value) || 0, category: () => cats.value() };
}

/** Dialog: creates an (empty) element and calls done with its id. */
export function newElement(category: number, done: (id: number, category: number) => void | Promise<void>, opts: { type?: string } = {}): void {
  void (async () => {
    const cats = await categorySelect({ selected: category, perm: 'create', allowCreate: true });
    const types = state.site.element_types;
    const typeSel = select(types.map((x) => ({ value: x.id, label: typeLabel(x.id) })), opts.type ?? 'core.html');
    const title = textInput({ required: true, maxlength: 250 });
    formDialog(t('element.new'), [cats.field(t('picker.category')), field(t('element.type'), typeSel), field(t('common.title'), title, { required: true }),
      h('p', { class: 'ew-help' }, t('picker.new_element_help', { lang: langName(L()) }))], t('common.create'), async () => {
      const el = await api.post<ElementView>('elements', { category_id: cats.value(), type: typeSel.value, lang: L(), title: title.value });
      invalidateTree();
      toast(t('element.created'));
      await done(el.id, cats.value());
    });
  })();
}
