// The engine of templates of data: builds the page skeleton and its CSS
// from a design. The designer and the wiki use exactly this code, so what is
// designed is what is shown (1:1). The limits come from the same file the
// server checks with (../limits.json).
import { h, imageSetting, state, t } from '@ewiki/coreplugin';
import type { LayoutContext, Layout } from '@ewiki/coreplugin';
import limitsFile from '../limits.json';

export const LIMITS = limitsFile;
export { positionsRoom, room, needRoom, viewOf, minRem, cssSafe, cssProblems, schemeOf } from './room';
import { cssProblems, cssSafe, minRem, viewOf } from './room';

export type ViewName = 'desktop' | 'tablet' | 'mobile';
export const VIEWS: ViewName[] = ['desktop', 'tablet', 'mobile'];

/** The colors of one scheme (light or dark). */
export interface Palette {
  bg: string; bg_subtle: string; text: string; text_muted: string; border: string; primary: string; on_primary: string;
  link: string; focus: string; header_bg: string; header_text: string; footer_bg: string; footer_text: string;
}
export { PALETTE_KEYS, withSettings } from './settings';
export interface Tokens extends Palette {
  /** the colors of the dark scheme; without it the template has light colors only */
  dark?: Palette;
  font: string; heading_font: string; scale: number; radius: number; content_width: number; logo: string;
}
export interface Block {
  id: string; kind: 'content' | 'area' | 'brand'; role: 'navigation' | 'complementary' | 'region' | 'none';
  label?: Record<string, string>; width: number; fit?: boolean; positions: string[]; after?: string[];
  layout: 'row' | 'column'; align: 'start' | 'center' | 'end' | 'between';
  /** a side column that stays in view / can be hidden (desktop) */
  sticky?: boolean; collapsible?: boolean;
}
export interface View { stack: boolean; hidden?: string[]; drawer?: string[]; widths?: Record<string, number> }
export interface Row {
  id: string; role: 'banner' | 'contentinfo' | 'none'; width: 'full' | 'content'; background: 'page' | 'subtle' | 'header' | 'footer';
  blocks: Block[]; desktop?: Pick<View, 'drawer'> & Partial<View>; tablet?: View; mobile?: View;
  /** a footer row whose blocks open and close on the phone */
  collapse_mobile?: boolean;
}
export interface Design { version: 1; tokens: Tokens; rows: Row[]; options: { back_to_top: boolean; sticky_header: boolean; header_shrink?: boolean; header_hide?: boolean; header_overlay?: boolean; print_plain?: boolean; boxed?: boolean; color_scheme?: 'system' | 'light' | 'dark' }; css?: string }

/** How a row looks in a view: as designed (manual) or by the rules (automatic). */
export interface ViewState { stack: boolean; hidden: string[]; drawer: string[]; widths: Record<string, number>; auto: boolean }

/** The breakpoints: the narrowest window of the tablet and of the desktop. */
const TABLET_MIN = LIMITS.views.tablet.min_px;
const DESKTOP_MIN = LIMITS.views.desktop.min_px;

// ---------------------------------------------------------------- CSS

/** An uploaded font in the tokens (the same form the server checks). */
export const OWN_FONT = /^own-[1-9][0-9]{0,17}$/;

const FONTS: Record<string, string> = {
  system: 'system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
  inter: '"Inter Variable", system-ui, sans-serif',
  'source-sans': '"Source Sans 3 Variable", system-ui, sans-serif',
  serif: 'Georgia, "Times New Roman", serif',
  mono: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
};

/** The content of the area "Logo and name": the logo of the settings (else
 * that of the design), on narrow screens the mark, in the dark colour
 * scheme the logos for dark colours; always the name of the wiki. The
 * size of the logo comes from the CSS (.ewd-logo), so it keeps its room
 * before it loads. */
function brandOf(d: Design, values: Record<string, unknown> | undefined): Node[] {
  const v = values ?? {};
  const own = imageSetting(v.logo);
  const logo = own?.src ?? d.tokens.logo;
  const mark = imageSetting(v.mark)?.src;
  const pic = (src: string, m: string | undefined, cls: string) => {
    const img = h('img', { src, alt: '', class: 'ewd-logo', decoding: 'async' });
    const el = m ? h('picture', { class: 'ewd-logo-picture' }, h('source', { media: '(max-width: 600px)', srcset: m }), img) : img;
    return cls ? h('span', { class: ['ewd-logo-picture', cls] }, el) : el;
  };
  const out: Node[] = [];
  const darkLogo = imageSetting(v.logo_on_dark)?.src;
  const darkMark = imageSetting(v.mark_on_dark)?.src;
  if (logo && (darkLogo || darkMark) && d.tokens.dark) out.push(pic(logo, mark, 'ew-on-light'), pic(darkLogo ?? logo, darkMark ?? mark, 'ew-on-dark'));
  else if (logo) out.push(pic(logo, mark, ''));
  out.push(h('span', { class: 'ewd-site-name' }, state.site.name));
  return out;
}

