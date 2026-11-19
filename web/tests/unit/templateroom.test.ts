// Room of templates of data: the engine of the browser computes the same
// widths as the server (plugins/core_template/testdata/room.json, written by
// the Go test TestRoomShared), so the designer never allows what the server
// refuses and never refuses what it allows.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { cssSafe, needRoom, positionsRoom, room } from '../../../plugins/core_template/web/room';
import type { Design, ViewName } from '../../../plugins/core_template/web/engine';
import wiki from '../../../plugins/core_template/examples/wiki.json';
import cms from '../../../plugins/core_template/examples/cms.json';
import shared from '../../../plugins/core_template/testdata/room.json';

const designs: Record<string, Design> = { wiki: wiki as unknown as Design, cms: cms as unknown as Design };

test('the engine computes the same room as the server', () => {
  for (const [name, cases] of Object.entries(shared as Record<string, { row: string; view: ViewName; need: number; room: number; positions: Record<string, number> }[]>)) {
    const d = designs[name];
    assert.ok(d, name);
    assert.ok(cases.length > 0, name);
    for (const c of cases) {
      const r = d.rows.find((x) => x.id === c.row);
      assert.ok(r, `${name}: row ${c.row}`);
      const v = c.view === 'desktop' ? undefined : r[c.view];
      assert.equal(needRoom(r, c.view, v ?? undefined), c.need, `${name} ${c.row} ${c.view}: need`);
      assert.equal(room(d, r, c.view), c.room, `${name} ${c.row} ${c.view}: room`);
      for (const b of r.blocks) assert.equal(positionsRoom(b), c.positions[b.id], `${name} ${c.row} ${b.id}: positions`);
    }
  }
});

test('positions side by side need room, one below the other do not', () => {
  const b = { id: 'x', kind: 'area', role: 'none', width: 0, positions: ['a', 'b', 'c'], layout: 'row', align: 'start' } as Design['rows'][number]['blocks'][number];
  assert.equal(positionsRoom(b), 3 * 4 + 2 * 0.5);
  assert.equal(positionsRoom({ ...b, layout: 'column' }), 0);
  assert.equal(positionsRoom({ ...b, positions: ['a'] }), 0);
});

test('only values of a safe form go into the CSS (also for drafts not saved yet)', () => {
  for (const d of Object.values(designs)) assert.ok(cssSafe(d));
  const bad = (f: (d: Design) => void) => {
    const d = structuredClone(designs.wiki);
    f(d);
    return cssSafe(d);
  };
  assert.equal(bad((d) => { d.tokens.bg = 'red;} body{display:none'; }), false);
  assert.equal(bad((d) => { d.rows[0].id = 'x"] , body{'; }), false);
  assert.equal(bad((d) => { d.rows[0].blocks[0].id = 'Top Bar'; }), false);
  assert.equal(bad((d) => { d.rows[0].blocks[0].positions = ['ok', '}'] }), false);
  assert.equal(bad((d) => { (d.tokens as unknown as Record<string, unknown>).scale = '100%;}' ; }), false);
  assert.equal(bad((d) => { d.tokens.logo = 'https://example.com/x.png'; }), false);
  assert.equal(bad((d) => { d.rows[2].mobile = { stack: true, widths: { 'a}': 3 } }; }), false);
  assert.equal(bad((d) => { (d as unknown as Record<string, unknown>).rows = 'x'; }), false);
});
