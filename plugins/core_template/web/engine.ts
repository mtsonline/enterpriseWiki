// The engine of templates of data: builds the page skeleton and its CSS
// from a design. The designer and the wiki use exactly this code, so what is
// designed is what is shown (1:1). The limits come from the same file the
// server checks with (../limits.json).
import { h, state, t } from '@ewiki/coreplugin';
import type { LayoutContext, Layout } from '@ewiki/coreplugin';
import limitsFile from '../limits.json';

export const LIMITS = limitsFile;

export type ViewName = 'desktop' | 'tablet' | 'mobile';
export const VIEWS: ViewName[] = ['desktop', 'tablet', 'mobile'];

export interface Tokens {
  bg: string; bg_subtle: string; text: string; text_muted: string; border: string; primary: string; on_primary: string;
  link: string; focus: string; header_bg: string; header_text: string; footer_bg: string; footer_text: string;
  font: string; heading_font: string; scale: number; radius: number; content_width: number; logo: string;
}
export interface Block {
  id: string; kind: 'content' | 'area' | 'brand'; role: 'navigation' | 'complementary' | 'region' | 'none';
  label?: Record<string, string>; width: number; fit?: boolean; positions: string[]; after?: string[];
  layout: 'row' | 'column'; align: 'start' | 'center' | 'end' | 'between';
}
export interface View { stack: boolean; hidden?: string[]; drawer?: string[]; widths?: Record<string, number> }
export interface Row {
  id: string; role: 'banner' | 'contentinfo' | 'none'; width: 'full' | 'content'; background: 'page' | 'subtle' | 'header' | 'footer';
  blocks: Block[]; tablet?: View; mobile?: View;
}
export interface Design { version: 1; tokens: Tokens; rows: Row[]; options: { back_to_top: boolean; sticky_header: boolean } }

/** How a row looks in a view: as designed (manual) or by the rules (automatic). */
export interface ViewState { stack: boolean; hidden: string[]; drawer: string[]; widths: Record<string, number>; auto: boolean }

/** The breakpoints: the narrowest window of the tablet and of the desktop. */
const TABLET_MIN = LIMITS.views.tablet.min_px;
const DESKTOP_MIN = LIMITS.views.desktop.min_px;

function minRem(b: Block, view: ViewName): number {
  return b.kind === 'content' ? LIMITS.views[view].content_min_rem : (LIMITS.block_min_rem as Record<string, number>)[b.kind];
}

/** The width (rem) a row has in the narrowest window of a view (as the server computes it). */
export function room(d: Design, r: Row, view: ViewName): number {
  let w = LIMITS.views[view].min_px / LIMITS.rem_px;
  if (r.width === 'content' && d.tokens.content_width < w) w = d.tokens.content_width;
  return w - 2 * LIMITS.padding_rem;
}

/** The width (rem) the blocks of a row need side by side in a view (as the server computes it). */
export function needRoom(r: Row, view: ViewName, v?: Partial<View>): number {
  let sum = 0;
  let n = 0;
  for (const b of r.blocks) {
    if (v?.hidden?.includes(b.id) || v?.drawer?.includes(b.id)) continue;
    n++;
    const w = v?.widths && b.id in v.widths ? v.widths[b.id] : b.width;
    sum += b.kind === 'content' ? minRem(b, view) : w > 0 ? w : minRem(b, view);
  }
  return sum + (n > 1 ? LIMITS.gap_rem * (n - 1) : 0);
}

/** The view of a row: the one designed by hand, else the rules:
 * - tablet: side columns beside the content go into a drawer when they do
 *   not fit; blocks go one below the other when they still do not fit;
 * - mobile: menus of the header and side columns beside the content go into
 *   drawers; blocks of the header take the rest; what does not fit goes one
 *   below the other. */
export function viewOf(d: Design, r: Row, view: ViewName): ViewState {
  if (view === 'desktop') return { stack: false, hidden: [], drawer: [], widths: {}, auto: false };
  const manual = r[view];
  if (manual) return { stack: manual.stack, hidden: manual.hidden ?? [], drawer: manual.drawer ?? [], widths: manual.widths ?? {}, auto: false };
  const hasContent = r.blocks.some((b) => b.kind === 'content');
  const drawer: string[] = [];
  const widths: Record<string, number> = {};
  if (view === 'mobile' && r.role === 'banner') {
    for (const b of r.blocks) if (b.role === 'navigation') drawer.push(b.id);
  }
  if (hasContent && needRoom(r, view, { drawer, widths }) > room(d, r, view)) {
    for (const b of r.blocks) if (b.role === 'complementary' || (view === 'mobile' && b.role === 'navigation')) drawer.push(b.id);
  }
  const stack = needRoom(r, view, { drawer, widths }) > room(d, r, view);
  return { stack, hidden: [], drawer, widths, auto: true };
}