/** The CSS class of a template on <body>. */
export function classOf(id: string): string {
  return `ewd-${id.replace(/^core\.template\//, '').replace(/[^a-z0-9-]/g, '-')}`;
}

/** A block behind a button: beside the content the button is a bar above
 * it and the open drawer comes first; elsewhere (header) the button stays
 * in the row and the open drawer comes below it. */
function drawerRules(r: Row, b: Block, rs: string, bs: string): string[] {
  const withContent = r.blocks.some((x) => x.kind === 'content');
  return [
    `${bs}:not(.ewd-open) { display: none; }`,
    `${bs}.ewd-open { flex: none; width: 100%; order: ${withContent ? -1 : 99}; }`,
    `${rs} > .ewd-inner > .ewd-drawer-button[aria-controls="ewd-${b.id}"] { display: inline-flex;${withContent ? ' order: -2; flex: 0 0 100%;' : ''} }`,
  ];
}

/** The CSS of a design: only values that were checked (colors #rrggbb,
 * numbers, ids of letters, digits and dashes), so nothing else can get into it. */
export function designCSS(d: Design, cls: string): string {
  const k = d.tokens;
  const s = `body.${cls}`;
  // nothing but values of a safe form goes into the CSS (see cssSafe)
  if (!cssSafe(d)) return '';
  const out: string[] = [];
  // uploaded fonts (own-ID, checked on the server): the server writes their
  // @font-face rules (one per file: weight, style, format) from checked
  // values; imported first, as CSS requires
  const own = (f: string) => OWN_FONT.test(f) ? `"ewd-${f}"` : '';
  const font = (f: string) => own(f) ? `${own(f)}, ${FONTS.system}` : FONTS[f] ?? FONTS.system;
  for (const f of new Set([k.font, k.heading_font])) {
    if (own(f)) out.push(`@import url("/api/v1/x/core.template/fonts/${f.slice(4)}/css");`);
  }
  out.push(`html:has(${s}) { font-size: ${Number(k.scale)}%; }`);
  const colors = (p: Palette, hover: string) => `--ew-bg: ${p.bg}; --ew-bg-subtle: ${p.bg_subtle}; --ew-bg-raised: ${p.bg}; --ew-text: ${p.text}; --ew-text-muted: ${p.text_muted};
  --ew-border: ${p.border}; --ew-primary: ${p.primary}; --ew-primary-hover: color-mix(in srgb, ${p.primary} 82%, ${hover}); --ew-on-primary: ${p.on_primary};
  --ew-link: ${p.link}; --ew-focus: ${p.focus};
  --ewd-header-bg: ${p.header_bg}; --ewd-header-text: ${p.header_text}; --ewd-footer-bg: ${p.footer_bg}; --ewd-footer-text: ${p.footer_text};`;
  out.push(`${s} { ${colors(k, 'black')} --ew-radius: ${Number(k.radius)}px; --ew-font: ${font(k.font)};
  --ewd-content-width: ${Number(k.content_width)}rem; --ewd-gap: ${LIMITS.gap_rem}rem; --ewd-pad: ${LIMITS.padding_rem}rem; }`);
  // the dark scheme (core/scheme.ts sets ewc-dark on the body): the other
  // colors of the wiki (inputs, notes, status …) come from the dark scheme of the core
  if (k.dark) out.push(`body.ewc-dark.${cls} { ${colors(k.dark, 'white')} }`);
  out.push(`${s} h1, ${s} h2, ${s} h3, ${s} h4 { font-family: ${font(k.heading_font)}; }`);
  const tablet: string[] = [];
  const mobile: string[] = [];
  for (const r of d.rows) {
    const rs = `${s} .ewd-row[data-row="${r.id}"]`;
    // an open drawer takes its own line: on the desktop too, the row wraps then
    if (viewOf(d, r, 'desktop').drawer.length) out.push(`${rs} > .ewd-inner { flex-wrap: wrap; }`);
    for (const b of r.blocks) {
      const bs = `${rs} > .ewd-inner > .ewd-block[data-block="${b.id}"]`;
      // fixed width, as wide as the content (down to the minimum) or the rest of the row
      const flex = (w: number, view: ViewName) => (w > 0 ? `flex: 0 1 ${w}rem; min-width: min(${w}rem, 100%);`
        : b.fit ? `flex: 0 1 auto; min-width: min(${minRem(b, view)}rem, 100%);` : `flex: 1 1 0; min-width: min(${minRem(b, view)}rem, 100%);`);
      out.push(`${bs} { ${flex(b.width, 'desktop')} }`);
      // behind a button also on the desktop (and so in every view)
      if (viewOf(d, r, 'desktop').drawer.includes(b.id)) out.push(...drawerRules(r, b, rs, bs));
      for (const [view, list] of [['tablet', tablet], ['mobile', mobile]] as const) {
        const v = viewOf(d, r, view);
        const w = b.id in v.widths ? v.widths[b.id] : b.width;
        if (v.hidden.includes(b.id)) list.push(`${bs} { display: none; }`);
        else if (v.stack) list.push(`${bs} { flex: none; width: 100%; min-width: 0; }`);
        else list.push(`${bs} { ${flex(w, view)} }`);
        if (v.drawer.includes(b.id)) list.push(...drawerRules(r, b, rs, bs));
      }
    }
    for (const [view, list] of [['tablet', tablet], ['mobile', mobile]] as const) {
      const v = viewOf(d, r, view);
      // tablet and phone: what does not fit side by side goes to the next line
      // (the desktop keeps the layout as designed and checked)
      list.push(`${rs} > .ewd-inner { flex-direction: ${v.stack ? 'column' : 'row'}; flex-wrap: wrap; }`);
    }
  }
  // side columns stay in view and can be hidden on the desktop only; footers fold on the phone only
  tablet.push(`${s} .ewd-block--sticky { position: static; max-height: none; overflow: visible; }`, `${s} .ewd-collapse-button { display: none; }`);
  mobile.push(`${s} .ewd-fold-button { display: flex; }`, `${s} .ewd-block:not(.ewd-unfolded) > .ewd-fold-body { display: none; }`);
  out.push(`@media (max-width: ${DESKTOP_MIN - 0.02}px) {\n${tablet.join('\n')}\n}`);
  out.push(`@media (max-width: ${TABLET_MIN - 0.02}px) {\n${mobile.join('\n')}\n}`);
  // own CSS of the template (the server sends it only while the administration allows it)
  if (d.css && cssProblems(d.css).length === 0) out.push(d.css);
  return out.join('\n');
}

