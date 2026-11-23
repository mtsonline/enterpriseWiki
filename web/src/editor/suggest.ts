// Suggestions of blocks while typing: "/" at the start of a paragraph, or
// ":" (also "::", ":::") followed by letters, as blocks are written in
// Markdown. The list shows matching blocks by name and the task with
// person and date; Enter or Tab opens the dialog of the block (its
// parameters are filled in there, nobody needs to know how they are
// written), Escape closes the list. A screen reader hears the number of
// suggestions and the active one (aria-activedescendant).
import { announce } from '../core/announce';
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import type { MacroInfo } from '../core/types';
import { fold, matches } from './blockgroups';
import { allowedMacros, label, loadMacros } from './macros';

export type SuggestChoice = { macro: string } | { task: true } | { all: true };

export interface SuggestItem { id: string; name: string; hint: string; choice: SuggestChoice }

/** The text typed for a suggestion at the end of before (the text of the
 * paragraph or line up to the cursor): "/" only at its start, colons after
 * a space or at the start with at least one letter. */
export function suggestQuery(before: string): { length: number; query: string } | null {
  const slash = /^\s*\/([\p{L}\p{N}._ -]{0,30})$/u.exec(before);
  if (slash && !/\s\s/.test(slash[1])) return { length: 1 + slash[1].length, query: slash[1].trim() };
  const colon = /(?:^|\s)(:{1,3})([\p{L}][\p{L}\p{N}._-]{0,30})$/u.exec(before);
  if (colon) return { length: colon[1].length + colon[2].length, query: colon[2] };
  return null;
}

/** The suggestions for a query: blocks whose name, label or description
 * match (name or label first), the task item, and "All blocks …". */
export function suggestItems(macros: MacroInfo[], query: string, max = 7): SuggestItem[] {
  const q = fold(query);
  const task = { id: 'task', name: t('editor.task_title'), hint: t('editor.task_desc'), choice: { task: true } as SuggestChoice };
  const scored: { item: SuggestItem; score: number }[] = [];
  for (const m of macros) {
    const name = label(m.labels) || m.name;
    const desc = label(m.description);
    const head = fold(`${name} ${m.name}`);
    let score = -1;
    if (!q) score = 2;
    else if (fold(m.name).startsWith(q) || fold(name).startsWith(q)) score = 0;
    else if (head.split(/[\s._-]+/).some((w) => w.startsWith(q))) score = 1;
    else if (matches(fold(`${head} ${desc}`), q)) score = 2;
    if (score >= 0) scored.push({ item: { id: `m-${m.name}`, name, hint: `:${m.name}`, choice: { macro: m.name } }, score });
  }
  scored.sort((a, b) => a.score - b.score || a.item.name.localeCompare(b.item.name));
  const out = scored.slice(0, max).map((x) => x.item);
  const taskWords = fold(`${task.name} task todo aufgabe checkliste checklist`);
  if (!q || taskWords.split(/\s+/).some((w) => w.startsWith(q))) out.unshift(task);
  out.push({ id: 'all', name: t('editor.suggest_all'), hint: '', choice: { all: true } });
  return out;
}

/** A list of suggestions under a text field or the visual editor. */
export class Suggest {
  private list: HTMLUListElement | null = null;
  private items: SuggestItem[] = [];
  private active = 0;
  private readonly id = uid('ewe-suggest');
  private gen = 0;
  /** the text that was closed with Escape: not shown again until it changes */
  dismissed = '';

  constructor(private readonly host: HTMLElement, private readonly pick: (c: SuggestChoice) => void) {}

  get open(): boolean { return this.list !== null; }

  /** Shows the suggestions for query below rect (or closes the list). */
  async show(query: string, rect: { left: number; bottom: number }): Promise<void> {
    const gen = ++this.gen;
    const macros = await loadMacros().then(allowedMacros).catch(() => [] as MacroInfo[]);
    if (gen !== this.gen) return;
    const items = suggestItems(macros, query);
    const was = this.list !== null;
    this.items = items;
    this.active = 0;
    if (!this.list) {
      this.list = h('ul', { class: 'ewe-suggest', role: 'listbox', id: this.id, 'aria-label': t('editor.suggest_label') }) as HTMLUListElement;
      document.body.append(this.list);
    }
    this.list.style.left = `${Math.max(4, window.scrollX + rect.left)}px`;
    this.list.style.top = `${window.scrollY + rect.bottom + 4}px`;
    this.draw();
    this.host.setAttribute('aria-controls', this.id);
    this.host.setAttribute('aria-expanded', 'true');
    this.host.setAttribute('aria-autocomplete', 'list');
    if (!was) announce(t('editor.suggest_count', { count: items.length }));
  }

  private draw(): void {
    if (!this.list) return;
    this.list.replaceChildren(...this.items.map((x, i) => {
      const li = h('li', { role: 'option', id: `${this.id}-${i}`, 'aria-selected': String(i === this.active), class: i === this.active ? 'is-active' : undefined },
        h('span', { class: 'ewe-suggest-name' }, x.name), x.hint ? h('span', { class: 'ewe-suggest-hint', 'aria-hidden': 'true' }, ` ${x.hint}`) : null);
      li.addEventListener('mousedown', (ev) => { ev.preventDefault(); this.choose(i); });
      return li;
    }));
    this.host.setAttribute('aria-activedescendant', `${this.id}-${this.active}`);
    this.list.children[this.active]?.scrollIntoView({ block: 'nearest' });
  }

  private choose(i: number): void {
    const c = this.items[i]?.choice;
    this.close();
    if (c) this.pick(c);
  }

  close(): void {
    this.gen++;
    this.list?.remove();
    this.list = null;
    this.items = [];
    for (const a of ['aria-controls', 'aria-activedescendant', 'aria-autocomplete']) this.host.removeAttribute(a);
    this.host.setAttribute('aria-expanded', 'false');
  }

  /** Handles a key while the list is open; true when it was used. */
  key(ev: KeyboardEvent, typed: string): boolean {
    if (!this.list || !this.items.length) return false;
    switch (ev.key) {
      case 'ArrowDown': this.active = (this.active + 1) % this.items.length; this.draw(); break;
      case 'ArrowUp': this.active = (this.active - 1 + this.items.length) % this.items.length; this.draw(); break;
      case 'Enter': case 'Tab': this.choose(this.active); break;
      case 'Escape': this.dismissed = typed; this.close(); break;
      default: return false;
    }
    ev.preventDefault();
    ev.stopPropagation();
    return true;
  }
}
