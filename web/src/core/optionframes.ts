// Frames of blocks with options, the same for placed blocks and for blocks
// in the content of a page: the CSS of the options (built from values of a
// valid form only, shared by all frames with the same options), the anchor
// (only where its id is free) and, with limited lines, a button to show all.
import { announce } from './announce';
import { h } from './dom';
import { t } from './i18n';
import { state } from './state';
import { formOK, optionsCSS, optionsKey, shownNow, type BlockOptions } from './blockoptions';

const rules = new Set<string>();
let style: HTMLStyleElement | null = null;

/** Gives a frame (.ew-bo with a .ew-bo-body inside) the look of its options;
 * false when the options have no valid form (the frame stays plain). */
export function styleFrame(frame: HTMLElement, o: BlockOptions): boolean {
  if (!formOK(o)) return false;
  const key = optionsKey(o);
  const css = optionsCSS(`.ew-bo[data-bo="${key}"]`, o);
  // (a block readers may close or with a name needs no CSS of its own)
  if (!css && o.closable !== true && typeof o.label !== 'string') return false;
  if (css && !rules.has(key)) {
    rules.add(key);
    style ??= document.head.appendChild(h('style', { id: 'ew-block-options' }));
    style.append(document.createTextNode(`${css}\n`));
  }
  frame.dataset.bo = key;
  backgroundVideo(frame, o);
  if (typeof o.anchor === 'string' && !document.getElementById(o.anchor)) frame.id = o.anchor;
  nameAndScroll(frame, o);
  if (o.sticky === true) stickyFrame(frame);
  if (typeof o.line_clamp === 'number' && o.line_clamp > 0 && !frame.querySelector(':scope > .ew-bo-more')) {
    const more = h('button', { type: 'button', class: 'ew-btn ew-btn--link ew-bo-more', 'aria-expanded': 'false' }, t('bo.more')) as HTMLButtonElement;
    more.addEventListener('click', () => {
      const open = frame.classList.toggle('ew-bo-open');
      more.setAttribute('aria-expanded', String(open));
      more.textContent = open ? t('bo.less') : t('bo.more');
    });
    frame.append(more);
  }
  return true;
}

/** A name for screen readers makes the block a region with that name; a
 * block that scrolls in itself is that region and reachable with the
 * keyboard (WCAG 2.1.1), so its body gets the name and the focus. */
function nameAndScroll(frame: HTMLElement, o: BlockOptions): void {
  const label = typeof o.label === 'string' ? o.label.trim() : '';
  const body = frame.querySelector<HTMLElement>(':scope > .ew-bo-body');
  const scrolls = typeof o.scroll_height === 'number' && o.scroll_height > 0 && !!body;
  const named = scrolls ? body : frame;
  if (scrolls && body) {
    body.tabIndex = 0;
    body.classList.add('ew-bo-scroll');
  }
  if (label && named) {
    named.setAttribute('role', 'region');
    named.setAttribute('aria-label', label);
  }
}

let fitsObserver: ResizeObserver | null = null;
const stickyFrames = new Set<HTMLElement>();

/** Whether a sticky block fits on the screen below the header: only then it
 * sticks (else its end could never be reached while it sticks). */
function checkFits(frame: HTMLElement): void {
  if (!frame.isConnected) { stickyFrames.delete(frame); fitsObserver?.unobserve(frame); return; }
  const top = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--ew-header-h')) || 56;
  frame.classList.toggle('ew-bo--fits', frame.offsetHeight + top + 32 <= window.innerHeight);
}

function stickyFrame(frame: HTMLElement): void {
  if (typeof ResizeObserver === 'undefined') return;
  if (!fitsObserver) {
    fitsObserver = new ResizeObserver((entries) => { for (const e of entries) checkFits(e.target as HTMLElement); });
    window.addEventListener('resize', () => { for (const f of [...stickyFrames]) checkFits(f); });
  }
  stickyFrames.add(frame);
  fitsObserver.observe(frame);
}

const CLOSED = 'ew-closed';

/** A short fingerprint of a text (FNV-1a), to remember closed blocks. */
function fingerprint(text: string): string {
  let h1 = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) h1 = Math.imul(h1 ^ text.charCodeAt(i), 0x01000193) >>> 0;
  return h1.toString(36);
}

function closedList(): string[] {
  try { const v = JSON.parse(localStorage.getItem(CLOSED) ?? '[]'); return Array.isArray(v) ? v.filter((x) => typeof x === 'string') : []; } catch { return []; }
}

/** A block readers may close (option closable, e.g. a notice bar): a
 * button closes it; this browser remembers it until the text of the block
 * changes. Without storage it closes for this page view only. Focus goes
 * on to the next part of the page. */