/** A sticky header may take at most sticky_max_vh of the height of the
 * window; when it is higher (a phone held sideways, an open drawer, large
 * text), it scrolls with the page so it never hides the content and the
 * focus (WCAG 1.4.10, 2.4.11). */
function keepStickyLow(el: HTMLElement, signal: AbortSignal): void {
  const sync = () => {
    // an open drawer does not count: it scrolls in itself below the closed
    // header (else opening the menu far down the page would make the header
    // too high, so not sticky any more, and the open menu would be out of sight)
    let open = 0;
    for (const d of el.querySelectorAll<HTMLElement>('.ewd-block.ewd-open')) open += d.getBoundingClientRect().height;
    const closed = Math.max(0, el.getBoundingClientRect().height - open);
    el.style.setProperty('--ewd-header-closed', `${Math.ceil(closed)}px`);
    const tall = closed > (window.innerHeight * LIMITS.sticky_max_vh) / 100;
    el.classList.toggle('ewd-sticky--off', tall);
  };
  new ResizeObserver(sync).observe(el);
  window.addEventListener('resize', sync, { passive: true, signal });
}

// ---------------------------------------------------------------- layout

// one listener for the scrolling of the page; each layout puts its own
// handlers here (a layout built again replaces those of the one before)
let scrollHandlers: (() => void)[] = [];
let scrollListening = false;
// the same for the change between the desktop and the smaller views
let desktopHandlers: (() => void)[] = [];
let desktopQuery: MediaQueryList | null = null;
function isDesktop(): boolean {
  if (!desktopQuery) {
    desktopQuery = window.matchMedia(`(min-width: ${DESKTOP_MIN}px)`);
    desktopQuery.addEventListener('change', () => { for (const f of desktopHandlers) f(); });
  }
  return desktopQuery.matches;
}
function onScroll(fn: () => void): void {
  scrollHandlers.push(fn);
  if (!scrollListening) {
    scrollListening = true;
    window.addEventListener('scroll', () => { for (const f of scrollHandlers) f(); }, { passive: true });
  }
}

