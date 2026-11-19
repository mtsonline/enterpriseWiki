// Contrast of templates of data: the designer computes the same ratios as
// the server (plugins/core_template/testdata/contrast.json, written by the Go
// test TestContrastShared) and its suggestions always pass.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { checkPairs, contrast, PAIRS, suggest } from '../../../plugins/core_template/web/contrast';
import shared from '../../../plugins/core_template/testdata/contrast.json';
import wiki from '../../../plugins/core_template/examples/wiki.json';

test('the designer computes the same contrast as the server', () => {
  assert.ok(shared.length > 10);
  for (const c of shared) assert.ok(Math.abs(contrast(c.a, c.b) - c.ratio) < 1e-5, `${c.a} on ${c.b}: ${contrast(c.a, c.b)} vs ${c.ratio}`);
});

test('the shipped example passes every pair', () => {
  assert.ok(PAIRS.length >= 10);
  for (const r of checkPairs(wiki.tokens)) assert.ok(r.ok, `${r.pair.fore} on ${r.pair.back}`);
});

test('contrast help: the suggestion passes and stays near the color', () => {
  for (const [fore, back, min] of [['#bbbbbb', '#ffffff', 4.5], ['#3355ff', '#1a1a2e', 4.5], ['#ffcc00', '#ffffff', 3], ['#777777', '#808080', 4.5]] as const) {
    const s = suggest(fore, back, min);
    if (s === null) {
      assert.ok(contrast('#000000', back) < min && contrast('#ffffff', back) < min, `${fore} on ${back}: a color exists`);
      continue;
    }
    assert.ok(contrast(s, back) >= min, `${s} on ${back}`);
  }
  assert.equal(suggest('#000000', '#ffffff', 4.5), '#000000');
});
