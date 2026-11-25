// Contrast of templates of data: the designer computes the same ratios as
// the server (plugins/core_template/testdata/contrast.json, written by the Go
// test TestContrastShared) and its suggestions always pass.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { checkPairs, contrast, darkFrom, PAIRS, suggest } from '../../../plugins/core_template/web/contrast';
import { cssSafe, schemeOf } from '../../../plugins/core_template/web/room';
import type { Design } from '../../../plugins/core_template/web/engine';
import cms from '../../../plugins/core_template/examples/cms.json';
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

test('dark colors: the first dark palette of a design passes every pair', () => {
  const lights = [wiki.tokens, cms.tokens,
    { ...wiki.tokens, primary: '#000000', link: '#000080', focus: '#202020' },
    { ...wiki.tokens, primary: '#ffcc00', link: '#ffcc00', focus: '#ffcc00' }];
  for (const l of lights) {
    const d = darkFrom(l);
    for (const r of checkPairs(d)) assert.ok(r.ok, `${l.primary}: ${r.pair.fore} on ${r.pair.back} ${r.value.toFixed(2)}`);
  }
});

test('dark colors: only a dark palette of a safe form gets into the CSS; the scheme of the design', () => {
  const base = structuredClone(wiki) as unknown as Design;
  assert.equal(schemeOf(base), '');
  const dark = structuredClone(base);
  dark.tokens.dark = darkFrom(dark.tokens);
  assert.equal(cssSafe(dark), true);
  assert.equal(schemeOf(dark), 'system');
  dark.options.color_scheme = 'dark';
  assert.equal(schemeOf(dark), 'dark');
  // nothing but hex colors gets into the CSS (designCSS writes nothing then)
  const bad = structuredClone(dark);
  (bad.tokens.dark as unknown as Record<string, string>).text = 'red; } body { display: none';
  assert.equal(cssSafe(bad), false);
  const missing = structuredClone(dark);
  delete (missing.tokens.dark as unknown as Record<string, string>).link;
  assert.equal(cssSafe(missing), false);
});
