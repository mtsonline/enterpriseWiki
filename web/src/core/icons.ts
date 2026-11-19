// Icons of the shipped set (one file per icon, built into assets/icons/<version>/):
// the server writes only their names (data-icon); here they get their file,
// drawn in the color of the text around them (a mask).
import { ICON_BASE } from '../generated/icons';

const NAME = /^[a-z0-9][a-z0-9-]{0,63}$/;
export const ICON_STYLES = ['outline', 'filled'] as const;

/** The address of an icon (only names of a valid form). */
export function iconURL(name: string, style = 'outline'): string {
  return NAME.test(name) && (ICON_STYLES as readonly string[]).includes(style) ? `${ICON_BASE}/${style}/${name}.svg` : '';
}

/** Gives the icons in root their files. */
export function hydrateIcons(root: ParentNode): void {
  root.querySelectorAll<HTMLElement>('[data-icon]').forEach((el) => {
    const url = iconURL(el.dataset.icon ?? '', el.dataset.iconStyle || 'outline');
    if (url) el.style.setProperty('--ew-icon', `url("${url}")`);
  });
}

export interface IconEntry { n: string; c: string; t: string[]; s: string }
let index: Promise<IconEntry[]> | null = null;

/** The index of all icons (names, categories, search words); loaded once, when the picker opens. */
export function iconIndex(): Promise<IconEntry[]> {
  index ??= fetch(`${ICON_BASE}/index.json`).then((r) => (r.ok ? r.json() as Promise<IconEntry[]> : [])).catch(() => { index = null; return []; });
  return index;
}

/** The icons that fit a search (name, search words, category), at most max. */
export function findIcons(all: IconEntry[], query: string, style: string, category: string, max: number): { list: IconEntry[]; total: number } {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const want = style === 'filled' ? 'f' : 'o';
  const hits = all.filter((e) => e.s.includes(want) && (!category || e.c === category)
    && words.every((w) => e.n.includes(w) || e.t.some((x) => x.includes(w)) || e.c.toLowerCase().includes(w)));
  return { list: hits.slice(0, max), total: hits.length };
}
