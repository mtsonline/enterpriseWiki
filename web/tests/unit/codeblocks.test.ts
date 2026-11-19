// Code blocks: the token colours keep 4.5:1 on their background (also the
// marked lines of a diff), and the language of a fence info is recognised.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { contrast } from '../../../plugins/core_template/web/contrast';
import { codeLanguage, codeLines, CODE_LANGUAGES } from '../../src/core/codeblocks';

const css = fs.readFileSync(path.join(process.cwd(), 'src/styles/code.css'), 'utf8');

test('every colour of a code token has 4.5:1 on the code background', () => {
  const bg = /--ew-code-bg:\s*(#[0-9a-f]{6})/i.exec(css)![1];
  const rules = [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)];
  let n = 0;
  for (const [, sel, body] of rules) {
    const fg = /(?:^|;|\s)color:\s*(#[0-9a-f]{6})/i.exec(body)?.[1];
    if (!fg) continue;
    const own = /background:\s*(#[0-9a-f]{6})/i.exec(body)?.[1] ?? bg;
    n++;
    assert.ok(contrast(fg, own) >= 4.5, `${sel.trim()}: ${fg} on ${own} is ${contrast(fg, own).toFixed(2)}`);
  }
  for (const v of ['--ew-code-text', '--ew-code-muted']) {
    const c = new RegExp(`${v}:\\s*(#[0-9a-f]{6})`, 'i').exec(css)![1];
    assert.ok(contrast(c, bg) >= 4.5, `${v}`);
  }
  assert.ok(n >= 10, `${n} colours checked`);
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
