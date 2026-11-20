// German search for icons: the words of the language find the icons, the
// categories and icons have German names, and the terms cover the set.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { findIcons, iconLabel, type IconEntry, type IconTerms } from '../../src/core/icons';
import de from '../../src/icons/terms/de.json';

const terms = de as unknown as IconTerms;
const all: IconEntry[] = [
  { n: 'arrow-left', c: 'Arrows', t: ['back', 'previous'], s: 'o' },
  { n: 'clock', c: 'System', t: ['time', 'watch'], s: 'of' },
  { n: 'trash', c: 'System', t: ['garbage', 'delete', 'remove', 'bin'], s: 'o' },
  { n: 'building-castle', c: 'Buildings', t: ['fortress'], s: 'o' },
];
const names = (q: string, tr: IconTerms | null = terms) => findIcons(all, q, 'outline', '', 50, tr).list.map((e) => e.n);

test('German words find the icons, English still works', () => {
  assert.deepEqual(names('Pfeil'), ['arrow-left']);
  assert.deepEqual(names('uhr'), ['clock']);
  assert.deepEqual(names('löschen'), ['trash']);
  assert.deepEqual(names('Burg'), ['building-castle']);
  assert.deepEqual(names('pfeil links'), ['arrow-left'], 'every word must fit');
  assert.deepEqual(names('loschen'), ['trash'], 'without umlauts too');
  assert.deepEqual(names('Gebäude'), ['building-castle'], 'by the German name of the category');
  assert.deepEqual(names('clock'), ['clock']);
  assert.deepEqual(names('Pfeil', null), [], 'without terms only English');
});

test('icons are named in German', () => {
  assert.equal(iconLabel(all[0], terms), 'Pfeil links');
  assert.equal(iconLabel(all[0], null), '');
});

test('the terms cover the icon set', () => {
  const meta = JSON.parse(fs.readFileSync(path.join(process.cwd(), 'node_modules', '@tabler', 'icons', 'icons.json'), 'utf8')) as Record<string, { category?: string; tags?: unknown[] }>;
  const words = new Set<string>();
  const cats = new Set<string>();
  let icons = 0, covered = 0;
  for (const [n, m] of Object.entries(meta)) {
    if (m.category) cats.add(m.category);
    for (const w of n.split('-')) words.add(w);
    for (const tag of m.tags ?? []) for (const w of String(tag).split(/[-\s]/)) words.add(w);
    if (m.category === 'Brand') continue;
    icons++;
    if (n.split('-').some((w) => terms.words[w])) covered++;
  }
  for (const c of cats) assert.ok(terms.categories[c], `category without a German name: ${c}`);
  assert.ok(covered / icons >= 0.9, `only ${covered} of ${icons} icons have a German word`);
  for (const [w, tr] of Object.entries(terms.words)) {
    assert.ok(words.has(w), `word not in the icon set (old?): ${w}`);
    assert.ok(Array.isArray(tr) && tr.length && tr.every((x) => typeof x === 'string' && x.trim()), `empty translation: ${w}`);
  }
});