/** The behaviour of a sticky header: lower once the page is scrolled, away
 * while scrolling down and back when scrolling up or when the keyboard focus
 * is in it (it never hides what has the focus, WCAG 2.4.11). Its height is
 * known to sticky side columns (--ewd-header-h). */
function headerBehaviour(el: HTMLElement, o: Design['options']): void {
  let last = window.scrollY;
  let shown = '';
  const height = () => {
    const v = `${el.classList.contains('ewd-sticky--off') || el.classList.contains('ewd-away') ? 0 : Math.ceil(el.getBoundingClientRect().height)}px`;
    if (v !== shown) { shown = v; document.documentElement.style.setProperty('--ewd-header-h', v); }
  };
  new ResizeObserver(height).observe(el);
  // a change of the header (lower) changes the height above the content;
  // the browser then moves the page by that much to keep the reader's place
  // (scroll anchoring). That move is not the reader scrolling up, so the
  // next scroll by exactly that much is only noted.
  let anchorShift = 0;
  onScroll(() => {
    const y = window.scrollY;
    if (anchorShift && Math.abs(last - y - anchorShift) <= 2) { anchorShift = 0; last = y; return; }
    anchorShift = 0;
    const before = el.offsetHeight;
    if (o.header_shrink) el.classList.toggle('ewd-shrunk', y > 40);
    if (o.header_hide) {
      // never away with the focus in it or with a drawer of it open
      if (y > last + 4 && y > el.offsetHeight && !el.contains(document.activeElement) && !el.querySelector('.ewd-open')) el.classList.add('ewd-away');
      else if (y < last - 4 || y <= el.offsetHeight) el.classList.remove('ewd-away');
    }
    anchorShift = before - el.offsetHeight;
    last = y;
    height();
  });
  if (o.header_hide) el.addEventListener('focusin', () => { el.classList.remove('ewd-away'); height(); });
}

/** The first hero banner below the header (in a row of the template, e.g.
 * the position "hero", or at the start of the content), as its outer frame
 * (a frame of options carries its background), or null. */
function heroBelow(header: HTMLElement): HTMLElement | null {
  const app = header.parentElement;
  if (!app) return null;
  for (const hero of app.querySelectorAll<HTMLElement>('.ewl-hero')) {
    if (header.contains(hero) || !(header.compareDocumentPosition(hero) & Node.DOCUMENT_POSITION_FOLLOWING)) continue;
    // its outer box: wrappers of the same size (the frame of its options, the
    // element of the block) move with it
    let box: HTMLElement = hero;
    const same = (a: DOMRect, b: DOMRect) => Math.abs(a.left - b.left) < 1 && Math.abs(a.right - b.right) < 1 && Math.abs(a.top - b.top) < 1 && Math.abs(a.bottom - b.bottom) < 1;
    while (box.parentElement && !box.parentElement.matches('.ew-pos, .ewd-block, .ewd-main') && same(box.getBoundingClientRect(), box.parentElement.getBoundingClientRect())) box = box.parentElement;
    return box;
  }
  return null;
}

/** The header lies on a hero banner that follows it directly (option
 * header_overlay): without its background, in the color of the banner's
 * text (whose contrast the banner has), the banner reaching under it over
 * the whole width of the header. Only where nothing visible lies between
 * them (a title, a path) and the banner is about as wide as the header (no
 * side column beside it); else the header stays as it is. It gets its
 * background back while a drawer or a menu of it is open and – sticky –
 * once the banner is scrolled away. */
