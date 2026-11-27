// Options of placed blocks: the same schema the server checks with
// (internal/content/blockoptions.json). Here: the rules (to show problems
// while choosing, the server checks again), whether a block is shown now,
// and its CSS. The CSS is built only from values of a valid form, so options
// that are not checked yet (a draft in the form) cannot inject anything.
import schemaFile from '../../../internal/content/blockoptions.json';
import { mediaMatches, type WindowSize } from './templateSchema';

export type ViewName = 'desktop' | 'tablet' | 'mobile';
export type OptionValue = string | number | boolean | string[] | Partial<Record<ViewName, number | string>>;
export type BlockOptions = Record<string, OptionValue>;

export interface OptionDef {
  id: string; type: 'enum' | 'bool' | 'int' | 'number' | 'space' | 'color' | 'file' | 'anchor' | 'label' | 'datetime' | 'views' | 'langs';
  values?: string[]; min?: number; max?: number; step?: number; unit?: string; responsive?: boolean;
}
export interface OptionGroup { id: string; options: OptionDef[] }

export const SCHEMA = schemaFile as unknown as {
  version: number; max_bytes: number; views: ViewName[]; langs_max: number; space_rem: number[]; contrast_text: number;
  column_min_rem: number; view_min_px: Record<ViewName, number>; rem_px: number; groups: OptionGroup[];
};
const DEFS = new Map<string, OptionDef>(SCHEMA.groups.flatMap((g) => g.options.map((o) => [o.id, o] as const)));
export const optionDef = (id: string): OptionDef | undefined => DEFS.get(id);

const COLOR = /^#[0-9a-f]{6}$/;
const ANCHOR = /^[a-z][a-z0-9-]{0,39}$/;
const FILE = /^\/api\/v1\/(files\/[A-Za-z0-9._-]{1,128}|media\/[0-9]{1,18}\/view)$/;
/** ids of the interface itself (the skip link jumps to #main …) */
const RESERVED_ANCHORS = ['main', 'top', 'content', 'app', 'search', 'nav', 'header', 'footer'];

/** The rule for anchors (CheckAnchor on the server): not an id of the interface
 * (reserved names, the prefix "ew", name-number or f<number>- like generated ids). */
export const anchorOK = (v: string): boolean => ANCHOR.test(v) && !RESERVED_ANCHORS.includes(v) && !v.startsWith('ew') && !/-[0-9]+$|^f[0-9]+-/.test(v);
const LANG = /^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$/;

/** Whether one value fits its option (type and range), as the server checks it. */
export function valueOK(def: OptionDef, v: unknown): boolean {
  switch (def.type) {
    case 'enum': return typeof v === 'string' && !!def.values?.includes(v);
    case 'bool': return typeof v === 'boolean';
    case 'int': case 'number': case 'space': {
      if (typeof v !== 'number' || !Number.isFinite(v)) return false;
      const [lo, hi] = def.type === 'space' ? [0, SCHEMA.space_rem.length - 1] : [def.min ?? 0, def.max ?? 0];
      if (def.type !== 'number' && !Number.isInteger(v)) return false;
      return v >= lo && v <= hi;
    }
    case 'color': return typeof v === 'string' && COLOR.test(v);
    case 'file': return typeof v === 'string' && FILE.test(v);
    case 'anchor': return typeof v === 'string' && anchorOK(v);
    case 'label': return typeof v === 'string' && v.trim() !== '' && [...v].length <= 100 && !/[\u0000-\u001f\u007f-\u009f]/.test(v);
    case 'datetime': return typeof v === 'string' && /^\d{4}-\d\d-\d\dT\d\d:\d\d(:\d\d(\.\d+)?)?(Z|[+-]\d\d:\d\d)$/.test(v) && !Number.isNaN(Date.parse(v));
    case 'views': return Array.isArray(v) && v.length <= SCHEMA.views.length && new Set(v).size === v.length && v.every((x) => SCHEMA.views.includes(x as ViewName));
    case 'langs': return Array.isArray(v) && v.length <= SCHEMA.langs_max && new Set(v).size === v.length && v.every((x) => typeof x === 'string' && LANG.test(x));
    default: return false;
  }
}

