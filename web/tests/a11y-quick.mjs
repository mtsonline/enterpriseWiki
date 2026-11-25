// Quick accessibility run: axe (WCAG 2.2 AA) and the overlap check on the key
// pages, at three widths, in the light and the dark scheme and with the skip
// link focused. A few minutes instead of the full browser tests.
//
//   EWIKI_E2E_URL=http://localhost:8080 EWIKI_E2E_USER=admin EWIKI_E2E_PASSWORD=... npm run test:a11y
//
// It starts nothing itself: run it against a wiki set up with "ewiki setup"
// (welcome space). It changes nothing in the wiki except the colour scheme of
// the user, which it sets back at the end.
//
// Optional: EWIKI_A11Y_ALL_ADMIN=1 checks every administration page (from its
// navigation) at every width, not only the key ones.
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { axeSource, checkPage, checkSkipLink } from './a11y-lib.mjs';

const BASE = process.env.EWIKI_E2E_URL ?? 'http://localhost:8080';
const USER = process.env.EWIKI_E2E_USER ?? 'admin';
const PASSWORD = process.env.EWIKI_E2E_PASSWORD ?? 'Admin-Passw0rd!';
const ALL_ADMIN = process.env.EWIKI_A11Y_ALL_ADMIN === '1';
const WIDTHS = [390, 800, 1280];
const SCHEMES = ['light', 'dark'];
// pages on which the focused skip link is checked as well
const SKIP = new Set(['start page', 'page', 'guest start page']);

if (!axeSource) throw new Error('axe-core is required (npm ci)');

let browser, ctx, page;
let oldScheme = '';
const errors = [];

before(async () => {
  browser = await chromium.launch();
  // bypassCSP: the strict CSP of the wiki (script-src 'self') would block injecting axe-core
  ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  page = await ctx.newPage();
  page.on('pageerror', (e) => errors.push(e.message));
});
after(async () => {
  if (page && oldScheme !== null) await call('PUT', '/api/v1/scheme', { scheme: oldScheme }).catch(() => undefined);
  await browser?.close();
});

async function idle(pg = page) {
  await pg.waitForLoadState('networkidle');
  await pg.waitForFunction(() => !document.querySelector('main[aria-busy="true"]'));
}

const call = (method, path, body) => page.evaluate(async ([method, path, body]) => {
  const res = await fetch(path, { method, headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'ewiki' }, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${text}`);
  return text ? JSON.parse(text) : null;
}, [method, path, body]);

/** Checks one page at every width and scheme; collects the findings instead of stopping at the first. */
async function sweep(name, path, pg = page, schemes = SCHEMES, widths = WIDTHS) {
  const found = [];
  for (const scheme of schemes) {
    await pg.evaluate(async (s) => {
      await fetch('/api/v1/scheme', { method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'ewiki' }, body: JSON.stringify({ scheme: s }) });
    }, scheme);
    for (const width of widths) {
      await pg.setViewportSize({ width, height: 900 });
      await pg.goto(BASE + path);
      await idle(pg);
      const label = `${name} (${path}, ${width} px, ${scheme})`;
      try { await checkPage(label, pg); } catch (e) { found.push(e.message); }
      if (SKIP.has(name)) {
        try { await checkSkipLink(label, pg); } catch (e) { found.push(e.message); }
      }
    }
  }
  assert.deepEqual(found, [], found.join('\n\n'));
}

test('guest pages', async () => {
  const guest = await browser.newPage({ viewport: { width: 1280, height: 900 }, bypassCSP: true });
  try {
    await guest.goto(BASE + '/');
    await idle(guest);
    await sweep('guest start page', '/', guest);
    await sweep('login', '/login', guest);
  } finally {
    // the scheme of a guest lives in a cookie of its own context
    await guest.context().close();
  }
});

test('login', async () => {
  await page.goto(BASE + '/login');
  await idle();
  await page.getByLabel(/user ?name|benutzername/i).fill(USER);
  await page.getByLabel(/^(password|passwort)/i).fill(PASSWORD);
  await page.locator('form.ew-login button[type=submit]').click();
  await page.waitForSelector('.ew-user-menu');
  oldScheme = await page.evaluate(() => document.documentElement.dataset.schemeChoice ?? '');
});

test('key pages', async () => {
  await page.goto(BASE + '/');
  await idle();
  const cat = await page.locator('.ew-tree a').first().getAttribute('href');
  await page.goto(BASE + cat);
  await idle();
  // the first page of the first category (the welcome space has some)
  const el = await page.locator('main a[href^="/e/"]').first().getAttribute('href').catch(() => null);
  const pages = [['start page', '/'], ['category', cat], ['search', '/search?q=wiki'], ['profile', '/profile'], ['dashboard', '/dashboard'],
    ['spaces', '/spaces'], ['media', '/media'], ['tasks', '/tasks']];
  if (el) pages.splice(2, 0, ['page', el], ['history', `${el}/history`]);
  const found = [];
  for (const [name, path] of pages) {
    try { await sweep(name, path); } catch (e) { found.push(e.message); }
  }
  assert.deepEqual(found, [], found.join('\n\n'));
});

test('administration pages', async () => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(`${BASE}/admin`);
  await idle();
  const sections = await page.locator('.ew-admin nav.ew-subnav a').evaluateAll((as) => as.map((a) => a.getAttribute('href')));
  assert.ok(sections.length >= 20, `administration pages: ${sections}`);
  const key = new Set(['/admin', '/admin/license', '/admin/users', '/admin/plugins', '/admin/settings']);
  const found = [];
  for (const href of sections) {
    const all = ALL_ADMIN || key.has(href);
    // the other pages once (light, wide): their layout is the one of the administration
    try { await sweep(href.slice(1), href, page, all ? SCHEMES : ['light'], all ? WIDTHS : [1280]); } catch (e) { found.push(e.message); }
  }
  assert.deepEqual(found, [], found.join('\n\n'));
});

test('no script errors', () => {
  assert.deepEqual(errors, []);
});
