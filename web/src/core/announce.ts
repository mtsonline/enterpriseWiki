// Screen reader announcements and visible status messages.
import { h } from './dom';

let live: HTMLElement | null = null;
let toasts: HTMLElement | null = null;

function ensure(): void {
  if (live) return;
  live = h('div', { class: 'sr-only', 'aria-live': 'polite', 'aria-atomic': 'true', id: 'ew-live' });
  toasts = h('div', { class: 'ew-toasts', role: 'status', 'aria-live': 'polite' });
  document.body.append(live, toasts);
}

/** Announces text to screen readers only. */
export function announce(text: string): void {
  ensure();
  live!.textContent = '';
  window.setTimeout(() => { live!.textContent = text; }, 50);
}

/** Shows a short visible message (also read by screen readers). */
export function toast(text: string, kind: 'ok' | 'error' = 'ok'): void {
  ensure();
  const el = h('div', { class: ['ew-toast', `ew-toast--${kind}`], role: kind === 'error' ? 'alert' : undefined }, text);
  toasts!.append(el);
  window.setTimeout(() => el.remove(), kind === 'error' ? 8000 : 4000);
}
