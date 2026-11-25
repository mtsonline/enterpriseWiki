// Contrast of templates of data, as the server computes it (WCAG 2.2): the
// pairs of tokens come from limits.json. The designer shows every pair and,
// for a pair that fails, the nearest color that passes (contrast help).
// No DOM, so the unit tests check it against the server (testdata/contrast.json).
import limitsFile from '../limits.json';

export interface Pair { fore: string; back: string; ui: boolean; rule: string }

export const PAIRS: Pair[] = limitsFile.contrast_pairs;

/** The contrast a pair needs. */
export function minOf(p: Pair): number {
  return p.ui ? limitsFile.contrast_ui : limitsFile.contrast_text;
}

function channels(hex: string): [number, number, number] | null {
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex);
  return m ? [parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)] : null;
}

function luminance(hex: string): number {
  const c = channels(hex);
  if (!c) return 0;
  const ch = (v: number) => {
    const x = v / 255;
    return x <= 0.04045 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * ch(c[0]) + 0.7152 * ch(c[1]) + 0.0722 * ch(c[2]);
}

/** The contrast ratio of two colors #rrggbb. */
export function contrast(a: string, b: string): number {
  let la = luminance(a);
  let lb = luminance(b);
  if (la < lb) [la, lb] = [lb, la];
  return (la + 0.05) / (lb + 0.05);
}

const hex = (c: number[]) => `#${c.map((v) => Math.round(Math.max(0, Math.min(255, v))).toString(16).padStart(2, '0')).join('')}`;

/** The nearest color to fore (same hue, mixed toward black or white) that has
 * at least min contrast on back; null when even black and white do not. */
export function suggest(fore: string, back: string, min: number): string | null {
  const f = channels(fore);
  if (!f || !channels(back)) return null;
  if (contrast(fore, back) >= min) return fore.toLowerCase();
  let best: { c: string; step: number } | null = null;
  for (const target of [0, 255]) {
    for (let step = 1; step <= 100; step++) {
      const k = step / 100;
      const c = hex(f.map((v) => v + (target - v) * k));
      if (contrast(c, back) >= min) {
        if (!best || step < best.step) best = { c, step };
        break;
      }
    }
  }
  return best?.c ?? null;
}

/** All pairs of the tokens with their contrast and, when too low, a suggestion. */
export function checkPairs(tokens: Record<string, unknown>): { pair: Pair; value: number; min: number; ok: boolean; suggestion: string | null }[] {
  return PAIRS.map((pair) => {
    const fore = String(tokens[pair.fore] ?? '');
    const back = String(tokens[pair.back] ?? '');
    const value = contrast(fore, back);
    const min = minOf(pair);
    const ok = value >= min;
    return { pair, value, min, ok, suggestion: ok ? null : suggest(fore, back, min) };
  });
}

/** The colors of one scheme, as in the tokens (engine.ts Palette). */
type Colors = Record<'bg' | 'bg_subtle' | 'text' | 'text_muted' | 'border' | 'primary' | 'on_primary' | 'link' | 'focus' | 'header_bg' | 'header_text' | 'footer_bg' | 'footer_text', string>;

/** A first dark palette for a design that has light colors only: the dark
 * colors of the wiki for surfaces and text, the colors of the design for
 * primary, link and focus, lightened just as far as the contrast needs.
 * Every pair passes (unit test); the designer can change everything. */
export function darkFrom(light: Colors): Colors {
  const bg = '#1d2125';
  const subtle = '#22272b';
  const on = (c: string, min: number) => {
    let x = suggest(c, bg, min) ?? '#ffffff';
    if (contrast(x, subtle) < min) x = suggest(x, subtle, min) ?? '#ffffff';
    return x;
  };
  const primary = on(light.primary, 4.5);
  return {
    bg, bg_subtle: subtle, text: '#dee4ea', text_muted: '#b6c2cf', border: '#8c9bab',
    primary, on_primary: contrast(primary, '#10151a') >= 4.5 ? '#10151a' : '#000000',
    link: on(light.link, 4.5), focus: on(light.focus, 3),
    header_bg: '#161a1d', header_text: '#dee4ea', footer_bg: '#161a1d', footer_text: '#dee4ea',
  };
}
