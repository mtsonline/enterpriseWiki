// Core widgets placed into template positions.
import { notificationBell } from './bell';
import { api } from '../core/api';
import { h, uid } from '../core/dom';
import { loadMessages, pick, t } from '../core/i18n';
import { navigate, rerender } from '../core/router';
import { changed, isAdmin, hasPerm, rememberLang, setEditMode, state } from '../core/state';
import type { Category } from '../core/types';
import { announce, toast } from '../core/announce';
import { refreshPositions } from '../sdk';
import { button, errorText } from './ui';

/** Context of the current page (set by category and element views). */
export const ctx = {
  categoryId: 0,
  path: [] as number[],
  perms: [] as string[],
};

export function setContext(categoryId: number, path: number[], perms: string[] | null): void {
  ctx.categoryId = categoryId;
  ctx.path = path;
  ctx.perms = perms ?? [];
}

export function canEditHere(): boolean {
  return !!state.user && (isAdmin() || ctx.perms.some((p) => p !== 'view'));
}

export function can(perm: string): boolean {
  return ctx.perms.includes(perm);
}

// ---------------------------------------------------------------- search

export function searchBox(): HTMLElement {
  const id = uid('search');
  const input = h('input', { id, type: 'search', name: 'q', class: 'ew-input ew-search-input', autocomplete: 'off', placeholder: t('search.placeholder') });
  const form = h('form', { role: 'search', class: 'ew-search', 'aria-label': t('search.label') },
    h('label', { for: id, class: 'sr-only' }, t('search.label')),
    input,
    button(t('search.submit'), () => undefined, { type: 'submit', variant: 'secondary' }),
  );
  form.addEventListener('submit', (ev) => {
    ev.preventDefault();
    const q = input.value.trim();
    if (q) navigate(`/search?q=${encodeURIComponent(q)}`);
  });
  const cur = new URLSearchParams(location.search).get('q');
  if (location.pathname === '/search' && cur) input.value = cur;
  return form;
}

// ---------------------------------------------------------------- language

/** Switches UI and content language. */
export async function setLanguage(lang: string): Promise<void> {
  try {
    await loadMessages(lang);
  } catch (e) {
    toast(errorText(e), 'error');
    return;
  }
  state.lang = lang;
  rememberLang(lang);
  changed();
  window.dispatchEvent(new Event('ewiki:relayout'));
  await rerender();
  await refreshPositions();
  document.querySelector<HTMLElement>('.ew-lang select')?.focus();
  announce(t('lang.switched', { lang: langName(lang) }));
}

export function langName(code: string): string {
  const k = `lang.${code}`;
  const v = t(k);
  if (v !== k) return v;
  try {
    const n = new Intl.DisplayNames([state.lang || 'en'], { type: 'language' }).of(code);
    if (n && n !== code) return n.charAt(0).toLocaleUpperCase(state.lang) + n.slice(1);
  } catch { /* unknown code */ }
  return code.toUpperCase();
}

export function languageSwitcher(): HTMLElement | null {
  const langs = state.site.languages;
  if (langs.length < 2) return null;
  const id = uid('lang');
  const sel = h('select', { id, class: 'ew-input ew-lang-select' },
    ...langs.map((l) => h('option', { value: l, selected: l === state.lang, lang: l }, langName(l))));
  sel.addEventListener('change', () => void setLanguage(sel.value));
  return h('div', { class: 'ew-lang' }, h('label', { for: id }, t('lang.label')), sel);
}

// ---------------------------------------------------------------- edit mode

export function editToggle(): HTMLElement | null {
  if (!canEditHere()) return null;
  return button(state.editMode ? t('edit.mode_on') : t('edit.mode_off'), () => {
    setEditMode(!state.editMode);
    void rerender();
    announce(state.editMode ? t('edit.mode_enabled') : t('edit.mode_disabled'));
  }, { variant: state.editMode ? 'primary' : 'secondary', pressed: state.editMode });
}

// ---------------------------------------------------------------- user menu

export function userMenu(): HTMLElement {
  if (!state.user) {
    const next = encodeURIComponent(location.pathname + location.search);
    return h('a', { href: `/login?next=${next}`, class: 'ew-btn ew-btn--primary' }, t('auth.login'));
  }
  const menuId = uid('menu');
  const items: HTMLElement[] = [
    h('li', null, h('a', { href: '/profile' }, t('nav.profile'))),
    h('li', null, h('a', { href: '/media' }, t('media.library'))),
    h('li', null, h('a', { href: '/trash' }, t('nav.trash'))),
  ];
  if (canOpenAdmin()) items.push(h('li', null, h('a', { href: '/admin' }, t('nav.admin'))));
  items.push(h('li', null, button(t('auth.logout'), () => void logout(), { variant: 'link' })));
  const list = h('ul', { id: menuId, class: 'ew-menu-list', hidden: true }, ...items);
  const name = state.user.display_name || state.user.username;
  const toggle = h('button', { type: 'button', class: 'ew-btn ew-btn--subtle ew-user-btn', 'aria-expanded': 'false', 'aria-controls': menuId },
    h('span', { class: 'ew-avatar', 'aria-hidden': 'true' }, name.slice(0, 1).toUpperCase()),
    h('span', null, name));
  const wrap = h('div', { class: 'ew-user-menu' }, toggle, list);
  const bell = notificationBell();
  const setOpen = (open: boolean) => {
    list.hidden = !open;
    toggle.setAttribute('aria-expanded', String(open));
  };
  toggle.addEventListener('click', () => setOpen(list.hidden === true));
  wrap.addEventListener('keydown', (ev) => {
    if (ev.key === 'Escape' && !list.hidden) {
      setOpen(false);
      toggle.focus();
    }
  });
  wrap.addEventListener('focusout', (ev) => {
    if (!wrap.contains(ev.relatedTarget as Node)) setOpen(false);
  });
  list.addEventListener('click', (ev) => {
    if ((ev.target as Element).closest('a,button')) setOpen(false);
  });
  return bell ? h('div', { class: 'ew-user-tools' }, bell, wrap) : wrap;
}

