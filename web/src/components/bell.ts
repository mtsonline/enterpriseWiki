// Notifications in the wiki (bell next to the user menu): mentions, shared
// pages and other messages; the list opens below the bell.
import { api } from '../core/api';
import { fmtDate, h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { navigate } from '../core/router';
import { state } from '../core/state';
import { announce } from '../core/announce';
import { button } from './ui';

interface Item { id: number; kind: string; title: string; body: string; link: string; actor: string; created_at: string; read_at: string | null }

let timer = 0;
let lastUnread = -1;

export function notificationBell(): HTMLElement | null {
  if (!state.user) return null;
  const panelId = uid('inbox');
  const count = h('span', { class: 'ew-bell-count', hidden: true });
  const label = h('span', { class: 'sr-only' }, t('inbox.title'));
  const toggle = h('button', { type: 'button', class: 'ew-btn ew-btn--subtle ew-bell', 'aria-expanded': 'false', 'aria-controls': panelId },
    h('span', { 'aria-hidden': 'true', class: 'ew-bell-icon' }, '🔔'), label, count);
  const list = h('ul', { class: 'ew-inbox-list' });
  const panel = h('div', { id: panelId, class: 'ew-inbox', hidden: true, role: 'region', 'aria-label': t('inbox.title') },
    h('div', { class: 'ew-inbox-head' }, h('h2', null, t('inbox.title')),
      button(t('inbox.mark_all'), async () => { await api.post('me/inbox/read', {}); await load(true); }, { variant: 'link' })),
    list);
  const wrap = h('div', { class: 'ew-bell-wrap' }, toggle, panel);
  const setCount = (n: number) => {
    count.hidden = n === 0;
    count.textContent = n > 99 ? '99+' : String(n);
    label.textContent = n ? t('inbox.title_unread', { n }) : t('inbox.title');
    if (lastUnread >= 0 && n > lastUnread) announce(t('inbox.new', { n: n - lastUnread }));
    lastUnread = n;
  };
  const load = async (draw: boolean, background = false) => {
    let res: { items: Item[]; unread: number };
    try { res = await (background ? api.poll : api.get)<{ items: Item[]; unread: number }>('me/inbox?limit=20'); } catch { return; }
    setCount(res.unread);
    if (!draw) return;
    list.replaceChildren(...(res.items.length ? res.items.map((n) => h('li', { class: n.read_at ? undefined : 'is-unread' },
      h('a', {
        href: n.link || '#',
        onclick: async (ev: MouseEvent) => {
          ev.preventDefault();
          if (!n.read_at) await api.post('me/inbox/read', { ids: [n.id] }).catch(() => undefined);
          setOpen(false);
          if (n.link) navigate(n.link);
        },
      }, h('strong', null, n.title), n.body ? h('span', { class: 'ew-inbox-body' }, n.body.length > 160 ? `${n.body.slice(0, 160)} …` : n.body) : null,
      h('span', { class: 'ew-meta' }, fmtDate(n.created_at, state.lang), n.read_at ? '' : ` · ${t('inbox.unread')}`)))) : [h('li', null, t('inbox.empty'))]));
  };
  const setOpen = (open: boolean) => {
    panel.hidden = !open;
    toggle.setAttribute('aria-expanded', String(open));
    if (open) void load(true);
  };
  toggle.addEventListener('click', () => setOpen(panel.hidden === true));
  wrap.addEventListener('keydown', (ev) => { if (ev.key === 'Escape' && !panel.hidden) { setOpen(false); toggle.focus(); } });
  wrap.addEventListener('focusout', (ev) => { if (!wrap.contains(ev.relatedTarget as Node)) setOpen(false); });
  void load(false);
  window.clearInterval(timer);
  timer = window.setInterval(() => { if (document.visibilityState === 'visible' && state.user) void load(!panel.hidden, true); }, 60000);
  return wrap;
}
