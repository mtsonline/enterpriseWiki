// Room of templates of data: how wide blocks are side by side in each view
// and how a row looks on tablet and phone. No DOM, so the unit tests check
// these numbers against the server's (testdata/room.json).
import limitsFile from '../limits.json';
import type { Block, Design, Row, View, ViewName, ViewState } from './engine';

const LIMITS = limitsFile;

/** The width (rem) the positions of a block need side by side (layout row),
 * as the server computes it; one below the other needs no more than one. */
export function positionsRoom(b: Block): number {
  const n = b.positions.length;
  if (b.layout !== 'row' || n < 2) return 0;
  return n * LIMITS.position_min_rem + (n - 1) * LIMITS.position_gap_rem;
}

export function minRem(b: Block, view: ViewName): number {
  return b.kind === 'content' ? LIMITS.views[view].content_min_rem : Math.max((LIMITS.block_min_rem as Record<string, number>)[b.kind], positionsRoom(b));
}

/** The width (rem) a row has in the narrowest window of a view (as the server computes it). */
export function room(d: Design, r: Row, view: ViewName): number {
  let w = LIMITS.views[view].min_px / LIMITS.rem_px;
  if (r.width === 'content' && d.tokens.content_width < w) w = d.tokens.content_width;
  return w - 2 * LIMITS.padding_rem;
}

/** The width (rem) the blocks of a row need side by side in a view (as the server computes it). */
export function needRoom(r: Row, view: ViewName, v?: Partial<View>): number {
  let sum = 0;
  let n = 0;
  for (const b of r.blocks) {
    if (v?.hidden?.includes(b.id) || v?.drawer?.includes(b.id)) continue;
    n++;
    const w = v?.widths && b.id in v.widths ? v.widths[b.id] : b.width;
    sum += b.kind === 'content' ? minRem(b, view) : w > 0 ? w : minRem(b, view);
  }
  return sum + (n > 1 ? LIMITS.gap_rem * (n - 1) : 0);
}

/** The view of a row: the one designed by hand, else the rules:
 * - tablet: side columns beside the content go into a drawer when they do
 *   not fit; blocks go one below the other when they still do not fit;
 * - mobile: menus of the header and side columns beside the content go into
 *   drawers; blocks of the header take the rest; what does not fit goes one
 *   below the other. */
export function viewOf(d: Design, r: Row, view: ViewName): ViewState {
  // behind a button on the desktop: behind one in every view
  const always = r.desktop?.drawer ?? [];
  const union = (list: string[]) => [...new Set([...list, ...always])];
  if (view === 'desktop') return { stack: false, hidden: [], drawer: [...always], widths: {}, auto: false };
  const manual = r[view];
  if (manual) return { stack: manual.stack, hidden: manual.hidden ?? [], drawer: union(manual.drawer ?? []), widths: manual.widths ?? {}, auto: false };
  const hasContent = r.blocks.some((b) => b.kind === 'content');
  const drawer: string[] = [...always];
  const widths: Record<string, number> = {};
  if (view === 'mobile' && r.role === 'banner') {
    for (const b of r.blocks) if (b.role === 'navigation' && !drawer.includes(b.id)) drawer.push(b.id);
  }
  if (hasContent && needRoom(r, view, { drawer, widths }) > room(d, r, view)) {
    for (const b of r.blocks) if ((b.role === 'complementary' || (view === 'mobile' && b.role === 'navigation')) && !drawer.includes(b.id)) drawer.push(b.id);
  }
  const stack = needRoom(r, view, { drawer, widths }) > room(d, r, view);
  return { stack, hidden: [], drawer, widths, auto: true };
}


const HEX = /^#[0-9a-f]{6}$/i;
const ID = /^[a-z][a-z0-9-]{0,39}$/;
const COLORS = ['bg', 'bg_subtle', 'text', 'text_muted', 'border', 'primary', 'on_primary', 'link', 'focus', 'header_bg', 'header_text', 'footer_bg', 'footer_text'];

/** Whether every value of a design that goes into CSS or into a selector has
 * a safe form (colors #rrggbb, ids and positions in lowercase letters,
 * numbers). The server checks much more on saving; this guards the engine
 * itself, for example the preview of a draft that is not saved yet. */
export function cssSafe(d: Design): boolean {
  if (!d || typeof d !== 'object' || !Array.isArray(d.rows) || !d.tokens || typeof d.tokens !== 'object') return false;
  const k = d.tokens as unknown as Record<string, unknown>;
  if (!COLORS.every((c) => typeof k[c] === 'string' && HEX.test(k[c] as string))) return false;
  if (![k.scale, k.radius, k.content_width].every((n) => typeof n === 'number' && Number.isFinite(n))) return false;
  if (typeof k.logo !== 'string' || (k.logo !== '' && !/^\/api\/v1\/files\/[A-Za-z0-9._/-]+$/.test(k.logo))) return false;
  const ok = (s: unknown) => typeof s === 'string' && ID.test(s);
  const num = (n: unknown) => typeof n === 'number' && Number.isFinite(n);
  for (const r of d.rows) {
    if (!r || !ok(r.id) || !Array.isArray(r.blocks)) return false;
    for (const b of r.blocks) {
      if (!b || !ok(b.id) || !num(b.width) || !Array.isArray(b.positions) || !b.positions.every(ok)) return false;
      if (b.after !== undefined && !(Array.isArray(b.after) && b.after.every(ok))) return false;
    }
    for (const v of [r.desktop, r.tablet, r.mobile]) {
      if (!v) continue;
      if (typeof v !== 'object') return false;
      if (v.widths !== undefined && !(typeof v.widths === 'object' && Object.entries(v.widths).every(([id, w]) => ok(id) && num(w)))) return false;
      for (const list of [v.hidden, v.drawer]) if (list !== undefined && !(Array.isArray(list) && list.every(ok))) return false;
    }
  }
  return true;
}