function headerOverlay(el: HTMLElement, sticky: boolean, signal: AbortSignal): void {
  let frame: HTMLElement | null = null;
  let painted: HTMLElement | null = null;
  let height = 0;
  let queued = 0;
  const undo = () => {
    el.classList.remove('ewd-overlay', 'ewd-overlay--solid');
    el.style.removeProperty('margin-bottom');
    el.style.removeProperty('--ewd-overlay-text');
    if (frame) {
      for (const p of ['margin-top', 'margin-left', 'width', 'max-width', 'align-self']) frame.style.removeProperty(p);
      frame.classList.remove('ewd-hero-under');
      frame = null;
    }
    if (painted) {
      for (const p of ['padding-top', 'border-radius']) painted.style.removeProperty(p);
      painted = null;
    }
  };
  const solid = () => {
    if (!frame) return;
    el.classList.toggle('ewd-overlay--solid', sticky && frame.getBoundingClientRect().bottom <= el.getBoundingClientRect().height);
  };
  const apply = () => {
    queued = 0;
    watch();
    // the same banner under a header of the same height: nothing to do (the
    // page changes often, e.g. a ticker; no measuring again each time). A
    // header that gets lower while scrolling keeps the room it had at the top.
    if (frame && frame.isConnected && heroBelow(el) === frame && (window.scrollY > 0 || Math.abs(el.getBoundingClientRect().height - height) < 1)) { solid(); return; }
    undo();
    const f = heroBelow(el);
    if (!f) return;
    const area = f.closest<HTMLElement>('.ewd-block');
    if (!area) return;
    const hr = el.getBoundingClientRect(), fr = f.getBoundingClientRect(), ar = area.getBoundingClientRect();
    const gap = fr.top - hr.bottom;
    // directly below the header, in an area about as wide as the header (no
    // side column beside it), not beside other parts in a row
    if (gap < 0 || gap > 64 || fr.height === 0 || ar.left - hr.left > 64 || hr.right - ar.right > 64) return;
    if (area.classList.contains('ewd-layout--row') && ar.width - fr.width > 128) return;
    const heroText = getComputedStyle(f.querySelector('.ewl-hero') ?? f).color;
    frame = f;
    height = hr.height;
    // the part that paints the banner (its own background or that of the
    // frame of its options) reaches up under the header
    const hero = f.querySelector<HTMLElement>('.ewl-hero') ?? f;
    let paint: HTMLElement = hero;
    for (let x: HTMLElement | null = f; x && x !== hero; x = x.querySelector<HTMLElement>(':scope > *')) {
      const cs = getComputedStyle(x);
      if (cs.backgroundImage !== 'none' || !/rgba\(0, 0, 0, 0\)|transparent/.test(cs.backgroundColor)) { paint = x; break; }
    }
    painted = paint;
    const pad = parseFloat(getComputedStyle(paint).paddingTop) || 0;
    // as wide as the header, from its left edge (measured, whatever aligns it)
    f.style.setProperty('align-self', 'flex-start');
    f.style.setProperty('max-width', 'none');
    f.style.setProperty('width', `${hr.width}px`);
    f.style.setProperty('margin-left', '0px');
    f.style.setProperty('margin-left', `${hr.left - f.getBoundingClientRect().left}px`);
    f.style.setProperty('margin-top', `${-gap}px`);
    paint.style.setProperty('padding-top', `${pad + hr.height}px`);
    paint.style.setProperty('border-radius', '0');
    f.classList.add('ewd-hero-under');
    el.style.setProperty('margin-bottom', `${-hr.height}px`);
    el.style.setProperty('--ewd-overlay-text', heroText);
    el.classList.add('ewd-overlay');
    solid();
  };
  const queue = () => { if (!queued) queued = requestAnimationFrame(apply); };
  // the page around the header (it is built before it is in the document)
  let watching = false;
  const watch = () => {
    if (watching || !el.parentElement) return;
    watching = true;
    new MutationObserver(queue).observe(el.parentElement, { childList: true, subtree: true });
  };
  window.addEventListener('resize', queue, { passive: true, signal });
  new ResizeObserver(queue).observe(el);
  if (sticky) onScroll(solid);
  queue();
}

/** The key under which the browser keeps whether a side column is hidden. */
const collapsedKey = (b: Block) => `ewd-collapsed:${location.host}:${b.id}`;
const readCollapsed = (b: Block): boolean => { try { return localStorage.getItem(collapsedKey(b)) === '1'; } catch { return false; } };
const writeCollapsed = (b: Block, v: boolean): void => { try { if (v) localStorage.setItem(collapsedKey(b), '1'); else localStorage.removeItem(collapsedKey(b)); } catch { /* not kept */ } };

/** The text of a label in the language of the page. */
export const pick = (m: Record<string, string> | undefined): string => (m ? m[state.lang] || m[state.site.default_language] || Object.values(m)[0] || '' : '');

