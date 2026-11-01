// core.menu frontend: renders menus at template positions and provides the
// "Menus" administration page.
import { api, button, confirmDialog, errorText, field, formDialog, h, mount, pageHeader, pick, registerAdminPage, registerSlot, rerender, select, state, t, textInput, toast } from '@ewiki/sdk';
import './style.css';

interface Item { label: Record<string, string>; url?: string; category_id?: number; children?: Item[] }
interface Menu { id: number; name: string; location: string; position: number; items: Item[] }

const BASE = '/api/v1/x/core.menu';
let cache: { at: number; user: number; menus: Menu[] } | null = null;

async function menus(): Promise<Menu[]> {
  const uid = state.user?.id ?? 0;
  if (!cache || cache.user !== uid || Date.now() - cache.at > 30000) {
    cache = { at: Date.now(), user: uid, menus: await api.get<Menu[]>(`${BASE}/menus`) };
  }
  return cache.menus;
}

function href(it: Item): string {
  return it.category_id ? `/c/${it.category_id}` : (it.url ?? '#');
}

function renderItems(items: Item[], dropdown: boolean, level = 0): HTMLUListElement {
  const ul = h('ul', { class: `ewm-level ewm-level-${level}` });
  for (const it of items) {
    const label = pick(it.label, state.lang, state.site.default_language);
    const current = location.pathname === href(it);
    const external = /^https?:/i.test(href(it)) && !href(it).startsWith(location.origin);
    const link = h('a', { href: href(it), 'aria-current': current ? 'page' : undefined, rel: external ? 'external noopener' : undefined }, label);
    const li = h('li', { class: 'ewm-item' }, link);
    if (it.children?.length) {
      if (dropdown && level === 0) {
        const sub = renderItems(it.children, dropdown, level + 1);
        sub.hidden = true;
        sub.id = `ewm-${Math.random().toString(36).slice(2)}`;
        const tb = h('button', { type: 'button', class: 'ewm-toggle', 'aria-expanded': 'false', 'aria-controls': sub.id, 'aria-label': t('tree.expand', { title: label }) }, h('span', { 'aria-hidden': 'true' }, '▾'));
        tb.addEventListener('click', () => { const open = sub.hidden; sub.hidden = !open; tb.setAttribute('aria-expanded', String(open)); });
        li.addEventListener('keydown', (ev) => { if (ev.key === 'Escape' && !sub.hidden) { sub.hidden = true; tb.setAttribute('aria-expanded', 'false'); tb.focus(); } });
        li.addEventListener('focusout', (ev) => { if (!li.contains(ev.relatedTarget as Node)) { sub.hidden = true; tb.setAttribute('aria-expanded', 'false'); } });
        li.append(tb, sub);
      } else {
        li.append(renderItems(it.children, dropdown, level + 1));
      }
    }
    ul.append(li);
  }
  return ul;
}

function slot(location: string, dropdown: boolean) {
  return async (): Promise<Node | null> => {
    let ms: Menu[];
    try { ms = (await menus()).filter((m) => m.location === location && m.items.length); } catch { return null; }
    if (!ms.length) return null;
    return h('div', { class: `ewm ewm--${location}` }, ...ms.map((m) => h('nav', { 'aria-label': t('core.menu.nav_label', { name: m.name }) },
      location === 'sidebar' ? h('h2', { class: 'ew-tree-title' }, m.name) : null, renderItems(m.items, dropdown))));
  };
}

// ---------------------------------------------------------------- admin

interface Row { depth: number; labels: Record<string, string>; url: string; cat: string }

function flatten(items: Item[], depth = 0, out: Row[] = []): Row[] {
  for (const it of items) {
    out.push({ depth, labels: { ...it.label }, url: it.url ?? '', cat: it.category_id ? String(it.category_id) : '' });
    flatten(it.children ?? [], depth + 1, out);
  }
  return out;
}

function unflatten(rows: Row[]): Item[] {
  const root: Item[] = [];
  const stack: Item[][] = [root];
  for (const r of rows) {
    const it: Item = { label: r.labels, children: [] };
    if (r.cat) it.category_id = Number(r.cat);
    else if (r.url) it.url = r.url;
    const d = Math.min(r.depth, stack.length - 1);
    stack.length = d + 1;
    stack[d].push(it);
    stack.push(it.children!);
  }
  const clean = (xs: Item[]): Item[] => xs.map((x) => ({ ...x, children: x.children?.length ? clean(x.children) : undefined }));
  return clean(root);
}

