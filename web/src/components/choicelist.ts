// A choice of several entries (groups, roles, people) that stays usable
// with long lists: short lists are checkboxes; longer ones are a list with a
// filter field after the W3C pattern "listbox" with several choices; people
// are searched on the server and appear only after a few letters.
//
// Keys in the list: arrows move, Space or Ctrl+Space chooses or removes the
// entry, Shift+arrow chooses the next entry as well (a range), Ctrl+Shift+
// Home/End chooses up to the first or last entry, Ctrl+A all shown entries,
// typing a letter moves to the filter field. The chosen entries stay listed
// above the list with a remove button each, also when the filter hides them.
import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { newest } from '../core/newest';
import { state } from '../core/state';
import { button, checkbox } from './ui';

/** One entry; detail is shown after the name (for people: the user name). */
export interface Choice { id: number; name: string; detail?: string; handle?: string }

export interface ChoiceList {
  el: HTMLElement;
  /** The ids of the chosen entries, in the order they were chosen. */
  value: () => number[];
  chosen: () => Choice[];
  focus: () => void;
}

/** Lists up to this many entries as checkboxes when the site sets nothing. */
export const CHOICE_CHECKBOXES = 5;
/** The search for people starts with this many letters. */
export const SEARCH_MIN = 2;
/** At most this many entries are shown at once; the filter narrows the rest. */
const SHOWN_MAX = 200;

/**
 * A choice of entries. Either items (a known list; with up to
 * opts.checkboxes, default the site setting, as checkboxes) or search (asked
 * for after SEARCH_MIN letters, always a list).
 */
export function choiceList(opts: {
  label: string;
  items?: Choice[];
  search?: (q: string) => Promise<Choice[]>;
  /** chosen at the start: ids of items, or whole entries for search */
  selected?: (number | Choice)[];
  /** default true; with false, choosing an entry replaces the chosen one */
  multiple?: boolean;
  help?: string;
  required?: boolean;
  checkboxes?: number;
  onChange?: () => void;
}): ChoiceList {
  const multiple = opts.multiple ?? true;
  const items = opts.items ?? [];
  const chosen = new Map<number, Choice>();
  for (const s of opts.selected ?? []) {
    const c = typeof s === 'number' ? items.find((x) => x.id === s) : s;
    if (c) chosen.set(c.id, c);
  }
  const limit = opts.checkboxes ?? (state.site?.choice_checkboxes || CHOICE_CHECKBOXES);
  if (!opts.search && multiple && items.length <= limit) return checkboxes(opts.label, items, chosen, opts);
  return listbox(opts.label, items, chosen, multiple, opts);
}

const text = (c: Choice) => (c.detail ? `${c.name} (${c.detail})` : c.name);

/** Short lists: one checkbox per entry in a fieldset. */
function checkboxes(label: string, items: Choice[], chosen: Map<number, Choice>, opts: { help?: string; onChange?: () => void }): ChoiceList {
  const boxes = items.map((c) => {
    const cb = checkbox(text(c), chosen.has(c.id));
    cb.input.addEventListener('change', () => { if (cb.input.checked) chosen.set(c.id, c); else chosen.delete(c.id); opts.onChange?.(); });
    return cb;
  });
  const help = opts.help ? h('p', { class: 'ew-help', id: uid('ch-help') }, opts.help) : null;
  const el = h('fieldset', { class: 'ew-fieldset ew-choice', 'aria-describedby': help?.id },
    h('legend', null, label), help, h('div', { class: 'ew-check-grid' }, ...boxes.map((b) => b.el)));
  return { el, value: () => [...chosen.keys()], chosen: () => [...chosen.values()], focus: () => boxes[0]?.input.focus() };
}

