// The engine of templates of data: builds the page skeleton and its CSS
// from a design. The designer and the wiki use exactly this code, so what is
// designed is what is shown (1:1). The limits come from the same file the
// server checks with (../limits.json).
import { h, state, t } from '@ewiki/coreplugin';
import type { LayoutContext, Layout } from '@ewiki/coreplugin';
import limitsFile from '../limits.json';

export const LIMITS = limitsFile;
export { positionsRoom, room, needRoom, viewOf, minRem, cssSafe, cssProblems } from './room';
import { cssProblems, cssSafe, minRem, viewOf } from './room';

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
export interface Design { version: 1; tokens: Tokens; rows: Row[]; options: { back_to_top: boolean; sticky_header: boolean; header_shrink?: boolean; header_hide?: boolean; print_plain?: boolean; boxed?: boolean }; css?: string }

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
function keepStickyLow(el: HTMLElement): void {
  const sync = () => {
    const tall = el.getBoundingClientRect().height > (window.innerHeight * LIMITS.sticky_max_vh) / 100;
    el.classList.toggle('ewd-sticky--off', tall);
  };
  new ResizeObserver(sync).observe(el);
  window.addEventListener('resize', sync, { passive: true });
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
  onScroll(() => {
    const y = window.scrollY;
    if (o.header_shrink) el.classList.toggle('ewd-shrunk', y > 40);
    if (o.header_hide) {
      if (y > last + 4 && y > el.offsetHeight && !el.contains(document.activeElement)) el.classList.add('ewd-away');
      else if (y < last - 4 || y <= el.offsetHeight) el.classList.remove('ewd-away');
    }
    last = y;
    height();
  });
  if (o.header_hide) el.addEventListener('focusin', () => { el.classList.remove('ewd-away'); height(); });
}

/** The key under which the browser keeps whether a side column is hidden. */
const collapsedKey = (b: Block) => `ewd-collapsed:${location.host}:${b.id}`;
const readCollapsed = (b: Block): boolean => { try { return localStorage.getItem(collapsedKey(b)) === '1'; } catch { return false; } };
const writeCollapsed = (b: Block, v: boolean): void => { try { if (v) localStorage.setItem(collapsedKey(b), '1'); else localStorage.removeItem(collapsedKey(b)); } catch { /* not kept */ } };

const pick = (m: Record<string, string> | undefined): string => (m ? m[state.lang] || m[state.site.default_language] || Object.values(m)[0] || '' : '');

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
        keepStickyLow(landmark);
        headerBehaviour(landmark, d.options);
      }
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