function menuDialog(m: Menu | null): void {
  const name = textInput({ value: m?.name ?? '', required: true });
  const loc = select(['header-nav', 'sidebar', 'footer'].map((x) => ({ value: x, label: x })), m?.location ?? 'header-nav');
  const pos = textInput({ type: 'number', value: String(m?.position ?? 0) });
  const rows: Row[] = flatten(m?.items ?? []);
  const list = h('div', { class: 'ewm-rows' });
  const draw = () => {
    list.replaceChildren(...rows.map((r, i) => {
      const depth = select([0, 1, 2, 3].map((d) => ({ value: String(d), label: String(d + 1) })), String(r.depth));
      depth.addEventListener('change', () => { r.depth = Number(depth.value); });
      const labels = state.site.languages.map((l) => {
        const inp = textInput({ value: r.labels[l] ?? '', lang: l });
        inp.addEventListener('input', () => { r.labels[l] = inp.value; });
        return field(`${t('core.menu.label')} (${l})`, inp);
      });
      const url = textInput({ value: r.url });
      url.addEventListener('input', () => { r.url = url.value; });
      const cat = textInput({ value: r.cat, type: 'number' });
      cat.addEventListener('input', () => { r.cat = cat.value; });
      return h('fieldset', { class: 'ewm-row' }, h('legend', null, `${t('core.menu.items')} ${i + 1}`),
        field(t('core.menu.level'), depth, { inline: true }), ...labels, field(t('core.menu.url'), url), field(t('core.menu.category'), cat),
        h('div', { class: 'ew-actions' },
          i > 0 ? button(t('core.menu.move_up'), () => { [rows[i - 1], rows[i]] = [rows[i], rows[i - 1]]; draw(); }, { variant: 'link' }) : null,
          i < rows.length - 1 ? button(t('core.menu.move_down'), () => { [rows[i + 1], rows[i]] = [rows[i], rows[i + 1]]; draw(); }, { variant: 'link' }) : null,
          button(t('core.menu.remove_item'), () => { rows.splice(i, 1); draw(); }, { variant: 'link' })));
    }));
  };
  draw();
  formDialog(m ? m.name : t('core.menu.add_menu'), [field(t('core.menu.name'), name, { required: true }), field(t('core.menu.location'), loc), field(t('core.menu.position'), pos),
    h('h3', null, t('core.menu.items')), list,
    button(t('core.menu.add_item'), () => { rows.push({ depth: 0, labels: {}, url: '', cat: '' }); draw(); list.querySelector<HTMLInputElement>('fieldset:last-child input')?.focus(); })], t('common.save'), async () => {
    const body = { name: name.value, location: loc.value, position: Number(pos.value) || 0, items: unflatten(rows.filter((r) => Object.values(r.labels).some((v) => v.trim()))) };
    if (m) await api.put(`${BASE}/menus/${m.id}`, body);
    else await api.post(`${BASE}/menus`, body);
    cache = null;
    toast(t('common.saved'));
    await rerender();
  }, { wide: true });
}

async function adminPage(container: HTMLElement): Promise<void> {
  let ms: Menu[];
  try { ms = await api.get<Menu[]>(`${BASE}/menus?all=1`); } catch (e) { mount(container, h('p', { role: 'alert' }, errorText(e))); return; }
  mount(container, pageHeader(t('core.menu.admin_title'), button(t('core.menu.add_menu'), () => menuDialog(null), { variant: 'primary' })),
    ms.length ? h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('core.menu.admin_title')),
      h('thead', null, h('tr', null, ...[t('core.menu.name'), t('core.menu.location'), t('core.menu.items'), t('common.actions')].map((x) => h('th', { scope: 'col' }, x)))),
      h('tbody', null, ...ms.map((m) => h('tr', null, h('th', { scope: 'row' }, m.name), h('td', null, m.location), h('td', null, String(flatten(m.items).length)),
        h('td', { class: 'ew-actions' },
          button(t('common.edit'), () => menuDialog(m), { variant: 'link', ariaLabel: `${t('common.edit')} ${m.name}` }),
          button(t('common.delete'), async () => {
            if (!(await confirmDialog(t('core.menu.delete_confirm', { name: m.name }), t('common.delete'), true))) return;
            try { await api.del(`${BASE}/menus/${m.id}`); cache = null; await rerender(); } catch (e) { toast(errorText(e), 'error'); }
          }, { variant: 'link', ariaLabel: `${t('common.delete')} ${m.name}` }))))))
      : h('p', null, t('core.menu.empty')));
}

export function register(): void {
  registerSlot({ id: 'core.menu.header', position: 'header-nav', order: 10, render: slot('header-nav', true) });
  registerSlot({ id: 'core.menu.sidebar', position: 'sidebar', order: 20, render: slot('sidebar', false) });
  registerSlot({ id: 'core.menu.footer', position: 'footer', order: 10, render: slot('footer', false) });
  registerAdminPage({ id: 'menus', label: () => t('core.menu.admin_title'), order: 10, render: adminPage });
}