/** Whether all options have a valid form (unknown options are not). */
export function formOK(o: BlockOptions): boolean {
  for (const [k, v] of Object.entries(o)) {
    const def = DEFS.get(k);
    if (!def) return false;
    if (def.responsive && v && typeof v === 'object' && !Array.isArray(v)) {
      const e = Object.entries(v);
      if (!e.length || !e.every(([view, x]) => SCHEMA.views.includes(view as ViewName) && valueOK(def, x))) return false;
    } else if (!valueOK(def, v)) return false;
  }
  return true;
}

// ------------------------------------------------------------ rules (as the server)

const rgb = (hx: string) => [1, 3, 5].map((i) => parseInt(hx.slice(i, i + 2), 16));
export function mix(a: string, b: string, t: number): string {
  const x = rgb(a);
  const y = rgb(b);
  return `#${x.map((v, i) => Math.round(v * (1 - t) + y[i] * t).toString(16).padStart(2, '0')).join('')}`;
}
function lum(hx: string): number {
  const ch = (v: number) => { const c = v / 255; return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4; };
  const [r, g, b] = rgb(hx);
  return 0.2126 * ch(r) + 0.7152 * ch(g) + 0.0722 * ch(b);
}
export function contrast(a: string, b: string): number {
  let x = lum(a);
  let y = lum(b);
  if (x < y) [x, y] = [y, x];
  return (x + 0.05) / (y + 0.05);
}

/** The colour itself, or mixed towards white (on a dark background) or black
 * (on a light one) just enough to reach the contrast min with bg: for a colour
 * someone chose for the light scheme that is shown in the dark one. Both as
 * #rrggbb. */
export function readableOn(color: string, bg: string, min = 4.5): string {
  if (contrast(color, bg) >= min) return color;
  const to = lum(bg) < 0.18 ? '#ffffff' : '#000000';
  for (let i = 1; i <= 20; i++) {
    const c = mix(color, to, i / 20);
    if (contrast(c, bg) >= min) return c;
  }
  return to;
}

/** The most columns of text that have room side by side in a view. */
export function maxColumns(view: ViewName): number {
  return Math.max(1, Math.floor((SCHEMA.view_min_px[view] / SCHEMA.rem_px - 2) / SCHEMA.column_min_rem));
}

/** The colors the text of a block can stand on, each under its overlay: the
 * solid color, the colors of the gradient (ends and between), and for a
 * picture (unknown colors) the overlay over black and over white. Empty when
 * the block keeps the background of the page. */
export function surfaces(o: BlockOptions): string[] {
  const s = (k: string) => (typeof o[k] === 'string' ? o[k] as string : '');
  const gradient = s('bg_gradient') === 'none' ? '' : s('bg_gradient');
  const overlay = COLOR.test(s('overlay_color')) ? s('overlay_color') : '';
  const alpha = overlay ? (typeof o.overlay_opacity === 'number' ? o.overlay_opacity : 0) / 100 : 0;
  let backs: string[] = [];
  if (s('bg_image') || s('bg_video')) backs = overlay && alpha > 0 ? ['#000000', '#ffffff'] : [];
  else if (gradient && COLOR.test(s('bg_gradient_from')) && COLOR.test(s('bg_gradient_to'))) backs = [0, 1, 2, 3, 4].map((i) => mix(s('bg_gradient_from'), s('bg_gradient_to'), i / 4));
  else if (COLOR.test(s('bg_color'))) backs = [s('bg_color')];
  return backs.map((b) => (alpha > 0 ? mix(b, overlay, alpha) : b));
}

/** A problem with the options, as a key of the interface texts and its values. */
export interface Problem { key: string; values?: Record<string, string | number> }

/** The rules every block keeps (the same as the server): own background
 * needs own text color with enough contrast, text on a picture needs an
 * overlay, columns need room. */
