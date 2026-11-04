// core.menu frontend: renders menus at template positions (multilevel:
// dropdowns with flyouts in horizontal positions, collapsible trees in
// vertical ones) and provides the "Menus" administration page.
import {
  activeTemplate, api, button, categorySelect, checkbox, confirmDialog, elementSelect, errorText, field, formDialog, h, mount, pageHeader, pick,
  positionLabel, positionOptions, refreshPositions, registerAdminPage, registerSlot, rerender, select, state, t, templateLabel, textInput, toast, type TreeNode,
} from '@ewiki/sdk';
import './style.css';

type ItemType = 'url' | 'category' | 'element' | 'menu' | 'heading';
interface Item {
  label: Record<string, string>; type?: ItemType; url?: string; category_id?: number; element_id?: number; menu_id?: number; new_tab?: boolean;
  children?: Item[]; href?: string; submenu?: boolean;
}
interface Menu { id: number; name: string; location: string; position: number; items: Item[] }

const BASE = '/api/v1/x/core.menu';
// one request for all positions (shared while it is running), cached 30 s
let cache: { at: number; user: number; lang: string; menus: Promise<Menu[]> } | null = null;

function menus(): Promise<Menu[]> {
  const uid = state.user?.id ?? 0;
  if (!cache || cache.user !== uid || cache.lang !== state.lang || Date.now() - cache.at > 30000) {
    const p = api.get<Menu[]>(`${BASE}/menus`);
    cache = { at: Date.now(), user: uid, lang: state.lang, menus: p };
    p.catch(() => { cache = null; });
  }
  return cache.menus;
}

let seq = 0;
const label = (it: Item) => pick(it.label, state.lang, state.site.default_language);
const isCurrent = (href?: string) => !!href && (location.pathname === href || location.pathname + location.search === href);
const containsCurrent = (it: Item): boolean => isCurrent(it.href) || (it.children ?? []).some(containsCurrent);

function link(it: Item): HTMLElement {
  const href = it.href ?? '#';
  const external = /^https?:/i.test(href) && !href.startsWith(location.origin);
  return h('a', {
    href, 'aria-current': isCurrent(it.href) ? 'page' : undefined,
    rel: external || it.new_tab ? 'noopener' : undefined, target: it.new_tab ? '_blank' : undefined,
  }, label(it), it.new_tab ? h('span', { class: 'sr-only' }, ` (${t('core.menu.opens_new_tab')})`) : null);
}

/**
 * Renders a level. mode "dropdown": nested levels open as panels (click,
 * keyboard, hover); "tree": nested levels collapse in place; "static": all
 * levels are shown (footer).
 */
function renderItems(items: Item[], mode: 'dropdown' | 'tree' | 'static', level = 0): HTMLUListElement {
  const ul = h('ul', { class: `ewm-level ewm-level-${level}` });
  for (const it of items) {
    const kids = it.children ?? [];
    const li = h('li', { class: ['ewm-item', kids.length > 0 && 'ewm-has-children', it.submenu && 'ewm-submenu', !it.href && 'ewm-heading'] });
    if (!kids.length || mode === 'static') {
      li.append(it.href ? link(it) : h('span', { class: 'ewm-label' }, label(it)));
      if (kids.length) li.append(renderItems(kids, mode, level + 1));
      ul.append(li);
      continue;
    }
    const sub = renderItems(kids, mode, level + 1);
    sub.id = `ewm-${++seq}`;
    const open = mode === 'tree' && containsCurrent(it);
    sub.hidden = !open;
    let toggle: HTMLButtonElement;
    if (it.href) {
      toggle = h('button', { type: 'button', class: 'ewm-toggle', 'aria-expanded': String(open), 'aria-controls': sub.id, 'aria-label': t('core.menu.submenu_of', { title: label(it) }) },
        h('span', { 'aria-hidden': 'true', class: 'ewm-caret' })) as HTMLButtonElement;
      li.append(h('div', { class: 'ewm-row' }, link(it), toggle));
    } else {
      // headings and submenus without own page: the whole label toggles
      toggle = h('button', { type: 'button', class: 'ewm-toggle ewm-toggle--label', 'aria-expanded': String(open), 'aria-controls': sub.id },
        label(it), h('span', { 'aria-hidden': 'true', class: 'ewm-caret' })) as HTMLButtonElement;
      li.append(toggle);
    }
    const set = (o: boolean) => {
      sub.hidden = !o;
      toggle.setAttribute('aria-expanded', String(o));
      li.classList.toggle('is-open', o);
      if (o && mode === 'dropdown') {
        // close siblings, keep the panel inside the viewport
        for (const sib of Array.from(ul.children)) if (sib !== li) (sib as HTMLElement & { ewmClose?: () => void }).ewmClose?.();
        sub.classList.remove('ewm-flip');
        const r = sub.getBoundingClientRect();
        if (r.right > window.innerWidth - 8) sub.classList.add('ewm-flip');
      }
    };
    (li as HTMLElement & { ewmClose?: () => void }).ewmClose = () => set(false);
    li.classList.toggle('is-open', open);
    let hoverAt = 0; // a click right after hover-open keeps the panel open
    toggle.addEventListener('click', () => set(Date.now() - hoverAt < 600 ? true : Boolean(sub.hidden)));
    if (mode === 'dropdown') {
      li.addEventListener('keydown', (ev) => {
        if (ev.key === 'Escape' && !sub.hidden) {
          ev.stopPropagation();
          set(false);
          toggle.focus();
        }
      });
      li.addEventListener('focusout', (ev) => { if (!li.contains(ev.relatedTarget as Node)) set(false); });
      if (window.matchMedia?.('(hover: hover)').matches) {
        let timer = 0;
        li.addEventListener('mouseenter', () => { clearTimeout(timer); if (sub.hidden) { hoverAt = Date.now(); set(true); } });
        li.addEventListener('mouseleave', () => { timer = window.setTimeout(() => set(false), 250); });
      }
    }
    li.append(sub);
    ul.append(li);
  }
  return ul;
}

