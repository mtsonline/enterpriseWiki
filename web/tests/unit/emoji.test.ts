// The search of the emoji dialog: every word in the name or the keywords,
// without regard to case and accents (German umlauts and ß).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fold, searchEmoji } from '../../src/editor/emoji';

const list: [string, string, string, number][] = [
  ['👍', 'Daumen hoch', 'Daumen gut Hand', 1],
  ['❤️', 'rotes Herz', 'Herz Liebe', 7],
  ['🍺', 'Bierkrug', 'Bier Krug trinken', 3],
  ['🍣', 'Sushi', 'Fisch', 3],
  ['🧑‍🦰', 'Person: rotes Haar', 'Person Haar', 1],
];

test('fold', () => {
  assert.equal(fold('Größe'), 'grosse');
  assert.equal(fold('ÄÖÜ'), 'aou');
});

test('search', () => {
  assert.deepEqual(searchEmoji(list, 'daumen').map((e) => e[0]), ['👍']);
  assert.deepEqual(searchEmoji(list, 'ROT').map((e) => e[0]), ['❤️', '🧑‍🦰']);
  assert.deepEqual(searchEmoji(list, 'rot herz').map((e) => e[0]), ['❤️']);
  assert.deepEqual(searchEmoji(list, 'trink').map((e) => e[0]), ['🍺']);
  assert.deepEqual(searchEmoji(list, '   '), []);
  assert.equal(searchEmoji(list, 'e', 2).length, 2);
});
