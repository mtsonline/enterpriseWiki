// The settings of a template of data in the browser: their values become
// the tokens of the design (the server checked them; a value of another
// form is still left out, so nothing else can reach the CSS).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { withSettings } from '../../../plugins/core_template/web/settings';
import { cssSafe } from '../../../plugins/core_template/web/room';
import type { Design } from '../../../plugins/core_template/web/engine';
import wiki from '../../../plugins/core_template/examples/wiki.json';

const design = wiki as unknown as Design;

test('the values of the settings become the tokens of the design', () => {
  const d = withSettings(design, { primary: '#1a3a6b', font: 'serif', scale: 110, radius: 2, content_width: 70 });
  assert.equal(d.tokens.primary, '#1a3a6b');
  assert.equal(d.tokens.font, 'serif');
  assert.deepEqual([d.tokens.scale, d.tokens.radius, d.tokens.content_width], [110, 2, 70]);
  assert.notEqual(design.tokens.primary, '#1a3a6b', 'the design itself stays as it is');
  assert.deepEqual(withSettings(design, undefined), design);
  assert.deepEqual(withSettings(design, {}).tokens, design.tokens);
});

test('values of another form are left out', () => {
  const d = withSettings(design, { primary: 'red;}body{display:none', scale: 'big', radius: Infinity, text: '#12345' });
  assert.deepEqual(d.tokens, design.tokens);
  assert.equal(withSettings(design, { font: 'x;}' }).tokens.font, design.tokens.font);
  assert.ok(cssSafe(withSettings(design, { primary: '#1a3a6b', font: 'serif' })));
});

test('the dark colours go into the dark palette only', () => {
  const dark = { ...design, tokens: { ...design.tokens, dark: { ...design.tokens } } } as Design;
  const d = withSettings(dark, { dark_primary: '#abcdef' });
  assert.equal(d.tokens.dark?.primary, '#abcdef');
  assert.equal(d.tokens.primary, design.tokens.primary);
  assert.equal(withSettings(design, { dark_primary: '#abcdef' }).tokens.dark, undefined, 'no dark colours without a dark palette');
});
