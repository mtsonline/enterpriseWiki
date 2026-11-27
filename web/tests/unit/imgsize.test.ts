// Images must not move the page when they load: a click aimed at a button
// below an image that is still loading lands elsewhere once the image has its
// size (an image preview pushed the Save button away; fixed with a fixed
// preview height).
//
// The rule for every image the frontend creates (h('img', …) and <img in
// HTML strings, in web/src and plugins/*/web):
//  1. it has width and height attributes (known size, or the size stored
//     with a medium; CSS may scale it, the base CSS sets height: auto), or
//  2. one of its classes has a CSS rule that fixes its height: height (not
//     auto, not a percentage, which depends on the parent), aspect-ratio, or
//     contain: size with a min-height, or
//  3. it lies in a box whose CSS fixes the height (BOXED: the selector named
//     must reserve the room), or
//  4. it is a listed exception with a reason (EXCEPTIONS).
// Stale entries in both lists fail too, so they do not outlive the code.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

const root = path.join(process.cwd(), '..');

/** an image in a box: the selector must have a rule that reserves the height */
const BOXED: { file: string; has: string; css: string }[] = [
  { file: 'web/src/components/settings.ts', has: 'h(\'img\', { src: `/api/v1/files/${hash}`', css: '.ew-image-preview' },
  { file: 'web/src/components/previewimage.ts', has: 'h(\'img\', { src: v, alt', css: '.ew-image-preview' },
  { file: 'web/src/components/profilecard.ts', has: 'h(\'img\', { src: `/api/v1/files/${p.photo}`', css: '.ew-card-photo img' },
  { file: 'web/src/views/content.ts', has: 'h(\'figure\', { class: \'ew-element-image\' }, h(\'img\'', css: '.ew-element-image img' },
  { file: 'plugins/core_carousel/web/index.ts', has: 'h(\'img\', { src: image, alt: \'\' })', css: '.ewcar-preview' },
];

/** images that may load without a reserved box, and why */
const EXCEPTIONS: { file: string; has: string; why: string }[] = [
  { file: 'web/src/components/media.ts', has: 'new Image()', why: 'not shown: the source of the canvas of the image editor' },
  { file: 'web/src/views/templateSettings.ts', has: 'new Image()', why: 'not shown: only reads the size of an uploaded image for the live preview' },
  { file: 'plugins/core_html/web/index.ts', has: '"${size}>`', why: 'width and height of the medium come in ${size} (when the medium has a size)' },
];

function walk(dir: string, ext: string, out: string[] = []): string[] {
  if (!fs.existsSync(dir)) return out;
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    if (e.name === 'node_modules' || e.name === 'generated') continue;
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p, ext, out);
    else if (p.endsWith(ext) && !p.endsWith('.d.ts')) out.push(p);
  }
  return out;
}
const plugins = fs.readdirSync(path.join(root, 'plugins')).map((p) => path.join(root, 'plugins', p, 'web'));
const sources = [path.join(root, 'web/src'), ...plugins].flatMap((d) => walk(d, '.ts'));
const css = [path.join(root, 'web/src/styles'), ...plugins].flatMap((d) => walk(d, '.css')).map((f) => fs.readFileSync(f, 'utf8')).join('\n');

/** CSS rules as [selectors, declarations] (comments removed; nested at-rules are flattened) */
const rules: [string[], string][] = [];
for (const m of css.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/([^{}@;]+)\{([^{}]*)\}/g)) {
  rules.push([m[1].split(',').map((s) => s.trim().replace(/\s+/g, ' ')), m[2]]);
}
/** the declarations reserve the height */
function reserves(decl: string): boolean {
  const d = `;${decl}`;
  if (/;\s*aspect-ratio\s*:/.test(d)) return true;
  if (/;\s*(height|block-size|min-height)\s*:\s*(?!auto\b)[^\s;%][^;%]*(;|$)/.test(d)) return true;
  return false;
}
/** a rule for exactly this selector (or a class selector ending in it) reserves the height */
function reservedBy(selector: string): boolean {
  return rules.some(([sels, decl]) => sels.some((s) => s === selector || s.endsWith(` ${selector}`) || s.endsWith(selector) && /[\s>]/.test(s.slice(0, -selector.length).slice(-1))) && reserves(decl));
}
/** a class reserves the height of the image itself (not min-height: that is a box) */
function classReserves(cls: string): boolean {
  const re = new RegExp(`\\.${cls.replace(/[-]/g, '\\-')}(?![\\w-])(:[\\w-]+)*$`);
  return rules.some(([sels, decl]) => sels.some((s) => re.test(s)) && (() => {
    const d = `;${decl}`;
    if (/;\s*aspect-ratio\s*:/.test(d) || /;\s*(height|block-size)\s*:\s*(?!auto\b)[^\s;%][^;%]*(;|$)/.test(d)) return true;
    return /;\s*contain\s*:\s*(size|strict)/.test(d) && /;\s*min-height\s*:/.test(d);
  })());
}

