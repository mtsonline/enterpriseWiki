// Date arithmetic for the date picker, without the DOM (unit-tested).
// Dates are plain calendar days "YYYY-MM-DD"; all arithmetic is in UTC so
// that daylight saving time never shifts a day.

export interface Day { y: number; m: number; d: number } // m: 1–12

const pad = (n: number, w = 2) => String(n).padStart(w, '0');

/** "YYYY-MM-DD" of a day. */
export const iso = (x: Day): string => `${pad(x.y, 4)}-${pad(x.m)}-${pad(x.d)}`;

/** The day of "YYYY-MM-DD", or null for anything else (also 2026-02-30). */
export function parseISO(s: string | null | undefined): Day | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s ?? '');
  if (!m) return null;
  const x = { y: +m[1], m: +m[2], d: +m[3] };
  return iso(addDays(x, 0)) === s ? x : null;
}

const toUTC = (x: Day) => Date.UTC(x.y, x.m - 1, x.d);
const fromUTC = (t: number): Day => { const d = new Date(t); return { y: d.getUTCFullYear(), m: d.getUTCMonth() + 1, d: d.getUTCDate() }; };

/** The day n days later (n may be negative). */
export const addDays = (x: Day, n: number): Day => fromUTC(toUTC(x) + n * 86400000);

/** The same day n months later, kept inside the month (31 Jan + 1 → 28/29 Feb). */
export function addMonths(x: Day, n: number): Day {
  const first = new Date(Date.UTC(x.y, x.m - 1 + n, 1));
  const y = first.getUTCFullYear(), m = first.getUTCMonth() + 1;
  return { y, m, d: Math.min(x.d, daysInMonth(y, m)) };
}

export const daysInMonth = (y: number, m: number): number => new Date(Date.UTC(y, m, 0)).getUTCDate();

/** Day of the week, 0 = Monday … 6 = Sunday. */
export const weekday = (x: Day): number => (new Date(toUTC(x)).getUTCDay() + 6) % 7;

/** -1, 0 or 1. */
export const compare = (a: Day, b: Day): number => Math.sign(toUTC(a) - toUTC(b));

/**
 * The weeks of the month of x as rows of 7 days, Monday first; days of the
 * neighbouring months fill the first and the last row (inMonth false).
 */
export function monthGrid(x: Day): { day: Day; inMonth: boolean }[][] {
  const first = { y: x.y, m: x.m, d: 1 };
  let cur = addDays(first, -weekday(first));
  const rows: { day: Day; inMonth: boolean }[][] = [];
  do {
    const row = [];
    for (let i = 0; i < 7; i++) {
      row.push({ day: cur, inMonth: cur.m === x.m && cur.y === x.y });
      cur = addDays(cur, 1);
    }
    rows.push(row);
  } while (cur.m === x.m && cur.y === x.y);
  return rows;
}

/** Where a key moves the focus in the grid (null: not a key of the grid). */
export function moveByKey(x: Day, key: string, shift: boolean): Day | null {
  switch (key) {
    case 'ArrowLeft': return addDays(x, -1);
    case 'ArrowRight': return addDays(x, 1);
    case 'ArrowUp': return addDays(x, -7);
    case 'ArrowDown': return addDays(x, 7);
    case 'Home': return addDays(x, -weekday(x));
    case 'End': return addDays(x, 6 - weekday(x));
    case 'PageUp': return addMonths(x, shift ? -12 : -1);
    case 'PageDown': return addMonths(x, shift ? 12 : 1);
    default: return null;
  }
}

/** Whether x lies within min and max (each may be missing). */
export const inRange = (x: Day, min: Day | null, max: Day | null): boolean =>
  (!min || compare(x, min) >= 0) && (!max || compare(x, max) <= 0);
