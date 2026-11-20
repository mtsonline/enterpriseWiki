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

/** Search words and category names of one language (src/icons/terms/<lang>.json). */
export interface IconTerms { categories: Record<string, string>; words: Record<string, string[]> }
const termsOf = new Map<string, Promise<IconTerms | null>>();

/** The terms of a language (null for English, the language of the set, or if there are none). */
export function iconTerms(lang: string): Promise<IconTerms | null> {
  if (lang === 'en' || !/^[a-z]{2,3}$/.test(lang)) return Promise.resolve(null);
  let p = termsOf.get(lang);
  if (!p) {
    p = fetch(`${ICON_BASE}/terms.${lang}.json`).then((r) => (r.ok ? r.json() as Promise<IconTerms> : null)).catch(() => { termsOf.delete(lang); return null; });
    termsOf.set(lang, p);
  }
  return p;
}

/** Lower case without accents and umlauts, so "uber" finds "Über". */
const fold = (s: string) => s.toLowerCase().replace(/ß/g, 'ss').normalize('NFD').replace(/[\u0300-\u036f]/g, '');

/** The English words of an icon: the parts of its name and of its search words. */
const tokensOf = new WeakMap<IconEntry, Set<string>>();
function tokens(e: IconEntry): Set<string> {
  let s = tokensOf.get(e);
  if (!s) {
    s = new Set([...e.n.split('-'), ...e.t, ...e.t.flatMap((x) => x.split(/[-\s]/))]);
    tokensOf.set(e, s);
  }
  return s;
}

/** The name of an icon in the language of the terms ("Pfeil links" for arrow-left), or "" without terms. */
export function iconLabel(e: IconEntry, terms: IconTerms | null): string {
  if (!terms) return '';
  const parts = e.n.split('-').map((w) => terms.words[w]?.[0] ?? w);
  const s = parts.join(' ');
  return s === e.n.replace(/-/g, ' ') ? '' : s;
}

/**
 * The icons that fit a search (name, search words, category), at most max.
 * With terms, words of that language find the icons too: "Pfeil" finds
 * every arrow, "Uhr" every clock.
 */
export function findIcons(all: IconEntry[], query: string, style: string, category: string, max: number, terms: IconTerms | null = null): { list: IconEntry[]; total: number } {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const want = style === 'filled' ? 'f' : 'o';
  // per word of the search: the English words whose translation contains it
  const english = words.map((w) => {
    const f = fold(w);
    const hit = new Set<string>();
    if (terms) for (const [en, tr] of Object.entries(terms.words)) if (tr.some((x) => fold(x).includes(f))) hit.add(en);
    return { w, f, hit };
  });
  const catName = (c: string) => fold(terms?.categories[c] ?? '');
  const hits = all.filter((e) => e.s.includes(want) && (!category || e.c === category)
    && english.every(({ w, f, hit }) => e.n.includes(w) || e.t.some((x) => x.includes(w)) || e.c.toLowerCase().includes(w)
      || (hit.size > 0 && [...tokens(e)].some((x) => hit.has(x))) || (!!terms && catName(e.c).includes(f))));
  return { list: hits.slice(0, max), total: hits.length };
}
