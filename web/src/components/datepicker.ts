// Accessible date picker (pattern "Date Picker Dialog" of the W3C ARIA
// Authoring Practices). The date picker of the browser reads only the day
// number ("25") while the person moves through it, without weekday, month
// or year. Every date field therefore gets a button "Choose date" that
// opens a dialog with a month grid:
//   - each day has its full name ("Wednesday, 25 November 2026"), today and
//     the chosen day are marked, days outside min/max are announced as not
//     available and cannot be chosen;
//   - the heading with month and year is a live region, so a change of
//     month is announced;
//   - keys: arrows (day, week), Home/End (start/end of the week), Page
//     Up/Down (month), Shift + Page Up/Down (year), Enter/Space (choose),
//     Escape (close without a change). The focus returns to the button.
// Typing into the field stays possible; the field keeps its value format
// (YYYY-MM-DD) and gets input and change events like after typing.

import { h, uid } from '../core/dom';
import { t } from '../core/i18n';
import { state } from '../core/state';
import { addMonths, compare, inRange, iso, monthGrid, moveByKey, parseISO, type Day } from '../core/calendarmath';
import { iconURL } from '../core/icons';
import { button, openDialog } from './ui';

const today = (): Day => { const n = new Date(); return { y: n.getFullYear(), m: n.getMonth() + 1, d: n.getDate() }; };
const asDate = (x: Day) => new Date(Date.UTC(x.y, x.m - 1, x.d));
const fmt = (opts: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat(state.lang || 'en', { ...opts, timeZone: 'UTC' });

/** "Wednesday, 25 November 2026" in the language of the page. */
export const longDate = (x: Day): string => fmt({ weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' }).format(asDate(x));
const monthTitle = (x: Day): string => fmt({ month: 'long', year: 'numeric' }).format(asDate(x));

/** Names of the weekdays, Monday first: [short, long]. */
function weekdayNames(): [string, string][] {
  const out: [string, string][] = [];
  for (let i = 0; i < 7; i++) {
    const d = asDate({ y: 2024, m: 1, d: 1 + i }); // 1 January 2024 was a Monday
    out.push([fmt({ weekday: 'short' }).format(d), fmt({ weekday: 'long' }).format(d)]);
  }
  return out;
}

/** Opens the picker for input; resolves when it is closed. */
export function openDatePicker(input: HTMLInputElement): void {
  const min = parseISO(input.min), max = parseISO(input.max);
  const chosen = parseISO(input.value);
  let focus: Day = chosen ?? today();
  if (!inRange(focus, min, max)) focus = min && compare(focus, min) < 0 ? min : max ?? focus;
  const live = h('h3', { class: 'ew-dp-month', 'aria-live': 'polite', id: uid('dp') });
  const grid = h('table', { class: 'ew-dp-grid', role: 'grid', 'aria-labelledby': live.id });
  const names = weekdayNames();
  const head = h('thead', null, h('tr', null, ...names.map(([s, l]) => h('th', { scope: 'col', abbr: l }, h('span', { 'aria-hidden': 'true' }, s), h('span', { class: 'sr-only' }, l)))));
  const body = h('tbody');
  grid.append(head, body);

  const choose = (x: Day) => {
    if (!inRange(x, min, max)) return;
    input.value = iso(x);
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
    handle.close();
  };
  const draw = (moveFocus: boolean) => {
    const now = today();
    live.textContent = monthTitle(focus);
    body.replaceChildren(...monthGrid(focus).map((row) => h('tr', null, ...row.map(({ day, inMonth }) => {
      const ok = inRange(day, min, max);
      const isFocus = compare(day, focus) === 0;
      let label = longDate(day);
      if (compare(day, now) === 0) label += `, ${t('datepicker.today')}`;
      if (!ok) label += `, ${t('datepicker.unavailable')}`;
      const cell = h('td', {
        class: ['ew-dp-day', !inMonth && 'ew-dp-day--other', !ok && 'ew-dp-day--off', compare(day, now) === 0 && 'ew-dp-day--today'],
        'data-date': iso(day), tabindex: isFocus ? '0' : '-1', 'aria-label': label,
        'aria-selected': chosen && compare(day, chosen) === 0 ? 'true' : 'false',
        'aria-disabled': ok ? undefined : 'true',
        'aria-current': compare(day, now) === 0 ? 'date' : undefined,
      }, String(day.d));
      cell.addEventListener('click', () => choose(day));
      return cell;
    }))));
    if (moveFocus) body.querySelector<HTMLElement>('[tabindex="0"]')?.focus();
  };
  const go = (to: Day) => { focus = to; draw(true); };
  grid.addEventListener('keydown', (ev) => {
    const to = moveByKey(focus, ev.key, ev.shiftKey);
    if (to) { ev.preventDefault(); go(to); return; }
    if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); choose(focus); }
  });
  const nav = (label: string, text: string, by: number) => button(h('span', { 'aria-hidden': 'true' }, text), () => go(addMonths(focus, by)), { variant: 'subtle', ariaLabel: label });
  const content = h('div', { class: 'ew-dp' },
    h('div', { class: 'ew-dp-nav' },
      nav(t('datepicker.prev_year'), '«', -12), nav(t('datepicker.prev_month'), '‹', -1),
      live,
      nav(t('datepicker.next_month'), '›', 1), nav(t('datepicker.next_year'), '»', 12)),
    grid,
    h('p', { class: 'ew-help ew-dp-keys' }, t('datepicker.keys')));
  const handle = openDialog(t('datepicker.title'), content, [button(t('common.cancel'), () => handle.close())]);
  draw(true);
}

/** Adds the button "Choose date" after a date field (once). */
export function enhanceDateInput(input: HTMLInputElement): void {
  if (input.dataset.ewDatepicker || input.closest('[contenteditable="true"]')) return;
  input.dataset.ewDatepicker = '1';
  const btn = h('button', { type: 'button', class: 'ew-btn ew-btn--subtle ew-dp-open' },
    h('span', { class: 'ew-icon', 'aria-hidden': 'true', style: `--ew-icon: url("${iconURL('calendar')}")` })) as HTMLButtonElement;
  const name = () => {
    const d = parseISO(input.value);
    const label = input.labels?.[0]?.textContent?.replace(/\s*\*\s*$/, '').trim();
    const what = label ? t('datepicker.open_for', { field: label }) : t('datepicker.open');
    btn.setAttribute('aria-label', d ? `${what}, ${t('datepicker.chosen', { date: longDate(d) })}` : what);
  };
  name();
  input.addEventListener('change', name);
  input.addEventListener('input', name);
  btn.disabled = input.disabled || input.readOnly;
  btn.addEventListener('click', () => openDatePicker(input));
  input.classList.add('ew-dp-input');
  input.insertAdjacentElement('afterend', btn);
}

let observer: MutationObserver | null = null;
let pending = 0;

/** Gives every date field of the page the picker, also those added later. */
export function watchDateInputs(): void {
  if (observer) return;
  const run = () => document.querySelectorAll<HTMLInputElement>('input[type="date"]:not([data-ew-datepicker])').forEach(enhanceDateInput);
  observer = new MutationObserver((list) => {
    // typing in an editor changes the page all the time: not a reason
    if (pending || list.every((m) => (m.target instanceof Element ? m.target : m.target.parentElement)?.closest('[contenteditable=true]'))) return;
    pending = requestAnimationFrame(() => { pending = 0; run(); });
  });
  observer.observe(document.body, { childList: true, subtree: true });
  run();
}
