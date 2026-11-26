// The blocks of a category page: title, management, watching, page
// actions, description, featured pages, the overview of the pages in the
// display of the category (list, cards, tiles, tree, handbook) and the
// subcategories, at the positions of the category page (category-…). The
// handbook navigation is a block of the site (side column) on the category
// page and on its pages.
import { api, qs } from '../core/api';
import { h, fmtDate, trustedHTML, uid } from '../core/dom';
import { pick, t } from '../core/i18n';
import { navigate } from '../core/router';
import { state } from '../core/state';
import type { Category, CategoryOverview, ElementSummary, ElementView } from '../core/types';
import { button } from '../components/ui';
import { ctx as pageCtx } from '../components/widgets';
import { announce } from '../core/announce';
import { currentPlacementLevels, listOverviewColumns, pageActionList, registerSlot, typeLabel, type PageContext } from '../coreplugin';

const L = () => state.lang;
const D = () => state.site.default_language;

/** The data of the category page shown (PageContext.category). */
export interface CategoryPage {
  category: Category;
  elements: ElementSummary[];
  subs: Category[];
}

/** The category page shown: its featured pages are blocks of their own. */
let shown: CategoryPage | null = null;

export function setShownCategory(c: CategoryPage | null): void {
  shown = c;
}

const FEATURED = 'core.category.element';

/** Featured pages of the page shown: the default page of the category (in
 * the content), the pages placed on the category page and the other pages
 * of the category (not placed: offered in the arrangement). */
async function featuredInstances(): Promise<{ key: string; label: string; position: string; order?: number }[]> {
  if (!shown) return [];
  const c = shown.category;
  const titleOf = new Map(shown.elements.map((e) => [String(e.id), pick(e.titles, L(), e.original_lang)]));
  const keys = new Set<string>();
  if (c.default_element_id) keys.add(String(c.default_element_id));
  for (const l of await currentPlacementLevels()) {
    if (l.level !== 'category_page' || l.target !== String(c.id)) continue;
    for (const p of l.placements) if (p.block.startsWith(`${FEATURED}/`)) keys.add(p.block.slice(FEATURED.length + 1));
  }
  for (const e of shown.elements.slice(0, 200)) keys.add(String(e.id));
  return [...keys].map((key) => ({
    key, label: t('block.core.category.element', { title: titleOf.get(key) ?? `#${key}` }),
    // the default page of the category: in the content (as before), the others not placed
    position: key === String(c.default_element_id) ? 'category-content' : '', order: 20,
  }));
}

/** Loads the featured pages of the page shown once (one request). */
let featuredLoad: { key: string; asked: Set<number>; views: Promise<Map<number, ElementView>> } | null = null;
function featuredView(id: number): Promise<ElementView | undefined> {
  const key = `${shown?.category.id}|${L()}`;
  if (!featuredLoad || featuredLoad.key !== key) {
    const asked = new Set<number>();
    featuredLoad = {
      key, asked,
      views: (async () => {
        if (shown?.category.default_element_id) asked.add(shown.category.default_element_id);
        for (const l of await currentPlacementLevels()) {
          if (l.level !== 'category_page') continue;
          for (const p of l.placements) if (p.position && p.block.startsWith(`${FEATURED}/`)) asked.add(Number(p.block.slice(FEATURED.length + 1)));
        }
        if (!asked.size) return new Map<number, ElementView>();
        const res = await api.get<{ items: ElementView[] }>(`elements/batch${qs({ ids: [...asked].join(','), lang: L() })}`);
        return new Map(res.items.map((v) => [v.id, v]));
      })().catch(() => new Map<number, ElementView>()),
    };
  }
  const load = featuredLoad;
  // a page asked for but missing may not be viewed: no second request
  return load.views.then((m) => m.get(id) ?? (load.asked.has(id) ? undefined : single(id)));
}

