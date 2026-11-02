// Feed subscription button and dialog (Atom/RSS). Logged-in users get a
// personal link that works in feed readers without signing in.
import { api, qs } from '../core/api';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { toast } from '../core/announce';
import { button, confirmDialog, openDialog } from './ui';

let token: Promise<string> | null = null;

/** The personal feed token of the current user ('' for guests). */
export function feedToken(): Promise<string> {
  if (!state.user) return Promise.resolve('');
  if (!token) {
    token = api.get<{ token: string }>('me/feed-token').then((r) => r.token).catch(() => '');
  }
  return token;
}

/** Forgets the cached token (after rotating). */
export function resetFeedToken(): void { token = null; }

/** Absolute feed URL; path is an API path like "/api/v1/feeds/categories/3". */
export async function feedURL(path: string, format: 'atom' | 'rss', params: Record<string, string> = {}): Promise<string> {
  const tok = await feedToken();
  return `${location.origin}${path}${qs({ ...params, format, lang: state.lang, token: tok || undefined })}`;
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
  return button(t('feed.button'), () => void feedDialog(path, title, params), { variant: 'secondary', ariaLabel: `${t('feed.button')}: ${title}` });
}

async function feedDialog(path: string, title: string, params: Record<string, string>): Promise<void> {
  const [atom, rss] = await Promise.all([feedURL(path, 'atom', params), feedURL(path, 'rss', params)]);
  const row = (label: string, url: string) => {
    const input = h('input', { class: 'ew-input', readonly: true, value: url, id: uid('feed') }) as HTMLInputElement;
    input.addEventListener('focus', () => input.select());
    return h('div', { class: 'ew-field' }, h('label', { for: input.id }, label),
      h('div', { class: 'ew-picker' }, input,
        button(t('feed.copy'), () => {
          void navigator.clipboard?.writeText(url).then(() => toast(t('feed.copied'))).catch(() => input.select());
        }, { variant: 'secondary', ariaLabel: `${t('feed.copy')}: ${label}` }),
        h('a', { href: url, class: 'ew-btn ew-btn--subtle', target: '_blank', rel: 'noopener' }, t('feed.open'))));
  };
  openDialog(t('feed.title', { title }), [
    h('p', null, t('feed.help')),
    row('Atom', atom), row('RSS', rss),
    state.user ? h('p', { class: 'ew-help', role: 'note' }, t('feed.personal_help'), ' ', h('a', { href: '/profile#feeds' }, t('feed.manage'))) : null,
  ]);
}

/** Profile section: show, renew and revoke the personal feed link. */
export function feedTokenSection(): HTMLElement {
  const status = h('p');
  const sec = h('section', { class: 'ew-section', id: 'feeds', 'aria-labelledby': 'fd-h' }, h('h2', { id: 'fd-h' }, t('feed.profile_title')),
    h('p', null, t('feed.profile_help')), status,
    h('div', { class: 'ew-actions' },
      button(t('feed.rotate'), async () => {
        if (!(await confirmDialog(t('feed.rotate_confirm'), t('feed.rotate')))) return;
        await api.post('me/feed-token');
        resetFeedToken();
        toast(t('feed.rotated'));
      }, { variant: 'secondary' }),
      button(t('feed.revoke'), async () => {
        if (!(await confirmDialog(t('feed.revoke_confirm'), t('feed.revoke'), true))) return;
        await api.del('me/feed-token');
        resetFeedToken();
        token = Promise.resolve('');
        toast(t('feed.revoked'));
      }, { variant: 'danger' })));
  return sec;
}