interface Img { file: string; line: number; text: string; attrs: string }
function images(): Img[] {
  const out: Img[] = [];
  for (const f of sources) {
    const src = fs.readFileSync(f, 'utf8');
    const rel = path.relative(root, f);
    const lineOf = (i: number) => src.slice(0, i).split('\n').length;
    const lineText = (i: number) => src.split('\n')[lineOf(i) - 1];
    for (const m of src.matchAll(/\bh\(\s*'img'\s*,\s*/g)) {
      const start = m.index + m[0].length;
      let attrs = '';
      if (src[start] === '{') {
        let depth = 0, i = start;
        for (; i < src.length; i++) {
          if (src[i] === '{') depth++;
          else if (src[i] === '}' && --depth === 0) break;
        }
        attrs = src.slice(start, i + 1);
      }
      out.push({ file: rel, line: lineOf(m.index), text: lineText(m.index), attrs });
    }
    for (const m of src.matchAll(/<img\b[^>]*>/g)) {
      if (/^\s*(\/\/|\*)/.test(lineText(m.index))) continue; // comments
      out.push({ file: rel, line: lineOf(m.index), text: lineText(m.index), attrs: m[0] });
    }
    for (const m of src.matchAll(/new Image\(\)|createElement\(\s*'img'\s*\)/g)) {
      out.push({ file: rel, line: lineOf(m.index), text: lineText(m.index), attrs: '' });
    }
  }
  return out;
}

test('images reserve their size before they load', () => {
  const all = images();
  assert.ok(all.length >= 15, `found only ${all.length} images: is the search still right?`);
  const bad: string[] = [];
  const usedBox = new Set<number>(), usedExc = new Set<number>();
  for (const img of all) {
    const a = img.attrs;
    if (/\bwidth\s*([:=,]|\})/.test(a) && /\bheight\s*([:=,]|\})/.test(a)) continue; // also { width, height } shorthand
    const classes = [...(/\bclass\s*:\s*([^,}]+(?:\?[^,}]+)?)/.exec(a)?.[1] ?? '').matchAll(/'([^']*)'/g)].flatMap((m) => m[1].split(/\s+/)).filter(Boolean);
    if (classes.some(classReserves)) continue;
    const b = BOXED.findIndex((x) => x.file === img.file && img.text.includes(x.has));
    if (b >= 0) {
      usedBox.add(b);
      if (!reservedBy(BOXED[b].css)) bad.push(`${img.file}:${img.line}: the box ${BOXED[b].css} does not reserve a height in the CSS`);
      continue;
    }
    const e = EXCEPTIONS.findIndex((x) => x.file === img.file && img.text.includes(x.has));
    if (e >= 0) { usedExc.add(e); continue; }
    bad.push(`${img.file}:${img.line}: ${img.text.trim().slice(0, 160)}`);
  }
  assert.deepEqual(bad, [], `images without a reserved size (give width and height, a class with a fixed height or aspect-ratio, or see the rule in web/tests/unit/imgsize.test.ts):\n${bad.join('\n')}`);
  assert.deepEqual(BOXED.filter((_, i) => !usedBox.has(i)).map((x) => `${x.file}: ${x.has}`), [], 'stale entries in BOXED');
  assert.deepEqual(EXCEPTIONS.filter((_, i) => !usedExc.has(i)).map((x) => `${x.file}: ${x.has}`), [], 'stale entries in EXCEPTIONS');
});

test('the rule of the image check itself', () => {
  assert.ok(reserves('width: 100%; aspect-ratio: 16 / 9'));
  assert.ok(reserves('min-height: calc(8rem + 2px); display: flex'));
  assert.ok(!reserves('max-height: 8rem; max-width: 16rem'));
  assert.ok(!reserves('height: auto'));
  assert.ok(!reserves('height: 100%'));
  assert.ok(!reserves('min-height: auto'));
});
