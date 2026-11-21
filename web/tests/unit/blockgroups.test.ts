// The menu "Insert block": groups in their order and the search.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { BLOCK_GROUPS, fold, groupMacros, matches } from '../../src/editor/blockgroups';
import type { MacroInfo } from '../../src/core/types';

const m = (name: string, group?: string): MacroInfo => ({ name, plugin: 'x', labels: { en: name }, description: {}, params: null, has_body: false, group });

test('groups in the order of the server, empty ones left out, unknown in "other"', () => {
  const g = groupMacros([m('tabs', 'layout'), m('panel', 'text'), m('accordion', 'layout'), m('odd', 'nonsense'), m('plain')]);
  assert.deepEqual(g.map((x) => x.group), ['text', 'layout', 'other']);
  assert.deepEqual(g[1].macros.map((x) => x.name), ['accordion', 'tabs']);
  assert.deepEqual(g[2].macros.map((x) => x.name), ['odd', 'plain']);
  // the same groups as the server (content.MacroGroups)
  assert.deepEqual(BLOCK_GROUPS, ['text', 'layout', 'navigation', 'media', 'lists', 'data', 'forms', 'automation', 'other']);
});

test('search: every word, without case and accents', () => {
  const s = fold('Überschrift mit wechselnden Wörtern rotating');
  assert.ok(matches(s, 'uberschrift'));
  assert.ok(matches(s, 'WÖRTERN rot'));
  assert.ok(!matches(s, 'wörtern tabelle'));
  assert.ok(matches(s, '   '));
});
