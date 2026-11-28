// The marks of Enterprise Wiki (web/public/brand, CC0 files; name and marks:
// TRADEMARKS.md): the "e" next to the site name when a template has no own
// logo, "powered by" in the footer, the seal when a valid license is
// installed (license.Status.valid: signature ok, not expired), and the grey
// ("ink") versions for print.
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { imageSetting, type ImageSetting } from '../core/templateImages';

export const BRAND = {
  logo: '/brand/logo.webp',
  logoInk: '/brand/logo-ink.webp',
  mark: '/brand/mark.webp',
  markInk: '/brand/mark-ink.webp',
  seal: '/brand/seal.webp',
} as const;

/** The default name of a site: the logo already says it. */
const PRODUCT = 'Enterprise Wiki';

/** The logos a template offers in its settings (roles logo, logo_on_dark,
 * mark, mark_on_dark); missing ones fall back to the marks of Enterprise
 * Wiki, the ones "on dark" to their light counterpart. */
export interface TemplateLogos {
  logo?: ImageSetting | null;
  logoOnDark?: ImageSetting | null;
  mark?: ImageSetting | null;
  markOnDark?: ImageSetting | null;
}

/** The logos of the settings of a template, by the keys of the roles. */
export function logosOf(v: Record<string, unknown>, keys = { logo: 'logo', logoOnDark: 'logo_on_dark', mark: 'mark', markOnDark: 'mark_on_dark' }): TemplateLogos {
  return { logo: imageSetting(v[keys.logo]), logoOnDark: imageSetting(v[keys.logoOnDark]), mark: imageSetting(v[keys.mark]), markOnDark: imageSetting(v[keys.markOnDark]) };
}

/** The content of the link to the start page in the header of a template:
 * its logo (from the settings, else the logo of Enterprise Wiki with its
 * text, and the name of the site next to it when it is not "Enterprise
 * Wiki"); on small screens the mark, so name and mark fit in one line. With
 * logos for dark backgrounds, those are shown in the dark colour scheme.
 * height in px; the CSS gives the height and width: auto. Every image has
 * its width and height, so it reserves its room before it loads. */
export function brandHeader(cls: string, height: number, logos: TemplateLogos = {}): HTMLElement[] {
  const name = state.site?.name ?? '';
  const own = name.trim() !== '' && name.trim() !== PRODUCT;
  const logo = logos.logo ?? { src: BRAND.logo, width: Math.round(height * LOGO_RATIO), height };
  // an own logo carries the name; the logo of Enterprise Wiki gets it next to it
  const named = !logos.logo && own;
  const alt = logos.logo ? (name.trim() || PRODUCT) : own ? '' : PRODUCT;
  // the mark on narrow screens: an own one, or the "e" while the name stands next to it
  const mark = logos.mark ?? (named ? { src: BRAND.mark, width: height, height } : null);
  const picture = (l: ImageSetting, m: ImageSetting | null, extra: string) => {
    const img = h('img', { class: cls, src: l.src, alt, width: l.width, height: l.height, decoding: 'async' });
    return m ? h('picture', { class: [`${cls}-picture`, extra] }, h('source', { media: '(max-width: 600px)', srcset: m.src, width: m.width, height: m.height }), img)
      : extra ? h('span', { class: [`${cls}-picture`, extra] }, img) : img;
  };
  const dark = logos.logoOnDark || logos.markOnDark;
  const out = dark
    ? [picture(logo, mark, 'ew-on-light'), picture(logos.logoOnDark ?? logo, logos.markOnDark ?? mark, 'ew-on-dark')]
    : [picture(logo, mark, '')];
  if (named) out.push(h('span', { class: `${cls}-name` }, name));
  return out;
}

/** width : height of logo.webp and logo-ink.webp */
const LOGO_RATIO = 315 / 128;

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

/** Shown only on paper: the grey logo and the name of the site (the screen
 * header is not printed). */
export function printBrand(): HTMLElement {
  const name = state.site?.name ?? '';
  return h('div', { class: 'ew-print-brand', 'aria-hidden': 'true' },
    h('img', { src: BRAND.logoInk, alt: '', width: Math.round(32 * LOGO_RATIO), height: 32 }),
    name.trim() && name.trim() !== PRODUCT ? h('span', null, name) : null);
}
