// Tooltips for buttons and links that show only an icon (✎, ×, ☰, 🔔 …):
// their name appears on hover, on keyboard focus and on a long press on
// touch screens (tablets and phones have no hover). One tooltip for the
// whole interface, by event delegation, so every icon button gets it
// without its own code.
//
// WCAG 1.4.13: the tooltip can be dismissed (Escape), hovered (it stays
// while the pointer is on it) and stays until the pointer or focus leaves.
// It takes no clicks itself (pointer-events: none): a tooltip never covers
// the next button; whether the pointer is on it is checked by position.
// It repeats the accessible name, so screen readers do not read it twice
// (aria-hidden).

const SHOW_DELAY = 350; // ms on hover
const LONG_PRESS = 500; // ms on touch
const TOUCH_SHOWN = 2500; // ms a tooltip of a long press stays

let tip: HTMLDivElement | null = null;
let owner: HTMLElement | null = null;
let showTimer: ReturnType<typeof setTimeout> | null = null;
let hideTimer: ReturnType<typeof setTimeout> | null = null;
let pressTimer: ReturnType<typeof setTimeout> | null = null;
let pressAt: { x: number; y: number } | null = null;
let suppressClickUntil = 0;

const TARGETS = 'button, a[href], [role="button"], summary';

/** The visible text of an element without screen reader only text. */
function visibleText(el: Element): string {
  let s = '';
  for (const n of el.childNodes) {
    if (n.nodeType === Node.TEXT_NODE) s += n.textContent ?? '';
    else if (n instanceof Element && !n.classList.contains('sr-only') && n.getAttribute('aria-hidden') !== 'true') s += visibleText(n);
    else if (n instanceof Element && n.getAttribute('aria-hidden') === 'true') s += n.textContent ?? ''; // the icon itself
  }
  return s.trim();
}

/** The name to show for an icon-only control, or "" for controls with a readable text. */
export function iconName(el: HTMLElement): string {
  const text = visibleText(el);
  // readable text (letters or digits beyond a single character): no tooltip needed
  if (/[\p{L}\p{N}]{2,}/u.test(text)) return '';
  const label = el.getAttribute('aria-label') || el.dataset.ewTitle || el.getAttribute('title')
    || [...el.querySelectorAll('.sr-only')].map((x) => x.textContent ?? '').join(' ');
  return label.replace(/\s+/g, ' ').trim();
}

function targetOf(e: Event): HTMLElement | null {
  const el = (e.target as Element | null)?.closest?.(TARGETS) as HTMLElement | null;
  if (!el || el.closest('.ew-tooltip') || el.closest('[inert]')) return null;
  return iconName(el) ? el : null;
}

function clearTimers(): void {
  if (showTimer) clearTimeout(showTimer);
  if (hideTimer) clearTimeout(hideTimer);
  showTimer = hideTimer = null;
}

/** Shows the tooltip of el (now). */
function show(el: HTMLElement): void {
  clearTimers();
  const name = iconName(el);
  if (!name || !el.isConnected) return;
  // the browser's own tooltip of a title would show twice: keep the title as data
  const title = el.getAttribute('title');
  if (title) {
    if (!el.hasAttribute('aria-label') && !visibleText(el).trim()) el.setAttribute('aria-label', title);
    el.dataset.ewTitle = title;
    el.removeAttribute('title');
  }
  if (!tip) {
    tip = document.createElement('div');
    tip.className = 'ew-tooltip';
    tip.setAttribute('role', 'tooltip');
    tip.setAttribute('aria-hidden', 'true');
  }
  tip.textContent = name;
  // inside a modal dialog only the dialog is shown above the page (top layer)
  const host = el.closest('dialog[open]') ?? document.body;
  if (tip.parentElement !== host) host.append(tip);
  owner = el;
  tip.hidden = false;
  place(el, tip);
}