export function problems(o: BlockOptions): Problem[] {
  const out: Problem[] = [];
  if (!formOK(o)) out.push({ key: 'bo.p.form' });
  const s = (k: string) => (typeof o[k] === 'string' ? o[k] as string : '');
  const text = s('text_color');
  const bg = s('bg_color');
  const gradient = s('bg_gradient') === 'none' ? '' : s('bg_gradient');
  const image = s('bg_image') || s('bg_video'); // a video has unknown colors like a picture
  const pattern = s('texture') === 'none' ? '' : s('texture');
  const min = SCHEMA.contrast_text;
  if ((bg || gradient || image || pattern) && !text) out.push({ key: 'bo.p.text_color' });
  if (gradient && (!s('bg_gradient_from') || !s('bg_gradient_to'))) out.push({ key: 'bo.p.gradient' });
  const overlay = s('overlay_color');
  const alpha = overlay ? (typeof o.overlay_opacity === 'number' ? o.overlay_opacity : 0) / 100 : 0;
  let backs: string[] = [];
  if (image) {
    if (!overlay || alpha === 0) out.push({ key: 'bo.p.overlay' });
    else backs = ['#000000', '#ffffff'];
  } else if (gradient && s('bg_gradient_from') && s('bg_gradient_to')) {
    backs = [0, 1, 2, 3, 4].map((i) => mix(s('bg_gradient_from'), s('bg_gradient_to'), i / 4));
  } else if (bg) backs = [bg];
  if (text && !backs.length && o.gradient_text === true) out.push({ key: 'bo.p.gradient_text_bg' });
  if (alpha > 0 && COLOR.test(overlay)) backs = backs.map((b) => mix(b, overlay, alpha));
  // a picture or a gradient has every brightness between its ends: the text
  // must be brighter or darker than all of them, not in between
  if (COLOR.test(text) && backs.length > 1) {
    const l = lum(text);
    const ls = backs.map(lum);
    if (l >= Math.min(...ls) && l <= Math.max(...ls)) out.push({ key: 'bo.p.between', values: { color: text, from: backs[0], to: backs[backs.length - 1] } });
  }
  if (COLOR.test(text)) {
    for (const b of backs) {
      const c = contrast(text, b);
      if (c < min) { out.push({ key: 'bo.p.contrast', values: { color: text, on: b, value: c.toFixed(2), min } }); break; }
    }
    if (o.gradient_text === true) {
      for (const g of [s('bg_gradient_from'), s('bg_gradient_to')]) {
        if (!g) { out.push({ key: 'bo.p.gradient' }); break; }
        for (const b of backs) {
          const c = contrast(g, b);
          if (c < min) { out.push({ key: 'bo.p.contrast', values: { color: g, on: b, value: c.toFixed(2), min } }); break; }
        }
      }
    }
  }
  const cols = o.columns;
  if (cols !== undefined) {
    // one value counts for all views; per view the larger view passes it on
    let last = 0;
    for (const view of SCHEMA.views) {
      const own = typeof cols === 'object' && !Array.isArray(cols) ? cols[view] : undefined;
      const v = typeof cols === 'number' ? cols : typeof own === 'number' ? own : last;
      last = v;
      if (v > maxColumns(view)) { out.push({ key: 'bo.p.columns', values: { view, max: maxColumns(view) } }); break; }
    }
  }
  if (s('from') && s('until') && Date.parse(s('until')) <= Date.parse(s('from'))) out.push({ key: 'bo.p.until' });
  if (typeof o.scroll_height === 'number' && o.scroll_height > 0 && !s('label').trim()) out.push({ key: 'bo.p.scroll_label' });
  if (s('border_sides') && s('border_sides') !== 'all' && (!s('border_style') || s('border_style') === 'none')) out.push({ key: 'bo.p.border_sides' });
  if (s('radius_corners') && s('radius_corners') !== 'all' && !(typeof o.radius === 'number' && o.radius > 0)) out.push({ key: 'bo.p.radius_corners' });
  return out;
}

// ------------------------------------------------------------ shown now?

/** Whether a block with these options is shown now (a rule of display only:
 * it never gives or takes permissions, the block loads its data as usual). */
export function shownNow(o: BlockOptions | undefined, now: number, signedIn: boolean, lang: string): boolean {
  if (!o) return true;
  if (o.audience === 'signed_in' && !signedIn) return false;
  if (o.audience === 'signed_out' && signedIn) return false;
  if (typeof o.from === 'string' && now < Date.parse(o.from)) return false;
  if (typeof o.until === 'string' && now >= Date.parse(o.until)) return false;
  if (Array.isArray(o.langs) && o.langs.length && !o.langs.includes(lang)) return false;
  return true;
}

// ------------------------------------------------------------ CSS