/** A featured page that was not in the first request (placed afterwards). */
async function single(id: number): Promise<ElementView | undefined> {
  try { return await api.get<ElementView>(`elements/${id}${qs({ lang: L() })}`); } catch { return undefined; }
}

/** Forgets the loaded featured pages (after arranging the page). */
export function forgetFeatured(): void {
  featuredLoad = null;
}

function featured(el: ElementView, title: string, edit: boolean): HTMLElement {
  const elEdit = edit && el.permissions?.includes('edit');
  const id = uid('feat');
  return h('section', { class: ['ew-default-element', elEdit && 'ew-default-element--edit'], 'aria-labelledby': id },
    h('h2', { id, class: el.title === title && !elEdit ? 'sr-only' : undefined }, el.title),
    elEdit ? h('div', { class: 'ew-actions', role: 'group', 'aria-label': t('element.page_actions', { title: el.title }) },
      h('a', { href: `/e/${el.id}/edit?lang=${L()}`, class: 'ew-btn ew-btn--primary' }, t('element.edit_page')),
      h('a', { href: `/e/${el.id}/history`, class: 'ew-btn ew-btn--secondary' }, t('element.history'))) : null,
    trustedHTML(el.html),
    h('p', { class: 'ew-meta' },
      h('a', { href: `/e/${el.id}` }, t('element.open_page')), ' · ',
      t('element.updated', { date: fmtDate(el.updated_at, L()) })));
}

// ---------------------------------------------------------------- overview

type Item = CategoryOverview['items'][number];

/** Only pictures of the wiki (no requests to other servers when reading a list). */
function localImage(src: string): string {
  return src.startsWith('/') && !src.startsWith('//') ? src : '';
}

function itemTitle(x: Item): string {
  return pick(x.titles, L(), x.original_lang) || t('element.untitled');
}

function pager(o: CategoryOverview): HTMLElement | null {
  if (o.pages <= 1) return null;
  const go = (n: number) => {
    const q = new URLSearchParams(location.search);
    if (n > 1) q.set('p', String(n));
    else q.delete('p');
    navigate(`${location.pathname}${q.size ? `?${q}` : ''}`);
  };
  return h('nav', { class: 'ew-pager', 'aria-label': t('category.pages_nav') },
    o.page > 1 ? button(t('common.previous'), () => go(o.page - 1), { variant: 'secondary' }) : null,
    h('span', { class: 'ew-meta', 'aria-current': 'page' }, ` ${t('category.page_of', { n: o.page, of: o.pages })} `),
    o.page < o.pages ? button(t('common.next'), () => go(o.page + 1), { variant: 'secondary' }) : null);
}

function listView(o: CategoryOverview, cp: CategoryPage, edit: boolean): HTMLElement {
  const canEdit = edit && !!cp.category.permissions?.includes('edit');
  return h('ul', { class: 'ew-list ew-overview-list' }, ...o.items.map((x) => h('li', null,
    h('a', { href: `/e/${x.id}` }, itemTitle(x)),
    canEdit ? [' ', h('a', { href: `/e/${x.id}/edit?lang=${L()}`, class: 'ew-inline-action', 'aria-label': t('element.edit_named', { title: itemTitle(x) }) }, t('common.edit'))] : null,
    edit ? [' ', h('span', { class: 'ew-badge' }, typeLabel(x.type))] : null,
    edit && x.id === cp.category.default_element_id ? h('span', { class: 'ew-badge ew-badge--info' }, t('element.default')) : null,
    x.hidden ? h('span', { class: 'ew-badge ew-badge--warn' }, t('element.hidden_badge')) : null,
    h('span', { class: 'ew-meta' }, ` · ${fmtDate(x.updated_at, L())}`),
    x.excerpt ? h('p', { class: 'ew-excerpt' }, x.excerpt) : null)));
}