function place(el: HTMLElement, t: HTMLDivElement): void {
  const r = el.getBoundingClientRect();
  const host = t.parentElement!;
  const hr = host === document.body ? { left: 0, top: 0 } : host.getBoundingClientRect();
  t.style.left = '0px';
  t.style.top = '0px';
  const w = t.offsetWidth, hgt = t.offsetHeight;
  const vw = document.documentElement.clientWidth, vh = document.documentElement.clientHeight;
  let x = r.left + r.width / 2 - w / 2;
  x = Math.max(4, Math.min(x, vw - w - 4));
  let y = r.bottom + 6;
  if (y + hgt > vh - 4) y = r.top - hgt - 6; // no room below: above
  const fixed = host === document.body;
  t.style.position = fixed ? 'fixed' : 'absolute';
  t.style.left = `${Math.round(x - (fixed ? 0 : hr.left))}px`;
  t.style.top = `${Math.round(y - (fixed ? 0 : hr.top))}px`;
}

function hide(): void {
  clearTimers();
  if (tip) tip.hidden = true;
  owner = null;
}

function hideSoon(): void {
  if (hideTimer) return; // already on the way out
  hideTimer = setTimeout(hide, 150);
}

let started = false;

/** Starts the tooltips of icon buttons (once). */
export function startTooltips(): void {
  if (started || typeof document === 'undefined') return;
  started = true;
  document.addEventListener('pointerover', (e) => {
    if (e.pointerType === 'touch') return;
    const el = targetOf(e);
    if (!el) return;
    if (el === owner) { if (hideTimer) clearTimeout(hideTimer); return; }
    clearTimers();
    showTimer = setTimeout(() => show(el), owner ? 0 : SHOW_DELAY);
  });
  document.addEventListener('pointerout', (e) => {
    if (e.pointerType === 'touch') return;
    const el = targetOf(e);
    if (!el) return;
    const to = e.relatedTarget as Node | null;
    if (to && el.contains(to)) return;
    if (showTimer && !owner) { clearTimeout(showTimer); showTimer = null; }
    if (el === owner) hideSoon();
  });
  // keyboard focus (not a click with the mouse)
  document.addEventListener('focusin', (e) => {
    const el = targetOf(e);
    if (!el) return;
    let visible = true;
    try { visible = el.matches(':focus-visible'); } catch { /* old browser: show */ }
    if (visible) show(el);
  });
  document.addEventListener('focusout', (e) => { if (e.target === owner) hide(); });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && tip && !tip.hidden) { hide(); }
  }, true);
  // touch: a long press shows the name instead of pressing the button
  document.addEventListener('pointerdown', (e) => {
    if (e.pointerType !== 'touch') { if (owner) hide(); return; }
    const el = targetOf(e);
    if (pressTimer) clearTimeout(pressTimer);
    if (!el) { hide(); return; }
    pressAt = { x: e.clientX, y: e.clientY };
    pressTimer = setTimeout(() => {
      pressTimer = null;
      show(el);
      suppressClickUntil = Date.now() + 800;
      hideTimer = setTimeout(hide, TOUCH_SHOWN);
    }, LONG_PRESS);
  }, { passive: true });
  const cancelPress = () => { if (pressTimer) { clearTimeout(pressTimer); pressTimer = null; } };
  document.addEventListener('pointermove', (e) => {
    if (pressTimer && pressAt && Math.hypot(e.clientX - pressAt.x, e.clientY - pressAt.y) > 10) cancelPress();
    // the pointer on the tooltip keeps it; leaving it (not to its button) hides it
    if (e.pointerType === 'touch' || !owner || !tip || tip.hidden) return;
    const r = tip.getBoundingClientRect();
    const on = e.clientX >= r.left - 2 && e.clientX <= r.right + 2 && e.clientY >= r.top - 8 && e.clientY <= r.bottom + 8;
    if (on) { if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; } } else if (!owner.contains(e.target as Node)) hideSoon();
  }, { passive: true });
  document.addEventListener('pointerup', cancelPress, { passive: true });
  document.addEventListener('pointercancel', cancelPress, { passive: true });
  // the long press only shows the name: the button is not pressed as well
  document.addEventListener('click', (e) => {
    if (Date.now() < suppressClickUntil) { suppressClickUntil = 0; e.preventDefault(); e.stopPropagation(); }
  }, true);
  // the context menu of a long press would cover the tooltip
  document.addEventListener('contextmenu', (e) => { if (Date.now() < suppressClickUntil && targetOf(e)) e.preventDefault(); });
  window.addEventListener('scroll', () => { if (owner) hide(); }, { passive: true, capture: true });
  window.addEventListener('resize', () => { if (owner) hide(); });
}
