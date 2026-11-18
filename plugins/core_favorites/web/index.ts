// Favorites: a star button in the actions of pages and categories, the list
// as a block of the placement (default: sidebar) and in the profile
// (remove, change the order with buttons).
import { api, button, errorText, h, pick, registerPageAction, registerProfileSection, registerSlot, state, t, toast, uid } from '@ewiki/coreplugin';
import './style.css';

const BASE = 'x/core.favorites';

interface Favorite { type: 'element' | 'category'; id: number; titles: Record<string, string>; url: string; urls?: Record<string, string> }
interface List { items: Favorite[] }

let data: List | null = null;
let loading: Promise<List | null> | null = null;
const listeners = new Set<() => void>();

function load(force = false): Promise<List | null> {
  if (!state.user) return Promise.resolve(null);
  if (data && !force) return Promise.resolve(data);
  loading ??= api.get<List>(`${BASE}/`).then((l) => { data = l; return l; }).catch(() => null).finally(() => { loading = null; });
  return loading;
}

function set(l: List): void {
  data = l;
  for (const f of listeners) f();
}

const title = (f: Favorite) => pick(f.titles, state.lang, state.site.default_language);
const href = (f: Favorite) => f.urls?.[state.lang] || f.url;
const isFav = (type: string, id: number) => !!data?.items.some((f) => f.type === type && f.id === id);

async function toggle(type: 'element' | 'category', id: number, name: string): Promise<void> {
  try {
    if (isFav(type, id)) {
      set(await api.del<List>(`${BASE}/${type}/${id}`));
      toast(t('core.favorites.removed', { title: name }));
    } else {
      set(await api.post<List>(`${BASE}/`, { type, id }));
      toast(t('core.favorites.added', { title: name }));
    }
  } catch (e) { toast(errorText(e), 'error'); }
}

function starButton(target: { type: 'element' | 'category'; id: number; title: string }): HTMLElement | null {
  if (!state.user) return null;
  const b = button('', () => void toggle(target.type, target.id, target.title), { variant: 'secondary' });
  b.classList.add('ewfav-star');
  const draw = () => {
    const on = isFav(target.type, target.id);
    b.setAttribute('aria-pressed', String(on));
    b.replaceChildren(h('span', { 'aria-hidden': 'true' }, on ? '★' : '☆'), ' ', t('core.favorites.button'));
    b.title = on ? t('core.favorites.remove_hint') : t('core.favorites.add_hint');
  };
  listeners.add(draw);
  draw();
  void load().then(draw);
  return b;
}

async function widget(position: string): Promise<HTMLElement | null> {
  if (!state.user) return null;
  const l = await load();
  if (!l) return null;
  // the list may be placed more than once on a page: unique ids
  const headId = uid(`ewfav-${position}`);
  const nav = h('nav', { class: 'ewfav', 'aria-labelledby': headId });
  const draw = () => {
    if (!data) { nav.hidden = true; return; }
    nav.hidden = false;
    nav.replaceChildren(h('h2', { id: headId, class: 'ewfav-title' }, t('core.favorites.title')),
      data.items.length
        ? h('ul', { class: 'ewfav-list' }, ...data.items.map((f) => h('li', null, h('a', { href: href(f), 'aria-current': location.pathname === href(f) ? 'page' : undefined }, title(f)))))
        : h('p', { class: 'ew-muted' }, t('core.favorites.empty')));
  };
  listeners.add(draw);
  draw();
  return nav;
}

async function profile(): Promise<HTMLElement | null> {
  const l = await load(true);
  if (!l) return null;
  const sec = h('section', { class: 'ew-section', 'aria-labelledby': 'fav-h' });
  const move = async (i: number, d: number) => {
    const items = [...(data?.items ?? [])];
    [items[i], items[i + d]] = [items[i + d], items[i]];
    try {
      set(await api.put<List>(`${BASE}/order`, { items: items.map((f) => ({ type: f.type, id: f.id })) }));
      (sec.querySelectorAll<HTMLElement>('.ewfav-row')[i + d]?.querySelector('a') as HTMLElement | null)?.focus();
    } catch (e) { toast(errorText(e), 'error'); }
  };
  const draw = () => {
    const items = data?.items ?? [];
    sec.replaceChildren(h('h2', { id: 'fav-h' }, t('core.favorites.title')), h('p', null, t('core.favorites.help')),
      items.length ? h('ol', { class: 'ewfav-manage' }, ...items.map((f, i) => h('li', { class: 'ewfav-row' },
        h('a', { href: href(f) }, title(f)), ' ', h('span', { class: 'ew-badge' }, t(`core.favorites.type.${f.type}`)),
        h('span', { class: 'ew-actions' },
          i > 0 ? button(t('category.move_up'), () => void move(i, -1), { variant: 'link', ariaLabel: `${t('category.move_up')}: ${title(f)}` }) : null,
          i < items.length - 1 ? button(t('category.move_down'), () => void move(i, 1), { variant: 'link', ariaLabel: `${t('category.move_down')}: ${title(f)}` }) : null,
          button(t('common.remove'), () => void toggle(f.type, f.id, title(f)), { variant: 'link', ariaLabel: `${t('common.remove')}: ${title(f)}` })))))
        : h('p', { class: 'ew-muted' }, t('core.favorites.empty')));
  };
  listeners.add(draw);
  draw();
  return sec;
}

export function register(): void {
  registerPageAction({ id: 'core.favorites', order: 5, render: (target) => starButton(target) });
  // a block of the placement: default in the sidebar; hidden there, the list
  // is only in the profile
  registerSlot({ id: 'core.favorites', position: 'sidebar', order: 20, label: () => t('core.favorites.title'), render: (position) => widget(position) });
  registerProfileSection({ id: 'core.favorites', order: 5, render: profile });
}
