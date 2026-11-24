// Code blocks: the token colours keep 4.5:1 on their background (also the
// marked lines of a diff), and the language of a fence info is recognised.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { contrast } from '../../../plugins/core_template/web/contrast';
import { codeLanguage, codeLines, CODE_LANGUAGES } from '../../src/core/codeblocks';

const css = fs.readFileSync(path.join(process.cwd(), 'src/styles/code.css'), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');

/** the custom properties declared in the rule with exactly this selector */
function palette(selector: string): Record<string, string> {
  const m = [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)].find(([, sel]) => sel.trim() === selector);
  assert.ok(m, `rule ${selector}`);
  return Object.fromEntries([...m![2].matchAll(/(--ew-[a-z0-9-]+):\s*(#[0-9a-f]{6})/gi)].map(([, k, v]) => [k, v]));
}

for (const [scheme, selector] of [['light', '.ew-codeblock'], ['dark', '.ewc-dark .ew-codeblock']]) {
  test(`every colour of a code token has 4.5:1 on the code background (${scheme})`, () => {
    const p = palette(selector);
    const bg = p['--ew-code-bg'];
    let n = 0;
    for (const [k, v] of Object.entries(p)) {
      if (k === '--ew-code-bg' || k === '--ew-code-border' || k.endsWith('-bg')) continue;
      const own = p[`${k}-bg`] ?? bg; // marked lines of a diff have their own background
      n++;
      assert.ok(contrast(v, own) >= 4.5, `${k}: ${v} on ${own} is ${contrast(v, own).toFixed(2)}`);
    }
    assert.ok(contrast(p['--ew-code-border'], bg) >= 1.2, 'the border is visible');
    assert.ok(n >= 12, `${n} colours checked`);
  });
}

test('every token colour of the code comes from the palette', () => {
  const light = Object.keys(palette('.ew-codeblock')).sort();
  assert.deepEqual(Object.keys(palette('.ewc-dark .ew-codeblock')).sort(), light, 'both schemes have the same colours');
  for (const [, sel, body] of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (!sel.includes('hljs-')) continue;
    for (const [, v] of body.matchAll(/(?:^|;|\s)(?:color|background):\s*([^;]+)/g)) {
      if (/^(Canvas|CanvasText)$/.test(v.trim())) continue; // forced colours
      assert.match(v.trim(), /^var\(--ew-hl-[a-z-]+\)$/, `${sel.trim()}: ${v}`);
    }
  }
});

test('the language of a code block', () => {
  assert.equal(codeLanguage('go'), 'go');
  assert.equal(codeLanguage('JS lines'), 'javascript');
  assert.equal(codeLanguage('html'), 'xml');
  assert.equal(codeLanguage('brainfuck'), '');
  assert.equal(codeLanguage('constructor'), '', 'no names of the object prototype');
  assert.equal(codeLanguage(''), '');
  assert.ok(codeLines('go lines'));
  assert.ok(!codeLines('lines'), 'the first word is the language');
  assert.ok(Object.keys(CODE_LANGUAGES).length >= 25);
});
