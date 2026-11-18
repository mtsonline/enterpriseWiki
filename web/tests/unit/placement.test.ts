// Resolution of placements: kinds of positions and the level of one page.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { kindOf, resolvePlacements, type BlockDefault } from '../../src/core/placement';

const blocks: BlockDefault[] = [
  { id: 'core.search', position: 'header-tools', order: 10, kind: 'site' },
  { id: 'core.element.title', position: 'element-header', order: 10, kind: 'element' },
  { id: 'core.share', position: 'element-toolbar', order: 120, kind: 'element' },
];

test('a level of one page arranges only element blocks at element positions', () => {
  const r = resolvePlacements([
    { level: 'instance', target: '7', placements: [
      { block: 'core.search', position: '', sort: 0 },
      { block: 'core.element.title', position: '', sort: 0 },
      { block: 'core.share', position: 'element-footer', sort: 5 },
    ] },
  ], blocks);
  assert.deepEqual(r.get('core.search'), [{ position: 'header-tools', sort: 10 }], 'a page cannot hide site blocks');
  assert.deepEqual(r.get('core.element.title'), [], 'title hidden');
  assert.deepEqual(r.get('core.share'), [{ position: 'element-footer', sort: 5 }]);
});

test('a position of the wrong kind is ignored', () => {
  const r = resolvePlacements([
    { level: 'site', target: '', placements: [
      { block: 'core.search', position: 'element-footer', sort: 1 },
      { block: 'core.share', position: 'sidebar', sort: 1 },
    ] },
  ], blocks);
  assert.deepEqual(r.get('core.search'), []);
  assert.deepEqual(r.get('core.share'), []);
});

test('required on the site: lower levels (also one page) keep it', () => {
  const r = resolvePlacements([
    { level: 'site', target: '', placements: [{ block: 'core.share', position: 'element-header', sort: 1, required: true }] },
    { level: 'instance', target: '7', placements: [{ block: 'core.share', position: '', sort: 0 }] },
  ], blocks);
  assert.deepEqual(r.get('core.share'), [{ position: 'element-header', sort: 1 }]);
});

test('editor blocks fit only the positions of the editing page', () => {
  const ed: BlockDefault[] = [{ id: 'core.editor.tags', position: 'editor-footer', order: 10, kind: 'editor' }];
  const r = resolvePlacements([
    { level: 'site', target: '', placements: [{ block: 'core.editor.tags', position: 'element-aside', sort: 1 }] },
    { level: 'category', target: '3', placements: [{ block: 'core.editor.tags', position: 'editor-aside', sort: 1 }] },
  ], ed);
  assert.deepEqual(r.get('core.editor.tags'), [{ position: 'editor-aside', sort: 1 }]);
});

test('one category page arranges only category blocks, at category positions', () => {
  const bl: BlockDefault[] = [
    { id: 'core.category.title', position: 'category-header', order: 10, kind: 'category' },
    { id: 'core.search', position: 'header-tools', order: 10, kind: 'site' },
  ];
  const r = resolvePlacements([
    { level: 'category_page', target: '4', placements: [
      { block: 'core.category.title', position: 'category-aside', sort: 5 },
      { block: 'core.search', position: 'footer', sort: 1 },
    ] },
  ], bl);
  assert.deepEqual(r.get('core.category.title'), [{ position: 'category-aside', sort: 5 }]);
  assert.deepEqual(r.get('core.search'), [{ position: 'header-tools', sort: 10 }]);
  assert.equal(kindOf('category-content'), 'category');
  assert.equal(kindOf('element-content'), 'element');
});
