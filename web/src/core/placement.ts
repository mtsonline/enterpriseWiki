// Resolution of the placement of building blocks (pure: no page, no DOM).

/** Kinds of positions: those of the template (site), those of an element
 * page (element-…), of the editing page (editor-…) and the columns of the
 * own dashboard (dashboard-…); every template has the latter three. A block
 * names the kind it fits. */
export type PositionKind = 'site' | 'element' | 'editor' | 'dashboard';

/** The positions of an element page. */
export const ELEMENT_POSITIONS = ['element-header', 'element-toolbar', 'element-aside', 'element-footer'] as const;

/** The positions of the page to edit or create an element. */
export const EDITOR_POSITIONS = ['editor-header', 'editor-toolbar', 'editor-aside', 'editor-footer', 'editor-statusbar'] as const;

/** The positions of the own dashboard: the full width above, then three columns. */
export const DASHBOARD_POSITIONS = ['dashboard-wide', 'dashboard-1', 'dashboard-2', 'dashboard-3'] as const;

export type PagePosition = (typeof ELEMENT_POSITIONS)[number] | (typeof EDITOR_POSITIONS)[number] | (typeof DASHBOARD_POSITIONS)[number];

/** The kind of a position (by its name). */
export function kindOf(position: string): PositionKind {
  return position.startsWith('element-') ? 'element' : position.startsWith('editor-') ? 'editor' : position.startsWith('dashboard-') ? 'dashboard' : 'site';
}

/** One placement of a building block (see content.Placement on the server). */
export interface Placement { block: string; position: string; sort: number; required?: boolean }
export interface PlacementLevel { level: 'template' | 'site' | 'category' | 'instance' | 'user'; target: string; placements: Placement[] }

/** What the resolution needs to know of a block. */
export interface BlockDefault { id: string; position: string; order: number; kind: PositionKind }

/** Whether a position fits a kind of blocks. */
export function positionFits(kind: PositionKind, position: string): boolean {
  return kindOf(position) === kind;
}

/** Applies levels (in order) to the default positions: a level that names a
 * block replaces its positions from above, unless a level above made it
 * required; an empty position hides it. A position that does not fit the
 * kind of the block is ignored; the level of one page (instance) arranges
 * only element blocks; the own level (user) without personal placement
 * (opts.personal false) only tiles of the dashboard. */
export function resolvePlacements(levels: PlacementLevel[], blocks: BlockDefault[], opts: { personal?: boolean } = {}): Map<string, { position: string; sort: number }[]> {
  const out = new Map<string, { position: string; sort: number }[]>();
  const kind = new Map<string, PositionKind>();
  for (const b of blocks) {
    out.set(b.id, b.position ? [{ position: b.position, sort: b.order }] : []);
    kind.set(b.id, b.kind);
  }
  const locked = new Set<string>();
  for (const l of levels) {
    const byBlock = new Map<string, Placement[]>();
    for (const p of l.placements) byBlock.set(p.block, [...(byBlock.get(p.block) ?? []), p]);
    for (const [block, ps] of byBlock) {
      if (locked.has(block) || !out.has(block)) continue; // unknown blocks (plugin off, instance deleted) rest
      const k = kind.get(block)!;
      if (l.level === 'instance' && k !== 'element') continue;
      // the own level arranges the own dashboard always, the rest only with personal placement
      if (l.level === 'user' && k !== 'dashboard' && opts.personal === false) continue;
      out.set(block, ps.filter((p) => p.position && positionFits(k, p.position)).map((p) => ({ position: p.position, sort: p.sort })));
      if (ps.some((p) => p.required) && (l.level === 'template' || l.level === 'site')) locked.add(block);
    }
  }
  return out;
}
