// Options of placed blocks: the browser decides like the server (the shared
// cases of internal/content/testdata/blockoptions_cases.json), the CSS comes
// from valid values only, and the display rules.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formOK, optionsCSS, optionsKey, problems, shownNow, type BlockOptions } from '../../src/core/blockoptions';
import cases from '../../../internal/content/testdata/blockoptions_cases.json';

test('the browser decides like the server (shared cases)', () => {
  assert.ok(cases.length >= 20);
  for (const c of cases as { name: string; ok: boolean; options: BlockOptions }[]) {
    const ps = problems(c.options);
    assert.equal(ps.length === 0, c.ok, `${c.name}: ${JSON.stringify(ps)}`);
  }
});

test('CSS only from values of a valid form', () => {
  const sel = '.ew-bo[data-bo="x"]';
  assert.equal(optionsCSS(sel, { bg_color: 'red;} body{display:none', text_color: '#ffffff' }), '');
  assert.equal(optionsCSS(sel, { css: 'x' } as unknown as BlockOptions), '');
  assert.equal(optionsCSS(sel, { bg_image: 'x") ; } body { y' }), '');
  const css = optionsCSS(sel, { bg_color: '#102040', text_color: '#ffffff', padding_top: { desktop: 6, mobile: 2 }, hide: ['mobile'], radius: 12, columns: { desktop: 3, mobile: 1 } });
  assert.match(css, /background: #102040;/);
  assert.match(css, /color: #ffffff;/);
  assert.match(css, /padding-top: 2rem;/);
  assert.match(css, /@media \(max-width: 639\.98px\) \{ \.ew-bo\[data-bo="x"\] \{ padding-top: 0.5rem; columns: 1 12rem;/);
  assert.match(css, /@media \(max-width: 639\.98px\) \{ \.ew-bo\[data-bo="x"\] \{ display: none !important; \} \}/);
  assert.match(css, /a:not\(\.ew-btn\) \{ color: inherit; text-decoration: underline; \}/, 'links keep the checked text color and are underlined');
  // a picture with an overlay, its dividers, limited lines
  const pic = optionsCSS(sel, { bg_image: '/api/v1/files/abc.png', overlay_color: '#000000', overlay_opacity: 75, text_color: '#ffffff', divider_bottom: 'wave', line_clamp: 3, bg_fixed: true });
  assert.match(pic, /url\("\/api\/v1\/files\/abc\.png"\)/);
  assert.match(pic, /color-mix\(in srgb, #000000 75%, transparent\)/);
  assert.match(pic, /::after \{ content: ""/);
  assert.match(pic, /-webkit-line-clamp: 3/);
  assert.match(pic, /prefers-reduced-motion: no-preference/, 'parallax only without reduced motion');
  assert.ok(formOK({}));
  assert.equal(optionsKey({ a: 1, b: 2 } as unknown as BlockOptions), optionsKey({ b: 2, a: 1 } as unknown as BlockOptions));
});

test('display rules: audience, time, language', () => {
  const now = Date.parse('2026-12-10T12:00:00Z');
  assert.ok(shownNow(undefined, now, false, 'de'));
  assert.ok(!shownNow({ audience: 'signed_in' }, now, false, 'de'));
  assert.ok(shownNow({ audience: 'signed_in' }, now, true, 'de'));
  assert.ok(!shownNow({ audience: 'signed_out' }, now, true, 'de'));
  assert.ok(!shownNow({ from: '2026-12-11T00:00:00Z' }, now, true, 'de'));
  assert.ok(!shownNow({ until: '2026-12-10T12:00:00Z' }, now, true, 'de'));
  assert.ok(shownNow({ from: '2026-12-01T00:00:00Z', until: '2026-12-24T00:00:00Z' }, now, true, 'de'));
  assert.ok(!shownNow({ langs: ['en'] }, now, true, 'de'));
  assert.ok(shownNow({ langs: ['en', 'de'] }, now, true, 'de'));
});