export async function logout(): Promise<void> {
  let redirect = '';
  try {
    redirect = (await api.post<{ redirect?: string } | undefined>('auth/logout'))?.redirect ?? '';
  } catch {
    /* ignore */
  }
  if (/^https?:\/\//.test(redirect)) {
    // single logout: the identity provider ends its session and sends the browser back
    window.location.assign(redirect);
    return;
  }
  state.user = null;
  setEditMode(false);
  treeCache.clear();
  changed();
  navigate('/');
  announce(t('auth.logged_out'));
}

// ---------------------------------------------------------------- category tree

export const treeCache = new Map<number, Category[]>();
const expanded = new Set<number>();

export function invalidateTree(): void {
  treeCache.clear();
}

async function children(parent: number): Promise<Category[]> {
  if (!treeCache.has(parent)) treeCache.set(parent, await api.get<Category[]>(`categories?parent=${parent}`));
  return treeCache.get(parent)!;
}

async function renderLevel(parent: number, current: number): Promise<HTMLUListElement> {
  const cats = await children(parent);
  const ul = h('ul', { class: 'ew-tree-level' }) as HTMLUListElement;
  for (const c of cats) {
    const title = pick(c.titles, state.lang, state.site.default_language);
    const isOpen = expanded.has(c.id);
    const li = h('li', { class: 'ew-tree-item' });
    const row = h('div', { class: ['ew-tree-row', c.id === current && 'is-current'] });
    if (c.has_children) {
      const tb = h('button', {
        type: 'button', class: 'ew-tree-toggle', 'aria-expanded': String(isOpen),
        'aria-label': t(isOpen ? 'tree.collapse' : 'tree.expand', { title }),
      }, h('span', { 'aria-hidden': 'true' }, isOpen ? '▾' : '▸'));
      tb.addEventListener('click', async () => {
        if (expanded.has(c.id)) expanded.delete(c.id);
        else expanded.add(c.id);
        const fresh = await renderLevel(parent, current);
        ul.replaceWith(fresh);
        fresh.querySelector<HTMLElement>(`[data-toggle-for="${c.id}"]`)?.focus();
      });
      tb.dataset.toggleFor = String(c.id);
      row.append(tb);
    } else {
      row.append(h('span', { class: 'ew-tree-spacer', 'aria-hidden': 'true' }));
    }
    row.append(h('a', { href: `/c/${c.id}`, 'aria-current': c.id === current ? 'page' : undefined },
      h('span', { class: 'ew-tree-num', 'aria-hidden': 'true' }, c.number), ' ', title));
    li.append(row);
    if (c.has_children && isOpen) li.append(await renderLevel(c.id, current));
    ul.append(li);
  }
  return ul;
}

export async function categoryTree(): Promise<HTMLElement> {
  for (const id of ctx.path.slice(0, -1)) expanded.add(id);
  const nav = h('nav', { class: 'ew-tree', 'aria-label': t('tree.label') }, h('h2', { class: 'ew-tree-title' }, t('tree.title')));
  try {
    const level = await renderLevel(0, ctx.categoryId);
    if (!level.children.length) nav.append(h('p', { class: 'ew-muted' }, t('tree.empty')));
    else nav.append(level);
  } catch (e) {
    nav.append(h('p', { role: 'alert' }, errorText(e)));
  }
  return nav;
}

// ---------------------------------------------------------------- breadcrumbs

export function breadcrumbs(items: { title: string; href?: string }[]): HTMLElement {
  return h('nav', { class: 'ew-breadcrumbs', 'aria-label': t('nav.breadcrumbs') },
    h('ol', null, ...items.map((it, i) => h('li', null,
      it.href && i < items.length - 1 ? h('a', { href: it.href }, it.title) : h('span', { 'aria-current': 'page' }, it.title)))));
}

// ---------------------------------------------------------------- category picker

export interface TreeNode { id: number; parent_id: number | null; depth: number; titles: Record<string, string>; permissions: string[] }

/** A select listing categories where the user has perm (with "top level" for admins). */
export async function categoryPicker(perm: string, selected: number, opts: { allowRoot?: boolean; exclude?: number } = {}): Promise<HTMLSelectElement> {
  const nodes = await api.get<TreeNode[]>('categories/tree');
  const sel = h('select', { class: 'ew-input' });
  if (opts.allowRoot && hasPerm('core.categories.admin')) sel.append(h('option', { value: '0', selected: selected === 0 }, t('category.top_level')));
  let skipDepth = 0;
  for (const n of nodes) {
    if (skipDepth && n.depth > skipDepth) continue;
    skipDepth = 0;
    if (opts.exclude && n.id === opts.exclude) {
      skipDepth = n.depth;
      continue;
    }
    const ok = isAdmin() || n.permissions.includes(perm);
    sel.append(h('option', { value: String(n.id), selected: n.id === selected, disabled: !ok },
      `${'— '.repeat(n.depth - 1)}${pick(n.titles, state.lang, state.site.default_language)}`));
  }
  return sel;
}

/** canOpenAdmin reports whether the user may open any administration page. */
export function canOpenAdmin(): boolean {
  if (!state.user) return false;
  return isAdmin() || state.managesCategories || (state.user.permissions ?? []).some((k) => !['core.share.use', 'core.mentions.use'].includes(k));
}
