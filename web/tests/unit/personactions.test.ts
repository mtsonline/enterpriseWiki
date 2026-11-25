// Entries of plugins in the menu on names: the id carries the plugin id,
// "core." is the core's; only visible entries are offered.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { personActions, registerPersonAction, type Person } from '../../src/core/personactions';

const p = (email?: string): Person => ({ id: 7, display_name: 'Ann', handle: 'ann@local', card: {}, email, self: false });

test('ids need the namespace of a plugin', () => {
  for (const id of ['call', 'core.people.call', 'Chat.call', '.call', 'chat.']) {
    assert.throws(() => registerPersonAction({ id, label: () => 'x', run: () => undefined }), undefined, id);
  }
});

test('only visible entries are offered, a failing check hides the entry, the same id replaces', () => {
  registerPersonAction({ id: 'chat.example.call', label: () => 'Call', visible: (x) => x.id === 7, run: () => undefined });
  registerPersonAction({ id: 'chat.example.mail', label: () => 'Mail', visible: (x) => !!x.email, run: () => undefined });
  registerPersonAction({ id: 'chat.example.broken', label: () => 'Broken', visible: () => { throw new Error('no'); }, run: () => undefined });
  assert.deepEqual(personActions(p()).map((a) => a.id), ['chat.example.call']);
  assert.deepEqual(personActions(p('ann@example.com')).map((a) => a.id), ['chat.example.call', 'chat.example.mail']);
  registerPersonAction({ id: 'chat.example.call', label: () => 'Call', visible: () => false, run: () => undefined });
  assert.deepEqual(personActions(p('ann@example.com')).map((a) => a.id), ['chat.example.mail']);
});