export function closableFrame(frame: HTMLElement, o: BlockOptions): void {
  if (o.closable !== true || frame.querySelector(':scope > .ew-bo-close')) return;
  const body = frame.querySelector<HTMLElement>(':scope > .ew-bo-body') ?? frame;
  const text = (body.textContent ?? '').replace(/\s+/g, ' ').trim();
  const id = fingerprint(text);
  if (closedList().includes(id)) { frame.hidden = true; return; }
  const name = (body.querySelector('h1, h2, h3, h4, h5, h6')?.textContent ?? text).trim().slice(0, 60);
  const btn = h('button', { type: 'button', class: 'ew-btn ew-btn--subtle ew-bo-close', 'aria-label': name ? t('bo.close_named', { name }) : t('bo.close') }, h('span', { 'aria-hidden': 'true' }, '×')) as HTMLButtonElement;
  btn.addEventListener('click', () => {
    try { localStorage.setItem(CLOSED, JSON.stringify([...closedList().filter((x) => x !== id), id].slice(-50))); } catch { /* storage refused: closed for this view */ }
    // the focus goes on to what follows (or to the main content)
    const all = Array.from(document.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), input:not([disabled]), select, textarea, [tabindex]:not([tabindex="-1"])'));
    const next = all.find((el) => !frame.contains(el) && (frame.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING) && el.offsetParent !== null);
    frame.hidden = true;
    announce(t('bo.closed'));
    const target = next ?? document.querySelector<HTMLElement>('main');
    if (target && !next && !target.hasAttribute('tabindex')) target.setAttribute('tabindex', '-1');
    target?.focus();
  });
  frame.classList.add('ew-bo--closable');
  frame.append(btn);
}

const VIDEO = /^\/api\/v1\/(files\/[A-Za-z0-9._-]{1,128}|media\/[0-9]{1,18}\/view)$/;
const COLOR = /^#[0-9a-f]{6}$/;

/** A video in the background of a frame: silent, in a loop, decoration for
 * screen readers, under the overlay of the options (the contrast of the text
 * was checked against it), always with a button to stop it (WCAG 2.2.2).
 * With "reduce motion" it does not start by itself. */
function backgroundVideo(frame: HTMLElement, o: BlockOptions): void {
  const src = typeof o.bg_video === 'string' ? o.bg_video : '';
  if (!VIDEO.test(src) || frame.querySelector(':scope > .ew-bo-video')) return;
  const poster = typeof o.bg_image === 'string' && VIDEO.test(o.bg_image) ? o.bg_image : undefined;
  const video = h('video', { class: 'ew-bo-video', src, poster, muted: true, loop: true, playsinline: true, preload: 'metadata', 'aria-hidden': 'true', tabindex: '-1' }) as HTMLVideoElement;
  video.muted = true;
  const veil = h('div', { class: 'ew-bo-veil', 'aria-hidden': 'true' });
  const color = typeof o.overlay_color === 'string' && COLOR.test(o.overlay_color) ? o.overlay_color : '';
  const alpha = typeof o.overlay_opacity === 'number' ? Math.min(95, Math.max(0, o.overlay_opacity)) / 100 : 0;
  if (color && alpha > 0) { veil.style.backgroundColor = color; veil.style.opacity = String(alpha); }
  const btn = h('button', { type: 'button', class: 'ew-btn ew-btn--subtle ew-bo-videobtn' }) as HTMLButtonElement;
  const label = () => { btn.textContent = video.paused ? t('bo.video_play') : t('bo.video_pause'); };
  btn.addEventListener('click', () => { if (video.paused) void video.play().catch(() => undefined); else video.pause(); });
  video.addEventListener('play', label);
  video.addEventListener('pause', label);
  label();
  frame.classList.add('ew-bo--video');
  frame.prepend(video, veil);
  frame.append(btn);
  if (!window.matchMedia('(prefers-reduced-motion: reduce)').matches) void video.play().catch(() => label());
}

/** Blocks in the content of a page with options (rendered by the server as
 * .ew-bo[data-ewbo]): their look, and whether they are shown now. */
export function hydrateOptionFrames(root: ParentNode): void {
  root.querySelectorAll<HTMLElement>('.ew-bo[data-ewbo]').forEach((frame) => {
    if (frame.dataset.bo) return;
    let o: BlockOptions;
    try { o = JSON.parse(frame.dataset.ewbo ?? '') as BlockOptions; } catch { return; }
    if (!o || typeof o !== 'object' || Array.isArray(o)) return;
    // a rule of display only: the content is part of the page anyway
    if (!shownNow(o, Date.now(), !!state.user, state.lang)) frame.hidden = true;
    if (styleFrame(frame, o)) closableFrame(frame, o);
  });
}
