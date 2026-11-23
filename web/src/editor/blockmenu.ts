// The menu of a block in the visual editor: right click, a long press on a
// touch screen, the context menu key or Shift+F10 open it at the block. It
// offers what the buttons of the block do, plus duplicating – deleting is
// one entry of a menu, never the click itself (WCAG 2.5.2: an action is
// not done on the down event, and a long press happens by mistake).
import { h } from '../core/dom';
import { t } from '../core/i18n';

export interface MenuItem {
  label: string;
  run: () => void;
  danger?: boolean;
  disabled?: boolean;
}

let open: { el: HTMLElement; close: (back: boolean) => void } | null = null;

/** Whether a block menu is open (a second opening is ignored). */
export function blockMenuOpen(): boolean { return open !== null; }

/** Opens the menu named name with the items at the point x, y of the window
 * (kept inside it). Escape, Tab, a click elsewhere or scrolling close it;
 * the focus goes back to where it was. */
export function blockMenu(name: string, items: MenuItem[], x: number, y: number, back?: HTMLElement | null): void {
  open?.close(false);
  const before = back ?? (document.activeElement as HTMLElement | null);
  const buttons = items.map((it) => {
    const b = h('button', { type: 'button', role: 'menuitem', tabindex: '-1', class: ['ewe-menu-item', it.danger && 'ewe-menu-item--danger'] }, it.label);
    if (it.disabled) b.setAttribute('aria-disabled', 'true');
    b.addEventListener('click', (e) => {
      e.preventDefault();
      if (it.disabled) return;
      close(false);
      it.run();
    });
    return b;
  });
  const el = h('div', { class: 'ewe-menu', role: 'menu', 'aria-label': t('editor.block_menu', { name }) }, ...buttons);
  const focusAt = (i: number) => { const n = buttons.length; buttons[((i % n) + n) % n].focus(); };
  el.addEventListener('keydown', (e) => {
    const i = buttons.indexOf(document.activeElement as HTMLButtonElement);
    if (e.key === 'ArrowDown') { e.preventDefault(); focusAt(i + 1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); focusAt(i - 1); }
    else if (e.key === 'Home') { e.preventDefault(); focusAt(0); }
    else if (e.key === 'End') { e.preventDefault(); focusAt(buttons.length - 1); }
    else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(true); }
    else if (e.key === 'Tab') { e.preventDefault(); close(true); }
    else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
      // the first entry starting with the letter typed
      const k = e.key.toLocaleLowerCase();
      const from = i + 1;
      for (let j = 0; j < buttons.length; j++) {
        const b = buttons[(from + j) % buttons.length];
        if (b.textContent?.toLocaleLowerCase().startsWith(k)) { b.focus(); break; }
      }
    }
  });
  const outside = (e: Event) => { if (!el.contains(e.target as Node)) close(false); };
  const onScroll = () => close(false);
  function close(backToBefore: boolean) {
    if (!open || open.el !== el) return;
    open = null;
    el.remove();
    document.removeEventListener('pointerdown', outside, true);
    window.removeEventListener('scroll', onScroll, true);
    window.removeEventListener('resize', onScroll);
    if (backToBefore && before?.isConnected) before.focus();
  }
  document.body.append(el);
  // inside the window
  const r = el.getBoundingClientRect();
  el.style.left = `${Math.max(4, Math.min(x, window.innerWidth - r.width - 4))}px`;
  el.style.top = `${Math.max(4, Math.min(y, window.innerHeight - r.height - 4))}px`;
  open = { el, close };
  // after this event: a click that opened it does not close it again
  setTimeout(() => {
    document.addEventListener('pointerdown', outside, true);
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', onScroll);
  }, 0);
  const first = buttons.findIndex((b) => !b.hasAttribute('aria-disabled'));
  focusAt(first < 0 ? 0 : first);
}

/** The long press of a touch screen on el: after LONG_MS without moving
 * more than a few pixels, fn gets the point; when fn took it (true), the
 * click when the finger is lifted is not a click. A press that starts on a button is left to
 * the button (its name shows on a long press, see tooltip.ts). */
const LONG_MS = 550;
export function onLongPress(el: HTMLElement, fn: (x: number, y: number, target: Element) => boolean): void {
  let timer = 0;
  let start: { x: number; y: number } | null = null;
  let fired = false;
  const stop = () => { clearTimeout(timer); timer = 0; start = null; };
  el.addEventListener('pointerdown', (e) => {
    fired = false;
    if (e.pointerType !== 'touch' || !(e.target instanceof Element) || e.target.closest('button, a, input, select, textarea')) return;
    const target = e.target;
    start = { x: e.clientX, y: e.clientY };
    timer = window.setTimeout(() => { timer = 0; fired = fn(start!.x, start!.y, target); }, LONG_MS);
  });
  el.addEventListener('pointermove', (e) => {
    if (start && Math.hypot(e.clientX - start.x, e.clientY - start.y) > 10) stop();
  });
  el.addEventListener('pointerup', stop);
  el.addEventListener('pointercancel', stop);
  el.addEventListener('click', (e) => { if (fired) { fired = false; e.preventDefault(); e.stopPropagation(); } }, true);
  // the browser's own menu of a long press (Android) is the same gesture
  el.addEventListener('contextmenu', (e) => { if (fired) e.preventDefault(); });
}