function cardsView(o: CategoryOverview, tiles: boolean): HTMLElement {
  return h('ul', { class: tiles ? 'ew-tiles' : 'ew-cards ew-overview-cards' }, ...o.items.map((x) => {
    const img = localImage(x.preview_image);
    // the picture belongs to the link of the page (its title names it): no own text
    return h('li', { class: tiles ? 'ew-tile-item' : 'ew-card' },
      img ? h('img', { src: img, alt: '', loading: 'lazy', class: tiles ? 'ew-tile-img' : 'ew-card-img' }) : null,
      h('h3', null, h('a', { href: `/e/${x.id}` }, itemTitle(x))),
      !tiles && x.excerpt ? h('p', null, x.excerpt) : null,
      h('p', { class: 'ew-meta' }, fmtDate(x.updated_at, L())));
  }));
}

/** The tree: the pages of the category and its subcategories to open (their
 * content is loaded when opened). */
function treeView(o: CategoryOverview, subs: Category[]): HTMLElement {
  const pages = (items: Item[]) => items.map((x) => h('li', { class: 'ew-tree-page' }, h('a', { href: `/e/${x.id}` }, itemTitle(x))));
  const node = (c: Category): HTMLElement => {
    const title = pick(c.titles, L(), D());
    const inner = h('ul', { class: 'ew-tree-children', hidden: true });
    let loaded = false;
    const toggle = button(title, () => void (async () => {
      const open = toggle.getAttribute('aria-expanded') !== 'true';
      toggle.setAttribute('aria-expanded', String(open));
      inner.hidden = !open;
      if (!open || loaded) return;
      loaded = true;
      try {
        const [ov, kids] = await Promise.all([api.get<CategoryOverview>(`categories/${c.id}/overview${qs({ lang: L(), view: 'tree' })}`), api.get<Category[]>(`categories?parent=${c.id}`)]);
        inner.replaceChildren(...kids.map(node), ...pages(ov.items),
          ov.pages > 1 ? h('li', null, h('a', { href: `/c/${c.id}` }, t('category.all_pages', { n: ov.total }))) : '');
        if (!kids.length && !ov.items.length) inner.append(h('li', { class: 'ew-muted' }, t('category.empty')));
        announce(t('category.tree_loaded', { title }));
      } catch {
        loaded = false;
        inner.replaceChildren(h('li', { class: 'ew-muted' }, t('error.title')));
      }
    })(), { variant: 'subtle' });
    toggle.setAttribute('aria-expanded', 'false');
    toggle.classList.add('ew-tree-toggle');
    return h('li', { class: 'ew-tree-cat' }, toggle, ' ', h('a', { href: `/c/${c.id}`, class: 'ew-tree-open', 'aria-label': t('category.open_named', { title }) }, '→'), inner);
  };
  return h('ul', { class: 'ew-tree-view' }, ...subs.map(node), ...pages(o.items));
}

// ---------------------------------------------------------------- more views

/** The whole pages of an overview (one request; pages the reader may not view are missing). */
async function wholePages(o: CategoryOverview): Promise<Map<number, ElementView>> {
  if (!o.items.length) return new Map();
  try {
    const res = await api.get<{ items: ElementView[] }>(`elements/batch${qs({ ids: o.items.map((x) => x.id).join(','), lang: L() })}`);
    return new Map(res.items.map((v) => [v.id, v]));
  } catch {
    return new Map();
  }
}

/** The content of a page in an overview, with the way to the page. */
function pageBody(x: Item, v: ElementView | undefined): Node[] {
  return [
    v ? trustedHTML(v.html) : h('p', { class: 'ew-muted' }, t('category.not_loaded')),
    h('p', { class: 'ew-meta' }, h('a', { href: `/e/${x.id}` }, t('category.open_page', { title: itemTitle(x) })), ' · ',
      t('element.updated', { date: fmtDate(x.updated_at, L()) })),
  ];
}

/** The table: title, the columns of plugins (e.g. properties) and the date
 * of the change; sortable by every column (WAI-ARIA sortable table: the
 * order is a button in the header, aria-sort says it, a message announces
 * it). Sorting applies to the pages shown on this page of the overview. */
