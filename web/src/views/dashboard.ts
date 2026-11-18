// The own dashboard (/dashboard): tiles over the full width and in three columns. Everybody arranges
// the own dashboard (also when personal placement is switched off); the
// administration sets the default, required tiles and which tiles are
// offered. Arranging works with the mouse (dragging) and without it: the
// arrangement dialog with selects and ↑ ↓ (WCAG 2.2, 2.5.7).
import { api } from '../core/api';
import { h, mount, fmtDate } from '../core/dom';
import { tile } from '../components/tile';
import { pick, t } from '../core/i18n';
import { navigate, type RouteCtx } from '../core/router';
import { isAdmin, state } from '../core/state';
import { announce, toast } from '../core/announce';
import { button, checkbox, errorText, openDialog } from '../components/ui';
import { setContext } from '../components/widgets';
import { placementEditor } from '../components/placement';
import { adminSections } from './admin';
import {
  DASHBOARD_POSITIONS, invalidatePlacements, pagePositionContainer, placeableBlocks, refreshPositions, registerSlot,
  type Placement, type PlacementLevel, type PagePosition,
} from '../coreplugin';
import { type Watch } from '../components/watch';


export async function dashboardView({ main }: RouteCtx): Promise<void> {
  setContext(0, [], []);
  if (!state.user) {
    navigate(`/login?next=${encodeURIComponent('/dashboard')}`, { replace: true });
    return;
  }
  const empty = h('p', { class: 'ew-muted', hidden: true }, t('dashboard.empty'));
  const cols: HTMLElement[] = [];
  const check = () => { empty.hidden = cols.some((c) => c.childElementCount > 0); };
  for (const p of DASHBOARD_POSITIONS) cols.push(pagePositionContainer(p, {}, () => { check(); if (dragging) prepareDrag(); }));
  // the full width above, then the columns
  const board = h('div', { class: 'ew-dashboard' }, cols[0], h('div', { class: 'ew-dashboard-cols' }, ...cols.slice(1)));

  // without the mouse: the arrangement dialog (selects, ↑ ↓)
  // (a tile is wide at the position "full width")
  const arrange = button(t('dashboard.arrange'), () => {
    void (async () => {
      let handle: { close: () => void } | null = null;
      const editor = await placementEditor('user', String(state.user!.id), { heading: false, kinds: ['dashboard'], onSaved: () => handle?.close() });
      handle = openDialog(t('dashboard.arrange_title'), editor, [], { wide: true });
    })();
  }, { variant: 'secondary' });

  // with the mouse: drag tiles between and within the columns
  let dragging = false;
  let required = new Set<string>();
  const drag = button(t('dashboard.drag'), () => {
    dragging = !dragging;
    drag.setAttribute('aria-pressed', String(dragging));
    board.classList.toggle('ew-dashboard--drag', dragging);
    if (dragging) void loadRequired().then(prepareDrag);
    else for (const tl of board.querySelectorAll<HTMLElement>('.ew-tile')) tl.draggable = false;
    announce(t(dragging ? 'dashboard.drag_on' : 'dashboard.drag_off'));
  }, { variant: 'secondary' });
  drag.setAttribute('aria-pressed', 'false');
  const loadRequired = async () => {
    const levels = await api.get<PlacementLevel[]>('placements');
    required = new Set(levels.filter((l) => l.level === 'template' || l.level === 'site').flatMap((l) => l.placements.filter((p) => p.required).map((p) => p.block)));
  };
  const prepareDrag = () => {
    for (const tl of board.querySelectorAll<HTMLElement>('.ew-tile')) tl.draggable = !required.has(tl.dataset.block ?? '');
  };
  let moving: HTMLElement | null = null;
  board.addEventListener('dragstart', (ev) => {
    const tl = (ev.target as HTMLElement).closest<HTMLElement>('.ew-tile');
    if (!dragging || !tl || !tl.draggable) return;
    moving = tl;
    tl.classList.add('is-moving');
    ev.dataTransfer?.setData('text/plain', tl.dataset.block ?? '');
  });
  board.addEventListener('dragover', (ev) => {
    if (!moving) return;
    const col = (ev.target as HTMLElement).closest<HTMLElement>('.ew-pos');
    if (!col) return;
    ev.preventDefault();
    // before the first tile whose middle is below the pointer
    const after = Array.from(col.querySelectorAll<HTMLElement>(':scope > .ew-tile')).find((x) => x !== moving && ev.clientY < x.getBoundingClientRect().top + x.offsetHeight / 2);
    if (after) col.insertBefore(moving, after);
    else col.append(moving);
  });
  board.addEventListener('dragend', () => {
    if (!moving) return;
    moving.classList.remove('is-moving');
    moving = null;
    void saveFromBoard();
  });

  // the own level holds the rest of the interface too: keep it, replace the tiles
  const saveFromBoard = async () => {
    try {
      const target = String(state.user!.id);
      const cur = await api.get<PlacementLevel>(`placements/user?target=${target}`);
      const kinds = new Map((await placeableBlocks()).map((b) => [b.id, b.kind]));
      const isTile = (b: string) => kinds.get(b) === 'dashboard';
      const rows: Placement[] = cur.placements.filter((p) => !isTile(p.block));
      const shown = new Set<string>();
      for (const col of cols) {
        Array.from(col.querySelectorAll<HTMLElement>(':scope > .ew-tile')).forEach((tl, i) => {
          const b = tl.dataset.block!;
          shown.add(b);
          if (required.has(b)) return; // required tiles stay where the administration put them
          rows.push({ block: b, position: col.dataset.position!, sort: (i + 1) * 10 });
        });
      }
      // tiles hidden before stay hidden
      rows.push(...cur.placements.filter((p) => isTile(p.block) && p.position === '' && !shown.has(p.block)));
      await api.put(`placements/user?target=${target}`, { placements: rows });
      invalidatePlacements();
      await refreshPositions();
      toast(t('dashboard.saved'));
    } catch (e) { toast(errorText(e), 'error'); }
  };

  mount(main, h('div', { class: 'ew-dashboard-page' },
    h('div', { class: 'ew-page-header' }, h('h1', null, t('dashboard.title')), h('div', { class: 'ew-actions' }, drag, arrange)),
    h('p', { class: 'ew-help' }, t('dashboard.help'),
      // the dashboard is the start page: the start page of the site stays reachable
      state.site.dashboard_home ? [' ', h('a', { href: '/start' }, t('dashboard.site_home'))] : null),
    empty, board));
}

