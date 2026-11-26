// All notifications of the signed-in person (/notifications): newest first,
// page by page, all or only unread ones. Each entry names who caused it with
// the menu on names (the id comes only for people the reader may open), can
// be opened, marked read and deleted. The bell shows only the latest ones and
// links here.
import { api } from '../core/api';
import { fmtDate, h, mount } from '../core/dom';
import { t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { state } from '../core/state';
import { announce, toast } from '../core/announce';
import { button, errorText, pageHeader } from '../components/ui';
import { setContext } from '../components/widgets';
import { personName } from '../components/person';
import { errorPage } from './content';

interface Item {
  id: number; kind: string; title: string; body: string; link: string;
  actor: string; actor_id?: number; created_at: string; read_at: string | null;
}
interface Page { items: Item[]; unread: number; more: boolean }

const PAGE = 30;

export async function notificationsView({ main, query }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  const unreadOnly = query.get('unread') === '1';
  const list = h('ul', { class: 'ew-notif-list' });
  const status = h('p', { class: 'ew-notif-count', role: 'status' });
  const more = button(t('notifications.more'), () => void load(false), { variant: 'secondary' });
  const setCount = (n: number) => { status.textContent = n ? t('notifications.unread_count', { n }) : t('notifications.all_read'); };

  const entry = (n: Item): HTMLElement => {
    const li = h('li', { class: ['ew-notif', n.read_at ? '' : 'is-unread'], 'data-id': String(n.id) });
    const markRead = n.read_at ? null : button(t('notifications.mark_read'), async () => {
      try {
        await api.post('me/inbox/read', { ids: [n.id] });
        n.read_at = new Date().toISOString();
        li.replaceWith(entry(n));
        announce(t('notifications.marked_read'));
      } catch (e) { toast(errorText(e), 'error'); }
    }, { variant: 'link', ariaLabel: t('notifications.mark_read_label', { title: n.title }) });
    const del = button(t('notifications.delete'), async () => {
      try {
        await api.del(`me/inbox/${n.id}`);
        li.remove();
        announce(t('inbox.deleted'));
        if (!list.childElementCount) list.append(h('li', null, t('inbox.empty')));
      } catch (e) { toast(errorText(e), 'error'); }
    }, { variant: 'link', ariaLabel: t('notifications.delete_label', { title: n.title }) });
    const title = n.link
      ? h('a', {
        href: n.link,
        onclick: async (ev: MouseEvent) => {
          if (ev.ctrlKey || ev.metaKey || ev.shiftKey || ev.button !== 0) return;
          ev.preventDefault();
          if (!n.read_at) await api.post('me/inbox/read', { ids: [n.id] }).catch(() => undefined);
          navigate(n.link);
        },
      }, n.title)
      : h('span', null, n.title);
    if (n.body) li.append(h('h2', { class: 'ew-notif-title' }, title), h('p', { class: 'ew-notif-body' }, n.body));
    else li.append(h('h2', { class: 'ew-notif-title' }, title));
    li.append(
      h('p', { class: 'ew-meta' },
        n.actor ? h('span', null, t('notifications.by'), ' ', personName(n.actor, n.actor_id)) : null,
        n.actor ? ' · ' : null,
        h('time', { datetime: n.created_at }, fmtDate(n.created_at, state.lang)),
        n.read_at ? null : ` · ${t('inbox.unread')}`),
      h('div', { class: 'ew-actions' }, markRead, del));
    return li;
  };

  const load = async (fromTop: boolean): Promise<void> => {
    // page after the oldest entry still shown (a deleted one no longer exists)
    const last = fromTop ? undefined : (list.lastElementChild as HTMLElement | null)?.dataset.id;
    const first = !last;
    const q = new URLSearchParams({ limit: String(PAGE) });
    if (unreadOnly) q.set('unread', '1');
    if (last) q.set('before', last);
    let res: Page;
    try {
      res = await api.get<Page>(`me/inbox?${q.toString()}`);
    } catch (e) {
      if (fromTop) errorPage(main, e);
      else toast(errorText(e), 'error');
      return;
    }
    setCount(res.unread);
    if (first) list.replaceChildren();
    if (first && !res.items.length) list.append(h('li', null, unreadOnly ? t('notifications.none_unread') : t('inbox.empty')));
    list.append(...res.items.map(entry));
    more.hidden = !res.more;
    // keyboard users continue at the first newly loaded entry
    const next = first ? null : list.children[list.children.length - res.items.length]?.querySelector<HTMLElement>('h2');
    if (next) { next.tabIndex = -1; next.focus(); }
  };

  const markAll = button(t('inbox.mark_all'), async () => {
    try {
      await api.post('me/inbox/read', {});
      announce(t('notifications.marked_all'));
      await load(true);
    } catch (e) { toast(errorText(e), 'error'); }
  }, { variant: 'secondary' });
  const filter = h('nav', { class: 'ew-notif-filter', 'aria-label': t('notifications.filter') },
    h('a', { href: '/notifications', 'aria-current': unreadOnly ? undefined : 'page' }, t('notifications.show_all')),
    h('a', { href: '/notifications?unread=1', 'aria-current': unreadOnly ? 'page' : undefined }, t('notifications.show_unread')));
  more.hidden = true;
  mount(main, pageHeader(t('notifications.title')), h('div', { class: 'ew-notif-tools' }, filter, markAll), status, list, h('div', { class: 'ew-actions' }, more));
  await load(true);
}