// Horizontal positions get dropdowns; footers show everything; the rest are trees.
const HORIZONTAL = new Set(['header-nav', 'top-bar', 'cms-nav', 'header-tools']);
const HEADING = /^(sidebar|footer-\d)/;

async function slot(location: string): Promise<Node | null> {
  let ms: Menu[];
  try { ms = (await menus()).filter((m) => m.location === location && m.items.length); } catch { return null; }
  if (!ms.length) return null;
  const mode = HORIZONTAL.has(location) ? 'dropdown' : location.startsWith('footer') ? 'static' : 'tree';
  return h('div', { class: ['ewm', `ewm--${location}`, mode === 'dropdown' ? 'ewm--horizontal' : 'ewm--vertical', `ewm--${mode}`] },
    ...ms.map((m) => h('nav', { 'aria-label': t('core.menu.nav_label', { name: m.name }) },
      HEADING.test(location) ? h('h2', { class: 'ew-tree-title' }, m.name) : null, renderItems(m.items, mode))));
}

// ---------------------------------------------------------------- admin

/** Editable state of one item (children are kept in the tree). */
interface Draft { labels: Record<string, string>; type: ItemType; url: string; cat: number; el: number; menu: number; newTab: boolean }
interface MNode { d: Draft; children: MNode[] }

const TYPES: ItemType[] = ['category', 'element', 'url', 'menu', 'heading'];
const MAX_DEPTH = 4;
const blank = (): Draft => ({ labels: {}, type: 'category', url: '', cat: 0, el: 0, menu: 0, newTab: false });

function toNodes(items: Item[]): MNode[] {
  return items.map((it) => ({
    d: {
      labels: { ...it.label }, type: it.type ?? (it.category_id ? 'category' : it.element_id ? 'element' : it.menu_id ? 'menu' : it.url ? 'url' : 'heading'),
      url: it.url ?? '', cat: it.category_id ?? 0, el: it.element_id ?? 0, menu: it.menu_id ?? 0, newTab: !!it.new_tab,
    },
    children: toNodes(it.children ?? []),
  }));
}

function toItems(nodes: MNode[]): Item[] {
  return nodes.map(({ d, children }) => {
    const it: Item = { label: Object.fromEntries(Object.entries(d.labels).filter(([, v]) => v.trim())), type: d.type };
    if (d.type === 'url') { it.url = d.url.trim(); if (d.newTab) it.new_tab = true; }
    if (d.type === 'category') it.category_id = d.cat;
    if (d.type === 'element') it.element_id = d.el;
    if (d.type === 'menu') it.menu_id = d.menu;
    if (children.length) it.children = toItems(children);
    return it;
  });
}

const count = (nodes: MNode[]): number => nodes.reduce((n, x) => n + 1 + count(x.children), 0);

/** Finds the list that contains node (and its index). */
function locate(nodes: MNode[], node: MNode): { list: MNode[]; i: number } | null {
  const i = nodes.indexOf(node);
  if (i >= 0) return { list: nodes, i };
  for (const x of nodes) {
    const r = locate(x.children, node);
    if (r) return r;
  }
  return null;
}