/** Tiles of the core. */
export function registerDashboardTiles(): void {
  const L = () => state.lang;
  const D = () => state.site.default_language;
  const item = (href: string, title: string, meta?: string) => h('li', null, h('a', { href }, title || t('dashboard.untitled')), meta ? h('span', { class: 'ew-meta' }, ` · ${meta}`) : null);
  const list = (items: HTMLElement[], none: string) => (items.length ? h('ul', { class: 'ew-tile-list' }, ...items) : h('p', { class: 'ew-muted' }, none));
  const reg = (id: string, position: PagePosition, order: number, render: () => Promise<HTMLElement | null>) =>
    registerSlot({ id, kind: 'dashboard', position, order, label: () => t(`block.${id}`), render: async () => (state.user ? render().catch(() => null) : null) });

  reg('core.dash.notifications', 'dashboard-1', 10, async () => {
    const res = await api.get<{ items: { id: number; title: string; link: string; created_at: string; read_at: string | null }[]; unread: number }>('me/inbox?limit=8');
    return tile(t('block.core.dash.notifications'), list(res.items.map((n) => item(n.link || '/', n.title, fmtDate(n.created_at, L()) + (n.read_at ? '' : ` · ${t('inbox.unread')}`))), t('inbox.empty')));
  });
  reg('core.dash.mine', 'dashboard-2', 10, async () => {
    const items = await api.get<{ id: number; titles: Record<string, string>; updated_at: string }[]>('recent?by=me&limit=8');
    return tile(t('block.core.dash.mine'), list(items.map((x) => item(`/e/${x.id}`, pick(x.titles, L(), D()), fmtDate(x.updated_at, L()))), t('dashboard.none_mine')));
  });
  reg('core.dash.recent', 'dashboard-3', 10, async () => {
    const items = await api.get<{ id: number; titles: Record<string, string>; updated_at: string }[]>('recent?limit=8');
    return tile(t('block.core.dash.recent'), list(items.map((x) => item(`/e/${x.id}`, pick(x.titles, L(), D()), fmtDate(x.updated_at, L()))), t('dashboard.none_recent')));
  });
  reg('core.dash.admin', 'dashboard-1', 30, adminStatusTile);
  reg('core.dash.watches', 'dashboard-3', 20, async () => {
    const data = await api.get<{ items: Watch[] }>('me/watches');
    return tile(t('block.core.dash.watches'), list(data.items.slice(0, 8).map((w) => item(w.target_type === 'category' ? `/c/${w.target_id}` : `/e/${w.target_id}`,
      pick(w.titles, L(), D()) || t('watch.deleted'), t(`watch.type.${w.target_type}`))), t('dashboard.none_watches')), { href: '/profile#wt-h', label: t('dashboard.all_watches') });
  });
}

interface AdminStatus {
  version: string; updates_checked_at: string | null; updates_available: number;
  mail_failed: number; mail_queued: number; queue_pending: number; queue_oldest: string | null;
  login_failed_24h: number; audit_forward_errors: number;
  plugins_inactive: string[]; restart_pending: boolean; build_failed: boolean;
  license_level: string; database_level: string;
}

/** The state of the wiki for administrators (GET admin/status). Problems are
 * marked with a word, not only with a colour (WCAG 1.4.1). */