const MEDIA: Record<ViewName, string> = {
  desktop: `(min-width: ${SCHEMA.view_min_px.desktop}px)`,
  tablet: `(min-width: ${SCHEMA.view_min_px.tablet}px) and (max-width: ${SCHEMA.view_min_px.desktop - 0.02}px)`,
  mobile: `(max-width: ${SCHEMA.view_min_px.tablet - 0.02}px)`,
};
/** Whether the option "hide on" hides a block in a window of this size (the
 * schematic of the placements marks such blocks per device). */
export function hiddenAt(o: BlockOptions | undefined, size: WindowSize): boolean {
  return Array.isArray(o?.hide) && (o.hide as ViewName[]).some((v) => MEDIA[v] !== undefined && mediaMatches(MEDIA[v], size));
}

// cascading: tablet applies to the phone too unless the phone has its own
const BELOW: Record<'tablet' | 'mobile', string> = {
  tablet: `(max-width: ${SCHEMA.view_min_px.desktop - 0.02}px)`,
  mobile: `(max-width: ${SCHEMA.view_min_px.tablet - 0.02}px)`,
};
const FONTS: Record<string, string> = {
  system: 'system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
  inter: '"Inter Variable", system-ui, sans-serif',
  'source-sans': '"Source Sans 3 Variable", system-ui, sans-serif',
  serif: 'Georgia, "Times New Roman", serif',
  mono: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
};
const SHADOWS: Record<string, string> = {
  sm: '0 1px 2px rgb(0 0 0 / .12)', md: '0 4px 12px rgb(0 0 0 / .14)', lg: '0 10px 28px rgb(0 0 0 / .16)', xl: '0 20px 48px rgb(0 0 0 / .2)', inset: 'inset 0 2px 8px rgb(0 0 0 / .18)',
};
const TEXT_SHADOWS: Record<string, string> = { soft: '0 1px 2px rgb(0 0 0 / .35)', strong: '0 2px 6px rgb(0 0 0 / .6)' };
const ASPECTS: Record<string, string> = { '16-9': '16 / 9', '4-3': '4 / 3', '1-1': '1 / 1', '3-2': '3 / 2', '21-9': '21 / 9' };
// the dividers as masks (fixed shapes; their color is a background color)
const DIVIDERS: Record<string, string> = {
  wave: 'M0 60 C 240 0 480 120 720 60 S 1200 0 1440 60 V120 H0 Z',
  zigzag: 'M0 120 L120 40 L240 120 L360 40 L480 120 L600 40 L720 120 L840 40 L960 120 L1080 40 L1200 120 L1320 40 L1440 120 Z',
  slope: 'M0 120 L1440 0 V120 Z',
  arc: 'M0 120 Q720 -60 1440 120 Z',
};
const PATTERNS: Record<string, (c: string) => string> = {
  dots: (c) => `radial-gradient(${c} 1px, transparent 1.5px) 0 0 / 16px 16px`,
  lines: (c) => `repeating-linear-gradient(0deg, ${c} 0 1px, transparent 1px 16px)`,
  grid: (c) => `linear-gradient(${c} 1px, transparent 1px) 0 0 / 24px 24px, linear-gradient(90deg, ${c} 1px, transparent 1px) 0 0 / 24px 24px`,
  diagonal: (c) => `repeating-linear-gradient(45deg, ${c} 0 1px, transparent 1px 12px)`,
};

// sides and corners in logical directions (right to left turns them)
const BORDER_SIDES: Record<string, string[]> = {
  top: ['block-start'], bottom: ['block-end'], top_bottom: ['block-start', 'block-end'],
  start: ['inline-start'], end: ['inline-end'], start_end: ['inline-start', 'inline-end'],
};
const RADIUS_CORNERS: Record<string, string[]> = {
  top: ['start-start', 'start-end'], bottom: ['end-start', 'end-end'], start: ['start-start', 'end-start'], end: ['start-end', 'end-end'],
};

const space = (n: number) => `${SCHEMA.space_rem[n] ?? 0}rem`;

const APPEAR = ['fade', 'rise', 'slide_start', 'slide_end', 'zoom'];

/** The value of an option in each view (desktop is the base). */
function perView(v: OptionValue | undefined): Partial<Record<ViewName, number | string>> {
  if (v === undefined) return {};
  if (typeof v === 'object' && !Array.isArray(v)) return v;
  return { desktop: v as number | string };
}

/** The CSS of a placed block with these options, for the selector sel. Empty
 * when the options do not all have a valid form. */