const height = (n: MNode): number => 1 + Math.max(0, ...n.children.map(height));

/** Titles of targets for the overview (loaded once per dialog). */
class Titles {
  private cats: Promise<Map<number, string>> | null = null;
  private els = new Map<number, Promise<string>>();
  constructor(private all: Menu[]) {}
  async of(d: Draft): Promise<string> {
    const own = pick(d.labels, state.lang, state.site.default_language);
    if (own) return own;
    if (d.type === 'category' && d.cat) {
      this.cats ??= api.get<TreeNode[]>('categories/tree').then((ns) => new Map(ns.map((n) => [n.id, pick(n.titles, state.lang, state.site.default_language)])))
        .catch(() => new Map<number, string>());
      return (await this.cats).get(d.cat) ?? `#${d.cat}`;
    }
    if (d.type === 'element' && d.el) {
      if (!this.els.has(d.el)) this.els.set(d.el, api.get<{ title: string }>(`elements/${d.el}`).then((e) => e.title).catch(() => `#${d.el}`));
      return this.els.get(d.el)!;
    }
    if (d.type === 'menu' && d.menu) return this.all.find((m) => m.id === d.menu)?.name ?? `#${d.menu}`;
    return d.url || '–';
  }
  target(d: Draft): string {
    return d.type === 'url' ? `${t('core.menu.type_url')}: ${d.url}` : t(`core.menu.type_${d.type}`);
  }
}

/** Target controls of a row (depend on the type). */
async function targetControls(r: Draft, all: Menu[], self: number | undefined, box: HTMLElement): Promise<void> {
  box.replaceChildren();
  if (r.type === 'category') {
    const cs = await categorySelect({ selected: r.cat, perm: 'view', noneLabel: t('picker.choose_category'), allowCreate: true, onChange: (id) => { r.cat = id; } });
    box.append(cs.field(t('core.menu.target_category')));
  } else if (r.type === 'element') {
    const es = await elementSelect({ selected: r.el, allowCreate: true, onChange: (id) => { r.el = id; } });
    box.append(es.el);
  } else if (r.type === 'url') {
    const url = textInput({ value: r.url, placeholder: '/search?q=…, https://…' });
    url.addEventListener('input', () => { r.url = url.value; });
    const nt = checkbox(t('core.menu.new_tab'), r.newTab);
    nt.input.addEventListener('change', () => { r.newTab = nt.input.checked; });
    box.append(field(t('core.menu.url'), url, { help: t('core.menu.url_help'), required: true }), nt.el);
  } else if (r.type === 'menu') {
    const opts = [{ value: '', label: t('core.menu.choose_menu') }, ...all.filter((m) => m.id !== self).map((m) => ({ value: String(m.id), label: m.location ? `${m.name} (${positionLabel(m.location)})` : m.name }))];
    const sel = select(opts, r.menu ? String(r.menu) : '');
    sel.addEventListener('change', () => { r.menu = Number(sel.value) || 0; });
    const create = button(t('core.menu.new_submenu'), () => {
      const name = textInput({ required: true });
      formDialog(t('core.menu.new_submenu'), [field(t('core.menu.name'), name, { required: true }), h('p', { class: 'ew-help' }, t('core.menu.new_submenu_help'))], t('common.create'), async () => {
        const m = await api.post<Menu>(`${BASE}/menus`, { name: name.value, location: '', items: [] });
        all.push(m);
        r.menu = m.id;
        await targetControls(r, all, self, box);
        box.querySelector('select')?.focus();
      });
    }, { variant: 'secondary' });
    box.append(field(t('core.menu.target_menu'), sel, { help: t('core.menu.target_menu_help') }), h('div', { class: 'ew-picker' }, create));
  } else {
    box.append(h('p', { class: 'ew-help' }, t('core.menu.heading_help')));
  }
}

