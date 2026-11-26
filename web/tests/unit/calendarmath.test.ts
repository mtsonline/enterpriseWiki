// The arithmetic of the date picker: valid days only, months that keep
// their day inside the month, weeks from Monday, the keys of the grid and
// the range of min and max.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { addDays, addMonths, inRange, iso, monthGrid, moveByKey, parseISO, weekday } from '../../src/core/calendarmath';

const d = (s: string) => parseISO(s)!;

test('only real days are read', () => {
  assert.deepEqual(parseISO('2026-11-25'), { y: 2026, m: 11, d: 25 });
  for (const bad of ['2026-02-30', '2026-13-01', '26-11-25', '2026-11-25T10:00', '', null, undefined]) assert.equal(parseISO(bad), null, String(bad));
  assert.notEqual(parseISO('2028-02-29'), null, 'leap year');
});

test('days and months move across years, a month keeps its day inside', () => {
  assert.equal(iso(addDays(d('2026-12-31'), 1)), '2027-01-01');
  assert.equal(iso(addDays(d('2026-03-01'), -1)), '2026-02-28');
  assert.equal(iso(addMonths(d('2026-01-31'), 1)), '2026-02-28');
  assert.equal(iso(addMonths(d('2028-01-31'), 1)), '2028-02-29');
  assert.equal(iso(addMonths(d('2026-01-15'), -1)), '2025-12-15');
  assert.equal(iso(addMonths(d('2026-11-25'), 12)), '2027-11-25');
});

test('weeks start on Monday and fill whole rows', () => {
  assert.equal(weekday(d('2026-11-23')), 0, 'Monday');
  assert.equal(weekday(d('2026-11-29')), 6, 'Sunday');
  const g = monthGrid(d('2026-11-25'));
  assert.ok(g.every((row) => row.length === 7));
  assert.equal(iso(g[0][0].day), '2026-10-26', 'starts with the Monday before the 1st');
  assert.equal(g[0][0].inMonth, false);
  const days = g.flat().filter((c) => c.inMonth).map((c) => c.day.d);
  assert.deepEqual(days, Array.from({ length: 30 }, (_, i) => i + 1));
  // February 2027 starts on a Monday and has exactly four weeks
  assert.equal(monthGrid(d('2027-02-10')).length, 4);
});

test('keys of the grid', () => {
  const x = d('2026-11-25'); // a Wednesday
  const k = (key: string, shift = false) => iso(moveByKey(x, key, shift)!);
  assert.equal(k('ArrowRight'), '2026-11-26');
  assert.equal(k('ArrowLeft'), '2026-11-24');
  assert.equal(k('ArrowDown'), '2026-12-02');
  assert.equal(k('ArrowUp'), '2026-11-18');
  assert.equal(k('Home'), '2026-11-23');
  assert.equal(k('End'), '2026-11-29');
  assert.equal(k('PageDown'), '2026-12-25');
  assert.equal(k('PageUp', true), '2025-11-25');
  assert.equal(moveByKey(x, 'a', false), null);
});

test('range of min and max', () => {
  assert.ok(inRange(d('2026-11-25'), null, null));
  assert.ok(inRange(d('2026-11-25'), d('2026-11-25'), d('2026-11-25')));
  assert.ok(!inRange(d('2026-11-24'), d('2026-11-25'), null));
  assert.ok(!inRange(d('2026-11-26'), null, d('2026-11-25')));
});