export function optionsCSS(sel: string, o: BlockOptions): string {
  if (!formOK(o)) return '';
  const base: string[] = [];
  const at: Record<'tablet' | 'mobile', string[]> = { tablet: [], mobile: [] };
  const extra: string[] = [];
  const responsive = (id: string, css: (v: number | string) => string) => {
    const pv = perView(o[id]);
    if (pv.desktop !== undefined) base.push(css(pv.desktop));
    if (pv.tablet !== undefined) at.tablet.push(css(pv.tablet));
    if (pv.mobile !== undefined) at.mobile.push(css(pv.mobile));
  };
  const s = (k: string) => (typeof o[k] === 'string' ? o[k] as string : '');
  const n = (k: string) => (typeof o[k] === 'number' ? o[k] as number : undefined);
  // general
  if (Array.isArray(o.hide)) for (const v of o.hide as ViewName[]) extra.push(`@media ${MEDIA[v]} { ${sel} { display: none !important; } }`);
  if (o.print === 'hide') extra.push(`@media print { ${sel} { display: none !important; } }`);
  if (o.break_before === true) extra.push(`@media print { ${sel} { break-before: page; } }`);
  if (o.break_inside_avoid === true) extra.push(`@media print { ${sel} { break-inside: avoid; } }`);
  responsive('order', (v) => `order: ${Number(v)};`);
  // size
  responsive('max_width', (v) => (Number(v) > 0 ? `max-width: ${Number(v)}rem; margin-inline: auto;` : ''));
  responsive('min_height', (v) => `min-height: ${Number(v)}rem;`);
  // scrolls in itself: the body is the scrolling region (it gets a name and
  // the keyboard focus in optionframes.ts)
  const scroll = n('scroll_height');
  if (scroll) extra.push(`${sel} > .ew-bo-body { max-block-size: ${scroll}rem; overflow: auto; overscroll-behavior: contain; }`);
  // sticky in its area: only as the last part of its area (nothing of the
  // area then scrolls under it and is hidden, WCAG 2.4.11), only on the
  // desktop and only while it fits on the screen (optionframes.ts)
  if (o.sticky === true) extra.push(`@media (min-width: ${SCHEMA.view_min_px.desktop}px) { ${sel}.ew-bo--fits:last-child { position: sticky; top: calc(var(--ew-sticky-top, var(--ew-header-h)) + 1rem); align-self: start; z-index: 1; } }`);
  if (ASPECTS[s('aspect')]) base.push(`aspect-ratio: ${ASPECTS[s('aspect')]};`);
  const dividerH = n('divider_height') ?? 48;
  const padAdd = (side: 'top' | 'bottom') => ((side === 'top' ? s('divider_top') : s('divider_bottom')) in DIVIDERS ? ` + ${dividerH}px` : '');
  for (const side of ['top', 'right', 'bottom', 'left'] as const) {
    const add = side === 'top' || side === 'bottom' ? padAdd(side) : '';
    const pv = perView(o[`padding_${side}`]);
    if (pv.desktop === undefined && add) base.push(`padding-${side}: calc(0rem${add});`);
    responsive(`padding_${side}`, (v) => `padding-${side}: ${add ? `calc(${space(Number(v))}${add})` : space(Number(v))};`);
  }
  for (const side of ['top', 'bottom'] as const) responsive(`margin_${side}`, (v) => `margin-${side}: ${space(Number(v))};`);
  responsive('text_align', (v) => `text-align: ${v};`);
  // background: pattern, overlay, gradient, picture (from top to bottom)
  const layers: string[] = [];
  const pattern = s('texture');
  if (PATTERNS[pattern]) layers.push(PATTERNS[pattern](s('texture_color') || 'rgb(0 0 0 / .12)'));
  const alpha = (n('overlay_opacity') ?? 0) / 100;
  if (s('overlay_color') && alpha > 0) {
    const c = s('overlay_color');
    const a = Math.round(alpha * 100);
    layers.push(`linear-gradient(color-mix(in srgb, ${c} ${a}%, transparent), color-mix(in srgb, ${c} ${a}%, transparent))`);
  }
  const g = s('bg_gradient');
  if ((g === 'linear' || g === 'radial' || g === 'conic') && s('bg_gradient_from') && s('bg_gradient_to')) {
    const angle = n('bg_gradient_angle') ?? 180;
    const colors = `${s('bg_gradient_from')}, ${s('bg_gradient_to')}`;
    layers.push(g === 'linear' ? `linear-gradient(${angle}deg, ${colors})` : g === 'radial' ? `radial-gradient(circle, ${colors})` : `conic-gradient(from ${angle}deg, ${colors})`);
  }
  if (s('bg_image')) {
    const pos = `${n('bg_focus_x') ?? 50}% ${n('bg_focus_y') ?? 50}%`;
    const size = s('bg_size') || 'cover';
    layers.push(`url("${s('bg_image')}") ${pos} / ${size} ${s('bg_repeat') || 'no-repeat'}`);
    if (o.bg_fixed === true) extra.push(`@media (prefers-reduced-motion: no-preference) and (min-width: ${SCHEMA.view_min_px.desktop}px) { ${sel} { background-attachment: fixed; } }`);
  }
  if (layers.length) base.push(`background: ${layers.join(', ')}${s('bg_color') ? ` ${s('bg_color')}` : ''};`);
  else if (s('bg_color')) base.push(`background: ${s('bg_color')};`);
  if (s('blend') && s('blend') !== 'normal') base.push(`background-blend-mode: ${s('blend')};`);
  if (o.glass === true) extra.push(`@supports (backdrop-filter: blur(1px)) { ${sel} { backdrop-filter: blur(12px); } }`);
  // border, shape, shadow
  if (s('border_style') && s('border_style') !== 'none') {
    const b = `${n('border_width') ?? 1}px ${s('border_style')} ${s('border_color') || 'currentColor'}`;
    const sides = BORDER_SIDES[s('border_sides')];
    base.push(sides ? sides.map((p) => `border-${p}: ${b};`).join(' ') : `border: ${b};`);
  }
  if (n('radius') !== undefined) {
    const corners = RADIUS_CORNERS[s('radius_corners')];
    base.push(corners ? corners.map((c) => `border-${c}-radius: ${n('radius')}px;`).join(' ') : `border-radius: ${n('radius')}px;`);
  }
  const shadows = [SHADOWS[s('shadow')], s('glow') ? `0 0 18px ${s('glow')}` : ''].filter(Boolean);
  if (shadows.length) base.push(`box-shadow: ${shadows.join(', ')};`);
  const fades: Record<string, string> = {
    top: 'linear-gradient(to bottom, transparent, #000 2rem)', bottom: 'linear-gradient(to top, transparent, #000 2rem)',
    both: 'linear-gradient(to bottom, transparent, #000 2rem, #000 calc(100% - 2rem), transparent)',
  };
  if (fades[s('fade_edges')]) base.push(`mask-image: ${fades[s('fade_edges')]};`);
  for (const [side, pseudo] of [['top', '::before'], ['bottom', '::after']] as const) {
    const d = DIVIDERS[s(`divider_${side}`)];
    if (!d) continue;
    const svg = `url("data:image/svg+xml,${encodeURIComponent(`<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 1440 120' preserveAspectRatio='none'><path d='${d}'/></svg>`)}")`;
    const flip = [o.divider_flip === true ? 'scaleX(-1)' : '', side === 'top' ? 'scaleY(-1)' : ''].filter(Boolean).join(' ');
    base.push('position: relative;');
    extra.push(`${sel}${pseudo} { content: ""; position: absolute; left: 0; right: 0; ${side}: 0; height: ${dividerH}px; background: ${s('divider_color') || 'currentColor'}; mask: ${svg} center / 100% 100% no-repeat; pointer-events: none;${flip ? ` transform: ${flip};` : ''} }`);
  }
  // motion: appears while it scrolls into view (scroll-driven, so without a
  // script and never hiding anything where the browser cannot do it); never
  // with "less motion"
  if (APPEAR.includes(s('appear'))) extra.push(`@media (prefers-reduced-motion: no-preference) { @supports (animation-timeline: view()) { ${sel} { animation: ew-appear-${s('appear').replace('_', '-')} linear both; animation-timeline: view(); animation-range: entry 0% entry 70%; } } }`);
  // text
  if (FONTS[s('font')]) base.push(`font-family: ${FONTS[s('font')]};`);
  responsive('font_size', (v) => `font-size: ${Number(v)}%;${Number(v) > 150 ? ' overflow-wrap: anywhere;' : ''}`);
  if (s('weight') && s('weight') !== 'inherit') base.push(`font-weight: ${Number(s('weight'))};`);
  if (n('line_height') !== undefined) base.push(`line-height: ${n('line_height')};`);
  if (n('letter_spacing') !== undefined) base.push(`letter-spacing: ${n('letter_spacing')}em;`);
  if (s('transform') === 'small-caps') base.push('font-variant-caps: small-caps;');
  else if (s('transform') && s('transform') !== 'none') base.push(`text-transform: ${s('transform')};`);
  if (s('text_color')) {
    // the parts inside (muted text, links, borders, focus, fields) take the
    // checked text color, and their surfaces the checked background, so
    // nothing inside keeps colors made for another background
    const c = s('text_color');
    const surface = surfaces(o)[0];
    base.push(`color: ${c}; --ew-text: ${c}; --ew-text-muted: ${c}; --ew-link: ${c}; --ew-border: ${c}; --ew-focus: ${c};`);
    if (surface) base.push(`--ew-bg: ${surface}; --ew-bg-subtle: ${surface}; --ew-bg-raised: ${surface};`);
    // links keep the text color (its contrast is checked) and are underlined, so they are not told by color alone
    extra.push(`${sel} a:not(.ew-btn) { color: inherit; text-decoration: underline; }`);
    extra.push(`${sel} :focus-visible { outline-color: currentColor; }`);
  }
  responsive('columns', (v) => `columns: ${Number(v)} ${SCHEMA.column_min_rem}rem; column-gap: 2rem;`);
  if (o.hyphens === true) base.push('hyphens: auto;');
  if (s('wrap') && s('wrap') !== 'normal') extra.push(`${sel} :is(h1, h2, h3, h4, h5, h6, p, li) { text-wrap: ${s('wrap')}; }`);
  if (o.drop_cap === true) extra.push(`${sel} p:first-of-type::first-letter { float: left; font-size: 3.2em; line-height: .9; margin: .05em .12em 0 0; font-weight: 700; }`);
  if (TEXT_SHADOWS[s('text_shadow')]) base.push(`text-shadow: ${TEXT_SHADOWS[s('text_shadow')]};`);
  const clamp = n('line_clamp');
  if (clamp) extra.push(`${sel}:not(.ew-bo-open) > .ew-bo-body { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: ${clamp}; line-clamp: ${clamp}; overflow: hidden; }`);
  if (o.gradient_text === true && s('bg_gradient_from') && s('bg_gradient_to')) {
    extra.push(`${sel} :is(h1, h2, h3, h4, h5, h6) { background: linear-gradient(${n('bg_gradient_angle') ?? 90}deg, ${s('bg_gradient_from')}, ${s('bg_gradient_to')}); -webkit-background-clip: text; background-clip: text; color: transparent; }`);
  }
  if (s('link_style') === 'underline') extra.push(`${sel} a:not(.ew-btn) { text-decoration: underline; }`);
  if (s('link_style') === 'thick') extra.push(`${sel} a:not(.ew-btn) { text-decoration: underline; text-decoration-thickness: .15em; text-underline-offset: .15em; }`);
  if (s('underline_style') || s('underline_color')) {
    // links stay underlined (never told by color alone); only the look changes
    extra.push(`${sel} a:not(.ew-btn) { text-decoration-line: underline;${s('underline_style') ? ` text-decoration-style: ${s('underline_style')};` : ''}${s('underline_color') ? ` text-decoration-color: ${s('underline_color')};` : ''} }`);
  }
  const out = [];
  if (base.length) out.push(`${sel} { ${base.join(' ')} }`);
  for (const v of ['tablet', 'mobile'] as const) if (at[v].length) out.push(`@media ${BELOW[v]} { ${sel} { ${at[v].join(' ')} } }`);
  out.push(...extra);
  return out.join('\n');
}