async function tableView(o: CategoryOverview): Promise<HTMLElement> {
  const keys = o.display.columns ?? [];
  const extra: { key: string; label: string }[] = [];
  const vals = new Map<number, Record<string, string>>();
  if (keys.length) {
    for (const p of listOverviewColumns()) {
      try {
        const r = await p.values(o.items.map((x) => x.id), keys, L());
        for (const c of r.columns) if (!extra.some((e) => e.key === c.key)) extra.push(c);
        for (const [id, rec] of r.values) vals.set(id, { ...(vals.get(id) ?? {}), ...rec });
      } catch { /* a provider that fails leaves its columns out */ }
    }
  }
  // keep the order chosen in the settings
  extra.sort((a, b) => keys.indexOf(a.key) - keys.indexOf(b.key));
  type Col = { key: string; label: string; text: (x: Item) => string; sortValue: (x: Item) => string | number };
  const cols: Col[] = [
    { key: 'title', label: t('category.col_title'), text: itemTitle, sortValue: (x) => itemTitle(x).toLocaleLowerCase(L()) },
    ...extra.map((c) => ({ key: c.key, label: c.label, text: (x: Item) => vals.get(x.id)?.[c.key] ?? '', sortValue: (x: Item) => (vals.get(x.id)?.[c.key] ?? '').toLocaleLowerCase(L()) })),
    { key: 'updated', label: t('category.col_updated'), text: (x) => fmtDate(x.updated_at, L()), sortValue: (x) => Date.parse(x.updated_at) },
  ];
  let sortKey = '';
  let dir: 1 | -1 = 1;
  const tbody = h('tbody');
  const heads: HTMLElement[] = [];
  const caption = h('caption', { class: 'sr-only' }, t('category.table_caption'));
  const draw = () => {
    const rows = [...o.items];
    const col = cols.find((c) => c.key === sortKey);
    if (col) rows.sort((a, b) => { const x = col.sortValue(a); const y = col.sortValue(b); return (x < y ? -1 : x > y ? 1 : 0) * dir; });
    tbody.replaceChildren(...rows.map((x) => h('tr', null, ...cols.map((c, i) => (i === 0
      ? h('th', { scope: 'row' }, h('a', { href: `/e/${x.id}` }, c.text(x)))
      : h('td', null, c.text(x)))))));
    cols.forEach((c, i) => heads[i].setAttribute('aria-sort', c.key === sortKey ? (dir === 1 ? 'ascending' : 'descending') : 'none'));
  };
  cols.forEach((c) => {
    const btn = button(c.label, () => {
      dir = sortKey === c.key ? (dir === 1 ? -1 : 1) : 1;
      sortKey = c.key;
      draw();
      announce(t(dir === 1 ? 'category.sorted_up' : 'category.sorted_down', { column: c.label }));
    }, { variant: 'subtle' });
    btn.classList.add('ew-sort-button');
    heads.push(h('th', { scope: 'col', 'aria-sort': 'none' }, btn));
  });
  draw();
  // a wide table scrolls within its own region (not the page; WCAG 1.4.10)
  return h('div', { class: 'ew-table-wrap', role: 'region', 'aria-label': t('category.table_caption'), tabindex: '0' },
    h('table', { class: 'ew-table ew-overview-table' }, caption, h('thead', null, h('tr', null, ...heads)), tbody));
}

/** The accordion: every page as a heading with a button that opens its
 * content (WAI-ARIA accordion). */
