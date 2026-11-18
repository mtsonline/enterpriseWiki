// Resolution of the placement of building blocks (pure: no page, no DOM).

/** Kinds of positions: those of the template (site), those of an element
 * page (element-…) and those of the editing page (editor-…); every
 * template has the latter two. A block names the kind it fits. */
export type PositionKind = 'site' | 'element' | 'editor';

/** The positions of an element page. */
export const ELEMENT_POSITIONS = ['element-header', 'element-toolbar', 'element-aside', 'element-footer'] as const;

/** The positions of the page to edit or create an element. */
export const EDITOR_POSITIONS = ['editor-header', 'editor-toolbar', 'editor-aside', 'editor-footer', 'editor-statusbar'] as const;

export type PagePosition = (typeof ELEMENT_POSITIONS)[number] | (typeof EDITOR_POSITIONS)[number];

/** The kind of a position (by its name). */
export function kindOf(position: string): PositionKind {
  return position.startsWith('element-') ? 'element' : position.startsWith('editor-') ? 'editor' : 'site';
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
 * only element blocks. */
export function resolvePlacements(levels: PlacementLevel[], blocks: BlockDefault[]): Map<string, { position: string; sort: number }[]> {
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
      out.set(block, ps.filter((p) => p.position && positionFits(k, p.position)).map((p) => ({ position: p.position, sort: p.sort })));
      if (ps.some((p) => p.required) && (l.level === 'template' || l.level === 'site')) locked.add(block);
    }
  }
  return out;
}
