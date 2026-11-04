// Suggestions for @mentions while typing in rich text editors and text
// fields (comments): ArrowUp/ArrowDown choose, Enter or Tab insert, Escape closes.
import { announce, api, h, hasPerm, state, t } from '@ewiki/sdk';
import './style.css';

interface User { id: number; handle: string; display_name: string }

let popup: HTMLUListElement | null = null;
let target: HTMLElement | null = null;
let items: User[] = [];
let active = 0;
let query = '';
let timer = 0;
const QUERY = /(?:^|[\s(>"'„“])@([\p{L}\p{N}][\p{L}\p{N}._-]{1,40})$/u;

function textBeforeCaret(el: HTMLElement): string {
  if (el instanceof HTMLTextAreaElement || el instanceof HTMLInputElement) return el.value.slice(0, el.selectionStart ?? el.value.length);
  const sel = window.getSelection();
  if (!sel || !sel.rangeCount || !el.contains(sel.anchorNode)) return '';
  const r = sel.getRangeAt(0).cloneRange();
  r.selectNodeContents(el);
  r.setEnd(sel.anchorNode!, sel.anchorOffset);
  return r.toString();
}

function close(): void {
  popup?.remove();
  popup = null;
  if (target) {
    target.removeAttribute('aria-activedescendant');
    target.removeAttribute('aria-controls');
    target.setAttribute('aria-expanded', 'false');
  }
  items = [];
}

function handleOf(u: User): string {
  return u.handle.endsWith('@local') ? u.handle.slice(0, -6) : u.handle;
}

function insert(u: User): void {
  if (!target) return;
  const text = `@${handleOf(u)} `;
  if (target instanceof HTMLTextAreaElement || target instanceof HTMLInputElement) {
    const end = target.selectionStart ?? target.value.length;
    const start = end - query.length - 1;
    target.setRangeText(text, start, end, 'end');
    target.dispatchEvent(new Event('input', { bubbles: true }));
  } else {
    const sel = window.getSelection();
    if (sel && sel.rangeCount && sel.anchorNode && sel.anchorNode.nodeType === Node.TEXT_NODE) {
      const node = sel.anchorNode as Text;
      const end = sel.anchorOffset;
      const start = Math.max(0, end - query.length - 1);
      node.replaceData(start, end - start, text);
      const r = document.createRange();
      r.setStart(node, start + text.length);
      r.collapse(true);
      sel.removeAllRanges();
      sel.addRange(r);
      target.dispatchEvent(new Event('input', { bubbles: true }));
    }
  }
  close();
}

function draw(): void {
  if (!target || !items.length) { close(); return; }
  if (!popup) {
    popup = h('ul', { class: 'ewmn-popup', role: 'listbox', id: 'ewmn-list', 'aria-label': t('core.mentions.suggestions') }) as HTMLUListElement;
    document.body.append(popup);
  }
  const r = target.getBoundingClientRect();
  popup.style.left = `${window.scrollX + r.left}px`;
  popup.style.top = `${window.scrollY + r.bottom + 2}px`;
  popup.replaceChildren(...items.map((u, i) => {
    const li = h('li', { role: 'option', id: `ewmn-${u.id}`, 'aria-selected': i === active ? 'true' : 'false', class: i === active ? 'is-active' : undefined },
      h('strong', null, u.display_name || handleOf(u)), ' ', h('span', null, `@${handleOf(u)}`));
    li.addEventListener('mousedown', (ev) => { ev.preventDefault(); insert(u); });
    return li;
  }));
  target.setAttribute('aria-controls', 'ewmn-list');
  target.setAttribute('aria-expanded', 'true');
  target.setAttribute('aria-activedescendant', `ewmn-${items[active].id}`);
}

function onInput(ev: Event): void {
  const el = ev.target as HTMLElement;
  if (!(el instanceof HTMLTextAreaElement) && !el.isContentEditable) return;
  if (!state.user || !hasPerm('core.mentions.use')) return;
  const m = QUERY.exec(textBeforeCaret(el));
  if (!m) { if (target === el) close(); return; }
  target = el;
  target.setAttribute('aria-autocomplete', 'list');
  query = m[1];
  window.clearTimeout(timer);
  timer = window.setTimeout(async () => {
    const q = query;
    const res = await api.get<User[]>(`users/lookup?q=${encodeURIComponent(q)}`).catch(() => []);
    if (q !== query || target !== el) return;
    items = res.slice(0, 8);
    active = 0;
    draw();
    if (items.length) announce(`${items.length} ${t('core.mentions.suggestions')}`);
  }, 200);
}

function onKey(ev: KeyboardEvent): void {
  if (!popup || !items.length || ev.target !== target) return;
  switch (ev.key) {
    case 'ArrowDown': active = (active + 1) % items.length; draw(); break;
    case 'ArrowUp': active = (active - 1 + items.length) % items.length; draw(); break;
    case 'Enter': case 'Tab': insert(items[active]); break;
    case 'Escape': close(); break;
    default: return;
  }
  ev.preventDefault();
  ev.stopPropagation();
}

export function register(): void {
  document.addEventListener('input', onInput, true);
  document.addEventListener('keydown', onKey, true);
  document.addEventListener('focusout', (ev) => { if (ev.target === target) window.setTimeout(close, 150); }, true);
}