async function adminStatusTile(): Promise<HTMLElement | null> {
  if (!isAdmin()) return null;
  const st = await api.get<AdminStatus>('admin/status');
  const L = state.lang;
  const mark = (level: 'ok' | 'warn' | 'error') => (level === 'ok' ? null
    : h('span', { class: ['ew-badge', 'ew-badge--warn'] }, t(level === 'error' ? 'dashboard.st_error' : 'dashboard.st_warn')));
  const lvl = (l: string): 'ok' | 'warn' | 'error' => (l === 'error' ? 'error' : l === 'warning' ? 'warn' : 'ok');
  const stale = st.queue_oldest !== null && Date.now() - new Date(st.queue_oldest).getTime() > 15 * 60 * 1000;
  const rows: [string, string, 'ok' | 'warn' | 'error', string?][] = [
    [t('dashboard.st_version'), st.version, 'ok'],
    [t('dashboard.st_updates'), st.updates_checked_at ? t('dashboard.st_updates_n', { n: st.updates_available, date: fmtDate(st.updates_checked_at, L) }) : t('dashboard.st_not_checked'),
      st.updates_available > 0 ? 'warn' : 'ok', '/admin/updates'],
    [t('dashboard.st_mail'), t('dashboard.st_mail_n', { failed: st.mail_failed, queued: st.mail_queued }), st.mail_failed > 0 ? 'warn' : 'ok', '/admin/mail'],
    [t('dashboard.st_queue'), t('dashboard.st_queue_n', { n: st.queue_pending }), stale ? 'warn' : 'ok'],
    [t('dashboard.st_logins'), t('dashboard.st_logins_n', { n: st.login_failed_24h }), 'ok', '/admin/audit'],
    [t('dashboard.st_forward'), st.audit_forward_errors ? t('dashboard.st_forward_errors') : t('dashboard.st_fine'), st.audit_forward_errors ? 'error' : 'ok', '/admin/audit'],
    [t('dashboard.st_plugins'), [
      st.plugins_inactive.length ? t('dashboard.st_plugins_inactive', { list: st.plugins_inactive.join(', ') }) : '',
      st.restart_pending ? t('dashboard.st_restart') : '', st.build_failed ? t('dashboard.st_build_failed') : '',
    ].filter(Boolean).join(' · ') || t('dashboard.st_fine'), st.build_failed || st.plugins_inactive.length ? 'error' : st.restart_pending ? 'warn' : 'ok', '/admin/plugins'],
    [t('dashboard.st_license'), t(`dashboard.st_level_${lvl(st.license_level)}`), lvl(st.license_level), '/admin/license'],
    [t('dashboard.st_database'), st.database_level ? t(`dashboard.st_level_${lvl(st.database_level)}`) : '–', lvl(st.database_level), '/admin/settings'],
  ];
  // links only to the administration pages the person may open
  const open = new Set(adminSections().map((x) => `/admin/${x.id}`));
  return tile(t('block.core.dash.admin'), h('dl', { class: 'ew-dl ew-tile-status' }, ...rows.flatMap(([k, v, l, href]) => [
    h('dt', null, href && open.has(href) ? h('a', { href }, k) : k), h('dd', null, v, l === 'ok' ? null : ' ', mark(l))])));
}

/** Administration: which tiles everybody may put on the own dashboard
 * (ui.dashboard_tiles; none chosen = all). */
export async function offeredTilesForm(): Promise<HTMLElement> {
  const tiles = (await placeableBlocks()).filter((b) => b.kind === 'dashboard');
  const s = await api.get<{ config: { ui: Record<string, unknown> & { dashboard_tiles?: string[] } } }>('admin/settings');
  const offered = new Set(s.config.ui.dashboard_tiles ?? []);
  const boxes = tiles.map((b) => ({ id: b.id, box: checkbox(b.label, offered.size === 0 || offered.has(b.id)) }));
  const status = h('p', { class: 'sr-only', 'aria-live': 'polite' });
  const save = button(t('common.save'), async () => {
    const chosen = boxes.filter((x) => x.box.input.checked).map((x) => x.id);
    try {
      // all chosen: stored as "all" (also tiles of plugins added later)
      await api.put('admin/settings', { ui: { ...s.config.ui, dashboard_tiles: chosen.length === boxes.length ? [] : chosen } });
      state.site.dashboard_tiles = chosen.length === boxes.length ? [] : chosen;
      toast(t('dashboard.offered_saved'));
    } catch (e) { toast(errorText(e), 'error'); }
  }, { variant: 'primary' });
  return h('fieldset', { class: 'ew-fieldset ew-panel' }, h('legend', null, t('dashboard.offered')),
    h('p', { class: 'ew-help' }, t('dashboard.offered_help')),
    h('div', { class: 'ew-check-grid' }, ...boxes.map((x) => x.box.el)), status, h('div', { class: 'ew-actions' }, save));
}
