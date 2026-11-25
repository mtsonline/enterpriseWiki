// The name of a person with a menu: wherever a person is named (author,
// history, comments, cases, team pages, notifications, administration),
// the name is a button that opens a menu with what the viewer may do:
// "Open profile", "Send e-mail" (only when the address may be shown) and
// the entries of plugins (registerPersonAction). The server sends the id of
// a person only to viewers who may open them (core.people.view and the
// rules for finding people); without an id the name stays plain text.
//
// The menu follows the menu pattern (WAI-ARIA APG): one tab stop (the
// name), Enter, Space or a click/tap opens it, the arrow keys, Home and End
// move, Escape and Tab close it and the focus returns to the name.
import { api } from '../core/api';
import { h } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { toast } from '../core/announce';

export { registerPersonAction, personActions, type Person, type PersonAction } from '../core/personactions';
import { personActions, type Person } from '../core/personactions';

// one request per person and page view (the server limits people per hour)
const cache = new Map<number, Promise<Person | null>>();

/** Loads a person (null: not allowed or gone). */
export function loadPerson(id: number): Promise<Person | null> {
  let p = cache.get(id);
  if (!p) {
    p = api.get<Person>(`people/${id}`).catch(() => null);
    cache.set(id, p);
    // a later view asks again (changed cards, rights)
    void p.then(() => setTimeout(() => cache.delete(id), 60_000));
  }
  return p;
}

let open: { el: HTMLElement; close: (back: boolean) => void } | null = null;

let listening = false;

function closeOpen(): void { open?.close(false); }

async function openMenu(btn: HTMLButtonElement, id: number, name: string): Promise<void> {
  if (open && btn.getAttribute('aria-expanded') === 'true') { open.close(true); return; }
  closeOpen();
  if (!listening) {
    // another page: an open menu closes
    window.addEventListener('popstate', closeOpen);
    listening = true;
  }
  btn.setAttribute('aria-busy', 'true');
  const person = await loadPerson(id);
  btn.removeAttribute('aria-busy');
  if (!btn.isConnected) return;
  const items: HTMLElement[] = [h('a', { href: `/people/${id}`, role: 'menuitem', tabindex: '-1', class: 'ew-person-item' }, t('person.open'))];
  if (person?.email) items.push(h('a', { href: `mailto:${person.email}`, role: 'menuitem', tabindex: '-1', class: 'ew-person-item' }, t('person.send_email')));
  if (person) {
    for (const a of personActions(person)) {
      const b = h('button', { type: 'button', role: 'menuitem', tabindex: '-1', class: 'ew-person-item', 'data-action': a.id }, a.label(person));
      b.addEventListener('click', () => {
        close(false);
        Promise.resolve(a.run(person)).catch(() => toast(t('person.action_failed'), 'error'));
      });
      items.push(b);
    }
  }
  const menuId = `ew-person-menu-${id}`;
  const el = h('div', { class: 'ew-person-menu', role: 'menu', id: menuId, 'aria-label': t('person.menu', { name }) }, ...items);
  const focusAt = (i: number) => { const n = items.length; items[((i % n) + n) % n].focus(); };
  el.addEventListener('keydown', (e) => {
    const i = items.indexOf(document.activeElement as HTMLElement);
    if (e.key === 'ArrowDown') { e.preventDefault(); focusAt(i + 1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); focusAt(i - 1); }
    else if (e.key === 'Home') { e.preventDefault(); focusAt(0); }
    else if (e.key === 'End') { e.preventDefault(); focusAt(items.length - 1); }
    else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(true); }
    else if (e.key === 'Tab') { e.preventDefault(); close(true); }
    else if (e.key === ' ' && document.activeElement instanceof HTMLAnchorElement) { e.preventDefault(); document.activeElement.click(); }
  });
  // a link chosen: the menu closes (the page changes)
  el.addEventListener('click', (e) => { if ((e.target as Element).closest('a')) close(false); });
  const outside = (e: Event) => { if (!el.contains(e.target as Node) && e.target !== btn && !btn.contains(e.target as Node)) close(false); };
  const onMove = () => close(false);
  function close(back: boolean) {
    if (!open || open.el !== el) return;
    open = null;
    el.remove();
    btn.setAttribute('aria-expanded', 'false');
    btn.removeAttribute('aria-controls');
    document.removeEventListener('pointerdown', outside, true);
    window.removeEventListener('scroll', onMove, true);
    window.removeEventListener('resize', onMove);
    if (back && btn.isConnected) btn.focus();
  }
  // inside a modal dialog (the rest of the page is inert there)
  (btn.closest('dialog') ?? document.body).append(el);
  // below the name, inside the window (above it when there is no room)
  const r = btn.getBoundingClientRect();
  const m = el.getBoundingClientRect();
  const top = r.bottom + 4 + m.height > window.innerHeight ? Math.max(4, r.top - 4 - m.height) : r.bottom + 4;
  el.style.left = `${Math.max(4, Math.min(r.left, window.innerWidth - m.width - 4))}px`;
  el.style.top = `${top}px`;
  open = { el, close };
  btn.setAttribute('aria-expanded', 'true');
  btn.setAttribute('aria-controls', menuId);
  setTimeout(() => {
    document.addEventListener('pointerdown', outside, true);
    window.addEventListener('scroll', onMove, true);
    window.addEventListener('resize', onMove);
  }, 0);
  focusAt(0);
}

/** The name of a person: with an id a button with the menu, else text. */
export function personName(name: string, id?: number | null): HTMLElement {
  const text = name || '–';
  if (!id || !state.user) return h('span', { class: 'ew-person-text' }, text);
  const btn = h('button', { type: 'button', class: 'ew-person', 'aria-haspopup': 'menu', 'aria-expanded': 'false', 'data-person': String(id) }, text) as HTMLButtonElement;
  btn.addEventListener('click', () => void openMenu(btn, id, text));
  btn.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); void openMenu(btn, id, text); }
  });
  return btn;
}

/** Turns the names marked by the server (span[data-person], e.g. on team
 * pages) into names with the menu. */
export function enhancePeople(root: ParentNode): void {
  root.querySelectorAll<HTMLElement>('span[data-person]').forEach((el) => {
    const id = Number(el.dataset.person);
    if (id > 0) el.replaceWith(personName(el.textContent ?? '', id));
  });
}

/** t() with elements as parameters (e.g. the name of a person in a
 * sentence): the text around them stays text. */
export function tParts(key: string, params: Record<string, string | number | Node>): (string | Node)[] {
  const nodes = new Map<string, Node>();
  const flat: Record<string, string | number> = {};
  for (const [k, v] of Object.entries(params)) {
    if (v instanceof Node) {
      const mark = `\u0000${k}\u0000`;
      nodes.set(mark, v);
      flat[k] = mark;
    } else flat[k] = v;
  }
  const out: (string | Node)[] = [];
  for (const part of t(key, flat).split(/(\u0000[^\u0000]*\u0000)/)) {
    if (!part) continue;
    out.push(nodes.get(part) ?? part);
  }
  return out;
}

