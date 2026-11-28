// The settings of a template of data in the browser: the values of
// Administration → Templates → Settings become the tokens of its design
// (the server describes and checks them: ../settings.go). Kept apart from
// the engine so the unit tests can run it without the wiki around it.
import type { Design, Palette } from './engine';

/** The colours of a palette (the keys of Palette, in the order of the form). */
export const PALETTE_KEYS = ['bg', 'bg_subtle', 'text', 'text_muted', 'border', 'primary', 'on_primary', 'link', 'focus', 'header_bg', 'header_text', 'footer_bg', 'footer_text'] as const;

const HEX = /^#[0-9a-fA-F]{6}$/;
const FONT = /^[a-z0-9][a-z0-9-]{0,47}$/;

/** The design with the effective values of its settings as its tokens
 * (Administration → Templates → Settings; the same keys as on the server,
 * settings.go: the colours by their names, dark_… for the dark ones, the
 * fonts, scale, radius and content_width). The server checked the values;
 * a value of another form is left out here too, and designCSS writes only
 * checked forms. Logos stay in the values (see brandOf). */
export function withSettings(d: Design, values: Record<string, unknown> | undefined): Design {
  if (!values) return d;
  const pal = (p: Palette, prefix: string): Palette => {
    const out = { ...p };
    for (const k of PALETTE_KEYS) {
      const v = values[prefix + k];
      if (typeof v === 'string' && HEX.test(v)) out[k] = v;
    }
    return out;
  };
  // a font is the key of a font of the wiki or own-ID (never written raw into CSS)
  const str = (k: string, cur: string) => (typeof values[k] === 'string' && FONT.test(values[k] as string) ? values[k] as string : cur);
  const n = (k: string, cur: number) => (typeof values[k] === 'number' && Number.isFinite(values[k]) ? values[k] as number : cur);
  const k = d.tokens;
  return {
    ...d,
    tokens: {
      ...k, ...pal(k, ''), ...(k.dark ? { dark: pal(k.dark, 'dark_') } : {}),
      font: str('font', k.font), heading_font: str('heading_font', k.heading_font),
      scale: n('scale', k.scale), radius: n('radius', k.radius), content_width: n('content_width', k.content_width),
    },
  };
}
