// The logo files and the sizes the code and the CSS assume for them (the
// width of the header logo is reserved before the image loads).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

const root = path.join(process.cwd());
/** width and height of a WebP file (extended format VP8X, as written for images with alpha) */
function webpSize(file: string): [number, number] {
  const b = fs.readFileSync(path.join(root, 'public/brand', file));
  assert.equal(b.toString('ascii', 0, 4), 'RIFF', file);
  assert.equal(b.toString('ascii', 8, 12), 'WEBP', file);
  assert.equal(b.toString('ascii', 12, 16), 'VP8X', `${file}: extended WebP (with alpha)`);
  return [1 + b.readUIntLE(24, 3), 1 + b.readUIntLE(27, 3)];
}

test('the logos have the sizes the code and the CSS reserve', () => {
  const [w, h] = webpSize('logo.webp');
  const [wi, hi] = webpSize('logo-ink.webp');
  assert.ok(Math.abs(w / h - wi / hi) < 0.02, 'colour and ink logo have the same proportions');
  const ts = fs.readFileSync(path.join(root, 'src/components/brand.ts'), 'utf8');
  const css = fs.readFileSync(path.join(root, 'src/styles/base.css'), 'utf8');
  const m = /const LOGO_RATIO = (\d+) \/ (\d+);/.exec(ts);
  assert.ok(m, 'LOGO_RATIO in brand.ts');
  assert.deepEqual([Number(m[1]), Number(m[2])], [w, h], 'LOGO_RATIO = size of logo.webp');
  assert.ok(css.includes(`aspect-ratio: ${w} / ${h}`), 'aspect-ratio of the header logo in base.css');
  for (const f of ['mark.webp', 'mark-ink.webp']) {
    const [mw, mh] = webpSize(f);
    assert.equal(mw, mh, `${f} is square`);
  }
  // sharp up to the largest header height (60 px) on screens with twice the pixels
  assert.ok(h >= 120, `logo.webp is ${h} px high`);
});

test('the brand files stay small', () => {
  for (const f of fs.readdirSync(path.join(root, 'public/brand'))) {
    const size = fs.statSync(path.join(root, 'public/brand', f)).size;
    assert.ok(size < 40_000, `${f}: ${size} bytes`);
  }
});