/** Long lists and the search for people: filter field, chosen entries and a listbox. */
function listbox(label: string, items: Choice[], chosen: Map<number, Choice>, multiple: boolean, opts: { search?: (q: string) => Promise<Choice[]>; help?: string; required?: boolean; onChange?: () => void }): ChoiceList {
  const id = uid('choice');
  const filter = h('input', { class: 'ew-input ew-choice-filter', type: 'text', id: `${id}-q`, autocomplete: 'off', spellcheck: false,
    placeholder: opts.search ? t('choice.search_placeholder', { n: SEARCH_MIN }) : t('choice.filter_placeholder') });
  const labelEl = h('label', { for: filter.id, id: `${id}-l` }, label, opts.required ? h('span', { class: 'ew-req', 'aria-hidden': 'true' }, ' *') : null);
  const list = h('div', { class: 'ew-choice-list', role: 'listbox', id: `${id}-list`, tabindex: 0, 'aria-labelledby': labelEl.id });
  if (multiple) list.setAttribute('aria-multiselectable', 'true');
  if (opts.required) filter.setAttribute('aria-required', 'true');
  filter.setAttribute('aria-controls', list.id);
  const note = h('p', { class: 'ew-help ew-choice-note', id: `${id}-note` });
  const keys = h('p', { class: 'ew-help ew-choice-keys', id: `${id}-keys` }, t(multiple ? 'choice.keys_multiple' : 'choice.keys_single'));
  const help = opts.help ? h('p', { class: 'ew-help', id: `${id}-help` }, opts.help) : null;
  filter.setAttribute('aria-describedby', [help?.id, note.id].filter(Boolean).join(' '));
  list.setAttribute('aria-describedby', [keys.id, help?.id].filter(Boolean).join(' '));
  const live = h('span', { class: 'sr-only', 'aria-live': 'polite' });
  const chips = h('ul', { class: 'ew-chips', 'aria-label': t('choice.chosen', { label }) });

  let shown: Choice[] = [];
  let active = -1;
  let anchor = -1;
  const options = new Map<number, HTMLElement>();

  const changed = () => { opts.onChange?.(); };
  const drawChips = () => {
    chips.replaceChildren(...[...chosen.values()].map((c) => h('li', { class: 'ew-chip' }, h('span', null, text(c)),
      button('×', () => {
        // the focus goes to the next chip (the chips are drawn anew), else to the filter
        const at = [...chosen.keys()].indexOf(c.id);
        remove(c);
        (chips.children[Math.min(at, chips.children.length - 1)]?.querySelector('button') ?? (filter.hidden ? list : filter)).focus();
      }, { variant: 'subtle', ariaLabel: t('common.remove_item', { name: c.name }) }))));
    chips.hidden = chosen.size === 0;
    // one person or group only: the search goes away once something is chosen
    const done = !multiple && chosen.size > 0;
    filter.hidden = done;
    list.hidden = done || (!!opts.search && shown.length === 0);
    keys.hidden = list.hidden;
  };
  const mark = (c: Choice) => options.get(c.id)?.setAttribute('aria-selected', String(chosen.has(c.id)));
  const add = (c: Choice) => {
    if (chosen.has(c.id)) return;
    if (!multiple) { const old = [...chosen.values()]; chosen.clear(); old.forEach(mark); }
    chosen.set(c.id, c);
    mark(c);
    live.textContent = t('common.added_item', { name: c.name });
  };
  const remove = (c: Choice) => {
    if (!chosen.delete(c.id)) return;
    mark(c);
    live.textContent = t('choice.removed', { name: c.name });
    drawChips();
    changed();
  };
  const toggle = (c: Choice) => {
    if (chosen.has(c.id)) remove(c);
    else { add(c); drawChips(); changed(); }
  };

  const setActive = (i: number) => {
    if (!shown.length) { active = -1; list.removeAttribute('aria-activedescendant'); return; }
    active = Math.max(0, Math.min(shown.length - 1, i));
    for (const el of options.values()) el.classList.remove('is-active');
    const el = options.get(shown[active].id)!;
    el.classList.add('is-active');
    list.setAttribute('aria-activedescendant', el.id);
    el.scrollIntoView({ block: 'nearest' });
  };
  const drawOptions = () => {
    options.clear();
    list.replaceChildren(...shown.map((c, i) => {
      const o = h('div', { role: 'option', id: `${id}-o${c.id}`, class: 'ew-choice-option', 'aria-selected': String(chosen.has(c.id)) },
        h('span', null, c.name), c.detail ? h('span', { class: 'ew-choice-detail' }, ` (${c.detail})`) : null);
      o.addEventListener('mousedown', (ev) => ev.preventDefault()); // keep the focus where it is
      o.addEventListener('click', (ev) => {
        if (ev.shiftKey && multiple && anchor >= 0) range(anchor, i);
        else { toggle(c); anchor = i; }
        setActive(i);
        list.focus();
      });
      options.set(c.id, o);
      return o;
    }));
    active = -1;
    anchor = -1;
    list.removeAttribute('aria-activedescendant');
  };
  const range = (from: number, to: number) => {
    const [a, b] = from < to ? [from, to] : [to, from];
    const picked = shown.slice(a, b + 1).filter((c) => !chosen.has(c.id));
    picked.forEach(add);
    if (picked.length) { live.textContent = t('choice.added_n', { n: picked.length }); drawChips(); changed(); }
  };
  const show = (all: Choice[], total = all.length) => {
    shown = all.slice(0, SHOWN_MAX);
    drawOptions();
    note.textContent = total > shown.length ? t('choice.more', { n: shown.length, total }) : '';
    drawChips();
  };

  // filter (known lists) or search (people)
  const fresh = newest();
  let timer = 0;
  const run = () => {
    const q = filter.value.trim();
    if (!opts.search) {
      const needle = q.toLocaleLowerCase();
      const hits = needle ? items.filter((c) => text(c).toLocaleLowerCase().includes(needle)) : items;
      show(hits);
      live.textContent = needle ? t('choice.found', { n: hits.length }) : '';
      return;
    }
    window.clearTimeout(timer);
    if (q.length < SEARCH_MIN) { show([]); note.textContent = q ? t('choice.search_min', { n: SEARCH_MIN }) : ''; return; }
    const current = fresh();
    timer = window.setTimeout(async () => {
      let res: Choice[] = [];
      try { res = await opts.search!(q); } catch { /* no search (guests, no right): nothing found */ }
      if (!current()) return;
      show(res);
      live.textContent = t('choice.found', { n: res.length });
    }, 200);
  };
  filter.addEventListener('input', run);
  filter.addEventListener('keydown', (ev) => {
    if (ev.key === 'ArrowDown' && shown.length) { ev.preventDefault(); list.focus(); setActive(0); }
    else if (ev.key === 'Enter') {
      ev.preventDefault();
      if (shown.length === 1) { toggle(shown[0]); if (opts.search) { filter.value = ''; run(); } }
    }
  });

  list.addEventListener('focus', () => { if (active < 0 && shown.length) setActive(0); });
  list.addEventListener('keydown', (ev) => {
    const ctrl = ev.ctrlKey || ev.metaKey;
    const move = (to: number) => {
      ev.preventDefault();
      const from = active;
      setActive(to);
      if (ev.shiftKey && multiple && active >= 0 && active !== from) {
        if (ctrl && (ev.key === 'Home' || ev.key === 'End')) range(from, active);
        else range(active, active);
        anchor = active;
      }
    };
    switch (ev.key) {
      case 'ArrowDown': move(active + 1); return;
      case 'ArrowUp': if (active <= 0 && !ev.shiftKey) { ev.preventDefault(); filter.focus(); return; } move(active - 1); return;
      case 'Home': move(0); return;
      case 'End': move(shown.length - 1); return;
      case 'PageDown': move(active + 10); return;
      case 'PageUp': move(active - 10); return;
      case ' ':
      case 'Enter':
        ev.preventDefault();
        if (active < 0) return;
        if (ev.shiftKey && multiple && anchor >= 0) range(anchor, active);
        else { toggle(shown[active]); anchor = active; }
        return;
    }
    if (ctrl && ev.key.toLowerCase() === 'a' && multiple) {
      ev.preventDefault();
      const all = shown.every((c) => chosen.has(c.id));
      if (all) { shown.forEach((c) => chosen.delete(c.id)); shown.forEach(mark); live.textContent = t('choice.removed_n', { n: shown.length }); drawChips(); changed(); }
      else range(0, shown.length - 1);
      return;
    }
    // typing goes to the filter field
    if (ev.key.length === 1 && !ctrl && !ev.altKey) { ev.preventDefault(); filter.focus(); filter.value += ev.key; run(); }
  });

  if (opts.search) show([]); else show(items);
  const el = h('div', { class: 'ew-field ew-choice' }, labelEl, help, chips, filter, note, list, keys, live);
  return { el, value: () => [...chosen.keys()], chosen: () => [...chosen.values()], focus: () => (filter.hidden ? chips.querySelector('button') ?? list : filter).focus() };
}
