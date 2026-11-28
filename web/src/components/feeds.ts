// Feed subscription button and dialog (Atom/RSS), personal feed links and
// the list of feed keys (profile, administration). Feed readers and
// calendar apps cannot sign in, so a personal link carries a feed key. The
// server keeps only the hash of a key: a personal link is shown once, when
// it is made, and each one has its own key, so deleting one never breaks
// the others.
import { api, qs } from '../core/api';
import { fmtDate, h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { toast } from '../core/announce';
import { button, confirmDialog, errorText, openDialog } from './ui';

/** A feed key as the server lists it (never the key itself). */
export interface FeedKey { id: number; name: string; prefix: string; created_at: string; last_used_at: string | null }

/** Absolute URL of a feed or calendar; path is an API path like
 * "/api/v1/feeds/categories/3". With key, the link is personal. */
export function feedURL(path: string, params: Record<string, string> = {}, key = ''): string {
  return `${location.origin}${path}${qs({ ...params, lang: state.lang, token: key || undefined })}`;
}

/** Adds <link rel="alternate"> for feed readers / browsers (until the next page). */
export function advertiseFeed(path: string, title: string): void {
  document.querySelectorAll('link[data-ew-feed]').forEach((l) => l.remove());
  const url = `${path}${qs({ format: 'atom', lang: state.lang })}`;
  document.head.append(h('link', { rel: 'alternate', type: 'application/atom+xml', title, href: url, 'data-ew-feed': '' }));
}

/** A "Feed" button that opens the subscription dialog. */
export function feedButton(path: string, title: string, params: Record<string, string> = {}): HTMLButtonElement {
  advertiseFeed(path, title);
  return button(t('feed.button'), () => feedDialog(path, title, params), { variant: 'secondary', ariaLabel: `${t('feed.button')}: ${title}` });
}

/** A read-only address with Copy (and Open, when wanted). */
export function addressRow(label: string, url: string, open = false): HTMLElement {
  const input = h('input', { class: 'ew-input', readonly: true, value: url, id: uid('feed') }) as HTMLInputElement;
  input.addEventListener('focus', () => input.select());
  return h('div', { class: 'ew-field' }, h('label', { for: input.id }, label),
    h('div', { class: 'ew-picker' }, input,
      button(t('feed.copy'), () => {
        void navigator.clipboard?.writeText(url).then(() => toast(t('feed.copied'))).catch(() => input.select());
      }, { variant: 'secondary', ariaLabel: `${t('feed.copy')}: ${label}` }),
      open ? h('a', { href: url, class: 'ew-btn ew-btn--subtle', target: '_blank', rel: 'noopener' }, t('feed.open')) : null));
}

/** The part of a subscription dialog for signed-in people: "Create a
 * personal link" makes a feed key named name and shows rows(key) once. */
export function personalLinks(name: string, rows: (key: string) => Node[]): HTMLElement | null {
  if (!state.user) return null;
  const box = h('div', { class: 'ew-feed-personal' });
  const create = button(t('feed.personal_create'), async () => {
    create.disabled = true;
    try {
      const k = await api.post<{ token: string }>('me/feed-keys', { name: name.slice(0, 200) });
      box.replaceChildren(
        h('p', { class: 'ew-help ew-warning', role: 'status' }, t('feed.personal_once')),
        ...rows(k.token),
        h('p', { class: 'ew-help' }, t('feed.personal_help'), ' ', h('a', { href: '/profile#feeds' }, t('feed.manage'))));
      box.querySelector<HTMLInputElement>('input')?.focus();
    } catch (e) {
      create.disabled = false;
      toast(errorText(e), 'error');
    }
  }, { variant: 'secondary' });
  box.append(h('h3', null, t('feed.personal_title')), h('p', { class: 'ew-help' }, t('feed.personal_intro')), create);
  return box;
}

function feedDialog(path: string, title: string, params: Record<string, string>): void {
  const rows = (key: string) => [
    addressRow('Atom', feedURL(path, { ...params, format: 'atom' }, key), !key),
    addressRow('RSS', feedURL(path, { ...params, format: 'rss' }, key), !key),
  ];
  openDialog(t('feed.title', { title }), [h('p', null, t('feed.help')), ...rows(''), personalLinks(title, rows)]);
}

/** The feed keys of a person: own (profile; delete each or all) or, for
 * those who manage people, of uid (delete all). */
export async function feedKeysBlock(owner?: { uid: number; name: string }): Promise<HTMLElement> {
  const base = owner ? `admin/users/${owner.uid}/feed-keys` : 'me/feed-keys';
  const box = h('div');
  const render = async () => {
    let list: FeedKey[];
    try { list = await api.get<FeedKey[]>(base); } catch (e) { box.replaceChildren(h('p', { role: 'alert' }, errorText(e))); return; }
    const del = async (fn: () => Promise<unknown>, msg: string) => {
      try { await fn(); await render(); toast(msg); } catch (e) { toast(errorText(e), 'error'); }
    };
    const rows = list.map((k) => h('tr', null,
      h('th', { scope: 'row' }, k.name, h('span', { class: 'ew-help' }, ` (${k.prefix}…)`)),
      h('td', null, fmtDate(k.created_at, state.lang)),
      h('td', null, k.last_used_at ? fmtDate(k.last_used_at, state.lang) : t('feed.never_used')),
      owner ? null : h('td', null, button(t('common.delete'), async () => {
        if (!(await confirmDialog(t('feed.delete_confirm', { name: k.name }), t('common.delete'), true))) return;
        await del(() => api.del(`me/feed-keys/${k.id}`), t('feed.deleted'));
      }, { variant: 'link', ariaLabel: `${t('common.delete')}: ${k.name}` }))));
    const headers = [t('common.name'), t('feed.created'), t('feed.last_used'), ...(owner ? [] : [t('common.actions')])];
    const actions = list.length ? [h('div', { class: 'ew-actions' }, button(t('feed.delete_all'), async () => {
      if (!(await confirmDialog(owner ? t('feed.delete_all_user_confirm', { name: owner.name }) : t('feed.delete_all_confirm'), t('feed.delete_all'), true))) return;
      await del(() => api.del(base), t('feed.deleted_all'));
    }, { variant: 'danger' }))] : [];
    box.replaceChildren(
      h('table', { class: 'ew-table' }, h('caption', { class: 'sr-only' }, t('feed.profile_title')),
        h('thead', null, h('tr', null, ...headers.map((x) => h('th', { scope: 'col' }, x)))),
        h('tbody', null, ...(rows.length ? rows : [h('tr', null, h('td', { colspan: String(headers.length) }, t('feed.none')))]))),
      ...actions);
  };
  await render();
  return box;
}

/** Profile section: the own feed keys. */
export async function feedKeysSection(): Promise<HTMLElement> {
  return h('section', { class: 'ew-section', id: 'feeds', 'aria-labelledby': 'fd-h' }, h('h2', { id: 'fd-h' }, t('feed.profile_title')),
    h('p', null, t('feed.profile_help')), await feedKeysBlock());
}
