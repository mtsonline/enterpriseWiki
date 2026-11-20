// Lists the English words of the icon set that have no search words in a
// language yet, the most used first: the gaps to fill in
// src/icons/terms/<lang>.json (key: the English word, value: the words of
// the language). Brand icons are left out (their names are names).
//
//   node tools/iconterms.mjs de            the 100 most used missing words
//   node tools/iconterms.mjs de --all      all of them
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const lang = process.argv[2];
if (!lang || !/^[a-z]{2,3}$/.test(lang)) {
  console.error('usage: node tools/iconterms.mjs LANG [--all]');
  process.exit(2);
}
const meta = JSON.parse(fs.readFileSync(path.join(here, 'node_modules', '@tabler', 'icons', 'icons.json'), 'utf8'));
const file = path.join(here, 'src', 'icons', 'terms', `${lang}.json`);
const terms = fs.existsSync(file) ? JSON.parse(fs.readFileSync(file, 'utf8')) : { categories: {}, words: {} };
const count = new Map();
let icons = 0, covered = 0;
for (const [name, m] of Object.entries(meta)) {
  if (m.category === 'Brand') continue;
  icons++;
  const parts = name.split('-').filter((w) => w.length > 1 && !/^\d+$/.test(w));
  if (parts.some((w) => terms.words[w])) covered++;
  for (const w of new Set(parts)) if (!terms.words[w]) count.set(w, (count.get(w) ?? 0) + 1);
}
const cats = [...new Set(Object.values(meta).map((m) => m.category).filter(Boolean))].filter((c) => !terms.categories[c]);
const missing = [...count].sort((a, b) => b[1] - a[1]);
console.log(`${lang}: ${covered} of ${icons} icons (${Math.round((covered / icons) * 100)} %) can be found by a word of their name; ${missing.length} words missing.`);
if (cats.length) console.log(`categories without a name: ${cats.join(', ')}`);
for (const [w, n] of process.argv.includes('--all') ? missing : missing.slice(0, 100)) console.log(`${w}\t${n}`);