async function accordionView(o: CategoryOverview): Promise<HTMLElement> {
  const pages = await wholePages(o);
  return h('div', { class: 'ew-accordion' }, ...o.items.map((x) => {
    const pid = uid('acc');
    const panel = h('div', { id: pid, class: 'ew-accordion-panel', role: 'region', hidden: true }, ...pageBody(x, pages.get(x.id)));
    const btn = h('button', { type: 'button', class: 'ew-accordion-button', 'aria-expanded': 'false', 'aria-controls': pid }, itemTitle(x));
    panel.setAttribute('aria-labelledby', btn.id = uid('accb'));
    btn.addEventListener('click', () => {
      const open = btn.getAttribute('aria-expanded') !== 'true';
      btn.setAttribute('aria-expanded', String(open));
      panel.hidden = !open;
    });
    return h('div', { class: 'ew-accordion-item' }, h('h3', { class: 'ew-accordion-head' }, btn), panel);
  }));
}

/** The tabs: the pages as tabs (WAI-ARIA tabs: arrow keys, Home and End
 * move between the tabs, only the chosen tab is in the tab order). */
async function tabsView(o: CategoryOverview): Promise<HTMLElement> {
  const pages = await wholePages(o);
  const tabs: HTMLButtonElement[] = [];
  const panels: HTMLElement[] = [];
  const choose = (i: number, focus: boolean) => {
    tabs.forEach((tb, j) => { tb.setAttribute('aria-selected', String(i === j)); tb.tabIndex = i === j ? 0 : -1; panels[j].hidden = i !== j; });
    if (focus) tabs[i].focus();
  };
  o.items.forEach((x, i) => {
    const tid = uid('tab');
    const pid = uid('tabp');
    const tb = h('button', { type: 'button', role: 'tab', id: tid, 'aria-controls': pid, class: 'ew-tab' }, itemTitle(x)) as HTMLButtonElement;
    tb.addEventListener('click', () => choose(i, false));
    tb.addEventListener('keydown', (ev) => {
      const n = tabs.length;
      const next = ev.key === 'ArrowRight' ? (i + 1) % n : ev.key === 'ArrowLeft' ? (i - 1 + n) % n : ev.key === 'Home' ? 0 : ev.key === 'End' ? n - 1 : -1;
      if (next < 0) return;
      ev.preventDefault();
      choose(next, true);
    });
    tabs.push(tb);
    panels.push(h('div', { role: 'tabpanel', id: pid, 'aria-labelledby': tid, tabindex: '0', class: 'ew-tabpanel' }, ...pageBody(x, pages.get(x.id))));
  });
  choose(0, false);
  return h('div', { class: 'ew-tabs-view' }, h('div', { role: 'tablist', 'aria-label': t('category.pages'), class: 'ew-tablist' }, ...tabs), ...panels);
}

/** All pages in full, one below the other. */
async function allView(o: CategoryOverview): Promise<HTMLElement> {
  const pages = await wholePages(o);
  return h('div', { class: 'ew-all-pages' }, ...o.items.map((x) => {
    const hid = uid('all');
    return h('section', { class: 'ew-all-page', 'aria-labelledby': hid }, h('h3', { id: hid }, itemTitle(x)), ...pageBody(x, pages.get(x.id)));
  }));
}

/** The A–Z index: the pages under their first letter, with a navigation of the letters. */
function azView(o: CategoryOverview): HTMLElement {
  const groups = new Map<string, Item[]>();
  for (const x of o.items) {
    const first = itemTitle(x).trim().charAt(0).toLocaleUpperCase(L());
    const letter = /\p{L}/u.test(first) ? first.normalize('NFD').charAt(0) : '#';
    groups.set(letter, [...(groups.get(letter) ?? []), x]);
  }
  const letters = [...groups.keys()].sort((a, b) => (a === '#' ? 1 : b === '#' ? -1 : a.localeCompare(b, L())));
  const base = uid('az');
  const anchor = (l: string) => `${base}-${l === '#' ? 'other' : l.codePointAt(0)}`;
  return h('div', { class: 'ew-az' },
    h('nav', { 'aria-label': t('category.az_nav') }, h('ul', { class: 'ew-az-letters' }, ...letters.map((l) => h('li', null,
      h('a', { href: `#${anchor(l)}`, 'aria-label': l === '#' ? t('category.az_other') : l }, l))))),
    ...letters.map((l) => h('section', { 'aria-labelledby': anchor(l), class: 'ew-az-group' },
      h('h3', { id: anchor(l), tabindex: '-1' }, l === '#' ? t('category.az_other') : l),
      h('ul', { class: 'ew-list' }, ...(groups.get(l) ?? []).map((x) => h('li', null, h('a', { href: `/e/${x.id}` }, itemTitle(x))))))));
}

