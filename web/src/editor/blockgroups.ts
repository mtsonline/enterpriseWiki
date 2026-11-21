// Groups of blocks in the menu of the editor and the search in it (no DOM:
// also used by the unit tests).
import type { MacroInfo } from '../core/types';

/** The groups in the order of the menu (content.MacroGroups), then the templates. */
export const BLOCK_GROUPS = ['text', 'layout', 'navigation', 'media', 'lists', 'data', 'forms', 'automation', 'other'];

/** Folds text for searching: lower case, without accents. */
export const fold = (s: string): string => s.normalize('NFD').replace(/[\u0300-\u036f]/g, '').toLowerCase();

/** Whether a block matches every word of the search. */
export function matches(search: string, query: string): boolean {
  const words = fold(query).split(/\s+/).filter(Boolean);
  return words.every((w) => search.includes(w));
}

/** Groups the macros for the menu: known groups in their order, empty ones left out. */
export function groupMacros(ms: MacroInfo[], name: (m: MacroInfo) => string = (m) => m.name): { group: string; macros: MacroInfo[] }[] {
  const by = new Map<string, MacroInfo[]>();
  for (const m of ms) {
    const g = BLOCK_GROUPS.includes(m.group ?? '') ? m.group! : 'other';
    by.set(g, [...(by.get(g) ?? []), m]);
  }
  return BLOCK_GROUPS.filter((g) => by.has(g)).map((g) => ({ group: g, macros: by.get(g)!.sort((a, b) => name(a).localeCompare(name(b))) }));
}