/** The page skeleton of a design (positions are live containers of the wiki). */
export function buildLayout(d: Design, ctx: LayoutContext): Layout {
  scrollHandlers = [];
  desktopHandlers = [];
  const main = h('main', { id: 'main', class: 'ewd-main', tabindex: '-1' });
  const rowEl = (r: Row): HTMLElement => {
    const inner = h('div', { class: 'ewd-inner' });
    for (const b of r.blocks) {
      const kids: Node[] = [];
      if (b.kind === 'brand') {
        kids.push(h('a', { href: '/', class: 'ewd-brand' }, ...brandOf(d, ctx.settings)));
      }
      for (const p of b.positions) kids.push(ctx.position(p));
      if (b.kind === 'content') {
        kids.push(main);
        for (const p of b.after ?? []) kids.push(ctx.position(p));
      }
      const label = pick(b.label);
      const tag = b.kind === 'content' ? 'div' : b.role === 'navigation' ? 'nav' : b.role === 'complementary' ? 'aside' : b.role === 'region' ? 'section' : 'div';
      // a footer that folds on the phone: a button with the name of the block opens its content
      const fold = !!r.collapse_mobile && b.kind !== 'content';
      const body = fold ? h('div', { class: 'ewd-fold-body', id: `ewd-${b.id}-body` }, ...kids) : null;
      const foldBtn = fold ? h('button', { type: 'button', class: 'ewd-fold-button', 'aria-expanded': 'false', 'aria-controls': `ewd-${b.id}-body` }, label || b.id, h('span', { class: 'ewd-fold-sign', 'aria-hidden': 'true' })) as HTMLButtonElement : null;
      const el = h(tag, {
        id: `ewd-${b.id}`, class: ['ewd-block', `ewd-block--${b.kind}`, `ewd-layout--${b.layout}`, `ewd-align--${b.align}`, b.sticky && 'ewd-block--sticky'], 'data-block': b.id, 'data-role': b.role,
        'aria-label': tag !== 'div' && label ? label : undefined,
      }, ...(fold ? [foldBtn!, body!] : kids));
      if (foldBtn) foldBtn.addEventListener('click', () => {
        const open = foldBtn.getAttribute('aria-expanded') !== 'true';
        foldBtn.setAttribute('aria-expanded', String(open));
        el.classList.toggle('ewd-unfolded', open);
      });
      // a side column that can be hidden (desktop): the button stays where the column was
      if (b.collapsible && b.role === 'complementary') {
        const name = label || t('template.side_column');
        const btn = h('button', { type: 'button', class: 'ewd-collapse-button ew-btn ew-btn--subtle', 'aria-controls': `ewd-${b.id}` }) as HTMLButtonElement;
        const set = (hidden: boolean) => {
          // hidden only where the button is (desktop); smaller views show the column as designed for them
          el.classList.toggle('ewd-collapsed', hidden && isDesktop());
          btn.dataset.hidden = String(hidden);
          btn.setAttribute('aria-expanded', String(!hidden));
          btn.textContent = hidden ? t('template.column_show', { name }) : t('template.column_hide', { name });
        };
        btn.addEventListener('click', () => { const hidden = btn.dataset.hidden !== 'true'; set(hidden); writeCollapsed(b, hidden); });
        desktopHandlers.push(() => set(btn.dataset.hidden === 'true'));
        set(readCollapsed(b));
        inner.append(btn);
      }
      // a block that can be a drawer in some view gets its button (shown only there)
      const drawerSomewhere = (['desktop', 'tablet', 'mobile'] as const).some((v) => viewOf(d, r, v).drawer.includes(b.id));
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
      const landmark = h(r.role === 'banner' ? 'header' : 'footer', { class: ['ewd-landmark', r.role === 'banner' && d.options.sticky_header && 'ewd-sticky'] }, ...group);
      if (r.role === 'banner' && d.options.sticky_header) {
        keepStickyLow(landmark, ctx.signal);
        headerBehaviour(landmark, d.options);
      }
      if (r.role === 'banner' && d.options.header_overlay) headerOverlay(landmark, !!d.options.sticky_header, ctx.signal);
      parts.push(landmark);
    } else {
      parts.push(rowEl(r));
      i++;
    }
  }
  if (d.options.back_to_top) {
    const top = h('a', { href: '#main', class: 'ewd-to-top ew-btn ew-btn--secondary', hidden: true }, h('span', { 'aria-hidden': 'true' }, '↑ '), t('template.to_top'));
    const sync = () => { top.hidden = window.scrollY < window.innerHeight; };
    onScroll(sync);
    parts.push(top);
  }
  return { root: h('div', { class: ['ewd-app', d.options.print_plain && 'ewd-print-plain', d.options.boxed && 'ewd-boxed'] }, ...parts), main };
}
