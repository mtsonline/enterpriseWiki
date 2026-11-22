// Maintenance mode (Administration → Maintenance): a bar on every page while
// the wiki is read-only (and for administrators in every mode), and the page
// shown instead of the wiki while it is closed.
import { api } from '../core/api';
import { h, mount } from '../core/dom';
import { pick, t } from '../core/i18n';
import { isAdmin, state } from '../core/state';
import { button } from './ui';

export interface MaintenanceInfo { mode: 'read_only' | 'closed'; message: Record<string, string>; until: string }

function untilText(m: MaintenanceInfo): string {
  if (!m.until) return '';
  const d = new Date(m.until);
  if (Number.isNaN(d.getTime())) return '';
  return t('maintenance.until', { time: d.toLocaleString(state.lang, { dateStyle: 'full', timeStyle: 'short' }) });
}

/** message: the text of the administration (plain text, lines kept) or the standard text. */
function message(m: MaintenanceInfo): HTMLElement[] {
  const own = pick(m.message, state.lang, state.site.default_language);
  const text = own || t(m.mode === 'closed' ? 'maintenance.closed_text' : 'maintenance.read_only_text');
  return text.split(/\n{2,}/).map((p) => h('p', { class: 'ew-maint-text' }, p));
}

/** closedPage: what people (not administrators) see while the wiki is closed. */
export function closedPage(app: HTMLElement): void {
  const m = state.site.maintenance as MaintenanceInfo;
  document.title = `${t('maintenance.title')} – ${state.site.name}`;
  const until = untilText(m);
  mount(app, h('main', { id: 'main', class: 'ew-maint-page', tabindex: '-1' },
    h('h1', null, t('maintenance.title')),
    ...message(m),
    until ? h('p', null, until) : null,
    state.user
      ? h('p', null, button(t('auth.logout'), async () => { await api.post('auth/logout').catch(() => undefined); location.href = '/'; }))
      : h('p', null, h('a', { href: '/login', class: 'ew-btn ew-btn--secondary' }, t('maintenance.admin_login')))));
}

/** maintenanceBar: the bar at the top while maintenance is on (null: none). */
export function maintenanceBar(): HTMLElement | null {
  const m = state.site.maintenance as MaintenanceInfo | undefined;
  if (!m) return null;
  const until = untilText(m);
  const own = isAdmin();
  if (!own && m.mode !== 'read_only') return null;
  return h('div', { class: 'ew-try-bar ew-maint-bar', role: 'region', 'aria-label': t('maintenance.title') },
    h('div', null,
      h('p', null, h('strong', null, t(m.mode === 'closed' ? 'maintenance.bar_closed' : 'maintenance.bar_read_only')), own ? ` ${t('maintenance.bar_admin')}` : ''),
      ...(own ? [] : message(m)),
      until ? h('p', null, until) : null),
    own ? h('a', { href: '/admin/maintenance', class: 'ew-btn ew-btn--secondary' }, t('maintenance.manage')) : null);
}