// ---------------------------------------------------------------- CSS

const FONTS: Record<string, string> = {
  system: 'system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
  inter: '"Inter Variable", system-ui, sans-serif',
  'source-sans': '"Source Sans 3 Variable", system-ui, sans-serif',
  serif: 'Georgia, "Times New Roman", serif',
  mono: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
};

/** The CSS class of a template on <body>. */
export function classOf(id: string): string {
  return `ewd-${id.replace(/^core\.template\//, '').replace(/[^a-z0-9-]/g, '-')}`;
}

/** The CSS of a design: only values that were checked (colors #rrggbb,
 * numbers, ids of letters, digits and dashes), so nothing else can get into it. */
export function designCSS(d: Design, cls: string): string {
  const k = d.tokens;
  const s = `body.${cls}`;
  const font = (f: string) => FONTS[f] ?? FONTS.system;
  const out: string[] = [];
  out.push(`html:has(${s}) { font-size: ${Number(k.scale)}%; }`);
  out.push(`${s} { --ew-bg: ${k.bg}; --ew-bg-subtle: ${k.bg_subtle}; --ew-bg-raised: ${k.bg}; --ew-text: ${k.text}; --ew-text-muted: ${k.text_muted};
  --ew-border: ${k.border}; --ew-primary: ${k.primary}; --ew-primary-hover: color-mix(in srgb, ${k.primary} 82%, black); --ew-on-primary: ${k.on_primary};
  --ew-link: ${k.link}; --ew-focus: ${k.focus}; --ew-radius: ${Number(k.radius)}px; --ew-font: ${font(k.font)};
  --ewd-header-bg: ${k.header_bg}; --ewd-header-text: ${k.header_text}; --ewd-footer-bg: ${k.footer_bg}; --ewd-footer-text: ${k.footer_text};
  --ewd-content-width: ${Number(k.content_width)}rem; --ewd-gap: ${LIMITS.gap_rem}rem; --ewd-pad: ${LIMITS.padding_rem}rem; }`);
  out.push(`${s} h1, ${s} h2, ${s} h3, ${s} h4 { font-family: ${font(k.heading_font)}; }`);
  const tablet: string[] = [];
  const mobile: string[] = [];
  for (const r of d.rows) {
    const rs = `${s} .ewd-row[data-row="${r.id}"]`;
    for (const b of r.blocks) {
      const bs = `${rs} > .ewd-inner > .ewd-block[data-block="${b.id}"]`;
      // fixed width, as wide as the content (down to the minimum) or the rest of the row
      const flex = (w: number, view: ViewName) => (w > 0 ? `flex: 0 1 ${w}rem; min-width: min(${w}rem, 100%);`
        : b.fit ? `flex: 0 1 auto; min-width: min(${minRem(b, view)}rem, 100%);` : `flex: 1 1 0; min-width: min(${minRem(b, view)}rem, 100%);`);
      out.push(`${bs} { ${flex(b.width, 'desktop')} }`);
      for (const [view, list] of [['tablet', tablet], ['mobile', mobile]] as const) {
        const v = viewOf(d, r, view);
        const w = b.id in v.widths ? v.widths[b.id] : b.width;
        if (v.hidden.includes(b.id)) list.push(`${bs} { display: none; }`);
        else if (v.stack) list.push(`${bs} { flex: none; width: 100%; min-width: 0; }`);
        else list.push(`${bs} { ${flex(w, view)} }`);
        if (v.drawer.includes(b.id)) {
          // beside the content: the button is a bar above it and the open
          // drawer comes first; elsewhere (header) the button stays in the
          // row and the open drawer comes below it
          const withContent = r.blocks.some((x) => x.kind === 'content');
          list.push(`${bs}:not(.ewd-open) { display: none; }`);
          list.push(`${bs}.ewd-open { flex: none; width: 100%; order: ${withContent ? -1 : 99}; }`);
          list.push(`${rs} > .ewd-inner > .ewd-drawer-button[aria-controls="ewd-${b.id}"] { display: inline-flex;${withContent ? ' order: -2; flex: 0 0 100%;' : ''} }`);
        }
      }
    }
    for (const [view, list] of [['tablet', tablet], ['mobile', mobile]] as const) {
      const v = viewOf(d, r, view);
      // tablet and phone: what does not fit side by side goes to the next line
      // (the desktop keeps the layout as designed and checked)
      list.push(`${rs} > .ewd-inner { flex-direction: ${v.stack ? 'column' : 'row'}; flex-wrap: wrap; }`);
    }
  }
  out.push(`@media (max-width: ${DESKTOP_MIN - 0.02}px) {\n${tablet.join('\n')}\n}`);
  out.push(`@media (max-width: ${TABLET_MIN - 0.02}px) {\n${mobile.join('\n')}\n}`);
  return out.join('\n');
}

