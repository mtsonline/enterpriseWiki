// Diagrams from text (```mermaid code blocks): drawn by Mermaid (MIT) in a
// frame without its own origin (sandbox="allow-scripts"), which cannot reach
// the wiki. The page sends the text, the frame answers with its height. The
// text of the diagram stays below it (for screen readers and without script).
import { h } from './dom';
import { t } from './i18n';
import { state } from './state';

const FRAME = '/diagram-frame';
const MIN = 40;
const MAX = 4000;

interface Shown { frame: HTMLIFrameElement; source: string; details: HTMLDetailsElement; note: HTMLElement; sent: boolean }
// by frame: its window exists only once the frame is in the document
const shown = new Set<Shown>();
let listening = false;

/** The page is dark: its background is dark. */
function dark(): boolean {
  const m = /rgba?\((\d+),\s*(\d+),\s*(\d+)/.exec(getComputedStyle(document.body).backgroundColor);
  if (!m) return false;
  const [r, g, b] = m.slice(1).map(Number);
  return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255 < 0.4;
}

function onMessage(ev: MessageEvent): void {
  const win = ev.source as Window | null;
  let s: Shown | undefined;
  for (const x of shown) {
    if (!x.frame.isConnected) shown.delete(x);
    else if (win && x.frame.contentWindow === win) s = x;
  }
  // only from a frame of this page; a frame without origin says "null"
  if (!s || ev.origin !== 'null') return;
  const d = ev.data as { ewDiagram?: boolean; ready?: boolean; ok?: boolean; height?: unknown } | null;
  if (!d || d.ewDiagram !== true) return;
  if (d.ready && !s.sent) {
    s.sent = true;
    win!.postMessage({ source: s.source, dark: dark(), lang: state.lang }, '*');
    return;
  }
  if (d.ok === false) {
    s.frame.hidden = true;
    s.note.textContent = t('diagram.error');
    s.details.open = true;
    return;
  }
  if (typeof d.height === 'number' && Number.isFinite(d.height)) {
    s.frame.style.height = `${Math.min(MAX, Math.max(MIN, Math.round(d.height)))}px`;
  }
}

/** Replaces ```mermaid code blocks in root with drawn diagrams. */
export function hydrateDiagrams(root: ParentNode): void {
  const codes = Array.from(root.querySelectorAll<HTMLElement>('pre > code.language-mermaid'));
  if (!codes.length) return;
  if (!listening) {
    window.addEventListener('message', onMessage);
    listening = true;
  }
  for (const code of codes) {
    const pre = code.parentElement as HTMLElement;
    const source = code.textContent ?? '';
    const frame = h('iframe', {
      class: 'ew-diagram-frame', src: FRAME, sandbox: 'allow-scripts', title: t('diagram.title'),
      referrerpolicy: 'no-referrer', loading: 'lazy',
    }) as HTMLIFrameElement;
    const note = h('p', { class: 'ew-diagram-note', role: 'status' });
    const text = h('pre', null, h('code', null, source));
    const details = h('details', { class: 'ew-diagram-text' }, h('summary', null, t('diagram.as_text')), text) as HTMLDetailsElement;
    const fig = h('figure', { class: 'ew-diagram' }, frame, note, details);
    pre.replaceWith(fig);
    shown.add({ frame, source, details, note, sent: false });
  }
}
