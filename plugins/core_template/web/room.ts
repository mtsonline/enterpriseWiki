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
  if (view === 'desktop') return { stack: false, hidden: [], drawer: [], widths: {}, auto: false };
  const manual = r[view];
  if (manual) return { stack: manual.stack, hidden: manual.hidden ?? [], drawer: manual.drawer ?? [], widths: manual.widths ?? {}, auto: false };
  const hasContent = r.blocks.some((b) => b.kind === 'content');
  const drawer: string[] = [];
  const widths: Record<string, number> = {};
  if (view === 'mobile' && r.role === 'banner') {
    for (const b of r.blocks) if (b.role === 'navigation') drawer.push(b.id);
  }
  if (hasContent && needRoom(r, view, { drawer, widths }) > room(d, r, view)) {
    for (const b of r.blocks) if (b.role === 'complementary' || (view === 'mobile' && b.role === 'navigation')) drawer.push(b.id);
  }
  const stack = needRoom(r, view, { drawer, widths }) > room(d, r, view);
  return { stack, hidden: [], drawer, widths, auto: true };
}

