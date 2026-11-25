// The marks of Enterprise Wiki (web/public/brand, CC0 files; name and marks:
// TRADEMARKS.md): the "e" next to the site name when a template has no own
// logo, "powered by" in the footer, the seal when a valid license is
// installed (license.Status.valid: signature ok, not expired), and the grey
// ("ink") versions for print.
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';

export const BRAND = {
  logo: '/brand/logo.webp',
  logoInk: '/brand/logo-ink.webp',
  mark: '/brand/mark.webp',
  markInk: '/brand/mark-ink.webp',
  seal: '/brand/seal.webp',
} as const;

/** The "e" (decorative: it stands next to the name it belongs to). */
export function brandMark(cls: string, size: number): HTMLImageElement {
  return h('img', { class: cls, src: BRAND.mark, alt: '', width: size, height: size, decoding: 'async' }) as HTMLImageElement;
}

/** Whether a valid license is installed (from /api/v1/site). */
export function licensed(): boolean {
  return state.site?.licensed === true;
}

/** The seal of a licensed installation, or null without a valid license. */
export function licenseSeal(cls: string, size: number): HTMLImageElement | null {
  if (!licensed()) return null;
  const alt = t('brand.seal');
  return h('img', { class: cls, src: BRAND.seal, alt, title: alt, width: size, height: size, decoding: 'async' }) as HTMLImageElement;
}

/** "Powered by Enterprise Wiki" for the footer of a template: the "e", the
 * text and, when licensed, the seal. */
export function poweredBy(cls: string): HTMLElement {
  return h('p', { class: `ew-powered ${cls}` },
    brandMark('ew-powered-mark', 20),
    h('span', null, t('footer.powered', { version: state.site?.version ?? '' })),
    licenseSeal('ew-powered-seal', 56));
}

/** Shown only on paper: the grey "e" and the name of the site (the screen
 * header is not printed). */
export function printBrand(): HTMLElement {
  return h('div', { class: 'ew-print-brand', 'aria-hidden': 'true' },
    h('img', { src: BRAND.markInk, alt: '', width: 32, height: 32 }),
    h('span', null, state.site?.name ?? ''));
}
