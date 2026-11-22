// The formulas the wiki sets keep their parts: every element and attribute
// Temml writes for usual formulas is on the list the browser keeps.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import temml from 'temml';
import { ATTRS, ELEMENTS } from '../../src/core/math';

const corpus = [
  'x^2 + y^2 = z^2', '\\frac{a}{b}', '\\sqrt[3]{x}', '\\sum_{i=1}^{n} i = \\frac{n(n+1)}{2}', '\\int_0^\\infty e^{-x}\\,dx',
  '\\begin{pmatrix} a & b \\\\ c & d \\end{pmatrix}', '\\begin{cases} 1 & x>0 \\\\ 0 & \\text{sonst} \\end{cases}', '\\lim_{x\\to 0} \\frac{\\sin x}{x}',
  '\\vec{v} \\cdot \\hat{n}', '\\overline{AB}', '\\underbrace{a+b}_{c}', '\\mathbb{R}', '\\mathcal{L}', '\\alpha\\beta\\gamma',
  '{}^{14}_{6}C', '\\left( \\frac{1}{2} \\right)', '\\color{red}{x}', '\\cancel{x}', '\\binom{n}{k}', 'f\'(x)', '\\begin{aligned} a &= b \\\\ c &= d \\end{aligned}',
];

test('Temml writes only what the browser keeps', () => {
  const tags = new Set<string>();
  const attrs = new Set<string>();
  for (const tex of corpus) {
    for (const block of [false, true]) {
      const s = temml.renderToString(tex, { displayMode: block, throwOnError: true, trust: false, annotate: false });
      for (const m of s.matchAll(/<([a-z]+)((?:\s+[a-z-]+="[^"]*")*)\s*\/?>/g)) {
        tags.add(m[1]);
        for (const a of m[2].matchAll(/([a-z-]+)="([^"]*)"/g)) {
          if (a[1] !== 'xmlns') attrs.add(`${a[1]}=${a[2]}`);
        }
      }
    }
  }
  assert.ok(tags.has('mfrac') && tags.has('mtable') && attrs.size > 0, 'the corpus was read');
  const missingTags = [...tags].filter((x) => !ELEMENTS.has(x));
  assert.deepEqual(missingTags, [], 'elements not kept');
  const missingAttrs = [...attrs].filter((x) => {
    const [k, ...v] = x.split('=');
    const re = ATTRS[k];
    return !re || !re.test(v.join('='));
  });
  // style is never kept (colours come with mathcolor)
  assert.deepEqual(missingAttrs.filter((x) => !x.startsWith('style=')), [], 'attributes not kept');
});

test('nothing of trust: links and HTML are not made', () => {
  for (const tex of ['\\href{javascript:alert(1)}{x}', '\\url{https://example.com}', '\\htmlClass{x}{y}', '\\includegraphics{a.png}']) {
    const s = temml.renderToString(tex, { throwOnError: false, trust: false });
    assert.ok(!/href=|<img|<a\b|javascript:/i.test(s), `${tex}: ${s}`);
  }
});