/** A short stable name for a set of options (the same options share their CSS). */
export function optionsKey(o: BlockOptions): string {
  const json = JSON.stringify(Object.keys(o).sort().map((k) => [k, o[k]]));
  let hsh = 2166136261;
  for (let i = 0; i < json.length; i++) { hsh ^= json.charCodeAt(i); hsh = Math.imul(hsh, 16777619); }
  return (hsh >>> 0).toString(36);
}

// ---------------------------------------------------------------- text form
// The options of a block in the content are the text value of its parameter
// "options", not JSON: web application firewalls (OWASP Core Rule Set) take
// JSON inside a text field for an attack and block the editor. Pairs
// id:value separated by spaces, id.view:value per view, lists with commas,
// every other character as %XX. The same as EncodeBlockOptions and
// DecodeBlockOptions on the server (shared cases:
// internal/content/testdata/blockoptions_text_cases.json).

const KEEP = /^[A-Za-z0-9._~/+#:-]$/;
function escapeText(s: string): string {
  let out = '';
  for (const b of new TextEncoder().encode(s)) {
    const c = String.fromCharCode(b);
    out += b < 128 && KEEP.test(c) ? c : `%${b.toString(16).toUpperCase().padStart(2, '0')}`;
  }
  return out;
}
function unescapeText(s: string): string {
  if (!s.includes('%')) return s;
  const bytes: number[] = [];
  for (let i = 0; i < s.length; i++) {
    if (s[i] !== '%') {
      bytes.push(...new TextEncoder().encode(s[i]));
      continue;
    }
    const hex = s.slice(i + 1, i + 3);
    if (!/^[0-9A-Fa-f]{2}$/.test(hex)) throw new Error('broken %XX');
    bytes.push(parseInt(hex, 16));
    i += 2;
  }
  return new TextDecoder().decode(new Uint8Array(bytes));
}
function textValue(v: unknown): string {
  if (Array.isArray(v)) return v.map((x) => escapeText(String(x))).join(',');
  if (typeof v === 'boolean' || typeof v === 'number') return String(v);
  return escapeText(String(v));
}

/** Options in the text form, ids in the order of the schema. */
export function encodeOptions(o: BlockOptions): string {
  const parts: string[] = [];
  for (const g of SCHEMA.groups) {
    for (const def of g.options) {
      const v = o[def.id];
      if (v === undefined || v === null) continue;
      if (typeof v === 'object' && !Array.isArray(v)) {
        for (const view of SCHEMA.views) {
          const x = (v as Partial<Record<ViewName, number | string>>)[view];
          if (x !== undefined && x !== null) parts.push(`${def.id}.${view}:${textValue(x)}`);
        }
        continue;
      }
      if (Array.isArray(v) && !v.length) continue;
      parts.push(`${def.id}:${textValue(v)}`);
    }
  }
  return parts.join(' ');
}

/** Reads the text form; throws on unknown ids or views, doubled ids and
 * values of the wrong type (the server refuses the same). */
export function decodeOptions(s: string): BlockOptions {
  const o: BlockOptions = {};
  for (const tok of s.split(/\s+/).filter(Boolean)) {
    const at = tok.indexOf(':');
    if (at < 0) throw new Error(`${tok}: not id:value`);
    const key = tok.slice(0, at);
    const raw = tok.slice(at + 1);
    const dot = key.indexOf('.');
    const id = dot < 0 ? key : key.slice(0, dot);
    const view = dot < 0 ? '' : key.slice(dot + 1);
    const def = DEFS.get(id);
    if (!def) throw new Error(`unknown option ${id}`);
    if (view && (!def.responsive || !SCHEMA.views.includes(view as ViewName))) throw new Error(`${id}: no value per view ${view}`);
    let v: string | number | boolean | string[];
    switch (def.type) {
      case 'bool':
        if (raw !== 'true' && raw !== 'false') throw new Error(`${id}: true or false`);
        v = raw === 'true';
        break;
      case 'int': case 'number': case 'space': {
        const n = Number(raw);
        if (raw.trim() === '' || !Number.isFinite(n)) throw new Error(`${id}: a number`);
        v = n;
        break;
      }
      case 'views': case 'langs':
        v = raw ? raw.split(',').map(unescapeText) : [];
        break;
      default:
        v = unescapeText(raw);
    }
    const prev = o[id];
    if (view) {
      if (prev !== undefined && (typeof prev !== 'object' || Array.isArray(prev))) throw new Error(`${id} twice`);
      const m = (prev ?? {}) as Partial<Record<ViewName, number | string>>;
      if (m[view as ViewName] !== undefined) throw new Error(`${key} twice`);
      m[view as ViewName] = v as number | string;
      o[id] = m;
    } else {
      if (prev !== undefined) throw new Error(`${id} twice`);
      o[id] = v;
    }
  }
  return o;
}