/** The timeline: the pages by date (newest first), under their month. */
function timelineView(o: CategoryOverview): HTMLElement {
  const created = o.display.date === 'created';
  const when = (x: Item) => new Date(created ? x.created_at : x.updated_at);
  const months = new Map<string, Item[]>();
  for (const x of o.items) {
    const d = when(x);
    const k = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
    months.set(k, [...(months.get(k) ?? []), x]);
  }
  return h('div', { class: 'ew-timeline' }, ...[...months].map(([k, xs]) => {
    const [y, m] = k.split('-').map(Number);
    const label = new Date(y, m - 1, 1).toLocaleDateString(L(), { year: 'numeric', month: 'long' });
    return h('section', { class: 'ew-timeline-month' },
      h('h3', null, label),
      h('ol', { class: 'ew-timeline-list' }, ...xs.map((x) => h('li', null,
        h('time', { datetime: (created ? x.created_at : x.updated_at), class: 'ew-meta' }, when(x).toLocaleDateString(L(), { dateStyle: 'medium' })), ' ',
        h('a', { href: `/e/${x.id}` }, itemTitle(x)),
        x.excerpt ? h('p', { class: 'ew-excerpt' }, x.excerpt) : null))));
  }));
}

/** The overview of a category, asked once for the blocks of one page view
 * (overview, subcategories, handbook). */
let overviewMemo: { key: string; at: number; o: Promise<CategoryOverview> } | null = null;
function loadOverview(id: number, page = 1): Promise<CategoryOverview> {
  const key = `${id}|${page}|${L()}|${state.editMode}`;
  if (!overviewMemo || overviewMemo.key !== key || Date.now() - overviewMemo.at > 2000) {
    const o = api.get<CategoryOverview>(`categories/${id}/overview${qs({ lang: L(), page })}`);
    overviewMemo = { key, at: Date.now(), o };
    o.catch(() => { if (overviewMemo?.o === o) overviewMemo = null; });
  }
  return overviewMemo.o;
}

const currentPage = () => Number(new URLSearchParams(location.search).get('p') ?? '1') || 1;

async function overview(cp: CategoryPage): Promise<HTMLElement | null> {
  const o = await loadOverview(cp.category.id, currentPage());
  const edit = state.editMode;
  const v = o.display.view;
  // the handbook shows its pages in the navigation of the side column
  if (v === 'handbook' && !edit) return null;
  if (!o.items.length && (v !== 'tree' || !cp.subs.length)) {
    const nothing = !cp.category.default_element_id && !cp.subs.length;
    return nothing ? h('p', { class: 'ew-muted' }, cp.category.permissions?.includes('create') ? t('category.empty_editor') : t('category.empty')) : null;
  }
  // without the edit mode the default page is shown in full already
  const o2 = edit ? o : { ...o, items: o.items.filter((x) => x.id !== cp.category.default_element_id) };
  if (!o2.items.length && v !== 'tree') return null;
  const body = v === 'cards' ? cardsView(o2, false) : v === 'tiles' ? cardsView(o2, true) : v === 'tree' ? treeView(o2, cp.subs)
    : v === 'table' ? await tableView(o2) : v === 'accordion' ? await accordionView(o2) : v === 'tabs' ? await tabsView(o2)
      : v === 'all' ? await allView(o2) : v === 'az' ? azView(o2) : v === 'timeline' ? timelineView(o2) : listView(o2, cp, edit);
  return h('section', { class: ['ew-section', 'ew-overview', `ew-overview--${v}`], 'aria-labelledby': 'ew-pages-title' },
    h('h2', { id: 'ew-pages-title' }, t('category.pages')), body, pager(o));
}