// ---------------------------------------------------------------- layout

const pick = (m: Record<string, string> | undefined): string => (m ? m[state.lang] || m[state.site.default_language] || Object.values(m)[0] || '' : '');

/** The page skeleton of a design (positions are live containers of the wiki). */
export function buildLayout(d: Design, ctx: LayoutContext): Layout {
  const main = h('main', { id: 'main', class: 'ewd-main', tabindex: '-1' });
  const rowEl = (r: Row): HTMLElement => {
    const inner = h('div', { class: 'ewd-inner' });
    for (const b of r.blocks) {
      const kids: Node[] = [];
      if (b.kind === 'brand') {
        kids.push(h('a', { href: '/', class: 'ewd-brand' },
          d.tokens.logo ? h('img', { src: d.tokens.logo, alt: '', class: 'ewd-logo' }) : null,
          h('span', { class: 'ewd-site-name' }, state.site.name)));
      }
      for (const p of b.positions) kids.push(ctx.position(p));
      if (b.kind === 'content') {
        kids.push(main);
        for (const p of b.after ?? []) kids.push(ctx.position(p));
      }
      const label = pick(b.label);
      const tag = b.kind === 'content' ? 'div' : b.role === 'navigation' ? 'nav' : b.role === 'complementary' ? 'aside' : b.role === 'region' ? 'section' : 'div';
      const el = h(tag, {
        id: `ewd-${b.id}`, class: ['ewd-block', `ewd-block--${b.kind}`, `ewd-layout--${b.layout}`, `ewd-align--${b.align}`], 'data-block': b.id,
        'aria-label': tag !== 'div' && label ? label : undefined,
      }, ...kids);
      // a block that can be a drawer in some view gets its button (shown only there)
      const drawerSomewhere = (['tablet', 'mobile'] as const).some((v) => viewOf(d, r, v).drawer.includes(b.id));
      if (drawerSomewhere) {
        const btn = h('button', { type: 'button', class: 'ewd-drawer-button ew-btn ew-btn--subtle', 'aria-expanded': 'false', 'aria-controls': `ewd-${b.id}` },
          h('span', { 'aria-hidden': 'true' }, b.role === 'navigation' ? '☰ ' : '▸ '), label || (b.role === 'navigation' ? t('nav.menu') : t('template.more')));
        const toggle = (open: boolean) => { el.classList.toggle('ewd-open', open); btn.setAttribute('aria-expanded', String(open)); };
        btn.addEventListener('click', () => toggle(btn.getAttribute('aria-expanded') !== 'true'));
        el.addEventListener('keydown', (ev: Event) => {
          if ((ev as KeyboardEvent).key === 'Escape' && el.classList.contains('ewd-open')) { toggle(false); btn.focus(); }
        });
        inner.append(btn);
      }
      inner.append(el);
    }
    return h('div', { class: ['ewd-row', `ewd-bg--${r.background}`, `ewd-width--${r.width}`], 'data-row': r.id }, inner);
  };
  // adjacent rows of the header (and of the footer) form one landmark
  const parts: Node[] = [h('a', { href: '#main', class: 'ew-skip' }, t('nav.skip'))];
  let i = 0;
  while (i < d.rows.length) {
    const r = d.rows[i];
    if (r.role === 'banner' || r.role === 'contentinfo') {
      const group: HTMLElement[] = [];
      while (i < d.rows.length && d.rows[i].role === r.role) group.push(rowEl(d.rows[i++]));
      parts.push(h(r.role === 'banner' ? 'header' : 'footer', { class: ['ewd-landmark', r.role === 'banner' && d.options.sticky_header && 'ewd-sticky'] }, ...group));
    } else {
      parts.push(rowEl(r));
      i++;
    }
  }
  if (d.options.back_to_top) {
    const top = h('a', { href: '#main', class: 'ewd-to-top ew-btn ew-btn--secondary', hidden: true }, h('span', { 'aria-hidden': 'true' }, '↑ '), t('template.to_top'));
    const sync = () => { top.hidden = window.scrollY < window.innerHeight; };
    window.addEventListener('scroll', sync, { passive: true });
    parts.push(top);
  }
  return { root: h('div', { class: 'ewd-app' }, ...parts), main };
}
