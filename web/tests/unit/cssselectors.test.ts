// Selectors that browsers drop: a rule with :has() inside :has() is invalid
// as a whole (Selectors Level 4), so nothing of it applies, without any
// warning. Found 23.11.2026: the button of an empty drawer was never hidden.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

/** the places of :has() that contain another :has() */
export function nestedHas(css: string): number[] {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, (c) => ' '.repeat(c.length));
  const found: number[] = [];
  for (let i = text.indexOf(':has('); i >= 0; i = text.indexOf(':has(', i + 1)) {
    let depth = 0;
    for (let j = i + 4; j < text.length; j++) {
      if (text[j] === '(') depth++;
      else if (text[j] === ')' && --depth === 0) {
        if (text.slice(i + 5, j).includes(':has(')) found.push(i);
        break;
      }
    }
  }
  return found;
}

function cssFiles(dir: string): string[] {
  if (!fs.existsSync(dir)) return [];
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) return e.name === 'node_modules' || e.name === 'generated' ? [] : cssFiles(p);
    return e.name.endsWith('.css') ? [p] : [];
  });
}

test('the rule finds :has() inside :has()', () => {
  assert.deepEqual(nestedHas('.a:has(+ .b:not(:has(.c))) { x: 1 }'), [2]);
  assert.deepEqual(nestedHas('.a:not(:has(+ .b .c:not(:empty))) { x: 1 }'), []);
  assert.deepEqual(nestedHas('/* .a:has(:has(.b)) */ .a { x: 1 }'), []);
});

test('no stylesheet of the wiki or its plugins has :has() inside :has()', () => {
  // the unit tests run in web/
  const files = [...cssFiles('src'), ...cssFiles('../plugins')];
  assert.ok(files.length > 5, `stylesheets found: ${files.length}`);
  for (const f of files) {
    const css = fs.readFileSync(f, 'utf8');
    const at = nestedHas(css);
    assert.deepEqual(at.map((i) => css.slice(i, i + 80)), [], `${f}: :has() inside :has() makes the whole rule invalid`);
  }
});