// ---------------------------------------------------------------- handbook

/** The handbook navigation: the pages of the category (the current page
 * marked), when its display is "handbook". */
async function handbookNav(): Promise<HTMLElement | null> {
  const id = pageCtx.categoryId;
  if (!id) return null;
  let o: CategoryOverview;
  // on the category page: the page of the overview shown
  try { o = await loadOverview(id, pageCtx.elementId === 0 ? currentPage() : 1); } catch { return null; }
  if (o.display.view !== 'handbook') return null;
  // (the address may be a readable path: the page shown is known by its id)
  const shownEl = pageCtx.elementId;
  const title = pick(o.category_titles, L(), D());
  const hid = uid('hb');
  return h('nav', { class: 'ew-handbook', 'aria-labelledby': hid },
    h('h2', { id: hid, class: 'ew-handbook-title' }, h('a', { href: `/c/${id}`, 'aria-current': shownEl === 0 ? 'page' : undefined }, title)),
    h('ol', { class: 'ew-handbook-list' }, ...o.items.map((x) => h('li', null,
      h('a', { href: `/e/${x.id}`, 'aria-current': shownEl === x.id ? 'page' : undefined }, itemTitle(x))))),
    o.pages > 1 ? h('p', null, h('a', { href: `/c/${id}` }, t('category.all_pages', { n: o.total }))) : null);
}

/** Previous and next page of a handbook: at the end of a page of a category
 * whose display is "handbook", the pages in the order of the navigation
 * (read like a book, from the first page to the last). */
async function handbookPager(cat?: Category): Promise<HTMLElement | null> {
  // on the page of the category: its first page is shown there (or none)
  const id = cat?.id ?? pageCtx.categoryId;
  const el = cat ? (cat.default_element_id ?? 0) : pageCtx.elementId;
  if (!id || (!cat && !el)) return null;
  let o: CategoryOverview;
  try { o = await loadOverview(id, cat ? currentPage() : 1); } catch { return null; }
  if (o.display.view !== 'handbook') return null;
  const i = o.items.findIndex((x) => x.id === el);
  if (i < 0 && !cat) return null;
  const prev = o.items[i - 1];
  const next = o.items[i + 1];
  if (!prev && !next) return null;
  const link = (x: Item, cls: string, label: string) =>
    h('a', { class: `ew-pagenav-link ${cls}`, href: `/e/${x.id}`, rel: cls === 'ew-pagenav-prev' ? 'prev' : 'next' },
      h('span', { class: 'ew-pagenav-label' }, label), h('span', { class: 'ew-pagenav-title' }, itemTitle(x)));
  return h('nav', { class: 'ew-pagenav', 'aria-label': t('category.pagenav') },
    prev ? link(prev, 'ew-pagenav-prev', t('category.pagenav_prev')) : h('span'),
    next ? link(next, 'ew-pagenav-next', t('category.pagenav_next')) : h('span'));
}

// ---------------------------------------------------------------- blocks

/** registerCategoryBlocks registers the blocks of category pages; manage
 * renders the buttons to manage the category (edit mode). */
