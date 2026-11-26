// The hint on long sentences and words counts like the server (the shared
// cases of internal/readability/testdata/cases.json) and finds the places
// in the text it marks.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { active, check, DEFAULT_RULES, longWords, markdownProse, sentences, wordCount } from '../../src/core/readability';
import cases from '../../../internal/readability/testdata/cases.json';

test('the browser counts like the server (shared cases)', () => {
  assert.ok(cases.texts.length >= 10);
  for (const c of cases.texts) {
    assert.deepEqual(sentences(c.text).map((s) => wordCount(s.text)), c.words, `${c.name}: words per sentence`);
    assert.deepEqual(longWords(c.text, DEFAULT_RULES.wordLetters).map((w) => w.word), c.long_words, `${c.name}: long words`);
  }
});

test('the site decides, the person within that (shared cases)', () => {
  for (const c of cases.active) assert.equal(active(c.site, c.pref), c.active, `site ${c.site}, person ${c.pref}`);
});

test('the places of long sentences and words in the original text', () => {
  const long = Array.from({ length: 21 }, (_, i) => `w${i}`).join(' ');
  const text = `Short one. ${long}. See \`a b c\` and https://example.com/x then Donaudampfschifffahrtsgesellschaft.`;
  const f = check(text, DEFAULT_RULES);
  assert.equal(f.length, 2);
  assert.equal(f[0].kind, 'sentence');
  assert.equal(f[0].count, 21);
  assert.equal(text.slice(f[0].start, f[0].end), `${long}.`, 'the sentence with its full stop');
  assert.equal(f[1].kind, 'word');
  assert.equal(text.slice(f[1].start, f[1].end), 'Donaudampfschifffahrtsgesellschaft');
  // code and addresses are one word, but the place is that of the original
  const g = check(`${'a '.repeat(19)}\`x y z\` https://example.com/a-b.`, { sentenceWords: 20, wordLetters: 22 });
  assert.equal(g.length, 1);
  assert.equal(g[0].count, 21);
  assert.equal(g[0].end, `${'a '.repeat(19)}\`x y z\` https://example.com/a-b.`.length);
  // own limits
  assert.equal(check('one two three four five six.', { sentenceWords: 5, wordLetters: 22 }).length, 1);
  assert.equal(check('Haushaltsplan', { sentenceWords: 20, wordLetters: 12 })[0]?.count, 13);
  assert.deepEqual(check('', DEFAULT_RULES), []);
});

test('Markdown: code blocks and block lines are not checked, the places stay', () => {
  const words = Array.from({ length: 25 }, (_, i) => `c${i}`).join(' ');
  const src = `Intro.\n\n\`\`\`\n${words}\n\`\`\`\n\n:::panel{type=info title="${words}"}\nInside.\n:::\n\n~~~go\n${words}\n~~~\nEnd.`;
  const prose = markdownProse(src);
  assert.equal(prose.length, src.length);
  assert.deepEqual(check(prose, DEFAULT_RULES), []);
  assert.equal(prose.slice(src.indexOf('Inside.'), src.indexOf('Inside.') + 7), 'Inside.');
  assert.ok(prose.endsWith('End.'));
});
