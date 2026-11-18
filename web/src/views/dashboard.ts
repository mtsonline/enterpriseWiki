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
import { state } from '../core/state';
import { announce, toast } from '../core/announce';
import { button, checkbox, errorText, openDialog } from '../components/ui';
import { setContext } from '../components/widgets';
import { placementEditor } from '../components/placement';
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
    h('p', { class: 'ew-help' }, t('dashboard.help')),
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
  reg('core.dash.watches', 'dashboard-3', 20, async () => {
    const data = await api.get<{ items: Watch[] }>('me/watches');
    return tile(t('block.core.dash.watches'), list(data.items.slice(0, 8).map((w) => item(w.target_type === 'category' ? `/c/${w.target_id}` : `/e/${w.target_id}`,
      pick(w.titles, L(), D()) || t('watch.deleted'), t(`watch.type.${w.target_type}`))), t('dashboard.none_watches')), { href: '/profile#wt-h', label: t('dashboard.all_watches') });
  });
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
