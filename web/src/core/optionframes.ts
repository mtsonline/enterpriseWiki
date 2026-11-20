// Frames of blocks with options, the same for placed blocks and for blocks
// in the content of a page: the CSS of the options (built from values of a
// valid form only, shared by all frames with the same options), the anchor
// (only where its id is free) and, with limited lines, a button to show all.
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
  if (!css) return false;
  if (!rules.has(key)) {
    rules.add(key);
    style ??= document.head.appendChild(h('style', { id: 'ew-block-options' }));
    style.append(document.createTextNode(`${css}\n`));
  }
  frame.dataset.bo = key;
  backgroundVideo(frame, o);
  if (typeof o.anchor === 'string' && !document.getElementById(o.anchor)) frame.id = o.anchor;
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
    styleFrame(frame, o);
  });
}
