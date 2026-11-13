// Overlapping requests: only the newest answer may be shown.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { newest } from '../../src/core/newest';

test('an older, slower answer does not overwrite a newer one', async () => {
  const fresh = newest();
  let shown = '';
  const load = async (value: string, wait: Promise<void>) => {
    const current = fresh();
    await wait;
    if (!current()) return;
    shown = value;
  };
  let releaseFirst!: () => void;
  const first = load('unfiltered', new Promise<void>((r) => { releaseFirst = r; }));
  await load('search result', Promise.resolve());
  releaseFirst();
  await first;
  assert.equal(shown, 'search result');
});

test('every guard is independent', () => {
  const a = newest();
  const b = newest();
  const ca = a();
  b();
  assert.equal(ca(), true);
  a();
  assert.equal(ca(), false);
});