/** Dialog for one item: target, labels and the parent item. */
async function itemDialog(tree: MNode[], node: MNode | null, all: Menu[], self: number | undefined, titles: Titles, done: () => void): Promise<void> {
  const d: Draft = node ? { ...node.d, labels: { ...node.d.labels } } : blank();
  // possible parents: every item that is not the item itself or below it, within the depth limit
  const own = node ? height(node) : 1;
  const parents: { value: string; label: string; node: MNode | null }[] = [{ value: '', label: t('core.menu.top_level'), node: null }];
  const walk = async (nodes: MNode[], depth: number) => {
    for (const x of nodes) {
      if (x === node) continue;
      if (depth + 1 + own <= MAX_DEPTH) parents.push({ value: String(parents.length), label: `${'— '.repeat(depth)}${await titles.of(x.d)}`, node: x });
      await walk(x.children, depth + 1);
    }
  };
  await walk(tree, 0);
  const current = node ? locate(tree, node) : null;
  const currentParent = current ? parents.find((p) => p.node && p.node.children === current.list) : null;
  const parent = select(parents.map(({ value, label }) => ({ value, label })), currentParent?.value ?? '');
  const type = select(TYPES.map((x) => ({ value: x, label: t(`core.menu.type_${x}`) })), d.type);
  const target = h('div', { class: 'ewm-target' });
  type.addEventListener('change', () => { d.type = type.value as ItemType; void targetControls(d, all, self, target); });
  await targetControls(d, all, self, target);
  const labels = state.site.languages.map((l) => {
    const inp = textInput({ value: d.labels[l] ?? '', lang: l });
    inp.addEventListener('input', () => { d.labels[l] = inp.value; });
    return field(`${t('core.menu.label')} (${l})`, inp);
  });
  formDialog(node ? t('core.menu.edit_item') : t('core.menu.add_item'), [
    field(t('core.menu.type'), type), target,
    h('fieldset', null, h('legend', null, t('core.menu.labels')), h('p', { class: 'ew-help' }, t('core.menu.label_help')), ...labels),
    field(t('core.menu.parent'), parent, { help: t('core.menu.parent_help') }),
  ], node ? t('common.save') : t('core.menu.add_item'), async () => {
    const hasLabel = Object.values(d.labels).some((v) => v.trim());
    if ((d.type === 'url' || d.type === 'heading') && !hasLabel) throw new Error(t('core.menu.err_item_label'));
    if ((d.type === 'url' && !d.url.trim()) || (d.type === 'category' && !d.cat) || (d.type === 'element' && !d.el) || (d.type === 'menu' && !d.menu)) {
      throw new Error(t('core.menu.err_item_target'));
    }
    const p = parents.find((x) => x.value === parent.value)?.node ?? null;
    const dest = p ? p.children : tree;
    if (node) {
      node.d = d;
      if (current && current.list !== dest) {
        current.list.splice(current.i, 1);
        dest.push(node);
      }
    } else {
      dest.push({ d, children: [] });
    }
    done();
  }, { wide: true });
}

/** Overview of the items: one line per item with edit, move and remove. */
function itemList(tree: MNode[], all: Menu[], self: number | undefined, titles: Titles, box: HTMLElement): void {
  const redraw = () => itemList(tree, all, self, titles, box);
  const render = (nodes: MNode[]): HTMLElement => h('ol', { class: 'ewm-edit-list' }, ...nodes.map((n, i) => {
    const name = h('strong', { class: 'ewm-edit-title' }, '…');
    void titles.of(n.d).then((x) => {
      name.textContent = x;
      li.querySelectorAll<HTMLButtonElement>(':scope > .ewm-edit-row button[data-act]').forEach((b) => b.setAttribute('aria-label', `${b.textContent}: ${x}`));
    });
    const act = (label: string, fn: () => void, variant: 'link' | 'secondary' = 'link') => {
      const b = button(label, fn, { variant });
      b.dataset.act = '1';
      return b;
    };
    const li = h('li', null,
      h('div', { class: 'ewm-edit-row' },
        h('span', { class: 'ewm-edit-text' }, name, h('span', { class: 'ewm-edit-target' }, titles.target(n.d))),
        h('span', { class: 'ew-actions' },
          act(t('common.edit'), () => void itemDialog(tree, n, all, self, titles, redraw)),
          i > 0 ? act(t('core.menu.move_up'), () => { [nodes[i - 1], nodes[i]] = [nodes[i], nodes[i - 1]]; redraw(); }) : null,
          i < nodes.length - 1 ? act(t('core.menu.move_down'), () => { [nodes[i + 1], nodes[i]] = [nodes[i], nodes[i + 1]]; redraw(); }) : null,
          act(t('core.menu.remove_item'), async () => {
            if (n.children.length && !(await confirmDialog(t('core.menu.remove_with_children', { n: count(n.children) }), t('core.menu.remove_item'), true))) return;
            nodes.splice(i, 1);
            redraw();
          }))),
      n.children.length ? render(n.children) : null);
    return li;
  }));
  mount(box, tree.length ? render(tree) : h('p', { class: 'ew-help' }, t('core.menu.no_items_yet')));
}

