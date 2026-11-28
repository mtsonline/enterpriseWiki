// The colours of the CMS templates (theme.cms and theme.cms.modern) in the
// light and the dark scheme: text 4.5:1, borders of controls 3:1 (WCAG 1.4.3,
// 1.4.11). The colours chosen in the settings: templatesettings.test.ts.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { contrast } from '../../src/core/blockoptions';

const strip = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, '');
const read = (p: string) => strip(fs.readFileSync(path.join(process.cwd(), '..', p), 'utf8'));
const cms = read('plugins/theme_cms/web/style.css');
const modern = read('plugins/theme_cms_modern/web/style.css');

/** the variables of the rules with exactly this selector: hex values, and the
 * fallback of var(--x, #hex) */
function vars(css: string, selector: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [, sel, body] of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (sel.trim() !== selector) continue;
    for (const [, k, v] of body.matchAll(/(--ew[a-z0-9-]*):\s*(?:var\(--[a-z0-9-]+,\s*)?(#[0-9a-f]{6})\b/gi)) out[k] = v.toLowerCase();
  }
  return out;
}

const cmsLight = vars(cms, '.ewcms-app');
const cmsDark = { ...cmsLight, ...vars(cms, 'body.ewc-dark .ewcms-app') };
const modLight = { ...cmsLight, ...vars(modern, '.ewcms-modern .ewcms-app') };
const modDark = { ...modLight, ...vars(cms, 'body.ewc-dark .ewcms-app'), ...vars(modern, 'body.ewc-dark.ewcms-modern .ewcms-app') };

const CMS_TEXT: [string, string][] = [
  ['--ewcms-text', '--ewcms-surface'], ['--ewcms-text', '--ewcms-page-bg'], ['--ewcms-text', '--ewcms-aside-bg'],
  ['--ewcms-heading', '--ewcms-surface'], ['--ewcms-heading', '--ewcms-aside-bg'], ['--ewcms-lead', '--ewcms-surface'],
  ['--ewcms-muted', '--ewcms-surface'], ['--ewcms-muted', '--ewcms-page-bg'], ['--ewcms-strong', '--ewcms-surface'],
  ['--ewcms-on-strong', '--ewcms-strong'], ['--ew-primary', '--ewcms-surface'], ['--ew-primary', '--ewcms-page-bg'], ['--ew-primary', '--ewcms-aside-bg'],
];
const MODERN_TEXT: [string, string][] = [
  ...CMS_TEXT.filter(([, b]) => b !== '--ewcms-aside-bg'),
  ['--ewcms-text', '--ewcms-soft'], ['--ewcms-text', '--ewcms-hover2'], ['--ewcms-muted', '--ewcms-soft'], ['--ewcms-ink', '--ewcms-soft'],
  ['--ewcms-topbar-link', '--ewcms-soft'], ['--ewcms-topbar-link', '--ewcms-hover2'], ['--ew-primary', '--ewcms-soft'],
];
const MODERN_NONTEXT: [string, string][] = [['--ewcms-input-line', '--ewcms-surface'], ['--ewcms-input-line', '--ewcms-soft']];

function check(name: string, v: Record<string, string>, pairs: [string, string][], min: number): void {
  for (const [fg, bg] of pairs) {
    assert.ok(v[fg] && v[bg], `${name}: ${fg} or ${bg} missing`);
    const c = contrast(v[fg], v[bg]);
    assert.ok(c >= min, `${name}: ${fg} ${v[fg]} on ${bg} ${v[bg]}: ${c.toFixed(2)} < ${min}`);
  }
}

test('theme.cms: text and links in both schemes', () => {
  check('light', cmsLight, CMS_TEXT, 4.5);
  check('dark', cmsDark, CMS_TEXT, 4.5);
});

test('theme.cms.modern: text, links and inputs in both schemes', () => {
  check('light', modLight, MODERN_TEXT, 4.5);
  check('dark', modDark, MODERN_TEXT, 4.5);
  check('light', modLight, MODERN_NONTEXT, 3);
  check('dark', modDark, MODERN_NONTEXT, 3);
});

test('the dark scheme sets every colour of the light one (no light colour left in dark)', () => {
  const same = new Set(['--ewcms-brand', '--ewcms-deep']);
  for (const [l, d, name] of [[cmsLight, vars(cms, 'body.ewc-dark .ewcms-app'), 'theme.cms'],
    [vars(modern, '.ewcms-modern .ewcms-app'), { ...vars(cms, 'body.ewc-dark .ewcms-app'), ...vars(modern, 'body.ewc-dark.ewcms-modern .ewcms-app') }, 'theme.cms.modern']] as const) {
    for (const k of Object.keys(l)) {
      if (same.has(k) || k === '--ewcms-dark' || k === '--ewcms-darker') continue;
      assert.ok(k in d, `${name}: ${k} has no dark value`);
    }
  }
});
