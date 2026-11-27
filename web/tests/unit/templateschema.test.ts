// The media queries of the schematic of templates: the breakpoints of the
// templates read for a size of window, as the browser does.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DEVICES, deviceSize, mediaMatches } from '../../src/core/templateSchema';

const COMPACT = '(max-width: 1100px), (max-height: 520px)';

test('the compact query of the templates on each device', () => {
  const got = Object.fromEntries(DEVICES.map((d) => [d.id, mediaMatches(COMPACT, deviceSize(d.id))]));
  assert.deepEqual(got, { desktop: false, tablet: true, phone: true, 'phone-landscape': true });
  assert.equal(mediaMatches(COMPACT, { width: 1101, height: 521 }), false);
  assert.equal(mediaMatches(COMPACT, { width: 1101, height: 520 }), true);
});

test('and, or, not, parentheses, orientation and rem', () => {
  const portrait = '((max-width: 1100px) or (max-height: 520px)) and (orientation: portrait)';
  assert.equal(mediaMatches(portrait, { width: 390, height: 844 }), true);
  assert.equal(mediaMatches(portrait, { width: 844, height: 390 }), false);
  assert.equal(mediaMatches('(min-width: 1101px) and (min-height: 521px)', { width: 1280, height: 800 }), true);
  assert.equal(mediaMatches('(max-width: 48rem)', { width: 768, height: 1000 }), true);
  assert.equal(mediaMatches('(max-width: 48rem)', { width: 769, height: 1000 }), false);
  assert.equal(mediaMatches('not (min-width: 640px)', { width: 390, height: 800 }), true);
  assert.equal(mediaMatches('screen and (min-width: 640px)', { width: 820, height: 800 }), true);
});

test('what it does not understand does not apply', () => {
  assert.equal(mediaMatches('(hover: hover)', { width: 1280, height: 800 }), false);
  assert.equal(mediaMatches('(max-width: 10px', { width: 5, height: 5 }), false);
  assert.equal(mediaMatches('', { width: 5, height: 5 }), false);
});