async function menuDialog(m: Menu | null, all: Menu[]): Promise<void> {
  const name = textInput({ value: m?.name ?? '', required: true });
  const locs = [...positionOptions(m?.location), { value: '', label: t('core.menu.no_location') }];
  const loc = select(locs, m ? m.location : 'header-nav');
  const pos = textInput({ type: 'number', value: String(m?.position ?? 0) });
  const tree = toNodes(m?.items ?? []);
  const titles = new Titles(all);
  const box = h('div', { class: 'ewm-edit' });
  itemList(tree, all, m?.id, titles, box);
  formDialog(m ? m.name : t('core.menu.add_menu'), [field(t('core.menu.name'), name, { required: true }),
    field(t('core.menu.location'), loc, { help: t('core.menu.location_help', { template: templateLabel(activeTemplate()?.id ?? '') }) }), field(t('core.menu.position'), pos),
    h('h3', null, t('core.menu.items')), box,
    button(t('core.menu.add_item'), () => void itemDialog(tree, null, all, m?.id, titles, () => itemList(tree, all, m?.id, titles, box)), { variant: 'secondary' })], t('common.save'), async () => {
    const body = { name: name.value, location: loc.value, position: Number(pos.value) || 0, items: toItems(tree) };
    if (m) await api.put(`${BASE}/menus/${m.id}`, body);
    else await api.post(`${BASE}/menus`, body);
    cache = null;
    toast(!loc.value ? t('core.menu.saved_submenu') : tree.length ? t('core.menu.saved_at', { position: positionLabel(loc.value) }) : t('core.menu.saved_empty'));
    await refreshPositions();
    await rerender();
  }, { wide: true });
}

async function adminPage(container: HTMLElement): Promise<void> {
  let ms: Menu[];
  try { ms = await api.get<Menu[]>(`${BASE}/menus?all=1`); } catch (e) { mount(container, h('p', { role: 'alert' }, errorText(e))); return; }
  const offered = new Set(activeTemplate()?.positions ?? []);
  const open = (m: Menu | null) => void menuDialog(m, ms).catch((e) => toast(errorText(e), 'error'));
  const usedAsSub = new Set<number>();
  const scan = (xs: Item[]) => xs.forEach((x) => { if (x.menu_id) usedAsSub.add(x.menu_id); scan(x.children ?? []); });
  ms.forEach((m) => scan(m.items));
  const status = (m: Menu) => {
    if (!m.items.length) return h('span', { class: 'ew-badge ew-badge--warn' }, t('core.menu.no_items'));
    if (!m.location) return h('span', { class: usedAsSub.has(m.id) ? 'ew-badge ew-badge--ok' : 'ew-badge ew-badge--warn' }, t(usedAsSub.has(m.id) ? 'core.menu.used_as_submenu' : 'core.menu.unused_submenu'));
    if (!offered.has(m.location)) return h('span', { class: 'ew-badge ew-badge--warn' }, t('core.menu.hidden_position'));
    return h('span', { class: 'ew-badge ew-badge--ok' }, t('core.menu.visible'));
  };
  mount(container, pageHeader(t('core.menu.admin_title'), button(t('core.menu.add_menu'), () => open(null), { variant: 'primary' })),
    h('p', null, t('core.menu.help', { template: templateLabel(activeTemplate()?.id ?? '') }), ' ',
      h('a', { href: '/?positions=1' }, t('positions.show'))),
    ms.length ? h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('core.menu.admin_title')),
      h('thead', null, h('tr', null, ...[t('core.menu.name'), t('core.menu.location'), t('core.menu.items'), t('common.status'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...ms.map((m) => h('tr', null, h('th', { scope: 'row' }, m.name),
        h('td', null, m.location ? [positionLabel(m.location), h('br'), h('code', null, m.location)] : t('core.menu.no_location')),
        h('td', null, String(count(toNodes(m.items)))),
        h('td', null, status(m)),
        h('td', { class: 'ew-actions' },
          button(t('common.edit'), () => open(m), { variant: 'link', ariaLabel: `${t('common.edit')} ${m.name}` }),
          button(t('common.delete'), async () => {
            if (!(await confirmDialog(t('core.menu.delete_confirm', { name: m.name }), t('common.delete'), true))) return;
            try { await api.del(`${BASE}/menus/${m.id}`); cache = null; await rerender(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: `${t('common.delete')} ${m.name}` }))))))
      : h('p', null, t('core.menu.empty')));
}

export function register(): void {
  // menus can be placed at every position of the active template
  registerSlot({ id: 'core.menu', position: '*', order: 20, render: slot });
  registerAdminPage({ id: 'menus', permission: 'core.menu.edit', label: () => t('core.menu.admin_title'), order: 10, render: adminPage });
}
