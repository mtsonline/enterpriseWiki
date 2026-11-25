// The fonts of the interface are served by the wiki itself (never from other
// servers), load without blocking the text (font-display: swap) and the ones
// used at once are preloaded, so the layout is the same on every machine.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

const web = process.cwd();
const css = fs.readFileSync(path.join(web, 'src/styles/base.css'), 'utf8');
const faces = [...css.matchAll(/@font-face\s*\{([^}]*)\}/g)].map((m) => m[1]);

test('Inter and Poppins are self-hosted with font-display: swap', () => {
  for (const family of ['Inter Variable', 'Poppins']) {
    const own = faces.filter((f) => f.includes(`font-family: '${family}'`));
    assert.ok(own.length >= 2, `${family}: latin and latin-ext`);
    for (const f of own) {
      assert.match(f, /font-display: swap;/, `${family}: font-display: swap`);
      const url = /src: url\(([^)]+)\)/.exec(f)?.[1] ?? '';
      assert.ok(!/^(https?:)?\/\//.test(url), `${family}: served by the wiki (${url})`);
      assert.ok(fs.existsSync(path.join(web, 'src/styles', url)), `${family}: ${url} exists`);
    }
  }
  // Poppins only in bold: the headings need nothing else (13 KB)
  assert.ok(faces.filter((f) => f.includes("'Poppins'")).every((f) => /font-weight: 700;/.test(f)));
});

test('Inter is the default font, Poppins the heading font of the wiki template only', () => {
  assert.match(css, /--ew-font: "Inter Variable", Inter, system-ui,/);
  assert.match(css, /--ew-heading-font: Poppins, var\(--ew-font\);/);
  // the lowest weight: a template or a design with its own heading font wins
  assert.match(css, /:where\(body:has\(\.ewc-app\)\) :is\(h1, h2, h3, h4\) \{ font-family: var\(--ew-heading-font\); \}/);
});

test('the fonts used at once are preloaded and named in the third-party notices', () => {
  const build = fs.readFileSync(path.join(web, 'build.mjs'), 'utf8');
  assert.ok(build.includes('inter-latin-wght-normal') && build.includes('poppins-latin-700-normal'), 'preload in build.mjs');
  const notices = fs.readFileSync(path.join(web, '../tools/notices/main.go'), 'utf8');
  for (const f of ['font-inter.txt', 'font-poppins.txt']) {
    assert.ok(notices.includes(`static/${f}`), f);
    assert.match(fs.readFileSync(path.join(web, '../tools/notices/static', f), 'utf8'), /SIL Open Font License, Version 1\.1/);
  }
});
