// Fixed colors in stylesheets: light and dark (and every template) work only
// when components take their colors from the variables (--ew-*). This test
// counts colors written directly into declarations; the list may only shrink
// (csscolors-baseline.json). Allowed everywhere: the variables themselves,
// shadows (dark on any background) and transparent/currentColor/inherit.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import baseline from './csscolors-baseline.json';

const COLOR = /#[0-9a-fA-F]{3,8}\b|\b(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch)\(|\b(?:white|black|red|green|blue|yellow|orange|gray|grey|silver|navy|maroon|purple|teal|olive|lime|aqua|fuchsia)\b/g;
const FREE = new Set(['box-shadow', 'text-shadow']);

/** the fixed colors in declarations of css (outside variables and shadows) */
export function fixedColors(css: string): string[] {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, ' ');
  const found: string[] = [];
  // a declaration: a property name, a colon and a value up to ; or }
  const decl = /([a-zA-Z0-9-]+)\s*:\s*([^;{}]+)(?=[;}])/g;
  for (let m = decl.exec(text); m; m = decl.exec(text)) {
    const prop = m[1].toLowerCase();
    if (prop.startsWith('--') || FREE.has(prop)) continue;
    // a declaration of a custom property is matched without its dashes
    const before = text.slice(Math.max(0, m.index - 2), m.index);
    if (before.endsWith('--')) continue;
    // fallbacks in var(--x, #fff) count too: they are fixed colors
    const value = m[2].replace(/url\([^)]*\)/g, '').replace(/--[a-zA-Z0-9-]+/g, '');
    for (const c of value.match(COLOR) ?? []) found.push(`${prop}: ${c}`);
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

test('the rule finds fixed colors, not variables and shadows', () => {
  assert.deepEqual(fixedColors(':root { --ew-bg: #fff; } .a { color: var(--ew-text); box-shadow: 0 1px 2px rgb(0 0 0 / .2); }'), []);
  assert.deepEqual(fixedColors('.a { background: #fff; border: 1px solid rgba(0,0,0,.1); color: white }'), ['background: #fff', 'border: rgba(', 'color: white']);
  assert.deepEqual(fixedColors('.a { color: var(--ew-x, #ccc); white-space: nowrap; }'), ['color: #ccc']);
  assert.deepEqual(fixedColors('.ew-fg-red { color: var(--ew-style-red); }'), [], 'names of variables are no colors');
  assert.deepEqual(fixedColors('/* color: red */ .a { background: url(#x) }'), []);
});

test('fixed colors in stylesheets only become fewer', () => {
  const files = [...cssFiles('src'), ...cssFiles('../plugins')];
  assert.ok(files.length > 5);
  const base = baseline as Record<string, number>;
  const now: Record<string, number> = {};
  for (const f of files) {
    const n = fixedColors(fs.readFileSync(f, 'utf8')).length;
    if (n > 0) now[f.replace(/\\/g, '/')] = n;
  }
  for (const [f, n] of Object.entries(now)) {
    assert.ok(n <= (base[f] ?? 0), `${f}: ${n} fixed colors, allowed ${base[f] ?? 0}; use the color variables (--ew-*) instead`);
  }
  for (const [f, n] of Object.entries(base)) {
    assert.equal(now[f] ?? 0, n, `${f}: fewer fixed colors than in csscolors-baseline.json – lower the number there`);
  }
});
