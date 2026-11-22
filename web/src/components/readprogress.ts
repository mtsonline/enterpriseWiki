// Reading progress: a thin bar that fills while the content of the page is
// read (block core.readprogress, placed for example under the header). It
// shows only where the reader is; screen readers skip it (the position in
// the text is known to them), so it is decoration.
import { h } from '../core/dom';

let bars: HTMLElement[] = [];
let queued = false;

function update(): void {
  queued = false;
  bars = bars.filter((b) => b.isConnected);
  if (!bars.length) return;
  const main = document.getElementById('main');
  let f = 0;
  if (main) {
    const r = main.getBoundingClientRect();
    const span = r.height - window.innerHeight;
    f = span <= 0 ? (r.top < 0 || r.bottom <= window.innerHeight ? 1 : 0) : Math.min(1, Math.max(0, -r.top / span));
  }
  for (const b of bars) b.style.setProperty('--ew-read', String(Math.round(f * 1000) / 1000));
}

function queue(): void {
  if (queued) return;
  queued = true;
  requestAnimationFrame(update);
}

let listening = false;

/** readProgress renders the bar. */
export function readProgress(): HTMLElement {
  const bar = h('div', { class: 'ew-readprogress', 'aria-hidden': 'true' }, h('div', { class: 'ew-readprogress-bar' }));
  bars.push(bar);
  if (!listening) {
    listening = true;
    window.addEventListener('scroll', queue, { passive: true });
    window.addEventListener('resize', queue, { passive: true });
    // the content changes after the bar is drawn (navigation, images)
    new ResizeObserver(queue).observe(document.body);
  }
  queue();
  return bar;
}
