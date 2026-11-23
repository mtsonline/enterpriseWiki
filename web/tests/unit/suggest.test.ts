// Suggestions of blocks while typing and the Markdown of a task item.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { suggestItems, suggestQuery } from '../../src/editor/suggest';
import { taskMarkdown } from '../../src/editor/taskdialog';
import type { MacroInfo } from '../../src/core/types';

test('what was typed for a suggestion', () => {
  assert.deepEqual(suggestQuery('/'), { length: 1, query: '' });
  assert.deepEqual(suggestQuery('  /dat'), { length: 4, query: 'dat' });
  assert.deepEqual(suggestQuery('Due :da'), { length: 3, query: 'da' });
  assert.deepEqual(suggestQuery('::tas'), { length: 5, query: 'tas' });
  assert.deepEqual(suggestQuery(':::pan'), { length: 6, query: 'pan' });
  assert.equal(suggestQuery('and/or'), null, 'a slash inside the text');
  assert.equal(suggestQuery('Time 10:30'), null, 'a colon after a number');
  assert.equal(suggestQuery('Note: '), null, 'a colon at the end of a word');
  assert.equal(suggestQuery('see http://x'), null);
  assert.equal(suggestQuery('/a  b'), null, 'two spaces end it');
});

const m = (name: string, en: string, de: string): MacroInfo => ({ name, plugin: 'core.macros', labels: { en, de }, description: {}, has_body: false, params: [] });

test('blocks matching the typed name, the task item and all blocks', () => {
  const list = [m('date', 'Date', 'Datum'), m('decisions', 'Report of decisions', 'Entscheidungs-Bericht'), m('tasks', 'Report of tasks', 'Aufgaben-Bericht'), m('panel', 'Panel', 'Hinweisbox')];
  const ids = (q: string) => suggestItems(list, q).map((x) => x.id);
  assert.deepEqual(ids('da'), ['m-date', 'all']);
  assert.deepEqual(ids('task'), ['task', 'm-tasks', 'all']);
  assert.deepEqual(ids('report'), ['m-decisions', 'm-tasks', 'all']);
  assert.equal(ids('').length, list.length + 2);
  assert.deepEqual(ids('zzz'), ['all']);
});

test('a task item: text stays text, people and the date', () => {
  assert.equal(taskMarkdown('Check the offer', ['anna@local', 'bob@ldap'], '2026-11-30'), '- [ ] Check the offer @anna @bob@ldap :date{value=2026-11-30}');
  assert.equal(taskMarkdown('- [x] *Buy* :date{value=1} [link]', [], ''), '- [ ] \\*Buy\\* \\:date\\{value=1\\} \\[link\\]');
  assert.equal(taskMarkdown('Line\nbreak', ['anna@local'], 'not a day'), '- [ ] Line break @anna');
});
