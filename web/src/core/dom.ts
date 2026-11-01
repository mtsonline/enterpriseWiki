// Tiny DOM helpers. Everything is created with real DOM APIs (no innerHTML
// with user data), which keeps the UI XSS-safe by construction.
import { hydrate } from './widgets';

export type Child = Node | string | number | null | undefined | false | Child[];
export type Attrs = Record<string, unknown> & {
  class?: string | (string | false | null | undefined)[];
  style?: string;
};

/** Creates an element. Attributes starting with "on" become event listeners. */
export function h<K extends keyof HTMLElementTagNameMap>(tag: K, attrs?: Attrs | null, ...children: Child[]): HTMLElementTagNameMap[K] {
  const el = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (v === undefined || v === null || v === false) continue;
      if (k === 'class') {
        el.className = Array.isArray(v) ? v.filter(Boolean).join(' ') : String(v);
      } else if (k.startsWith('on') && typeof v === 'function') {
        el.addEventListener(k.slice(2).toLowerCase(), v as EventListener);
      } else if (k === 'value' && 'value' in el) {
        (el as unknown as HTMLInputElement).value = String(v);
      } else if (k === 'checked' && 'checked' in el) {
        (el as unknown as HTMLInputElement).checked = Boolean(v);
      } else if (k === 'selected' && 'selected' in el) {
        (el as unknown as HTMLOptionElement).selected = Boolean(v);
      } else if (v === true) {
        el.setAttribute(k, '');
      } else {
        el.setAttribute(k, String(v));
      }
    }
  }
  append(el, children);
  return el;
}

/** Appends children (strings become text nodes). */
export function append(parent: Node, children: Child[]): void {
  for (const c of children) {
    if (c === null || c === undefined || c === false) continue;
    if (Array.isArray(c)) append(parent, c);
    else if (c instanceof Node) parent.appendChild(c);
    else parent.appendChild(document.createTextNode(String(c)));
  }
}

/** Replaces the content of el. */
export function mount(el: Element, ...children: Child[]): void {
  el.replaceChildren();
  append(el, children);
}

/** Inserts trusted, server-sanitized HTML. Only use for rendered content from the API. */
export function trustedHTML(html: string, cls = 'ew-content'): HTMLElement {
  const div = document.createElement('div');
  div.className = cls;
  div.innerHTML = html;
  hydrate(div);
  return div;
}

let idSeq = 0;
/** Returns a unique DOM id. */
export function uid(prefix = 'ew'): string {
  idSeq += 1;
  return `${prefix}-${idSeq}`;
}

/** Formats a date for the current UI language. */
export function fmtDate(iso: string | null | undefined, lang: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  return isNaN(d.getTime()) ? '' : d.toLocaleString(lang, { dateStyle: 'medium', timeStyle: 'short' });
}

/** Visually hidden text for screen readers. */
export function srOnly(text: string): HTMLElement {
  return h('span', { class: 'sr-only' }, text);
}
