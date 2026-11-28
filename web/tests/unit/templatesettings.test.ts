// The settings the templates declare (plugin.json, "template_settings"):
// their defaults are the look the template is built with, so every colour
// with contrast_on keeps 4.5:1 to the colours it stands on, and every CSS
// variable a setting writes is one the template (or the core) reads.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { contrast } from '../../src/core/blockoptions';

interface Setting { key: string; kind: string; default?: unknown; contrast_on?: string[]; css_vars?: string[] }
const root = path.join(process.cwd(), '..');
const manifests = fs.readdirSync(path.join(root, 'plugins')).map((d) => path.join(root, 'plugins', d, 'plugin.json'))
  .filter((p) => fs.existsSync(p)).map((p) => ({ dir: path.dirname(p), m: JSON.parse(fs.readFileSync(p, 'utf8')) as { id: string; template_settings?: Setting[] } }))
  .filter((x) => x.m.template_settings?.length);

test('the templates of the wiki declare settings', () => {
  const ids = manifests.map((x) => x.m.id).sort();
  for (const id of ['theme.cms', 'theme.cms.modern', 'theme.wiki']) assert.ok(ids.includes(id), `${id} declares no settings`);
});

test('the default colours keep 4.5:1 where they stand on each other', () => {
  for (const { m } of manifests) {
    const byKey = new Map(m.template_settings!.map((s) => [s.key, s]));
    for (const s of m.template_settings!) {
      for (const on of s.contrast_on ?? []) {
        const bg = on.startsWith('#') ? on : String(byKey.get(on)?.default ?? '');
        const c = contrast(String(s.default), bg);
        assert.ok(c >= 4.5, `${m.id}: ${s.key} ${String(s.default)} on ${on} ${bg}: ${c.toFixed(2)}`);
      }
    }
  }
});

test('every CSS variable of a setting is read by the template or the core', () => {
  const core = fs.readFileSync(path.join(root, 'web/src/styles/base.css'), 'utf8');
  for (const { dir, m } of manifests) {
    const own = fs.readdirSync(path.join(dir, 'web')).filter((f) => f.endsWith('.css')).map((f) => fs.readFileSync(path.join(dir, 'web', f), 'utf8')).join('\n');
    // a variant reads the CSS of the template it extends too
    const base = m.id === 'theme.cms.modern' ? fs.readFileSync(path.join(root, 'plugins/theme_cms/web/style.css'), 'utf8') : '';
    for (const s of m.template_settings!) {
      for (const v of s.css_vars ?? []) {
        assert.ok(`${own}\n${base}\n${core}`.includes(`var(${v}`), `${m.id}: ${s.key} writes ${v}, which nothing reads`);
      }
    }
  }
});