export function registerCategoryBlocks(manage: (cp: CategoryPage) => HTMLElement | null, info: (cp: CategoryPage) => (HTMLElement | null)[],
  title: (cp: CategoryPage) => HTMLElement, watch: (cp: CategoryPage) => HTMLElement | null, feed: (cp: CategoryPage) => HTMLElement | null): void {
  const cp = (c?: PageContext) => (c?.category as CategoryPage | undefined) ?? null;
  const reg = (id: string, position: string, order: number, render: (x: CategoryPage, c?: PageContext) => Node | null | Promise<Node | null>, always = false, replacedBy?: string[]) =>
    registerSlot({ id, kind: 'category', position, order, always, replacedBy, label: () => t(`block.${id}`), render: (_p, _k, c) => { const x = cp(c); return x ? render(x, c) : null; } });
  // the path and the title; the title bar holds both and stands in for them when placed
  reg('core.category.path', 'category-header', 5, (_x, c) => c?.path?.() ?? null, false, ['core.category.titlebar']);
  reg('core.category.title', 'category-header', 10, title, false, ['core.category.titlebar']);
  reg('core.category.titlebar', '', 8, (x, c) => h('div', { class: 'ew-titlebar' }, c?.path?.() ?? null, title(x)));
  reg('core.category.manage', 'category-toolbar', 10, manage, true);
  reg('core.category.watch', 'category-toolbar', 20, watch);
  reg('core.category.feed', 'category-toolbar', 30, feed);
  reg('core.category.info', 'category-content', 10, (x) => {
    const parts = info(x).filter((n): n is HTMLElement => !!n);
    return parts.length ? h('div', { class: 'ew-category-info' }, ...parts) : null;
  });
  reg('core.category.overview', 'category-content', 30, (x) => overview(x).catch(() => null));
  reg('core.category.subcategories', 'category-content', 40, (x) => subcategories(x));
  // the way through a handbook: from its page to the next one
  reg('core.category.pagenav', 'category-content', 45, (x) => handbookPager(x.category).catch(() => null));
  // every page action (favourite, share …) is a block of its own here too
  registerSlot({
    id: 'core.category.action', kind: 'category', position: '*', order: 100,
    label: () => t('block.core.category.action', { name: '' }),
    instances: async () => pageActionList().map((a, i) => ({ key: a.id.replace(/[^a-z0-9_-]/g, '_'), label: t('block.core.category.action', { name: a.label?.() ?? a.id }), position: 'category-toolbar', order: 100 + (a.order ?? 50) + i / 100 })),
    render: (_p, key, c) => {
      const x = cp(c);
      const a = pageActionList().find((y) => y.id.replace(/[^a-z0-9_-]/g, '_') === key);
      if (!x || !a) return null;
      const ttl = pick(x.category.titles, L(), D());
      try { return a.render({ type: 'category', id: x.category.id, title: ttl, categoryId: x.category.id }); } catch { return null; }
    },
  });
  // featured pages: each page shown in full is a block of its own
  registerSlot({
    id: FEATURED, kind: 'category', position: '*', order: 20, single: true,
    label: () => t('block.core.category.element', { title: '' }),
    instances: featuredInstances,
    render: async (_p, key, c) => {
      const x = cp(c);
      if (!x || !key) return null;
      const v = await featuredView(Number(key));
      return v ? featured(v, pick(x.category.titles, L(), D()), state.editMode) : null;
    },
  });
  // the handbook: a navigation in the side column (category page and its pages)
  registerSlot({ id: 'core.category.nav', position: 'sidebar', order: 5, label: () => t('block.core.category.nav'), render: () => handbookNav() });
  // … and the way to the previous and the next page, at the end of a page
  registerSlot({ id: 'core.element.pagenav', kind: 'element', position: 'element-footer', order: 5, label: () => t('block.core.element.pagenav'), render: () => handbookPager() });
}

let subsRender: ((cp: CategoryPage) => HTMLElement | null) | null = null;

/** setSubcategoriesRender sets how the subcategories are shown (cards with
 * the buttons to order them in the edit mode). */
export function setSubcategoriesRender(f: (cp: CategoryPage) => HTMLElement | null): void {
  subsRender = f;
}

async function subcategories(cp: CategoryPage): Promise<HTMLElement | null> {
  if (!cp.subs.length || !subsRender) return null;
  // the tree shows the subcategories itself
  try {
    const o = await loadOverview(cp.category.id, currentPage());
    if (o.display.view === 'tree' && !state.editMode) return null;
  } catch { /* shown as cards */ }
  return subsRender(cp);
}
