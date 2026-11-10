// Markdown round trips of the editor (parse -> serialize keeps the text).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parse, serialize } from '../../src/editor/markdown';

const same = [
  '# Title\n\nHello **bold** and *it* and ~~gone~~ and `code`.\n',
  '- one\n- two\n  - nested\n',
  '3. a\n4. b\n',
  '- [ ] open\n- [x] done\n',
  '> quote\n>\n> more\n',
  '```go\nx := 1\n```\n',
  '| A | B |\n| --- | :---: |\n| 1 | x \\| y |\n',
  '::toc{levels=3}\n',
  ':::panel{title="A note" type=info}\nText **inside**\n:::\n',
  '::::columns\n:::panel{type=info}\ninner\n:::\n\nafter\n::::\n',
  'Status :status[DONE]{color=green} ok\n',
  '![Alt text](/api/v1/media/3/view)\n',
  '[link](https://example.org "Title")\n',
  '<figure><img src="/a.png" alt="A"><figcaption>Cap</figcaption></figure>\n',
  'line one\\\nline two\n',
  'at 10:30 see \\:note and ::x\n',
];

for (const src of same) {
  test(`round trip: ${JSON.stringify(src.slice(0, 40))}`, () => {
    assert.equal(serialize(parse(src)), src);
  });
}

test('text that looks like a macro is escaped', () => {
  const doc = parse('plain');
  const tr = doc.type.schema.text(':status[x]{a=b} and ::toc');
  const p = doc.type.schema.node('paragraph', null, [tr]);
  const d = doc.type.schema.node('doc', null, [p]);
  const md = serialize(d);
  assert.equal(md, '\\:status\\[x\\]{a=b} and ::toc\n');
  assert.equal(parse(md).firstChild?.firstChild?.type.name, 'text');
});
