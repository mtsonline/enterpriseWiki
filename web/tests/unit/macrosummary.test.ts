// The head of a macro in the visual editor names its parameters readably.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { paramSummary } from '../../src/editor/macrosummary';
import type { MacroInfo } from '../../src/core/types';

const info: MacroInfo = {
  name: 'buttons', plugin: 'core.layout', labels: { en: 'Buttons', de: 'Knopfgruppe' }, description: {}, has_body: false,
  params: [
    { name: 'text1', labels: { en: 'Text of button 1', de: 'Text der Schaltfläche 1' }, type: 'text' },
    { name: 'style1', labels: { en: 'Style of button 1', de: 'Stil der Schaltfläche 1' }, type: 'select', options: ['primary', 'secondary'], default: 'primary',
      option_labels: { primary: { en: 'Prominent', de: 'Auffällig' }, secondary: { en: 'Subtle', de: 'Dezent' } } },
    { name: 'one', labels: { en: 'Only one open', de: 'Immer nur einer offen' }, type: 'bool' },
    { name: 'first', labels: { en: 'First one open', de: 'Erster offen' }, type: 'bool' },
    { name: 'options', labels: { en: 'Options', de: 'Optionen' }, type: 'custom:blockoptions' },
  ],
};

test('labels instead of names, options by their label', () => {
  assert.equal(paramSummary(info, { text1: 'Go', style1: 'secondary', one: 'true', first: 'false' }, 'de'),
    'Text der Schaltfläche 1: Go · Stil der Schaltfläche 1: Dezent · Immer nur einer offen');
  assert.equal(paramSummary(info, { text1: 'Go', style1: 'secondary' }, 'en'), 'Text of button 1: Go · Style of button 1: Subtle');
});

test('defaults, switched off options, empty values and block options are left out', () => {
  assert.equal(paramSummary(info, { style1: 'primary', one: 'false', text1: '', options: '{"padding":"m"}' }, 'de'), '');
});

test('unknown parameters and macros show name=value; long summaries are cut', () => {
  assert.equal(paramSummary(undefined, { a: '1', b: 'x' }, 'de'), 'a=1 · b=x');
  const long = paramSummary(info, { text1: 'x'.repeat(200) }, 'en');
  assert.equal(long.length, 100);
  assert.ok(long.endsWith('…'));
});
