// The color variables of the core in the light and the dark scheme: every
// pair of text and background keeps 4.5:1, borders and controls 3:1 (WCAG
// 1.4.3, 1.4.11). Both schemes define the same variables.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { contrast } from '../../../plugins/core_template/web/contrast';

const strip = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, '');
const base = strip(fs.readFileSync(path.join(process.cwd(), 'src/styles/base.css'), 'utf8'));
const macros = strip(fs.readFileSync(path.join(process.cwd(), '../plugins/core_macros/web/style.css'), 'utf8'));

/** the hex colors declared in the rules with exactly this selector */
function vars(css: string, selector: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [, sel, body] of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (sel.trim() !== selector) continue;
    for (const [, k, v] of body.matchAll(/(--ewl?[a-z0-9-]*):\s*(#[0-9a-f]{6})\b/gi)) out[k] = v;
  }
  return out;
}

const TEXT: [string, string][] = [
  ['--ew-text', '--ew-bg'], ['--ew-text', '--ew-bg-subtle'], ['--ew-text', '--ew-bg-raised'], ['--ew-text-muted', '--ew-bg'],
  ['--ew-text-muted', '--ew-bg-subtle'], ['--ew-link', '--ew-bg'], ['--ew-on-primary', '--ew-primary'], ['--ew-on-danger', '--ew-danger'],
  ['--ew-ok-text', '--ew-ok-bg'], ['--ew-warn-text', '--ew-warn-bg'], ['--ew-info-text', '--ew-info-bg'], ['--ew-danger-text', '--ew-danger-bg'],
  ['--ew-toast-text', '--ew-toast-bg'], ['--ew-badge-text', '--ew-badge-bg'], ['--ew-pos-label', '--ew-pos-label-bg'],
  ['--ew-pos-bar-text', '--ew-pos-bar-bg'], ['--ew-class2-text', '--ew-class2-bg'], ['--ew-note-text', '--ew-note-bg'],
  ['--ew-status-grey-text', '--ew-status-grey-bg'], ['--ew-status-blue-text', '--ew-status-blue-bg'], ['--ew-status-green-text', '--ew-status-green-bg'],
  ['--ew-status-yellow-text', '--ew-status-yellow-bg'], ['--ew-status-red-text', '--ew-status-red-bg'],
  ['--ew-status-orange-text', '--ew-status-orange-bg'], ['--ew-status-purple-text', '--ew-status-purple-bg'], ['--ew-status-teal-text', '--ew-status-teal-bg'],
  ['--ew-chart-bar-text', '--ew-chart-bar'], ['--ew-inverse-text', '--ew-inverse-bg'], ['--ew-inverse-link', '--ew-inverse-bg'],
  ['--ew-cta-text', '--ew-cta-bg'], ['--ew-cta-text', '--ew-cta-hover'], ['--ew-console-text', '--ew-console-bg'],
  ['--ew-danger', '--ew-bg'],
];
const NONTEXT: [string, string][] = [
  ['--ew-input-border', '--ew-bg'], ['--ew-focus', '--ew-bg'], ['--ew-info-border', '--ew-bg'], ['--ew-ok-border', '--ew-bg'],
  ['--ew-warn-border', '--ew-bg'], ['--ew-star', '--ew-bg'], ['--ew-note-border', '--ew-bg'], ['--ew-primary', '--ew-bg'],
  ['--ew-pos-outline', '--ew-bg'], ['--ew-class2-border', '--ew-bg'], ['--ew-chart-bar', '--ew-bg'],
  ...Array.from({ length: 12 }, (_, i): [string, string] => [`--ewl-chart-${i + 1}`, '--ew-bg']),
];

const light = vars(base, ':root');
const dark = { ...light, ...vars(base, 'body.ewc-dark') };

for (const [name, v] of [['light', light], ['dark', dark]] as const) {
  test(`core colors keep their contrast (${name})`, () => {
    for (const [f, b] of TEXT) {
      assert.ok(v[f] && v[b], `${f} and ${b} are defined`);
      assert.ok(contrast(v[f], v[b]) >= 4.5, `${f} ${v[f]} on ${b} ${v[b]}: ${contrast(v[f], v[b]).toFixed(2)}`);
    }
    for (const [f, b] of NONTEXT) {
      assert.ok(v[f] && v[b], `${f} and ${b} are defined`);
      assert.ok(contrast(v[f], v[b]) >= 3, `${f} ${v[f]} on ${b} ${v[b]}: ${contrast(v[f], v[b]).toFixed(2)}`);
    }
  });
}

test('the dark scheme sets every color of the light one that depends on it', () => {
  const own = vars(base, 'body.ewc-dark');
  // colors that are the same in both schemes on purpose
  const same = new Set(['--ew-frame-bg', '--ew-on-color', '--ew-inverse-bg', '--ew-inverse-text', '--ew-inverse-link', '--ew-hero-to', '--ew-cta-bg', '--ew-cta-text', '--ew-cta-hover']);
  for (const k of Object.keys(light)) if (!same.has(k)) assert.ok(own[k], `${k} has a dark value`);
});

for (const [name, sel] of [['light', ':root'], ['dark', 'body.ewc-dark']] as const) {
  test(`text styles keep 4.5:1 on the page and on every highlight (${name})`, () => {
    const p = vars(macros, sel);
    const fg = Object.entries(p).filter(([k]) => !k.includes('-hl-'));
    const bg = [...Object.entries(p).filter(([k]) => k.includes('-hl-')).map(([, v]) => v), name === 'light' ? light['--ew-bg'] : dark['--ew-bg'], name === 'light' ? light['--ew-bg-raised'] : dark['--ew-bg-raised']];
    assert.ok(fg.length >= 6 && bg.length >= 7);
    for (const [k, f] of fg) for (const b of bg) assert.ok(contrast(f, b) >= 4.5, `${k} ${f} on ${b}: ${contrast(f, b).toFixed(2)}`);
  });
}
